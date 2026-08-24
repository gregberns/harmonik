package queue

import (
	"errors"
	"testing"
)

func failureParkedDropFixture() *Queue {
	return &Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0300",
		Name:          "main",
		Status:        QueueStatusPausedByFailure,
		Groups: []Group{{
			GroupIndex: 0,
			Kind:       GroupKindWave,
			Status:     GroupStatusCompleteWithFailures,
			Items: []Item{{
				BeadID: "hk-stale",
				Status: ItemStatusFailed,
			}},
		}},
	}
}

func TestPlanFailedDrop_NamesTheFailedItemsOfTheLastGroup(t *testing.T) {
	plan, err := PlanFailedDrop("main", failureParkedDropFixture())
	if err != nil {
		t.Fatalf("PlanFailedDrop: %v", err)
	}
	if len(plan.DroppedItems) != 1 || plan.DroppedItems[0] != "hk-stale" {
		t.Fatalf("dropped items = %v, want [hk-stale]", plan.DroppedItems)
	}
	if plan.GroupIndex != 0 {
		t.Fatalf("group index = %d, want 0", plan.GroupIndex)
	}
}

func TestPlanFailedDrop_RefusesAMissingQueue(t *testing.T) {
	_, err := PlanFailedDrop("main", nil)
	assertDropReason(t, err, DropReasonQueueNotFound)
}

func TestPlanFailedDrop_RefusesAQueueThatIsNotFailureParked(t *testing.T) {
	q := failureParkedDropFixture()
	q.Status = QueueStatusActive
	q.Groups[0].Status = GroupStatusActive
	_, err := PlanFailedDrop("main", q)
	assertDropReason(t, err, DropReasonQueueNotRecoverable)
}

func TestPlanFailedDrop_RefusesWhenPendingWorkSitsBehindTheFailure(t *testing.T) {
	q := failureParkedDropFixture()
	q.Groups = append(q.Groups, Group{
		GroupIndex: 1,
		Kind:       GroupKindWave,
		Status:     GroupStatusPending,
		Items:      []Item{{BeadID: "hk-pending", Status: ItemStatusPending}},
	})
	_, err := PlanFailedDrop("main", q)
	assertDropReason(t, err, DropReasonTrailingGroups)
}

func assertDropReason(t *testing.T, err error, want DropReason) {
	t.Helper()
	var dropErr *DropError
	if !errors.As(err, &dropErr) {
		t.Fatalf("error = %v (%T), want *DropError", err, err)
	}
	if dropErr.Reason != want {
		t.Fatalf("reason = %q, want %q", dropErr.Reason, want)
	}
}
