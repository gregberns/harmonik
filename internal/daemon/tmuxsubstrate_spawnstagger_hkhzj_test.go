package daemon_test

// tmuxsubstrate_spawnstagger_hkhzj_test.go — unit tests for the spawn-stagger
// mechanism (hk-hzj).
//
// # The bug
//
// Under a concurrent dispatch burst at --max-concurrent ≥ 4 (2/8 gurney-q beads
// failed), multiple claude agents cold-started simultaneously and competed for disk
// I/O and CPU. With disk at ≥90% utilisation, agent cold-start exceeded the (then-)
// 30s agent_ready_timeout, causing spurious run_failed events.
//
// # The fix (two-part)
//
// 1. Increase defaultAgentReadyTimeout from 30s → 90s.
// 2. Add WithSpawnStagger: a configurable minimum interval between consecutive
//    tmux window creations. Spreading cold-starts reduces peak I/O contention.
//    WithSpawnStagger(d) enforces "at least d between consecutive SpawnWindow
//    window-creation calls" by sleeping inside the newWindowMu-protected section
//    of callNewWindowBounded.
//
// # What is tested
//
//   - SpawnStagger_SecondSpawnWaitsForInterval: with spawnStagger=50ms, a second
//     SpawnWindow must not complete until at least 50ms after the first window
//     was created.
//   - SpawnStagger_FirstSpawnNotDelayed: the first SpawnWindow call experiences
//     no stagger delay (lastWindowAt is zero, so the "elapsed < stagger" branch
//     is skipped).
//   - SpawnStagger_ContextCancelDuringStagger: when ctx is cancelled while the
//     stagger is sleeping, SpawnWindow returns an ErrStructural error promptly.
//   - SpawnStagger_ZeroDisablesStagger: with spawnStagger=0 (the default), N
//     concurrent SpawnWindow calls complete without inter-call delay.
//
// # Helper prefix
//
// Helpers use the prefix "hkhzjStagger" per implementer-protocol.md §Helper-prefix
// discipline (namespaced by bead id + purpose).
//
// # Bead
//
//   - hk-hzj (agent_ready_timeout recurs under concurrent dispatch burst).

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

// hkhzjStaggerAdapter is a concurrency-safe fake tmux adapter for the stagger
// tests. NewWindowIn always succeeds immediately; it records the timestamp of
// each call so tests can verify inter-call intervals.
type hkhzjStaggerAdapter struct {
	mu        sync.Mutex
	callTimes []time.Time
}

func (a *hkhzjStaggerAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkhzjStaggerAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *hkhzjStaggerAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *hkhzjStaggerAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.callTimes = append(a.callTimes, time.Now())
	n := len(a.callTimes)
	a.mu.Unlock()
	return tmux.Outcome{Handle: tmux.WindowHandle("stagger-session:win" + string(rune('a'+n%26)))}
}
func (a *hkhzjStaggerAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (a *hkhzjStaggerAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *hkhzjStaggerAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkhzjStaggerAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *hkhzjStaggerAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *hkhzjStaggerAdapter) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *hkhzjStaggerAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *hkhzjStaggerAdapter) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *hkhzjStaggerAdapter) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *hkhzjStaggerAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*hkhzjStaggerAdapter)(nil)

func hkhzjStaggerSpawn(ctx context.Context, sub handler.Substrate, name string) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-stagger-" + name,
	})
}

// TestSpawnStagger_SecondSpawnWaitsForInterval verifies that with a spawnStagger
// of 50ms, the second SpawnWindow does not complete (i.e., its window is not
// created) until at least 50ms after the first window was created.
func TestSpawnStagger_SecondSpawnWaitsForInterval(t *testing.T) {
	t.Parallel()

	const stagger = 50 * time.Millisecond

	adapter := &hkhzjStaggerAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "stagger-session",
		daemon.WithSpawnCap(4),
		daemon.WithSpawnStagger(stagger),
	)

	ctx := context.Background()

	// First spawn: should complete immediately (no stagger on first call).
	sess1, err := hkhzjStaggerSpawn(ctx, sub, "1")
	if err != nil {
		t.Fatalf("first SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess1.Kill(ctx) }()

	// Second spawn: should be delayed by at least stagger from first window creation.
	sess2, err := hkhzjStaggerSpawn(ctx, sub, "2")
	if err != nil {
		t.Fatalf("second SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess2.Kill(ctx) }()

	// Verify the two NewWindowIn calls were separated by at least stagger.
	adapter.mu.Lock()
	times := append([]time.Time(nil), adapter.callTimes...)
	adapter.mu.Unlock()

	if len(times) != 2 {
		t.Fatalf("expected 2 NewWindowIn calls, got %d", len(times))
	}

	gap := times[1].Sub(times[0])
	if gap < stagger {
		t.Errorf("SpawnStagger_SecondSpawnWaitsForInterval FAIL: gap between window 1 and window 2 = %v; want >= %v (spawn stagger not enforced)", gap, stagger)
	}
}

// TestSpawnStagger_FirstSpawnNotDelayed verifies that the first SpawnWindow call
// experiences no stagger delay: it completes as fast as the underlying adapter.
func TestSpawnStagger_FirstSpawnNotDelayed(t *testing.T) {
	t.Parallel()

	const stagger = 5 * time.Second // deliberately large; first spawn must not wait

	adapter := &hkhzjStaggerAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "stagger-session",
		daemon.WithSpawnCap(4),
		daemon.WithSpawnStagger(stagger),
	)

	ctx := context.Background()
	start := time.Now()

	sess, err := hkhzjStaggerSpawn(ctx, sub, "1")
	if err != nil {
		t.Fatalf("first SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess.Kill(ctx) }()

	elapsed := time.Since(start)
	// The first call must complete far faster than the stagger delay (5s).
	// Allow 1s for tmux overhead / slow CI machines.
	if elapsed > time.Second {
		t.Errorf("SpawnStagger_FirstSpawnNotDelayed FAIL: first SpawnWindow took %v; want < 1s (stagger should not apply to first spawn)", elapsed)
	}
}

// TestSpawnStagger_ContextCancelDuringStagger verifies that when the context is
// cancelled while the stagger sleep is in progress, SpawnWindow returns an error
// (wrapping ErrStructural) promptly rather than blocking until stagger elapses.
func TestSpawnStagger_ContextCancelDuringStagger(t *testing.T) {
	t.Parallel()

	const stagger = 5 * time.Second // long enough that cancel fires during sleep

	adapter := &hkhzjStaggerAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "stagger-session",
		daemon.WithSpawnCap(4),
		daemon.WithSpawnStagger(stagger),
	)

	ctx := context.Background()

	// First spawn: no stagger, succeeds immediately.
	sess1, err := hkhzjStaggerSpawn(ctx, sub, "1")
	if err != nil {
		t.Fatalf("first SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess1.Kill(ctx) }()

	// Second spawn: would wait stagger=5s, but we cancel the context after 100ms.
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = hkhzjStaggerSpawn(cancelCtx, sub, "2")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("SpawnStagger_ContextCancelDuringStagger FAIL: expected error on ctx cancel, got nil")
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("SpawnStagger_ContextCancelDuringStagger FAIL: error %v does not wrap handler.ErrStructural", err)
	}
	// Cancel should have fired well before stagger (5s). Allow 1s for overhead.
	if elapsed > time.Second {
		t.Errorf("SpawnStagger_ContextCancelDuringStagger FAIL: SpawnWindow took %v after ctx cancel; want < 1s (stagger blocked ctx cancel)", elapsed)
	}
}

// TestSpawnStagger_ZeroDisablesStagger verifies that with spawnStagger=0 (the
// default), N concurrent SpawnWindow calls complete without inter-call delay.
// This is the regression guard: disabling stagger must not accidentally re-enable
// the contention problem or add unexpected latency.
func TestSpawnStagger_ZeroDisablesStagger(t *testing.T) {
	t.Parallel()

	const n = 4
	adapter := &hkhzjStaggerAdapter{}
	// No WithSpawnStagger call — default is 0 = disabled.
	sub := daemon.NewTmuxSubstrate(adapter, "stagger-session",
		daemon.WithSpawnCap(n),
	)

	ctx := context.Background()
	start := time.Now()

	var sessions []handler.SubstrateSession
	for i := range n {
		sess, err := hkhzjStaggerSpawn(ctx, sub, string(rune('a'+i)))
		if err != nil {
			t.Fatalf("SpawnWindow %d failed: %v", i, err)
		}
		sessions = append(sessions, sess)
	}
	for _, sess := range sessions {
		_ = sess.Kill(ctx)
	}

	elapsed := time.Since(start)
	// Without stagger, 4 serial spawns through the mutex should complete well under 1s
	// on any reasonable machine (each tmux.Outcome is instant from the fake adapter).
	if elapsed > time.Second {
		t.Errorf("SpawnStagger_ZeroDisablesStagger FAIL: %d serial spawns without stagger took %v; want < 1s", n, elapsed)
	}

	adapter.mu.Lock()
	nCalls := len(adapter.callTimes)
	adapter.mu.Unlock()
	if nCalls != n {
		t.Errorf("SpawnStagger_ZeroDisablesStagger FAIL: expected %d NewWindowIn calls, got %d", n, nCalls)
	}
}

