package core

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ctxRestoreEnforceFixtureTransitionBase returns a Transition skeleton suitable
// for use with NewContextRestoreTransition. All required fields are set except
// TransitionKind, OutcomeStatus, ActorRole, and Evidence (which are enforced by
// the constructor). Helper prefix: ctxRestoreEnforce per hk-b3f.107.
func ctxRestoreEnforceFixtureTransitionBase(t *testing.T, runID RunID) Transition {
	t.Helper()

	now := time.Now()
	fromState := State{
		StateID:   StateID(uuid.MustParse("01942b3c-0000-7000-8000-000000001070")),
		RunID:     runID,
		NodeID:    NodeID("review-node"),
		EnteredAt: now,
		TransitionHistory: CommitRange{
			FirstCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LastCommitSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	toState := State{
		StateID:   StateID(uuid.MustParse("01942b3c-0000-7000-8000-000000001071")),
		RunID:     runID,
		NodeID:    NodeID("review-node"),
		EnteredAt: now,
		TransitionHistory: CommitRange{
			FirstCommitSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LastCommitSHA:  "cccccccccccccccccccccccccccccccccccccccc",
		},
	}
	return Transition{
		TransitionID:  TransitionID(uuid.MustParse("01942b3c-0000-7000-8000-000000001072")),
		RunID:         runID,
		FromState:     fromState,
		ToState:       toState,
		ChosenAction:  ActionDescriptor("context-restore"),
		PolicyVersion: PolicyVersion("v1.0.0"),
		SchemaVersion: 1,
		// TransitionKind, OutcomeStatus, ActorRole, Evidence are set by constructor.
	}
}

// ctxRestoreEnforceFixtureRunID allocates a fixed RunID for context-restore
// enforcement tests (hk-b3f.107).
func ctxRestoreEnforceFixtureRunID() RunID {
	return RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000001073"))
}

// --- ValidateContextRestoreInitiationSource ---

// TestContextRestoreEnforce_ValidateDaemonPermitted verifies that
// ActorRoleDaemon is permitted to initiate a context-restore transition (EM-046).
func TestContextRestoreEnforce_ValidateDaemonPermitted(t *testing.T) {
	t.Parallel()

	err := ValidateContextRestoreInitiationSource(TransitionKindContextRestore, ActorRoleDaemon)
	if err != nil {
		t.Errorf("expected nil error for daemon initiation, got: %v", err)
	}
}

// TestContextRestoreEnforce_ValidateReconciliationPermitted verifies that
// ActorRoleReconciliation is permitted to initiate a context-restore
// transition (EM-046).
func TestContextRestoreEnforce_ValidateReconciliationPermitted(t *testing.T) {
	t.Parallel()

	err := ValidateContextRestoreInitiationSource(TransitionKindContextRestore, ActorRoleReconciliation)
	if err != nil {
		t.Errorf("expected nil error for reconciliation initiation, got: %v", err)
	}
}

// TestContextRestoreEnforce_ValidateHandlerRolesForbidden verifies that all
// handler roles (Planner, Researcher, Builder, Reviewer, Verifier, Scheduler,
// Governor) are rejected when they attempt to initiate a context-restore
// transition (EM-046).
func TestContextRestoreEnforce_ValidateHandlerRolesForbidden(t *testing.T) {
	t.Parallel()

	handlerRoles := []ActorRole{
		ActorRolePlanner,
		ActorRoleResearcher,
		ActorRoleBuilder,
		ActorRoleReviewer,
		ActorRoleVerifier,
		ActorRoleScheduler,
		ActorRoleGovernor,
	}
	for _, role := range handlerRoles {
		err := ValidateContextRestoreInitiationSource(TransitionKindContextRestore, role)
		if err == nil {
			t.Errorf("expected error for handler role %q initiating context-restore (EM-046), got nil", role)
			continue
		}
		if !errors.Is(err, ErrContextRestoreHandlerForbidden) {
			t.Errorf("handler role %q: want errors.Is(err, ErrContextRestoreHandlerForbidden), got: %v", role, err)
		}
	}
}

// TestContextRestoreEnforce_ValidateNonContextRestoreKindsUnrestricted verifies
// that ValidateContextRestoreInitiationSource returns nil for non-context-restore
// transition kinds regardless of actor role.
func TestContextRestoreEnforce_ValidateNonContextRestoreKindsUnrestricted(t *testing.T) {
	t.Parallel()

	nonRestoreKinds := []TransitionKind{
		TransitionKindForward,
		TransitionKindLocalPatchback,
		TransitionKindArchitecturalRollback,
		TransitionKindPolicyRollback,
	}
	for _, kind := range nonRestoreKinds {
		// Even a handler role is fine for non-context-restore kinds.
		err := ValidateContextRestoreInitiationSource(kind, ActorRolePlanner)
		if err != nil {
			t.Errorf("kind %q with Planner role: expected nil error, got: %v", kind, err)
		}
	}
}

// --- NewContextRestoreTransition ---

// TestContextRestoreEnforce_NewDaemonInitiated verifies that
// NewContextRestoreTransition succeeds for ActorRoleDaemon and enforces the
// daemon-synthesized field invariants (EM-046 + EM-023a).
func TestContextRestoreEnforce_NewDaemonInitiated(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)

	tr, err := NewContextRestoreTransition(base, ActorRoleDaemon)
	if err != nil {
		t.Fatalf("NewContextRestoreTransition(daemon): unexpected error: %v", err)
	}

	if tr.TransitionKind != TransitionKindContextRestore {
		t.Errorf("TransitionKind = %q, want %q (EM-046)", tr.TransitionKind, TransitionKindContextRestore)
	}
	if tr.OutcomeStatus != OutcomeStatusSuccess {
		t.Errorf("OutcomeStatus = %q, want %q (EM-046 synthesized outcome)", tr.OutcomeStatus, OutcomeStatusSuccess)
	}
	if tr.ActorRole != ActorRoleDaemon {
		t.Errorf("ActorRole = %q, want %q (EM-046)", tr.ActorRole, ActorRoleDaemon)
	}
	synthesized, _ := tr.Evidence[EvidenceKeySynthesizedOutcome].(bool)
	if !synthesized {
		t.Errorf("Evidence[%q] = %v, want true (EM-023a synthesized-outcome marker)", EvidenceKeySynthesizedOutcome, tr.Evidence[EvidenceKeySynthesizedOutcome])
	}
}

// TestContextRestoreEnforce_NewReconciliationInitiated verifies that
// NewContextRestoreTransition succeeds for ActorRoleReconciliation (EM-046).
func TestContextRestoreEnforce_NewReconciliationInitiated(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)

	tr, err := NewContextRestoreTransition(base, ActorRoleReconciliation)
	if err != nil {
		t.Fatalf("NewContextRestoreTransition(reconciliation): unexpected error: %v", err)
	}

	if tr.ActorRole != ActorRoleReconciliation {
		t.Errorf("ActorRole = %q, want %q (EM-046)", tr.ActorRole, ActorRoleReconciliation)
	}
	if tr.TransitionKind != TransitionKindContextRestore {
		t.Errorf("TransitionKind = %q, want %q", tr.TransitionKind, TransitionKindContextRestore)
	}
}

// TestContextRestoreEnforce_NewHandlerRejected verifies that
// NewContextRestoreTransition returns ErrContextRestoreHandlerForbidden for
// any handler actor role (EM-046).
func TestContextRestoreEnforce_NewHandlerRejected(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)

	handlerRoles := []ActorRole{
		ActorRolePlanner,
		ActorRoleBuilder,
		ActorRoleReviewer,
	}
	for _, role := range handlerRoles {
		_, err := NewContextRestoreTransition(base, role)
		if err == nil {
			t.Errorf("expected error for handler role %q, got nil", role)
			continue
		}
		if !errors.Is(err, ErrContextRestoreHandlerForbidden) {
			t.Errorf("handler role %q: want errors.Is(err, ErrContextRestoreHandlerForbidden), got: %v", role, err)
		}
	}
}

// TestContextRestoreEnforce_NewRejectsNonNilRollbackToStateID verifies that
// NewContextRestoreTransition rejects a base Transition with a non-nil
// RollbackToStateID per EM-044/EM-046 (context-restore must not relocate graph
// position).
func TestContextRestoreEnforce_NewRejectsNonNilRollbackToStateID(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)
	target := StateID(uuid.MustParse("01942b3c-0000-7000-8000-000000001074"))
	base.RollbackToStateID = &target

	_, err := NewContextRestoreTransition(base, ActorRoleDaemon)
	if err == nil {
		t.Error("expected error for non-nil RollbackToStateID in context-restore (EM-044/EM-046), got nil")
	}
}

// TestContextRestoreEnforce_NewSynthesizedOutcomeMarker verifies that the
// EvidenceKeySynthesizedOutcome key is set to true in the Evidence map of the
// resulting Transition, per EM-023a (synthesized outcome marker).
func TestContextRestoreEnforce_NewSynthesizedOutcomeMarker(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)

	tr, err := NewContextRestoreTransition(base, ActorRoleDaemon)
	if err != nil {
		t.Fatalf("NewContextRestoreTransition: %v", err)
	}

	if tr.Evidence == nil {
		t.Fatal("Evidence is nil, want non-nil with synthesized_outcome key (EM-023a)")
	}
	val, ok := tr.Evidence[EvidenceKeySynthesizedOutcome]
	if !ok {
		t.Errorf("Evidence[%q] absent, want true (EM-023a)", EvidenceKeySynthesizedOutcome)
	}
	if b, _ := val.(bool); !b {
		t.Errorf("Evidence[%q] = %v, want true (EM-023a)", EvidenceKeySynthesizedOutcome, val)
	}
}

// TestContextRestoreEnforce_NewPreservesExistingEvidence verifies that
// NewContextRestoreTransition preserves any existing evidence keys in the base
// Transition while adding EvidenceKeySynthesizedOutcome.
func TestContextRestoreEnforce_NewPreservesExistingEvidence(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)
	base.Evidence = Evidence{"custom_key": "custom_value"}

	tr, err := NewContextRestoreTransition(base, ActorRoleDaemon)
	if err != nil {
		t.Fatalf("NewContextRestoreTransition: %v", err)
	}

	if tr.Evidence["custom_key"] != "custom_value" {
		t.Error("NewContextRestoreTransition must preserve existing evidence keys")
	}
	synthesized, _ := tr.Evidence[EvidenceKeySynthesizedOutcome].(bool)
	if !synthesized {
		t.Errorf("Evidence[%q] = %v, want true (EM-023a)", EvidenceKeySynthesizedOutcome, tr.Evidence[EvidenceKeySynthesizedOutcome])
	}
}

// TestContextRestoreEnforce_NewTransitionPassesValid verifies that a
// NewContextRestoreTransition result, with all required fields populated, passes
// Transition.Valid() (EM-046 + EM-023a).
func TestContextRestoreEnforce_NewTransitionPassesValid(t *testing.T) {
	t.Parallel()

	runID := ctxRestoreEnforceFixtureRunID()
	base := ctxRestoreEnforceFixtureTransitionBase(t, runID)

	tr, err := NewContextRestoreTransition(base, ActorRoleDaemon)
	if err != nil {
		t.Fatalf("NewContextRestoreTransition: %v", err)
	}

	if !tr.Valid() {
		t.Error("daemon-synthesized context-restore Transition must pass Valid() (EM-046 + EM-023a)")
	}
}

// TestContextRestoreEnforce_ErrSentinelIsDistinct verifies that
// ErrContextRestoreHandlerForbidden is a distinct error value that can be
// tested with errors.Is.
func TestContextRestoreEnforce_ErrSentinelIsDistinct(t *testing.T) {
	t.Parallel()

	err := ValidateContextRestoreInitiationSource(TransitionKindContextRestore, ActorRolePlanner)
	if !errors.Is(err, ErrContextRestoreHandlerForbidden) {
		t.Errorf("ValidateContextRestoreInitiationSource should wrap ErrContextRestoreHandlerForbidden; got: %v", err)
	}

	// Confirm it does NOT match unrelated sentinel.
	if errors.Is(err, ErrRetryCapExhausted) {
		t.Error("ErrContextRestoreHandlerForbidden must not match ErrRetryCapExhausted")
	}
}
