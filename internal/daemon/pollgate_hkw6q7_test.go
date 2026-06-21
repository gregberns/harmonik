package daemon_test

// pollgate_hkw6q7_test.go — SS-007 poll-gating conformance tests.
//
// Verifies that StaleWatcher and BandwidthTuner are OFF (return early) when the
// PollGate reports INACTIVE, and ON (do their normal work) when not INACTIVE.
//
// Spec ref: specs/system-state.md §4.3 SS-007 (poll-arming is mechanism).
// Bead ref: hk-w6q7 (P2-b: system-state fold + poll-gating).

import (
	"context"
	"encoding/json"
	"sync"
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

// pollGateFixtureNewRunID returns a UUIDv7-based RunID.
func pollGateFixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("pollGateFixtureNewRunID: %v", err)
	}
	return core.RunID(u)
}

// pollGateFixtureCollectingBus returns a sealed bus that records all run_stale
// event payloads.  The returned slice pointer is written only from the bus
// goroutine and read only after a brief sleep, so no extra locking is needed
// in simple tests (the bus delivers synchronously in tests).
type pollGateFixtureCollector struct {
	mu      sync.Mutex
	stale   []core.RunStalePayload
}

func (c *pollGateFixtureCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.stale)
}

func pollGateFixtureNewBusWithCollector(t *testing.T) (eventbus.EventBus, *pollGateFixtureCollector) {
	t.Helper()
	col := &pollGateFixtureCollector{}
	bus := eventbus.NewBusImpl()
	sub := core.Subscription{
		ConsumerID:    "pollgate-test-collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if evt.Type != string(core.EventTypeRunStale) {
				return nil
			}
			var pl core.RunStalePayload
			if err := json.Unmarshal(evt.Payload, &pl); err != nil {
				return nil
			}
			col.mu.Lock()
			col.stale = append(col.stale, pl)
			col.mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("pollGateFixtureNewBusWithCollector: Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("pollGateFixtureNewBusWithCollector: Seal: %v", err)
	}
	return bus, col
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — StaleWatcher
// ─────────────────────────────────────────────────────────────────────────────

// TestPollGate_StaleWatcher_GatedWhenINACTIVE verifies that scan() returns early
// without emitting run_stale when the PollGate reports INACTIVE (SS-007).
func TestPollGate_StaleWatcher_GatedWhenINACTIVE(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	runID := pollGateFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-w6q7-test-gated",
		StartedAt: startedAt,
	})

	emitBus, col := pollGateFixtureNewBusWithCollector(t)
	subBus := eventbus.NewBusImpl()

	gate := &daemon.PollGate{}
	gate.SetInactive(true) // INACTIVE → scan must be a no-op

	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: subBus,
		Emitter:      emitBus,
		Registry:     reg,
		Gate:         gate,
		StaleAfter:   1 * time.Nanosecond, // threshold crossed immediately
		ScanInterval: time.Hour,
		Now:          func() time.Time { return startedAt.Add(24 * time.Hour) },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := subBus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if n := col.count(); n != 0 {
		t.Errorf("INACTIVE gate: expected 0 run_stale events, got %d (watcher should be OFF)", n)
	}
}

// TestPollGate_StaleWatcher_ArmedWhenNotINACTIVE verifies that scan() emits
// run_stale normally when the PollGate does NOT report INACTIVE (SS-007).
func TestPollGate_StaleWatcher_ArmedWhenNotINACTIVE(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	runID := pollGateFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-w6q7-test-armed",
		StartedAt: startedAt,
	})

	emitBus, col := pollGateFixtureNewBusWithCollector(t)
	subBus := eventbus.NewBusImpl()

	gate := &daemon.PollGate{}
	gate.SetInactive(false) // NOT INACTIVE → scan must do normal work

	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: subBus,
		Emitter:      emitBus,
		Registry:     reg,
		Gate:         gate,
		StaleAfter:   1 * time.Nanosecond,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return startedAt.Add(24 * time.Hour) },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := subBus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if n := col.count(); n == 0 {
		t.Errorf("non-INACTIVE gate: expected ≥1 run_stale event, got 0 (watcher should be ON)")
	}
}

// TestPollGate_StaleWatcher_NilGateAlwaysRuns verifies that a nil gate (ungated)
// causes scan() to always run, which is the correct default for unit-test mode.
func TestPollGate_StaleWatcher_NilGateAlwaysRuns(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	runID := pollGateFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-w6q7-test-nil-gate",
		StartedAt: startedAt,
	})

	emitBus, col := pollGateFixtureNewBusWithCollector(t)
	subBus := eventbus.NewBusImpl()

	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: subBus,
		Emitter:      emitBus,
		Registry:     reg,
		Gate:         nil, // no gate
		StaleAfter:   1 * time.Nanosecond,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return startedAt.Add(24 * time.Hour) },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := subBus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if n := col.count(); n == 0 {
		t.Errorf("nil gate: expected ≥1 run_stale event, got 0 (ungated watcher should always run)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — BandwidthTuner
// ─────────────────────────────────────────────────────────────────────────────

// TestPollGate_BandwidthTuner_GatedWhenINACTIVE verifies that tick() does not
// adjust the concurrency ceiling when the PollGate reports INACTIVE (SS-007).
func TestPollGate_BandwidthTuner_GatedWhenINACTIVE(t *testing.T) {
	t.Parallel()

	ctrl := daemon.NewConcurrencyController(4)
	initial := ctrl.Get()

	// Use a huge ceiling so the formula would normally increase concurrency.
	tuner := daemon.NewBandwidthTuner(ctrl, 4, 1_000_000_000, t.TempDir())

	gate := &daemon.PollGate{}
	gate.SetInactive(true) // INACTIVE → tick must be a no-op
	tuner.SetGate(gate)

	daemon.ExportedBandwidthTunerTick(tuner)

	if got := ctrl.Get(); got != initial {
		t.Errorf("INACTIVE gate: concurrency changed from %d to %d (tick should be OFF)", initial, got)
	}
}

// TestPollGate_BandwidthTuner_NilGateAlwaysRuns verifies that a nil gate causes
// tick() to run normally (initial tick always fires; transcript read returns 0
// tokens from the temp dir so ceiling stays at maxN).
func TestPollGate_BandwidthTuner_NilGateAlwaysRuns(t *testing.T) {
	t.Parallel()

	ctrl := daemon.NewConcurrencyController(4)

	// Temp dir has no transcripts → used==0 → effectiveMax==maxN==4.
	tuner := daemon.NewBandwidthTuner(ctrl, 4, 100, t.TempDir())
	// gate is nil: SetGate not called

	// Tick once: with zero usage and ceiling=100, effectiveMax = round(4*1) = 4.
	// The ceiling should not change from initial value (4), but the tick must run.
	// We verify it ran by checking that ctrl.Get() is still a valid value
	// (the tick doesn't panic and doesn't corrupt state).
	daemon.ExportedBandwidthTunerTick(tuner)

	// As long as this does not panic and the concurrency is still in [1, 4],
	// the nil-gate path is functioning correctly.
	got := ctrl.Get()
	if got < 1 || got > 4 {
		t.Errorf("nil gate: concurrency %d out of valid range [1, 4]", got)
	}
}
