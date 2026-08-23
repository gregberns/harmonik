package queue

import "github.com/gregberns/harmonik/internal/core"

// ResumeFromFailure clears a queue's paused-by-failure parking and re-arms its
// failed items so the dispatcher resumes work on them.
//
// It is the in-memory mutation half of the §A.3 `queue-resume` recovery verb.
// When q.Status is paused-by-failure it:
//
//  1. Re-arms every failed item across all groups (failed → pending, Attempts
//     reset to 0, LastFailureReason cleared) so the dispatch-eligibility gates
//     in state.go (which skip items at or above MaxItemAttempts) admit them
//     again. See RearmFailedItems for the per-group mechanics.
//  2. Flips any group that had reached complete-with-failures back to active so
//     AdvanceGroup (QM-032: terminal group states are absorbing) re-evaluates it
//     once its re-armed items finish.
//  3. Transitions Queue.status paused-by-failure → active.
//
// It returns the bead IDs that were re-armed (deterministic group-then-item
// order, for the caller's diagnostics) and ok=true when a paused-by-failure
// queue was resumed. When q is nil or its status is anything other than
// paused-by-failure the call is an idempotent no-op: it returns (nil, false)
// and leaves q untouched (so a duplicate operator-resume, or a resume against a
// paused-by-drain / active / completed queue, is harmless — paused-by-drain has
// its own active↔drain recovery path in the daemon's QueueOperatorEventConsumer).
//
// ResumeFromFailure performs no I/O and emits no events; the caller MUST Persist
// (QM-001) before surfacing the resumed status, per QM-063.
//
// Bead ref: hk-fkpb7. Spec ref: specs/queue-model.md §8.3 QM-052, §A.3.
func ResumeFromFailure(q *Queue) (rearmed []core.BeadID, ok bool) {
	if q == nil || q.Status != QueueStatusPausedByFailure {
		return nil, false
	}

	for gi := range q.Groups {
		rearmed = append(rearmed, RearmFailedItems(&q.Groups[gi])...)
		if q.Groups[gi].Status == GroupStatusCompleteWithFailures {
			if err := ReactivateFailedGroup(&q.Groups[gi]); err != nil {
				return nil, false
			}
		}
	}

	if err := ResumeQueueFromFailure(q); err != nil {
		return nil, false
	}
	return rearmed, true
}

// RearmFailedItems re-arms every failed item in g so the dispatcher will retry
// it: failed → pending, Attempts reset to 0 (clearing the MaxItemAttempts
// skip-gate applied by waveEligible/streamEligible), and LastFailureReason
// cleared. It is the retry primitive backing `harmonik queue retry`.
//
// It mutates g in place and returns the re-armed bead IDs in item-list order.
// Non-failed items (pending, dispatched, completed, deferred-for-ledger-dep)
// are left untouched, so re-arming is safe to call on a partially-failed group:
// only the items that actually failed are reset. A nil group or a group with no
// failed items is a no-op (returns nil).
//
// RearmFailedItems does NOT change the group's status — the caller decides
// whether the group needs to re-open (ResumeFromFailure does this for a group
// parked at complete-with-failures). This keeps the per-group retry path
// composable with the queue-level resume path.
//
// Bead ref: hk-fkpb7.
func RearmFailedItems(g *Group) []core.BeadID {
	if g == nil {
		return nil
	}
	failedCount := 0
	for i := range g.Items {
		if g.Items[i].Status == ItemStatusFailed {
			failedCount++
		}
	}
	if failedCount == 0 {
		return nil
	}

	rearmed := make([]core.BeadID, 0, failedCount)
	for i := range g.Items {
		if g.Items[i].Status != ItemStatusFailed {
			continue
		}
		if err := ReactivateFailedItem(&g.Items[i]); err != nil {
			continue
		}
		rearmed = append(rearmed, g.Items[i].BeadID)
	}
	return rearmed
}
