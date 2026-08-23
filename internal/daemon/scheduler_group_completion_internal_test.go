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

const (
	completionQueueName = queue.QueueNameMain

	completionGroupIndex = 1

	completionDecoyBead = core.BeadID("hk-completion-decoy")

	completionFirstBead  = core.BeadID("hk-completion-first")
	completionSecondBead = core.BeadID("hk-completion-second")

	completionDecoyIndex  = 0
	completionFirstIndex  = 1
	completionSecondIndex = 2
)

var completionStamp = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

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
			QueueID:    queueID,
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

func completionPort(projectDir string, store *queuewiring.QueueStore) reapSeamPort {
	return reapSeamPort{
		bus:           &intentDurabilityEmitter{},
		projectDir:    projectDir,
		queueStore:    store,
		runRegistry:   newLocalRunRegistry(),
		maxConcurrent: 2,
	}
}

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

	evaluateGroupAdvanceWithOutcome(t.Context(), port, completionQueueName, queueID,
		completionGroupIndex, completionFirstIndex, true, completionStamp)

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
	if persisted.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("persisted queue status = %q; want %q — recording an outcome must not resume a drained queue",
			persisted.Status, queue.QueueStatusPausedByDrain)
	}
	if second.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted second item = %q; want %q — the completion wrote an item it was not given",
			second.Status, queue.ItemStatusDispatched)
	}
}

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
		QueueID:    queueID,
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

func failDecoyItem(t *testing.T, store *queuewiring.QueueStore, projectDir string) {
	t.Helper()
	snapshot := store.Snapshot(completionQueueName)
	if snapshot.Queue == nil {
		t.Fatal("setup: completion queue is missing")
	}
	failed := failQueueItem(context.Background(), store, projectDir, queueReservation{
		QueueName:         completionQueueName,
		QueueID:           snapshot.Queue.QueueID,
		GroupIndex:        completionGroupIndex,
		ItemIndex:         completionDecoyIndex,
		BeadID:            completionDecoyBead,
		RunID:             newReservationRunID(t),
		ClaimTransitionID: newReservationTransitionID(t),
	}, "fixture_decoy", queue.PreclaimTerminalCrossQueue)
	if failed.Verdict != reservationItemFailed {
		t.Fatalf("setup: fail decoy verdict = %q (err %v)", failed.Verdict, failed.Err)
	}
}

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
		QueueID:    queueID,
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
