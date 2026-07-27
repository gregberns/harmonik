package queuewiring

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

func transactionStoreFixture(t *testing.T) (*QueueStore, *queue.Queue, string) {
	t.Helper()
	projectDir := t.TempDir()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0020",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{},
	}
	store := NewQueueStore()
	store.SetQueue(q)
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return store, q, projectDir
}

func TestQueueStoreTransactRejectsStaleSnapshotBeforeIO(t *testing.T) {
	t.Parallel()
	store := NewQueueStore()
	store.SetQueue(&queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0021",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{},
	})
	stale := store.Snapshot(queue.QueueNameMain)
	store.SetQueue(store.Queue())
	projectDir := filepath.Join(t.TempDir(), "must-remain-absent")

	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      stale,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationPause,
		Mutate: func(q *queue.Queue) error {
			q.Status = queue.QueueStatusPausedByDrain
			return nil
		},
	})
	if got.Outcome != queue.OutcomeRejected || got.Err == nil {
		t.Fatalf("stale transaction = (%q, %v), want rejected", got.Outcome, got.Err)
	}
	if _, err := os.Stat(projectDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale transaction performed namespace I/O: %v", err)
	}
}

func TestQueueStoreTransactRejectsMutatedSnapshotBeforeIO(t *testing.T) {
	t.Parallel()
	store := NewQueueStore()
	store.SetQueue(&queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0022",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{},
	})
	mutated := store.Snapshot(queue.QueueNameMain)
	mutated.Queue.Status = queue.QueueStatusCancelled
	projectDir := filepath.Join(t.TempDir(), "must-remain-absent")

	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      mutated,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationPause,
		Mutate: func(q *queue.Queue) error {
			q.Status = queue.QueueStatusPausedByDrain
			return nil
		},
	})
	if got.Outcome != queue.OutcomeRejected || got.Err == nil {
		t.Fatalf("mutated snapshot = (%q, %v), want rejected", got.Outcome, got.Err)
	}
	if _, err := os.Stat(projectDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mutated snapshot performed namespace I/O: %v", err)
	}
}

func TestQueueStoreTransactConcurrentSnapshotHasOneWriter(t *testing.T) {
	t.Parallel()
	store, _, projectDir := transactionStoreFixture(t)
	snapshot := store.Snapshot(queue.QueueNameMain)

	start := make(chan struct{})
	results := make(chan TransactionResult, 2)
	var wg sync.WaitGroup
	for _, status := range []queue.QueueStatus{
		queue.QueueStatusPausedByDrain,
		queue.QueueStatusCancelled,
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- store.Transact(context.Background(), TransactionRequest{
				Snapshot:      snapshot,
				ProjectDir:    projectDir,
				OperationKind: queue.OperationPause,
				Mutate: func(q *queue.Queue) error {
					q.Status = status
					return nil
				},
			})
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var committed, rejected int
	for result := range results {
		switch result.Outcome {
		case queue.OutcomeCommittedDurable:
			committed++
		case queue.OutcomeRejected:
			rejected++
		default:
			t.Fatalf("unexpected race outcome %q (err=%v)", result.Outcome, result.Err)
		}
	}
	if committed != 1 || rejected != 1 {
		t.Fatalf("race outcomes committed=%d rejected=%d, want 1/1", committed, rejected)
	}

	memory := store.Queue()
	//nolint:gosec // path is rooted in test-owned t.TempDir
	diskBytes, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", "main.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk queue.Queue
	if err := json.Unmarshal(diskBytes, &disk); err != nil {
		t.Fatal(err)
	}
	if memory.Status != disk.Status {
		t.Fatalf("memory status %q differs from durable status %q", memory.Status, disk.Status)
	}
	if store.Snapshot(queue.QueueNameMain).Generation != snapshot.Generation+1 {
		t.Fatal("concurrent transaction advanced generation more than once")
	}
}

func TestQueueStoreTransactMutationFailurePreservesMemoryAndDisk(t *testing.T) {
	t.Parallel()
	store, prior, projectDir := transactionStoreFixture(t)
	snapshot := store.Snapshot(queue.QueueNameMain)

	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationPause,
		Mutate: func(q *queue.Queue) error {
			q.Status = queue.QueueStatusPausedByDrain
			return errors.New("mutation refused")
		},
	})
	if got.Outcome != queue.OutcomeRejected || got.Err == nil {
		t.Fatalf("mutation failure = (%q, %v), want rejected", got.Outcome, got.Err)
	}
	if store.Queue().Status != prior.Status {
		t.Fatal("mutation failure changed in-memory queue")
	}
	//nolint:gosec // path is rooted in test-owned t.TempDir
	diskBytes, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", "main.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk queue.Queue
	if err := json.Unmarshal(diskBytes, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Status != prior.Status {
		t.Fatal("mutation failure changed durable queue")
	}
}

func TestQueueStoreTransactNoOpCollapsesBeforeNamespaceIO(t *testing.T) {
	t.Parallel()
	store, _, projectDir := transactionStoreFixture(t)
	snapshot := store.Snapshot(queue.QueueNameMain)

	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationMaintenance,
		Mutate:        func(*queue.Queue) error { return nil },
	})
	if got.Outcome != queue.OutcomeCommittedDurable || got.Err != nil {
		t.Fatalf("no-op = (%q, %v), want committed durable", got.Outcome, got.Err)
	}
	if got.Snapshot.Generation != snapshot.Generation {
		t.Fatal("no-op transaction advanced generation")
	}
	qDir := filepath.Join(projectDir, ".harmonik", "queues")
	entries, err := os.ReadDir(qDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "main.json" {
		t.Fatalf("no-op transaction created namespace artifacts: %v", entries)
	}
}

func TestQueueStoreTransactLinkedHandoffRetainsPredecessor(t *testing.T) {
	t.Parallel()
	store, _, projectDir := transactionStoreFixture(t)
	snapshot := store.Snapshot(queue.QueueNameMain)

	got := store.Transact(context.Background(), TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationCancellation,
		ArchiveHandoff: &queue.ArchiveHandoffPlan{
			ArchiveOrigin:       "operator-cancel",
			ArchiveKind:         "cancelled",
			SourceIdentity:      snapshot.Queue.QueueID,
			DestinationBasename: "main.json.cancelled-fixed",
		},
		Mutate: func(q *queue.Queue) error {
			q.Status = queue.QueueStatusCancelled
			return nil
		},
	})
	if got.Outcome != queue.OutcomeCommittedDurable || got.Err != nil {
		t.Fatalf("linked transaction = (%q, %v), want committed durable", got.Outcome, got.Err)
	}
	intentPath := filepath.Join(projectDir, ".harmonik", "queues", "main.replace-intent")
	if _, err := os.Stat(intentPath); err != nil {
		t.Fatalf("linked predecessor was not retained: %v", err)
	}
}
