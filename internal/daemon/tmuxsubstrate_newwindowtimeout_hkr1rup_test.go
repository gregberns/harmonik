package daemon_test

// tmuxsubstrate_newwindowtimeout_hkr1rup_test.go — regression tests for the
// no-spawn wedge: a hung `tmux new-window` shell call (hk-r1rup).
//
// # The bug
//
// SpawnWindow calls adapter.NewWindowIn, which shells out to `tmux new-window`.
// That call had NO timeout. When the tmux invocation hangs (server busy, FD
// exhaustion, etc.) NewWindowIn returns neither a value nor an error, so
// SpawnWindow → handler.Launch never returns: launch_initiated never fires, and
// the run wedges at launch_stall_detected → run_stale forever, holding a daemon
// slot until the 30-min implementer budget expires and fails no_commit. This is
// DISTINCT from the spawn-semaphore acquire wedge (hk-4l7zs), which already has a
// bounded acquire timeout and emits spawn_cap_blocked.
//
// # What is tested
//
//   - NewWindowTimeout_SpawnDoesNotHang: when NewWindowIn blocks forever, a new
//     SpawnWindow must NOT block indefinitely — it must time out and return
//     ErrStructural (the hk-r1rup bounded-call fix). Without the fix this test
//     hangs (caught by the bounded select below).
//   - NewWindowTimeout_FiresDiagnosticHook: the tmux_new_window_timeout diagnostic
//     hook fires on the bounded timeout, with a positive waited duration; and the
//     returned error is ErrTmuxNewWindowTimeout-wrapped.
//
// # Helper prefix
//
// Helpers use the prefix "hkr1rup" per implementer-protocol.md (namespaced by
// bead id to avoid redeclaration collisions with parallel daemon test beads).
//
// # Bead
//
//   - hk-r1rup (bound `tmux new-window` to kill the no-spawn wedge).

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

// hkr1rupBlockingAdapter is a concurrency-safe fake tmux adapter whose
// NewWindowIn BLOCKS until release is closed OR the call's context is cancelled
// (the bounded-call ctx), simulating a hung `tmux new-window` invocation. All
// other adapter methods are inert.
type hkr1rupBlockingAdapter struct {
	// release, when closed, lets NewWindowIn return a successful Outcome.
	// Left open (never closed) by the tests to simulate an indefinite hang.
	release chan struct{}

	mu       sync.Mutex
	calls    int
	ctxFired bool // set true if a NewWindowIn call observed ctx cancellation
}

func newHKR1RUPBlockingAdapter() *hkr1rupBlockingAdapter {
	return &hkr1rupBlockingAdapter{release: make(chan struct{})}
}

func (a *hkr1rupBlockingAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkr1rupBlockingAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *hkr1rupBlockingAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// NewWindowIn blocks until either release is closed (success) or ctx is
// cancelled (the bounded-call timeout fired). It RESPECTS ctx — so this test
// also exercises the ctx-aware adapter path — but the daemon's goroutine+select
// wrapper is the backstop that must save us even if an adapter ignored ctx.
func (a *hkr1rupBlockingAdapter) NewWindowIn(ctx context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	select {
	case <-a.release:
		return tmux.Outcome{Handle: tmux.WindowHandle("hkr1rup-session:win0"), PaneID: "%1"}
	case <-ctx.Done():
		a.mu.Lock()
		a.ctxFired = true
		a.mu.Unlock()
		return tmux.Outcome{Err: ctx.Err()}
	}
}

func (a *hkr1rupBlockingAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (a *hkr1rupBlockingAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *hkr1rupBlockingAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkr1rupBlockingAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *hkr1rupBlockingAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *hkr1rupBlockingAdapter) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *hkr1rupBlockingAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *hkr1rupBlockingAdapter) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *hkr1rupBlockingAdapter) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *hkr1rupBlockingAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*hkr1rupBlockingAdapter)(nil)

func hkr1rupSpawn(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-r1rup-window",
	})
}

// TestNewWindowTimeout_SpawnDoesNotHang reproduces the core hk-r1rup wedge: a
// hung `tmux new-window` call. SpawnWindow must time out (bounded by the
// new-window timeout) and return ErrStructural rather than block forever.
//
// Pre-fix (no bounded new-window call) this SpawnWindow blocks indefinitely —
// the outer bounded select fails the test with "blocked indefinitely".
func TestNewWindowTimeout_SpawnDoesNotHang(t *testing.T) {
	t.Parallel()

	adapter := newHKR1RUPBlockingAdapter() // release never closed → NewWindowIn hangs

	// Short new-window timeout so the test is fast; production default is 60s.
	sub := daemon.NewTmuxSubstrate(adapter, "hkr1rup-session",
		daemon.WithNewWindowTimeout(150*time.Millisecond))

	// Use a background context with NO cancellation/deadline: the fix must NOT
	// rely on the caller's ctx to break the wedge — the daemon's run context is
	// the 30-min budget, far longer than the new-window timeout. Only the
	// new-window timeout must save us.
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := hkr1rupSpawn(ctx, sub)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected tmux new-window timeout error, got nil (call returned?)")
		}
		if !errors.Is(err, handler.ErrStructural) {
			t.Errorf("new-window timeout error is not ErrStructural: %v", err)
		}
		if !errors.Is(err, daemon.ErrTmuxNewWindowTimeout) {
			t.Errorf("new-window timeout error is not ErrTmuxNewWindowTimeout: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SpawnWindow blocked indefinitely on a hung tmux new-window — no-spawn wedge (hk-r1rup) NOT fixed")
	}
}

// TestNewWindowTimeout_FiresDiagnosticHook verifies the tmux_new_window_timeout
// diagnostic hook fires on the bounded timeout, carrying a positive waited
// duration, and that the returned error chains through the sentinel.
func TestNewWindowTimeout_FiresDiagnosticHook(t *testing.T) {
	t.Parallel()

	adapter := newHKR1RUPBlockingAdapter() // hangs

	var (
		hookMu      sync.Mutex
		hookFired   bool
		gotWaitedMS int64
	)
	sub := daemon.NewTmuxSubstrate(adapter, "hkr1rup-session",
		daemon.WithNewWindowTimeout(120*time.Millisecond),
		daemon.WithNewWindowTimedOutHook(func(waited time.Duration) {
			hookMu.Lock()
			defer hookMu.Unlock()
			hookFired = true
			gotWaitedMS = waited.Milliseconds()
		}))

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := hkr1rupSpawn(ctx, sub)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected new-window timeout error, got nil")
		}
		if !errors.Is(err, daemon.ErrTmuxNewWindowTimeout) {
			t.Errorf("error does not wrap ErrTmuxNewWindowTimeout: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SpawnWindow blocked indefinitely — hk-r1rup bound did not fire")
	}

	hookMu.Lock()
	defer hookMu.Unlock()
	if !hookFired {
		t.Fatal("tmux_new_window_timeout diagnostic hook did not fire on the bounded timeout")
	}
	if gotWaitedMS <= 0 {
		t.Errorf("hook waitedMS=%d want > 0", gotWaitedMS)
	}
}
