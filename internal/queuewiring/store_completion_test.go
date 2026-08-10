package queuewiring

import (
	"errors"
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
			GroupIndex:  0,
			Kind:        queue.GroupKindStream,
			Status:      queue.GroupStatusCompleteSuccess,
			CompletedAt: &stamp,
			Items:       []queue.Item{{BeadID: core.BeadID("hk-store-complete"), Status: queue.ItemStatusCompleted}},
		}},
	}, stamp
}

func TestQueueStoreCompleteOrdersReceiptObservationCleanupAndRelease(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}
	observed := false
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
		ProjectDir:    projectDir,
		TransactionID: storeCompletionTransactionID,
		ReceiptID:     storeCompletionReceiptID,
		CompletedAt:   stamp,
		Observe: func(receipt queue.CompletionReceipt) error {
			observed = true
			before := store.Snapshot(queue.QueueNameMain)
			if before.Queue == nil || before.Queue.Status != queue.QueueStatusCompleted {
				t.Fatalf("observer could not read completed snapshot: %+v", before.Queue)
			}
			replacement := cloneQueue(before.Queue)
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
	if !observed {
		t.Fatal("completion observation was not attempted")
	}
	if result.Phase != queue.CompletionPhaseOwnershipReleased || !result.Committed() {
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
				ProjectDir:    projectDir,
				TransactionID: storeCompletionTransactionID,
				ReceiptID:     storeCompletionReceiptID,
				CompletedAt:   stamp,
				Observe:       func(queue.CompletionReceipt) error { return nil },
			}, tc.cleanupCanonical, tc.cleanupIntent)
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

func TestQueueStoreCompleteRequiresObserverBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      store.Snapshot(queue.QueueNameMain),
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

func TestQueueStoreCompleteRejectsStaleSnapshotBeforeIO(t *testing.T) {
	store := NewQueueStore()
	q, stamp := storeCompletionFixture()
	store.SetQueue(q)
	stale := store.Snapshot(queue.QueueNameMain)
	store.SetQueue(q)
	projectDir := t.TempDir()
	result := store.Complete(t.Context(), queue.CompletionRequest{
		Snapshot:      stale,
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
