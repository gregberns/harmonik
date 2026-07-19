package orchestrator

// groupadvance.go — the pure EM-015f group-advance CLASSIFICATION micro-predicates,
// extracted from internal/daemon/workloop.go evaluateGroupAdvanceWithOutcome (M5
// slice 3C).
//
// DELIBERATELY NOT a monolithic planner. evaluateGroupAdvanceWithOutcome is
// mutate→classify→mutate→classify: the classification interleaves with TWO
// effectful queue.AdvanceGroup calls that mutate group state AND produce
// order-appended events. A single plan cannot be computed from a pre-pass status
// vector. These functions harvest ONLY the genuinely-pure classification bits;
// the daemon keeps both AdvanceGroup calls, all bus.Emit/event ordering, Persist,
// CompleteAndUnlink, Wake, and cancel-on-drain exactly where they are and in the
// same order.
//
// Each predicate takes plain strings/ints — never queue.* typed enums. The
// daemon projects queue.GroupStatus to its string value at the call boundary
// (as 3A/3B do), keeping orchestrator on $gostd + internal/core only.
//
// Spec ref: specs/execution-model.md §4.3 EM-015f.
// Bead ref: hk-45ude.

// Group-status string projections. These mirror the string values of
// queue.GroupStatusPending / GroupStatusCompleteSuccess /
// GroupStatusCompleteWithFailures (internal/queue/types.go). The daemon projects
// the typed enum to these strings at the boundary so orchestrator never imports
// internal/queue.
const (
	groupStatusPending              = "pending"
	groupStatusCompleteSuccess      = "complete-success"
	groupStatusCompleteWithFailures = "complete-with-failures"
)

// FirstPendingGroupIndex returns the index of the first group whose projected
// status is "pending", or -1 when none is pending. It drives the next-group
// activation decision: after a group reaches complete-success, the daemon
// activates the first still-pending group (the daemon then performs the
// effectful queue.AdvanceGroup on that group and collects its events).
func FirstPendingGroupIndex(statuses []string) int {
	for i, s := range statuses {
		if s == groupStatusPending {
			return i
		}
	}
	return -1
}

// GroupReachedSuccess reports whether a group's post-AdvanceGroup status is
// complete-success — the sole trigger for next-group activation (EM-015f).
func GroupReachedSuccess(newGroupStatus string) bool {
	return newGroupStatus == groupStatusCompleteSuccess
}

// GroupFailurePausesQueue reports whether a group's post-AdvanceGroup status is
// complete-with-failures — the classification that transitions the queue to
// paused-by-failure.
func GroupFailurePausesQueue(newGroupStatus string) bool {
	return newGroupStatus == groupStatusCompleteWithFailures
}

// AllGroupsSucceeded reports whether every group reached complete-success (with
// at least one group). This is the sole condition that triggers CompleteAndUnlink
// (QM-003 / hk-xsutm): a paused-by-failure queue retains queue.json, only the
// full-success case removes it. An empty slice returns false.
func AllGroupsSucceeded(statuses []string) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, s := range statuses {
		if s != groupStatusCompleteSuccess {
			return false
		}
	}
	return true
}
