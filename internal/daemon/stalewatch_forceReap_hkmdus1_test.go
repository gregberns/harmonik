package daemon_test

// stalewatch_forceReap_hkmdus1_test.go — unit tests for the re-entrant
// force-reap watchdog and the fast dead-process reap in StaleWatcher (hk-mdus1).
//
// The force-reap watchdog is the backstop for the one-shot auto-cancellers
// (kill-consumer, never-spawned reaper): when handle.Cancel() is invoked but the
// per-run goroutine does NOT unwind (parked on a non-runCtx wait, or Cancel was
// nil), the RunHandle — and the concurrency slot it occupies in
// RunRegistry.Len()/LenForQueue — leaks forever, gridlocking the fleet. The
// watchdog force-Unregisters the leaked handle forceReapGrace after Cancel was
// first invoked, freeing the slot and driving the queue item terminal.
//
// Coverage:
//   - TestForceReap_WedgedCancelForceUnregistersSlot — Cancel that does not
//     unwind gets the slot force-Unregistered after the grace; ForceReap seam runs.
//   - TestForceReap_NilCancelStillReaped            — Cancel==nil path is reaped.
//   - TestForceReap_NotBeforeGrace                  — no reap before the grace elapses.
//   - TestForceReap_SeamReceivesQueueCoordinates    — force-reap seam gets queue fields.
//   - TestFastDeadProcessReap_CancelsWhenDeadAndSilent — dead+silent ⇒ cancel.
//   - TestFastDeadProcessReap_SkipsWhenAlive        — alive process ⇒ no cancel.

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

func frNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("frNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// frWatcherOpts collects the optional seams a force-reap test wants wired.
type frWatcherOpts struct {
	forceReap      func(runID core.RunID, handle *daemon.RunHandle)
	runProcessDead func(runID core.RunID, handle *daemon.RunHandle) bool
	staleAfter     time.Duration
	deadStaleAfter time.Duration
}

func frBuildWatcher(t *testing.T, reg *daemon.RunRegistry, clk *mutableClock, opts frWatcherOpts) (*daemon.StaleWatcher, *staleFixtureBus) {
	t.Helper()
	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	staleAfter := opts.staleAfter
	if staleAfter <= 0 {
		staleAfter = 10 * time.Minute
	}
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus:          unsealed,
		Emitter:               sfb.bus,
		Registry:              reg,
		StaleAfter:            staleAfter,
		ScanInterval:          time.Hour, // manual scans only
		Now:                   clk.Now,
		ForceReap:             opts.forceReap,
		RunProcessDead:        opts.runProcessDead,
		DeadProcessStaleAfter: opts.deadStaleAfter,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("frBuildWatcher: Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("frBuildWatcher: Seal: %v", err)
	}
	return w, sfb
}

func frObserve(w *daemon.StaleWatcher, clk *mutableClock, runID core.RunID, typ core.EventType, at time.Time) {
	clk.Set(at)
	runIDCopy := runID
	evt := core.Event{
		EventID: core.EventID(uuid.Must(uuid.NewV7())),
		Type:    string(typ),
		RunID:   &runIDCopy,
	}
	daemon.ExportedStalewatchObserve(w, context.Background(), evt)
}

// TestForceReap_WedgedCancelForceUnregistersSlot is the load-bearing test: a run
// whose Cancel is invoked (by the kill-consumer backstop) but does NOT unwind
// the goroutine (Cancel here is a no-op that never Unregisters) must have its
// leaked registry slot force-Unregistered after forceReapGrace.
func TestForceReap_WedgedCancelForceUnregistersSlot(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := frNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-mdus1-wedged",
		StartedAt: epoch,
		// Simulate a wedged goroutine: Cancel fires but never Unregisters.
		Cancel: func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	var reapCalled atomic.Bool
	w, _ := frBuildWatcher(t, reg, clk, frWatcherOpts{
		staleAfter: 10 * time.Minute,
		forceReap: func(_ core.RunID, _ *daemon.RunHandle) {
			reapCalled.Store(true)
		},
	})

	frObserve(w, clk, runID, core.EventTypeRunStarted, epoch)

	// First stale scan: kill-consumer backstop invokes Cancel and records
	// cancelledAt. The slot is still registered (Cancel did not unwind).
	clk.Set(epoch.Add(11 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	if !cancelCalled.Load() {
		t.Fatal("expected Cancel to be invoked by the kill-consumer backstop")
	}
	if got := reg.Len(); got != 1 {
		t.Fatalf("slot must remain registered immediately after Cancel; Len=%d want 1", got)
	}

	// Before the grace elapses: still registered, not force-reaped.
	clk.Set(epoch.Add(11 * time.Minute).Add(80 * time.Second))
	daemon.ExportedStalewatchScan(w, context.Background())
	if got := reg.Len(); got != 1 {
		t.Fatalf("slot must not be force-reaped before the grace; Len=%d want 1", got)
	}
	if reapCalled.Load() {
		t.Fatal("ForceReap seam must not fire before the grace elapses")
	}

	// After the grace: the leaked slot is force-Unregistered and the seam runs.
	clk.Set(epoch.Add(11 * time.Minute).Add(100 * time.Second))
	daemon.ExportedStalewatchScan(w, context.Background())
	if got := reg.Len(); got != 0 {
		t.Fatalf("expected the leaked slot to be force-Unregistered after the grace; Len=%d want 0", got)
	}
	if !reapCalled.Load() {
		t.Fatal("expected the ForceReap seam to fire on force-reap")
	}
}

// TestForceReap_NilCancelStillReaped verifies the Cancel==nil early-return leak
// is closed: even when no per-run context was wired, the run is force-reaped
// after the grace.
func TestForceReap_NilCancelStillReaped(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := frNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{
		BeadID:    "hk-mdus1-nilcancel",
		StartedAt: epoch,
		Cancel:    nil, // no per-run context — the historical permanent-leak case
	}
	reg.Register(runID, handle)

	w, _ := frBuildWatcher(t, reg, clk, frWatcherOpts{staleAfter: 10 * time.Minute})

	frObserve(w, clk, runID, core.EventTypeRunStarted, epoch)

	// Stale scan records cancelledAt even though Cancel is nil.
	clk.Set(epoch.Add(11 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	if got := reg.Len(); got != 1 {
		t.Fatalf("slot still registered right after stale; Len=%d want 1", got)
	}

	// After the grace: force-reaped despite Cancel==nil.
	clk.Set(epoch.Add(13 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	if got := reg.Len(); got != 0 {
		t.Fatalf("expected Cancel==nil run to be force-reaped after the grace; Len=%d want 0", got)
	}
}

// TestForceReap_SeamReceivesQueueCoordinates verifies the ForceReap seam is
// handed the RunHandle's denormalized queue coordinates so the daemon can drive
// the owning queue item terminal.
func TestForceReap_SeamReceivesQueueCoordinates(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := frNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	qid := "queue-abc"
	gidx := 2
	handle := &daemon.RunHandle{
		BeadID:          "hk-mdus1-queue",
		StartedAt:       epoch,
		QueueName:       "featureq",
		QueueID:         &qid,
		QueueGroupIndex: &gidx,
		QueueItemIndex:  5,
		Cancel:          func() {},
	}
	reg.Register(runID, handle)

	var gotName string
	var gotQID string
	var gotGIdx, gotItem int
	w, _ := frBuildWatcher(t, reg, clk, frWatcherOpts{
		staleAfter: 10 * time.Minute,
		forceReap: func(_ core.RunID, h *daemon.RunHandle) {
			gotName = h.QueueName
			if h.QueueID != nil {
				gotQID = *h.QueueID
			}
			if h.QueueGroupIndex != nil {
				gotGIdx = *h.QueueGroupIndex
			}
			gotItem = h.QueueItemIndex
		},
	})

	frObserve(w, clk, runID, core.EventTypeRunStarted, epoch)
	clk.Set(epoch.Add(11 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	clk.Set(epoch.Add(13 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())

	if reg.Len() != 0 {
		t.Fatalf("expected force-reap; Len=%d want 0", reg.Len())
	}
	if gotName != "featureq" || gotQID != "queue-abc" || gotGIdx != 2 || gotItem != 5 {
		t.Fatalf("ForceReap seam got wrong queue coords: name=%q qid=%q gidx=%d item=%d", gotName, gotQID, gotGIdx, gotItem)
	}
}

// TestFastDeadProcessReap_CancelsWhenDeadAndSilent verifies that when the
// dead-process probe reports the agent gone AND the run has been silent past
// DeadProcessStaleAfter, the run is cancelled immediately (starting the
// force-reap grace) instead of waiting out the 10–30 min stale thresholds.
func TestFastDeadProcessReap_CancelsWhenDeadAndSilent(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := frNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-mdus1-deadproc",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := frBuildWatcher(t, reg, clk, frWatcherOpts{
		staleAfter:     24 * time.Hour, // keep kill-consumer far away
		deadStaleAfter: 2 * time.Minute,
		runProcessDead: func(_ core.RunID, _ *daemon.RunHandle) bool { return true },
	})

	frObserve(w, clk, runID, core.EventTypeRunStarted, epoch)

	// Within the silence window: dead but too recent ⇒ not cancelled.
	clk.Set(epoch.Add(1 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	if cancelCalled.Load() {
		t.Fatal("fast dead-process reap must not fire before DeadProcessStaleAfter")
	}

	// Past the silence window: dead + silent ⇒ cancelled.
	clk.Set(epoch.Add(3 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	if !cancelCalled.Load() {
		t.Fatal("expected fast dead-process reap to cancel a dead+silent run")
	}
	if !daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("expected handle.aborted=true after fast dead-process reap")
	}
}

// TestFastDeadProcessReap_SkipsWhenAlive verifies a live process is never
// fast-reaped even after the silence window.
func TestFastDeadProcessReap_SkipsWhenAlive(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := frNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-mdus1-alive",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := frBuildWatcher(t, reg, clk, frWatcherOpts{
		staleAfter:     24 * time.Hour,
		deadStaleAfter: 2 * time.Minute,
		runProcessDead: func(_ core.RunID, _ *daemon.RunHandle) bool { return false },
	})

	frObserve(w, clk, runID, core.EventTypeRunStarted, epoch)
	clk.Set(epoch.Add(10 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	if cancelCalled.Load() {
		t.Fatal("fast dead-process reap must not fire for a live process")
	}
	if reg.Len() != 1 {
		t.Fatalf("live run must stay registered; Len=%d want 1", reg.Len())
	}
}
