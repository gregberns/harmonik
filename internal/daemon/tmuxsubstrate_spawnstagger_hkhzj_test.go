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
//   - SpawnStagger_FullIntervalSurvivesSlowWindowCreation: the interval is
//     measured from the END of a window creation, so a creation that takes real
//     time does not eat into it (hk-mirga, below).
//   - SpawnStagger_FailedWindowCreationStillStartsTheInterval: a creation that
//     comes back with an error still occupied the tmux server, so it still
//     starts the interval.
//
// # The interval is measured from the end of a creation (hk-mirga)
//
// callNewWindowBounded originally stamped lastWindowAt BEFORE calling the
// adapter, so the wait was charged the creation's own duration: the QUIET gap
// delivered — from one `tmux new-window` returning to the next one starting —
// was spawnStagger MINUS the creation time, and it reached zero once a creation
// took longer than the stagger. The stagger therefore weakened exactly as tmux
// slowed down, which is the load it exists to relieve.
//
// The four tests above could not see it. Each of them measures either the delay
// a caller experiences or the interval between two call STARTS, and start-to-start
// pacing was max(createDuration, stagger) before the fix and createDuration +
// stagger after it — never below stagger either way. They also drive an adapter
// that returns instantly, so there was no creation duration for the defect to
// spend. Both new tests set createDelay, and the QUIET gap is what they assert.
//
// # Helpers
//
// The "hkhzjStagger" prefix on the older helpers is a per-bead naming rule that
// implementer-protocol.md §"Where tests go" has since RETRACTED — it produced
// bead-named files and duplicate fakes. New helpers here are named after the
// behaviour they serve (staggerQuietGap), and new tests extend the fake above
// rather than declaring a second one.
//
// # Beads
//
//   - hk-hzj (agent_ready_timeout recurs under concurrent dispatch burst).
//   - hk-mirga (the stagger was measured from the start of a creation).

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
// tests. It records when each NewWindowIn call was entered and when it returned,
// so tests can verify both the interval between call starts and the quiet gap
// between one creation returning and the next starting.
//
// The zero value creates windows instantly and successfully — the shape the
// first four tests want. createDelay makes a creation take real time, which is
// what the quiet gap needs in order to be measurable at all; outcomeErr makes
// the creation fail while still taking that time.
type hkhzjStaggerAdapter struct {
	createDelay time.Duration
	outcomeErr  error

	mu          sync.Mutex
	callTimes   []time.Time
	returnTimes []time.Time
}

func (a *hkhzjStaggerAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkhzjStaggerAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *hkhzjStaggerAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *hkhzjStaggerAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	entered := time.Now()
	time.Sleep(a.createDelay)
	a.mu.Lock()
	a.callTimes = append(a.callTimes, entered)
	// Taken as late as this call can take it. The substrate stamps lastWindowAt a
	// few microseconds LATER — after the outcome crosses the channel in
	// callBoundedTmuxCreate — so a gap measured from here is at most that much
	// generous, and the shortfall these tests hunt is three orders of magnitude
	// bigger.
	a.returnTimes = append(a.returnTimes, time.Now())
	n := len(a.callTimes)
	a.mu.Unlock()
	if a.outcomeErr != nil {
		return tmux.Outcome{Err: a.outcomeErr}
	}
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

// staggerQuietGap returns the quiet interval the substrate left between two
// window creations: from the moment creation 1 returned to the moment creation 2
// started. That is the interval the stagger exists to buy — the head start one
// agent gets before the next window is created and the next cold start begins.
//
// It also fails the test unless the fake really spent wantCreateDelay inside the
// first creation. An instant creation leaves nothing for a start-stamped
// interval to consume, so without that check a caller could pass this helper an
// adapter against which its assertion could not fail (PRINCIPLES.md §7).
func staggerQuietGap(t *testing.T, adapter *hkhzjStaggerAdapter, wantCreateDelay time.Duration) time.Duration {
	t.Helper()

	adapter.mu.Lock()
	entered := append([]time.Time(nil), adapter.callTimes...)
	returned := append([]time.Time(nil), adapter.returnTimes...)
	adapter.mu.Unlock()

	if len(entered) != 2 || len(returned) != 2 {
		t.Fatalf("expected 2 NewWindowIn calls, got %d entered / %d returned", len(entered), len(returned))
	}
	if took := returned[0].Sub(entered[0]); took < wantCreateDelay {
		t.Fatalf("fake adapter did not spend its creation time: first NewWindowIn took %v, want >= %v", took, wantCreateDelay)
	}
	return entered[1].Sub(returned[0])
}

// staggerKillOnCleanup releases a spawned session at the end of the test and
// fails if the substrate cannot release it.
func staggerKillOnCleanup(t *testing.T, ctx context.Context, sess handler.SubstrateSession, label string) {
	t.Helper()
	t.Cleanup(func() {
		if err := sess.Kill(ctx); err != nil {
			t.Errorf("killing %s: %v", label, err)
		}
	})
}

// TestSpawnStagger_FullIntervalSurvivesSlowWindowCreation defends the promise
// WithSpawnStagger makes: the next window creation starts at least spawnStagger
// after the previous one ended. Creating a window takes real time here, so a
// stagger measured from the START of a creation delivers only stagger minus that
// time (hk-mirga).
func TestSpawnStagger_FullIntervalSurvivesSlowWindowCreation(t *testing.T) {
	const (
		stagger     = 25 * time.Millisecond
		createDelay = 15 * time.Millisecond
	)

	adapter := &hkhzjStaggerAdapter{createDelay: createDelay}
	sub := daemon.NewTmuxSubstrate(adapter, "stagger-session",
		daemon.WithSpawnCap(4),
		daemon.WithSpawnStagger(stagger),
	)

	ctx := context.Background()

	sess1, err := hkhzjStaggerSpawn(ctx, sub, "slow-1")
	if err != nil {
		t.Fatalf("first SpawnWindow failed: %v", err)
	}
	staggerKillOnCleanup(t, ctx, sess1, "window 1")

	sess2, err := hkhzjStaggerSpawn(ctx, sub, "slow-2")
	if err != nil {
		t.Fatalf("second SpawnWindow failed: %v", err)
	}
	staggerKillOnCleanup(t, ctx, sess2, "window 2")

	if gap := staggerQuietGap(t, adapter, createDelay); gap < stagger {
		t.Errorf("SpawnStagger_FullIntervalSurvivesSlowWindowCreation FAIL: window creation 2 started %v after creation 1 returned; want >= %v (a %v creation was charged against the stagger)",
			gap, stagger, createDelay)
	}
}

// TestSpawnStagger_FailedWindowCreationStillStartsTheInterval defends the other
// half: a creation that comes back with an error still held the tmux server for
// its duration, so it still starts the interval and the next creation waits a
// full spawnStagger from that failure. The stamp is deferred, so this is the
// test that breaks if it stops covering the paths that do not return a handle.
func TestSpawnStagger_FailedWindowCreationStillStartsTheInterval(t *testing.T) {
	const (
		stagger     = 25 * time.Millisecond
		createDelay = 15 * time.Millisecond
	)

	wantErr := errors.New("tmux new-window refused")
	adapter := &hkhzjStaggerAdapter{createDelay: createDelay, outcomeErr: wantErr}
	sub := daemon.NewTmuxSubstrate(adapter, "stagger-session",
		daemon.WithSpawnCap(4),
		daemon.WithSpawnStagger(stagger),
	)

	ctx := context.Background()

	if _, err := hkhzjStaggerSpawn(ctx, sub, "failed-1"); !errors.Is(err, wantErr) {
		t.Fatalf("first SpawnWindow: got err %v, want one wrapping %v", err, wantErr)
	}
	if _, err := hkhzjStaggerSpawn(ctx, sub, "failed-2"); !errors.Is(err, wantErr) {
		t.Fatalf("second SpawnWindow: got err %v, want one wrapping %v", err, wantErr)
	}

	if gap := staggerQuietGap(t, adapter, createDelay); gap < stagger {
		t.Errorf("SpawnStagger_FailedWindowCreationStillStartsTheInterval FAIL: window creation 2 started %v after the failed creation 1 returned; want >= %v",
			gap, stagger)
	}
}
