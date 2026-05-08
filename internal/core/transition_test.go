package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// b3f77State returns a valid State for use in Transition test fixtures.
func b3f77State(t *testing.T) State {
	t.Helper()
	return State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     RunID(uuid.Must(uuid.NewV7())),
		NodeID:    NodeID("step-1"),
		EnteredAt: time.Now(),
		TransitionHistory: CommitRange{
			FirstCommitSHA: "aabbcc1",
			LastCommitSHA:  "ddeeff2",
		},
	}
}

// b3f77Confidence returns a *float64 for use in Transition test fixtures.
func b3f77Confidence(v float64) *float64 {
	return &v
}

// b3f77StateID returns a *StateID for use in rollback Transition test fixtures.
func b3f77StateID(t *testing.T) *StateID {
	t.Helper()
	id := StateID(uuid.Must(uuid.NewV7()))
	return &id
}

// b3f77ValidTransition returns a fully-populated forward Transition with all
// required fields set to valid values.
func b3f77ValidTransition(t *testing.T) Transition {
	t.Helper()
	return Transition{
		TransitionID:      TransitionID(uuid.Must(uuid.NewV7())),
		RunID:             RunID(uuid.Must(uuid.NewV7())),
		FromState:         b3f77State(t),
		ToState:           b3f77State(t),
		ActorRole:         ActorRoleBuilder,
		CandidateActions:  []ActionDescriptor{"action-a", "action-b"},
		ChosenAction:      ActionDescriptor("action-a"),
		PolicyVersion:     PolicyVersion("v1.0.0"),
		Evidence:          Evidence{"key": "value"},
		VerifierMetrics:   VerifierMetrics{"score": 0.95},
		Confidence:        b3f77Confidence(0.9),
		OutcomeStatus:     OutcomeStatusSuccess,
		TransitionKind:    TransitionKindForward,
		RollbackToStateID: nil,
		SchemaVersion:     1,
	}
}

func TestTransitionValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	if !tr.Valid() {
		t.Error("Valid() = false for fully-populated forward Transition, want true")
	}
}

func TestTransitionValid_ZeroTransitionID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionID = TransitionID(uuid.Nil)
	if tr.Valid() {
		t.Error("Valid() = true with zero TransitionID, want false")
	}
}

func TestTransitionValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.RunID = RunID(uuid.Nil)
	if tr.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestTransitionValid_InvalidFromState(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.FromState.NodeID = ""
	if tr.Valid() {
		t.Error("Valid() = true with invalid FromState, want false")
	}
}

func TestTransitionValid_InvalidToState(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ToState.RunID = RunID(uuid.Nil)
	if tr.Valid() {
		t.Error("Valid() = true with invalid ToState, want false")
	}
}

func TestTransitionValid_EmptyActorRole(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ActorRole = ActorRole("")
	if tr.Valid() {
		t.Error("Valid() = true with empty ActorRole, want false")
	}
}

func TestTransitionValid_UnknownActorRole(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ActorRole = ActorRole("unknown-role")
	if tr.Valid() {
		t.Error("Valid() = true with unknown ActorRole, want false")
	}
}

func TestTransitionValid_DaemonActorRole(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ActorRole = ActorRoleDaemon
	if !tr.Valid() {
		t.Error("Valid() = false for ActorRoleDaemon (synthesised outcome), want true")
	}
}

func TestTransitionValid_ReconciliationActorRole(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ActorRole = ActorRoleReconciliation
	if !tr.Valid() {
		t.Error("Valid() = false for ActorRoleReconciliation (synthesised outcome), want true")
	}
}

func TestTransitionValid_EmptyChosenAction(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ChosenAction = ActionDescriptor("")
	if tr.Valid() {
		t.Error("Valid() = true with empty ChosenAction, want false")
	}
}

func TestTransitionValid_EmptyPolicyVersion(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.PolicyVersion = PolicyVersion("")
	if tr.Valid() {
		t.Error("Valid() = true with empty PolicyVersion, want false")
	}
}

func TestTransitionValid_InvalidOutcomeStatus(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.OutcomeStatus = OutcomeStatus("UNKNOWN")
	if tr.Valid() {
		t.Error("Valid() = true with invalid OutcomeStatus, want false")
	}
}

func TestTransitionValid_InvalidTransitionKind(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKind("bogus")
	if tr.Valid() {
		t.Error("Valid() = true with invalid TransitionKind, want false")
	}
}

func TestTransitionValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.SchemaVersion = 0
	if tr.Valid() {
		t.Error("Valid() = true with zero SchemaVersion, want false")
	}
}

func TestTransitionValid_NegativeSchemaVersion(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.SchemaVersion = -1
	if tr.Valid() {
		t.Error("Valid() = true with negative SchemaVersion, want false")
	}
}

// TestTransitionValid_NilConfidence verifies that nil Confidence is accepted
// (the spec declares Float | None).
func TestTransitionValid_NilConfidence(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.Confidence = nil
	if !tr.Valid() {
		t.Error("Valid() = false with nil Confidence, want true (spec: Float | None)")
	}
}

// TestTransitionValid_NilEvidenceAndMetrics verifies that nil maps are
// accepted — the spec does not require non-nil maps.
func TestTransitionValid_NilEvidenceAndMetrics(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.Evidence = nil
	tr.VerifierMetrics = nil
	if !tr.Valid() {
		t.Error("Valid() = false with nil Evidence and VerifierMetrics, want true")
	}
}

// TestTransitionValid_EmptyCandidateActions verifies that an empty
// CandidateActions slice is accepted (e.g. daemon-synthesised transitions).
func TestTransitionValid_EmptyCandidateActions(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.CandidateActions = nil
	if !tr.Valid() {
		t.Error("Valid() = false with nil CandidateActions, want true")
	}
}

// --- EM-044 rollback_to_state_id constraint tests ---

// TestTransitionValid_ArchitecturalRollbackRequiresStateID verifies that
// architectural-rollback MUST set RollbackToStateID (EM-044).
func TestTransitionValid_ArchitecturalRollbackRequiresStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindArchitecturalRollback
	tr.RollbackToStateID = nil
	if tr.Valid() {
		t.Error("Valid() = true for architectural-rollback with nil RollbackToStateID, want false")
	}
}

// TestTransitionValid_ArchitecturalRollbackWithStateID verifies that
// architectural-rollback with a non-nil RollbackToStateID is valid (EM-044).
func TestTransitionValid_ArchitecturalRollbackWithStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindArchitecturalRollback
	tr.RollbackToStateID = b3f77StateID(t)
	if !tr.Valid() {
		t.Error("Valid() = false for architectural-rollback with non-nil RollbackToStateID, want true")
	}
}

// TestTransitionValid_PolicyRollbackRequiresStateID verifies that
// policy-rollback MUST set RollbackToStateID (EM-044).
func TestTransitionValid_PolicyRollbackRequiresStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindPolicyRollback
	tr.RollbackToStateID = nil
	if tr.Valid() {
		t.Error("Valid() = true for policy-rollback with nil RollbackToStateID, want false")
	}
}

// TestTransitionValid_PolicyRollbackWithStateID verifies that policy-rollback
// with a non-nil RollbackToStateID is valid (EM-044).
func TestTransitionValid_PolicyRollbackWithStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindPolicyRollback
	tr.RollbackToStateID = b3f77StateID(t)
	if !tr.Valid() {
		t.Error("Valid() = false for policy-rollback with non-nil RollbackToStateID, want true")
	}
}

// TestTransitionValid_ForwardMustOmitRollbackStateID verifies that forward
// transitions MUST NOT set RollbackToStateID (EM-044).
func TestTransitionValid_ForwardMustOmitRollbackStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindForward
	tr.RollbackToStateID = b3f77StateID(t)
	if tr.Valid() {
		t.Error("Valid() = true for forward transition with non-nil RollbackToStateID, want false")
	}
}

// TestTransitionValid_LocalPatchbackMustOmitRollbackStateID verifies that
// local-patchback MUST NOT set RollbackToStateID (EM-044).
func TestTransitionValid_LocalPatchbackMustOmitRollbackStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindLocalPatchback
	tr.RollbackToStateID = b3f77StateID(t)
	if tr.Valid() {
		t.Error("Valid() = true for local-patchback with non-nil RollbackToStateID, want false")
	}
}

// TestTransitionValid_ContextRestoreMustOmitRollbackStateID verifies that
// context-restore MUST NOT set RollbackToStateID (EM-044, EM-046).
func TestTransitionValid_ContextRestoreMustOmitRollbackStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindContextRestore
	tr.RollbackToStateID = b3f77StateID(t)
	if tr.Valid() {
		t.Error("Valid() = true for context-restore with non-nil RollbackToStateID, want false")
	}
}

// TestTransitionValid_LocalPatchbackOmitsRollbackStateID verifies that
// local-patchback with nil RollbackToStateID is valid (EM-044).
func TestTransitionValid_LocalPatchbackOmitsRollbackStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindLocalPatchback
	tr.RollbackToStateID = nil
	if !tr.Valid() {
		t.Error("Valid() = false for local-patchback with nil RollbackToStateID, want true")
	}
}

// TestTransitionValid_ContextRestoreOmitsRollbackStateID verifies that
// context-restore with nil RollbackToStateID is valid (EM-044, EM-046).
func TestTransitionValid_ContextRestoreOmitsRollbackStateID(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.TransitionKind = TransitionKindContextRestore
	tr.RollbackToStateID = nil
	if !tr.Valid() {
		t.Error("Valid() = false for context-restore with nil RollbackToStateID, want true")
	}
}

// TestTransitionValid_PartialSuccessStatus verifies that PARTIAL_SUCCESS is
// accepted as a valid OutcomeStatus (EM-023a).
func TestTransitionValid_PartialSuccessStatus(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.OutcomeStatus = OutcomeStatusPartialSuccess
	if !tr.Valid() {
		t.Error("Valid() = false with PARTIAL_SUCCESS OutcomeStatus, want true")
	}
}

// TestTransitionValid_SynthesisedEvidenceKey verifies that the reserved
// synthesized_outcome evidence key does not affect Valid().
func TestTransitionValid_SynthesisedEvidenceKey(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ActorRole = ActorRoleDaemon
	tr.Evidence = Evidence{"synthesized_outcome": true}
	if !tr.Valid() {
		t.Error("Valid() = false for daemon transition with synthesized_outcome evidence key, want true")
	}
}
