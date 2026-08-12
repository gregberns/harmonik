package daemon_test

// workloop_reservationwindow_test.go — what happens to a reserved queue item
// when the work loop exits before the bead is ever claimed.
//
// The window is narrow and expensive. reserveQueueItem commits the item as
// DISPATCHED with a RunID in one durable write. The bead is claimed some lines
// later. Between those two points the loop can leave, and the item is then
// recorded on disk as handed to a run that never existed.
//
// That state does not self-heal. A dispatched item is never re-selected. The
// boot provenance pass reads a dispatched item as a live owner and excludes its
// bead from the stale sweep, and the Class-B reconcile pass only repairs beads
// the ledger holds in_progress — this bead is still open, because the claim is
// exactly what did not happen. So an item lost in this window is stranded until
// somebody edits the queue file, and any wave group holding it never advances.
//
// # What these tests found
//
// The test backlog recorded this as three unreleased early returns and marked
// it RED TODAY. Driven against the tree, two of the three are not defects:
// both reachable exits leave through exitClean, which calls
// drainQueuesForRestart and parks the whole active queue as paused-by-drain
// with a one-shot restart intent, so the item goes with the queue into a state
// the next start owns rather than stranding. Those two paths are pinned below
// so the protection stays honest — it is load-bearing, and nothing was
// asserting it.
//
// Shutdown used to ARCHIVE the queue instead — CancelQueueOnShutdown renamed
// the canonical file to a .cancelled-<ts> suffix and the item went away with
// it. That is gone. Parking keeps the file where it is, so "the item is safe"
// is now a claim about two programs, not one: the exit parks it and the next
// start takes it back. Every assertion in this file is written to span both,
// because a park that no start can consume is the same strand under a new
// name. Spec: queue-model.md §8.5 QM-054 (park with resume_on_start) and §8.6
// QM-055 (the next start recovers dispatched items, then resumes).
//
// The third exit WAS real: the claim TransitionID generation failure returned
// an error directly instead of draining, so it skipped the drain and left
// exactly the stranded item described above. It had no test because it had no
// seam — TransitionIDGenerator.Next fails only when the UUIDv7 draw fails, and
// nothing let a test induce that.
//
// Both halves are now closed. runloop.TransitionIDSource is the seam, so a test
// can hand the loop a generator that refuses; scheduler.go routes the failure
// through exitFatal, which drains on the way out and still returns the error.
// The third test below drives it.
//
// Mutation that must turn these red: delete the drainQueuesForRestart call from
// exitClean in scheduler.go. All three tests below fail; that call is the only
// thing keeping any of these exits safe. Two narrower mutations attack a
// different half of the contract each: drop the `q.ResumeOnStart = true` line
// from queue.PauseQueueForRestart, and the queue parks with no restart intent,
// so the next start leaves it paused for ever and the name never frees; or drop
// the ResumeOnStart branch from loadOneQueueAtStartup in internal/lifecycle, and
// the intent is written but never consumed. Neither of those two leaves a
// dispatched item behind — the queue is simply left parked — so the only thing
// that catches them outside the clean-exit test is
// assertNoStrandedDispatchedItem failing a queue left in ANY non-active state
// instead of reading the status and returning. That test asserts both halves
// directly; the two sibling tests inherit the check through the helper. For the
// claim-TransitionID test alone, the narrower mutation is to put back the bare
// `return fmt.Errorf(...)` at the claim-TID failure in scheduler.go.

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

// haltAtClaimLedger halts dispatch at the moment of the claim — the shape a
// SIGTERM takes when it lands in the reservation window. By the time ClaimBead
// runs, reserveQueueItem has already committed the item as dispatched, so
// cancelling here puts the loop in the window with a durable reservation
// outstanding.
type haltAtClaimLedger struct {
	*stubBeadLedger
	halt     func()
	claimErr error
	claimed  chan struct{}
}

func (l *haltAtClaimLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	select {
	case <-l.claimed:
		// Already fired once; later calls behave normally.
	default:
		close(l.claimed)
		l.halt()
		return l.claimErr
	}
	return l.claimErr
}

// reservationWindowQueue builds a one-item active wave queue, the smallest
// thing that can carry a reservation.
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

// openBeadLedger answers the two questions the next daemon start asks the Beads
// ledger while it recovers a queue: what is this bead, and what is in flight. It
// reports every bead open and nothing in flight, which is the truth for a bead
// the loop reserved but never claimed — the exact case this file is about.
type openBeadLedger struct{}

func (openBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (openBeadLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

// runNextDaemonStart drives the real startup queue-recovery path against
// whatever this exit left on disk, and returns what that start would carry into
// its dispatch loop. It is the second half of every claim in this file: the exit
// no longer disposes of the queue itself, it hands it to the next start, so an
// assertion that stops at the file on disk stops one program too early.
//
// A start that returns an error is itself a failure of the property. That is the
// literal shape of "the parked queue blocks the next start".
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

// assertNoStrandedDispatchedItem is the whole point of this file. It reads the
// canonical queue file back from disk — not the in-memory store, which would
// report success for a queue that is still on disk holding a dispatched item —
// and fails unless the item is left somewhere a later program can still pick it
// up.
//
// Three states fail, and for the same reason each time: the bead was reserved
// and never claimed, so nothing except this queue is holding it.
//
//   - The file is ABSENT. That is not a release, it is the operator's submitted
//     work deleted with no receipt, and the clean-exit test below calls the same
//     disk state lost work. Passing it here would let this file contradict
//     itself.
//   - The queue is left NOT ACTIVE. Only an active queue is dispatched from, and
//     a queue parked at paused-by-drain holds its name against QM-027 exactly as
//     an active one does — internal/queue/validation.go releases the name for
//     completed and paused-by-failure, and otherwise only for the zero-value
//     status a corrupt file carries. So a park nobody
//     consumes strands the item AND refuses every later submit to that name.
//     Active-versus-parked is not the discriminator; whether the restart intent
//     is written and then consumed is.
//   - The queue is ACTIVE and still records the item as dispatched. A dispatched
//     item is never re-selected.
//
// A queue parked WITH the restart intent is the one state that is not judged on
// what the exit left, because the next start owns the repair: that start puts
// the parked file back to active, so a dispatched item sitting in it is harmless
// only if the start's recovery pass takes it back first. That case runs the real
// start and then judges the queue the start produced, by the same three rules.
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
		// Parked with restart intent: the next start owns the repair. Run it and
		// judge what it leaves, not what the exit left.
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

// runLoopToExit runs the work loop, waits for it to return, and asserts it left
// cleanly. The caller supplies the call because the runtime type is unexported
// to this package.
//
// The returned error is worth asserting rather than discarding: a clean exit
// goes through exitClean, which returns nil AFTER draining the queue. A non-nil
// error means the loop took one of the direct error returns instead — the
// family this file is about, and the family that skips the drain.
func runLoopToExit(t *testing.T, run func() error) {
	t.Helper()
	if err := runLoopCapturingExit(t, run); err != nil {
		t.Fatalf("work loop exited via a direct error return, which skips the queue drain: %v", err)
	}
}

// runLoopCapturingExit runs the work loop to completion and hands back whatever
// it returned. The claim-TID case below needs the error rather than a failure:
// a fatal error there is CORRECT and must still propagate, and the claim under
// test is that it propagates AND drains, not that it stops happening.
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

// refusingTIDSource is a TransitionID source that never issues one. It stands in
// for a UUIDv7 draw that fails — the only way the real generator can fail, and a
// thing no test can make the real generator do.
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
			// The claim failed AND dispatch is halting. The loop returns at the
			// dispatchCtx check above the release block, so the release that
			// exists for an ordinary claim failure never runs.
			name:     "claim fails as dispatch halts",
			claimErr: context.Canceled,
		},
		{
			// The claim succeeded but dispatch is halting anyway. The item is
			// dispatched and the bead is claimed; the loop unwinds from a later
			// point.
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

	// Positive evidence that the loop reached the window at all. Without this the
	// two assertions below are satisfied for free by a loop that never dispatched.
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

	// Half one — what the exit left on disk.
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

	// Half two — what the next start makes of it. This is the part that decides
	// whether the park was a handoff or a wedge.
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

	// And the durable file agrees with what the start is holding. A start that
	// resumes only in memory re-parks nothing and repeats the whole recovery on
	// every boot.
	durable, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load the canonical queue after the next start resumed it: %v", err)
	}
	if durable == nil || durable.Status != queue.QueueStatusActive || durable.ResumeOnStart {
		t.Errorf("the canonical file after the next start = %+v; want active with the restart intent cleared, so the resume is durable and not repeated", durable)
	}

	assertNoStrandedDispatchedItem(t, projectDir)
}

// countQueueItems totals the items a queue still carries across all its groups.
func countQueueItems(q *queue.Queue) int {
	n := 0
	for gi := range q.Groups {
		n += len(q.Groups[gi].Items)
	}
	return n
}
