package daemon_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

type haltAtClaimLedger struct {
	*stubBeadLedger
	halt     func()
	claimErr error
	claimed  chan struct{}
}

func (l *haltAtClaimLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	select {
	case <-l.claimed:
	default:
		close(l.claimed)
		l.halt()
		return l.claimErr
	}
	return l.claimErr
}

func reservationWindowQueue(t *testing.T, beadID core.BeadID) *queue.Queue {
	t.Helper()
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
			CreatedAt:  now,
		}},
	}
}

type openBeadLedger struct{}

func (openBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (openBeadLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

func runNextDaemonStart(t *testing.T, projectDir string) []*queue.Queue {
	t.Helper()
	loaded, err := lifecycle.LoadQueueAtStartup(
		context.Background(), projectDir, openBeadLedger{}, nil, slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("the next daemon start refused the queue this exit left behind, so the exit wedged the daemon: %v", err)
	}
	return loaded
}

func assertNoStrandedDispatchedItem(t *testing.T, projectDir string) {
	t.Helper()

	q, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load main queue after shutdown: %v", err)
	}
	if q == nil {
		t.Fatal("the canonical main queue file is gone after the exit; a clean shutdown parks the queue in place (QM-054), so an absent file is the submitted work deleted without a receipt, not an item released")
	}
	if q.Status == queue.QueueStatusPausedByDrain && q.ResumeOnStart {
		runNextDaemonStart(t, projectDir)
		q, err = queue.Load(context.Background(), projectDir, queue.QueueNameMain)
		if err != nil {
			t.Fatalf("load main queue after the next start recovered it: %v", err)
		}
		if q == nil {
			t.Fatal("the next start unlinked the main queue file while it still held an item that never completed; the file is unlinked only when every group is complete-success, so this is the work disappearing rather than finishing")
		}
	}
	if q.Status != queue.QueueStatusActive {
		t.Fatalf(
			"the main queue is %q (resume_on_start=%v) after the exit and any recovery the next start ran; nothing is ever dispatched from a queue that is not active, "+
				"and a paused-by-drain queue whose restart intent was never written or never consumed holds the name \"main\" against QM-027 for ever, "+
				"so the item is stranded and every later submit to that name is refused",
			q.Status, q.ResumeOnStart)
	}
	for gi := range q.Groups {
		for ii := range q.Groups[gi].Items {
			item := q.Groups[gi].Items[ii]
			if item.Status == queue.ItemStatusDispatched {
				t.Fatalf(
					"bead %s is left dispatched in a still-active queue after the loop exited before the claim: "+
						"a dispatched item is never re-selected and no reconcile pass repairs an open bead, so it is stranded",
					item.BeadID)
			}
		}
	}
}

func runLoopToExit(t *testing.T, run func() error) {
	t.Helper()
	if err := runLoopCapturingExit(t, run); err != nil {
		t.Fatalf("work loop exited via a direct error return, which skips the queue drain: %v", err)
	}
}

func runLoopCapturingExit(t *testing.T, run func() error) error {
	t.Helper()
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		err = run()
	}()
	awaitLoopTeardown(t, done, "reservation-window work loop")
	return err
}

type refusingTIDSource struct {
	calls atomic.Int32
	err   error
}

func (s *refusingTIDSource) Next() (core.TransitionID, error) {
	s.calls.Add(1)
	return core.TransitionID{}, s.err
}

// TestWorkLoop_DispatchHaltAfterTheReservationDoesNotLeaveTheItemDispatched
// drives the reachable half of the window: dispatch is halted while the claim
// is in flight, with the item already durably reserved.
//
// Two sub-cases split on what the claim itself did, because the loop reads them
// through different branches — one returns before the release block, the other
// after it.
func TestWorkLoop_DispatchHaltAfterTheReservationDoesNotLeaveTheItemDispatched(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	cases := []struct {
		name     string
		claimErr error
	}{
		{
			name:     "claim fails as dispatch halts",
			claimErr: context.Canceled,
		},
		{
			name:     "claim succeeds as dispatch halts",
			claimErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir, _ := workloopFixtureProjectDir(t)
			workloopFixtureGitRepo(t, projectDir)

			const beadID = core.BeadID("reservation-window-bead-001")
			q := reservationWindowQueue(t, beadID)
			if err := queue.Persist(context.Background(), projectDir, q); err != nil {
				t.Fatalf("persist queue: %v", err)
			}

			qs := daemon.ExportedNewQueueStore()
			qs.SetQueue(q)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ledger := &haltAtClaimLedger{
				stubBeadLedger: &stubBeadLedger{labels: workloopFixtureSingleLabels},
				halt:           cancel,
				claimErr:       tc.claimErr,
				claimed:        make(chan struct{}),
			}

			deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
				BrAdapter:        ledger,
				Bus:              &stubEventCollector{},
				ProjectDir:       projectDir,
				HandlerBinary:    "/bin/sh",
				HandlerArgs:      []string{"-c", "exit 0"},
				IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
				AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
				QueueStore:       qs,
			})

			runLoopToExit(t, func() error { return daemon.ExportedRunWorkLoop(ctx, deps) })

			select {
			case <-ledger.claimed:
			default:
				t.Fatal("the claim never ran, so the loop never entered the reservation window; this test proved nothing")
			}

			assertNoStrandedDispatchedItem(t, projectDir)
		})
	}
}

// TestWorkLoop_AClaimTransitionIDFailureDoesNotStrandTheReservedItem drives the
// third exit — the one that used to strand. The loop reserves the item, asks for
// a TransitionID to stamp the claim with, and is refused.
//
// Two things must both hold, and they pull in opposite directions, which is why
// they are asserted together:
//
//   - The error propagates. A refused TransitionID is a real fault and the loop
//     must not swallow it and carry on.
//   - The queue is drained anyway. The item was already committed to disk as
//     dispatched before the refusal, so an exit that only reports the error
//     leaves it owned by a run that never started.
//
// The failing source is the point of the seam: the real generator refuses only
// on a UUIDv7 fault, so without an injectable source this exit is unreachable
// from a test and the fix for it is unverifiable in exactly the way the bug was.
func TestWorkLoop_AClaimTransitionIDFailureDoesNotStrandTheReservedItem(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("reservation-window-bead-003")
	q := reservationWindowQueue(t, beadID)
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist queue: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	refused := errors.New("uuid: no entropy available")
	tidGen := &refusingTIDSource{err: refused}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        &stubBeadLedger{labels: workloopFixtureSingleLabels},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		TIDGen:           tidGen,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runLoopCapturingExit(t, func() error { return daemon.ExportedRunWorkLoop(ctx, deps) })

	if got := tidGen.calls.Load(); got == 0 {
		t.Fatal("the loop never asked for a TransitionID, so it never reached the claim; this test proved nothing")
	}

	if err == nil {
		t.Fatal("the loop exited nil after the TransitionID source refused; a refused TransitionID is a fault and must propagate")
	}
	if !errors.Is(err, refused) {
		t.Errorf("the loop exited with %v, which does not wrap the refusal it was given; the cause is lost", err)
	}
	if !strings.Contains(err.Error(), "claim TransitionID") {
		t.Errorf("the exit error %q does not name the claim TransitionID step, so an operator cannot tell which generation failed", err)
	}

	assertNoStrandedDispatchedItem(t, projectDir)
}

// TestWorkLoop_ACleanExitParksTheActiveQueueAndTheNextStartResumesIt states the
// property the case above depends on, on its own: the drain is what makes a halt
// in the reservation window survivable, so it is worth pinning where a reader
// will find it rather than only as a side effect.
//
// This is the cheaper, broader version — no reservation, just an active queue
// and an immediate halt.
//
// The claim spans two programs and neither half is worth anything alone. A clean
// exit parks the queue exactly where it is (QM-054) and writes a durable
// one-shot restart intent; the next start consumes that intent and puts the
// queue back to active (QM-055). Parking is not what makes the name safe: a
// queue parked at paused-by-drain owns the name "main" against QM-027 exactly as
// an active one does — internal/queue/validation.go releases the name for a
// completed or paused-by-failure queue, and otherwise only for the zero-value
// status a corrupt file carries — so a park nobody
// consumes refuses every later submit with queue_already_active just as surely
// as a queue nobody parked. The restart intent, written and then consumed, is
// the whole difference. So the exit and the start are judged here on three
// things, and each of them alone is enough to break the daemon:
//
//   - the queue is still on disk at its canonical path — losing the file loses
//     the submitted work, silently and with no receipt;
//   - it is parked, not active, and it carries the restart intent — without the
//     intent the next start reads an explicit operator pause and holds it for
//     ever, waiting for a resume nobody knows to give;
//   - the real next start consumes that intent, puts the queue back to active
//     with the bit cleared, and still holds the work.
func TestWorkLoop_ACleanExitParksTheActiveQueueAndTheNextStartResumesIt(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	q := reservationWindowQueue(t, core.BeadID("reservation-window-bead-002"))
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist queue: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runLoopToExit(t, func() error { return daemon.ExportedRunWorkLoop(ctx, deps) })

	parked, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load the canonical queue after exit: %v", err)
	}
	if parked == nil {
		t.Fatal("the canonical queue file is gone after exit; a clean shutdown parks the queue in place so the next start can carry on with it, and a queue that disappears takes the operator's submitted work with it")
	}
	if parked.Status == queue.QueueStatusActive {
		t.Fatal("the queue is still ACTIVE on disk after exit; nothing is driving it, and it owns the name \"main\" against QM-027 until somebody edits the file by hand, so every later submit is refused")
	}
	if parked.Status != queue.QueueStatusPausedByDrain {
		t.Fatalf("the queue is %q on disk after exit; a clean shutdown parks an active queue as paused-by-drain (QM-054)", parked.Status)
	}
	if !parked.ResumeOnStart {
		t.Fatal("the queue is parked with no restart intent; that is the shape of an explicit operator pause, so the next start will hold it paused for ever waiting on a resume nobody knows to give")
	}
	if len(parked.Groups) != 1 || len(parked.Groups[0].Items) != 1 {
		t.Fatalf("the parked queue holds %d group(s) and no longer carries its single item intact; the park must not edit the work", len(parked.Groups))
	}

	resumed := runNextDaemonStart(t, projectDir)
	if len(resumed) != 1 {
		t.Fatalf("the next start carried %d queue(s) into its dispatch loop; want the one this exit parked", len(resumed))
	}
	if resumed[0].Status != queue.QueueStatusActive {
		t.Errorf("the next start left the parked queue %q instead of putting it back to active; the restart intent was written and never consumed, so the work never resumes (QM-055)", resumed[0].Status)
	}
	if resumed[0].ResumeOnStart {
		t.Error("the next start resumed the queue but did not clear the restart intent; the intent is one-shot, so a queue the operator pauses later would be resumed out from under them")
	}
	if resumed[0].QueueID != parked.QueueID {
		t.Errorf("the next start carried queue_id %s; the parked queue was %s, so this is not the same queue continuing", resumed[0].QueueID, parked.QueueID)
	}
	if got := countQueueItems(resumed[0]); got != 1 {
		t.Errorf("the resumed queue holds %d item(s); the one bead this queue was submitted with must survive the round trip", got)
	}

	durable, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load the canonical queue after the next start resumed it: %v", err)
	}
	if durable == nil || durable.Status != queue.QueueStatusActive || durable.ResumeOnStart {
		t.Errorf("the canonical file after the next start = %+v; want active with the restart intent cleared, so the resume is durable and not repeated", durable)
	}

	assertNoStrandedDispatchedItem(t, projectDir)
}

func countQueueItems(q *queue.Queue) int {
	n := 0
	for gi := range q.Groups {
		n += len(q.Groups[gi].Items)
	}
	return n
}
