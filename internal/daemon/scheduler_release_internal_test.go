package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// The release is the other half of the reservation, and the half that had no
// durability test at all. Deleting the persist from the old raw revert left the
// suite green, because the tests that depend on the revert read the in-memory
// store and never the file. Every assertion below reads the queue back off
// disk for that reason.
//
// Bead ref: hk-mk4cl.

// releaseQueueName is the queue every case below reserves on. The release is
// per-item, so a second queue name would add no coverage.
const releaseQueueName = "main"

// The fixture deliberately holds TWO groups and TWO items, and the target is
// the SECOND item of the SECOND group. A one-group one-item fixture cannot tell
// a lookup that honours the group index, the item index and the bead id from
// one hardcoded to the first of each — the three dimensions this change claims
// to identify its item by. The decoys are what make the claim falsifiable.
const (
	releaseDecoyGroupBead = core.BeadID("hk-release-decoy-group")
	releaseDecoyItemBead  = core.BeadID("hk-release-decoy-item")
	releaseGroupIndex     = 1
	releaseItemIndex      = 1
)

// releaseFixture builds a project directory holding one active queue whose
// target item sits behind a completed group and a sibling item.
func releaseFixture(t *testing.T, beadID core.BeadID) (string, *queuewiring.QueueStore) {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		Name:          releaseQueueName,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusCompleteSuccess,
				Items:      []queue.Item{{BeadID: releaseDecoyGroupBead, Status: queue.ItemStatusCompleted}},
			},
			{
				GroupIndex: releaseGroupIndex,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: releaseDecoyItemBead, Status: queue.ItemStatusPending},
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
			},
		},
	}
	store := queuewiring.NewQueueStore()
	store.SetQueueByName(releaseQueueName, q)
	return projectDir, store
}

// releaseTarget names the item every case acts on.
func releaseTarget(beadID core.BeadID, runID core.RunID) queueReservation {
	return queueReservation{
		QueueName:  releaseQueueName,
		GroupIndex: releaseGroupIndex,
		ItemIndex:  releaseItemIndex,
		BeadID:     beadID,
		RunID:      runID,
	}
}

// loadReleasedItem reads the TARGET item back off disk, and also returns the
// two decoys so a test can prove the write did not touch them.
func loadReleasedItem(t *testing.T, projectDir string) (target, decoyGroup, decoyItem queue.Item) {
	t.Helper()
	//nolint:gosec // the path is built from t.TempDir(); the test wrote this file itself
	raw, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", releaseQueueName+".json"))
	if err != nil {
		t.Fatalf("read persisted queue: %v", err)
	}
	var persisted queue.Queue
	if unmarshalErr := json.Unmarshal(raw, &persisted); unmarshalErr != nil {
		t.Fatalf("unmarshal persisted queue: %v", unmarshalErr)
	}
	if len(persisted.Groups) != 2 || len(persisted.Groups[1].Items) != 2 {
		t.Fatalf("persisted queue lost its shape: %s", raw)
	}
	return persisted.Groups[1].Items[1], persisted.Groups[0].Items[0], persisted.Groups[1].Items[0]
}

// reserveForRelease drives the fixture to the state a release starts from: the
// target item durably dispatched, carrying the returned run.
func reserveForRelease(t *testing.T, beadID core.BeadID) (string, *queuewiring.QueueStore, core.RunID) {
	t.Helper()
	projectDir, store := releaseFixture(t, beadID)
	runID := newReservationRunID(t)
	reserved := reserveQueueItem(context.Background(), store, projectDir, releaseTarget(beadID, runID))
	if reserved.Verdict != reservationReserved {
		t.Fatalf("setup: reserve verdict = %q; want %q", reserved.Verdict, reservationReserved)
	}
	return projectDir, store, runID
}

// The claim failed, so the item must go back — and it must go back on disk. If
// only memory is reverted, the next boot reads a dispatched item naming a run
// that was never claimed and never launched.
func TestReleaseReservation_ReturnsTheItemToPendingDurably(t *testing.T) {
	const beadID = core.BeadID("hk-release-1")
	projectDir, store, runID := reserveForRelease(t, beadID)

	released := releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, runID), "claim_failed")
	if released.Verdict != reservationReleased {
		t.Fatalf("verdict = %q (err %v); want %q", released.Verdict, released.Err, reservationReleased)
	}

	persisted, decoyGroup, decoyItem := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusPending {
		t.Errorf("persisted status = %q; want %q — the revert did not reach disk",
			persisted.Status, queue.ItemStatusPending)
	}
	if persisted.RunID != nil {
		t.Errorf("persisted RunID = %q; want nil — a released item must not name a run that never started", *persisted.RunID)
	}
	if persisted.LastFailureReason != "claim_failed" {
		t.Errorf("persisted LastFailureReason = %q; want %q", persisted.LastFailureReason, "claim_failed")
	}
	// hk-6pspu: the attempt budget is monotonic. Resetting it here would let a
	// bead that can never be claimed cycle for as long as the daemon runs.
	if persisted.Attempts != 1 {
		t.Errorf("persisted Attempts = %d; want 1 — the release must not refund the attempt", persisted.Attempts)
	}
	// The release must touch exactly one item. A lookup that ignores the group
	// index or the item index would reopen a neighbour for dispatch.
	if decoyGroup.Status != queue.ItemStatusCompleted {
		t.Errorf("completed group's item = %q; want %q — the release reached into another group",
			decoyGroup.Status, queue.ItemStatusCompleted)
	}
	if decoyItem.LastFailureReason != "" || decoyItem.Attempts != 0 {
		t.Errorf("sibling item = {reason %q, attempts %d}; want {empty, 0} — the release wrote the wrong item index",
			decoyItem.LastFailureReason, decoyItem.Attempts)
	}
}

// The in-memory store must agree with the file. A release that commits to disk
// but leaves the live queue dispatched would strand the item until restart.
func TestReleaseReservation_LeavesMemoryAndDiskAgreeing(t *testing.T) {
	const beadID = core.BeadID("hk-release-2")
	projectDir, store, runID := reserveForRelease(t, beadID)

	releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, runID), "claim_failed")

	live := store.Snapshot(releaseQueueName).Queue
	if live == nil {
		t.Fatal("snapshot queue = nil")
	}
	inMemory := live.Groups[1].Items[1]
	if inMemory.Status != queue.ItemStatusPending || inMemory.RunID != nil {
		t.Errorf("in-memory item = {%q, %v}; want {%q, nil}",
			inMemory.Status, inMemory.RunID, queue.ItemStatusPending)
	}
}

// A release names the run giving the reservation back. If the item is held by a
// different run, releasing it would reopen for dispatch an item somebody else
// is executing — two implementers on one bead, by the path built to prevent it.
func TestReleaseReservation_RefusesAnItemDispatchedToAnotherRun(t *testing.T) {
	const beadID = core.BeadID("hk-release-3")
	projectDir, store, holdingRun := reserveForRelease(t, beadID)

	stranger := newReservationRunID(t)
	released := releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, stranger), "claim_failed")
	if released.Verdict != reservationRetryLater {
		t.Errorf("verdict = %q; want %q — a mismatched run must not release the item",
			released.Verdict, reservationRetryLater)
	}

	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted status = %q; want %q — the holding run's item was reopened",
			persisted.Status, queue.ItemStatusDispatched)
	}
	if persisted.RunID == nil || *persisted.RunID != holdingRun.String() {
		t.Errorf("persisted RunID = %v; want %q — the holding run lost its stamp",
			persisted.RunID, holdingRun)
	}
}

// Nothing to undo is not an error worth shutting a queue over, and it is not a
// success either. The item moved; look again next tick.
func TestReleaseReservation_RefusesAnItemThatIsNotDispatched(t *testing.T) {
	const beadID = core.BeadID("hk-release-4")
	projectDir, store := releaseFixture(t, beadID)

	released := releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, newReservationRunID(t)), "claim_failed")
	if released.Verdict != reservationRetryLater {
		t.Errorf("verdict = %q; want %q", released.Verdict, reservationRetryLater)
	}
}

// A double release is the ordinary producer of reservationRetryLater on this
// path, and it is where the operator report used to be false. The first release
// returns the item to pending. The second finds an item that is no longer
// dispatched, writes nothing, and reports retry_later — at which point the item
// is PENDING and the next tick re-selects it.
//
// The three assertions are one story and are worth nothing apart: the verdict,
// the state on disk that the verdict describes, and the sentence an operator is
// given about that state. Pinning the verdict alone is what let the wrong
// sentence ship — TestReleaseReservation_RefusesAnItemThatIsNotDispatched
// already pinned the verdict, and the report still said the opposite.
func TestReleaseReservation_SecondReleaseLeavesTheItemPendingAndSaysSo(t *testing.T) {
	const beadID = core.BeadID("hk-release-double")
	projectDir, store, runID := reserveForRelease(t, beadID)

	first := releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, runID), "claim_failed")
	if first.Verdict != reservationReleased {
		t.Fatalf("setup: first release verdict = %q; want %q", first.Verdict, reservationReleased)
	}

	second := releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, runID), "claim_failed")
	if second.Verdict != reservationRetryLater {
		t.Fatalf("second release verdict = %q; want %q", second.Verdict, reservationRetryLater)
	}

	target, _, _ := loadReleasedItem(t, projectDir)
	if target.Status != queue.ItemStatusPending {
		t.Fatalf("persisted status = %q; want %q — the premise of this test is that the item is "+
			"already back, so a report calling it stranded is false", target.Status, queue.ItemStatusPending)
	}
	if target.RunID != nil {
		t.Errorf("persisted RunID = %q; want nil — a released item names no run", *target.RunID)
	}

	// The operator sentence must not contradict the state two lines above.
	advice := releaseOutcomeAdvice(second.Verdict)
	if strings.Contains(advice, "still dispatched") {
		t.Errorf("advice for %q says %q — the item is pending on disk and the next tick re-selects it, "+
			"so this sends the operator looking for a strand that does not exist", second.Verdict, advice)
	}
	if strings.Contains(advice, "boot reconciliation") {
		t.Errorf("advice for %q says %q — nothing here waits for boot reconciliation", second.Verdict, advice)
	}
}

// The contended verdict is the one that DOES strand the item, and it must keep
// saying so. Without this, the fix above could be "achieved" by softening every
// message until none of them warns about anything.
func TestReleaseOutcomeAdvice_ContendedStillWarnsAboutTheStrand(t *testing.T) {
	advice := releaseOutcomeAdvice(reservationReleaseContended)
	if !strings.Contains(advice, "still dispatched") {
		t.Errorf("advice for %q = %q; a contended release leaves the item dispatched to a run that "+
			"will never execute it, and the operator has to be told", reservationReleaseContended, advice)
	}
	if !strings.Contains(advice, "boot reconciliation") {
		t.Errorf("advice for %q = %q; boot reconciliation is the only thing that clears this, and it "+
			"is the only actionable part of the message", reservationReleaseContended, advice)
	}
}

// An unrecognised verdict means somebody added one and did not come here. The
// safe answer is the cautious one: never tell an operator an item recovered
// when this function does not know that it did.
func TestReleaseOutcomeAdvice_UnknownVerdictDoesNotPromiseRecovery(t *testing.T) {
	advice := releaseOutcomeAdvice(reservationVerdict("verdict_added_after_this_test_was_written"))
	if strings.Contains(advice, "stranded nothing") {
		t.Errorf("advice for an unknown verdict = %q; it must not claim the item is fine", advice)
	}
	if advice == "" {
		t.Error("advice for an unknown verdict is empty; the report would then say nothing at all")
	}
}

// The bug this whole change exists for: the release write can fail, and the
// caller has to be told. The old path logged the error and carried on.
func TestReleaseReservation_WriteFailureIsReportedToTheCaller(t *testing.T) {
	const beadID = core.BeadID("hk-release-5")
	projectDir, store, runID := reserveForRelease(t, beadID)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring the fixture directory so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

	released := releaseReservation(context.Background(), store, projectDir, releaseTarget(beadID, runID), "claim_failed")
	if released.Verdict != reservationWriteFailed {
		t.Fatalf("verdict = %q; want %q — a lost release must not read as done",
			released.Verdict, reservationWriteFailed)
	}
	if released.Err == nil {
		t.Error("Err = nil; want the underlying write error, which is what names the operator's repair")
	}
}

// A failed release shuts the queue the same way a failed reservation does, and
// the refusal that follows must keep reading as a write failure. Calling it
// retry_later would turn a queue that will never accept another write into a
// silent spin.
func TestReleaseReservation_QuarantinedQueueStaysLoud(t *testing.T) {
	const beadID = core.BeadID("hk-release-6")
	projectDir, store, runID := reserveForRelease(t, beadID)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring the fixture directory so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

	release := releaseTarget(beadID, runID)
	if first := releaseReservation(context.Background(), store, projectDir, release, "claim_failed"); first.Verdict != reservationWriteFailed {
		t.Fatalf("first verdict = %q; want %q", first.Verdict, reservationWriteFailed)
	}

	// Repairing the directory must not un-quarantine the queue: QM-001 says the
	// daemon refuses further mutations, and recovery is an operator restart.
	if err := os.Chmod(queuesDir, 0o700); err != nil { //nolint:gosec // restoring write access to prove the quarantine is sticky
		t.Fatalf("chmod queues dir: %v", err)
	}
	second := releaseReservation(context.Background(), store, projectDir, release, "claim_failed")
	if second.Verdict != reservationWriteFailed {
		t.Errorf("verdict after quarantine = %q; want %q", second.Verdict, reservationWriteFailed)
	}
	if !errors.Is(second.Err, queuewiring.ErrQueueQuarantined) {
		t.Errorf("err = %v; want it to wrap ErrQueueQuarantined so the caller can tell it apart", second.Err)
	}
}

// bumpGeneration performs the raw write that every completion goroutine does:
// it installs the queue it just read, which advances the store generation and
// makes any snapshot taken before it stale.
//
// It also changes a byte, on the decoy item rather than the item under test.
// Transact checks the generation BEFORE it compares snapshot bytes, so a
// generation-only bump would exercise only the first of the two guards, and a
// future reordering would leave these tests passing for the wrong reason. A
// real completion goroutine changes both.
func bumpGeneration(store *queuewiring.QueueStore) {
	locked := store.LockForMutation()
	defer locked.Done()
	live := locked.LockedQueueByName(releaseQueueName)
	if live == nil {
		return
	}
	for gi := range live.Groups {
		for ii := range live.Groups[gi].Items {
			if live.Groups[gi].Items[ii].BeadID == releaseDecoyItemBead {
				live.Groups[gi].Items[ii].LastFailureReason += "x"
			}
		}
	}
	locked.LockedSetQueueByName(releaseQueueName, live)
}

// One optimistic pass against a snapshot that has gone stale must report
// contention, and it must write nothing. Reading this as a plain rejection is
// what would strand the item: nothing re-selects a dispatched item.
func TestReleaseAttempt_StaleSnapshotReportsContentionAndWritesNothing(t *testing.T) {
	const beadID = core.BeadID("hk-release-7")
	projectDir, store, runID := reserveForRelease(t, beadID)

	stale := store.Snapshot(releaseQueueName)
	bumpGeneration(store)

	attempt := releaseAttempt(context.Background(), store, projectDir, stale, releaseTarget(beadID, runID), "claim_failed")
	if attempt.Verdict != reservationReleaseContended {
		t.Fatalf("verdict = %q; want %q — a lost snapshot race must be distinguishable from a lost write",
			attempt.Verdict, reservationReleaseContended)
	}

	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted status = %q; want %q — a refused attempt must not write",
			persisted.Status, queue.ItemStatusDispatched)
	}
}

// The regression this loop exists to prevent. The raw revert it replaces held
// one lock across read-modify-write, so it could not lose this race; a
// single-shot transaction can, and losing it strands the item at dispatched
// where nothing will ever re-select it.
//
// The staleness is arranged, not raced for. A completion goroutine bumping the
// generation on the same queue is the real producer — evaluateGroupAdvanceWithOutcome
// does exactly this raw write on every run that finishes — but racing it for the
// window between the read and the write does not reliably reproduce a lost
// attempt, so a test built that way passes whether the loop retries or not.
func TestReleaseFrom_RetriesAfterLosingTheSnapshotRace(t *testing.T) {
	const beadID = core.BeadID("hk-release-8")
	projectDir, store, runID := reserveForRelease(t, beadID)

	// The first attempt is guaranteed to lose: this snapshot is stale before the
	// loop ever runs.
	stale := store.Snapshot(releaseQueueName)
	bumpGeneration(store)

	released := releaseFrom(context.Background(), store, projectDir, stale, releaseTarget(beadID, runID), "claim_failed")
	if released.Verdict != reservationReleased {
		t.Fatalf("verdict = %q (err %v); want %q — the release gave up on a stale snapshot and stranded the item",
			released.Verdict, released.Err, reservationReleased)
	}

	persisted, _, _ := loadReleasedItem(t, projectDir)
	if persisted.Status != queue.ItemStatusPending || persisted.RunID != nil {
		t.Errorf("persisted item = {%q, %v}; want {%q, nil}",
			persisted.Status, persisted.RunID, queue.ItemStatusPending)
	}
}
