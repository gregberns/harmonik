package queuewiring

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

const (
	storeCompletionQueueID       = "0197c452-0000-7000-8000-000000000001"
	storeCompletionTransactionID = "0197c452-0000-7000-8000-000000000002"
	storeCompletionReceiptID     = "0197c452-0000-7000-8000-000000000003"
)

func storeCompletionFixture() (*queue.Queue, time.Time) {
	stamp := time.Date(2026, 8, 10, 18, 0, 0, 123456000, time.UTC)
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       storeCompletionQueueID,
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: core.BeadID("hk-store-complete"), Status: queue.ItemStatusDispatched}},
		}},
	}, stamp
}

func storeCompletionInput(q *queue.Queue, stamp time.Time) queue.GroupCompletionInput {
	return queue.GroupCompletionInput{
		ExpectedQueueID:     q.QueueID,
		Location:            queue.GroupCompletionLocation{GroupIndex: 0, ItemIndex: 0},
		Outcome:             queue.GroupCompletionOutcomeCompleted,
		CompletedAt:         stamp,
		CompletionReceiptID: storeCompletionReceiptID,
	}
}

func storeCompletionCandidate(t testing.TB, q *queue.Queue, stamp time.Time) *queue.Queue {
	t.Helper()
	result, err := queue.DecideGroupCompletion(*q, storeCompletionInput(q, stamp))
	if err != nil || result.Disposition != queue.GroupCompletionDispositionQueueCompleted {
		t.Fatalf("completion fixture decision: disposition=%q err=%v", result.Disposition, err)
	}
	return result.NextQueue
}

func TestQueueStoreCompleteOrdersReceiptObservationCleanupAndRelease(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}
	observationCalls := 0
	releaseSampled := false
	wantPlan, err := queue.PrepareCompletion(*storeCompletionCandidate(t, q, stamp), storeCompletionTransactionID, storeCompletionReceiptID, stamp)
	if err != nil {
		t.Fatal(err)
	}
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		ReleaseTime: func() time.Time {
			releaseSampled = true
			if snapshot := store.Snapshot(queue.QueueNameMain); snapshot.Queue != nil {
				t.Fatalf("release time sampled while ownership remained: %+v", snapshot.Queue)
			}
			return stamp.Add(time.Minute)
		},
		Observe: func(receipt queue.CompletionReceipt) error {
			observationCalls++
			if receipt != wantPlan.Receipt {
				t.Fatalf("observed receipt = %+v, want %+v", receipt, wantPlan.Receipt)
			}
			before := store.Snapshot(queue.QueueNameMain)
			if before.Queue == nil || before.Queue.Status != queue.QueueStatusCompleted {
				t.Fatalf("observer could not read completed snapshot: %+v", before.Queue)
			}
			replacement := queue.CloneQueue(before.Queue)
			replacement.Workers++
			store.SetQueueByName(queue.QueueNameMain, replacement)
			store.ClearQueueByName(queue.QueueNameMain)

			locked := store.LockForMutation()
			aliased := locked.LockedQueueByName(queue.QueueNameMain)
			if aliased != nil {
				locked.Done()
				t.Fatalf("locked mutation view exposed quarantined owner: %+v", aliased)
			}
			locked.LockedSetQueueByName(queue.QueueNameMain, replacement)
			locked.Done()

			after := store.Snapshot(queue.QueueNameMain)
			if !sameQueue(after.Queue, before.Queue) || after.Generation != before.Generation {
				t.Fatalf("observer-window writer changed completion owner: before=%+v after=%+v", before, after)
			}
			canonical, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", "main.json")) //nolint:gosec // path is under t.TempDir
			if err != nil || len(canonical) == 0 {
				t.Fatalf("canonical was not present at observation: %v", err)
			}
			receiptPath := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts", receipt.QueueID+"--"+receipt.ReceiptID+".json")
			if _, err := os.Stat(receiptPath); err != nil {
				t.Fatalf("receipt was not durable at observation: %v", err)
			}
			return errors.New("observation failure does not gate cleanup")
		},
	})
	if observationCalls != 1 {
		t.Fatalf("completion observation calls = %d, want 1", observationCalls)
	}
	if !releaseSampled {
		t.Fatal("release time was not sampled")
	}
	if result.Phase != queue.CompletionPhaseMarkerDurable || !result.Committed() {
		t.Fatalf("result = %+v", result)
	}
	if result.ObservationErr == nil {
		t.Fatal("observation error was not reported")
	}
	if snapshot := store.Snapshot(queue.QueueNameMain); snapshot.Queue != nil {
		t.Fatalf("queue ownership remains after release: %+v", snapshot.Queue)
	}
	for _, path := range []string{
		filepath.Join(projectDir, ".harmonik", "queues", "main.json"),
		filepath.Join(projectDir, ".harmonik", "queues", "main.replace-intent"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cleanup path %s: %v", path, err)
		}
	}
}

func TestQueueStoreCompleteReceiptFailureRetainsPreReleaseOwnership(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}
	receiptRoot := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts")
	externalRoot := t.TempDir()
	if err := os.Symlink(externalRoot, receiptRoot); err != nil {
		t.Fatal(err)
	}
	observationCalls := 0
	releaseCalls := 0
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		ReleaseTime: func() time.Time {
			releaseCalls++
			return stamp.Add(time.Minute)
		},
		Observe: func(queue.CompletionReceipt) error {
			observationCalls++
			return nil
		},
	})
	if result.Phase != queue.CompletionPhaseCanonicalCommitted || result.Outcome != queue.OutcomeCommitIndeterminate || result.Err == nil {
		t.Fatalf("result = %+v", result)
	}
	if observationCalls != 0 || releaseCalls != 0 {
		t.Fatalf("pre-release effects: observation=%d release=%d", observationCalls, releaseCalls)
	}
	retained := store.Snapshot(queue.QueueNameMain)
	if !sameQueue(retained.Queue, q) {
		t.Fatalf("receipt failure changed live owner: %+v", retained.Queue)
	}
	transact := store.Transact(t.Context(), queue.TransactionRequest{
		Snapshot:      retained,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationMaintenance,
		Mutate:        func(*queue.Queue) error { return nil },
	})
	if !errors.Is(transact.Err, ErrQueueQuarantined) {
		t.Fatalf("receipt failure did not quarantine owner: %v", transact.Err)
	}
	canonical, err := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
	if err != nil || canonical == nil || canonical.Status != queue.QueueStatusCompleted {
		t.Fatalf("completed canonical = %+v, err=%v", canonical, err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", "main.replace-intent")); err != nil {
		t.Fatalf("completion intent missing: %v", err)
	}
	if entries, err := os.ReadDir(externalRoot); err != nil || len(entries) != 0 {
		t.Fatalf("symlink target changed: entries=%v err=%v", entries, err)
	}
}

func TestQueueStoreCompleteCleanupFailureRetainsOwnershipAndQuarantine(t *testing.T) {
	for _, tc := range []struct {
		name             string
		cleanupCanonical func(string, string, string, string) error
		cleanupIntent    func(string, string) error
	}{
		{
			name: "canonical remove",
			cleanupCanonical: func(string, string, string, string) error {
				return errors.New("cut canonical remove")
			},
			cleanupIntent: queue.CleanupReplaceIntent,
		},
		{
			name:             "intent remove",
			cleanupCanonical: queue.CleanupCompletedCanonical,
			cleanupIntent: func(string, string) error {
				return errors.New("cut intent remove")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewQueueStore()
			q, stamp := storeCompletionFixture()
			store.SetQueue(q)
			projectDir := t.TempDir()
			if err := queue.Persist(t.Context(), projectDir, q); err != nil {
				t.Fatal(err)
			}
			result := store.complete(t.Context(), queue.CompletionRequest{
				Snapshot:      store.Snapshot(queue.QueueNameMain),
				Candidate:     storeCompletionCandidate(t, q, stamp),
				DecisionInput: storeCompletionInput(q, stamp),
				ProjectDir:    projectDir,
				TransactionID: storeCompletionTransactionID,
				ReceiptID:     storeCompletionReceiptID,
				CompletedAt:   stamp,
				ReleaseTime:   func() time.Time { return stamp.Add(time.Minute) },
				Observe:       func(queue.CompletionReceipt) error { return nil },
			}, tc.cleanupCanonical, tc.cleanupIntent, queue.InstallCompletionReleaseMarker)
			if result.CleanupErr == nil || result.Phase != queue.CompletionPhaseObservationAttempted {
				t.Fatalf("result = %+v", result)
			}
			retained := store.Snapshot(queue.QueueNameMain)
			if retained.Queue == nil || retained.Queue.Status != queue.QueueStatusCompleted {
				t.Fatalf("completion ownership was released: %+v", retained.Queue)
			}
			transact := store.Transact(t.Context(), queue.TransactionRequest{
				Snapshot:      retained,
				ProjectDir:    projectDir,
				OperationKind: queue.OperationMaintenance,
				Mutate:        func(*queue.Queue) error { return nil },
			})
			if !errors.Is(transact.Err, ErrQueueQuarantined) {
				t.Fatalf("retained name was not quarantined: %v", transact.Err)
			}
		})
	}
}

func TestQueueStoreCompleteRetainsExactOwnerAtPostUnlinkFaults(t *testing.T) {
	for _, tc := range []struct {
		name             string
		absentPath       string
		cleanupCanonical func(string, string, string, string) error
		cleanupIntent    func(string, string) error
	}{
		{
			name:       "canonical unlinked before directory durability",
			absentPath: filepath.Join(".harmonik", "queues", "main.json"),
			cleanupCanonical: func(projectDir, name, _, _ string) error {
				if err := os.Remove(filepath.Join(projectDir, ".harmonik", "queues", name+".json")); err != nil {
					return err
				}
				return errors.New("cut canonical directory sync")
			},
			cleanupIntent: queue.CleanupReplaceIntent,
		},
		{
			name:             "intent unlinked before directory durability",
			absentPath:       filepath.Join(".harmonik", "queues", "main.replace-intent"),
			cleanupCanonical: queue.CleanupCompletedCanonical,
			cleanupIntent: func(projectDir, name string) error {
				if err := os.Remove(filepath.Join(projectDir, ".harmonik", "queues", name+".replace-intent")); err != nil {
					return err
				}
				return errors.New("cut intent directory sync")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewQueueStore()
			q, stamp := storeCompletionFixture()
			store.SetQueue(q)
			projectDir := t.TempDir()
			if err := queue.Persist(t.Context(), projectDir, q); err != nil {
				t.Fatal(err)
			}
			observationCalls := 0
			releaseCalls := 0
			markerCalls := 0
			result := store.complete(t.Context(), queue.CompletionRequest{
				Snapshot:      store.Snapshot(queue.QueueNameMain),
				Candidate:     storeCompletionCandidate(t, q, stamp),
				DecisionInput: storeCompletionInput(q, stamp),
				ProjectDir:    projectDir,
				TransactionID: storeCompletionTransactionID,
				ReceiptID:     storeCompletionReceiptID,
				CompletedAt:   stamp,
				ReleaseTime: func() time.Time {
					releaseCalls++
					return stamp.Add(time.Minute)
				},
				Observe: func(queue.CompletionReceipt) error {
					observationCalls++
					return nil
				},
			}, tc.cleanupCanonical, tc.cleanupIntent,
				func(string, queue.CompletionReleaseMarkerInputs, time.Time) (queue.CompletionReleaseMarker, error) {
					markerCalls++
					return queue.CompletionReleaseMarker{}, nil
				},
			)
			if result.Phase != queue.CompletionPhaseObservationAttempted || result.CleanupErr == nil {
				t.Fatalf("result = %+v", result)
			}
			if observationCalls != 1 || releaseCalls != 0 || markerCalls != 0 {
				t.Fatalf("calls: observe=%d release=%d marker=%d", observationCalls, releaseCalls, markerCalls)
			}
			if _, err := os.Lstat(filepath.Join(projectDir, tc.absentPath)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("selected cleanup path still exists or could not be classified: %v", err)
			}
			retained := store.Snapshot(queue.QueueNameMain)
			if retained.Queue == nil || retained.Queue.Status != queue.QueueStatusCompleted {
				t.Fatalf("completed owner was not retained: %+v", retained.Queue)
			}
			generation := retained.Generation
			transact := store.Transact(t.Context(), queue.TransactionRequest{
				Snapshot:      retained,
				ProjectDir:    projectDir,
				OperationKind: queue.OperationMaintenance,
				Mutate:        func(*queue.Queue) error { return nil },
			})
			if !errors.Is(transact.Err, ErrQueueQuarantined) {
				t.Fatalf("post-unlink fault did not quarantine owner: %v", transact.Err)
			}
			if after := store.Snapshot(queue.QueueNameMain); after.Generation != generation || !sameQueue(after.Queue, retained.Queue) {
				t.Fatalf("refused writer changed retained owner: before=%+v after=%+v", retained, after)
			}
		})
	}
}

func TestQueueStoreCompleteMarkerFailureKeepsReleasedNameAndRetriesSameReceipt(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}
	markerFailure := errors.New("cut marker install")
	result := store.complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		ReleaseTime:   func() time.Time { return stamp.Add(time.Minute) },
		Observe:       func(queue.CompletionReceipt) error { return nil },
	}, queue.CleanupCompletedCanonical, queue.CleanupReplaceIntent,
		func(string, queue.CompletionReleaseMarkerInputs, time.Time) (queue.CompletionReleaseMarker, error) {
			return queue.CompletionReleaseMarker{}, markerFailure
		},
	)
	if result.Phase != queue.CompletionPhaseMarkerFailed || !errors.Is(result.MarkerErr, markerFailure) {
		t.Fatalf("result = %+v", result)
	}
	if snapshot := store.Snapshot(queue.QueueNameMain); snapshot.Queue != nil {
		t.Fatalf("marker failure reacquired ownership: %+v", snapshot.Queue)
	}

	newQueue := queue.CloneQueue(q)
	newQueue.QueueID = "0197c452-0000-7000-8000-000000000099"
	store.SetQueueByName(queue.QueueNameMain, newQueue)
	if snapshot := store.Snapshot(queue.QueueNameMain); snapshot.Queue == nil || snapshot.Queue.QueueID != newQueue.QueueID {
		t.Fatalf("released name did not accept a new identity: %+v", snapshot.Queue)
	}

	prepared, err := queue.PrepareCompletion(*storeCompletionCandidate(t, q, stamp), storeCompletionTransactionID, storeCompletionReceiptID, stamp)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := queue.InstallCompletionReleaseMarker(projectDir, prepared.MarkerInputs, stamp.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := queue.InstallCompletionReleaseMarker(projectDir, prepared.MarkerInputs, stamp.Add(3*time.Minute))
	if err != nil || repeated != marker {
		t.Fatalf("marker retry = %+v, err=%v; want %+v", repeated, err, marker)
	}
}

func TestQueueStoreCompleteMarkerInstallAmbiguityKeepsNameReleased(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}
	observationCalls := 0
	releaseCalls := 0
	markerCalls := 0
	cut := errors.New("marker root sync reported failure after install")
	result := store.complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		ReleaseTime: func() time.Time {
			releaseCalls++
			return stamp.Add(time.Minute)
		},
		Observe: func(queue.CompletionReceipt) error {
			observationCalls++
			return nil
		},
	}, queue.CleanupCompletedCanonical, queue.CleanupReplaceIntent,
		func(projectDir string, inputs queue.CompletionReleaseMarkerInputs, releasedAt time.Time) (queue.CompletionReleaseMarker, error) {
			markerCalls++
			marker, err := queue.InstallCompletionReleaseMarker(projectDir, inputs, releasedAt)
			if err != nil {
				return queue.CompletionReleaseMarker{}, err
			}
			return marker, cut
		},
	)
	if result.Phase != queue.CompletionPhaseMarkerFailed || !errors.Is(result.MarkerErr, cut) {
		t.Fatalf("result = %+v", result)
	}
	if observationCalls != 1 || releaseCalls != 1 || markerCalls != 1 {
		t.Fatalf("calls: observe=%d release=%d marker=%d", observationCalls, releaseCalls, markerCalls)
	}
	if snapshot := store.Snapshot(queue.QueueNameMain); snapshot.Queue != nil {
		t.Fatalf("marker ambiguity reacquired released owner: %+v", snapshot.Queue)
	}
	markerBasename, err := queue.CompletionReleaseMarkerBasename(q.QueueID, storeCompletionReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts", markerBasename)
	firstBytes, err := os.ReadFile(markerPath) //nolint:gosec // path is under t.TempDir and uses validated IDs.
	if err != nil {
		t.Fatal(err)
	}
	newQueue := queue.CloneQueue(q)
	newQueue.QueueID = "0197c452-0000-7000-8000-000000000099"
	store.SetQueueByName(queue.QueueNameMain, newQueue)
	if got := store.Snapshot(queue.QueueNameMain).Queue; got == nil || got.QueueID != newQueue.QueueID {
		t.Fatalf("released name refused newer identity: %+v", got)
	}
	retryTimeCalls := 0
	recovered, err := queue.RecoverCompletionReleaseMarkers(projectDir, func() time.Time {
		retryTimeCalls++
		return stamp.Add(2 * time.Minute)
	})
	if err != nil || len(recovered) != 1 || recovered[0].Err != nil {
		t.Fatalf("marker recovery = %+v, err=%v", recovered, err)
	}
	if retryTimeCalls != 0 {
		t.Fatalf("existing marker resampled release time %d times", retryTimeCalls)
	}
	secondBytes, err := os.ReadFile(markerPath) //nolint:gosec // path is under t.TempDir and uses validated IDs.
	if err != nil || !bytes.Equal(secondBytes, firstBytes) {
		t.Fatalf("marker changed on retry: err=%v", err)
	}
}

func TestQueueStoreCompleteRequiresObserverBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
	})
	if result.Phase != queue.CompletionPhaseRejected || result.Outcome != queue.OutcomeRejected {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing observer performed I/O: %v", err)
	}
	retained := store.Snapshot(queue.QueueNameMain)
	if retained.Queue == nil || retained.Queue.QueueID != q.QueueID {
		t.Fatalf("missing observer released ownership: %+v", retained.Queue)
	}
}

func TestQueueStoreCompleteRequiresReleaseTimeBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		Observe:       func(queue.CompletionReceipt) error { return nil },
	})
	if result.Phase != queue.CompletionPhaseRejected || result.Outcome != queue.OutcomeRejected {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing release time performed I/O: %v", err)
	}
	if retained := store.Snapshot(queue.QueueNameMain); retained.Queue == nil || retained.Queue.QueueID != q.QueueID {
		t.Fatalf("missing release time released ownership: %+v", retained.Queue)
	}
}

func TestQueueStoreCompleteRejectsStaleSnapshotBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	stale := store.Snapshot(queue.QueueNameMain)
	store.SetQueue(q)
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      stale,
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
	})
	if result.Phase != queue.CompletionPhaseRejected || result.Outcome != queue.OutcomeRejected {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale completion performed I/O: %v", err)
	}
}

func TestQueueStoreCompleteRejectsCandidateThatDoesNotMatchDecisionBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	candidate := storeCompletionCandidate(t, q, stamp)
	candidate.Workers++
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     candidate,
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		ReleaseTime:   func() time.Time { return stamp.Add(time.Minute) },
		Observe:       func(queue.CompletionReceipt) error { return nil },
	})
	if result.Phase != queue.CompletionPhaseRejected || result.Outcome != queue.OutcomeRejected {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched candidate performed I/O: %v", err)
	}
	retained := store.Snapshot(queue.QueueNameMain)
	if !sameQueue(retained.Queue, q) {
		t.Fatalf("mismatched candidate changed live queue: %+v", retained.Queue)
	}
}

func TestQueueStoreCompleteRejectsReceiptThatDoesNotMatchDecisionBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     storeCompletionCandidate(t, q, stamp),
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     "0197c452-0000-7000-8000-000000000099",
		CompletedAt:   stamp,
		ReleaseTime:   func() time.Time { return stamp.Add(time.Minute) },
		Observe:       func(queue.CompletionReceipt) error { return nil },
	})
	if result.Phase != queue.CompletionPhaseRejected || result.Outcome != queue.OutcomeRejected {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched receipt performed I/O: %v", err)
	}
}

func TestQueueStoreCompleteRejectsNormalizedButNonExactCandidateNameBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	candidate := storeCompletionCandidate(t, q, stamp)
	candidate.Name = ""
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		Candidate:     candidate,
		DecisionInput: storeCompletionInput(q, stamp),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		ReleaseTime:   func() time.Time { return stamp.Add(time.Minute) },
		Observe:       func(queue.CompletionReceipt) error { return nil },
	})
	if result.Phase != queue.CompletionPhaseRejected || result.Outcome != queue.OutcomeRejected {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-exact candidate name performed I/O: %v", err)
	}
}
