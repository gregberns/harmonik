package queue_test

// queue_cancel_hk0mmy4_test.go — regression test for HandleQueueCancel
// (hk-0mmy4).
//
// Bug: `harmonik queue cancel` archived the per-queue file on disk but never
// touched a live daemon's in-memory QueueStore. The daemon's copy kept
// Status=active with the cancelled item still ItemStatusDispatched, so the
// hk-a11re cross-queue dedup guard in the work loop kept treating it as a
// live conflict and hard-blocked re-dispatch of the same bead from another
// queue with LastFailureReason="cross_queue_duplicate".
//
// Fix: HandleQueueCancel (the daemon-socket-routed "queue-cancel" op) now
// archives the file AND calls QueueSetter.ClearQueueByName to reap the
// in-memory slot.
//
// This test proves the reap: after HandleQueueCancel succeeds, the wired
// QueueSetter fake no longer reports the cancelled queue's name — which is
// exactly the condition the cross-queue dedup guard checks via
// LockedAllQueueNames() before it will flag a conflict.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
//
// Bead ref: hk-0mmy4.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// queueCancelHk0mmy4FakeSetter is a minimal QueueSetter fake that tracks
// which named slots are currently "loaded" in-memory, mirroring
// daemon.QueueStore's map[string]*Queue semantics closely enough to prove the
// reap behaviour.
type queueCancelHk0mmy4FakeSetter struct {
	slots map[string]*queue.Queue
}

func newQueueCancelHk0mmy4FakeSetter() *queueCancelHk0mmy4FakeSetter {
	return &queueCancelHk0mmy4FakeSetter{slots: make(map[string]*queue.Queue)}
}

func (s *queueCancelHk0mmy4FakeSetter) SetQueue(q *queue.Queue) {
	s.slots[queue.NormaliseQueueName(q.Name)] = q
}

func (s *queueCancelHk0mmy4FakeSetter) ClearQueueByName(name string) {
	delete(s.slots, name)
}

// TestHandleQueueCancel_ReapsInMemorySlot is the load-bearing regression for
// hk-0mmy4: cancelling a queue that is "live" in the daemon's in-memory
// QueueStore must remove it from that store, not just archive the file.
func TestHandleQueueCancel_ReapsInMemorySlot(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	const (
		queueName = "alpha"
		beadX     = core.BeadID("hk-0mmy4-dispatched-bead")
	)

	// Simulate the daemon's live in-memory state at the moment of cancel: the
	// queue is active with an in-flight (dispatched) item for beadX — exactly
	// what the hk-a11re cross-queue dedup guard scans for.
	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qid-alpha-hk0mmy4",
		Name:          queueName,
		SubmittedAt:   now,
		Workers:       1,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadX, Status: queue.ItemStatusDispatched},
				},
				CreatedAt: now,
			},
		},
	}
	ctx := context.Background()
	if err := queue.Persist(ctx, projectDir, q); err != nil {
		t.Fatalf("persist alpha: %v", err)
	}

	setter := newQueueCancelHk0mmy4FakeSetter()
	setter.SetQueue(q)

	a := queue.NewHandlerAdapter(nil, projectDir, setter, nil)

	params, err := json.Marshal(queue.QueueCancelRequest{Queue: queueName})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	res, rpcErr := a.HandleQueueCancel(ctx, params)
	if rpcErr != nil {
		t.Fatalf("HandleQueueCancel: unexpected RPCError %+v", rpcErr)
	}

	var resp queue.QueueCancelResponse
	if err := json.Unmarshal(res, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.QueueID != q.QueueID {
		t.Fatalf("response.QueueID = %q, want %q", resp.QueueID, q.QueueID)
	}
	if resp.PriorStatus != string(queue.QueueStatusActive) {
		t.Fatalf("response.PriorStatus = %q, want %q", resp.PriorStatus, queue.QueueStatusActive)
	}

	// The load-bearing assertion: the in-memory slot must be gone. This is
	// the exact condition LockedAllQueueNames() (workloop.go's hk-a11re
	// guard) consults before it will flag a cross_queue_duplicate conflict —
	// with the slot reaped, a fresh dispatch of beadX from another queue no
	// longer sees this queue at all.
	if _, stillPresent := setter.slots[queueName]; stillPresent {
		t.Fatal("HandleQueueCancel did not reap the in-memory QueueStore slot (hk-0mmy4 regression)")
	}

	// The file must also be archived on disk (unchanged disk contract with
	// the daemon-less CLI path).
	entries, err := os.ReadDir(filepath.Join(projectDir, ".harmonik", "queues"))
	if err != nil {
		t.Fatalf("read queues dir: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Name() == queueName+".json" {
			t.Fatalf("queue file %q still present un-archived", e.Name())
		}
		if filepath.Ext(e.Name()) != "" && e.Name() != queueName+".json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an archived queue file under %s, entries=%v", filepath.Join(projectDir, ".harmonik", "queues"), entries)
	}
}

// TestHandleQueueCancel_AlreadyCompleted_RefusesWithoutForce mirrors the
// CLI's completed-without-force guard so the daemon-routed path rejects the
// same way the daemon-less path does.
func TestHandleQueueCancel_AlreadyCompleted_RefusesWithoutForce(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qid-completed-hk0mmy4",
		Name:          "main",
		SubmittedAt:   time.Now(),
		Workers:       1,
		Status:        queue.QueueStatusCompleted,
	}
	ctx := context.Background()
	if err := queue.Persist(ctx, projectDir, q); err != nil {
		t.Fatalf("persist: %v", err)
	}

	setter := newQueueCancelHk0mmy4FakeSetter()
	a := queue.NewHandlerAdapter(nil, projectDir, setter, nil)

	params, _ := json.Marshal(queue.QueueCancelRequest{})
	_, rpcErr := a.HandleQueueCancel(ctx, params)
	if rpcErr == nil {
		t.Fatal("HandleQueueCancel(completed, force=false): expected an RPCError, got nil")
	}
	if rpcErr.Message != "queue_already_completed" {
		t.Fatalf("RPCError.Message = %q, want %q", rpcErr.Message, "queue_already_completed")
	}
}

// TestHandleQueueCancel_AbsentQueue_StillReapsStaleSlot covers the corrupt/
// absent-file case: even with nothing to archive, any stale in-memory slot
// under that name must still be reaped.
func TestHandleQueueCancel_AbsentQueue_StillReapsStaleSlot(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "queues"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	setter := newQueueCancelHk0mmy4FakeSetter()
	setter.slots["ghost"] = &queue.Queue{Name: "ghost", Status: queue.QueueStatusActive}
	a := queue.NewHandlerAdapter(nil, projectDir, setter, nil)

	params, _ := json.Marshal(queue.QueueCancelRequest{Queue: "ghost"})
	res, rpcErr := a.HandleQueueCancel(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("HandleQueueCancel(absent): unexpected RPCError %+v", rpcErr)
	}
	var resp queue.QueueCancelResponse
	if err := json.Unmarshal(res, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.QueueID != "" {
		t.Fatalf("response.QueueID = %q, want empty (no file on disk)", resp.QueueID)
	}
	if _, stillPresent := setter.slots["ghost"]; stillPresent {
		t.Fatal("HandleQueueCancel did not reap the stale in-memory slot for an absent-on-disk queue")
	}
}
