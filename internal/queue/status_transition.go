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

// FailDeferredItem records that an upstream ledger dependency failed.
func FailDeferredItem(item *Item, blocker string) error {
	if item == nil {
		return fmt.Errorf("queue: fail deferred item: nil item")
	}
	if item.Status != ItemStatusDeferredForLedgerDep {
		return fmt.Errorf("queue: fail deferred item: status %q is not deferred", item.Status)
	}
	if blocker == "" {
		return fmt.Errorf("queue: fail deferred item: blocker is empty")
	}
	setItemStatus(item, ItemStatusFailed)
	item.LastFailureReason = "dependency_failed:" + blocker
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
	item.PreclaimTerminal = nil
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

// queueAcceptsItemCompletion reports whether a queue in this status may still
// record the terminal outcome of an item that is ALREADY in flight.
//
// Recording an outcome is neither a dispatch nor an advance. specs/queue-model.md
// §8.5 QM-054 is the sentence that authorises this: "No new items are
// dispatched while status == paused-by-drain. In-flight runs continue per
// ON-027 step (2)." §QM-031 says the neighbouring thing — a PENDING group does
// not start while the queue is not active. Neither says a run that finishes
// after the pause loses its result. A drain means "stop dispatching and let
// in-flight runs finish", so a guard that refuses the finish makes the drain
// impossible.
//
// Paused-by-failure is deliberately absent. Such a queue reached that status
// because its active group already went terminal, so it holds no in-flight
// item, and a completion aimed at it lands in the earlier terminal-group branch
// of resolveGroupCompletionTarget rather than here. Adding it would widen the
// gate for a case that never reaches it.
//
// Cancelled and completed are absent for the same reason and a stronger one:
// the queue is gone, and a write to it would resurrect a released name.
func queueAcceptsItemCompletion(status QueueStatus) bool {
	switch status {
	case QueueStatusActive, QueueStatusPausedByDrain, QueueStatusPausedByBudget:
		return true
	default:
		return false
	}
}

// PauseQueueForGroupFailure parks a queue after one named group has completed
// with failures. Later groups remain pending.
//
// It accepts the same statuses a completion accepts, not active alone. A group
// can fail DURING a drain, and refusing the transition then would leave the
// queue paused-by-drain: the resume-on-start bit auto-resumes it on the next
// daemon start, and the queue would then start the successor group PAST a group
// that failed. Failure outranks drain.
//
// Clearing ResumeOnStart is the load-bearing half of that. The bit says "this
// pause was mechanical, resume without asking", and it must not survive a
// pause that an operator has to look at.
//
// SPEC GAP, KNOWN: specs/queue-model.md §8.3 QM-052 describes the pause and
// says nothing about the resume bit, and QueueStatusPausedByBudget is not in
// that spec at all. The behaviour here is right and the spec has to catch up.
// Amendment tracked on its own bead.
func PauseQueueForGroupFailure(q *Queue, groupIndex int) error {
	if q == nil || !queueAcceptsItemCompletion(q.Status) {
		return fmt.Errorf("queue: pause queue for group failure: queue does not accept a completion")
	}
	for i := range q.Groups {
		if q.Groups[i].GroupIndex == groupIndex && q.Groups[i].Status == GroupStatusCompleteWithFailures {
			setQueueStatus(q, QueueStatusPausedByFailure)
			q.ResumeOnStart = false
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
//
// It accepts every status a completion accepts, plus the already-completed
// status for idempotence. Requiring active alone traded one stall for another:
// a drain that lands on the LAST group's last item leaves all the work done,
// the canonical file never unlinked and the queue name never released, and the
// boot reconciliation pass re-derives the same refusal, so nothing heals it.
func CompleteQueue(q *Queue) error {
	if q == nil {
		return fmt.Errorf("queue: complete queue: nil queue")
	}
	if !queueAcceptsItemCompletion(q.Status) && q.Status != QueueStatusCompleted {
		return fmt.Errorf("queue: complete queue: status %q does not accept a completion", q.Status)
	}
	if len(q.Groups) == 0 {
		return fmt.Errorf("queue: complete queue: queue has no groups")
	}
	for _, group := range q.Groups {
		if group.Status != GroupStatusCompleteSuccess {
			return fmt.Errorf("queue: complete queue: group %d has status %q", group.GroupIndex, group.Status)
		}
	}
	if q.Status != QueueStatusCompleted {
		setQueueStatus(q, QueueStatusCompleted)
		// A completed queue is unlinked and its name released, so a resume bit
		// left set describes a queue that no longer exists. Same known spec gap
		// as PauseQueueForGroupFailure above: §8.3 QM-052 does not describe this
		// write yet.
		q.ResumeOnStart = false
	}
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
