package daemon

import (
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

func TestDecideGroupCompletionEffects(t *testing.T) {
	tests := []struct {
		name string
		in   groupCompletionDurability
		want groupCompletionEffects
	}{
		{name: "stale no change still refills", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionNoChange}, want: groupCompletionEffects{Refill: true}},
		{name: "receipt retry has no effects", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionReceiptRequired}, want: groupCompletionEffects{}},
		{name: "intermediate committed", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionIntermediate, Outcome: queue.OutcomeCommittedDurable}, want: groupCompletionEffects{Wake: true, EmitIntents: true, Refill: true}},
		{name: "successor commit failed", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionSuccessorActivated, Outcome: queue.OutcomeRejected}, want: groupCompletionEffects{Refill: true, LogFailure: true}},
		{name: "paused committed", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionPausedByFailure, Outcome: queue.OutcomeCommittedDurable}, want: groupCompletionEffects{Wake: true, CancelQueueExit: true, EmitIntents: true, Refill: true}},
		{name: "paused committed with cleanup diagnostic", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionPausedByFailure, Outcome: queue.OutcomeCommittedDurable, CleanupError: true}, want: groupCompletionEffects{Wake: true, CancelQueueExit: true, EmitIntents: true, Refill: true, LogFailure: true}},
		{name: "final not committed", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeNotCommitted, Phase: queue.CompletionPhaseNotCommitted}, want: groupCompletionEffects{Refill: true, LogFailure: true}},
		{name: "final observation attempted retains ownership", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseObservationAttempted, ObservationError: true}, want: groupCompletionEffects{Refill: true, LogFailure: true, ObservationAttempted: true}},
		{name: "final cleanup failure retains ownership", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseObservationAttempted, CleanupError: true}, want: groupCompletionEffects{Refill: true, LogFailure: true, ObservationAttempted: true}},
		{name: "final released marker failure", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseMarkerFailed, MarkerError: true}, want: groupCompletionEffects{CancelQueueDrain: true, CancelQueueExit: true, Refill: true, LogFailure: true, ObservationAttempted: true, OwnershipReleased: true}},
		{name: "final marker durable with observation diagnostic", in: groupCompletionDurability{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseMarkerDurable, ObservationError: true}, want: groupCompletionEffects{CancelQueueDrain: true, CancelQueueExit: true, Refill: true, LogFailure: true, ObservationAttempted: true, OwnershipReleased: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decideGroupCompletionEffects(tc.in)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("effects = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFinalCompletionNeverOuterEmitsOrWrites(t *testing.T) {
	tests := []struct {
		phase   queue.CompletionPhase
		outcome queue.NamespaceOutcome
		want    groupCompletionEffects
	}{
		{phase: queue.CompletionPhaseRejected, outcome: queue.OutcomeRejected, want: groupCompletionEffects{Refill: true, LogFailure: true}},
		{phase: queue.CompletionPhaseNotCommitted, outcome: queue.OutcomeNotCommitted, want: groupCompletionEffects{Refill: true, LogFailure: true}},
		{phase: queue.CompletionPhaseCommitIndeterminate, outcome: queue.OutcomeCommitIndeterminate, want: groupCompletionEffects{Refill: true, LogFailure: true}},
		{phase: queue.CompletionPhaseCanonicalCommitted, outcome: queue.OutcomeCommitIndeterminate, want: groupCompletionEffects{Refill: true, LogFailure: true}},
		{phase: queue.CompletionPhaseReceiptDurable, outcome: queue.OutcomeCommittedDurable, want: groupCompletionEffects{Refill: true}},
		{phase: queue.CompletionPhaseObservationAttempted, outcome: queue.OutcomeCommittedDurable, want: groupCompletionEffects{Refill: true, ObservationAttempted: true}},
		{phase: queue.CompletionPhaseCleaned, outcome: queue.OutcomeCommittedDurable, want: groupCompletionEffects{Refill: true, ObservationAttempted: true}},
		{phase: queue.CompletionPhaseOwnershipReleased, outcome: queue.OutcomeCommittedDurable, want: groupCompletionEffects{CancelQueueDrain: true, CancelQueueExit: true, Refill: true, ObservationAttempted: true, OwnershipReleased: true}},
		{phase: queue.CompletionPhaseMarkerFailed, outcome: queue.OutcomeCommittedDurable, want: groupCompletionEffects{CancelQueueDrain: true, CancelQueueExit: true, Refill: true, LogFailure: true, ObservationAttempted: true, OwnershipReleased: true}},
		{phase: queue.CompletionPhaseMarkerDurable, outcome: queue.OutcomeCommittedDurable, want: groupCompletionEffects{CancelQueueDrain: true, CancelQueueExit: true, Refill: true, ObservationAttempted: true, OwnershipReleased: true}},
	}
	for _, tc := range tests {
		in := groupCompletionDurability{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: tc.outcome, Phase: tc.phase}
		if tc.phase == queue.CompletionPhaseMarkerFailed {
			in.MarkerError = true
		}
		got, err := decideGroupCompletionEffects(in)
		if err != nil || got != tc.want {
			t.Fatalf("phase %q: effects=%+v err=%v, want %+v", tc.phase, got, err, tc.want)
		}
	}
}

func TestGroupCompletionEffectsRejectContradictoryFacts(t *testing.T) {
	tests := []groupCompletionDurability{
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommitIndeterminate, Phase: queue.CompletionPhaseMarkerDurable},
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable},
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: "invented"},
		{Disposition: queue.GroupCompletionDispositionIntermediate, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseCleaned},
		{Disposition: queue.GroupCompletionDispositionNoChange, Outcome: queue.OutcomeCommittedDurable},
		{Disposition: "invented"},
		{Disposition: queue.GroupCompletionDispositionIntermediate, Outcome: queue.OutcomeRejected, CleanupError: true},
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseReceiptDurable, ObservationError: true},
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseCleaned, CleanupError: true},
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseMarkerFailed},
		{Disposition: queue.GroupCompletionDispositionQueueCompleted, Outcome: queue.OutcomeCommittedDurable, Phase: queue.CompletionPhaseMarkerDurable, MarkerError: true},
	}
	for i, input := range tests {
		if got, err := decideGroupCompletionEffects(input); err == nil || got != (groupCompletionEffects{}) {
			t.Fatalf("invalid facts %d accepted: effects=%+v err=%v", i, got, err)
		}
	}
}

func TestGroupCompletionEffectsAcceptReceiptInstallFailureBoundary(t *testing.T) {
	// WriteReplacement reports this exact pair when the completed canonical is
	// durable but completion receipt installation is indeterminate.
	got, err := decideGroupCompletionEffects(groupCompletionDurability{
		Disposition: queue.GroupCompletionDispositionQueueCompleted,
		Outcome:     queue.OutcomeCommitIndeterminate,
		Phase:       queue.CompletionPhaseCanonicalCommitted,
	})
	want := groupCompletionEffects{Refill: true, LogFailure: true}
	if err != nil || got != want {
		t.Fatalf("effects=%+v err=%v, want %+v", got, err, want)
	}
}
