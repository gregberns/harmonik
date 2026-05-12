package operatornfr

// ReviewLoopPhase names the current activity within a review-loop iteration.
// It is the human-readable discriminator rendered by `harmonik status` inline
// when a run's resolved workflow_mode is review-loop.
//
// Spec ref: operator-nfr.md §4.9 ON-035a — "review-loop information is
// rendered inline in `harmonik status` when a run's resolved `workflow_mode`
// is `review-loop`"; event-model.md §8.1a emission-ordering rule.
type ReviewLoopPhase string

// Declared ReviewLoopPhase constants; their values match the event-model.md
// §8.1a emission ordering so that the current phase is derivable from the
// last emitted review-loop event for a given run.
const (
	// ReviewLoopPhaseImplementing is active while the implementer agent is
	// running (between run_started / implementer_resumed and reviewer_launched).
	//
	// Spec ref: event-model.md §8.1a.1 implementer_resumed;
	// §8.1a emission-ordering rule step (a).
	ReviewLoopPhaseImplementing ReviewLoopPhase = "implementing"

	// ReviewLoopPhaseReviewing is active while the reviewer agent is running
	// (between reviewer_launched and reviewer_verdict).
	//
	// Spec ref: event-model.md §8.1a.2 reviewer_launched;
	// §8.1a emission-ordering rule step (b).
	ReviewLoopPhaseReviewing ReviewLoopPhase = "reviewing"

	// ReviewLoopPhaseDone is set once review_loop_cycle_complete has been
	// emitted. The run is at its terminal boundary.
	//
	// Spec ref: event-model.md §8.1a.6 review_loop_cycle_complete;
	// §8.1a emission-ordering rule — terminal phase.
	ReviewLoopPhaseDone ReviewLoopPhase = "done"
)

// ReviewLoopIterationState holds the three fields that `harmonik status` MUST
// render inline when a run's resolved workflow_mode is review-loop, per
// operator-nfr.md §4.9 ON-035a.
//
// The fields are derived from the review-loop event stream declared in
// event-model.md §8.1a: IterationCount from `implementer_resumed.iteration_count`
// / `reviewer_verdict.iteration_count`, LastVerdict from
// `reviewer_verdict.verdict`, and Phase from the most recently observed
// §8.1a event type.
//
// Spec ref: operator-nfr.md §4.9 ON-035a; event-model.md §8.1a.
type ReviewLoopIterationState struct {
	// IterationCount is the 1-based index of the current (or final) iteration.
	// Sourced from the `iteration_count` field present on every §8.1a event
	// payload per event-model.md §8.1a.1–§8.1a.5.
	//
	// Spec ref: event-model.md §8.1a.1 implementer_resumed.iteration_count.
	IterationCount int

	// LastVerdict is the most recently received reviewer verdict string.
	// The value is one of "APPROVE", "REQUEST_CHANGES", or "BLOCK" per
	// event-model.md §8.1a.3 reviewer_verdict.verdict.
	// Empty string when no verdict has been received yet (phase = implementing
	// on the first iteration).
	//
	// Spec ref: event-model.md §8.1a.3 reviewer_verdict.verdict ∈
	// {APPROVE, REQUEST_CHANGES, BLOCK}.
	LastVerdict string

	// Phase is the current activity within the review-loop cycle, derived
	// from the most recently observed §8.1a event type per the emission
	// ordering rule in event-model.md §8.1a.
	//
	// Spec ref: operator-nfr.md §4.9 ON-035a — "current phase inline";
	// event-model.md §8.1a emission-ordering rule.
	Phase ReviewLoopPhase
}

// Valid reports whether s is well-formed:
//   - IterationCount must be ≥ 1 (review-loop counts from 1).
//   - Phase must be one of the declared ReviewLoopPhase constants.
//   - LastVerdict, when non-empty, must be one of the three recognised verdict
//     strings.
func (s ReviewLoopIterationState) Valid() bool {
	if s.IterationCount < 1 {
		return false
	}
	switch s.Phase {
	case ReviewLoopPhaseImplementing, ReviewLoopPhaseReviewing, ReviewLoopPhaseDone:
	default:
		return false
	}
	if s.LastVerdict != "" {
		switch s.LastVerdict {
		case "APPROVE", "REQUEST_CHANGES", "BLOCK":
		default:
			return false
		}
	}
	return true
}
