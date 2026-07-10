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
