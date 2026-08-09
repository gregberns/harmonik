package daemon

// scheduler_adoptrelease_internal_test.go — the run-session adoption revert.
//
// When an adopted tmux session turns out to be dead, adoptLiveRunSession gives
// its queue item back. That revert had no test of any kind. It also had no run
// guard, so it could set an item back to pending underneath a NEWER run that
// already held it, and it dropped the error from its own persist, so memory
// said pending while disk said dispatched.
//
// Every assertion about the item below reads the queue back off disk. Reading
// the in-memory store is how the dropped persist stayed invisible.
//
// The fixture is shared with scheduler_release_internal_test.go on purpose: it
// puts the target at the SECOND item of the SECOND group, so a lookup hardcoded
// to the first of either is caught.
//
// Bead ref: hk-o85ye, hk-mk4cl.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runloop"
)

// adoptedRecord builds the registry record adoptLiveRunSession acts on, aimed
// at the shared fixture's target item and naming runID as the run that holds it.
func adoptedRecord(beadID core.BeadID, runID string) runpkg.Record {
	return runpkg.Record{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        string(beadID),
		QueueName:     releaseQueueName,
		QueueID:       newTestQueueID(),
		GroupIndex:    releaseGroupIndex,
		ItemIndex:     releaseItemIndex,
		SessionName:   "harmonik-run-" + runID,
	}
}

// denyQueueWrites makes the queues directory unwritable for the rest of the
// test, so the next queue write fails the way a full disk does.
func denyQueueWrites(t *testing.T, projectDir string) {
	t.Helper()
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring the fixture directory so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})
}

// drainWake empties the store's coalescing wake channel, so a later read of it
// reports only what the code under test signalled.
func drainWake(store interface{ WakeCh() <-chan struct{} }) {
	for {
		select {
		case <-store.WakeCh():
		default:
			return
		}
	}
}

// The revert has to reach disk. A revert that changes only the live queue
// pointer leaves the next boot reading an item dispatched to a session that has
// already exited, which is the state the adoption exists to clear.
func TestReleaseAdoptedRunItem_ReturnsTheItemToPendingDurably(t *testing.T) {
	const beadID = core.BeadID("hk-adopt-1")
	projectDir, store, runID := reserveForRelease(t, beadID)

	releaseAdoptedRunItem(context.Background(), store, projectDir, adoptedRecord(beadID, runID.String()), runID)

	persisted, decoyGroup, decoyItem := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusPending {
		t.Errorf("persisted status = %q; want %q — the revert did not reach disk",
			persisted.Status, queue.ItemStatusPending)
	}
	if persisted.RunID != nil {
		t.Errorf("persisted RunID = %q; want nil — the dead run must not keep its stamp", *persisted.RunID)
	}
	if persisted.LastFailureReason != "run_session_adopted_dead" {
		t.Errorf("persisted LastFailureReason = %q; want %q — the operator cannot see why the item came back",
			persisted.LastFailureReason, "run_session_adopted_dead")
	}
	// The revert must touch exactly one item. The old lookup used raw slice
	// positions and no group status, so a wrong index reopened a neighbour.
	if decoyGroup.Status != queue.ItemStatusCompleted {
		t.Errorf("completed group's item = %q; want %q — the revert reached into another group",
			decoyGroup.Status, queue.ItemStatusCompleted)
	}
	if decoyItem.Status != queue.ItemStatusPending || decoyItem.LastFailureReason != "" {
		t.Errorf("sibling item = {%q, %q}; want {%q, empty} — the revert wrote the wrong item index",
			decoyItem.Status, decoyItem.LastFailureReason, queue.ItemStatusPending)
	}
}

// The guard this change exists for. The dead run's goroutine can arrive after
// the item was already released and re-dispatched to a NEWER run. Reverting it
// then reopens for dispatch an item somebody else is executing — two
// implementers on one bead, through the path built to prevent exactly that.
func TestReleaseAdoptedRunItem_RefusesAnItemHeldByANewerRun(t *testing.T) {
	const beadID = core.BeadID("hk-adopt-2")
	// The reservation on the fixture is the NEWER run: the one live right now.
	projectDir, store, liveRun := reserveForRelease(t, beadID)

	// The dead run is a different run, and this is its adoption goroutine
	// arriving late.
	deadRun := newReservationRunID(t)
	if deadRun == liveRun {
		t.Fatal("setup: the dead run and the live run must differ")
	}
	releaseAdoptedRunItem(context.Background(), store, projectDir, adoptedRecord(beadID, deadRun.String()), deadRun)

	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted status = %q; want %q — the dead run's goroutine reopened an item a live run holds, "+
			"which puts two implementers on one bead", persisted.Status, queue.ItemStatusDispatched)
	}
	if persisted.RunID == nil || *persisted.RunID != liveRun.String() {
		t.Errorf("persisted RunID = %v; want %q — the live run lost its stamp", persisted.RunID, liveRun)
	}
}

// A release whose write failed must not read as a release that worked. The old
// code printed the persist error and carried on, so the goroutine went on to
// clear the registry record for an item that was still dispatched on disk.
func TestReleaseAdoptedRunItem_AFailedWriteLeavesNoFalseRecovery(t *testing.T) {
	const beadID = core.BeadID("hk-adopt-3")
	projectDir, store, runID := reserveForRelease(t, beadID)
	denyQueueWrites(t, projectDir)
	drainWake(store)

	releaseAdoptedRunItem(context.Background(), store, projectDir, adoptedRecord(beadID, runID.String()), runID)

	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted status = %q; want %q — a lost write must not change disk",
			persisted.Status, queue.ItemStatusDispatched)
	}
	// The wake tells the dispatch loop there is work to pick up. Firing it after
	// a failed release sends the loop to look at an item that is still
	// dispatched, and a dispatched item is never re-selected.
	select {
	case <-store.WakeCh():
		t.Error("the store was woken after a failed release; the dispatch loop was told the item is back when it is not")
	default:
	}
}

// A successful release must wake the dispatch loop. This goroutine is not that
// loop, and an advance transaction does not wake the store on its own, so the
// item would sit pending until the next poll tick.
func TestReleaseAdoptedRunItem_ASuccessfulReleaseWakesTheDispatchLoop(t *testing.T) {
	const beadID = core.BeadID("hk-adopt-4")
	projectDir, store, runID := reserveForRelease(t, beadID)
	drainWake(store)

	releaseAdoptedRunItem(context.Background(), store, projectDir, adoptedRecord(beadID, runID.String()), runID)

	select {
	case <-store.WakeCh():
	default:
		t.Error("the store was not woken after a successful release; the item waits for the next poll tick")
	}
}

// The report is the whole of "not swallowed". A write failure must produce a
// message that names the queue and the repair, because a quarantined queue does
// not clear by retrying.
func TestAdoptedReleaseReport_AWriteFailureNamesTheQueueAndTheRepair(t *testing.T) {
	report := adoptedReleaseReport("main", "hk-adopt-5", newReservationRunID(t), reservationResult{
		Verdict: reservationWriteFailed,
		Outcome: queue.OutcomeCommitIndeterminate,
		Err:     errors.New("no space left on device"),
	})
	if report == "" {
		t.Fatal("a failed release produced no report; the error is swallowed exactly as it was before")
	}
	for _, want := range []string{"main", "hk-adopt-5", "no space left on device", "restart the daemon"} {
		if !strings.Contains(report, want) {
			t.Errorf("report = %q; it must contain %q", report, want)
		}
	}
}

// The counterpart that makes the test above falsifiable. A report function that
// always speaks would satisfy "not swallowed" and say nothing true.
func TestAdoptedReleaseReport_ASuccessfulReleaseSaysNothing(t *testing.T) {
	report := adoptedReleaseReport("main", "hk-adopt-6", newReservationRunID(t), reservationResult{
		Verdict: reservationReleased,
		Outcome: queue.OutcomeCommittedDurable,
	})
	if report != "" {
		t.Errorf("report for a released item = %q; want empty — an operator warned on every healthy release "+
			"stops reading the warning", report)
	}
}

// The remaining verdicts do not share a consequence, and the sentence about
// that consequence must come from releaseOutcomeAdvice rather than be written
// here. A run-mismatch refusal strands nothing: the item belongs to a live run.
func TestAdoptedReleaseReport_ARefusedReleaseCarriesTheSharedAdvice(t *testing.T) {
	report := adoptedReleaseReport("main", "hk-adopt-7", newReservationRunID(t), reservationResult{
		Verdict: reservationRetryLater,
		Outcome: queue.OutcomeRejected,
		Err:     errReleaseRunMismatch,
	})
	if !strings.Contains(report, releaseOutcomeAdvice(reservationRetryLater)) {
		t.Errorf("report = %q; it must carry releaseOutcomeAdvice(%q), which is the one tested statement of "+
			"what happens next", report, reservationRetryLater)
	}
}

// adoptStubLedger is a beadLedger that records the ReopenBead calls the adoption
// path makes and refuses every other method, which the adoption path never
// reaches.
type adoptStubLedger struct {
	reopened []core.BeadID
}

func (l *adoptStubLedger) Ready(context.Context) ([]core.BeadRecord, error) {
	return nil, errors.New("adoptStubLedger: Ready is not reachable from adoptLiveRunSession")
}

func (l *adoptStubLedger) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, errors.New("adoptStubLedger: ShowBead is not reachable from adoptLiveRunSession")
}

func (l *adoptStubLedger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return errors.New("adoptStubLedger: ClaimBead is not reachable from adoptLiveRunSession")
}

func (l *adoptStubLedger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return errors.New("adoptStubLedger: CloseBead is not reachable from adoptLiveRunSession")
}

func (l *adoptStubLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	l.reopened = append(l.reopened, beadID)
	return nil
}

// The wiring test. noopTmuxAdapter lists no sessions, so the monitor loop sees
// the adopted session gone on its first tick and runs the revert for real.
// Without this the revert could be correct and never called.
func TestAdoptLiveRunSession_ADeadSessionReleasesTheItemDurably(t *testing.T) {
	const beadID = core.BeadID("hk-adopt-8")
	projectDir, store, runID := reserveForRelease(t, beadID)

	ledger := &adoptStubLedger{}
	adoptLiveRunSession(t.Context(), ledger, runloop.RunEnv{ProjectDir: projectDir}, store,
		core.NewTransitionIDGenerator(), adoptedRecord(beadID, runID.String()), &noopTmuxAdapter{})

	if len(ledger.reopened) != 1 || ledger.reopened[0] != beadID {
		t.Errorf("reopened = %v; want exactly [%s]", ledger.reopened, beadID)
	}
	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusPending || persisted.RunID != nil {
		t.Errorf("persisted item = {%q, %v}; want {%q, nil} — adoption did not give the item back",
			persisted.Status, persisted.RunID, queue.ItemStatusPending)
	}
}

// A record whose run id will not parse cannot name the run that holds the item,
// and an item is released by the run holding it. Reverting on that record is the
// unguarded write this change removes, so it must write nothing at all.
func TestAdoptLiveRunSession_AnUnparseableRunIDReleasesNothing(t *testing.T) {
	const beadID = core.BeadID("hk-adopt-9")
	projectDir, store, holdingRun := reserveForRelease(t, beadID)

	rec := adoptedRecord(beadID, "not-a-uuid")
	ledger := &adoptStubLedger{}
	adoptLiveRunSession(t.Context(), ledger, runloop.RunEnv{ProjectDir: projectDir}, store,
		core.NewTransitionIDGenerator(), rec, &noopTmuxAdapter{})

	if len(ledger.reopened) != 0 {
		t.Errorf("reopened = %v; want none — the reopen needs the run identity this record does not carry",
			ledger.reopened)
	}
	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted status = %q; want %q — the item was reverted without a run to check it against",
			persisted.Status, queue.ItemStatusDispatched)
	}
	if persisted.RunID == nil || *persisted.RunID != holdingRun.String() {
		t.Errorf("persisted RunID = %v; want %q", persisted.RunID, holdingRun)
	}
}
