package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// The group completion is the one path that marks a queue item terminal. Every
// case below asserts on the queue read back OFF DISK, never out of the store:
// the claim is durability, and the in-memory store agrees with a decision that
// never reached a file.
//
// Bead ref: hk-nw6on.

const (
	// completionQueueName is the queue every case completes on. The completion
	// is per-item, so a second queue name would add no coverage.
	completionQueueName = queue.QueueNameMain

	// The fixture holds TWO groups, and the items it completes sit in the
	// SECOND group behind a completed group and a pending sibling. A one-group
	// one-item fixture cannot tell a lookup that honours the group index, the
	// item index and the bead id from one hardcoded to the first of each.
	completionGroupIndex = 1

	// completionDecoyBead is the THIRD item. The stale-snapshot case changes a
	// byte on it as well as bumping the generation, because the write checks
	// the generation BEFORE it compares bytes: a generation-only bump would
	// exercise one of the two guards and leave a future reordering passing for
	// the wrong reason.
	completionDecoyBead = core.BeadID("hk-completion-decoy")

	completionFirstBead  = core.BeadID("hk-completion-first")
	completionSecondBead = core.BeadID("hk-completion-second")

	completionDecoyIndex  = 0
	completionFirstIndex  = 1
	completionSecondIndex = 2
)

// completionStamp is the terminal time every case reports. A fixed value keeps
// the persisted bytes comparable between runs.
var completionStamp = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

// completionFixture builds a project directory holding one active queue whose
// two target items sit behind a completed group and a pending sibling, and
// reserves both targets so they are durably dispatched.
//
// The pending sibling is what keeps the group active after both completions,
// so each case tests the item write rather than the queue-completion machinery.
func completionFixture(t *testing.T) (projectDir string, store *queuewiring.QueueStore, queueID string) {
	t.Helper()
	projectDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	queueID = newTestQueueID()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          completionQueueName,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusCompleteSuccess,
				Items:      []queue.Item{{BeadID: "hk-completion-decoy-group", Status: queue.ItemStatusCompleted}},
			},
			{
				GroupIndex: completionGroupIndex,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: completionDecoyBead, Status: queue.ItemStatusPending},
					{BeadID: completionFirstBead, Status: queue.ItemStatusPending},
					{BeadID: completionSecondBead, Status: queue.ItemStatusPending},
				},
			},
		},
	}
	store = queuewiring.NewQueueStore()
	store.SetQueueByName(completionQueueName, q)

	for _, target := range []struct {
		bead  core.BeadID
		index int
	}{
		{completionFirstBead, completionFirstIndex},
		{completionSecondBead, completionSecondIndex},
	} {
		reserved := reserveQueueItem(context.Background(), store, projectDir, queueReservation{
			QueueName:  completionQueueName,
			GroupIndex: completionGroupIndex,
			ItemIndex:  target.index,
			BeadID:     target.bead,
			RunID:      newReservationRunID(t),
		})
		if reserved.Verdict != reservationReserved {
			t.Fatalf("setup: reserve %s verdict = %q (err %v); want %q", target.bead, reserved.Verdict, reserved.Err, reservationReserved)
		}
	}
	return projectDir, store, queueID
}

// completionPort builds the seam the completion runs through. eagerRefill is
// left zero so the refill effect no-ops, and the emitter is a spy rather than
// nil because a committed completion emits its group intents.
func completionPort(projectDir string, store *queuewiring.QueueStore) reapSeamPort {
	return reapSeamPort{
		bus:           &intentDurabilityEmitter{},
		projectDir:    projectDir,
		queueStore:    store,
		runRegistry:   newLocalRunRegistry(),
		maxConcurrent: 2,
	}
}

// loadCompletionItems reads the active group's three items back off disk.
func loadCompletionItems(t *testing.T, projectDir string) (persisted queue.Queue, decoy, first, second queue.Item) {
	t.Helper()
	//nolint:gosec // the path is built from t.TempDir(); the test wrote this file itself
	raw, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", completionQueueName+".json"))
	if err != nil {
		t.Fatalf("read persisted queue: %v", err)
	}
	if unmarshalErr := json.Unmarshal(raw, &persisted); unmarshalErr != nil {
		t.Fatalf("unmarshal persisted queue: %v", unmarshalErr)
	}
	if len(persisted.Groups) != 2 || len(persisted.Groups[completionGroupIndex].Items) != 3 {
		t.Fatalf("persisted queue lost its shape: %s", raw)
	}
	items := persisted.Groups[completionGroupIndex].Items
	return persisted, items[completionDecoyIndex], items[completionFirstIndex], items[completionSecondIndex]
}

// staleCompletionSnapshot returns a snapshot that is guaranteed to lose, having
// invalidated the live queue against BOTH of the write's guards: the generation
// counter has moved and the bytes differ at the item the caller is not writing.
func staleCompletionSnapshot(store *queuewiring.QueueStore) queuewiring.Snapshot {
	stale := store.Snapshot(completionQueueName)
	locked := store.LockForMutation()
	defer locked.Done()
	live := locked.LockedQueueByName(completionQueueName)
	if live == nil {
		return stale
	}
	for gi := range live.Groups {
		for ii := range live.Groups[gi].Items {
			if live.Groups[gi].Items[ii].BeadID == completionDecoyBead {
				live.Groups[gi].Items[ii].LastFailureReason += "x"
			}
		}
	}
	// LockedSetQueueByName bumps the generation, so the snapshot above is now
	// stale on the generation guard as well as on the byte comparison.
	locked.LockedSetQueueByName(completionQueueName, live)
	return stale
}

// The regression the retry loop exists to prevent. The completion takes a
// snapshot under a read lock, decides off-lock, and compare-and-swaps. Losing
// that swap wrote NOTHING: the item stayed dispatched in memory and on disk,
// the run was already unregistered by the dispatch goroutine's deferred call so
// the stale watcher could not re-drive it, and nothing re-selects a dispatched
// item — so the group never reached all-terminal and the queue stalled forever.
//
// The staleness is arranged, not raced for. Five ordinary writers bump the
// generation on a live queue, including an eager refill fired by a sibling
// completion's own effects, but racing them for the window between the read and
// the write does not reliably reproduce a loss, so a test built that way passes
// whether the loop retries or not.
func TestEvaluateGroupAdvanceFrom_RetriesAfterLosingTheSnapshotRace(t *testing.T) {
	projectDir, store, queueID := completionFixture(t)
	port := completionPort(projectDir, store)

	// The first run finishes and records its outcome normally. Its write is one
	// of the generation bumps the second run has to survive.
	evaluateGroupAdvanceWithOutcome(t.Context(), port, completionQueueName, queueID,
		completionGroupIndex, completionFirstIndex, true, completionStamp)

	// The second run's snapshot is stale before the loop ever runs.
	stale := staleCompletionSnapshot(store)
	evaluateGroupAdvanceFrom(t.Context(), port, stale, groupCompletion{
		QueueName:   completionQueueName,
		QueueID:     queueID,
		GroupIndex:  completionGroupIndex,
		ItemIndex:   completionSecondIndex,
		Success:     true,
		CompletedAt: completionStamp,
	})

	_, decoy, first, second := loadCompletionItems(t, projectDir)
	if first.Status != queue.ItemStatusCompleted {
		t.Errorf("persisted first item = %q; want %q", first.Status, queue.ItemStatusCompleted)
	}
	if second.Status != queue.ItemStatusCompleted {
		t.Errorf("persisted second item = %q; want %q — the completion gave up on a stale snapshot, so this run's "+
			"outcome was never recorded and its group can never reach all-terminal",
			second.Status, queue.ItemStatusCompleted)
	}
	// The retry re-reads the live queue, so it must carry the byte another
	// writer changed rather than replay the caller's stale copy over it.
	if decoy.LastFailureReason != "x" {
		t.Errorf("persisted decoy LastFailureReason = %q; want %q — the retry wrote the stale snapshot back and "+
			"reverted another writer's change", decoy.LastFailureReason, "x")
	}
	if decoy.Status != queue.ItemStatusPending {
		t.Errorf("persisted decoy status = %q; want %q — the completion wrote the wrong item index",
			decoy.Status, queue.ItemStatusPending)
	}
}

// A drain means "stop dispatching and let in-flight runs finish". The
// completion refused every outcome on a queue that was not active, and its
// caller printed one stderr line and returned, so every run that finished after
// the pause was stuck at dispatched forever and the queue file was never
// updated — which makes the drain impossible to finish.
//
// specs/queue-model.md §QM-031 stops a PENDING group from starting while the
// queue is not active, and the operator surface says a drain stops the daemon
// advancing the queue while in-flight runs proceed. Recording the outcome of an
// item that is ALREADY dispatched is neither a dispatch nor an advance.
func TestEvaluateGroupAdvanceWithOutcome_RecordsAnOutcomeOnADrainingQueue(t *testing.T) {
	projectDir, store, queueID := completionFixture(t)
	port := completionPort(projectDir, store)
	pauseCompletionQueueForDrain(t, store)

	evaluateGroupAdvanceWithOutcome(t.Context(), port, completionQueueName, queueID,
		completionGroupIndex, completionFirstIndex, true, completionStamp)

	persisted, _, first, second := loadCompletionItems(t, projectDir)
	if first.Status != queue.ItemStatusCompleted {
		t.Errorf("persisted first item = %q; want %q — a run that finished during the drain lost its outcome, and "+
			"nothing re-selects a dispatched item", first.Status, queue.ItemStatusCompleted)
	}
	// The drain must survive the write. A completion that resumed the queue
	// would start dispatching again, which is the one thing a drain forbids.
	if persisted.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("persisted queue status = %q; want %q — recording an outcome must not resume a drained queue",
			persisted.Status, queue.QueueStatusPausedByDrain)
	}
	// The other in-flight run has not reported yet, so it stays dispatched.
	if second.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted second item = %q; want %q — the completion wrote an item it was not given",
			second.Status, queue.ItemStatusDispatched)
	}
}

// pauseCompletionQueueForDrain parks the fixture's queue the way an operator
// drain does, through the real transition rather than by assigning the status.
func pauseCompletionQueueForDrain(t *testing.T, store *queuewiring.QueueStore) {
	t.Helper()
	locked := store.LockForMutation()
	defer locked.Done()
	live := locked.LockedQueueByName(completionQueueName)
	if live == nil {
		t.Fatal("fixture queue is absent")
	}
	if err := queue.PauseQueueForDrain(live); err != nil {
		t.Fatalf("pause for drain: %v", err)
	}
	locked.LockedSetQueueByName(completionQueueName, live)
}

// finalCompletionFixture builds a project directory holding one active queue of
// ONE group and ONE item, reserved so it is durably dispatched. Completing that
// item is the last item of the last group, so it completes the whole queue.
func finalCompletionFixture(t *testing.T) (projectDir string, store *queuewiring.QueueStore, queueID string) {
	t.Helper()
	projectDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	queueID = newTestQueueID()
	store = queuewiring.NewQueueStore()
	store.SetQueueByName(completionQueueName, &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          completionQueueName,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: completionFirstBead, Status: queue.ItemStatusPending}},
		}},
	})
	reserved := reserveQueueItem(context.Background(), store, projectDir, queueReservation{
		QueueName:  completionQueueName,
		GroupIndex: 0,
		ItemIndex:  0,
		BeadID:     completionFirstBead,
		RunID:      newReservationRunID(t),
	})
	if reserved.Verdict != reservationReserved {
		t.Fatalf("setup: reserve verdict = %q (err %v); want %q", reserved.Verdict, reserved.Err, reservationReserved)
	}
	return projectDir, store, queueID
}

// The second load-bearing consequence, read off disk. A drain that lands on the
// LAST group's last item must still complete the queue. Requiring an active
// queue traded one stall for another: all the work done, the canonical file
// never unlinked and the queue name never released — and the boot
// reconciliation pass re-derives the same refusal, so nothing heals it.
func TestEvaluateGroupAdvanceWithOutcome_CompletesTheQueueOnADrainingQueue(t *testing.T) {
	projectDir, store, queueID := finalCompletionFixture(t)
	port := completionPort(projectDir, store)
	pauseCompletionQueueForDrain(t, store)

	evaluateGroupAdvanceWithOutcome(t.Context(), port, completionQueueName, queueID,
		0, 0, true, completionStamp)

	// The completed canonical file is unlinked and the name is released. A
	// queue file still on disk is the stall this case exists to catch.
	canonical := filepath.Join(projectDir, ".harmonik", "queues", completionQueueName+".json")
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(canonical) //nolint:errcheck,gosec // best-effort detail for the failure message; the path is under t.TempDir()
		t.Fatalf("canonical queue file still exists (stat err %v): %s — the queue name was never released and the "+
			"boot reconciliation pass cannot heal it", err, raw)
	}
	if live := store.Snapshot(completionQueueName); live.Queue != nil {
		t.Errorf("store still holds the queue with status %q; want the name released", live.Queue.Status)
	}
}

// The first load-bearing consequence, read off disk. A group can fail DURING a
// clean-shutdown pause. Leaving the queue paused-by-drain lets the resume bit
// auto-resume it on the next daemon start, and the queue then starts the
// successor group PAST a group that failed.
func TestEvaluateGroupAdvanceWithOutcome_GroupFailureOutranksARestartPause(t *testing.T) {
	projectDir, store, queueID := completionFixture(t)
	port := completionPort(projectDir, store)
	// The decoy is the third pending item; fail it too so the group can reach
	// all-terminal from the two completions below.
	failDecoyItem(t, store, projectDir)
	pauseCompletionQueueForRestart(t, store)

	evaluateGroupAdvanceWithOutcome(t.Context(), port, completionQueueName, queueID,
		completionGroupIndex, completionFirstIndex, true, completionStamp)
	evaluateGroupAdvanceWithOutcome(t.Context(), port, completionQueueName, queueID,
		completionGroupIndex, completionSecondIndex, false, completionStamp)

	persisted, _, first, second := loadCompletionItems(t, projectDir)
	if first.Status != queue.ItemStatusCompleted || second.Status != queue.ItemStatusFailed {
		t.Fatalf("persisted items = %q / %q; want %q / %q", first.Status, second.Status,
			queue.ItemStatusCompleted, queue.ItemStatusFailed)
	}
	if persisted.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("persisted queue status = %q; want %q — a queue left paused-by-drain auto-resumes on the next "+
			"daemon start and then runs the successor of a failed group",
			persisted.Status, queue.QueueStatusPausedByFailure)
	}
	if persisted.ResumeOnStart {
		t.Error("persisted resume-on-start is still set — the next daemon start would resume a queue an operator " +
			"has to look at")
	}
	if persisted.Groups[completionGroupIndex].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("persisted group status = %q; want %q",
			persisted.Groups[completionGroupIndex].Status, queue.GroupStatusCompleteWithFailures)
	}
}

// failDecoyItem marks the fixture's third item failed through the reservation
// owner, so the group can reach all-terminal on the two reported outcomes.
func failDecoyItem(t *testing.T, store *queuewiring.QueueStore, projectDir string) {
	t.Helper()
	failed := failQueueItem(context.Background(), store, projectDir, queueReservation{
		QueueName:  completionQueueName,
		GroupIndex: completionGroupIndex,
		ItemIndex:  completionDecoyIndex,
		BeadID:     completionDecoyBead,
	}, "fixture_decoy")
	if failed.Verdict != reservationItemFailed {
		t.Fatalf("setup: fail decoy verdict = %q (err %v)", failed.Verdict, failed.Err)
	}
}

// pauseCompletionQueueForRestart parks the fixture's queue the way a clean
// daemon shutdown does, which is the pause that sets the resume-on-start bit.
func pauseCompletionQueueForRestart(t *testing.T, store *queuewiring.QueueStore) {
	t.Helper()
	locked := store.LockForMutation()
	defer locked.Done()
	live := locked.LockedQueueByName(completionQueueName)
	if live == nil {
		t.Fatal("fixture queue is absent")
	}
	if err := queue.PauseQueueForRestart(live); err != nil {
		t.Fatalf("pause for restart: %v", err)
	}
	if !live.ResumeOnStart {
		t.Fatal("fixture: the restart pause did not set the resume bit, so this case proves nothing")
	}
	locked.LockedSetQueueByName(completionQueueName, live)
}

// ── the give-up branch ───────────────────────────────────────────────────────

// perpetuallyStaleLedger invalidates the caller's snapshot from INSIDE the
// completion attempt, which is the only place a test can reach.
//
// The retry loop re-reads a fresh snapshot after every loss, so arranging one
// stale snapshot up front loses exactly one attempt. Losing all of them needs a
// writer that moves the queue between the loop's read and the attempt's write —
// and the failure-propagation ledger is called in precisely that window, on
// every attempt, before the transaction runs.
//
// The sibling's technique does NOT port. releaseFrom's give-up test forces its
// losses with a cancelled context, but a cancelled context is not a stale
// snapshot: under the positive ErrStaleSnapshot classification it settles
// rather than contends, so copying that here would exercise the wrong branch
// and pass for the wrong reason.
//
// BlocksEdge answers false, so nothing is propagated and the group keeps its
// shape. The bump is the whole point of the fake; the answer is incidental.
type perpetuallyStaleLedger struct {
	store *queuewiring.QueueStore

	// budget is the number of attempts this fake spoils, written as a literal
	// rather than read from groupCompletionRetryBudget. The control that proves
	// this test bites RAISES that constant so a later attempt lands; a fake
	// keyed to the constant would raise with it and keep the test green.
	budget int

	calls int
}

func (l *perpetuallyStaleLedger) LookupStatus(context.Context, core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (l *perpetuallyStaleLedger) BlocksEdge(context.Context, core.BeadID, core.BeadID) (bool, error) {
	l.calls++
	if l.calls > l.budget {
		return false, nil
	}
	locked := l.store.LockForMutation()
	defer locked.Done()
	live := locked.LockedQueueByName(completionQueueName)
	if live == nil {
		return false, nil
	}
	// Move the bytes as well as the counter. The write checks the generation
	// BEFORE it compares bytes, so a counter-only bump would exercise one of
	// the two guards and leave a future reordering passing for the wrong
	// reason. This writes to the store only — never to disk — so the on-disk
	// assertion below is about the completion alone.
	for gi := range live.Groups {
		for ii := range live.Groups[gi].Items {
			if live.Groups[gi].Items[ii].BeadID == completionDecoyBead {
				live.Groups[gi].Items[ii].LastFailureReason += "x"
			}
		}
	}
	locked.LockedSetQueueByName(completionQueueName, live)
	return false, nil
}

// strandedCompletionFixture builds a project directory holding one active queue
// whose group holds a DEFERRED decoy and one dispatched target.
//
// The decoy is deferred rather than pending because that is what makes the
// ledger run: FailDeferredDependents only consults it for items in the
// ledger-deferred state. Deferred is not terminal, so the group stays active
// and the completion reaches the ordinary intermediate write.
func strandedCompletionFixture(t *testing.T) (projectDir string, store *queuewiring.QueueStore, queueID string) {
	t.Helper()
	projectDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	queueID = newTestQueueID()
	store = queuewiring.NewQueueStore()
	store.SetQueueByName(completionQueueName, &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          completionQueueName,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: completionDecoyBead, Status: queue.ItemStatusDeferredForLedgerDep},
				{BeadID: completionFirstBead, Status: queue.ItemStatusPending},
			},
		}},
	})
	reserved := reserveQueueItem(context.Background(), store, projectDir, queueReservation{
		QueueName:  completionQueueName,
		GroupIndex: 0,
		ItemIndex:  1,
		BeadID:     completionFirstBead,
		RunID:      newReservationRunID(t),
	})
	if reserved.Verdict != reservationReserved {
		t.Fatalf("setup: reserve verdict = %q (err %v); want %q", reserved.Verdict, reserved.Err, reservationReserved)
	}
	return projectDir, store, queueID
}

// The give-up branch is the one path that admits defeat, and until now it was
// the one path nothing exercised. A bounded budget is only worth having if
// running out SAYS the item is stranded instead of failing silently.
//
// Two things are asserted, and both matter. The report must name the
// consequence, because the symptom is a queue that reads as slow rather than as
// an error. And the item must still be dispatched on disk: a path that gave up
// must not claim to have recorded anything.
func TestEvaluateGroupAdvanceFrom_GivesUpAfterTheBudgetAndSaysTheItemIsStranded(t *testing.T) {
	projectDir, store, queueID := strandedCompletionFixture(t)
	port := completionPort(projectDir, store)
	ledger := &perpetuallyStaleLedger{store: store, budget: 3}
	port.queueLedger = ledger

	stderr := completionCaptureStderr(t, func() {
		evaluateGroupAdvanceFrom(t.Context(), port, store.Snapshot(completionQueueName), groupCompletion{
			QueueName:   completionQueueName,
			QueueID:     queueID,
			GroupIndex:  0,
			ItemIndex:   1,
			Success:     false,
			CompletedAt: completionStamp,
		})
	})

	// Every attempt reached the ledger, so every attempt really did run and
	// really did lose. A test where the loop exited early would still see the
	// stranded line if the budget were misread, but it would not see this.
	if ledger.calls != groupCompletionRetryBudget {
		t.Errorf("ledger was consulted %d times; want %d — the loop did not spend its whole budget",
			ledger.calls, groupCompletionRetryBudget)
	}
	if !strings.Contains(stderr, "GROUP COMPLETION STRANDED") {
		t.Errorf("stderr did not report a strand; got: %s", stderr)
	}
	for _, phrase := range []string{
		"still dispatched",
		"cannot reach all-terminal",
		"nothing re-selects a dispatched item",
	} {
		if !strings.Contains(stderr, phrase) {
			t.Errorf("stranded report does not name the consequence %q — an operator reads this as a slow queue "+
				"rather than a stall; got: %s", phrase, stderr)
		}
	}

	// The give-up path must not claim to have recorded anything.
	persisted := loadStrandedQueue(t, projectDir)
	target := persisted.Groups[0].Items[1]
	if target.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted item = %q; want %q — the completion gave up and still wrote an outcome",
			target.Status, queue.ItemStatusDispatched)
	}
	if persisted.Groups[0].Status != queue.GroupStatusActive {
		t.Errorf("persisted group = %q; want %q", persisted.Groups[0].Status, queue.GroupStatusActive)
	}
}

// loadStrandedQueue reads the one-group fixture's queue back off disk.
func loadStrandedQueue(t *testing.T, projectDir string) queue.Queue {
	t.Helper()
	//nolint:gosec // the path is built from t.TempDir(); the test wrote this file itself
	raw, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", completionQueueName+".json"))
	if err != nil {
		t.Fatalf("read persisted queue: %v", err)
	}
	var persisted queue.Queue
	if unmarshalErr := json.Unmarshal(raw, &persisted); unmarshalErr != nil {
		t.Fatalf("unmarshal persisted queue: %v", unmarshalErr)
	}
	if len(persisted.Groups) != 1 || len(persisted.Groups[0].Items) != 2 {
		t.Fatalf("persisted queue lost its shape: %s", raw)
	}
	return persisted
}

// completionCaptureStderr runs fn with os.Stderr redirected to a pipe and
// returns everything fn wrote there. The reader is drained on its own goroutine
// so a writer that outruns the pipe buffer cannot deadlock the test.
func completionCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	var buf bytes.Buffer
	drained := make(chan error, 1)
	go func() {
		_, copyErr := buf.ReadFrom(r)
		drained <- copyErr
	}()

	fn()

	os.Stderr = orig
	if closeErr := w.Close(); closeErr != nil {
		t.Errorf("close pipe writer: %v", closeErr)
	}
	if copyErr := <-drained; copyErr != nil {
		t.Errorf("read captured stderr: %v", copyErr)
	}
	if closeErr := r.Close(); closeErr != nil {
		t.Errorf("close pipe reader: %v", closeErr)
	}
	return buf.String()
}
