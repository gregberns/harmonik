package daemon_test

// tmuxsubstrate_spawncap_hkxb5yi_test.go — unit tests for the concurrent-spawn
// cap added by hk-xb5yi (WithSpawnCap option on NewTmuxSubstrate).
//
// # What is tested
//
//   - SpawnWindow succeeds up to the cap limit.
//   - SpawnWindow blocks when the cap is full and unblocks when Kill releases a slot.
//   - SpawnWindow returns an error when ctx is cancelled while waiting for a slot.
//   - Cap=0 (disabled) has no effect — SpawnWindow never blocks.
//
// # Helper prefix
//
// Helpers use the prefix "spawnCapFixture" per implementer-protocol.md §Helper-prefix
// discipline.
//
// # Spec refs
//
//   - Bead: hk-xb5yi (concurrent-spawn cap + reap-on-exit).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// spawnCapFixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// spawnCapFixtureAdapter is a deterministic fakeTmuxAdapter variant that
// supports configurable per-call outcomes for the spawn-cap tests.
//
// Unlike the shared fakeTmuxAdapter in tmuxsubstrate_hkgql2011_test.go, this
// adapter is safe for concurrent use across goroutines (uses a mutex).
type spawnCapFixtureAdapter struct {
	mu              sync.Mutex
	windowCallCount int
	killCallCount   int
	newWindowErr    error // when non-nil, NewWindowIn returns this as Err
}

func (a *spawnCapFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *spawnCapFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *spawnCapFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *spawnCapFixtureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.windowCallCount++
	if a.newWindowErr != nil {
		return tmux.Outcome{Err: a.newWindowErr}
	}
	return tmux.Outcome{Handle: tmux.WindowHandle("test-session:win" + string(rune('0'+a.windowCallCount)))}
}
func (a *spawnCapFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killCallCount++
	return nil
}
func (a *spawnCapFixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *spawnCapFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *spawnCapFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *spawnCapFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *spawnCapFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }
func (a *spawnCapFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}
func (a *spawnCapFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }
func (a *spawnCapFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error  { return nil }
func (a *spawnCapFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*spawnCapFixtureAdapter)(nil)

// spawnCapFixtureSpawn is a helper that calls SpawnWindow with minimal inputs
// and returns the session or an error.
func spawnCapFixtureSpawn(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-test-window",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSpawnCap_NoCap_NeverBlocks verifies that a substrate with cap=0 (no cap)
// spawns windows without any semaphore blocking.
func TestSpawnCap_NoCap_NeverBlocks(t *testing.T) {
	t.Parallel()

	adapter := &spawnCapFixtureAdapter{}
	// cap=0: WithSpawnCap(0) is a no-op.
	sub := daemon.NewTmuxSubstrate(adapter, "test-session", daemon.WithSpawnCap(0))

	ctx := context.Background()
	// Spawn 5 windows — all succeed immediately without cap.
	var sessions []handler.SubstrateSession
	for i := 0; i < 5; i++ {
		sess, err := spawnCapFixtureSpawn(ctx, sub)
		if err != nil {
			t.Fatalf("SpawnWindow[%d] unexpected error: %v", i, err)
		}
		sessions = append(sessions, sess)
	}

	// Kill all sessions (cleanup).
	for _, sess := range sessions {
		_ = sess.Kill(ctx)
	}
}

// TestSpawnCap_CapEnforced_SpawnBlocksWhenFull verifies that SpawnWindow blocks
// when all cap slots are occupied and unblocks when Kill releases a slot.
func TestSpawnCap_CapEnforced_SpawnBlocksWhenFull(t *testing.T) {
	t.Parallel()

	adapter := &spawnCapFixtureAdapter{}
	const cap = 2
	sub := daemon.NewTmuxSubstrate(adapter, "test-session", daemon.WithSpawnCap(cap))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fill the cap.
	sess1, err := spawnCapFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("spawn 1 failed: %v", err)
	}
	sess2, err := spawnCapFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("spawn 2 failed: %v", err)
	}

	// Third SpawnWindow MUST block because the cap (2) is full.
	// Launch it in a goroutine.
	spawnDone := make(chan error, 1)
	var sess3 handler.SubstrateSession
	go func() {
		var e error
		sess3, e = spawnCapFixtureSpawn(ctx, sub)
		spawnDone <- e
	}()

	// Verify the third spawn is blocked (not completed within 50ms).
	select {
	case err := <-spawnDone:
		t.Fatalf("expected third SpawnWindow to block, but it returned immediately (err=%v)", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked.
	}

	// Release one slot by killing sess1 — third spawn should now complete.
	if err := sess1.Kill(ctx); err != nil {
		t.Fatalf("sess1.Kill: %v", err)
	}

	select {
	case err := <-spawnDone:
		if err != nil {
			t.Fatalf("third SpawnWindow returned error after slot release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("third SpawnWindow did not complete after Kill released a slot")
	}

	// Cleanup remaining sessions.
	_ = sess2.Kill(ctx)
	if sess3 != nil {
		_ = sess3.Kill(ctx)
	}
}

// TestSpawnCap_CtxCancel_UnblocksWaitingSpawn verifies that cancelling the
// context unblocks a SpawnWindow call that is waiting for a slot.
func TestSpawnCap_CtxCancel_UnblocksWaitingSpawn(t *testing.T) {
	t.Parallel()

	adapter := &spawnCapFixtureAdapter{}
	const cap = 1
	sub := daemon.NewTmuxSubstrate(adapter, "test-session", daemon.WithSpawnCap(cap))

	bgCtx := context.Background()

	// Fill the single slot.
	sess1, err := spawnCapFixtureSpawn(bgCtx, sub)
	if err != nil {
		t.Fatalf("spawn 1 failed: %v", err)
	}

	// Try to spawn a second session with a cancellable context.
	spawnCtx, spawnCancel := context.WithCancel(bgCtx)
	spawnDone := make(chan error, 1)
	go func() {
		_, e := spawnCapFixtureSpawn(spawnCtx, sub)
		spawnDone <- e
	}()

	// Confirm it's blocked.
	select {
	case err := <-spawnDone:
		t.Fatalf("expected spawn to block but got: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	// Cancel the spawn context.
	spawnCancel()

	select {
	case err := <-spawnDone:
		if err == nil {
			t.Fatal("expected error after context cancel, got nil")
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, handler.ErrStructural) {
			t.Errorf("unexpected error type: %v (want context.Canceled or ErrStructural)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SpawnWindow did not return after context cancel")
	}

	_ = sess1.Kill(bgCtx)
}

// TestSpawnCap_SpawnFailure_SlotReleased verifies that when NewWindowIn returns
// an error, the semaphore slot is released so the next spawn can succeed.
func TestSpawnCap_SpawnFailure_SlotReleased(t *testing.T) {
	t.Parallel()

	adapter := &spawnCapFixtureAdapter{
		newWindowErr: errors.New("tmux: window creation failed"),
	}
	const cap = 1
	sub := daemon.NewTmuxSubstrate(adapter, "test-session", daemon.WithSpawnCap(cap))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// First spawn fails (adapter returns error).
	_, err := spawnCapFixtureSpawn(ctx, sub)
	if err == nil {
		t.Fatal("expected SpawnWindow to fail when adapter returns error, got nil")
	}

	// Now clear the error and try again. Slot MUST be available (released on failure).
	adapter.mu.Lock()
	adapter.newWindowErr = nil
	adapter.mu.Unlock()

	sess, err := spawnCapFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("second SpawnWindow failed after slot release: %v", err)
	}
	_ = sess.Kill(ctx)
}

// TestSpawnCap_KillIdempotent_SlotReleasedOnce verifies that calling Kill
// multiple times does not release multiple semaphore slots (killOnce guard).
func TestSpawnCap_KillIdempotent_SlotReleasedOnce(t *testing.T) {
	t.Parallel()

	adapter := &spawnCapFixtureAdapter{}
	const cap = 1
	sub := daemon.NewTmuxSubstrate(adapter, "test-session", daemon.WithSpawnCap(cap))

	ctx := context.Background()

	sess, err := spawnCapFixtureSpawn(ctx, sub)
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	// Kill three times — only one slot should be released.
	_ = sess.Kill(ctx)
	_ = sess.Kill(ctx)
	_ = sess.Kill(ctx)

	// After kill, the single slot must be free. Spawn another session immediately.
	done := make(chan error, 1)
	go func() {
		s, e := spawnCapFixtureSpawn(ctx, sub)
		if s != nil {
			_ = s.Kill(ctx)
		}
		done <- e
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second spawn after idempotent Kill failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second spawn blocked indefinitely — Kill released >1 slots or 0 slots")
	}
}
