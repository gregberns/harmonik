package queuewiring_test

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

func queueStoreFixtureQueue(t *testing.T) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "test-queue-id-" + t.Name(),
		SubmittedAt:   time.Now().UTC(),
		Groups:        []queue.Group{},
		Status:        queue.QueueStatusActive,
	}
}

// TestQueueStoreSingleInstance asserts that the accessor returns the queue
// set by SetQueue (QM-060: single active queue instance per daemon).
//
// Queue() returns a deep copy rather than the stored pointer (hk-ri2in.4:
// handing out the live pointer let a reader's unguarded field access race
// with the write path's in-place mutation under the write lock), so this
// compares by value rather than by pointer identity.
func TestQueueStoreSingleInstance(t *testing.T) {
	t.Parallel()

	qs := queuewiring.NewQueueStore()
	if qs.Queue() != nil {
		t.Fatal("new QueueStore: expected nil queue, got non-nil")
	}

	q := queueStoreFixtureQueue(t)
	qs.SetQueue(q)

	got := qs.Queue()
	if got == nil {
		t.Fatal("Queue: expected non-nil after SetQueue, got nil")
	}
	if !reflect.DeepEqual(got, q) {
		t.Fatalf("Queue: returned value does not match set value: want %+v, got %+v", q, got)
	}
}

func TestQueueStoreQueueDetachesNestedPointers(t *testing.T) {
	runID := "run-original"
	receiptID := "receipt-original"
	stamp := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	q := queueStoreFixtureQueue(t)
	q.FailedRecoveryReceiptID = &receiptID
	q.Groups = []queue.Group{{
		StartedAt: &stamp,
		Items: []queue.Item{{
			RunID:          &runID,
			TemplateParams: map[string]string{"mode": "original"},
		}},
	}}
	qs := queuewiring.NewQueueStore()
	qs.SetQueue(q)

	snapshot := qs.Queue()
	*snapshot.FailedRecoveryReceiptID = "changed-receipt"
	*snapshot.Groups[0].StartedAt = stamp.Add(time.Hour)
	*snapshot.Groups[0].Items[0].RunID = "changed-run"
	snapshot.Groups[0].Items[0].TemplateParams["mode"] = "changed"

	fresh := qs.Queue()
	if *fresh.FailedRecoveryReceiptID != receiptID || *fresh.Groups[0].StartedAt != stamp ||
		*fresh.Groups[0].Items[0].RunID != runID || fresh.Groups[0].Items[0].TemplateParams["mode"] != "original" {
		t.Fatalf("snapshot mutation reached store: %+v", fresh)
	}
}

// TestQueueStoreClearQueue asserts that ClearQueue removes the queue.
func TestQueueStoreClearQueue(t *testing.T) {
	t.Parallel()

	qs := queuewiring.NewQueueStore()
	qs.SetQueue(queueStoreFixtureQueue(t))
	qs.ClearQueue()

	if qs.Queue() != nil {
		t.Fatal("Queue: expected nil after ClearQueue, got non-nil")
	}
}

// TestQueueStoreConcurrentReadSerialWrite asserts mutex correctness: many
// concurrent readers observe only complete Set/Clear transitions (no torn
// reads), and a single writer does not race with readers.
//
// This exercises the QM-060 single-writer discipline: Queue (read path) is
// safe to call concurrently; SetQueue is the serialized writer.
func TestQueueStoreConcurrentReadSerialWrite(t *testing.T) {
	t.Parallel()

	const numReaders = 64
	const numWrites = 32

	qs := queuewiring.NewQueueStore()

	qs.SetQueue(queueStoreFixtureQueue(t))

	var wg sync.WaitGroup
	wg.Add(numReaders + 1)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = qs.Queue()
			}
		}()
	}

	go func() {
		defer wg.Done()
		for i := 0; i < numWrites; i++ {
			if i%2 == 0 {
				qs.SetQueue(queueStoreFixtureQueue(t))
			} else {
				qs.ClearQueue()
			}
		}
	}()

	wg.Wait()
}

// TestQueueStoreLockForMutation asserts that LockForMutation serialises a
// read-then-write sequence: the locked view sees the pre-mutation state and
// can update atomically without concurrent interference.
//
// Spec ref: specs/queue-model.md §9.1 QM-060, §9.6 QM-064.
func TestQueueStoreLockForMutation(t *testing.T) {
	t.Parallel()

	qs := queuewiring.NewQueueStore()
	original := queueStoreFixtureQueue(t)
	qs.SetQueue(original)

	replacement := queueStoreFixtureQueue(t)

	lq := qs.LockForMutation()
	if lq.Queue() != original {
		lq.Done()
		t.Fatalf("LockForMutation: locked view returned wrong queue: want %p, got %p", original, lq.Queue())
	}
	lq.SetQueue(replacement)
	lq.Done()

	if got := qs.Queue(); !reflect.DeepEqual(got, replacement) {
		t.Fatalf("after LockForMutation swap: want %+v, got %+v", replacement, got)
	}
}
