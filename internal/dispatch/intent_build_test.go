package dispatch

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestIntentPhaseBuildersPreserveBinding(t *testing.T) {
	binding := testIntent(PhasePrepared).Binding
	prepared, err := NewPrepared(binding)
	if err != nil {
		t.Fatalf("NewPrepared: %v", err)
	}
	claim, err := prepared.WithClaimDurable()
	if err != nil {
		t.Fatalf("WithClaimDurable: %v", err)
	}
	runIntent, err := claim.WithRunDurable()
	if err != nil {
		t.Fatalf("WithRunDurable: %v", err)
	}
	handoff, err := runIntent.WithHandoffDurable("crew-charlie")
	if err != nil {
		t.Fatalf("WithHandoffDurable: %v", err)
	}
	if handoff.Binding != binding {
		t.Fatalf("binding changed: %+v", handoff.Binding)
	}
	if handoff.Run == nil || handoff.Run.RecordRunID != binding.RunID {
		t.Fatalf("run binding = %+v", handoff.Run)
	}
	if handoff.Handoff == nil || handoff.Handoff.SessionName != "crew-charlie" || handoff.Handoff.WorktreeLeaseRunID != binding.RunID {
		t.Fatalf("handoff binding = %+v", handoff.Handoff)
	}
}

func TestIntentPhaseBuildersRejectSkippedOrRepeatedPhases(t *testing.T) {
	prepared, err := NewPrepared(testIntent(PhasePrepared).Binding)
	if err != nil {
		t.Fatalf("NewPrepared: %v", err)
	}
	if _, err := prepared.WithRunDurable(); err == nil {
		t.Fatal("WithRunDurable skipped claim phase")
	}
	claim, err := prepared.WithClaimDurable()
	if err != nil {
		t.Fatalf("WithClaimDurable: %v", err)
	}
	if _, err := claim.WithClaimDurable(); err == nil {
		t.Fatal("WithClaimDurable repeated phase")
	}
}

func TestWithClaimRefusedBuildsOnlyActionableCauses(t *testing.T) {
	prepared := testIntent(PhasePrepared)
	for _, cause := range []ClaimRefusalCause{ClaimRefusalDependency, ClaimRefusalSupportedNonOpen} {
		refused, err := prepared.WithClaimRefused(cause)
		if err != nil {
			t.Fatalf("cause %q: %v", cause, err)
		}
		if refused.Binding != prepared.Binding || refused.Refusal == nil || refused.Refusal.Cause != cause {
			t.Fatalf("refused intent = %+v", refused)
		}
	}
	if _, err := prepared.WithClaimRefused("already_assigned"); err == nil {
		t.Fatal("WithClaimRefused accepted non-actionable ownership conflict")
	}
	refused := testIntent(PhaseClaimRefused)
	if _, err := refused.WithClaimRefused(ClaimRefusalDependency); err == nil {
		t.Fatal("WithClaimRefused accepted repeated transition")
	}
}

func TestIntentPhaseBuildersRejectInvalidPredecessors(t *testing.T) {
	prepared := testIntent(PhasePrepared)
	prepared.Binding.BeadID = ""
	if _, err := prepared.WithClaimDurable(); err == nil {
		t.Fatal("WithClaimDurable accepted invalid prepared predecessor")
	}
	claim := testIntent(PhaseClaimDurable)
	claim.Run = &RunBinding{RecordRunID: claim.Binding.RunID}
	if _, err := claim.WithRunDurable(); err == nil {
		t.Fatal("WithRunDurable accepted invalid claim predecessor")
	}
	runIntent := testIntent(PhaseRunDurable)
	runIntent.Handoff = &HandoffBinding{SessionName: "early", WorktreeLeaseRunID: runIntent.Binding.RunID}
	if _, err := runIntent.WithHandoffDurable("crew-charlie"); err == nil {
		t.Fatal("WithHandoffDurable accepted invalid run predecessor")
	}
}

func TestWithHandoffDurableDetachesRunBinding(t *testing.T) {
	prior := testIntent(PhaseRunDurable)
	next, err := prior.WithHandoffDurable("crew-charlie")
	if err != nil {
		t.Fatalf("WithHandoffDurable: %v", err)
	}
	next.Run.RecordRunID = core.RunID{}
	if prior.Run.RecordRunID != prior.Binding.RunID {
		t.Fatalf("predecessor run binding changed: %+v", prior.Run)
	}
}
