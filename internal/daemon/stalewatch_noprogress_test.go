package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/runregistry"
)

type noProgressFixture struct {
	t   *testing.T
	bus eventbus.EventBus
	w   *daemon.StaleWatcher

	clockMu sync.Mutex
	now     time.Time

	emitMu  sync.Mutex
	emitted []core.RunStalePayload
}

func newNoProgressFixture(t *testing.T, reg *runregistry.RunRegistry, start time.Time, cfg daemon.StaleWatcherConfig) *noProgressFixture {
	t.Helper()
	f := &noProgressFixture{t: t, bus: eventbus.NewBusImpl(), now: start}

	sub := core.Subscription{
		ConsumerID:    "noprogress-collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if evt.Type != core.EventTypeRunStale {
				return nil
			}
			var pl core.RunStalePayload
			if err := json.Unmarshal(evt.Payload, &pl); err != nil {
				return fmt.Errorf("collector: unmarshal run_stale payload: %w", err)
			}
			f.emitMu.Lock()
			f.emitted = append(f.emitted, pl)
			f.emitMu.Unlock()
			return nil
		},
	}
	if _, err := f.bus.Subscribe(sub); err != nil {
		t.Fatalf("newNoProgressFixture: Subscribe collector: %v", err)
	}

	cfg.SubscribeBus = f.bus
	cfg.Emitter = f.bus
	cfg.Registry = reg
	cfg.ScanInterval = time.Hour // the tests drive scan() by hand
	cfg.Now = f.clock
	f.w = daemon.NewStaleWatcher(cfg)

	if err := f.w.Subscribe(); err != nil {
		t.Fatalf("newNoProgressFixture: Subscribe watcher: %v", err)
	}
	if err := f.bus.Seal(); err != nil {
		t.Fatalf("newNoProgressFixture: Seal: %v", err)
	}
	return f
}

func (f *noProgressFixture) clock() time.Time {
	f.clockMu.Lock()
	defer f.clockMu.Unlock()
	return f.now
}

func (f *noProgressFixture) setClock(at time.Time) {
	f.clockMu.Lock()
	f.now = at
	f.clockMu.Unlock()
}

func (f *noProgressFixture) emit(runID core.RunID, typ core.EventType) {
	f.t.Helper()
	if err := f.bus.EmitWithRunID(context.Background(), runID, typ, json.RawMessage(`{}`)); err != nil {
		f.t.Fatalf("emit %s: %v", typ, err)
	}
	time.Sleep(20 * time.Millisecond)
}

func (f *noProgressFixture) scan() {
	daemon.ExportedStalewatchScan(f.w, context.Background())
	time.Sleep(20 * time.Millisecond)
}

func (f *noProgressFixture) collected() []core.RunStalePayload {
	f.emitMu.Lock()
	defer f.emitMu.Unlock()
	out := make([]core.RunStalePayload, len(f.emitted))
	copy(out, f.emitted)
	return out
}

// TestStaleWatch_HeartbeatOnlyRunGoesStale is the regression test for the wedge.
//
// The run launches normally, reaches agent_ready, and then produces NOTHING but
// the daemon's own 5-minute beat. Against the pre-fix watcher this emits zero
// run_stale events for as long as the beat continues, because every beat resets
// the deadline the beat is measured against.
func TestStaleWatch_HeartbeatOnlyRunGoesStale(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	start := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-wedged",
		StartedAt: start,
	})

	f := newNoProgressFixture(t, reg, start, daemon.StaleWatcherConfig{
		StaleAfter:      10 * time.Minute,
		NoProgressAfter: 120 * time.Minute,
	})

	f.emit(runID, core.EventTypeRunStarted)
	f.emit(runID, core.EventTypeLaunchInitiated)
	f.emit(runID, core.EventTypeAgentReady)

	const beat = 5 * time.Minute
	for elapsed := beat; elapsed <= 119*time.Minute; elapsed += beat {
		f.setClock(start.Add(elapsed))
		f.emit(runID, core.EventTypeAgentHeartbeat)
		f.scan()
	}
	if got := len(f.collected()); got != 0 {
		t.Fatalf("run_stale before the no-progress window: got %d events, want 0", got)
	}

	f.setClock(start.Add(121 * time.Minute))
	f.emit(runID, core.EventTypeAgentHeartbeat)
	f.scan()

	got := f.collected()
	if len(got) != 1 {
		t.Fatalf("run_stale after 121 min of heartbeats and no progress: got %d events, want 1", len(got))
	}
	pl := got[0]
	if pl.BeadID != "hk-wedged" {
		t.Errorf("bead_id: got %q want %q", pl.BeadID, "hk-wedged")
	}
	if pl.NoProgressSeconds == nil {
		t.Fatalf("no_progress_seconds: nil; the alarm cannot say which clock fired")
	}
	if *pl.NoProgressSeconds < int64((121 * time.Minute).Seconds()) {
		t.Errorf("no_progress_seconds: got %d, want at least %d (time since agent_ready)",
			*pl.NoProgressSeconds, int64((121 * time.Minute).Seconds()))
	}
	if pl.AgeSeconds > int64((1 * time.Minute).Seconds()) {
		t.Errorf("age_seconds: got %d, want small — it must stay the seconds since the last event, which was the beat",
			pl.AgeSeconds)
	}
	if pl.AgeSeconds < 1 {
		t.Errorf("age_seconds: got %d, want at least 1 — core.RunStalePayload.Valid drops a payload with age_seconds <= 0", pl.AgeSeconds)
	}
	if pl.LastEventType != string(core.EventTypeAgentHeartbeat) {
		t.Errorf("last_event_type: got %q want %q", pl.LastEventType, string(core.EventTypeAgentHeartbeat))
	}
}

// TestStaleWatch_QuietButProgressingRunIsNotStale is the other half. A false
// stale on the QUIET clock is not a missed alarm — that clock's first run_stale
// arms the kill-consumer backstop and cancels the run — so a run that works
// quietly and then reports a phase must stay silent on both clocks.
func TestStaleWatch_QuietButProgressingRunIsNotStale(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	start := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-slow-but-working",
		StartedAt: start,
	})

	f := newNoProgressFixture(t, reg, start, daemon.StaleWatcherConfig{
		StaleAfter:      10 * time.Minute,
		NoProgressAfter: 120 * time.Minute,
	})

	f.emit(runID, core.EventTypeRunStarted)
	f.emit(runID, core.EventTypeLaunchInitiated)
	f.emit(runID, core.EventTypeAgentReady)

	const beat = 5 * time.Minute
	for elapsed := beat; elapsed <= 80*time.Minute; elapsed += beat {
		f.setClock(start.Add(elapsed))
		f.emit(runID, core.EventTypeAgentHeartbeat)
		f.scan()
	}

	f.setClock(start.Add(80 * time.Minute))
	f.emit(runID, core.EventTypeImplementerPhaseComplete)

	for elapsed := 85 * time.Minute; elapsed <= 190*time.Minute; elapsed += beat {
		f.setClock(start.Add(elapsed))
		f.emit(runID, core.EventTypeAgentHeartbeat)
		f.scan()
	}

	if got := f.collected(); len(got) != 0 {
		t.Fatalf("a run that reported a phase 80 min in must not be stale: got %d run_stale events (first age_seconds=%d)",
			len(got), got[0].AgeSeconds)
	}
}

// TestStaleWatch_NoProgressPerBeadLabelOverride verifies the escape hatch. A
// bead whose single phase legitimately outlives the default window widens its
// own window, the same way run_max_age and stale_after already work.
func TestStaleWatch_NoProgressPerBeadLabelOverride(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	start := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-long-phase",
		StartedAt: start,
		Labels:    []string{"workflow:default", "no_progress_after=21600"}, // 6 h
	})

	f := newNoProgressFixture(t, reg, start, daemon.StaleWatcherConfig{
		StaleAfter:      10 * time.Minute,
		NoProgressAfter: 120 * time.Minute,
	})

	f.emit(runID, core.EventTypeRunStarted)
	f.emit(runID, core.EventTypeLaunchInitiated)
	f.emit(runID, core.EventTypeAgentReady)

	const beat = 5 * time.Minute
	for elapsed := beat; elapsed <= 240*time.Minute; elapsed += beat {
		f.setClock(start.Add(elapsed))
		f.emit(runID, core.EventTypeAgentHeartbeat)
		f.scan()
	}

	if got := f.collected(); len(got) != 0 {
		t.Fatalf("no_progress_after=21600 must hold off the 120-min default: got %d run_stale events", len(got))
	}
}

// TestStaleWatch_NoProgressReportsButDoesNotCancel is the load-bearing boundary.
//
// The quiet window arms killConsumerBackstop on its first emission, which
// cancels the run and throws away whatever the agent had done. The no-progress
// clock deliberately does NOT, and that is what lets its window be sized
// approximately: the harnesses it uniquely covers (codex, pi) have too little
// recorded history to bound their legitimate tail, so an early fire must cost a
// noisy alarm and nothing more.
//
// Without this test the change is only pinned as an event count, and the risk
// it carries — a false stale killing a working agent — is never exercised.
func TestStaleWatch_NoProgressReportsButDoesNotCancel(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	start := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

	var cancelled atomic.Bool
	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-wedged-but-not-doomed",
		StartedAt: start,
		Cancel:    func() { cancelled.Store(true) },
	})

	f := newNoProgressFixture(t, reg, start, daemon.StaleWatcherConfig{
		StaleAfter:      10 * time.Minute,
		NoProgressAfter: 120 * time.Minute,
	})

	f.emit(runID, core.EventTypeRunStarted)
	f.emit(runID, core.EventTypeLaunchInitiated)
	f.emit(runID, core.EventTypeAgentReady)

	const beat = 5 * time.Minute
	for elapsed := beat; elapsed <= 130*time.Minute; elapsed += beat {
		f.setClock(start.Add(elapsed))
		f.emit(runID, core.EventTypeAgentHeartbeat)
		f.scan()
	}

	if got := len(f.collected()); got == 0 {
		t.Fatalf("the no-progress clock must still REPORT: got 0 run_stale events")
	}
	if cancelled.Load() {
		t.Errorf("the no-progress clock cancelled the run; only the quiet window may arm the kill-consumer backstop")
	}
}

// TestStaleWatch_QuietRunStillCancels is the other side of that boundary. The
// pre-existing behaviour must be untouched: a run that goes genuinely silent
// still trips the quiet window and still gets cancelled. Without this, scoping
// the backstop to `quiet` could disable it everywhere and no test would notice.
func TestStaleWatch_QuietRunStillCancels(t *testing.T) {
	reg := runregistry.NewRunRegistry()
	runID := staleFixtureNewRunID(t)
	start := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

	var cancelled atomic.Bool
	reg.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-gone-silent",
		StartedAt: start,
		Cancel:    func() { cancelled.Store(true) },
	})

	f := newNoProgressFixture(t, reg, start, daemon.StaleWatcherConfig{
		StaleAfter:      10 * time.Minute,
		NoProgressAfter: 120 * time.Minute,
	})

	f.emit(runID, core.EventTypeRunStarted)
	f.emit(runID, core.EventTypeLaunchInitiated)
	f.emit(runID, core.EventTypeAgentReady)

	f.setClock(start.Add(15 * time.Minute))
	f.scan()

	if got := f.collected(); len(got) != 1 {
		t.Fatalf("a silent run must still trip the quiet window: got %d run_stale events, want 1", len(got))
	}
	if !cancelled.Load() {
		t.Errorf("the quiet window must still arm the kill-consumer backstop; scoping it to the quiet clock disabled it")
	}
}

// TestStaleWatch_DaemonOwnEventsAreNotProgress pins the classification rule.
//
// Both of these are run-stamped and both are emitted by the DAEMON about the
// run rather than by the agent. no_progress_detected means the diff hash did not
// change between iterations — it is a statement that the run did NOT move, so
// counting it as motion lets the one event meaning "stuck" reset the stuck
// clock. implementer_resumed is the daemon's own resume nudge, emitted before
// the dispatch and not after any answer to it, so a loop that keeps resuming and
// keeps producing nothing would refresh the clock for as long as it ran.
func TestStaleWatch_DaemonOwnEventsAreNotProgress(t *testing.T) {
	for _, tc := range []struct {
		name   string
		evType core.EventType
	}{
		{"no_progress_detected", core.EventTypeNoProgressDetected},
		{"implementer_resumed", core.EventTypeImplementerResumed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := runregistry.NewRunRegistry()
			runID := staleFixtureNewRunID(t)
			start := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)

			reg.Register(runID, &runregistry.RunHandle{
				BeadID:    "hk-spinning",
				StartedAt: start,
			})

			f := newNoProgressFixture(t, reg, start, daemon.StaleWatcherConfig{
				StaleAfter:      10 * time.Minute,
				NoProgressAfter: 120 * time.Minute,
			})

			f.emit(runID, core.EventTypeRunStarted)
			f.emit(runID, core.EventTypeLaunchInitiated)
			f.emit(runID, core.EventTypeAgentReady)

			const beat = 5 * time.Minute
			for elapsed := beat; elapsed <= 130*time.Minute; elapsed += beat {
				f.setClock(start.Add(elapsed))
				f.emit(runID, core.EventTypeAgentHeartbeat)
				if elapsed%(15*time.Minute) == 0 {
					f.emit(runID, tc.evType)
				}
				f.scan()
			}

			if got := f.collected(); len(got) == 0 {
				t.Fatalf("%s refreshed the no-progress clock: got 0 run_stale events after 130 min with no agent motion", tc.name)
			}
		})
	}
}
