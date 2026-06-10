package daemon

// crewstart_hkvrnh3_test.go — regression test for hk-vrnh3:
// crew-bound named queue rejects FIRST submit (queue_already_active -32010).
//
// Root cause: ensureQueue persisted a Queue with Status="" (zero value). The
// QM-027 single-active guard treats any status other than "completed" or
// "paused-by-failure" as active, so the first crew queue-submit hit -32010.
//
// Fix: ensureQueue now sets Status=QueueStatusCompleted on the placeholder.
//
// Bead ref: hk-vrnh3.
// Task check: go test ./internal/daemon/ -run CrewQueue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// crewQueueLedger is a minimal BeadLedger for QM-020/QM-021/QM-022 that
// reports every bead as open with no blocking edges.
type crewQueueLedger struct{}

func (crewQueueLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (crewQueueLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// TestCrewQueue_FirstSubmitAccepted is the direct regression for hk-vrnh3.
//
// Sequence:
//  1. crew-start with queue "crew-alpha" → ensureQueue persists placeholder.
//  2. HandleQueueSubmit to "crew-alpha" → must be accepted (no -32010).
//
// Before the fix, step 2 returned queue_already_active (-32010) because the
// placeholder had Status="" which the QM-027 guard treated as active.
func TestCrewQueue_FirstSubmitAccepted(t *testing.T) {
	t.Parallel()

	h, dir := newTestCrewHandler(t, &fakeSubstrate{}, nil)
	ctx := context.Background()

	// Step 1: crew-start ensures the queue exists as a completed placeholder.
	raw, _ := json.Marshal(CrewStartRequest{Name: "crew-alpha", Queue: "crew-alpha"})
	_, err := h.HandleCrewStart(ctx, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("crew-start: %v", err)
	}

	// Step 2: first queue-submit to the crew's queue must succeed.
	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "crew-alpha",
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					{BeadID: "hk-vrnh3-test-bead", Status: queue.ItemStatusPending},
				},
			},
		},
	}
	ledger := crewQueueLedger{}
	resp, _, _, rpcErr := queue.HandleQueueSubmit(ctx, req, ledger, dir, 1)

	if rpcErr != nil {
		t.Fatalf("first queue-submit to crew queue got RPC error %d (%s): want nil — hk-vrnh3 regression",
			rpcErr.Code, rpcErr.Message)
	}
	if resp.QueueID == "" {
		t.Error("queue_id must be non-empty on accepted submit")
	}
	if resp.Status != queue.QueueStatusActive {
		t.Errorf("status = %q, want %q", resp.Status, queue.QueueStatusActive)
	}
}

// TestCrewQueue_EnsuredQueueStatusIsCompleted verifies that after crew-start
// the persisted placeholder queue has Status=completed. This is the mechanism
// fix: QM-027 allows resubmit over completed queues.
func TestCrewQueue_EnsuredQueueStatusIsCompleted(t *testing.T) {
	t.Parallel()

	h, dir := newTestCrewHandler(t, &fakeSubstrate{}, nil)
	ctx := context.Background()

	raw, _ := json.Marshal(CrewStartRequest{Name: "crew-beta", Queue: "crew-beta"})
	if _, err := h.HandleCrewStart(ctx, json.RawMessage(raw)); err != nil {
		t.Fatalf("crew-start: %v", err)
	}

	q, err := queue.Load(ctx, dir, "crew-beta")
	if err != nil {
		t.Fatalf("queue.Load: %v", err)
	}
	if q == nil {
		t.Fatal("ensured queue file is missing")
	}
	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("ensured queue status = %q, want %q — QM-027 requires completed to allow first submit",
			q.Status, queue.QueueStatusCompleted)
	}
}
