package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runregistry"
)

func stallFeedRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("stallFeedRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

type stallFeedFixture struct {
	jsonlPath string
	writer    *eventbus.JSONLWriter
	bus       eventbus.EventBus
	registry  *runregistry.RunRegistry
	watcher   *StaleWatcher
	feed      *runloop.StallFeed
	closed    bool

	// mu guards now. The virtual clock is read by the bus observer goroutine
	// through the watcher's Now hook and written by the test goroutine between
	// emits, so an unguarded field is a race the -race tier catches.
	mu  sync.Mutex
	now time.Time
}

func (f *stallFeedFixture) setNow(at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = at
}

func (f *stallFeedFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *stallFeedFixture) flush(t *testing.T) {
	t.Helper()
	if f.closed {
		return
	}
	f.closed = true
	if err := f.writer.Close(); err != nil {
		t.Fatalf("stallFeedFixture.flush: close writer: %v", err)
	}
}

func stallFeedNewFixture(t *testing.T, now time.Time, runMaxAge time.Duration) *stallFeedFixture {
	t.Helper()
	f := &stallFeedFixture{
		jsonlPath: filepath.Join(t.TempDir(), "events.jsonl"),
		registry:  runregistry.NewRunRegistry(),
		feed:      runloop.NewStallFeed(),
		now:       now,
	}
	w, err := eventbus.OpenJSONLWriter(f.jsonlPath)
	if err != nil {
		t.Fatalf("stallFeedNewFixture: open JSONL writer: %v", err)
	}
	f.writer = w
	t.Cleanup(func() {
		if !f.closed {
			f.closed = true
			_ = w.Close()
		}
	})
	f.bus = eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), w)

	f.watcher = NewStaleWatcher(StaleWatcherConfig{
		SubscribeBus: f.bus,
		Emitter:      f.bus,
		Registry:     f.registry,
		Now:          f.clock,
		StallFeed:    f.feed,
		// The age ceiling sits above every scenario in this file except the one
		// that tests the ceiling itself. The age backstop is an absolute budget
		// rule: it fires on ANY run past the ceiling, working or not, so leaving
		// it low would mask whether the other two signatures tell a wedged run
		// apart from a working one.
		RunMaxAge:           runMaxAge,
		RunSilenceStall:     22 * time.Minute,
		ReviewFinalizeStall: 10 * time.Minute,
	})
	if err := f.watcher.Subscribe(); err != nil {
		t.Fatalf("stallFeedNewFixture: Subscribe: %v", err)
	}
	if err := f.bus.Seal(); err != nil {
		t.Fatalf("stallFeedNewFixture: Seal: %v", err)
	}
	return f
}

func (f *stallFeedFixture) register(t *testing.T, beadID string, startedAt time.Time) core.RunID {
	t.Helper()
	runID := stallFeedRunID(t)
	f.registry.Register(runID, &runregistry.RunHandle{
		BeadID:    core.BeadID(beadID),
		QueueName: "lane-a",
		StartedAt: startedAt,
	})
	return runID
}

func (f *stallFeedFixture) beat(t *testing.T, runID core.RunID) {
	t.Helper()
	f.emit(t, runID, core.EventTypeAgentHeartbeat)
}

func (f *stallFeedFixture) emit(t *testing.T, runID core.RunID, evType core.EventType) {
	t.Helper()
	if err := f.bus.EmitWithRunID(context.Background(), runID, evType, nil); err != nil {
		t.Fatalf("stallFeedFixture.emit %s: %v", evType, err)
	}
	f.awaitObserved(t, runID)
}

func (f *stallFeedFixture) awaitObserved(t *testing.T, runID core.RunID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.watcher.mu.Lock()
		st, ok := f.watcher.states[runID]
		seen := ok && !st.lastLivenessAt.Before(f.clock())
		f.watcher.mu.Unlock()
		if seen {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("awaitObserved: the watcher never observed a beat for run %s", runID)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *stallFeedFixture) stalls(t *testing.T, active ...core.RunID) []DashStall {
	t.Helper()
	f.flush(t)
	ids := make(map[string]bool, len(active))
	for _, id := range active {
		ids[id.String()] = true
	}
	b := &DashboardBuilder{eventsPath: f.jsonlPath}
	return b.readActiveStalls(time.Now().UTC(), ids)
}

// TestStallFeeder_AHungRunShowsAsHungOnTheDashboardAndAHealthyOneDoesNot is the
// acceptance test for the reporting half.
//
// It reads the panel through readActiveStalls — the function the dashboard
// itself calls — so it fails if the feeder emits a stall_detected the panel
// cannot join to a live run, and it fails if the feeder reports a run that is
// working.
func TestStallFeeder_AHungRunShowsAsHungOnTheDashboardAndAHealthyOneDoesNot(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)

	hung := f.register(t, "hk-hung", start)
	healthy := f.register(t, "hk-healthy", start)

	for elapsed := 5 * time.Minute; elapsed <= 3*time.Hour; elapsed += 5 * time.Minute {
		f.setNow(start.Add(elapsed))
		f.beat(t, healthy)
	}
	f.setNow(start.Add(3 * time.Hour))
	f.beat(t, healthy)

	f.watcher.scan(context.Background())

	got := f.stalls(t, hung, healthy)

	var sawHung, sawHealthy bool
	for _, s := range got {
		switch s.RunID {
		case hung.String():
			sawHung = true
			if s.BeadID != "hk-hung" {
				t.Errorf("stall for the hung run names bead %q, want hk-hung", s.BeadID)
			}
			if s.ElapsedMs <= 0 {
				t.Errorf("stall for the hung run reports elapsed_ms %d, want a positive age", s.ElapsedMs)
			}
		case healthy.String():
			sawHealthy = true
		}
	}
	if !sawHung {
		t.Errorf("the dashboard active-stall panel is empty for a run that has been wedged for three hours; stalls=%+v", got)
	}
	if sawHealthy {
		t.Errorf("the dashboard reports a stall for a run that beat every five minutes for three hours; stalls=%+v", got)
	}
}

// TestStallFeeder_TheAgeBackstopFiresOnlyPastTheCeiling covers the third
// signature, which is a different kind of rule from the other two.
//
// heartbeat_gap and review_stall ask whether a run is moving. run_age asks
// whether it has spent its budget, and it fires on a run that is beating
// happily. That is intended — a run past its ceiling is over budget by
// definition — but it makes run_age the one signature that can reach a working
// agent, so the control here is a run that is equally busy and still INSIDE the
// ceiling.
func TestStallFeeder_TheAgeBackstopFiresOnlyPastTheCeiling(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 2*time.Hour)

	overBudget := f.register(t, "hk-over", start)
	inBudget := f.register(t, "hk-inside", start.Add(2*time.Hour))

	for elapsed := 5 * time.Minute; elapsed <= 3*time.Hour; elapsed += 5 * time.Minute {
		f.setNow(start.Add(elapsed))
		f.beat(t, overBudget)
		if !f.clock().Before(start.Add(2 * time.Hour)) {
			f.beat(t, inBudget)
		}
	}
	f.setNow(start.Add(3 * time.Hour))
	f.beat(t, overBudget)
	f.beat(t, inBudget)

	f.watcher.scan(context.Background())

	got := f.stalls(t, overBudget, inBudget)
	var sawOver, sawInside bool
	for _, s := range got {
		switch s.RunID {
		case overBudget.String():
			sawOver = true
			if s.Signature != string(core.StallSignatureRunAge) {
				t.Errorf("the over-budget run is reported as %q, want run_age", s.Signature)
			}
		case inBudget.String():
			sawInside = true
		}
	}
	if !sawOver {
		t.Errorf("a run one hour past its two-hour ceiling is not reported; stalls=%+v", got)
	}
	if sawInside {
		t.Errorf("a run inside its ceiling is reported as stalled; stalls=%+v", got)
	}
}

// TestStallFeeder_APerBeadCeilingKeepsALongRunAlive pins the escape hatch. Some
// beads legitimately outlive the fleet ceiling, and the answer this codebase
// already uses for its other three watchdogs is a per-bead label. Without it
// the age backstop reaps exactly the long jobs it should leave alone.
func TestStallFeeder_APerBeadCeilingKeepsALongRunAlive(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 2*time.Hour)

	runID := stallFeedRunID(t)
	f.registry.Register(runID, &runregistry.RunHandle{
		BeadID:    "hk-long",
		QueueName: "lane-a",
		StartedAt: start,
		Labels:    []string{"run_max_age=28800"},
	})

	for elapsed := 5 * time.Minute; elapsed <= 3*time.Hour; elapsed += 5 * time.Minute {
		f.setNow(start.Add(elapsed))
		f.beat(t, runID)
	}
	f.setNow(start.Add(3 * time.Hour))
	f.beat(t, runID)

	f.watcher.scan(context.Background())

	if got := f.stalls(t, runID); len(got) != 0 {
		t.Errorf("a bead carrying an eight-hour ceiling was reported stalled at three hours; stalls=%+v", got)
	}
}

// TestStallFeeder_AVerdictThatNeverFinalizesIsReported covers the review-stall
// signature: the reviewer decided, and the merge-and-close spine that should
// follow within minutes never ran.
func TestStallFeeder_AVerdictThatNeverFinalizesIsReported(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)
	wedged := f.register(t, "hk-wedged", start)

	f.setNow(start.Add(10 * time.Minute))
	f.emit(t, wedged, core.EventTypeLaunchInitiated)
	f.setNow(start.Add(20 * time.Minute))
	f.emit(t, wedged, core.EventTypeReviewerVerdict)

	for elapsed := 25 * time.Minute; elapsed <= 40*time.Minute; elapsed += 5 * time.Minute {
		f.setNow(start.Add(elapsed))
		f.beat(t, wedged)
	}

	f.watcher.scan(context.Background())

	var sawReviewStall bool
	got := f.stalls(t, wedged)
	for _, s := range got {
		if s.Signature == string(core.StallSignatureReviewStall) {
			sawReviewStall = true
		}
	}
	if !sawReviewStall {
		t.Errorf("a verdict that never finalized in twenty minutes is not reported as a review stall; stalls=%+v", got)
	}
}

// TestStallFeeder_AReworkingRunIsNotAReviewStall is the false-positive guard for
// that signature, and it is the sharper half.
//
// A graph run dispatches several nodes under ONE run id. When a reviewer asks
// for changes, the graph re-launches the IMPLEMENTER on the same run — so a
// verdict is followed by more work, not by a terminal event, and it is followed
// by more work for as long as the rework takes. Read as a watermark, that run
// looks like a verdict that never finalized, and ten minutes after its first
// request-changes the watchdog kills an implementer that is doing exactly what
// the reviewer asked for.
func TestStallFeeder_AReworkingRunIsNotAReviewStall(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)

	reworking := f.register(t, "hk-rework", start)
	stallCh, release := f.feed.Register(reworking.String())
	defer release()

	f.setNow(start.Add(10 * time.Minute))
	f.emit(t, reworking, core.EventTypeLaunchInitiated)
	f.setNow(start.Add(20 * time.Minute))
	f.emit(t, reworking, core.EventTypeReviewerVerdict)

	f.setNow(start.Add(21 * time.Minute))
	f.emit(t, reworking, core.EventTypeLaunchInitiated)
	for elapsed := 26 * time.Minute; elapsed <= 60*time.Minute; elapsed += 5 * time.Minute {
		f.setNow(start.Add(elapsed))
		f.beat(t, reworking)
	}

	f.watcher.scan(context.Background())

	for _, s := range f.stalls(t, reworking) {
		if s.Signature == string(core.StallSignatureReviewStall) {
			t.Errorf("a run reworking after a request-changes verdict is reported as a review stall")
		}
	}
	select {
	case ev := <-stallCh:
		t.Errorf("a reworking implementer was fed %q, which kills it mid-rework", ev.Kind)
	default:
	}
}

// TestStallFeeder_ALaterNodeCanStillBeReported pins the other half of the same
// mechanism. The report is de-duplicated per run and signature so one wedged run
// cannot flood the log at the scan cadence, but a graph run reaches the launch
// once per node — so a set carried across nodes would mean that after the first
// node is stall-killed, no later node of that run could ever be.
func TestStallFeeder_ALaterNodeCanStillBeReported(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)
	runID := f.register(t, "hk-graph", start)

	f.setNow(start.Add(time.Minute))
	f.emit(t, runID, core.EventTypeLaunchInitiated)
	f.setNow(start.Add(time.Hour))
	f.watcher.scan(context.Background())

	f.setNow(start.Add(time.Hour + time.Minute))
	f.emit(t, runID, core.EventTypeLaunchInitiated)
	f.setNow(start.Add(2 * time.Hour))
	f.watcher.scan(context.Background())

	var gaps int
	got := f.stalls(t, runID)
	for _, s := range got {
		if s.Signature == string(core.StallSignatureHeartbeatGap) {
			gaps++
		}
	}
	if gaps < 2 {
		t.Errorf("a second silent node of the same run is not reported (%d heartbeat-gap reports); "+
			"after the first stall kill no later node of a graph run could ever be reported; stalls=%+v", gaps, got)
	}
}

// TestStallFeeder_TheWatchersOwnAlarmIsNotASignOfLife pins the trap that makes
// a watchdog silence itself.
//
// The watcher observes the bus with a wildcard subscription, so it sees its OWN
// run_stale and stall_detected events for the run they are about. If those
// count as the run being alive, the run's silence clock resets every time an
// alarm fires and the detector can never fire again — and because observers are
// dispatched off the emitting goroutine, whether it fires at all comes down to a
// race. This is not hypothetical: it is what the first build of this feeder did,
// and it showed up as a test that passed alone and failed in a full run.
func TestStallFeeder_TheWatchersOwnAlarmIsNotASignOfLife(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)
	hung := f.register(t, "hk-hung", start)

	f.setNow(start.Add(3 * time.Hour))

	if err := f.bus.EmitWithRunID(context.Background(), hung, core.EventTypeRunStale, nil); err != nil {
		t.Fatalf("emit run_stale: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.watcher.mu.Lock()
		st, ok := f.watcher.states[hung]
		seen := ok && !st.lastEventAt.Before(f.clock())
		f.watcher.mu.Unlock()
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the watcher never observed its own run_stale")
		}
		time.Sleep(time.Millisecond)
	}

	f.watcher.scan(context.Background())

	if got := f.stalls(t, hung); len(got) == 0 {
		t.Errorf("a run wedged for three hours is not reported after the watcher alarmed about it; " +
			"the watchdog treated its own alarm as a sign of life")
	}
}

// TestStallFeeder_TheFeederDoesNotReEmitTheSameStallOnEveryScan pins the
// de-duplication the payload doc requires. Without it the log floods at the
// scan cadence and the panel repeats one wedged run once per tick, which
// buries every other run on the same panel.
func TestStallFeeder_TheFeederDoesNotReEmitTheSameStallOnEveryScan(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)
	hung := f.register(t, "hk-hung", start)

	f.setNow(start.Add(3 * time.Hour))
	f.watcher.scan(context.Background())
	f.setNow(start.Add(3*time.Hour + time.Minute))
	f.watcher.scan(context.Background())
	f.setNow(start.Add(3*time.Hour + 2*time.Minute))
	f.watcher.scan(context.Background())

	got := f.stalls(t, hung)
	perSignature := make(map[string]int, len(got))
	for _, s := range got {
		perSignature[s.Signature]++
	}
	if len(perSignature) == 0 {
		t.Fatalf("no stall reported for a run wedged for three hours across three scans")
	}
	for sig, n := range perSignature {
		if n != 1 {
			t.Errorf("signature %q emitted %d times across three scans, want exactly 1", sig, n)
		}
	}
}

// TestStallFeeder_AHungRunsDispatchIsToldToKillTheAgentAndAHealthyOneIsNot is
// the acceptance test for the recovery half.
//
// internal/runexec stepDispatchWorking kills the agent on EvNoChangeTimeout or
// EvHeartbeatStale. This test asserts the feeder posts one of those two kinds
// on the hung run's dispatch feed, and posts nothing at all on the healthy
// run's feed. It reads the kind rather than "some event arrived", because only
// those two kinds reach the kill arm — every other kind is a no-op there.
func TestStallFeeder_AHungRunsDispatchIsToldToKillTheAgentAndAHealthyOneIsNot(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)

	hung := f.register(t, "hk-hung", start)
	healthy := f.register(t, "hk-healthy", start)

	hungCh, releaseHung := f.feed.Register(hung.String())
	defer releaseHung()
	healthyCh, releaseHealthy := f.feed.Register(healthy.String())
	defer releaseHealthy()

	for elapsed := 5 * time.Minute; elapsed <= 3*time.Hour; elapsed += 5 * time.Minute {
		f.setNow(start.Add(elapsed))
		f.beat(t, healthy)
	}
	f.setNow(start.Add(3 * time.Hour))
	f.beat(t, healthy)

	f.watcher.scan(context.Background())

	select {
	case ev := <-hungCh:
		if ev.Kind != runexec.EvNoChangeTimeout && ev.Kind != runexec.EvHeartbeatStale {
			t.Errorf("the hung run's dispatch was fed %q, which stepDispatchWorking ignores; "+
				"want no_change_timeout or heartbeat_stale", ev.Kind)
		}
		if ev.At.IsZero() {
			t.Errorf("the stall event carries no timestamp; the reactor reads no clock and needs a shell-stamped At")
		}
	default:
		t.Errorf("nothing was fed to the dispatch of a run wedged for three hours, so the kill arm never fires")
	}

	select {
	case ev := <-healthyCh:
		t.Errorf("a run that beat every five minutes for three hours was fed %q, which kills a working agent", ev.Kind)
	default:
	}
}

// TestStallFeeder_StallDetectedPayloadsAreWellFormed guards the join the panel
// depends on. readActiveStalls drops any payload whose Valid() is false and any
// payload whose run_id is not live, and it drops them SILENTLY — so a feeder
// that emits a malformed record looks exactly like a feeder that emits nothing.
func TestStallFeeder_StallDetectedPayloadsAreWellFormed(t *testing.T) {
	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	f := stallFeedNewFixture(t, start, 24*time.Hour)
	hung := f.register(t, "hk-hung", start)

	f.setNow(start.Add(3 * time.Hour))
	f.watcher.scan(context.Background())
	f.flush(t)

	var seen int
	var zero core.EventID
	for ev := range eventbus.ScanAfter(f.jsonlPath, zero) {
		if ev.Type != core.EventTypeStallDetected {
			continue
		}
		seen++
		if ev.RunID == nil {
			t.Errorf("stall_detected carries no run_id on the envelope, so the panel cannot join it to a run")
			continue
		}
		if ev.RunID.String() != hung.String() {
			t.Errorf("stall_detected names run %s, want %s", ev.RunID, hung)
		}
		var p core.StallDetectedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Errorf("stall_detected payload does not decode: %v", err)
			continue
		}
		if !p.Valid() {
			t.Errorf("stall_detected payload is not valid, so readActiveStalls drops it: %+v", p)
		}
	}
	if seen == 0 {
		t.Errorf("no stall_detected event was written for a run wedged for three hours")
	}
}
