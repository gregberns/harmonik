package daemon_test

// tmuxsubstrate_session_keepalive_hk9ptu_test.go — regression tests for the
// proactive session keepalive in tmuxSubstrate (hk-9ptu).
//
// # The bug
//
// After a daemon restart via supervisor-revive (DaemonWatchdog), the daemon
// falls back to the deterministic "harmonik-<hash>-default" session (hk-u9ji
// flywheel exclusion) and creates it via EnsureSession at boot.  The session
// then sits with only an idle zsh window (no active beads).
//
// If the session is killed externally between dispatches (e.g. by the operator,
// a rogue reaper, or OS-level TMOUT idle-shell exit), the reactive hk-yaj
// ErrNoSession self-heal in SpawnWindow recovers ONLY when a SpawnWindow call
// hits ErrNoSession.  Between dispatches no SpawnWindow is in-flight, so the
// dead session can sit undetected until the NEXT dispatch attempt fails — but
// because ALL concurrent SpawnWindow calls share the same dead session, a burst
// of new beads all hit ErrNoSession simultaneously, causing fleet-wide
// launch_initiated outage until the hk-yaj retry recreates the session.
//
// # The fix
//
// WithSessionKeepalive enables a proactive RunSessionKeepalive goroutine that
// calls EnsureSession on the adapter at a fixed interval (default 30 s).  If
// the session is dead the call recreates it before the next dispatch, so the
// hk-yaj SpawnWindow retry path is never needed for this class of failure.
//
// # What is tested
//
//   - KeepaliveCallsEnsureSessionPeriodically: EnsureSession is called ≥1 time
//     within a generous timeout when keepalive is enabled with a short interval.
//
//   - KeepaliveStopsOnContextCancel: RunSessionKeepalive exits promptly when
//     the context is cancelled; EnsureSession is NOT called after cancellation.
//
//   - KeepaliveNoopWhenAdapterLacksEnsure: when the adapter does not implement
//     sessionEnsurer, RunSessionKeepalive returns immediately without calling
//     anything and without blocking.
//
//   - KeepaliveNoopWhenNotEnabled: when WithSessionKeepalive is NOT passed,
//     RunSessionKeepalive returns immediately (keepaliveEnabled=false guard).
//
//   - KeepaliveCustomInterval: a custom interval passed to WithSessionKeepalive
//     is honoured (EnsureSession is called at that cadence, not the default 30 s).
//
// # Bead
//
//   - hk-9ptu (daemon restart orphans -default spawn session; proactive keepalive).

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─── fixture adapters ─────────────────────────────────────────────────────────

// keepaliveCountingAdapter counts EnsureSession calls.
type keepaliveCountingAdapter struct {
	noSessionFixtureBase
	ensureCalls atomic.Int64
}

func (a *keepaliveCountingAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.windowCount++
	return tmux.Outcome{Handle: tmux.WindowHandle("sess:win-keepalive")}
}

func (a *keepaliveCountingAdapter) EnsureSession(_ context.Context, _, _ string) error {
	a.ensureCalls.Add(1)
	return nil
}

var _ tmux.Adapter = (*keepaliveCountingAdapter)(nil)

// keepaliveNoEnsureAdapter does NOT implement sessionEnsurer — verifies that
// RunSessionKeepalive is a no-op in this case.
type keepaliveNoEnsureAdapter struct {
	noSessionFixtureBase
}

func (a *keepaliveNoEnsureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.windowCount++
	return tmux.Outcome{Handle: tmux.WindowHandle("sess:win-noensure")}
}

var _ tmux.Adapter = (*keepaliveNoEnsureAdapter)(nil)

// ─── tests ────────────────────────────────────────────────────────────────────

// TestSessionKeepalive_CallsEnsureSessionPeriodically verifies that
// RunSessionKeepalive calls EnsureSession at least once within a short timeout
// when a sub-millisecond keepalive interval is configured.
func TestSessionKeepalive_CallsEnsureSessionPeriodically(t *testing.T) {
	t.Parallel()

	adapter := &keepaliveCountingAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-keepalive-test",
		daemon.WithSessionKeepalive(5*time.Millisecond), // very short for fast test
		daemon.WithNewWindowTimeout(2*time.Second),
	)

	sk, ok := sub.(interface{ RunSessionKeepalive(ctx context.Context) })
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement RunSessionKeepalive — interface guard missing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sk.RunSessionKeepalive(ctx)
	}()

	// Wait up to 500 ms for at least one EnsureSession call.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if adapter.ensureCalls.Load() >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	if adapter.ensureCalls.Load() < 1 {
		t.Errorf("EnsureSession not called within 500 ms; got %d calls", adapter.ensureCalls.Load())
	}

	// Stop the goroutine.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("RunSessionKeepalive goroutine did not stop within 2 s after ctx cancel")
	}
}

// TestSessionKeepalive_StopsOnContextCancel verifies that RunSessionKeepalive
// exits promptly on ctx cancellation and does not call EnsureSession afterwards.
func TestSessionKeepalive_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	adapter := &keepaliveCountingAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-keepalive-cancel-test",
		daemon.WithSessionKeepalive(10*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second),
	)

	sk, ok := sub.(interface{ RunSessionKeepalive(ctx context.Context) })
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement RunSessionKeepalive")
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		sk.RunSessionKeepalive(ctx)
	}()

	// Let it tick a few times.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("RunSessionKeepalive did not stop within 500 ms after ctx cancel")
		return
	}

	// Snapshot call count immediately after stop; no further calls should land.
	countAtStop := adapter.ensureCalls.Load()
	time.Sleep(50 * time.Millisecond)
	if after := adapter.ensureCalls.Load(); after != countAtStop {
		t.Errorf("EnsureSession called after ctx cancel: count went from %d to %d", countAtStop, after)
	}
}

// TestSessionKeepalive_NoopWhenAdapterLacksEnsure verifies that
// RunSessionKeepalive is a no-op (returns immediately, blocks nothing) when the
// adapter does not implement sessionEnsurer.
func TestSessionKeepalive_NoopWhenAdapterLacksEnsure(t *testing.T) {
	t.Parallel()

	adapter := &keepaliveNoEnsureAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-keepalive-noensure-test",
		daemon.WithSessionKeepalive(5*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second),
	)

	sk, ok := sub.(interface{ RunSessionKeepalive(ctx context.Context) })
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement RunSessionKeepalive")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sk.RunSessionKeepalive(ctx)
	}()

	// Must return promptly since adapter lacks EnsureSession.
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("RunSessionKeepalive did not return promptly when adapter lacks EnsureSession")
	}
}

// TestSessionKeepalive_NoopWhenNotEnabled verifies that RunSessionKeepalive
// returns immediately (without calling EnsureSession) when WithSessionKeepalive
// was not passed to NewTmuxSubstrate.
func TestSessionKeepalive_NoopWhenNotEnabled(t *testing.T) {
	t.Parallel()

	adapter := &keepaliveCountingAdapter{}
	// No WithSessionKeepalive — keepaliveEnabled stays false.
	sub := daemon.NewTmuxSubstrate(adapter, "hk-keepalive-disabled-test",
		daemon.WithNewWindowTimeout(2*time.Second),
	)

	sk, ok := sub.(interface{ RunSessionKeepalive(ctx context.Context) })
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement RunSessionKeepalive")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sk.RunSessionKeepalive(ctx)
	}()

	// Must return promptly without calling EnsureSession.
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("RunSessionKeepalive did not return promptly when keepaliveEnabled=false")
	}

	if adapter.ensureCalls.Load() != 0 {
		t.Errorf("EnsureSession called %d times; want 0 when keepalive is not enabled",
			adapter.ensureCalls.Load())
	}
}

// TestSessionKeepalive_CustomInterval verifies that a custom interval is used
// rather than the 30 s default.  We use a 10 ms interval and assert at least
// two calls arrive within 500 ms, which would not happen with a 30 s interval.
func TestSessionKeepalive_CustomInterval(t *testing.T) {
	t.Parallel()

	adapter := &keepaliveCountingAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "hk-keepalive-custom-interval-test",
		daemon.WithSessionKeepalive(10*time.Millisecond),
		daemon.WithNewWindowTimeout(2*time.Second),
	)

	sk, ok := sub.(interface{ RunSessionKeepalive(ctx context.Context) })
	if !ok {
		t.Fatal("NewTmuxSubstrate result does not implement RunSessionKeepalive")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sk.RunSessionKeepalive(ctx)

	// Wait up to 500 ms for at least 2 calls (would fail with the 30 s default).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if adapter.ensureCalls.Load() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if adapter.ensureCalls.Load() < 2 {
		t.Errorf("expected ≥2 EnsureSession calls within 500 ms with 10 ms interval; got %d",
			adapter.ensureCalls.Load())
	}
}
