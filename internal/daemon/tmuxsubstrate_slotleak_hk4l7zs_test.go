package daemon_test

// tmuxsubstrate_slotleak_hk4l7zs_test.go — regression tests for the
// spawn-semaphore slot leak / over-hold bug (hk-4l7zs).
//
// # The bug
//
// SpawnWindow acquires a spawn-semaphore slot and the slot is released ONLY
// when the session's Kill() is called. The daemon emitted launch_initiated and
// then called SpawnWindow, which blocked INDEFINITELY when no slot was free.
// In production a single new bead wedged at launch_initiated while only 2 runs
// were active on a 12-slot pool — i.e. slots were held outside the active-run
// set (acquired-but-never-released). With no acquire timeout the wedge lasted
// until the 30-min implementer budget expired, failing the run no_commit.
//
// # What is tested
//
//   - SlotLeak_SpawnBlocksForeverWithoutTimeout: when the pool is saturated by
//     leaked slots (sessions never Killed), a new SpawnWindow must NOT block
//     indefinitely — it must time out and return ErrStructural (the hk-4l7zs
//     bounded-acquire fix). Without the fix this test hangs (caught by the
//     bounded select below).
//   - SlotLeak_TimeoutFiresDiagnosticHook: the spawn_cap_blocked diagnostic hook
//     fires on timeout with the saturated in-use/cap counts.
//   - SlotLeak_AcquireReleaseRoundTrips: a sequence of acquire/Kill round-trips
//     (mimicking many completed runs) returns the free-slot count to full —
//     no slot is leaked across runs.
//
// # Helper prefix
//
// Helpers use the prefix "slotLeakFixture" per implementer-protocol.md.
//
// # Bead
//
//   - hk-4l7zs (spawn-semaphore slot leak / over-hold).

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

// slotLeakFixtureAdapter is a concurrency-safe fake tmux adapter for the
// slot-leak tests. NewWindowIn always succeeds with a unique handle.
type slotLeakFixtureAdapter struct {
	mu          sync.Mutex
	windowCount int
}

func (a *slotLeakFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *slotLeakFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *slotLeakFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *slotLeakFixtureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.windowCount++
	return tmux.Outcome{Handle: tmux.WindowHandle("slotleak-session:win" + string(rune('a'+a.windowCount%26)))}
}
func (a *slotLeakFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (a *slotLeakFixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *slotLeakFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *slotLeakFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *slotLeakFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *slotLeakFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error      { return nil }
func (a *slotLeakFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error  { return nil }
func (a *slotLeakFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error       { return nil }
func (a *slotLeakFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error        { return nil }
func (a *slotLeakFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*slotLeakFixtureAdapter)(nil)

func slotLeakFixtureSpawn(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-slotleak-window",
	})
}

// TestSlotLeak_SpawnBlocksForeverWithoutTimeout reproduces the core hk-4l7zs
// wedge: the spawn pool is saturated by sessions that acquired slots and were
// never Killed (leaked). A new launch's SpawnWindow must time out rather than
// block forever.
//
// Pre-fix (no bounded acquire) this SpawnWindow blocks indefinitely — the
// outer bounded select fails the test with "blocked indefinitely".
func TestSlotLeak_SpawnBlocksForeverWithoutTimeout(t *testing.T) {
	t.Parallel()

	adapter := &slotLeakFixtureAdapter{}
	const capN = 2
	// Short acquire timeout so the test is fast; production default is minutes.
	sub := daemon.NewTmuxSubstrate(adapter, "slotleak-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(200*time.Millisecond))

	// Use a background context with NO cancellation/deadline: the fix must NOT
	// rely on ctx to break the wedge — the daemon's run context is the 30-min
	// budget, far longer than the acquire timeout. Only the acquire timeout
	// must save us.
	ctx := context.Background()

	// Saturate the pool by acquiring every slot and NEVER releasing (leak).
	for i := 0; i < capN; i++ {
		if _, err := slotLeakFixtureSpawn(ctx, sub); err != nil {
			t.Fatalf("saturating spawn %d failed: %v", i, err)
		}
	}
	if got := daemon.ExportedSpawnSlotsInUse(sub); got != capN {
		t.Fatalf("pool not saturated: SpawnSlotsInUse()=%d want %d", got, capN)
	}

	// A new launch must return (with an error) within a bounded wall-clock
	// window. Pre-fix it never returns.
	done := make(chan error, 1)
	go func() {
		_, err := slotLeakFixtureSpawn(ctx, sub)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected spawn-cap timeout error, got nil (slot appeared from nowhere?)")
		}
		if !errors.Is(err, handler.ErrStructural) {
			t.Errorf("spawn-cap timeout error is not ErrStructural: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SpawnWindow blocked indefinitely on a saturated pool — slot-leak wedge (hk-4l7zs) NOT fixed")
	}
}

// TestSlotLeak_TimeoutFiresDiagnosticHook verifies the spawn_cap_blocked
// diagnostic hook fires on acquire timeout, carrying the saturated counts.
func TestSlotLeak_TimeoutFiresDiagnosticHook(t *testing.T) {
	t.Parallel()

	adapter := &slotLeakFixtureAdapter{}
	const capN = 1

	var (
		hookMu      sync.Mutex
		hookFired   bool
		gotInUse    int
		gotCapSize  int
		gotWaitedMS int64
	)
	sub := daemon.NewTmuxSubstrate(adapter, "slotleak-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(150*time.Millisecond),
		daemon.WithSpawnCapBlockedHook(func(waited time.Duration, inUse, capSize int) {
			hookMu.Lock()
			defer hookMu.Unlock()
			hookFired = true
			gotInUse = inUse
			gotCapSize = capSize
			gotWaitedMS = waited.Milliseconds()
		}))

	ctx := context.Background()
	if _, err := slotLeakFixtureSpawn(ctx, sub); err != nil { // leak the single slot
		t.Fatalf("saturating spawn failed: %v", err)
	}

	if _, err := slotLeakFixtureSpawn(ctx, sub); err == nil {
		t.Fatal("expected blocked spawn to error, got nil")
	}

	hookMu.Lock()
	defer hookMu.Unlock()
	if !hookFired {
		t.Fatal("spawn_cap_blocked diagnostic hook did not fire on acquire timeout")
	}
	if gotCapSize != capN {
		t.Errorf("hook capSize=%d want %d", gotCapSize, capN)
	}
	if gotInUse != capN {
		t.Errorf("hook inUse=%d want %d (pool should be saturated)", gotInUse, capN)
	}
	if gotWaitedMS <= 0 {
		t.Errorf("hook waitedMS=%d want > 0", gotWaitedMS)
	}
}

// TestSlotLeak_AcquireReleaseRoundTrips drives many acquire/Kill round-trips
// (mimicking completed runs) and asserts the free-slot count returns to full
// after each — no slot is leaked across runs. A run that holds two slots at
// once (impl + reviewer over-hold) or fails to release would leave residual
// slots in use and eventually wedge new launches.
func TestSlotLeak_AcquireReleaseRoundTrips(t *testing.T) {
	t.Parallel()

	adapter := &slotLeakFixtureAdapter{}
	const capN = 4
	sub := daemon.NewTmuxSubstrate(adapter, "slotleak-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(2*time.Second))

	ctx := context.Background()

	// Simulate 20 completed runs, each acquiring an implementer slot then a
	// reviewer slot — releasing the implementer BEFORE acquiring the reviewer
	// (release-before-reacquire), then releasing the reviewer. After each run
	// the pool must be fully free.
	for run := 0; run < 20; run++ {
		impl, err := slotLeakFixtureSpawn(ctx, sub)
		if err != nil {
			t.Fatalf("run %d implementer spawn failed (slot leak from a prior run?): %v", run, err)
		}
		// Release the implementer slot before acquiring the reviewer slot.
		if err := impl.Kill(ctx); err != nil {
			t.Fatalf("run %d implementer Kill: %v", run, err)
		}
		rev, err := slotLeakFixtureSpawn(ctx, sub)
		if err != nil {
			t.Fatalf("run %d reviewer spawn failed: %v", run, err)
		}
		if err := rev.Kill(ctx); err != nil {
			t.Fatalf("run %d reviewer Kill: %v", run, err)
		}
		if got := daemon.ExportedSpawnSlotsInUse(sub); got != 0 {
			t.Fatalf("after run %d: SpawnSlotsInUse()=%d want 0 — slot leaked across runs", run, got)
		}
	}
}
