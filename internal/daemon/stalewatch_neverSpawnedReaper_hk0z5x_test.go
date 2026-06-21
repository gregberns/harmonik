package daemon_test

// stalewatch_neverSpawnedReaper_hk0z5x_test.go — unit tests for the
// never-spawned reaper path in StaleWatcher (hk-0z5x).
//
// The never-spawned reaper fires when launch_initiated has been observed for a
// run but agent_ready has NOT been observed within NeverSpawnedReaperTimeout.
// It cancels the per-run context (handle.Cancel) and marks handle.aborted=true
// so beadRunOne can emit run_failed and reopen the bead.
//
// Test coverage:
//   - TestNeverSpawnedReaper_FiringCondition               — fires after deadline
//   - TestNeverSpawnedReaper_SuppressedIfAgentReady        — suppressed when agent_ready seen
//   - TestNeverSpawnedReaper_SuppressedIfNoLaunchInitiated — no fire without launch_initiated
//   - TestNeverSpawnedReaper_FiresOnce                     — fires at most once per run
//   - TestNeverSpawnedReaper_NotYetDue                     — no fire before deadline
//   - TestNeverSpawnedReaper_NilCancelSafe                 — no panic when Cancel is nil
//   - TestNeverSpawnedReaper_AbortedSetBeforeCancel        — aborted flag set before Cancel called

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// nsrNewRunID returns a UUIDv7-based RunID.
func nsrNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("nsrNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// mutableClock is a thread-safe clock that tests can advance.
type mutableClock struct {
	mu sync.Mutex
	t  time.Time
}

func newMutableClock(t time.Time) *mutableClock { return &mutableClock{t: t} }
func (c *mutableClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *mutableClock) Set(t time.Time)         { c.mu.Lock(); defer c.mu.Unlock(); c.t = t }
func (c *mutableClock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

// nsrBuildWatcher builds a StaleWatcher with a mutable clock and the given
// reaper timeout. Returns the watcher, the mutable clock, and a collector bus.
func nsrBuildWatcher(t *testing.T, reg *daemon.RunRegistry, reaperTimeout time.Duration, clk *mutableClock) (*daemon.StaleWatcher, *staleFixtureBus) {
	t.Helper()
	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus:              unsealed,
		Emitter:                   sfb.bus,
		Registry:                  reg,
		StaleAfter:                10 * time.Minute,
		ScanInterval:              time.Hour,
		NeverSpawnedReaperTimeout: reaperTimeout,
		Now:                       clk.Now,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("nsrBuildWatcher: Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("nsrBuildWatcher: Seal: %v", err)
	}
	return w, sfb
}

// nsrSimulateEvent advances the clock to eventTime then delivers the event to
// the watcher's observe callback so its internal per-run state is updated.
func nsrSimulateEvent(w *daemon.StaleWatcher, clk *mutableClock, runID core.RunID, typ core.EventType, eventTime time.Time) {
	clk.Set(eventTime)
	runIDCopy := runID
	evt := core.Event{
		EventID: core.EventID(uuid.Must(uuid.NewV7())),
		Type:    string(typ),
		RunID:   &runIDCopy,
	}
	daemon.ExportedStalewatchObserve(w, context.Background(), evt)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestNeverSpawnedReaper_FiringCondition verifies the reaper fires when
// launch_initiated was seen but agent_ready was not, and the deadline passed.
func TestNeverSpawnedReaper_FiringCondition(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-0z5x-fire",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	const reaperTimeout = 30 * time.Minute
	w, _ := nsrBuildWatcher(t, reg, reaperTimeout, clk)

	// Emit launch_initiated at t=0.
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	// Advance clock past the deadline and scan.
	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if !cancelCalled.Load() {
		t.Error("expected handle.Cancel() to be called by the never-spawned reaper")
	}
	if !daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted to be true after reaper fires")
	}
}

// TestNeverSpawnedReaper_SuppressedIfAgentReady verifies the reaper does NOT
// fire when agent_ready has been seen (normal implementer start path).
func TestNeverSpawnedReaper_SuppressedIfAgentReady(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-0z5x-suppressed",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)

	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)
	nsrSimulateEvent(w, clk, runID, core.EventTypeAgentReady, epoch.Add(1*time.Second))

	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("expected handle.Cancel() NOT to be called when agent_ready was observed")
	}
	if daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted to remain false when agent_ready was observed")
	}
}

// TestNeverSpawnedReaper_SuppressedIfNoLaunchInitiated verifies the reaper
// does NOT fire when launch_initiated has not been observed.
func TestNeverSpawnedReaper_SuppressedIfNoLaunchInitiated(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-0z5x-no-launch",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)

	// Only run_started — no launch_initiated.
	nsrSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("expected reaper NOT to fire when launch_initiated was never seen")
	}
}

// TestNeverSpawnedReaper_FiresOnce verifies the reaper fires at most once per
// run even across multiple scan passes.
func TestNeverSpawnedReaper_FiresOnce(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCount atomic.Int32
	handle := &daemon.RunHandle{
		BeadID:    "hk-0z5x-once",
		StartedAt: epoch,
		Cancel:    func() { cancelCount.Add(1) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	daemon.ExportedStalewatchScan(w, context.Background())
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if got := cancelCount.Load(); got != 1 {
		t.Errorf("expected Cancel() called exactly once, got %d", got)
	}
}

// TestNeverSpawnedReaper_NotYetDue verifies the reaper does NOT fire when the
// deadline has not yet been reached.
func TestNeverSpawnedReaper_NotYetDue(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-0z5x-not-due",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	// Clock: only 15 minutes after launch_initiated — deadline NOT exceeded.
	clk.Set(epoch.Add(15 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("expected reaper NOT to fire before the deadline")
	}
}

// TestNeverSpawnedReaper_PerDispatch_DOTReviewerStallsBeforeAgentReady verifies
// the per-dispatch never-spawned reaper added by hk-sj6a.
//
// Scenario: a DOT run whose implementer node completed (launch_initiated +
// agent_ready) but whose reviewer node stalled before agent_ready.  The classic
// check is suppressed (agentReadySeen=true from the implementer).  The
// per-dispatch check must fire after NeverSpawnedReaperTimeout from the
// reviewer's launch_initiated.
func TestNeverSpawnedReaper_PerDispatch_DOTReviewerStallsBeforeAgentReady(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-sj6a-per-dispatch",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	const reaperTimeout = 30 * time.Minute
	w, _ := nsrBuildWatcher(t, reg, reaperTimeout, clk)

	// Implementer dispatch: launch_initiated then agent_ready.
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)
	nsrSimulateEvent(w, clk, runID, core.EventTypeAgentReady, epoch.Add(1*time.Minute))

	// Reviewer dispatch: launch_initiated but NO agent_ready (stall).
	reviewerLaunchAt := epoch.Add(10 * time.Minute)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, reviewerLaunchAt)

	// Advance past NeverSpawnedReaperTimeout from the reviewer's launch_initiated.
	clk.Set(reviewerLaunchAt.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if !cancelCalled.Load() {
		t.Error("hk-sj6a: expected per-dispatch never-spawned reaper to fire for " +
			"DOT reviewer that stalled before agent_ready")
	}
	if !daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("hk-sj6a: expected handle.aborted=true after per-dispatch reaper fires")
	}
}

// TestNeverSpawnedReaper_PerDispatch_SuppressedWhenReviewerGetsAgentReady verifies
// the per-dispatch reaper does NOT fire when the reviewer's session does get
// agent_ready (the normal, healthy DOT run path).
func TestNeverSpawnedReaper_PerDispatch_SuppressedWhenReviewerGetsAgentReady(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-sj6a-suppressed",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)

	// Implementer: both events.
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)
	nsrSimulateEvent(w, clk, runID, core.EventTypeAgentReady, epoch.Add(1*time.Minute))

	// Reviewer: launch_initiated AND agent_ready (healthy path).
	reviewerLaunchAt := epoch.Add(10 * time.Minute)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, reviewerLaunchAt)
	nsrSimulateEvent(w, clk, runID, core.EventTypeAgentReady, reviewerLaunchAt.Add(1*time.Minute))

	// Advance past reaper timeout — reaper should remain suppressed.
	clk.Set(reviewerLaunchAt.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("hk-sj6a: per-dispatch reaper must NOT fire when reviewer got agent_ready")
	}
}

// TestNeverSpawnedReaper_NilCancelSafe verifies the reaper does not panic when
// handle.Cancel is nil (run registered before per-run context was wired).
func TestNeverSpawnedReaper_NilCancelSafe(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	// Cancel is intentionally nil — old-style registration without per-run ctx.
	handle := &daemon.RunHandle{
		BeadID:    "hk-0z5x-nil-cancel",
		StartedAt: epoch,
		Cancel:    nil,
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	clk.Set(epoch.Add(31 * time.Minute))
	// Must not panic.
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	// aborted must NOT be set when Cancel is nil (fireNeverSpawnedReaper logs
	// a warning and returns early before calling handle.aborted.Store(true)).
	if daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted to remain false when Cancel is nil")
	}
}
