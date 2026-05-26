package daemon_test

// panecleanup_hke6mtt_test.go — tests for tmux pane cleanup after run-fail/cancel
// (hk-e6mtt).
//
// Regression guard for the fix in workloop.go and reviewloop.go: after
// waitWithSocketGrace returns (process exited naturally), the daemon now calls
// sess.Kill on the tmux substrate path (watcher==nil) so the tmux pane window
// is destroyed and does not persist as a stale window in the harmonik session.
//
// These tests verify the substrate-level behavior that makes the workloop fix
// correct:
//   1. Kill called after process natural exit still issues KillWindow (not short-
//      circuited by early-exit assumptions).
//   2. Kill is idempotent when called from the cancel path (already called inside
//      waitWithSocketGrace) AND again from the post-wait cleanup path.
//
// Bead: hk-e6mtt.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestPaneCleanup_KillAfterNaturalExit_StillCleansWindow verifies that calling
// Kill on a tmuxSubstrateSession after the hosted process has already exited
// naturally (no context cancellation, no prior Kill) still issues KillWindow.
//
// This is the new post-waitWithSocketGrace cleanup path added by hk-e6mtt:
// workloop.go calls sess.Kill(context.Background()) after waitWithSocketGrace
// returns on the tmux substrate path (watcher==nil), ensuring the tmux pane
// window is destroyed even when the process exited cleanly without a Kill
// being issued during the wait.
func TestPaneCleanup_KillAfterNaturalExit_StillCleansWindow(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		// pid=0 bypasses the signal step in Kill and goes straight to KillWindow.
		panePIDResult: 0,
	}
	substrate := daemon.NewTmuxSubstrate(fake, "test-session")

	sess, err := substrate.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	})
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// Simulate the workloop post-waitWithSocketGrace call: Kill is called after
	// the process has already exited. Because Kill has NOT been called yet (no
	// prior cancel path), killOnce fires and KillWindow MUST be called once.
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("Kill after natural exit: unexpected error: %v", err)
	}

	if fake.killWindowCalled != 1 {
		t.Errorf("KillWindow call count after post-exit Kill: got %d, want 1", fake.killWindowCalled)
	}
}

// TestPaneCleanup_KillTwice_SecondCallIsNoOp verifies that when Kill is called
// twice (cancel path calls it inside waitWithSocketGrace, then the post-wait
// cleanup path calls it again), KillWindow is only called once.
//
// This guards the idempotency guarantee that makes it safe for workloop.go to
// always call sess.Kill after waitWithSocketGrace without checking whether it
// was already called on the cancel path.
func TestPaneCleanup_KillTwice_SecondCallIsNoOp(t *testing.T) {
	t.Parallel()

	fake := &fakeTmuxAdapter{
		newWindowInOutcome: tmux.Outcome{Handle: tmux.WindowHandle("test-session:hk-win")},
		panePIDResult:      0,
	}
	substrate := daemon.NewTmuxSubstrate(fake, "test-session")

	sess, err := substrate.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "hk-win",
		Cwd:        t.TempDir(),
		Argv:       []string{"claude"},
	})
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}

	// First call: the cancel path (inside waitWithSocketGrace on ctx.Done()).
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("Kill (cancel path): unexpected error: %v", err)
	}

	// Second call: the post-wait cleanup path added by hk-e6mtt.
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("Kill (post-wait cleanup): unexpected error: %v", err)
	}

	// KillWindow MUST be called exactly once — the second Kill is a no-op.
	if fake.killWindowCalled != 1 {
		t.Errorf("KillWindow call count after two Kill calls: got %d, want 1", fake.killWindowCalled)
	}
}
