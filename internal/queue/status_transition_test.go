package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

type fakeTransactionStore struct{}

func (fakeTransactionStore) Snapshot(string) queue.QueueSnapshot {
	return queue.QueueSnapshot{}
}

func (fakeTransactionStore) Transact(context.Context, queue.TransactionRequest) queue.TransactionResult {
	return queue.TransactionResult{}
}

func TestResolveDeferredItemRejectsOtherStates(t *testing.T) {
	t.Parallel()

	item := queue.Item{Status: queue.ItemStatusPending}
	if err := queue.ResolveDeferredItem(&item); err == nil {
		t.Fatal("ResolveDeferredItem succeeded for pending item")
	}
	if item.Status != queue.ItemStatusPending {
		t.Fatalf("status = %q, want pending", item.Status)
	}

	item.Status = queue.ItemStatusDeferredForLedgerDep
	if err := queue.ResolveDeferredItem(&item); err != nil {
		t.Fatalf("ResolveDeferredItem: %v", err)
	}
	if item.Status != queue.ItemStatusPending {
		t.Fatalf("status = %q, want pending", item.Status)
	}
}

func TestCompleteActiveGroupRequiresTerminalItems(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.FixedZone("test", -7*60*60))
	group := queue.Group{
		Status: queue.GroupStatusActive,
		Items:  []queue.Item{{Status: queue.ItemStatusPending}},
	}
	if err := queue.CompleteActiveGroup(&group, stamp); err == nil {
		t.Fatal("CompleteActiveGroup succeeded with pending item")
	}
	if group.Status != queue.GroupStatusActive {
		t.Fatalf("status = %q, want active", group.Status)
	}

	group.Items[0].Status = queue.ItemStatusCompleted
	if err := queue.CompleteActiveGroup(&group, stamp); err != nil {
		t.Fatalf("CompleteActiveGroup: %v", err)
	}
	if group.Status != queue.GroupStatusCompleteSuccess {
		t.Fatalf("status = %q, want complete-success", group.Status)
	}
	if group.CompletedAt == nil || !group.CompletedAt.Equal(stamp.UTC()) {
		t.Fatalf("completed_at = %v, want %v", group.CompletedAt, stamp.UTC())
	}
}

func TestCompleteQueueRequiresSuccessfulGroups(t *testing.T) {
	t.Parallel()

	q := queue.Queue{
		Status: queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Status:     queue.GroupStatusActive,
		}},
	}
	if err := queue.CompleteQueue(&q); err == nil {
		t.Fatal("CompleteQueue succeeded with active group")
	}
	if q.Status != queue.QueueStatusActive {
		t.Fatalf("status = %q, want active", q.Status)
	}

	q.Groups[0].Status = queue.GroupStatusCompleteSuccess
	if err := queue.CompleteQueue(&q); err != nil {
		t.Fatalf("CompleteQueue: %v", err)
	}
	if q.Status != queue.QueueStatusCompleted {
		t.Fatalf("status = %q, want completed", q.Status)
	}
}

func TestStartupItemTransitionsAndTransactionPort(t *testing.T) {
	t.Parallel()

	runID := "run-1"
	item := queue.Item{Status: queue.ItemStatusDispatched, RunID: &runID}
	if err := queue.RecoverDispatchedItemToPending(&item); err != nil {
		t.Fatalf("RecoverDispatchedItemToPending: %v", err)
	}
	if item.Status != queue.ItemStatusPending || item.RunID != nil {
		t.Fatalf("recovered item = %+v, want pending item without run ID", item)
	}
	if err := queue.ReconcileItemToFailed(&item); err != nil {
		t.Fatalf("ReconcileItemToFailed: %v", err)
	}
	if err := queue.ReconcileItemToCompleted(&item); err == nil {
		t.Fatal("ReconcileItemToCompleted succeeded from failed")
	}

	var store queue.TransactionStore = fakeTransactionStore{}
	if got := store.Snapshot("main"); got.Name != "" || got.Queue != nil {
		t.Fatalf("fake snapshot = %+v, want zero value", got)
	}
}
