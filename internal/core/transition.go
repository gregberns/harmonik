package core

import "github.com/google/uuid"

// Transition is the full AlphaGo trace record for a single move from one
// state to another in a workflow run (execution-model.md §4.1.EM-004, §6.1).
//
// The record is the canonical, authoritative durable form of every transition
// (EM-028). It is stored as a typed JSON sibling file at
// .harmonik/transitions/<run_id>/<transition_id>.json (EM-018, EM-019) and
// must be written atomically within a checkpoint commit (EM-016). The
// transition event on the event bus is a projection of this record (EM-028);
// the projection type is TransitionEventPayload.
//
// # Immutability contract (EM-020)
//
// Once committed to a checkpoint commit, a Transition record file MUST NEVER
// be rewritten. A new transition in the same run MUST be written under a new
// transition_id at a new sibling-file path; it MUST NOT modify any prior file.
// History-rewriting operations (git amend, rebase, filter-branch) against
// committed transition records are a policy violation detected by post-hoc
// audit tooling per §4.4.EM-020a (see hk-b3f.26 for the audit tool bead).
//
// The write-once guarantee is structural: each transition occupies a unique
// path (.harmonik/transitions/<run_id>/<transition_id>.json) derived from a
// daemon-generated UUIDv7 transition_id per EM-018a. No caller path appends
// to or overwrites an existing sibling file; callers MUST generate a new
// transition_id (and therefore a new path) for every new transition.
//
// # Field notes
//
// Evidence is the typed Evidence wrapper (execution-model.md §6.1). Reserved
// keys EvidenceKeySubWorkflowPin (EM-034c) and EvidenceKeySynthesizedOutcome
// (EM-023a) are declared as constants on the Evidence type.
//
// VerifierMetrics is the typed VerifierMetrics wrapper (execution-model.md
// §6.1). No reserved keys are cited by the spec at this version.
//
// # Schema compatibility
//
// Transition carries SchemaVersion under the N-1 readability contract of
// operator-nfr.md §4.5 (ON-018). A reader at version N-1 MUST successfully
// parse and interpret artifacts written at version N, treating additive fields
// as unknown but non-fatal. Breaking changes (rename or removal) require a
// migration release and must increment SchemaVersion. The current version is 1.
type Transition struct {
	// TransitionID is the UUIDv7 identifier for this transition.
	// Generated in the daemon process per EM-018a; unique within a run.
	// Must not be uuid.Nil.
	TransitionID TransitionID

	// RunID is the run this transition belongs to.
	// Must not be uuid.Nil.
	RunID RunID

	// FromState is the source state at the start of this transition.
	// Must be a valid State (FromState.Valid() == true).
	FromState State

	// ToState is the destination state after this transition.
	// Must be a valid State (ToState.Valid() == true).
	ToState State

	// ActorRole is the role name of the actor that produced this transition
	// (architecture.md §4.8.AR-032). For daemon-synthesised outcomes the value
	// is ActorRoleDaemon or ActorRoleReconciliation per EM-046.
	// Must be a declared ActorRole constant (ActorRole.Valid() == true).
	ActorRole ActorRole

	// CandidateActions is the full set of actions considered before ChoseAction
	// was selected. May be empty (e.g. for daemon-synthesised transitions where
	// no handler considered alternatives).
	CandidateActions []ActionDescriptor

	// ChosenAction is the action that was ultimately executed.
	// Must not be empty.
	ChosenAction ActionDescriptor

	// PolicyVersion identifies the policy snapshot under which the decision was
	// made (execution-model.md §6.1 RECORD Transition, field policy_version).
	// Must not be empty.
	PolicyVersion PolicyVersion

	// Evidence carries structured evidence for the transition.
	// Reserved keys: EvidenceKeySubWorkflowPin (EM-034c),
	// EvidenceKeySynthesizedOutcome (EM-023a). Large payloads MUST be
	// externalised per EM-021 and referenced by relative path from this map.
	Evidence Evidence

	// VerifierMetrics carries structured verifier metrics for this transition.
	// No reserved keys are cited by the spec at this version.
	VerifierMetrics VerifierMetrics

	// Confidence is the confidence score associated with this transition.
	// nil means unset (the spec declares Float | None).
	Confidence *float64

	// OutcomeStatus is the Outcome.status of the transition's associated
	// outcome. It drives the EM-023a durability decision. Must be a declared
	// OutcomeStatus constant (OutcomeStatus.Valid() == true).
	OutcomeStatus OutcomeStatus

	// TransitionKind is the kind of this transition per EM-044.
	// Must be a declared TransitionKind constant (TransitionKind.Valid() == true).
	TransitionKind TransitionKind

	// RollbackToStateID is the target earlier StateID for architectural-rollback
	// and policy-rollback transitions (EM-044). MUST be non-nil iff
	// TransitionKind ∈ {architectural-rollback, policy-rollback}. MUST be nil
	// for forward, local-patchback, and context-restore kinds.
	RollbackToStateID *StateID

	// SchemaVersion is the schema version of this record under the N-1
	// readability contract of operator-nfr.md §4.5 ON-018. The current
	// version is 1. Must be > 0.
	SchemaVersion int
}

// Valid reports whether the Transition record carries valid values for all
// required fields.
//
// Rules (per execution-model.md §4.1.EM-004, §4.10.EM-044, §6.1):
//   - TransitionID is not uuid.Nil
//   - RunID is not uuid.Nil
//   - FromState.Valid() is true
//   - ToState.Valid() is true
//   - ActorRole is a declared ActorRole constant
//   - ChosenAction is non-empty
//   - PolicyVersion is non-empty
//   - OutcomeStatus is a declared OutcomeStatus constant
//   - TransitionKind is a declared TransitionKind constant
//   - RollbackToStateID is non-nil iff TransitionKind ∈ {architectural-rollback,
//     policy-rollback}; nil for forward, local-patchback, context-restore
//   - SchemaVersion > 0
func (tr Transition) Valid() bool {
	if uuid.UUID(tr.TransitionID) == uuid.Nil {
		return false
	}
	if uuid.UUID(tr.RunID) == uuid.Nil {
		return false
	}
	if !tr.FromState.Valid() {
		return false
	}
	if !tr.ToState.Valid() {
		return false
	}
	if !tr.ActorRole.Valid() {
		return false
	}
	if !tr.ChosenAction.Valid() {
		return false
	}
	if !tr.PolicyVersion.Valid() {
		return false
	}
	if !tr.OutcomeStatus.Valid() {
		return false
	}
	if !tr.TransitionKind.Valid() {
		return false
	}
	// EM-044: rollback_to_state_id is required for architectural-rollback and
	// policy-rollback; must be absent for all other kinds.
	needsRollback := tr.TransitionKind == TransitionKindArchitecturalRollback ||
		tr.TransitionKind == TransitionKindPolicyRollback
	if needsRollback && tr.RollbackToStateID == nil {
		return false
	}
	if !needsRollback && tr.RollbackToStateID != nil {
		return false
	}
	if tr.SchemaVersion <= 0 {
		return false
	}
	return true
}
