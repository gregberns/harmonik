package daemon_test

// reviewloop_substrate_hkt5j2w_test.go — scenario test: review-loop tmux substrate
// wired (hk-t5j2w).
//
// # Placement note
//
// The bead body targets test/scenario/; however ExportedRunReviewLoop and the
// WorkLoopDepsParams fields it depends on are test-only exports (export_test.go,
// package daemon) and are only compiled into the daemon test binary.  An
// external package (test/scenario/) cannot import them.  Per implementer-protocol
// §Path-discrepancy resolution ("bead body wins"), this file is placed in
// internal/daemon/ where the seam is available; the deviation is documented here.
//
// # What this test covers
//
// TestScenario_ReviewLoop_SubstrateWired asserts that when deps.substrate is
// non-nil, runReviewLoop calls Substrate.SpawnWindow for BOTH the implementer
// and reviewer phases — catching the hk-2hb2y root-cause: implSpec.Substrate /
// revSpec.Substrate were unwired so SpawnWindow was never called, leaving
// tmuxSubstrate.lastHandle empty, causing pasteInjectOnLaunch to crash with
// "no window spawned yet".
//
// The spy substrate runs the real handler script (exec.CommandContext) so all
// side-effects (git commit, review.json write) occur normally.  No real tmux.
//
// Helper prefix: rlSubWired (bead hk-t5j2w, per implementer-protocol §Helper-prefix
// discipline).
//
// Spec refs: specs/process-lifecycle.md §4.7 PL-021b; handler-contract.md §4.
// Bead: hk-t5j2w.

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
// rlSubWiredSpySubstrate — spy Substrate that counts SpawnWindow calls
// ─────────────────────────────────────────────────────────────────────────────

// rlSubWiredSpySubstrate is a handler.Substrate spy that:
//   - Counts every SpawnWindow call via an atomic counter.
//   - Executes the real Argv from SubstrateSpawn using exec.CommandContext so
//     the shell-script handler actually runs (git commits, review.json writes).
//   - Exposes the real stdout pipe so handler.Launch wires a SpawnWatcher and
//     the review-loop completion path (watcher.Done + sess.Wait) is exercised.
//
// No real tmux required: the spy is a pure in-process counter + exec wrapper.
type rlSubWiredSpySubstrate struct {
	spawnCount atomic.Int64
}

// SpawnWindow increments the counter, starts the Argv as a subprocess, and
// returns a session backed by the live *exec.Cmd.
func (s *rlSubWiredSpySubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCount.Add(1)
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("rlSubWiredSpySubstrate: SubstrateSpawn.Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("rlSubWiredSpySubstrate: StdoutPipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rlSubWiredSpySubstrate: Start: %w", err)
	}
	return &rlSubWiredExecSession{cmd: cmd, stdout: stdout}, nil
}

// spawnCalls returns the number of SpawnWindow calls recorded so far.
func (s *rlSubWiredSpySubstrate) spawnCalls() int {
	return int(s.spawnCount.Load())
}

// Compile-time assertion: rlSubWiredSpySubstrate implements handler.Substrate.
var _ handler.Substrate = (*rlSubWiredSpySubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// rlSubWiredExecSession — SubstrateSession backed by a real *exec.Cmd
// ─────────────────────────────────────────────────────────────────────────────

// rlSubWiredExecSession wraps an exec.Cmd as a handler.SubstrateSession.
// Stdout() returns the real pipe so handler.Launch wires a SpawnWatcher.
type rlSubWiredExecSession struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdout io.Reader
}

func (s *rlSubWiredExecSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *rlSubWiredExecSession) Wait(_ context.Context) error {
	// Ignore exit-code errors so the review loop can proceed normally.
	_ = s.cmd.Wait()
	return nil
}

func (s *rlSubWiredExecSession) Outcome() handler.Outcome { return handler.Outcome{} }

func (s *rlSubWiredExecSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns the stdout pipe so handler.Launch wires a SpawnWatcher.
func (s *rlSubWiredExecSession) Stdout() io.Reader { return s.stdout }

// Compile-time assertion: rlSubWiredExecSession implements handler.SubstrateSession.
var _ handler.SubstrateSession = (*rlSubWiredExecSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rlSubWiredProjectDir creates the minimal project directory for the substrate-
// wiring scenario: .harmonik/events/ and .harmonik/beads-intents/.
func rlSubWiredProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlSubWiredProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlSubWiredProjectDir: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rlSubWiredProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("review-loop substrate wiring scenario\n"), 0o644); err != nil {
		t.Fatalf("rlSubWiredProjectDir: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// rlSubWiredWorktree creates a detached git worktree under projectDir and
// registers cleanup. Returns the worktree path and the parent commit SHA.
func rlSubWiredWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlSubWiredWorktree: git rev-parse HEAD: %v", err)
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
		t.Fatalf("rlSubWiredWorktree: git worktree add: %v\n%s", err, out)
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlSubWiredWorktree: mkdir .harmonik: %v", err)
	}

	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})

	return wtPath, parentSHA
}

// rlSubWiredRunID generates a fresh RunID using UUIDv7.
func rlSubWiredRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlSubWiredRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlSubWiredHandlerScript writes a /bin/sh handler script that:
//   - On odd invocations (implementer): commits a unique file so the diff hash
//     advances (avoids false no-progress detection per EM-015e).
//   - On even invocations (reviewer): writes an APPROVE review.json and exits.
func rlSubWiredHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := wtPath
	script := `#!/bin/sh
set -e
WTP='` + wtpEsc + `'
CNT_FILE="$WTP/.harmonik/rlsubwired_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  # Reviewer: write APPROVE verdict.
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"substrate wired scenario"}' > "$WTP/.harmonik/review.json"
else
  # Implementer: commit a unique file so diff hash advances.
  printf '%d' "$CNT" > "$WTP/subwired_impl_$CNT.txt"
  git -C "$WTP" add "subwired_impl_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "subwired impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "rlsubwired_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlSubWiredHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_SubstrateWired verifies that when deps.substrate is
// non-nil, runReviewLoop calls Substrate.SpawnWindow for BOTH the implementer
// and reviewer phases of a single-iteration APPROVE cycle.
//
// This is the scenario-level regression test for hk-2hb2y: before that fix,
// implSpec.Substrate and revSpec.Substrate were never assigned inside
// runReviewLoop, so SpawnWindow was never called, leaving tmuxSubstrate.lastHandle
// empty, causing pasteInjectOnLaunch to crash with "no window spawned yet".
//
// The spy substrate runs the real handler binary (exec.CommandContext) so all
// filesystem side-effects (git commits, review.json writes) occur normally.
// No real tmux is required.
//
// Assertions:
//  1. result.Success = true (APPROVE verdict received).
//  2. SpawnWindow called ≥ 2 times (once for implementer, once for reviewer).
//  3. reviewer_launched and review_loop_cycle_complete events emitted.
//
// Spec refs: specs/process-lifecycle.md §4.7 PL-021b; handler-contract.md §4.
// Bead: hk-t5j2w.
func TestScenario_ReviewLoop_SubstrateWired(t *testing.T) {
	t.Parallel()

	projectDir := rlSubWiredProjectDir(t)
	wtPath, parentSHA := rlSubWiredWorktree(t, projectDir)
	scriptPath := rlSubWiredHandlerScript(t, wtPath)

	spy := &rlSubWiredSpySubstrate{}
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
		Substrate:           spy,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlSubWiredRunID(t),
		core.BeadID("scenario-rl-substrate-wired-001"),
		wtPath, parentSHA,
	)

	// Assertion 1: APPROVE cycle must succeed.
	if !result.Success {
		t.Errorf("expected success=true on APPROVE cycle; summary=%q", result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("completion_reason = %q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
	}

	// Assertion 2: SpawnWindow must be called at least twice — once for the
	// implementer phase and once for the reviewer phase.
	// This is the core regression guard for hk-2hb2y: implSpec.Substrate and
	// revSpec.Substrate must both be set before h.Launch is called.
	gotSpawns := spy.spawnCalls()
	if gotSpawns < 2 {
		t.Errorf("SpawnWindow call count = %d; want ≥2 (implementer + reviewer); "+
			"implSpec.Substrate or revSpec.Substrate may be unwired (hk-2hb2y)",
			gotSpawns)
	}

	// Assertion 3: reviewer_launched and review_loop_cycle_complete must be emitted.
	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewerLaunched))
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewLoopCycleComplete))

	t.Logf("TestScenario_ReviewLoop_SubstrateWired PASS: SpawnWindow called %d times; "+
		"APPROVE verdict received", gotSpawns)
}
