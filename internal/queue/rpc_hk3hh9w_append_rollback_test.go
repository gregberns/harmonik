package queue_test

// rpc_hk3hh9w_append_rollback_test.go — regression test for hk-3hh9w.
//
// appendUnderLock resolves the LIVE locked in-memory queue and, before the fix,
// ran AppendItems (which mutates in place) directly on it, then Persisted. A
// failed Persist left the in-memory store ahead of disk — the queue held the
// appended items while disk did not, with no rollback and an error returned to
// the caller. The fix takes a snapshot: AppendItems runs on a clone and the
// clone is installed into the store only after Persist succeeds, so a failed
// Persist leaves the live store byte-for-byte untouched.
//
// The negative case forces a Persist failure (read-only .harmonik dir so the
// atomic-write MkdirAll fails) and asserts the live store queue is unchanged
// and no install happened. The positive case confirms a successful append still
// installs the mutated queue — guarding against the clone breaking the happy
// path.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// hk3FakeLocker is a QueueSetter+MutationLocker double backed by an in-memory
// queue map, so appendUnderLock resolves the LIVE queue (not a disk copy) and
// its LockedSetQueueByName install is observable.
type hk3FakeLocker struct {
	mu         sync.Mutex
	queues     map[string]*queue.Queue
	setCalls   int
	lastSet    *queue.Queue
	lastSetKey string
}

func newHK3FakeLocker(name string, q *queue.Queue) *hk3FakeLocker {
	return &hk3FakeLocker{queues: map[string]*queue.Queue{name: q}}
}

func (f *hk3FakeLocker) SetQueue(*queue.Queue)      {}
func (f *hk3FakeLocker) ClearQueueByName(string)    {}
func (f *hk3FakeLocker) Wake()                      {}
func (f *hk3FakeLocker) LockForMutationView() queue.LockedQueueView {
	f.mu.Lock()
	return hk3FakeView{f}
}

type hk3FakeView struct{ f *hk3FakeLocker }

func (v hk3FakeView) LockedQueueByName(name string) *queue.Queue { return v.f.queues[name] }
func (v hk3FakeView) LockedSetQueueByName(name string, q *queue.Queue) {
	v.f.setCalls++
	v.f.lastSet = q
	v.f.lastSetKey = name
	v.f.queues[name] = q
}
func (v hk3FakeView) LockedAllQueueNames() []string {
	names := make([]string, 0, len(v.f.queues))
	for k := range v.f.queues {
		names = append(names, k)
	}
	return names
}
func (v hk3FakeView) Done() { v.f.mu.Unlock() }

// hk3LiveQueue builds an active stream queue named "main" with one existing
// pending item, as the live in-memory store entry to append onto.
func hk3LiveQueue() *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0192a7c0-0000-7000-8000-0000000000aa",
		Name:          "main",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: "hk-exist01", Status: queue.ItemStatusPending},
				},
			},
		},
	}
}

func hk3AppendParams(t *testing.T, newBead core.BeadID) []byte {
	t.Helper()
	req := queue.QueueAppendRequest{
		Name:       "main",
		GroupIndex: 0,
		BeadIDs:    []core.BeadID{newBead},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal append req: %v", err)
	}
	return raw
}

func TestHandlerAdapter_AppendPersistFailure_LeavesStoreUnmutated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only-dir persist-failure cannot be forced as root")
	}

	const newBead core.BeadID = "hk-new01"
	projectDir := rpcFixtureTempProjectDir(t) // creates .harmonik/ (0755)
	ledger := rpcFixtureOpenLedger("hk-exist01", newBead)

	live := hk3LiveQueue()
	locker := newHK3FakeLocker("main", live)
	adapter := queue.NewHandlerAdapter(ledger, projectDir, locker, nil)

	// Force Persist to fail: make .harmonik read-only so the atomic-write
	// MkdirAll(.harmonik/queues) inside Persist returns EACCES. EnumerateQueueNames
	// (loadOtherQueues) reads a not-yet-existent queues/ dir → ErrNotExist → nil,
	// so nothing short-circuits before Persist.
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.Chmod(harmonikDir, 0o500); err != nil {
		t.Fatalf("chmod .harmonik read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(harmonikDir, 0o755) }) // let TempDir cleanup succeed

	_, rpcErr := adapter.HandleQueueAppend(t.Context(), hk3AppendParams(t, newBead))
	if rpcErr == nil {
		t.Fatal("expected an RPCError from the failed Persist, got nil")
	}

	// The live store queue MUST be untouched: still exactly the one pre-existing
	// item, no appended bead (memory must not be ahead of disk).
	got := locker.queues["main"]
	if got == nil {
		t.Fatal("live queue vanished from store")
	}
	items := got.Groups[0].Items
	if len(items) != 1 {
		t.Fatalf("live store queue was mutated on a failed Persist: group[0] has %d items, want 1 "+
			"(hk-3hh9w: memory ahead of disk, no rollback)", len(items))
	}
	if items[0].BeadID != "hk-exist01" {
		t.Errorf("existing item changed: got %q, want hk-exist01", items[0].BeadID)
	}
	for _, it := range items {
		if it.BeadID == newBead {
			t.Fatalf("appended bead %q leaked into the live store despite Persist failure", newBead)
		}
	}

	// And the clone must never have been installed on failure.
	if locker.setCalls != 0 {
		t.Errorf("LockedSetQueueByName called %d times on a failed Persist, want 0", locker.setCalls)
	}
}

func TestHandlerAdapter_AppendPersistSuccess_InstallsMutatedQueue(t *testing.T) {
	const newBead core.BeadID = "hk-new01"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger("hk-exist01", newBead)

	live := hk3LiveQueue()
	locker := newHK3FakeLocker("main", live)
	adapter := queue.NewHandlerAdapter(ledger, projectDir, locker, nil)

	_, rpcErr := adapter.HandleQueueAppend(t.Context(), hk3AppendParams(t, newBead))
	if rpcErr != nil {
		t.Fatalf("append: unexpected RPCError: code=%d msg=%q detail=%v", rpcErr.Code, rpcErr.Message, rpcErr.Detail)
	}

	// The mutated clone must have been installed with the appended bead at the tail.
	if locker.setCalls != 1 {
		t.Fatalf("LockedSetQueueByName called %d times, want exactly 1", locker.setCalls)
	}
	installed := locker.lastSet
	if installed == nil {
		t.Fatal("installed queue is nil")
	}
	items := installed.Groups[0].Items
	if len(items) != 2 {
		t.Fatalf("installed queue group[0] has %d items, want 2 (1 existing + 1 appended)", len(items))
	}
	if items[1].BeadID != newBead {
		t.Errorf("tail item = %q, want appended %q", items[1].BeadID, newBead)
	}

	// The install must be a distinct object from the original live pointer (a
	// clone was swapped in, not the in-place-mutated original).
	if installed == live {
		t.Error("installed queue is the original live pointer; snapshot-and-swap expected a clone")
	}

	// Disk must reflect the append.
	queueFile := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueFile); statErr != nil {
		t.Errorf("queues/main.json not persisted after successful append: %v", statErr)
	}
}
