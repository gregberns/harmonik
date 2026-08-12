package daemon

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/queue"
)

type groupCompletionDurability struct {
	Disposition      queue.GroupCompletionDisposition
	Outcome          queue.NamespaceOutcome
	Phase            queue.CompletionPhase
	ObservationError bool
	CleanupError     bool
	MarkerError      bool
}

type groupCompletionEffects struct {
	Wake                 bool
	CancelQueueDrain     bool
	CancelQueueExit      bool
	EmitIntents          bool
	Refill               bool
	LogFailure           bool
	ObservationAttempted bool
	OwnershipReleased    bool
}

// decideGroupCompletionEffects maps durable facts to effects that remain at
// the daemon edge. QueueStore transactions already install or release their
// exact queue owner, so this policy never authorizes a second raw store write.
func decideGroupCompletionEffects(in groupCompletionDurability) (groupCompletionEffects, error) {
	if err := validateGroupCompletionDurability(in); err != nil {
		return groupCompletionEffects{}, err
	}
	if in.Disposition == queue.GroupCompletionDispositionReceiptRequired {
		return groupCompletionEffects{}, nil
	}
	out := groupCompletionEffects{Refill: true}
	if in.Disposition == queue.GroupCompletionDispositionNoChange {
		return out, nil
	}
	out.LogFailure = in.Outcome != queue.OutcomeCommittedDurable || in.ObservationError || in.CleanupError || in.MarkerError
	switch in.Disposition {
	// successor-held commits the same transaction as successor-activated and it
	// wakes the loop for the same reason: the item and its group changed on
	// disk, and the eager refill and the dispatch loop must both look again.
	// The successor group did not start, but that is the queue's pause talking,
	// not a reason to withhold the effects of the write that DID land.
	case queue.GroupCompletionDispositionIntermediate,
		queue.GroupCompletionDispositionSuccessorActivated,
		queue.GroupCompletionDispositionSuccessorHeld:
		if in.Outcome == queue.OutcomeCommittedDurable {
			out.Wake = true
			out.EmitIntents = true
		}
	case queue.GroupCompletionDispositionPausedByFailure:
		if in.Outcome == queue.OutcomeCommittedDurable {
			out.Wake = true
			out.CancelQueueExit = true
			out.EmitIntents = true
		}
	case queue.GroupCompletionDispositionQueueCompleted:
		out.ObservationAttempted = completionReachedObservation(in.Phase)
		out.OwnershipReleased = completionReleasedOwnership(in.Phase)
		if out.OwnershipReleased {
			out.CancelQueueDrain = true
			out.CancelQueueExit = true
		}
	default:
		out.LogFailure = true
	}
	return out, nil
}

func validateGroupCompletionDurability(in groupCompletionDurability) error {
	hasDiagnostics := in.ObservationError || in.CleanupError || in.MarkerError
	switch in.Disposition {
	case queue.GroupCompletionDispositionNoChange, queue.GroupCompletionDispositionReceiptRequired:
		if in.Outcome != "" || in.Phase != "" || hasDiagnostics {
			return fmt.Errorf("group completion %s has durability facts", in.Disposition)
		}
		return nil
	case queue.GroupCompletionDispositionIntermediate,
		queue.GroupCompletionDispositionSuccessorActivated,
		queue.GroupCompletionDispositionSuccessorHeld,
		queue.GroupCompletionDispositionPausedByFailure:
		return validateNonFinalGroupCompletionDurability(in)
	case queue.GroupCompletionDispositionQueueCompleted:
		return validateFinalGroupCompletionDurability(in)
	default:
		return fmt.Errorf("unknown group completion disposition %q", in.Disposition)
	}
}

func validateNonFinalGroupCompletionDurability(in groupCompletionDurability) error {
	if in.Phase != "" || in.ObservationError || in.MarkerError || !validNamespaceOutcome(in.Outcome) {
		return fmt.Errorf("group completion %s has invalid transaction facts", in.Disposition)
	}
	if in.CleanupError && in.Outcome != queue.OutcomeCommittedDurable {
		return fmt.Errorf("group completion cleanup diagnostic contradicts outcome %q", in.Outcome)
	}
	return nil
}

func validateFinalGroupCompletionDurability(in groupCompletionDurability) error {
	if !validFinalCompletionPhaseOutcome(in.Phase, in.Outcome) {
		return fmt.Errorf("group completion final phase %q contradicts outcome %q", in.Phase, in.Outcome)
	}
	if in.MarkerError != (in.Phase == queue.CompletionPhaseMarkerFailed) {
		return fmt.Errorf("group completion marker diagnostic contradicts phase %q", in.Phase)
	}
	if in.CleanupError && in.Phase != queue.CompletionPhaseObservationAttempted {
		return fmt.Errorf("group completion cleanup diagnostic contradicts phase %q", in.Phase)
	}
	if in.ObservationError && !completionReachedObservation(in.Phase) {
		return fmt.Errorf("group completion observation diagnostic contradicts phase %q", in.Phase)
	}
	return nil
}

func validNamespaceOutcome(outcome queue.NamespaceOutcome) bool {
	switch outcome {
	case queue.OutcomeRejected, queue.OutcomeNotCommitted, queue.OutcomeCommittedDurable, queue.OutcomeCommitIndeterminate:
		return true
	default:
		return false
	}
}

func validFinalCompletionPhaseOutcome(phase queue.CompletionPhase, outcome queue.NamespaceOutcome) bool {
	switch phase {
	case queue.CompletionPhaseRejected:
		return outcome == queue.OutcomeRejected
	case queue.CompletionPhaseNotCommitted:
		return outcome == queue.OutcomeNotCommitted
	case queue.CompletionPhaseCommitIndeterminate:
		return outcome == queue.OutcomeCommitIndeterminate
	case queue.CompletionPhaseCanonicalCommitted:
		return outcome == queue.OutcomeCommitIndeterminate
	case queue.CompletionPhaseReceiptDurable, queue.CompletionPhaseObservationAttempted, queue.CompletionPhaseCleaned,
		queue.CompletionPhaseOwnershipReleased, queue.CompletionPhaseMarkerDurable,
		queue.CompletionPhaseMarkerFailed:
		return outcome == queue.OutcomeCommittedDurable
	default:
		return false
	}
}

func completionReachedObservation(phase queue.CompletionPhase) bool {
	switch phase {
	case queue.CompletionPhaseObservationAttempted, queue.CompletionPhaseCleaned,
		queue.CompletionPhaseOwnershipReleased, queue.CompletionPhaseMarkerDurable,
		queue.CompletionPhaseMarkerFailed:
		return true
	default:
		return false
	}
}

func completionReleasedOwnership(phase queue.CompletionPhase) bool {
	switch phase {
	case queue.CompletionPhaseOwnershipReleased, queue.CompletionPhaseMarkerDurable, queue.CompletionPhaseMarkerFailed:
		return true
	default:
		return false
	}
}
