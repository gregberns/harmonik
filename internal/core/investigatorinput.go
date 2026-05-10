package core

import "github.com/google/uuid"

// InvestigatorInput documents the logical fields the reconciliation investigator
// MUST be able to retrieve via its skills, bounded by the snapshot token (RC-015).
//
// LOGICAL VIEW: this is NOT a daemon-assembled record. The daemon does NOT
// assemble a single InvestigatorInput.json file and hand it to the investigator.
// Per RC-015, the investigator self-assembles these fields at runtime by querying:
//   - Beads-CLI (bead_record, target_bead_id, category)
//   - git-inspection (last_checkpoint, workspace_observation)
//   - bounded JSONL reader (jsonl_tail, per RC-014)
//   - workspace-inspection (workspace_observation, session_log_ref)
//
// All queries are bounded by snapshot_token so the investigator's view is
// consistent with the system state at the moment of dispatch.
//
// Spec reference: specs/reconciliation/schemas.md §6.1 RECORD InvestigatorInput.
//
// # Structural invariants (enforced by Valid)
//
//   - SnapshotToken passes SnapshotToken.Valid().
//   - TargetRunID is not the zero UUID.
//   - TargetWorkflowID is not the zero UUID.
//   - TargetWorkflowVersion is non-empty.
//   - LastCheckpoint passes Checkpoint.Valid().
//   - LastTransition passes Transition.Valid().
//   - WorkspaceObservation passes WorkspaceObservation.Valid().
//   - Category is a declared ReconciliationCategory constant.
//   - PlaybookRef is non-empty.
//   - BudgetWallClockSeconds is positive (> 0).
type InvestigatorInput struct {
	// SnapshotToken bounds the investigator's view of all three stores
	// (git, Beads, JSONL) per RC-015. Must pass SnapshotToken.Valid().
	SnapshotToken SnapshotToken

	// TargetRunID is the outer run being reconciled. Must not be the zero UUID.
	TargetRunID RunID

	// TargetWorkflowID is the workflow definition ID of the outer run.
	// Must not be the zero UUID.
	TargetWorkflowID WorkflowID

	// TargetWorkflowVersion is the pinned workflow version string for the
	// outer run. Non-empty required.
	TargetWorkflowVersion string

	// TargetBeadID is the Beads bead ID when the outer run is bead-bound,
	// or nil when the run has no associated bead. Nullable per spec
	// ("String | None").
	TargetBeadID *string

	// BeadRecord is the Beads record as-of SnapshotToken.BeadsAuditEntryID,
	// or nil when TargetBeadID is nil (no associated bead). Nullable per spec
	// ("BeadRecord | None").
	BeadRecord *BeadRecord

	// LastCheckpoint is the tip of the target run's task branch at the time
	// the snapshot was captured. Must pass Checkpoint.Valid().
	LastCheckpoint Checkpoint

	// LastTransition is the full transition record at LastCheckpoint, per
	// execution-model.md §6.1. Must pass Transition.Valid().
	LastTransition Transition

	// JSONLTail contains the events since LastCheckpoint, for observational
	// purposes only (RC-014). The investigator MUST NOT treat JSONL-tail
	// events as authoritative state; git and Beads are the authoritative
	// stores. Nil and empty slices are both valid.
	//
	// TODO(hk-63oh.82): replace []string with []EventEnvelope once the
	// EventEnvelope typed alias is defined (event-model.md §4.1). Using
	// []string placeholder per typed-alias-deferral protocol.
	JSONLTail []string

	// WorkspaceObservation is the point-in-time read-only observation of the
	// run's worktree: path existence, branch tip, WIP status, and any
	// git-in-progress operation. Renamed from workspace_state on 2026-05-09
	// per hk-63oh.80 to avoid cross-spec collision with workspace-model.md
	// §6.1 ENUM. Must pass WorkspaceObservation.Valid().
	WorkspaceObservation WorkspaceObservation

	// SessionLogRef is the CASS handle for the agent session log per
	// workspace-model.md §4.7, or nil when no session log is present.
	// Nullable per spec ("String | None").
	SessionLogRef *string

	// Category is the ReconciliationCategory that triggered this investigator
	// dispatch. Must be a declared ReconciliationCategory constant.
	Category ReconciliationCategory

	// PlaybookRef is the per-category playbook identifier per RC-016.
	// Non-empty required.
	PlaybookRef string

	// BudgetWallClockSeconds is the mandatory wall-clock budget in seconds
	// for this investigator run per RC-017. Must be positive (> 0).
	BudgetWallClockSeconds int
}

// Valid reports whether all structural invariants of InvestigatorInput are
// satisfied.
//
// Rules per specs/reconciliation/schemas.md §6.1 RECORD InvestigatorInput:
//   - SnapshotToken passes SnapshotToken.Valid().
//   - TargetRunID is not the zero UUID.
//   - TargetWorkflowID is not the zero UUID.
//   - TargetWorkflowVersion is non-empty.
//   - LastCheckpoint passes Checkpoint.Valid().
//   - LastTransition passes Transition.Valid().
//   - WorkspaceObservation passes WorkspaceObservation.Valid().
//   - Category is a declared ReconciliationCategory constant.
//   - PlaybookRef is non-empty.
//   - BudgetWallClockSeconds is positive (> 0).
//
// Nullable fields (TargetBeadID, BeadRecord, SessionLogRef) and observational
// slices (JSONLTail) have no invariants beyond Go's zero-value safety.
func (inp InvestigatorInput) Valid() bool {
	if !inp.SnapshotToken.Valid() {
		return false
	}
	if uuid.UUID(inp.TargetRunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(inp.TargetWorkflowID) == uuid.Nil {
		return false
	}
	if inp.TargetWorkflowVersion == "" {
		return false
	}
	if !inp.LastCheckpoint.Valid() {
		return false
	}
	if !inp.LastTransition.Valid() {
		return false
	}
	if !inp.WorkspaceObservation.Valid() {
		return false
	}
	if !inp.Category.Valid() {
		return false
	}
	if inp.PlaybookRef == "" {
		return false
	}
	if inp.BudgetWallClockSeconds <= 0 {
		return false
	}
	return true
}
