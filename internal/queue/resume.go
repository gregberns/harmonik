package queue

// resume.go — recovery transitions for a queue parked at paused-by-failure.
//
// §8.3 (QM-052) parks a queue at `paused-by-failure` when an active group
// reaches `complete-with-failures`; §A.3 reserves the `queue-resume` recovery
// verb (manual `paused-by-failure → active` after the operator addresses the
// failed beads) for v0.2. This file implements the pure state-mutation half of
// that verb so the daemon's operator-resume handler and the `harmonik queue
// retry` CLI can drive recovery without a daemon restart + fresh submit.
//
// The two entry points are intentionally narrow and side-effect-free (no I/O,
// no events): the caller persists (QM-001) and emits afterwards, mirroring the
// persist-before-emit discipline (QM-063) used by AdvanceGroup and
// ReevaluateDeferred.
//
//   - ResumeFromFailure — clear the paused-by-failure flag (status → active)
//     and re-arm every failed item so the dispatcher picks them up again.
//   - RearmFailedItems  — re-arm failed items in a single group without touching
//     queue-level status (the retry primitive).
//
// Bead ref: hk-fkpb7. Spec ref: specs/queue-model.md §8.3 QM-052, §A.3.

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
		// A group that reached complete-with-failures must re-open so the
		// state machine re-evaluates it (QM-032 makes terminal group states
		// absorbing, so it cannot self-resurrect). Groups that were still
		// pending/active when the queue parked are left as-is.
		if q.Groups[gi].Status == GroupStatusCompleteWithFailures {
			q.Groups[gi].Status = GroupStatusActive
			q.Groups[gi].CompletedAt = nil
		}
	}

	q.Status = QueueStatusActive
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
	var rearmed []core.BeadID
	for i := range g.Items {
		if g.Items[i].Status != ItemStatusFailed {
			continue
		}
		g.Items[i].Status = ItemStatusPending
		g.Items[i].Attempts = 0
		g.Items[i].LastFailureReason = ""
		rearmed = append(rearmed, g.Items[i].BeadID)
	}
	return rearmed
}
