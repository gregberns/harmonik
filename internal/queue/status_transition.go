package queue

import (
	"fmt"
	"time"
)

// NewPendingItem sets the first status for an item.
func NewPendingItem(item Item) Item {
	setItemStatus(&item, ItemStatusPending)
	return item
}

// NewPendingGroup sets the first status for a group.
func NewPendingGroup(group Group) Group {
	setGroupStatus(&group, GroupStatusPending)
	return group
}

// NewActiveGroup sets the first status for a group that starts work at once.
func NewActiveGroup(group Group) Group {
	setGroupStatus(&group, GroupStatusActive)
	return group
}

// NewActiveQueue sets the first status for a queue.
func NewActiveQueue(q Queue) Queue {
	setQueueStatus(&q, QueueStatusActive)
	return q
}

// DeferItemForLedgerDependency changes a pending item to the ledger-deferred
// state. A deferred item is not terminal and emits no event on later recovery.
func DeferItemForLedgerDependency(item *Item) error {
	if item == nil {
		return fmt.Errorf("queue: defer item: nil item")
	}
	if item.Status != ItemStatusPending {
		return fmt.Errorf("queue: defer item: status %q is not pending", item.Status)
	}
	setItemStatus(item, ItemStatusDeferredForLedgerDep)
	return nil
}

// ResolveDeferredItem changes a ledger-deferred item back to pending.
func ResolveDeferredItem(item *Item) error {
	if item == nil {
		return fmt.Errorf("queue: resolve deferred item: nil item")
	}
	if item.Status != ItemStatusDeferredForLedgerDep {
		return fmt.Errorf("queue: resolve deferred item: status %q is not deferred", item.Status)
	}
	setItemStatus(item, ItemStatusPending)
	return nil
}

// RecoverDispatchedItemToPending restores an item whose durable dispatch claim
// was lost during startup recovery.
func RecoverDispatchedItemToPending(item *Item) error {
	if item == nil {
		return fmt.Errorf("queue: recover dispatched item: nil item")
	}
	if item.Status != ItemStatusDispatched {
		return fmt.Errorf("queue: recover dispatched item: status %q is not dispatched", item.Status)
	}
	setItemStatus(item, ItemStatusPending)
	item.RunID = nil
	return nil
}

// ReconcileItemToCompleted records a terminal ledger result during startup.
func ReconcileItemToCompleted(item *Item) error {
	if item == nil {
		return fmt.Errorf("queue: reconcile item completed: nil item")
	}
	switch item.Status {
	case ItemStatusPending, ItemStatusDispatched, ItemStatusDeferredForLedgerDep:
		setItemStatus(item, ItemStatusCompleted)
		return nil
	default:
		return fmt.Errorf("queue: reconcile item completed: status %q is not recoverable", item.Status)
	}
}

// ReconcileItemToFailed records a stranded pending startup item as failed.
func ReconcileItemToFailed(item *Item) error {
	if item == nil {
		return fmt.Errorf("queue: reconcile item failed: nil item")
	}
	switch item.Status {
	case ItemStatusPending, ItemStatusDeferredForLedgerDep:
		setItemStatus(item, ItemStatusFailed)
		return nil
	default:
		return fmt.Errorf("queue: reconcile item failed: status %q is not recoverable", item.Status)
	}
}

// CompleteItem records one terminal execution outcome.
func CompleteItem(item *Item, outcome GroupCompletionOutcome) error {
	if item == nil {
		return fmt.Errorf("queue: complete item: nil item")
	}
	switch outcome {
	case GroupCompletionOutcomeCompleted:
		setItemStatus(item, ItemStatusCompleted)
	case GroupCompletionOutcomeFailed:
		setItemStatus(item, ItemStatusFailed)
	default:
		return fmt.Errorf("queue: complete item: invalid outcome %q", outcome)
	}
	return nil
}

// ReactivateFailedItem restores a failed item for an explicit retry.
func ReactivateFailedItem(item *Item) error {
	if item == nil {
		return fmt.Errorf("queue: reactivate item: nil item")
	}
	if item.Status != ItemStatusFailed {
		return fmt.Errorf("queue: reactivate item: status %q is not failed", item.Status)
	}
	setItemStatus(item, ItemStatusPending)
	item.Attempts = 0
	item.LastFailureReason = ""
	item.RunID = nil
	return nil
}

// ReactivateFailedGroup reopens a group parked with failures.
func ReactivateFailedGroup(group *Group) error {
	if group == nil {
		return fmt.Errorf("queue: reactivate group: nil group")
	}
	if group.Status != GroupStatusCompleteWithFailures {
		return fmt.Errorf("queue: reactivate group: status %q is not complete-with-failures", group.Status)
	}
	setGroupStatus(group, GroupStatusActive)
	group.CompletedAt = nil
	return nil
}

// ResumeQueueFromFailure resumes a queue that was paused by group failure.
func ResumeQueueFromFailure(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: resume queue: nil queue")
	}
	if q.Status != QueueStatusPausedByFailure {
		return fmt.Errorf("queue: resume queue: status %q is not paused-by-failure", q.Status)
	}
	setQueueStatus(q, QueueStatusActive)
	return nil
}

// PauseQueueForFailure parks an active queue after terminal group failures.
func PauseQueueForFailure(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: pause queue for failure: nil queue")
	}
	if q.Status != QueueStatusActive {
		return fmt.Errorf("queue: pause queue for failure: status %q is not active", q.Status)
	}
	if len(q.Groups) == 0 {
		return fmt.Errorf("queue: pause queue for failure: queue has no groups")
	}
	failed := false
	for _, group := range q.Groups {
		if !groupIsTerminal(group.Status) {
			return fmt.Errorf("queue: pause queue for failure: group %d is not terminal", group.GroupIndex)
		}
		if group.Status == GroupStatusCompleteWithFailures {
			failed = true
		}
	}
	if !failed {
		return fmt.Errorf("queue: pause queue for failure: no group failed")
	}
	setQueueStatus(q, QueueStatusPausedByFailure)
	return nil
}

// PauseQueueForGroupFailure parks an active queue after one named group has
// completed with failures. Later groups remain pending.
func PauseQueueForGroupFailure(q *Queue, groupIndex int) error {
	if q == nil || q.Status != QueueStatusActive {
		return fmt.Errorf("queue: pause queue for group failure: queue is not active")
	}
	for i := range q.Groups {
		if q.Groups[i].GroupIndex == groupIndex && q.Groups[i].Status == GroupStatusCompleteWithFailures {
			setQueueStatus(q, QueueStatusPausedByFailure)
			return nil
		}
	}
	return fmt.Errorf("queue: pause queue for group failure: group %d is not complete-with-failures", groupIndex)
}

// PauseQueueForDrain parks an active queue for an operator drain.
func PauseQueueForDrain(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: pause queue for drain: nil queue")
	}
	if q.Status != QueueStatusActive {
		return fmt.Errorf("queue: pause queue for drain: status %q is not active", q.Status)
	}
	setQueueStatus(q, QueueStatusPausedByDrain)
	q.ResumeOnStart = false
	return nil
}

// PauseQueueForRestart parks an active queue during a clean daemon shutdown.
// The durable resume bit lets startup distinguish this mechanical pause from
// an explicit operator pause.
func PauseQueueForRestart(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: pause queue for restart: nil queue")
	}
	if q.Status != QueueStatusActive {
		return fmt.Errorf("queue: pause queue for restart: status %q is not active", q.Status)
	}
	setQueueStatus(q, QueueStatusPausedByDrain)
	q.ResumeOnStart = true
	return nil
}

// ResumeQueueFromDrain resumes a queue paused for an operator drain.
func ResumeQueueFromDrain(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: resume queue from drain: nil queue")
	}
	if q.Status != QueueStatusPausedByDrain {
		return fmt.Errorf("queue: resume queue from drain: status %q is not paused-by-drain", q.Status)
	}
	setQueueStatus(q, QueueStatusActive)
	q.ResumeOnStart = false
	return nil
}

// CompleteQueue marks a queue completed after every group completed successfully.
func CompleteQueue(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: complete queue: nil queue")
	}
	if q.Status != QueueStatusActive && q.Status != QueueStatusCompleted {
		return fmt.Errorf("queue: complete queue: status %q is not active", q.Status)
	}
	if len(q.Groups) == 0 {
		return fmt.Errorf("queue: complete queue: queue has no groups")
	}
	for _, group := range q.Groups {
		if group.Status != GroupStatusCompleteSuccess {
			return fmt.Errorf("queue: complete queue: group %d has status %q", group.GroupIndex, group.Status)
		}
	}
	if q.Status == QueueStatusActive {
		setQueueStatus(q, QueueStatusCompleted)
	}
	return nil
}

// CancelQueue marks an active queue cancelled before its archive handoff.
func CancelQueue(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: cancel queue: nil queue")
	}
	if q.Status != QueueStatusActive {
		return fmt.Errorf("queue: cancel queue: status %q is not active", q.Status)
	}
	setQueueStatus(q, QueueStatusCancelled)
	return nil
}

// InstallCommittedQueueStatus copies a durable candidate status into its live
// caller-owned queue after the write succeeds.
func InstallCommittedQueueStatus(destination, candidate *Queue) error {
	if destination == nil || candidate == nil {
		return fmt.Errorf("queue: install committed queue status: nil queue")
	}
	setQueueStatus(destination, candidate.Status)
	return nil
}

// CompleteActiveGroup marks an active group terminal only after every item is
// terminal. completedAt is stored in UTC.
func CompleteActiveGroup(group *Group, completedAt time.Time) error {
	if group == nil {
		return fmt.Errorf("queue: complete group: nil group")
	}
	if group.Status != GroupStatusActive {
		return fmt.Errorf("queue: complete group: status %q is not active", group.Status)
	}
	if !allItemsTerminal(group) {
		return fmt.Errorf("queue: complete group: items are not terminal")
	}
	_, failures := countOutcomes(group)
	if failures == 0 {
		setGroupStatus(group, GroupStatusCompleteSuccess)
	} else {
		setGroupStatus(group, GroupStatusCompleteWithFailures)
	}
	stamp := completedAt.UTC()
	group.CompletedAt = &stamp
	return nil
}

// ActivatePendingGroup starts one pending successor at the supplied time.
func ActivatePendingGroup(group *Group, startedAt time.Time) error {
	if group == nil || group.Status != GroupStatusPending {
		return fmt.Errorf("queue: activate group: group is not pending")
	}
	setGroupStatus(group, GroupStatusActive)
	stamp := startedAt.UTC()
	group.StartedAt = &stamp
	return nil
}

func setItemStatus(item *Item, status ItemStatus)     { item.Status = status }
func setGroupStatus(group *Group, status GroupStatus) { group.Status = status }
func setQueueStatus(q *Queue, status QueueStatus)     { q.Status = status }
