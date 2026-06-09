package daemon_test

// pasteinject_hk2hb2y_test.go — scenario tests for substrate wiring in runReviewLoop (hk-2hb2y).
//
// The fix for hk-2hb2y (commit a7bcd49) wired implSpec.Substrate and revSpec.Substrate
// inside runReviewLoop so that SpawnWindow is called for both the implementer and
// reviewer phases. Without the fix, SpawnWindow was never called, leaving
// tmuxSubstrate.lastHandle empty, causing pasteInjectOnLaunch to fail.
//
// These tests verify the fix at the integration level using a spy substrate:
//
//  1. TestPasteInjectSubstrateWiring_ReviewLoopCallsSpawnWindowTwice
//     Verify that ≥2 SpawnWindow calls are made during a single-iteration APPROVE
//     cycle (one for implementer, one for reviewer).
//
//  2. TestPasteInjectSubstrateWiring_NilSubstrateFallback
//     Verify that a nil Substrate does not panic and the review loop succeeds using
//     the ordinary exec.CommandContext path (single-mode fallback).
//
// Helper prefix: pasteinjectFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-f31xv; sibling bead hk-zrj83 uses same prefix).

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// pasteinjectFixtureSpySubstrate — spy Substrate that counts SpawnWindow calls
// ─────────────────────────────────────────────────────────────────────────────

// pasteinjectFixtureSpySubstrate is a handler.Substrate spy that:
//   - Counts every SpawnWindow call.
//   - Executes the real command from SubstrateSpawn.Argv using exec.CommandContext
//     so the shell-script handler actually runs and produces its side effects.
//   - Exposes the command's stdout so handler.Launch wires a SpawnWatcher and
//     the review loop's normal session-completion path (exit code) is exercised.
type pasteinjectFixtureSpySubstrate struct {
	spawnCount atomic.Int64
}

// SpawnWindow increments the call counter, runs the real Argv as a subprocess
// (using exec.CommandContext via t.Context() captured at call time), and returns
// a session backed by the live *exec.Cmd.
func (s *pasteinjectFixtureSpySubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("pasteinjectFixtureSpySubstrate: SubstrateSpawn.Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pasteinjectFixtureSpySubstrate: StdoutPipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pasteinjectFixtureSpySubstrate: Start: %w", err)
	}
	return &pasteinjectFixtureExecSession{cmd: cmd, stdout: stdout}, nil
}

// spawnCalls returns the number of SpawnWindow calls observed so far.
func (s *pasteinjectFixtureSpySubstrate) spawnCalls() int {
	return int(s.spawnCount.Load())
}

// Compile-time assertion: pasteinjectFixtureSpySubstrate implements handler.Substrate.
var _ handler.Substrate = (*pasteinjectFixtureSpySubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// pasteinjectFixtureExecSession — SubstrateSession backed by a real *exec.Cmd
// ─────────────────────────────────────────────────────────────────────────────

// pasteinjectFixtureExecSession wraps an exec.Cmd as a handler.SubstrateSession.
// It exposes the real stdout pipe so handler.Launch wires a SpawnWatcher.
type pasteinjectFixtureExecSession struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdout io.Reader
}

func (s *pasteinjectFixtureExecSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *pasteinjectFixtureExecSession) Wait(_ context.Context) error {
	// Wait ignores exit-code errors so the review loop can proceed normally.
	_ = s.cmd.Wait()
	return nil
}

func (s *pasteinjectFixtureExecSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *pasteinjectFixtureExecSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns the stdout pipe so handler.Launch wires a SpawnWatcher.
func (s *pasteinjectFixtureExecSession) Stdout() io.Reader { return s.stdout }

// Compile-time assertion: pasteinjectFixtureExecSession implements handler.SubstrateSession.
var _ handler.SubstrateSession = (*pasteinjectFixtureExecSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures shared by both tests
// ─────────────────────────────────────────────────────────────────────────────

// pasteinjectFixtureProjectSetup creates a minimal project directory + git repo.
// Returns the project dir.
func pasteinjectFixtureProjectSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("pasteinjectFixtureProjectSetup: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("pasteinjectFixtureProjectSetup: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pasteinjectFixtureProjectSetup: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("pasteinject substrate wiring test\n"), 0o644); err != nil {
		t.Fatalf("pasteinjectFixtureProjectSetup: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// pasteinjectFixtureWorktree creates a detached git worktree and returns the
// worktree path and the parent commit SHA.
func pasteinjectFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("pasteinjectFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = string(out)
	if len(parentSHA) > 0 && parentSHA[len(parentSHA)-1] == '\n' {
		parentSHA = parentSHA[:len(parentSHA)-1]
	}

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("pasteinjectFixtureWorktree: git worktree add: %v\n%s", err, out)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("pasteinjectFixtureWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// pasteinjectFixtureRunID generates a fresh test RunID using UUIDv7.
func pasteinjectFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("pasteinjectFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// pasteinjectFixtureHandlerScript writes an APPROVE shell-script handler to a
// temp dir. The script commits a file (odd calls = implementer) or writes an
// APPROVE review.json (even calls = reviewer). This is the same convention used
// by rlBridgeFixtureHandlerScript in reviewloop_hkgql2015_test.go.
func pasteinjectFixtureHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := wtPath
	script := `#!/bin/sh
set -e
WTP='` + wtpEsc + `'
CNT_FILE="$WTP/.harmonik/pi_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  # Reviewer: write APPROVE verdict.
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"spy test"}' > "$WTP/.harmonik/review.json"
else
  # Implementer: commit a file.
  printf '%d' "$CNT" > "$WTP/pi_impl_$CNT.txt"
  git -C "$WTP" add "pi_impl_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "pi_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("pasteinjectFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectSubstrateWiring_ReviewLoopCallsSpawnWindowTwice verifies that
// when a non-nil Substrate is supplied in WorkLoopDepsParams, runReviewLoop
// calls SpawnWindow at least twice during a single-iteration APPROVE cycle:
// once for the implementer phase and once for the reviewer phase.
//
// This is the core regression test for hk-2hb2y: before the fix, implSpec.Substrate
// and revSpec.Substrate were never set, so SpawnWindow was never called, leaving
// tmuxSubstrate.lastHandle empty and causing pasteInjectOnLaunch to fail with
// "no window spawned yet".
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
func TestPasteInjectSubstrateWiring_ReviewLoopCallsSpawnWindowTwice(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := pasteinjectFixtureProjectSetup(t)
	wtPath, parentSHA := pasteinjectFixtureWorktree(t, projectDir)
	scriptPath := pasteinjectFixtureHandlerScript(t, wtPath)

	spy := &pasteinjectFixtureSpySubstrate{}
	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		Substrate:           spy,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		pasteinjectFixtureRunID(t),
		core.BeadID("pi-substrate-wiring-001"),
		wtPath, parentSHA,
	)

	if !result.Success {
		t.Fatalf("expected success=true on APPROVE cycle; summary=%q", result.Summary)
	}

	got := spy.spawnCalls()
	if got < 2 {
		t.Errorf("SpawnWindow call count = %d; want ≥2 (implementer + reviewer)", got)
	}
}

// TestPasteInjectSubstrateWiring_NilSubstrateFallback verifies that a nil Substrate
// does not panic and the review loop completes successfully using the ordinary
// exec.CommandContext path. This is the MVH single-mode path (no tmux required).
//
// Regression: if reviewloop.go guards are missing, a nil substrate passed to
// handler.Launch would panic. This test confirms the nil-safe fallback works.
func TestPasteInjectSubstrateWiring_NilSubstrateFallback(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := pasteinjectFixtureProjectSetup(t)
	wtPath, parentSHA := pasteinjectFixtureWorktree(t, projectDir)
	scriptPath := pasteinjectFixtureHandlerScript(t, wtPath)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	// Substrate is intentionally left nil — WorkLoopDepsParams zero value.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// Substrate: nil (zero value) — exec.CommandContext path
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Must not panic; must succeed via the exec.CommandContext path.
	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		pasteinjectFixtureRunID(t),
		core.BeadID("pi-nil-substrate-001"),
		wtPath, parentSHA,
	)

	if !result.Success {
		t.Fatalf("nil-substrate fallback: expected success=true; summary=%q", result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("completion_reason = %q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
	}
}
