package crewrun

// idlereap_test.go — the regression pins for the DISABLED SD-3 idle-crew sweep.
//
// StartWatcher has a deliberately empty body: automatic crew idle-reaping was
// turned off by operator directive (2026-07-18) after it tore down worker and
// gate crews ~5 minutes into standby. An empty body is the easiest thing in the
// codebase to "helpfully" restore, so it needs tests that fail when it is.
//
// Both tests here drive the REAL StartWatcher and assert on OBSERVABLE
// BEHAVIOUR — no crew is stopped, and the crew registry is never even read.
// Neither inspects the body, so a re-enable that hand-rolls its own pump instead
// of restoring loop() fails them just the same. What they cannot see is a caller
// that bypasses StartWatcher entirely; StartWatcher is the only production entry
// point (bootworkloop.go), and a second one is the change to be suspicious of.
//
// Their earlier incarnation lived in idlereap_hks2eac_test.go and was swept up
// by the Phase 1 ticket-named-test-file deletion (ec66da798) — the doc comment
// on StartWatcher went on claiming they existed for months while the empty body
// was in fact unpinned.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/queue"
)

// reapObservationWindow is how long a test watches for evidence that the sweep
// woke up. It is a large multiple of the millisecond-scale GraceAfter and
// ScanInterval the tests configure, so an enabled sweep has dozens of ticks in
// which to act. The tests only ever pay it in full when they PASS: any sign of
// life fails them immediately via the signal channel.
const reapObservationWindow = 250 * time.Millisecond

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

// recordingCrewStopper is a test double for crewStopper. It records every crew
// it was asked to tear down and signals the first such call, so a re-enabled
// sweep is caught the moment it acts rather than at the end of a fixed sleep.
type recordingCrewStopper struct {
	mu      sync.Mutex
	stopped []string
	first   chan struct{}
	once    sync.Once
}

func newRecordingCrewStopper() *recordingCrewStopper {
	return &recordingCrewStopper{first: make(chan struct{})}
}

func (f *recordingCrewStopper) HandleCrewStop(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var req CrewStopRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.stopped = append(f.stopped, req.Name)
	f.mu.Unlock()
	f.once.Do(func() { close(f.first) })
	return json.Marshal(struct{}{})
}

func (f *recordingCrewStopper) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.stopped...)
}

// TestCrewIdleReaper_StartWatcher_Disabled_NeverReaps pins the operator directive
// at the level the operator cares about: a crew that has drained its queue and
// gone idle KEEPS RUNNING. The crew is placed in the exact state an enabled sweep
// reaps — bound queue at QueueStatusCompleted, past its grace window — and the
// assertion is that teardown never happens.
//
// A hermetic test cannot spawn a real remote-control tmux pane, so "the session
// survives" is proven at its causal root: HandleCrewStop is what quits the pane
// and deletes the registry record, and it is never called.
func TestCrewIdleReaper_StartWatcher_Disabled_NeverReaps(t *testing.T) {
	t.Parallel()

	queues := newFakeCrewQueues()
	// Drained and completed — under an ENABLED sweep this is precisely the
	// reap-eligible state once GraceAfter elapses.
	queues.set("paul", queue.QueueStatusCompleted)
	stopper := newRecordingCrewStopper()

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir:   "/fake/project",
		Queues:       queues,
		Stopper:      stopper,
		GraceAfter:   10 * time.Millisecond, // an enabled sweep would reap almost at once
		ScanInterval: 2 * time.Millisecond,  // many ticks inside the observation window
		ListCrews: func(string) ([]crew.Record, error) {
			return []crew.Record{{Name: "paul", Queue: "paul"}}, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.StartWatcher(ctx)

	select {
	case <-stopper.first:
		t.Fatalf("automatic crew idle-reaping is DISABLED by operator directive (2026-07-18): "+
			"an idle crew whose queue has completed must stay alive, but StartWatcher tore down %v "+
			"— the SD-3 sweep has been re-enabled", stopper.names())
	case <-time.After(reapObservationWindow):
	}
}

// TestCrewIdleReaper_StartWatcher_Disabled_NeverScans is the tighter complement:
// the disabled watcher must never so much as READ the crew registry. A sweep
// calls ListCrews on its first tick, before any grace window, so this catches a
// re-enable that has not yet reaped anything — including one whose grace window
// is too long for the reap test to observe.
func TestCrewIdleReaper_StartWatcher_Disabled_NeverScans(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	scans := 0
	firstScan := make(chan struct{})
	var once sync.Once

	queues := newFakeCrewQueues()
	queues.set("paul", queue.QueueStatusCompleted)

	r := NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir:   "/fake/project",
		Queues:       queues,
		Stopper:      newRecordingCrewStopper(),
		GraceAfter:   time.Hour,            // deliberately long: no reap could ever be observed
		ScanInterval: 1 * time.Millisecond, // but an enabled sweep still scans immediately
		ListCrews: func(string) ([]crew.Record, error) {
			mu.Lock()
			scans++
			mu.Unlock()
			once.Do(func() { close(firstScan) })
			return []crew.Record{{Name: "paul", Queue: "paul"}}, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.StartWatcher(ctx)

	select {
	case <-firstScan:
		mu.Lock()
		got := scans
		mu.Unlock()
		t.Fatalf("automatic crew idle-reaping is DISABLED by operator directive (2026-07-18): "+
			"StartWatcher must never launch the sweep, but the crew registry was scanned %d time(s) "+
			"— the sweep goroutine has been started", got)
	case <-time.After(reapObservationWindow):
	}
}
