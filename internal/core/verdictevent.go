package core

import "github.com/google/uuid"

// VerdictEvent is the payload type for Outcome.Payload when
// Outcome.Kind == OutcomeKindReconciliationVerdict
// (reconciliation/schemas.md §6.1 RECORD VerdictEvent, RC-022a).
//
// The struct carries the reconciliation investigator's verdict plus the
// evidence and context references needed by the daemon's verdict-executor
// (RC-025). Valid() enforces all structural invariants.
//
// # Structural invariants (enforced by Valid)
//
//   - Verdict is non-empty and one of the seven declared constants.
//   - InvestigatorRunID is not uuid.Nil.
//   - TargetRunID is not uuid.Nil.
//   - Context is non-empty iff Verdict == VerdictResumeWithContext (RC-022a).
//   - CheckpointRef is non-nil iff Verdict == VerdictResetToCheckpoint.
//   - SnapshotToken.Valid() returns true.
//   - SchemaVersion is > 0.
//
// # Schema compatibility
//
// SchemaVersion follows the N-1 readability contract of operator-nfr.md §4.5
// (ON-018). Additive-only field additions are non-breaking. Renaming or
// removing fields is breaking and MUST increment SchemaVersion.
// The current schema version is 1.
type VerdictEvent struct {
	// Verdict is the investigator's decision. Required; must be one of the
	// seven declared Verdict constants per reconciliation/schemas.md §6.1.
	Verdict Verdict

	// InvestigatorRunID is the run_id of the reconciliation workflow that
	// produced this verdict. Must not be uuid.Nil.
	InvestigatorRunID uuid.UUID

	// TargetRunID is the run_id of the outer run being reconciled.
	// Must not be uuid.Nil.
	TargetRunID uuid.UUID

	// EvidenceRef is an optional git commit hash of the reconciliation commit
	// carrying evidence. Nil when no evidence commit has been created.
	EvidenceRef *string

	// Context carries investigator-supplied text injected into the run's shared
	// context (execution-model.md §4.1 EM-005) when Verdict ==
	// VerdictResumeWithContext. MUST be non-nil and non-empty iff Verdict ==
	// VerdictResumeWithContext (RC-022a). MUST be nil or empty otherwise.
	Context *string

	// CheckpointRef is the transition_id identifying the earlier checkpoint to
	// roll back to (reconciliation/schemas.md §6.1). MUST be non-nil iff
	// Verdict == VerdictResetToCheckpoint; MUST be nil otherwise.
	//
	// The spec declares this field as UUID | None (a transition_id value). The
	// TransitionID type already exists in internal/core/transitionid.go and is
	// used here directly.
	CheckpointRef *TransitionID

	// SnapshotToken bounds the investigator's view of the system state at
	// dispatch time; consumed by the staleness check at verdict-execution time
	// per RC-024 (reconciliation/schemas.md §6.1).
	SnapshotToken SnapshotToken

	// SchemaVersion is the schema version of this record. N-1 readable per
	// operator-nfr.md §4.5 ON-018. The current schema version is 1.
	// Must be > 0.
	SchemaVersion int
}

// Valid reports whether all structural invariants of the VerdictEvent are
// satisfied.
//
// Rules per reconciliation/schemas.md §6.1 and RC-022a:
//   - Verdict is non-empty and a declared constant (Valid() true).
//   - InvestigatorRunID is not uuid.Nil.
//   - TargetRunID is not uuid.Nil.
//   - Context is non-empty iff Verdict == VerdictResumeWithContext (RC-022a).
//   - CheckpointRef is non-nil iff Verdict == VerdictResetToCheckpoint.
//   - SnapshotToken.Valid() returns true.
//   - SchemaVersion is > 0.
func (e VerdictEvent) Valid() bool {
	if !e.Verdict.Valid() {
		return false
	}
	if e.InvestigatorRunID == uuid.Nil {
		return false
	}
	if e.TargetRunID == uuid.Nil {
		return false
	}

	// RC-022a: context non-empty iff verdict=resume-with-context.
	contextPresent := e.Context != nil && *e.Context != ""
	if e.Verdict == VerdictResumeWithContext && !contextPresent {
		return false
	}
	if e.Verdict != VerdictResumeWithContext && contextPresent {
		return false
	}

	// checkpoint_ref non-nil iff verdict=reset-to-checkpoint.
	if e.Verdict == VerdictResetToCheckpoint && e.CheckpointRef == nil {
		return false
	}
	if e.Verdict != VerdictResetToCheckpoint && e.CheckpointRef != nil {
		return false
	}

	if !e.SnapshotToken.Valid() {
		return false
	}
	if e.SchemaVersion <= 0 {
		return false
	}
	return true
}
