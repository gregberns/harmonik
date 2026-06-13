package daemon_test

// stalewatch_wkzlc_test.go — unit tests for StaleWatcher (hk-wkzlc).
//
// Test coverage per bead acceptance criteria:
//
//   - TestStaleWatch_NoEmitBelowThreshold       — active run below M → no run_stale
//   - TestStaleWatch_EmitAtThreshold             — active run at M → run_stale emitted
//   - TestStaleWatch_ExponentialBackoff          — second emit at 2M, third at 4M
//   - TestStaleWatch_NoEmitAfterRunDeregistered  — run removed from registry → pruned, no emit
//   - TestStaleWatch_BeadIDFromRegistry          — bead_id populated from RunHandle
//   - TestStaleWatch_LastEventTypeTracked        — observer updates lastEventType on each event
//   - TestStaleWatch_PayloadValid                — emitted RunStalePayload passes Valid()
//
// Bead ref: hk-wkzlc.

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
// Test helpers (prefix: staleFixture)
// ─────────────────────────────────────────────────────────────────────────────

// staleFixtureBus builds a sealed in-memory bus that records emitted run_stale
// events into a collector.
type staleFixtureBus struct {
	bus     eventbus.EventBus
	mu      sync.Mutex
	emitted []core.RunStalePayload
}

// staleFixtureNewBus creates a bus, subscribes a run_stale collector, and seals
// the bus. All events emitted after the seal are captured into the collector.
func staleFixtureNewBus(t *testing.T) *staleFixtureBus {
	t.Helper()
	sfb := &staleFixtureBus{}
	sfb.bus = eventbus.NewBusImpl()
	// Subscribe an observer that captures run_stale payloads.
	sub := core.Subscription{
		ConsumerID:    "stale-test-collector",
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
			sfb.mu.Lock()
			sfb.emitted = append(sfb.emitted, pl)
			sfb.mu.Unlock()
			return nil
		},
	}
	if _, err := sfb.bus.Subscribe(sub); err != nil {
		t.Fatalf("staleFixtureNewBus: Subscribe: %v", err)
	}
	if err := sfb.bus.Seal(); err != nil {
		t.Fatalf("staleFixtureNewBus: Seal: %v", err)
	}
	return sfb
}

func (sfb *staleFixtureBus) collected() []core.RunStalePayload {
	sfb.mu.Lock()
	defer sfb.mu.Unlock()
	out := make([]core.RunStalePayload, len(sfb.emitted))
	copy(out, sfb.emitted)
	return out
}

// staleFixtureNewRunID returns a UUIDv7-based RunID.
func staleFixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("staleFixtureNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestStaleWatch_NoEmitBelowThreshold verifies that no run_stale event is emitted
// when the run's age is strictly less than staleAfter.
func TestStaleWatch_NoEmitBelowThreshold(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Register the run; started 5 minutes ago; staleAfter is 10 min.
	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-test1",
		StartedAt: startedAt,
	})

	now := startedAt.Add(5 * time.Minute)

	sfb := staleFixtureNewBus(t)
	// For this test we need a fresh unsealed bus to subscribe before Seal —
	// the collector is already sealed above, so we build a second bus
	// specifically for the watcher subscription.
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   10 * time.Minute,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return now },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Manually trigger a scan.
	daemon.ExportedStalewatchScan(w, context.Background())

	got := sfb.collected()
	if len(got) != 0 {
		t.Errorf("expected 0 run_stale events, got %d", len(got))
	}
}

// TestStaleWatch_EmitAtThreshold verifies that run_stale is emitted when the
// run's age equals or exceeds staleAfter.
func TestStaleWatch_EmitAtThreshold(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-test2",
		StartedAt: startedAt,
	})

	// exactly at threshold
	now := startedAt.Add(10 * time.Minute)

	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   10 * time.Minute,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return now },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	daemon.ExportedStalewatchScan(w, context.Background())

	// Wait briefly for async observer dispatch.
	time.Sleep(50 * time.Millisecond)
	got := sfb.collected()
	if len(got) != 1 {
		t.Fatalf("expected 1 run_stale event, got %d", len(got))
	}
	pl := got[0]
	if pl.RunID != runID.String() {
		t.Errorf("run_id mismatch: got %s want %s", pl.RunID, runID.String())
	}
	if pl.BeadID != "hk-test2" {
		t.Errorf("bead_id mismatch: got %s want hk-test2", pl.BeadID)
	}
	if pl.EmitCount != 1 {
		t.Errorf("emit_count: got %d want 1", pl.EmitCount)
	}
	if pl.AgeSeconds < 600 {
		t.Errorf("age_seconds: got %d want ≥600", pl.AgeSeconds)
	}
}

// TestStaleWatch_ExponentialBackoff verifies that after the first run_stale
// emission, re-emission happens at 2M, then 4M (exponential doubling).
func TestStaleWatch_ExponentialBackoff(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testback",
		StartedAt: startedAt,
	})

	staleAfter := 10 * time.Minute
	// Controllable clock: starts at threshold.
	clockMu := sync.Mutex{}
	clockVal := startedAt.Add(staleAfter)
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockVal
	}
	advanceClock := func(d time.Duration) {
		clockMu.Lock()
		clockVal = clockVal.Add(d)
		clockMu.Unlock()
	}

	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   staleAfter,
		ScanInterval: time.Hour,
		Now:          nowFn,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	// First scan at M → emit 1.
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("after first scan: expected 1 event, got %d", n)
	}

	// Advance to M + 1 min (< 2M = 20 min) → no new emit.
	advanceClock(1 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("at M+1min: expected still 1 event, got %d", n)
	}

	// Advance to M + 10 min (= 2M total from startedAt) → emit 2.
	advanceClock(9 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	evts := sfb.collected()
	if len(evts) != 2 {
		t.Fatalf("at 2M: expected 2 events, got %d", len(evts))
	}
	if evts[1].EmitCount != 2 {
		t.Errorf("second event emit_count: got %d want 2", evts[1].EmitCount)
	}

	// Advance by another 19 min (total age = 39 min; 2M=20min → next at 4M=40min)
	// → still 2 events (just under 4M threshold).
	advanceClock(19 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 2 {
		t.Fatalf("at 3.9M: expected still 2 events, got %d", n)
	}

	// Advance by 1 more min (= 40 min from startedAt; total age = 4M) → emit 3.
	advanceClock(1 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	evts = sfb.collected()
	if len(evts) != 3 {
		t.Fatalf("at 4M: expected 3 events, got %d", len(evts))
	}
	if evts[2].EmitCount != 3 {
		t.Errorf("third event emit_count: got %d want 3", evts[2].EmitCount)
	}
}

// TestStaleWatch_NoEmitAfterRunDeregistered verifies that once a run is removed
// from the registry, its state is pruned and no new run_stale events fire.
func TestStaleWatch_NoEmitAfterRunDeregistered(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testprune",
		StartedAt: startedAt,
	})

	staleAfter := 10 * time.Minute
	now := startedAt.Add(staleAfter) // at threshold

	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   staleAfter,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return now },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	// First scan → emit 1.
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("expected 1 event, got %d", n)
	}

	// Deregister the run.
	reg.Unregister(runID)

	// Advance clock far past any backoff window.
	now = startedAt.Add(2 * time.Hour)

	// Second scan: run is gone from registry → state is pruned → no new emit.
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Errorf("after deregister: expected still 1 event, got %d", n)
	}
}

// TestStaleWatch_LastEventTypeTracked verifies that when the bus delivers an
// event for a run, the watcher's last_event_type reflects that event's type.
func TestStaleWatch_LastEventTypeTracked(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testtrack",
		StartedAt: startedAt,
	})

	staleAfter := 10 * time.Minute

	// Build the watcher on an unsealed bus; subscribe, then seal.
	unsealedForWatcher := eventbus.NewBusImpl()
	sfb := &staleFixtureBus{}
	sfb.bus = unsealedForWatcher

	// Subscribe the collector (for run_stale) on the same bus.
	collectorSub := core.Subscription{
		ConsumerID:    "stale-test-collector-track",
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
			sfb.mu.Lock()
			sfb.emitted = append(sfb.emitted, pl)
			sfb.mu.Unlock()
			return nil
		},
	}
	if _, err := unsealedForWatcher.Subscribe(collectorSub); err != nil {
		t.Fatalf("Subscribe collector: %v", err)
	}

	// clockNow drives what Now() returns. Starts just before threshold so that
	// the heartbeat arrives while clock < staleAfter. After the heartbeat,
	// clock is advanced past staleAfter from the heartbeat time so the run is
	// considered stale (the heartbeat resets the reference, and 10+ min later
	// the run is stale again). Closed over by nowFn.
	clockNow := startedAt.Add(1 * time.Minute) // well before threshold
	nowFn := func() time.Time { return clockNow }
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealedForWatcher,
		Emitter:      unsealedForWatcher,
		Registry:     reg,
		StaleAfter:   staleAfter,
		ScanInterval: time.Hour,
		Now:          nowFn,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe watcher: %v", err)
	}
	if err := unsealedForWatcher.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	// Deliver a synthetic agent_heartbeat event for the run so the watcher
	// records the last event type for this run.
	if err := unsealedForWatcher.EmitWithRunID(ctx, runID, core.EventTypeAgentHeartbeat, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EmitWithRunID heartbeat: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Advance clock: heartbeat was recorded at startedAt+1min (the clockNow
	// when the observer ran). Now advance to 1min + staleAfter + 1s to make
	// the age cross the threshold.
	clockNow = startedAt.Add(1*time.Minute + staleAfter + time.Second)

	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)

	evts := sfb.collected()
	if len(evts) != 1 {
		t.Fatalf("expected 1 run_stale event, got %d", len(evts))
	}
	if evts[0].LastEventType != string(core.EventTypeAgentHeartbeat) {
		t.Errorf("last_event_type: got %q want %q",
			evts[0].LastEventType, string(core.EventTypeAgentHeartbeat))
	}
}

// TestBeadStaleAfter_LabelParsing verifies the beadStaleAfter helper parses the
// "stale_after=<seconds>" label correctly across valid, invalid, and absent
// cases.
func TestBeadStaleAfter_LabelParsing(t *testing.T) {
	def := 10 * time.Minute

	cases := []struct {
		name   string
		labels []string
		want   time.Duration
	}{
		{"no labels", nil, def},
		{"empty labels", []string{}, def},
		{"unrelated label", []string{"workflow:default", "priority:high"}, def},
		{"valid override 1200s", []string{"stale_after=1200"}, 1200 * time.Second},
		{"valid override 600s", []string{"stale_after=600"}, 600 * time.Second},
		{"override with other labels", []string{"workflow:default", "stale_after=1800", "foo"}, 1800 * time.Second},
		{"zero value falls back", []string{"stale_after=0"}, def},
		{"negative falls back", []string{"stale_after=-1"}, def},
		{"non-numeric falls back", []string{"stale_after=abc"}, def},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := daemon.ExportedBeadStaleAfter(tc.labels, def)
			if got != tc.want {
				t.Errorf("beadStaleAfter(%v, %v) = %v, want %v", tc.labels, def, got, tc.want)
			}
		})
	}
}

// TestStaleWatch_PerBeadLabelOverride verifies that a run registered with a
// "stale_after=<seconds>" label uses the label value as its stale threshold
// instead of the watcher's default.
func TestStaleWatch_PerBeadLabelOverride(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Register the run with a 20-minute per-bead override; watcher default is 10 min.
	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testlabel",
		Labels:    []string{"stale_after=1200"}, // 1200s = 20 min
		StartedAt: startedAt,
	})

	// Advance clock to 10 min (past default, but not yet past per-bead override).
	now := startedAt.Add(10 * time.Minute)

	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   10 * time.Minute,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return now },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// At 10 min the per-bead threshold (20 min) is not crossed → no emit.
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 0 {
		t.Fatalf("at default threshold (10min): expected 0 events, got %d (per-bead override not applied)", n)
	}

	// Advance to 20 min — crosses the per-bead override → emit 1.
	now = startedAt.Add(20 * time.Minute)
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)
	evts := sfb.collected()
	if len(evts) != 1 {
		t.Fatalf("at per-bead threshold (20min): expected 1 event, got %d", len(evts))
	}
	if evts[0].BeadID != "hk-testlabel" {
		t.Errorf("bead_id: got %s want hk-testlabel", evts[0].BeadID)
	}
}

// TestStaleWatch_ReviewerLaunchNodeGating verifies that when the last event for
// a run is reviewer_launched, the stale watcher applies the
// ReviewerLaunchStaleAfter floor rather than the default StaleAfter, preventing
// false-positive run_stale events during normal reviewer execution windows
// (logmine F38, hk-0z2).
func TestStaleWatch_ReviewerLaunchNodeGating(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testrevgating",
		StartedAt: startedAt,
	})

	defaultStaleAfter := 10 * time.Minute
	reviewerLaunchStaleAfter := 30 * time.Minute

	// Controllable clock: starts at startedAt.
	clockVal := startedAt
	nowFn := func() time.Time { return clockVal }

	sfb := staleFixtureNewBus(t)
	// Build a second bus that the watcher subscribes to (and the reviewer_launched
	// event is emitted on). The watcher emits run_stale to sfb.bus.
	unsealedForWatcher := eventbus.NewBusImpl()

	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus:             unsealedForWatcher,
		Emitter:                  sfb.bus,
		Registry:                 reg,
		StaleAfter:               defaultStaleAfter,
		ReviewerLaunchStaleAfter: reviewerLaunchStaleAfter,
		ScanInterval:             time.Hour,
		Now:                      nowFn,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealedForWatcher.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	// Deliver reviewer_launched at t=0. The watcher observes it and records
	// lastEventType = "reviewer_launched".
	if err := unsealedForWatcher.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EmitWithRunID reviewer_launched: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Advance to just past the default threshold (10 min + 1 s).
	// Without the gate this would fire run_stale; with the gate it must not.
	clockVal = startedAt.Add(defaultStaleAfter + time.Second)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 0 {
		t.Fatalf("at default threshold+1s after reviewer_launched: expected 0 run_stale events (gate suppressed), got %d", n)
	}

	// Advance to just past the reviewer-launch floor (30 min + 1 s) — run_stale
	// must now fire because the reviewer has been silent for the full floor window.
	clockVal = startedAt.Add(reviewerLaunchStaleAfter + time.Second)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	evts := sfb.collected()
	if len(evts) != 1 {
		t.Fatalf("at reviewer-launch floor+1s: expected 1 run_stale event, got %d", len(evts))
	}
	if evts[0].BeadID != "hk-testrevgating" {
		t.Errorf("bead_id: got %s want hk-testrevgating", evts[0].BeadID)
	}
	if evts[0].LastEventType != string(core.EventTypeReviewerLaunched) {
		t.Errorf("last_event_type: got %q want %q", evts[0].LastEventType, string(core.EventTypeReviewerLaunched))
	}
}

// TestStaleWatch_ReviewerLaunchGateDoesNotSuppressHighBackoff verifies that
// once the exponential backoff has grown beyond ReviewerLaunchStaleAfter, the
// backoff value (not the gate floor) governs subsequent re-emissions.
func TestStaleWatch_ReviewerLaunchGateDoesNotSuppressHighBackoff(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testrevbackoff",
		StartedAt: startedAt,
	})

	defaultStaleAfter := 10 * time.Minute
	reviewerLaunchStaleAfter := 30 * time.Minute

	clockVal := startedAt
	nowFn := func() time.Time { return clockVal }

	sfb := staleFixtureNewBus(t)
	unsealedForWatcher := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus:             unsealedForWatcher,
		Emitter:                  sfb.bus,
		Registry:                 reg,
		StaleAfter:               defaultStaleAfter,
		ReviewerLaunchStaleAfter: reviewerLaunchStaleAfter,
		ScanInterval:             time.Hour,
		Now:                      nowFn,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealedForWatcher.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	// Deliver reviewer_launched at t=0.
	if err := unsealedForWatcher.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EmitWithRunID reviewer_launched: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// First emission at the reviewer-launch floor (30 min).
	clockVal = startedAt.Add(reviewerLaunchStaleAfter)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("first emit: expected 1, got %d", n)
	}
	// After first emit, nextEmitAfter is set to effectiveThreshold*2 = 30*2 = 60 min.
	// Because the gate raised the base from 10 min to 30 min, the doubling
	// now uses the gate floor so the backoff schedule is: 30, 60, 120, ...
	// Second emission fires when age from startedAt >= 30 + 60 = 90 min.
	// (age is measured from lastEventAt = startedAt+0, so threshold is 60 min
	//  but age is already 30 min; we need age to reach 60 min, i.e. clockVal = startedAt+60 min.)
	// Actually: age = clockVal - startedAt; nextEmitAfter = 60 min; fires when age >= 60 min.
	clockVal = startedAt.Add(59 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("just under second emit (59min): expected 1, got %d", n)
	}
	clockVal = startedAt.Add(60*time.Minute + time.Second)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 2 {
		t.Fatalf("second emit (60min+1s): expected 2, got %d", n)
	}
}

// TestStaleWatch_PayloadValid verifies that the emitted RunStalePayload passes
// its own Valid() check.
func TestStaleWatch_PayloadValid(t *testing.T) {
	reg := daemon.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &daemon.RunHandle{
		BeadID:    "hk-testvalid",
		StartedAt: startedAt,
	})

	staleAfter := 10 * time.Minute
	nowVal := startedAt.Add(staleAfter)

	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus: unsealed,
		Emitter:      sfb.bus,
		Registry:     reg,
		StaleAfter:   staleAfter,
		ScanInterval: time.Hour,
		Now:          func() time.Time { return nowVal },
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	evts := sfb.collected()
	if len(evts) != 1 {
		t.Fatalf("expected 1 run_stale event, got %d", len(evts))
	}
	if !evts[0].Valid() {
		t.Errorf("RunStalePayload.Valid() returned false: %+v", evts[0])
	}
}
