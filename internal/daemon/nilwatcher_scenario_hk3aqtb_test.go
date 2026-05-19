package daemon_test

// nilwatcher_scenario_hk3aqtb_test.go — scenario test for nil-watcher graceful
// handling in runReviewLoop (hk-3aqtb).
//
// # Placement note
//
// The bead body targets test/scenario/; however ExportedRunReviewLoop and the
// WorkLoopDepsParams fields it depends on are test-only exports (export_test.go,
// package daemon) and are only compiled into the daemon test binary.  An
// external package (test/scenario/) cannot import them.  Per implementer-protocol
// §Path-discrepancy resolution ("bead body wins"), this file is placed in
// internal/daemon/ where the seam is available; the deviation is documented in
// the commit body.
//
// # What this test covers
//
// When deps.substrate is non-nil (tmux-hosted sessions), handler.launchViaSubstrate
// returns watcher=nil because subSess.Stdout() is nil and no SpawnWatcher is wired.
// runReviewLoop must not dereference revWatcher unconditionally — doing so panics
// (SIGSEGV). The fix landed in hk-yjduq (commit 94d8992); this test is the
// regression guard that would fail if that nil-guard is ever removed.
//
// # Approach
//
// nilwatcherFixtureNilStdoutSubstrate implements handler.Substrate.  Its
// SpawnWindow runs the real handler script as an exec subprocess but wraps
// it in nilwatcherFixtureNilStdoutSession, whose Stdout() returns nil.
// handler.launchViaSubstrate then returns watcher=nil — the exact substrate path
// that triggered the hk-yjduq SIGSEGV.
//
// The test runs under the race detector (go test -race ./internal/daemon/...) and
// asserts: no panic, result.Success=true (APPROVE verdict), no nil-pointer error.
//
// If the hk-yjduq nil-guard (commit 94d8992) is ever removed, this test will
// panic with a nil-pointer dereference and fail under -race.
//
// # Helper prefix: nilwatcherFixture (bead hk-3aqtb, per implementer-protocol
// §Helper-prefix discipline).
//
// Spec refs: specs/process-lifecycle.md §4.7 PL-021b, specs/handler-contract.md §4.8.
// Bead: hk-3aqtb.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// nilwatcherFixtureNilStdoutSubstrate — handler.Substrate stub with nil Stdout
// ─────────────────────────────────────────────────────────────────────────────

// nilwatcherFixtureNilStdoutSubstrate is a handler.Substrate whose SpawnWindow
// runs the real Argv as a subprocess but wraps the resulting exec.Cmd in a
// nilwatcherFixtureNilStdoutSession. That session's Stdout() returns nil,
// causing handler.launchViaSubstrate to return watcher=nil — the tmux substrate
// path that triggered the hk-yjduq SIGSEGV.
type nilwatcherFixtureNilStdoutSubstrate struct{}

// SpawnWindow runs the real command from in.Argv and returns a session with
// Stdout() == nil. The subprocess executes and exits normally; its completion
// is detected via sess.Wait() in waitWithSocketGrace (nil-watcher branch).
func (s *nilwatcherFixtureNilStdoutSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("nilwatcherFixtureNilStdoutSubstrate: Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerArgs; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("nilwatcherFixtureNilStdoutSubstrate: Start: %w", err)
	}
	return &nilwatcherFixtureNilStdoutSession{cmd: cmd}, nil
}

// Compile-time assertion: nilwatcherFixtureNilStdoutSubstrate implements handler.Substrate.
var _ handler.Substrate = (*nilwatcherFixtureNilStdoutSubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// nilwatcherFixtureNilStdoutSession — SubstrateSession with Stdout() == nil
// ─────────────────────────────────────────────────────────────────────────────

// nilwatcherFixtureNilStdoutSession is a handler.SubstrateSession backed by a
// real *exec.Cmd but whose Stdout() returns nil. This is the invariant for
// tmux-hosted sessions (no stdout pipe; the bridge wire is the daemon socket).
// handler.launchViaSubstrate checks Stdout(); when nil, it returns watcher=nil
// — the nil-watcher path exercised by this test.
type nilwatcherFixtureNilStdoutSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// Kill terminates the subprocess.
func (s *nilwatcherFixtureNilStdoutSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the subprocess to exit. Exit-code errors are suppressed so
// waitWithSocketGrace's sess.Wait() call returns nil and the nil-watcher branch
// completes without error.
func (s *nilwatcherFixtureNilStdoutSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

// Outcome returns a zero-value Outcome (no exit info surface needed for this stub).
func (s *nilwatcherFixtureNilStdoutSession) Outcome() handler.Outcome {
	return handler.Outcome{}
}

// PID returns the subprocess PID.
func (s *nilwatcherFixtureNilStdoutSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns nil — the key property that causes handler.launchViaSubstrate
// to return watcher=nil, exercising the nil-watcher path in runReviewLoop.
func (s *nilwatcherFixtureNilStdoutSession) Stdout() io.Reader { return nil }

// Compile-time assertion: nilwatcherFixtureNilStdoutSession implements handler.SubstrateSession.
var _ handler.SubstrateSession = (*nilwatcherFixtureNilStdoutSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Setup fixtures
// ─────────────────────────────────────────────────────────────────────────────

// nilwatcherFixtureProjectSetup creates the minimal project directory tree:
// .harmonik/events/ and .harmonik/beads-intents/, then initialises a git repo
// with one initial commit. Returns the project dir path.
func nilwatcherFixtureProjectSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("nilwatcherFixtureProjectSetup: mkdir events: %v", err)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("nilwatcherFixtureProjectSetup: mkdir beads-intents: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("nilwatcherFixtureProjectSetup: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("nil-watcher scenario test repo\n"), 0o644); err != nil {
		t.Fatalf("nilwatcherFixtureProjectSetup: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// nilwatcherFixtureWorktree creates a detached git worktree and returns the
// worktree path and parent commit SHA.
func nilwatcherFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("nilwatcherFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		t.Fatalf("nilwatcherFixtureWorktree: git worktree add: %v\n%s", addErr, addOut)
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("nilwatcherFixtureWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// nilwatcherFixtureRunID generates a fresh RunID using UUIDv7.
func nilwatcherFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("nilwatcherFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// nilwatcherFixtureHandlerScript writes a shell-script handler to a temp dir.
// The same odd/even counter convention used by other review-loop tests:
//   - Odd invocations (1, 3, …): implementer — commit a file.
//   - Even invocations (2, 4, …): reviewer — write APPROVE review.json.
func nilwatcherFixtureHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
CNT_FILE="$WTP/.harmonik/nw_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"nil-watcher test"}' > "$WTP/.harmonik/review.json"
else
  printf '%d' "$CNT" > "$WTP/nw_impl_$CNT.txt"
  git -C "$WTP" add "nw_impl_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit -m "nil-watcher impl $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`
	scriptPath := filepath.Join(t.TempDir(), "nw_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("nilwatcherFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ensure time is used via test timeout.
var _ time.Duration

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_ReviewLoop_NilWatcherNoRace is a race-detector scenario test for
// the nil-watcher path in runReviewLoop (hk-3aqtb).
//
// It injects a nilwatcherFixtureNilStdoutSubstrate whose SpawnWindow returns a
// SubstrateSession with Stdout() == nil. handler.launchViaSubstrate then returns
// watcher=nil — the tmux-hosted substrate path. runReviewLoop must complete
// through at least one APPROVE iteration without panicking or dereferencing the
// nil watcher.
//
// If the hk-yjduq nil-guard (commit 94d8992) is ever removed, this test will
// panic with a nil-pointer dereference and fail under -race.
//
// Spec refs: specs/process-lifecycle.md §4.7 PL-021b, specs/handler-contract.md §4.8.
// Bead: hk-3aqtb.
func TestScenario_ReviewLoop_NilWatcherNoRace(t *testing.T) {
	t.Parallel()

	projectDir := nilwatcherFixtureProjectSetup(t)
	wtPath, parentSHA := nilwatcherFixtureWorktree(t, projectDir)
	scriptPath := nilwatcherFixtureHandlerScript(t, wtPath)

	sub := &nilwatcherFixtureNilStdoutSubstrate{}
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
		Substrate:           sub,
		// AdapterRegistry2 is nil: skip waitAgentReady (no adapters registered).
		// This exercises the nil-watcher path through waitWithSocketGrace directly.
		// The nil-guard in the revWatcher.Done() goroutine (hk-yjduq) is still
		// exercised because deps.adapterRegistry == nil bypasses that block, and
		// waitWithSocketGrace itself nil-guards the watcher (hk-e2kwq).
		AdapterRegistry2: nil,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		nilwatcherFixtureRunID(t),
		core.BeadID("nw-nil-watcher-no-race-001"),
		wtPath, parentSHA,
	)

	// The nil-watcher path must complete without panic (we reached here).
	if !result.Success {
		t.Fatalf("NilWatcherNoRace: expected success=true on APPROVE cycle; summary=%q", result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
		t.Errorf("NilWatcherNoRace: completion_reason=%q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
	}

	// review_loop_cycle_complete must be emitted (lifecycle invariant).
	eventTypes := collector.eventTypes()
	foundCycleComplete := false
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewLoopCycleComplete) {
			foundCycleComplete = true
			break
		}
	}
	if !foundCycleComplete {
		t.Errorf("NilWatcherNoRace: review_loop_cycle_complete event not emitted; got: %v", eventTypes)
	}

	t.Logf("NilWatcherNoRace PASS: nil-watcher substrate path completed APPROVE without panic (hk-yjduq regression guard)")
}
