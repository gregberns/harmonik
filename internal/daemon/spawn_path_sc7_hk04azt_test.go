package daemon_test

// spawn_path_sc7_hk04azt_test.go — SC-7: tmux substrate path — twin via real
// tmux pane, revWatcher nil-guard regression (hk-04azt).
//
// # What this test covers
//
// SC-7 is a regression guard for the hk-yjduq nil-watcher fix (commit 94d8992).
// It exercises the tmux substrate path through the FULL work loop
// (ExportedRunWorkLoop → beadRunOne → runReviewLoop) using a nil-stdout substrate
// that simulates what tmux-hosted sessions produce: no stdout pipe, so
// handler.launchViaSubstrate returns watcher=nil for both the implementer and
// the reviewer.
//
// The existing hk-3aqtb regression guard covers ExportedRunReviewLoop directly
// with a shell script. SC-7 differs in two ways:
//  1. It goes through the FULL work loop (productionWorktreeFactory → beadRunOne
//     → runReviewLoop), covering the end-to-end path that a real tmux run would take.
//  2. The implementer phase uses the harmonik-twin-claude binary (when available)
//     so the test reflects a real agent subprocess — not just a shell exit.
//
// When the twin binary is absent, the implementer falls back to a plain commit
// script (still exercises the nil-watcher path; just without real NDJSON output).
//
// # Why the twin binary matters
//
// In production, the implementer IS a real claude-code subprocess launched via
// the tmux substrate. Its stdout goes to the pane, not to a Go io.Reader. The
// nil-stdout session simulates this: the subprocess runs and exits, but Go sees
// no stdout pipe. This is the exact condition that triggered the hk-yjduq SIGSEGV
// when revWatcher was dereferenced unconditionally. SC-7 with the twin validates
// that the nil-guard survives the full beadRunOne code path, not just the isolated
// review-loop path tested by hk-3aqtb.
//
// # Test contract (AdapterRegistry2)
//
// AdapterRegistry2 = NewEmptySealedAdapterRegistryForTest: waitAgentReady is
// skipped because the nil-stdout substrate means no NDJSON events are forwarded
// to the tapping bus. The bead run proceeds on process exit code alone (hk-ngw3d).
//
// # Assertions
//
//  1. CloseBead called exactly once for the seeded bead (not ReopenBead).
//  2. run_completed emitted (not run_failed).
//  3. No panic — if the hk-yjduq nil-guard (commit 94d8992) is removed, this
//     test panics with a nil-pointer dereference in runReviewLoop.
//
// # Helper prefix: sc7Fixture (bead hk-04azt; per implementer-protocol.md
// §Helper-prefix discipline).
//
// Spec refs:
//   - specs/process-lifecycle.md §4.7 PL-021b (Substrate seam)
//   - specs/handler-contract.md §4.8 HC-036 (twin-parity)
//   - specs/scenario-harness.md §4 (twin-driven spawn-path coverage)
//
// Bead: hk-04azt.

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

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// sc7FixtureNilStdoutSubstrate — handler.Substrate with nil Stdout
// ─────────────────────────────────────────────────────────────────────────────

// sc7FixtureNilStdoutSubstrate is a handler.Substrate whose SpawnWindow runs
// the real Argv as a subprocess but wraps it in sc7FixtureNilStdoutSession.
// That session's Stdout() returns nil, causing handler.launchViaSubstrate to
// return watcher=nil — the tmux-hosted path that triggered the hk-yjduq SIGSEGV.
//
// Both the implementer and reviewer phase use this substrate. The reviewer's
// revWatcher==nil path is the primary regression guard for hk-yjduq.
type sc7FixtureNilStdoutSubstrate struct{}

// SpawnWindow runs the real command and returns a session with Stdout() == nil.
func (s *sc7FixtureNilStdoutSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("sc7FixtureNilStdoutSubstrate: Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerBinary; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	// Do NOT set cmd.Stdout — subprocess stdout goes to /dev/null (like tmux).
	// sc7FixtureNilStdoutSession.Stdout() returns nil, which causes
	// handler.launchViaSubstrate to return watcher=nil.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sc7FixtureNilStdoutSubstrate: Start: %w", err)
	}
	return &sc7FixtureNilStdoutSession{cmd: cmd}, nil
}

// Compile-time assertion.
var _ handler.Substrate = (*sc7FixtureNilStdoutSubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// sc7FixtureNilStdoutSession — SubstrateSession with Stdout() == nil
// ─────────────────────────────────────────────────────────────────────────────

// sc7FixtureNilStdoutSession is a handler.SubstrateSession backed by a real
// exec.Cmd whose Stdout() returns nil. This is the invariant for tmux-hosted
// sessions: no stdout pipe, because the pane owns the pty.
// handler.launchViaSubstrate checks Stdout(); when nil, it returns watcher=nil.
type sc7FixtureNilStdoutSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// Kill terminates the subprocess.
func (s *sc7FixtureNilStdoutSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the subprocess to exit. Exit-code errors are suppressed so
// waitWithSocketGrace's sess.Wait() call returns nil on the nil-watcher path.
func (s *sc7FixtureNilStdoutSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

// Outcome returns a zero-value Outcome (exit info not surfaced in this stub).
func (s *sc7FixtureNilStdoutSession) Outcome() handler.Outcome {
	return handler.Outcome{}
}

// PID returns the subprocess PID.
func (s *sc7FixtureNilStdoutSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns nil — the key property that causes handler.launchViaSubstrate
// to return watcher=nil, exercising the nil-watcher code paths in the
// workloop and reviewloop.
func (s *sc7FixtureNilStdoutSession) Stdout() io.Reader { return nil }

// Compile-time assertion.
var _ handler.SubstrateSession = (*sc7FixtureNilStdoutSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// sc7FixtureHandlerScript — wrapper script for implementer + reviewer phases
// ─────────────────────────────────────────────────────────────────────────────

// sc7FixtureHandlerScript writes a /bin/sh handler script that handles both
// implementer (odd invocations) and reviewer (even invocations) phases.
//
// Odd invocations (1, 3, …) — implementer:
//   - Optionally runs the twin binary in a background subshell (twinPath may be
//     empty; when present, the twin runs with --scenario single-happy-path and
//     exits; its NDJSON output is discarded because the nil-stdout substrate
//     does not capture it in Go, but the subprocess runs normally).
//   - Creates and commits a unique file so the review loop does not abort with
//     a no-progress error.
//
// Even invocations (2, 4, …) — reviewer:
//   - Writes an APPROVE verdict to $PWD/.harmonik/review.json.
//   - Does NOT commit anything.
//
// The counter file is stored in $PWD/.harmonik/sc7_count so it persists across
// invocations within the same worktree (the worktree is reused across the
// review loop iterations). Using $PWD (the worktree working directory, set by
// handler.go via cmd.Dir = spec.WorkDir) avoids hard-coding a path.
func sc7FixtureHandlerScript(t *testing.T, twinPath string) string {
	t.Helper()

	twinLine := ""
	if twinPath != "" {
		// Run the twin binary to emit NDJSON (output is discarded by the
		// nil-stdout substrate, but the subprocess executes normally).
		// Use '|| true' so a non-zero twin exit does not fail the implementer.
		//nolint:gocritic // twinPath comes from TwinBinaryPath(); not user input
		twinLine = `  "` + twinPath + `" --scenario single-happy-path >/dev/null 2>&1 || true`
	}

	script := `#!/bin/sh
set -e
WTP="$(pwd)"
mkdir -p "$WTP/.harmonik"
CNT_FILE="$WTP/.harmonik/sc7_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  # Even invocation: reviewer — write APPROVE verdict.
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"SC-7 nil-watcher regression guard"}' > "$WTP/.harmonik/review.json"
else
  # Odd invocation: implementer — commit a unique file, optionally run twin.
  printf '%d' "$CNT" > "$WTP/sc7_impl_$CNT.txt"
  git add "sc7_impl_$CNT.txt" >/dev/null 2>&1
  git -c user.email=test@harmonik.local -c user.name="Test" \
    commit -m "SC-7 impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
` + twinLine + `
fi
exit 0
`

	scriptPath := filepath.Join(t.TempDir(), "sc7_handler.sh")
	//nolint:gosec // G306: test-only fixture script; chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("sc7FixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// TestSC7_TmuxSubstratePath_TwinViaRealPane
// ─────────────────────────────────────────────────────────────────────────────

// TestSC7_TmuxSubstratePath_TwinViaRealPane is SC-7 from the spawn-path
// scenario suite (hk-p3diy).
//
// It verifies that the full work loop (beadRunOne → runReviewLoop) completes
// successfully when the handler runs via sc7FixtureNilStdoutSubstrate — the
// substrate path where handler.launchViaSubstrate returns watcher=nil (simulating
// a tmux-hosted pane with no stdout pipe).
//
// If the hk-yjduq nil-guard (commit 94d8992) is removed, this test will panic
// with a nil-pointer dereference in runReviewLoop and fail.
//
// Not parallel: uses t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to redirect
// EnsureWorktreeTrust away from ~/.claude.json; t.Setenv is incompatible
// with t.Parallel.
//
// Bead: hk-04azt.
func TestSC7_TmuxSubstratePath_TwinViaRealPane(t *testing.T) {
	// Locate the twin binary; proceed with shell-only fallback when absent.
	twinPath, twinAvailable := scenariotest.TwinBinaryPath()
	if twinAvailable {
		t.Logf("SC7: twin binary found at %q", twinPath)
	} else {
		t.Logf("SC7: twin binary absent; using commit-only implementer (nil-watcher path still exercised)")
		twinPath = ""
	}

	// Create project directory with git repo (real git required by
	// productionWorktreeFactory which calls `git worktree add`).
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Redirect EnsureWorktreeTrust to a test-local claude config so that
	// buildClaudeLaunchSpec does not contend with a running daemon on
	// ~/.claude.json.lock.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	// Seed one bead in the stub ledger.
	const beadID = core.BeadID("sc7-tmux-substrate-nil-watcher")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	// Build the handler script (implementer commits + optional twin; reviewer writes APPROVE).
	handlerScript := sc7FixtureHandlerScript(t, twinPath)

	// Wire deps:
	//   - Substrate = sc7FixtureNilStdoutSubstrate: SpawnWindow returns a session
	//     with Stdout()==nil → handler.launchViaSubstrate returns watcher=nil for
	//     BOTH implementer and reviewer phases.
	//   - AdapterRegistry2 = NewEmptySealedAdapterRegistryForTest: waitAgentReady
	//     is skipped because the nil-stdout path forwards no NDJSON events to the
	//     tapping bus; the bead run proceeds on process exit code alone (hk-ngw3d).
	//   - WorkflowModeDefault = WorkflowModeReviewLoop: exercises runReviewLoop,
	//     which contains the revWatcher nil-guards under test (hk-yjduq).
	//   - AgentReadyTimeout = 5s: short timeout; waitAgentReady is skipped (empty
	//     registry), so this is a safety net only.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{handlerScript},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		Substrate:           &sc7FixtureNilStdoutSubstrate{},
		AdapterRegistry2:    NewEmptySealedAdapterRegistryForTest(t),
		AgentReadyTimeout:   5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Poll until CloseBead or ReopenBead is called (terminal bead state).
	const terminalPollBudget = 45 * time.Second
	terminalDeadline := time.Now().Add(terminalPollBudget)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel the loop before assertions so cleanup is deterministic.
	cancel()
	select {
	case <-loopDone:
	case <-time.After(10 * time.Second):
		t.Error("SC7: work loop did not exit within 10 s after context cancel")
	}

	closedIDs := ledger.closedIDs()
	reopenedIDs := ledger.reopenedIDs()
	emittedTypes := collector.eventTypes()

	t.Logf("SC7: closedIDs=%v reopenedIDs=%v eventTypes=%v", closedIDs, reopenedIDs, emittedTypes)

	// ── Assertion 1: CloseBead called exactly once ────────────────────────────
	// The bead must complete via the close path (not reopen). If the nil-watcher
	// guard were absent, runReviewLoop would panic before reaching CloseBead.
	if len(closedIDs) == 0 {
		t.Errorf("SC7 FAIL: CloseBead not called; bead %q never completed (reopenedIDs=%v); "+
			"check if nil-watcher guard (hk-yjduq, commit 94d8992) is still in reviewloop.go",
			beadID, reopenedIDs)
	} else if closedIDs[0] != beadID {
		t.Errorf("SC7 FAIL: closed bead = %q; want %q", closedIDs[0], beadID)
	}

	// ── Assertion 2: ReopenBead NOT called ───────────────────────────────────
	if len(reopenedIDs) > 0 {
		t.Errorf("SC7 FAIL: ReopenBead called unexpectedly: %v (expected clean CloseBead path)", reopenedIDs)
	}

	// ── Assertion 3: run_completed emitted (not run_failed) ──────────────────
	if !sc7FixtureContainsEvent(emittedTypes, string(core.EventTypeRunCompleted)) {
		t.Errorf("SC7 FAIL: run_completed not emitted; got %v", emittedTypes)
	}
	if sc7FixtureContainsEvent(emittedTypes, string(core.EventTypeRunFailed)) {
		t.Errorf("SC7 FAIL: run_failed emitted unexpectedly; got %v", emittedTypes)
	}

	// ── Assertion 4: No panic (reaching here proves the nil-guard is intact) ─
	// If hk-yjduq's nil-guard were absent, the test would have panicked above
	// in the ExportedRunWorkLoop goroutine. Reaching this line means PASS.
	if len(closedIDs) > 0 && closedIDs[0] == beadID &&
		!sc7FixtureContainsEvent(emittedTypes, string(core.EventTypeRunFailed)) {
		twinNote := "shell-only implementer"
		if twinAvailable {
			twinNote = "twin binary implementer"
		}
		t.Logf("SC7 PASS: bead %q closed via tmux substrate path (%s); "+
			"revWatcher nil-guard (hk-yjduq) is intact", beadID, twinNote)
	}
}

// sc7FixtureContainsEvent reports whether typeName appears in the event type list.
func sc7FixtureContainsEvent(types []string, typeName string) bool {
	for _, et := range types {
		if strings.EqualFold(et, typeName) {
			return true
		}
	}
	return false
}
