package daemon_test

// queuestore_namedqueues_hktigaf2_test.go — name-keyed QueueStore tests (hk-tigaf.2).
//
// Covers:
//   - QueueByName / SetQueueByName / ClearQueueByName
//   - AllQueues snapshot
//   - SetQueue stores at q.Name (normalised)
//   - QueueByName("main") mirrors Queue() for backward compat
//   - Multiple concurrent names are independent
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer).
// Bead ref: hk-tigaf.2.

import (
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// namedQueueFixture builds a minimal *queue.Queue with the given name.
func namedQueueFixture(t *testing.T, name string) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "test-queue-" + name + "-" + t.Name(),
		Name:          name,
		SubmittedAt:   time.Now().UTC(),
		Groups:        []queue.Group{},
		Status:        queue.QueueStatusActive,
	}
}

// TestQueueStoreByNameBasicRoundTrip asserts that SetQueueByName / QueueByName
// round-trip the pointer correctly for a named slot.
func TestQueueStoreByNameBasicRoundTrip(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()

	q := namedQueueFixture(t, "work")
	qs.SetQueueByName("work", q)

	got := qs.QueueByName("work")
	if got == nil {
		t.Fatal("QueueByName: expected non-nil after SetQueueByName, got nil")
	}
	if got != q {
		t.Fatalf("QueueByName: returned different pointer: want %p, got %p", q, got)
	}
}

// TestQueueStoreByNameMissingSlot asserts that QueueByName returns nil for an
// unknown name.
func TestQueueStoreByNameMissingSlot(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	if qs.QueueByName("does-not-exist") != nil {
		t.Fatal("QueueByName: expected nil for absent name, got non-nil")
	}
}

// TestQueueStoreClearQueueByName asserts that ClearQueueByName removes the
// named slot.
func TestQueueStoreClearQueueByName(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueueByName("work", namedQueueFixture(t, "work"))
	qs.ClearQueueByName("work")

	if qs.QueueByName("work") != nil {
		t.Fatal("QueueByName: expected nil after ClearQueueByName, got non-nil")
	}
}

// TestQueueStoreMultipleNamesIndependent asserts that two named slots do not
// interfere: clearing one leaves the other intact.
func TestQueueStoreMultipleNamesIndependent(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()

	qa := namedQueueFixture(t, "alpha")
	qb := namedQueueFixture(t, "beta")
	qs.SetQueueByName("alpha", qa)
	qs.SetQueueByName("beta", qb)

	qs.ClearQueueByName("alpha")

	if qs.QueueByName("alpha") != nil {
		t.Fatal("alpha slot: expected nil after clear, got non-nil")
	}
	if got := qs.QueueByName("beta"); got != qb {
		t.Fatalf("beta slot: want %p, got %p", qb, got)
	}
}

// TestQueueStoreSetQueueUsesNameField asserts that SetQueue (the compat shim)
// stores the queue under q.Name (normalised). A queue with Name="main" is
// retrievable via Queue() and QueueByName("main").
func TestQueueStoreSetQueueUsesNameField(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	q := namedQueueFixture(t, queue.QueueNameMain)
	qs.SetQueue(q)

	if got := qs.Queue(); got != q {
		t.Fatalf("Queue() after SetQueue(main): want %p, got %p", q, got)
	}
	if got := qs.QueueByName(queue.QueueNameMain); got != q {
		t.Fatalf("QueueByName(main) after SetQueue(main): want %p, got %p", q, got)
	}
}

// TestQueueStoreSetQueueEmptyNameNormalisesToMain asserts that SetQueue on a
// queue with empty Name stores at "main" (the NormaliseQueueName rule).
func TestQueueStoreSetQueueEmptyNameNormalisesToMain(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "test-empty-name",
		Name:          "", // intentionally empty
		SubmittedAt:   time.Now().UTC(),
		Groups:        []queue.Group{},
		Status:        queue.QueueStatusActive,
	}
	qs.SetQueue(q)

	if got := qs.Queue(); got != q {
		t.Fatalf("Queue() after SetQueue(empty Name): want %p, got %p", q, got)
	}
	if got := qs.QueueByName(queue.QueueNameMain); got != q {
		t.Fatalf("QueueByName(main) after SetQueue(empty Name): want %p, got %p", q, got)
	}
}

// TestQueueStoreAllQueues asserts that AllQueues returns a snapshot of all
// loaded named queues.
func TestQueueStoreAllQueues(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()

	qa := namedQueueFixture(t, "alpha")
	qb := namedQueueFixture(t, "beta")
	qs.SetQueueByName("alpha", qa)
	qs.SetQueueByName("beta", qb)

	snap := qs.AllQueues()
	if len(snap) != 2 {
		t.Fatalf("AllQueues: want 2 entries, got %d", len(snap))
	}
	if snap["alpha"] != qa {
		t.Fatalf("AllQueues[alpha]: want %p, got %p", qa, snap["alpha"])
	}
	if snap["beta"] != qb {
		t.Fatalf("AllQueues[beta]: want %p, got %p", qb, snap["beta"])
	}
}

// TestQueueStoreAllQueuesIsSnapshot asserts that mutating the returned map
// does not affect the store.
func TestQueueStoreAllQueuesIsSnapshot(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueueByName("main", namedQueueFixture(t, "main"))

	snap := qs.AllQueues()
	delete(snap, "main") // mutate the snapshot

	if qs.QueueByName("main") == nil {
		t.Fatal("AllQueues snapshot mutation affected the store")
	}
}

// TestQueueStoreNamedConcurrentReadSerialWrite exercises the QM-060
// single-writer discipline for multiple named slots under concurrent access.
func TestQueueStoreNamedConcurrentReadSerialWrite(t *testing.T) {
	t.Parallel()

	const names = 4
	const readers = 32
	const writes = 16

	nameList := []string{"alpha", "beta", "gamma", "delta"}

	qs := daemon.ExportedNewQueueStore()
	for _, n := range nameList {
		qs.SetQueueByName(n, namedQueueFixture(t, n))
	}

	var wg sync.WaitGroup
	wg.Add(readers + names)

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = qs.AllQueues()
			}
		}()
	}
	for _, n := range nameList {
		n := n
		go func() {
			defer wg.Done()
			for i := 0; i < writes; i++ {
				if i%2 == 0 {
					qs.SetQueueByName(n, namedQueueFixture(t, n))
				} else {
					qs.ClearQueueByName(n)
				}
			}
		}()
	}
	wg.Wait()
}
