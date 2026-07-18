package daemon

// crewidlereap_hks2eac_test.go — unit tests for SD-3 (hk-s2eac): idle-
// completed-crew teardown.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/queue"
)

// fakeCrewQueues is a test double for crewQueueLookup keyed by queue name.
type fakeCrewQueues struct {
	mu   sync.Mutex
	byNm map[string]*queue.Queue
}

func newFakeCrewQueues() *fakeCrewQueues {
	return &fakeCrewQueues{byNm: make(map[string]*queue.Queue)}
}

func (f *fakeCrewQueues) QueueByName(name string) *queue.Queue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byNm[name]
}

func (f *fakeCrewQueues) set(name string, status queue.QueueStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byNm[name] = &queue.Queue{Name: name, Status: status}
}

func (f *fakeCrewQueues) clear(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byNm, name)
}

// fakeCrewStopper is a test double for crewStopper recording every call.
type fakeCrewStopper struct {
	mu      sync.Mutex
	stopped []string
}

func (f *fakeCrewStopper) HandleCrewStop(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var req CrewStopRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.stopped = append(f.stopped, req.Name)
	f.mu.Unlock()
	return json.Marshal(struct{}{})
}

func (f *fakeCrewStopper) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.stopped))
	copy(out, f.stopped)
	return out
}

// fakeClock lets tests advance time deterministically.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func listCrewsFrom(records []crew.Record) crewListFunc {
	return func(string) ([]crew.Record, error) {
		out := make([]crew.Record, len(records))
		copy(out, records)
		return out, nil
	}
}

// TestCrewIdleReaper_ReapsAfterGraceWindow: a crew whose bound queue reads
// QueueStatusCompleted for at least GraceAfter is torn down.
func TestCrewIdleReaper_ReapsAfterGraceWindow(t *testing.T) {
	queues := newFakeCrewQueues()
	queues.set("paul", queue.QueueStatusCompleted)
	stopper := &fakeCrewStopper{}
	clock := newFakeClock(time.Now())

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir: "/fake/project",
		Queues:     queues,
		Stopper:    stopper,
		GraceAfter: 5 * time.Minute,
		Now:        clock.Now,
		ListCrews:  listCrewsFrom([]crew.Record{{Name: "paul", Queue: "paul"}}),
	})

	ctx := context.Background()

	// First tick: candidacy is recorded, but grace has not elapsed — no reap.
	r.scan(ctx)
	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("expected no reap on first tick, got %v", got)
	}

	// Advance short of the grace window: still no reap.
	clock.Advance(4 * time.Minute)
	r.scan(ctx)
	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("expected no reap before grace elapses, got %v", got)
	}

	// Advance past the grace window: reap fires exactly once for "paul".
	clock.Advance(2 * time.Minute)
	r.scan(ctx)
	got := stopper.names()
	if len(got) != 1 || got[0] != "paul" {
		t.Fatalf("expected reap of crew %q, got %v", "paul", got)
	}

	// A further tick must not reap again (candidacy was cleared on reap).
	clock.Advance(10 * time.Minute)
	r.scan(ctx)
	if got := stopper.names(); len(got) != 1 {
		t.Fatalf("expected exactly one reap total, got %v", got)
	}
}

// TestCrewIdleReaper_ReTaskedCrewIsNotTornDown: if the crew's queue leaves
// QueueStatusCompleted (new work submitted/appended) before the grace window
// elapses, the crew must NOT be torn down — this is the anti-spawn-churn
// guarantee PLAN-v2.md's open question recommends.
func TestCrewIdleReaper_ReTaskedCrewIsNotTornDown(t *testing.T) {
	queues := newFakeCrewQueues()
	queues.set("paul", queue.QueueStatusCompleted)
	stopper := &fakeCrewStopper{}
	clock := newFakeClock(time.Now())

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir: "/fake/project",
		Queues:     queues,
		Stopper:    stopper,
		GraceAfter: 5 * time.Minute,
		Now:        clock.Now,
		ListCrews:  listCrewsFrom([]crew.Record{{Name: "paul", Queue: "paul"}}),
	})

	ctx := context.Background()
	r.scan(ctx) // candidacy recorded

	clock.Advance(2 * time.Minute)
	// Captain re-tasks the crew: the bound queue is re-armed with new work.
	queues.set("paul", queue.QueueStatusActive)
	r.scan(ctx)

	clock.Advance(10 * time.Minute)
	r.scan(ctx)

	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("expected re-tasked crew to survive, but it was reaped: %v", got)
	}
}

// TestCrewIdleReaper_CorrectlyIdleCrewNeverReaped: a crew with no bound queue
// loaded (never assigned work, or queue not yet observed) must never be
// treated as idle-completed — absence of a completion signal is not itself a
// completion signal (mirrors DESIGN.md Layer B's negative-idle guard).
func TestCrewIdleReaper_CorrectlyIdleCrewNeverReaped(t *testing.T) {
	queues := newFakeCrewQueues() // no queues registered at all
	stopper := &fakeCrewStopper{}
	clock := newFakeClock(time.Now())

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir: "/fake/project",
		Queues:     queues,
		Stopper:    stopper,
		GraceAfter: 5 * time.Minute,
		Now:        clock.Now,
		ListCrews:  listCrewsFrom([]crew.Record{{Name: "fresh-crew", Queue: "fresh-queue"}}),
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		clock.Advance(10 * time.Minute)
		r.scan(ctx)
	}

	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("expected a correctly-idle crew (no completion signal) to never be reaped, got %v", got)
	}
}

// TestCrewIdleReaper_ActiveQueueNeverReaped: a crew whose bound queue is
// actively dispatching is never a reap candidate.
func TestCrewIdleReaper_ActiveQueueNeverReaped(t *testing.T) {
	queues := newFakeCrewQueues()
	queues.set("shannon", queue.QueueStatusActive)
	stopper := &fakeCrewStopper{}
	clock := newFakeClock(time.Now())

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir: "/fake/project",
		Queues:     queues,
		Stopper:    stopper,
		GraceAfter: 1 * time.Minute,
		Now:        clock.Now,
		ListCrews:  listCrewsFrom([]crew.Record{{Name: "shannon", Queue: "shannon"}}),
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		clock.Advance(5 * time.Minute)
		r.scan(ctx)
	}

	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("expected active-queue crew to never be reaped, got %v", got)
	}
}

// TestCrewIdleReaper_PersistentOversightNeverReaped (GATE-0, hk-dy5gw): a
// persistent oversight role (admiral, watch — manifest lifecycle.persistent:
// true) never submits work, so its crew-start formality queue reads
// QueueStatusCompleted forever (ensureQueue, hk-vrnh3). Because PersistentType
// flags its type, the reaper must skip it across every grace window — otherwise
// admiral and watch are culled at exactly GraceAfter (5m0s), the same root
// cause as the watch self-terminate hk-kojyr. An ordinary bead-crew whose type
// is NOT persistent is still reaped.
func TestCrewIdleReaper_PersistentOversightNeverReaped(t *testing.T) {
	queues := newFakeCrewQueues()
	// All three queues read Completed (formality queue for oversight, drained
	// queue for the worker) — the persistent flag, not queue shape, decides.
	queues.set("admiral-q", queue.QueueStatusCompleted)
	queues.set("watch-q", queue.QueueStatusCompleted)
	queues.set("paul-q", queue.QueueStatusCompleted)
	stopper := &fakeCrewStopper{}
	clock := newFakeClock(time.Now())

	persistent := map[string]bool{"admiral": true, "watch": true}

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir:     "/fake/project",
		Queues:         queues,
		Stopper:        stopper,
		GraceAfter:     5 * time.Minute,
		Now:            clock.Now,
		PersistentType: func(typeName string) bool { return persistent[typeName] },
		ListCrews: listCrewsFrom([]crew.Record{
			{Name: "admiral", Type: "admiral", Queue: "admiral-q"},
			{Name: "watch", Type: "watch", Queue: "watch-q"},
			{Name: "paul", Type: "crew", Queue: "paul-q"},
		}),
	})

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		clock.Advance(5 * time.Minute)
		r.scan(ctx)
	}

	// The persistent oversight roles are NEVER reaped; the ordinary bead-crew IS.
	// (The fake stopper does not remove the reaped record — production does — so
	// paul may appear more than once; assert membership, not an exact count.)
	got := stopper.names()
	for _, name := range got {
		if name == "admiral" || name == "watch" {
			t.Fatalf("persistent oversight crew %q was reaped; got %v", name, got)
		}
	}
	if len(got) == 0 {
		t.Fatalf("expected the non-persistent bead-crew %q to be reaped, got none", "paul")
	}
}

// TestCrewIdleReaper_StartWatcher_Disabled_NeverReaps is the hk-98at0 E2E
// backfill for the SD-3 idle-reap DISABLE (operator directive 2026-07-18,
// hk-s2eac): an idle crew whose bound queue has DRAINED to QueueStatusCompleted
// must NOT be reaped, because StartWatcher never launches the sweep goroutine.
//
// This exercises the REAL StartWatcher entry point end-to-end (goroutine
// lifecycle included) rather than calling scan()/checkCrew() directly like the
// reaper-LOGIC tests above — so it is the guard that actually catches a
// regression that RE-ENABLES idle-reaping (e.g. reverting StartWatcher to start
// loop()). The parameters are deliberately aggressive: an ENABLED sweep would
// reap "paul" within ~1 grace window (GraceAfter 10ms, ScanInterval 2ms → a few
// ms), so a reap is observed almost immediately if the disable regresses; the
// 200ms observation window is >10× that, making the "no reap" assertion a
// large-margin presence-of-reap check, not a fragile wall-clock race.
//
// Assertion note: this proves the daemon-hosted signal the operator cares about
// (the crew is not torn down → its session/registry Record survive). A hermetic
// test cannot spawn a real claude --remote-control tmux pane (see
// scenario_captain_crew_e2e), so "session stays alive" is proven at its causal
// root: HandleCrewStop (which quits the pane and removes the registry Record) is
// never invoked — the fake Stopper records zero calls.
func TestCrewIdleReaper_StartWatcher_Disabled_NeverReaps(t *testing.T) {
	queues := newFakeCrewQueues()
	// The crew has drained all its work: bound queue reads Completed. Under an
	// ENABLED sweep this is the exact reap-eligible state after the grace window.
	queues.set("paul", queue.QueueStatusCompleted)
	stopper := &fakeCrewStopper{}

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir:   "/fake/project",
		Queues:       queues,
		Stopper:      stopper,
		GraceAfter:   10 * time.Millisecond, // tiny → an enabled sweep reaps almost at once
		ScanInterval: 2 * time.Millisecond,  // tiny → many scans inside the window below
		ListCrews:    listCrewsFrom([]crew.Record{{Name: "paul", Queue: "paul"}}),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Launch the REAL (disabled) StartWatcher. If it were re-enabled to start
	// loop(), the 2ms sweep would reap "paul" within ~10-12ms.
	r.StartWatcher(ctx)

	// Observe well past GraceAfter + dozens of ScanIntervals. Disabled → no
	// goroutine → no scan → no reap, regardless of how long we wait.
	time.Sleep(200 * time.Millisecond)

	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("idle-reap is DISABLED (hk-s2eac): a drained/completed idle crew must NOT be reaped, "+
			"but StartWatcher reaped %v — the SD-3 sweep appears to have been re-enabled", got)
	}
}

// TestCrewIdleReaper_NoOpWithoutProjectDir: scan is a no-op in unit-test mode
// (no ProjectDir), matching StaleWatcher/QuiesceArbiter's guarded-construction
// convention.
func TestCrewIdleReaper_NoOpWithoutProjectDir(t *testing.T) {
	queues := newFakeCrewQueues()
	queues.set("paul", queue.QueueStatusCompleted)
	stopper := &fakeCrewStopper{}

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		Queues:    queues,
		Stopper:   stopper,
		ListCrews: listCrewsFrom([]crew.Record{{Name: "paul", Queue: "paul"}}),
	})

	r.scan(context.Background())
	if got := stopper.names(); len(got) != 0 {
		t.Fatalf("expected no-op without ProjectDir, got %v", got)
	}
}
