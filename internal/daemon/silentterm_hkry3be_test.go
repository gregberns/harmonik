package daemon_test

// silentterm_hkry3be_test.go — tests for silent agent-termination detection
// (hk-ry3be).
//
// Scenario tested:
//
//  1. External tmux kill-window with process still alive in OS table:
//     The tmux pane disappears (WindowPanePID returns error) but the shell
//     PID is still visible via kill(pid, 0) (processDead returns false).
//     Before the hk-ry3be fix, Wait blocked indefinitely.  After the fix,
//     Wait must return within a short deadline.
//
//  2. Successful agent_completed path: a real child process exits normally →
//     processDead returns true → Wait returns without consulting tmux.
//
// Helper-prefix: silentTermFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ry3be).

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// silentTermFixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// silentTermFixtureNewSubstrate constructs a handler.Substrate backed by the
// given adapter for the "test-session" session name.
func silentTermFixtureNewSubstrate(t *testing.T, adapter tmux.Adapter) handler.Substrate {
	t.Helper()
	return daemon.NewTmuxSubstrate(adapter, "test-session")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSilentTerm_WaitUnblocksWhenPaneGoneButPIDAlive is the core regression test
// for hk-ry3be.
//
// Setup: spawn a session where the PID is the running test process (so
// processDead always returns false while the test is executing), but
// WindowPanePID returns an error (pane is gone from tmux's view).
//
// Expected: Wait returns within 3 s (two 500ms poll ticks with margin).
//
// Without the hk-ry3be fix, this would block indefinitely because the
// pid>0 fast path checked only processDead and never consulted tmux.
func TestSilentTerm_WaitUnblocksWhenPaneGoneButPIDAlive(t *testing.T) {
	t.Parallel()

	// Use the test process PID — guaranteed alive for the duration of this test.
	livePID := os.Getpid()

	// Adapter: WindowPanePID always fails (pane gone), but NewWindowIn
	// returns the live PID via panePIDResult so SpawnWindow populates sess.pid.
	//
	// We need to intercept NewWindowIn to return livePID from WindowPanePID;
	// the panePIDResult in the pane-gone adapter is unused by SpawnWindow's
	// WindowPanePID call because SpawnWindow calls WindowPanePID(ctx, handle),
	// which uses the same error path we set for runWait.  To inject the live PID
	// directly, we use a specialised adapter where NewWindowIn sets a paneID of
	// "%" + livePID (fake) but the real PID is returned by WindowPanePID on the
	// FIRST call only (SpawnWindow's PID lookup), and subsequent calls fail
	// (runWait's secondary check).
	adapter := &silentTermFixturePidFirstThenGone{
		spawnPID: livePID,
	}
	substrate := silentTermFixtureNewSubstrate(t, adapter)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win-silent",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	}
	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// PID should match the live PID injected.
	if sess.PID() != livePID {
		t.Fatalf("PID: got %d, want %d (test process PID)", sess.PID(), livePID)
	}

	// Wait must unblock within 3 s despite the PID being alive.
	waitCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Errorf("Wait returned unexpected error: %v (want nil — pane-gone path should unblock cleanly)", err)
	}
}

// silentTermFixturePidFirstThenGone is a tmux.Adapter variant used by
// TestSilentTerm_WaitUnblocksWhenPaneGoneButPIDAlive.
//
// WindowPanePID returns spawnPID on the first call (so SpawnWindow populates
// sess.pid with a live process PID), then returns an error on all subsequent
// calls (simulating the pane being killed externally after spawn).
//
// This dual-behaviour is necessary because both SpawnWindow and runWait call
// WindowPanePID: SpawnWindow needs a live PID, runWait needs to observe the
// pane-gone error.
type silentTermFixturePidFirstThenGone struct {
	spawnPID  int
	callCount atomic.Int64
}

func (a *silentTermFixturePidFirstThenGone) ProbeTmux(_ context.Context) error { return nil }
func (a *silentTermFixturePidFirstThenGone) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *silentTermFixturePidFirstThenGone) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *silentTermFixturePidFirstThenGone) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win-silent")}
}

func (a *silentTermFixturePidFirstThenGone) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	n := a.callCount.Add(1)
	if n == 1 {
		// First call: SpawnWindow PID lookup — return the live PID.
		return a.spawnPID, nil
	}
	// Subsequent calls: runWait secondary check — pane is gone.
	return 0, errors.New("tmux: no such window (simulated kill-window)")
}

func (a *silentTermFixturePidFirstThenGone) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "%99", nil
}

func (a *silentTermFixturePidFirstThenGone) KillSession(_ context.Context, _ string) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) SendKeysEnter(_ context.Context, _ string) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) SendKeysQuit(_ context.Context, _ string) error {
	return nil
}

func (a *silentTermFixturePidFirstThenGone) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// Compile-time assertion.
var _ tmux.Adapter = (*silentTermFixturePidFirstThenGone)(nil)

// TestSilentTerm_SuccessfulAgentCompletedPathUnaffected verifies that the
// normal successful-exit path — where the OS process actually exits — still
// works correctly after the hk-ry3be fix.
//
// Setup: a real child process (sleep 1) is spawned and its PID is injected
// via a healthy adapter (WindowPanePID always succeeds — pane present).
// The child exits naturally after ~1 s.  processDead(pid) should then return
// true, and Wait should unblock via the fast path without the secondary pane
// check triggering.
//
// Expected: Wait returns within 5 s.
func TestSilentTerm_SuccessfulAgentCompletedPathUnaffected(t *testing.T) {
	t.Parallel()

	// Spawn a short-lived child process to stand in for claude completing its work.
	cmd := exec.CommandContext(t.Context(), "sleep", "1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep subprocess: %v", err)
	}
	childPID := cmd.Process.Pid

	// Reap the child when it exits so it does not remain a zombie; zombies make
	// kill(pid, 0) return ESRCH only after Wait is called on macOS/Linux, which
	// would otherwise cause processDead to incorrectly return false.
	childExited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(childExited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-childExited:
		case <-time.After(5 * time.Second):
		}
	})

	// Use a healthy adapter where WindowPanePID always succeeds (pane alive).
	// This ensures Wait only unblocks via processDead (process exited), not via
	// the secondary pane-gone check introduced by hk-ry3be.
	healthyAdapter := &silentTermFixtureHealthyAdapter{spawnPID: childPID}
	substrate := silentTermFixtureNewSubstrate(t, healthyAdapter)

	spawn := handler.SubstrateSpawn{
		WindowName: "hk-win-success",
		Cwd:        t.TempDir(),
		Argv:       []string{"sleep", "1"},
	}

	sess, err := substrate.SpawnWindow(t.Context(), spawn)
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}
	if sess.PID() != childPID {
		t.Fatalf("PID: got %d, want %d", sess.PID(), childPID)
	}

	// Wait for the child to exit and be reaped (so processDead returns true).
	select {
	case <-childExited:
	case <-time.After(5 * time.Second):
		t.Fatal("child process did not exit within 5 s")
	}

	// Wait must unblock via processDead fast path (process exited, pane still "present").
	waitCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Errorf("Wait returned error: %v (want nil — process-exited fast path)", err)
	}
}

// silentTermFixtureHealthyAdapter is a tmux.Adapter where WindowPanePID
// always returns the configured PID (pane alive). Used to verify the process-
// exit fast path is NOT broken by the hk-ry3be secondary check.
type silentTermFixtureHealthyAdapter struct {
	spawnPID int
}

func (a *silentTermFixtureHealthyAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *silentTermFixtureHealthyAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *silentTermFixtureHealthyAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *silentTermFixtureHealthyAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win-success")}
}

func (a *silentTermFixtureHealthyAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return a.spawnPID, nil
}

func (a *silentTermFixtureHealthyAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "%98", nil
}

func (a *silentTermFixtureHealthyAdapter) KillSession(_ context.Context, _ string) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) SendKeysEnter(_ context.Context, _ string) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) SendKeysQuit(_ context.Context, _ string) error {
	return nil
}

func (a *silentTermFixtureHealthyAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// Compile-time assertion.
var _ tmux.Adapter = (*silentTermFixtureHealthyAdapter)(nil)
