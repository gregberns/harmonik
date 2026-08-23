package daemon_test

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

	sess1, err := hkhzjStaggerSpawn(ctx, sub, "1")
	if err != nil {
		t.Fatalf("first SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess1.Kill(ctx) }()

	sess2, err := hkhzjStaggerSpawn(ctx, sub, "2")
	if err != nil {
		t.Fatalf("second SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess2.Kill(ctx) }()

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

	sess1, err := hkhzjStaggerSpawn(ctx, sub, "1")
	if err != nil {
		t.Fatalf("first SpawnWindow failed: %v", err)
	}
	defer func() { _ = sess1.Kill(ctx) }()

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
