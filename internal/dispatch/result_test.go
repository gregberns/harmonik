package dispatch

import "testing"

func TestResultValidShapes(t *testing.T) {
	i := testIntent(PhaseClaimDurable)
	tests := []Result{
		{Class: ResultCommitted, Intent: &i},
		{Class: ResultReplayable, Intent: &i},
		{Class: ResultRefused, Reason: ReasonStateChanged},
		{Class: ResultRefused, Reason: ReasonExternalRefusal},
		{Class: ResultRepairRequired, Reason: ReasonIdentityConflict},
		{Class: ResultRepairRequired, Reason: ReasonCorruptFact},
		{Class: ResultRepairRequired, Reason: ReasonAuthorityUnavailable},
	}
	for _, result := range tests {
		if err := result.Validate(); err != nil {
			t.Fatalf("Validate(%+v): %v", result, err)
		}
	}
}

func TestResultRejectsContradictoryShapes(t *testing.T) {
	i := testIntent(PhasePrepared)
	tests := []Result{
		{},
		{Class: ResultCommitted},
		{Class: ResultCommitted, Intent: &i, Reason: ReasonStateChanged},
		{Class: ResultReplayable},
		{Class: ResultRefused, Intent: &i, Reason: ReasonStateChanged},
		{Class: ResultRefused},
		{Class: ResultRefused, Reason: ReasonIdentityConflict},
		{Class: ResultRepairRequired, Intent: &i, Reason: ReasonIdentityConflict},
		{Class: ResultRepairRequired},
		{Class: ResultRepairRequired, Reason: ReasonStateChanged},
	}
	for _, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("Validate(%+v) = nil", result)
		}
	}
}
