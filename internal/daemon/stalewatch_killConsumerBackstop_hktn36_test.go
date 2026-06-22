package daemon_test

// stalewatch_killConsumerBackstop_hktn36_test.go — unit tests for the
// kill-consumer backstop path in StaleWatcher (hk-tn36).
//
// The kill-consumer backstop fires on the FIRST run_stale emission for any
// active run. It calls handle.Cancel() (and sets handle.aborted=true) so that
// waitWithSocketGrace unblocks and the bead can reopen, providing a
// defense-in-depth layer when pasteInjectQuitOnCommit's HB-staleness kill
// (8 min) did not fire (e.g. eventCh=nil, or the kill goroutine itself stalled).
//
// Test coverage:
//   - TestKillConsumerBackstop_FiresOnFirstRunStale — cancel called on first run_stale
//   - TestKillConsumerBackstop_FiresOnce            — cancel called at most once
//   - TestKillConsumerBackstop_NilCancelSafe        — no panic when Cancel is nil
//   - TestKillConsumerBackstop_AbortedSetBeforeCancel — aborted flag set before cancel
//   - TestKillConsumerBackstop_NotFiredBeforeStale  — no fire before M minutes

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// kcbNewRunID returns a UUIDv7-based RunID.
func kcbNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("kcbNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// kcbBuildWatcher builds a StaleWatcher with the given staleAfter and a
// mutable clock. Returns the watcher, a sealed collector bus, and the clock.
func kcbBuildWatcher(t *testing.T, reg *daemon.RunRegistry, staleAfter time.Duration, clk *mutableClock) (*daemon.StaleWatcher, *staleFixtureBus) {
	t.Helper()
	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   staleAfter,
		ScanInterval: time.Hour, // manual scans only
		Now:          clk.Now,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("kcbBuildWatcher: Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("kcbBuildWatcher: Seal: %v", err)
	}
	return w, sfb
}

// kcbSimulateEvent advances the clock and delivers the event to the watcher.
func kcbSimulateEvent(w *daemon.StaleWatcher, clk *mutableClock, runID core.RunID, typ core.EventType, at time.Time) {
	clk.Set(at)
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

// TestKillConsumerBackstop_FiresOnFirstRunStale verifies that handle.Cancel()
// is called when a run goes stale (M minutes of silence) for the first time.
func TestKillConsumerBackstop_FiresOnFirstRunStale(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := kcbNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-tn36-fire",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	const staleAfter = 10 * time.Minute
	w, sfb := kcbBuildWatcher(t, reg, staleAfter, clk)

	// Emit run_started so the run has at least one event.
	kcbSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	// Advance clock past staleAfter and scan.
	clk.Set(epoch.Add(11 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	// run_stale must have been emitted.
	events := sfb.collected()
	if len(events) == 0 {
		t.Fatal("expected at least one run_stale event")
	}

	// handle.Cancel() must have been called by the backstop.
	if !cancelCalled.Load() {
		t.Error("expected handle.Cancel() to be called by the kill-consumer backstop")
	}
	if !daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted=true after kill-consumer backstop fires")
	}
}

// TestKillConsumerBackstop_FiresOnce verifies the backstop fires at most once
// even when multiple scan passes cross the stale threshold.
func TestKillConsumerBackstop_FiresOnce(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := kcbNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCount atomic.Int32
	handle := &daemon.RunHandle{
		BeadID:    "hk-tn36-once",
		StartedAt: epoch,
		Cancel:    func() { cancelCount.Add(1) },
	}
	reg.Register(runID, handle)

	w, _ := kcbBuildWatcher(t, reg, 10*time.Minute, clk)
	kcbSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	clk.Set(epoch.Add(11 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())

	// Second scan: 2M window (20 min), so the next emission fires.
	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if got := cancelCount.Load(); got != 1 {
		t.Errorf("expected handle.Cancel() called exactly once, got %d", got)
	}
}

// TestKillConsumerBackstop_NilCancelSafe verifies no panic occurs when
// handle.Cancel is nil (run registered without per-run context).
func TestKillConsumerBackstop_NilCancelSafe(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := kcbNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{
		BeadID:    "hk-tn36-nil-cancel",
		StartedAt: epoch,
		Cancel:    nil, // intentionally nil
	}
	reg.Register(runID, handle)

	w, _ := kcbBuildWatcher(t, reg, 10*time.Minute, clk)
	kcbSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	clk.Set(epoch.Add(11 * time.Minute))
	// Must not panic.
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	// aborted must NOT be set when Cancel is nil (backstop returns early before
	// calling handle.aborted.Store(true), mirroring fireNeverSpawnedReaper).
	if daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted to remain false when Cancel is nil")
	}
}

// TestKillConsumerBackstop_AbortedSetBeforeCancel verifies that handle.aborted
// is set to true before handle.Cancel() is called so beadRunOne can distinguish
// a per-run abort from daemon-wide shutdown.
func TestKillConsumerBackstop_AbortedSetBeforeCancel(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := kcbNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var abortedAtCancel atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-tn36-aborted-order",
		StartedAt: epoch,
	}
	// Cancel reads aborted synchronously to confirm ordering.
	handle.Cancel = func() {
		abortedAtCancel.Store(daemon.ExportedRunHandleIsAborted(handle))
	}
	reg.Register(runID, handle)

	w, _ := kcbBuildWatcher(t, reg, 10*time.Minute, clk)
	kcbSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	clk.Set(epoch.Add(11 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if !abortedAtCancel.Load() {
		t.Error("expected handle.aborted=true when Cancel() was called (ordering violation)")
	}
}

// TestKillConsumerBackstop_NotFiredBeforeStale verifies the backstop does NOT
// fire before the stale threshold is reached.
func TestKillConsumerBackstop_NotFiredBeforeStale(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := kcbNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-tn36-not-yet",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, sfb := kcbBuildWatcher(t, reg, 10*time.Minute, clk)
	kcbSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	// Only 5 minutes — below the 10-min stale threshold.
	clk.Set(epoch.Add(5 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("expected backstop NOT to fire before stale threshold")
	}
	if daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted to remain false before stale threshold")
	}
	if got := len(sfb.collected()); got != 0 {
		t.Errorf("expected 0 run_stale events before threshold, got %d", got)
	}
}
