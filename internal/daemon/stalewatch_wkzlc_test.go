package daemon_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

type staleFixtureBus struct {
	bus     eventbus.EventBus
	mu      sync.Mutex
	emitted []core.RunStalePayload
}

func staleFixtureNewBus(t *testing.T) *staleFixtureBus {
	t.Helper()
	sfb := &staleFixtureBus{}
	sfb.bus = eventbus.NewBusImpl()
	sub := core.Subscription{
		ConsumerID:    "stale-test-collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if evt.Type != core.EventTypeRunStale {
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

func staleFixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("staleFixtureNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// TestStaleWatch_NoEmitBelowThreshold verifies that no run_stale event is emitted
// when the run's age is strictly less than staleAfter.
func TestStaleWatch_NoEmitBelowThreshold(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-test1",
		StartedAt: startedAt,
	})

	now := startedAt.Add(5 * time.Minute)

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

	got := sfb.collected()
	if len(got) != 0 {
		t.Errorf("expected 0 run_stale events, got %d", len(got))
	}
}

// TestStaleWatch_EmitAtThreshold verifies that run_stale is emitted when the
// run's age equals or exceeds staleAfter.
func TestStaleWatch_EmitAtThreshold(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-test2",
		StartedAt: startedAt,
	})

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
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-testback",
		StartedAt: startedAt,
	})

	staleAfter := 10 * time.Minute
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
		// hk-mdus1: isolate the run_stale re-emission SCHEDULE (M, 2M, 4M …)
		// under test from the force-reap watchdog. This fixture keeps a bare
		// handle registered indefinitely (no goroutine to unwind it), so with
		// the production 90 s grace the watchdog would force-Unregister it ~90 s
		// after the first stale (which triggers the kill-consumer cancel) and
		// truncate the backoff. A huge grace keeps the emission cadence
		// observable; force-reap behavior itself is covered by the dedicated
		// stalewatch_forceReap_hkmdus1_test.go suite.
		ForceReapGrace: 24 * time.Hour,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("after first scan: expected 1 event, got %d", n)
	}

	advanceClock(1 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("at M+1min: expected still 1 event, got %d", n)
	}

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

	advanceClock(19 * time.Minute)
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 2 {
		t.Fatalf("at 3.9M: expected still 2 events, got %d", n)
	}

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
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
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

	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("expected 1 event, got %d", n)
	}

	reg.Unregister(runID)

	now = startedAt.Add(2 * time.Hour)

	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Errorf("after deregister: expected still 1 event, got %d", n)
	}
}

// TestStaleWatch_LastEventTypeTracked verifies that when the bus delivers an
// event for a run, the watcher's last_event_type reflects that event's type.
func TestStaleWatch_LastEventTypeTracked(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-testtrack",
		StartedAt: startedAt,
	})

	staleAfter := 10 * time.Minute

	unsealedForWatcher := eventbus.NewBusImpl()
	sfb := &staleFixtureBus{}
	sfb.bus = unsealedForWatcher

	collectorSub := core.Subscription{
		ConsumerID:    "stale-test-collector-track",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if evt.Type != core.EventTypeRunStale {
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

	var clockMu sync.Mutex
	clockNow := startedAt.Add(1 * time.Minute) // well before threshold
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockNow
	}
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

	if err := unsealedForWatcher.EmitWithRunID(ctx, runID, core.EventTypeAgentHeartbeat, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EmitWithRunID heartbeat: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	clockMu.Lock()
	clockNow = startedAt.Add(1*time.Minute + staleAfter + time.Second)
	clockMu.Unlock()

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
		// colon-form (valid label syntax: alnum/hyphen/underscore/colon)
		{"colon form 120s", []string{"stale_after:120"}, 120 * time.Second},
		{"colon form 1800s", []string{"stale_after:1800"}, 1800 * time.Second},
		{"colon form with other labels", []string{"workflow:default", "stale_after:900", "foo"}, 900 * time.Second},
		{"colon form zero falls back", []string{"stale_after:0"}, def},
		{"colon form negative falls back", []string{"stale_after:-1"}, def},
		{"colon form non-numeric falls back", []string{"stale_after:abc"}, def},
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
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-testlabel",
		Labels:    []string{"stale_after=1200"}, // 1200s = 20 min
		StartedAt: startedAt,
	})

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
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 0 {
		t.Fatalf("at default threshold (10min): expected 0 events, got %d (per-bead override not applied)", n)
	}

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

// TestStaleWatch_PerBeadColonFormLabelOverride verifies that a run registered
// with a "stale_after:<seconds>" label (colon form — the only form accepted by
// beads label validation) uses that value as its stale threshold.
func TestStaleWatch_PerBeadColonFormLabelOverride(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-testcolonlabel",
		Labels:    []string{"stale_after:120"},
		StartedAt: startedAt,
	})

	now := startedAt.Add(1 * time.Minute) // before per-bead threshold

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
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 0 {
		t.Fatalf("before per-bead threshold: expected 0 events, got %d", n)
	}

	now = startedAt.Add(121 * time.Second)
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)
	evts := sfb.collected()
	if len(evts) != 1 {
		t.Fatalf("at colon-form threshold (121s): expected 1 event, got %d", len(evts))
	}
	if evts[0].BeadID != "hk-testcolonlabel" {
		t.Errorf("bead_id: got %s want hk-testcolonlabel", evts[0].BeadID)
	}
}

// TestStaleWatch_ReviewerLaunchNodeGating verifies that when the last event for
// a run is reviewer_launched, the stale watcher applies the
// ReviewerLaunchStaleAfter floor rather than the default StaleAfter, preventing
// false-positive run_stale events during normal reviewer execution windows
// (logmine F38, hk-0z2).
func TestStaleWatch_ReviewerLaunchNodeGating(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-testrevgating",
		StartedAt: startedAt,
	})

	defaultStaleAfter := 10 * time.Minute
	reviewerLaunchStaleAfter := 30 * time.Minute

	var clockMu sync.Mutex
	clockVal := startedAt
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockVal
	}

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

	if err := unsealedForWatcher.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EmitWithRunID reviewer_launched: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	clockMu.Lock()
	clockVal = startedAt.Add(defaultStaleAfter + time.Second)
	clockMu.Unlock()
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 0 {
		t.Fatalf("at default threshold+1s after reviewer_launched: expected 0 run_stale events (gate suppressed), got %d", n)
	}

	clockMu.Lock()
	clockVal = startedAt.Add(reviewerLaunchStaleAfter + time.Second)
	clockMu.Unlock()
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
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-testrevbackoff",
		StartedAt: startedAt,
	})

	defaultStaleAfter := 10 * time.Minute
	reviewerLaunchStaleAfter := 30 * time.Minute

	var clockMu sync.Mutex
	clockVal := startedAt
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockVal
	}

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
		// hk-mdus1: isolate the re-emission schedule from the force-reap
		// watchdog (see the note in TestStaleWatch_ExponentialBackoff).
		ForceReapGrace: 24 * time.Hour,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := unsealedForWatcher.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ctx := context.Background()

	if err := unsealedForWatcher.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EmitWithRunID reviewer_launched: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	clockMu.Lock()
	clockVal = startedAt.Add(reviewerLaunchStaleAfter)
	clockMu.Unlock()
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("first emit: expected 1, got %d", n)
	}
	clockMu.Lock()
	clockVal = startedAt.Add(59 * time.Minute)
	clockMu.Unlock()
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 1 {
		t.Fatalf("just under second emit (59min): expected 1, got %d", n)
	}
	clockMu.Lock()
	clockVal = startedAt.Add(60*time.Minute + time.Second)
	clockMu.Unlock()
	daemon.ExportedStalewatchScan(w, ctx)
	time.Sleep(50 * time.Millisecond)
	if n := len(sfb.collected()); n != 2 {
		t.Fatalf("second emit (60min+1s): expected 2, got %d", n)
	}
}

// TestStaleWatch_PayloadValid verifies that the emitted RunStalePayload passes
// its own Valid() check.
func TestStaleWatch_PayloadValid(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
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
