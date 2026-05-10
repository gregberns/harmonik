package core

import "github.com/google/uuid"

// RunStartedPayload is the typed event payload for the run_started event
// (event-model.md §8.1.1; specs/event-model.md §6.3).
//
// The daemon MUST emit run_started AFTER create_run has allocated the run_id
// AND AFTER the Beads atomic-claim write has persisted (beads-integration.md
// §4.3 BI-009) AND BEFORE any node in the run is dispatched, per
// execution-model.md §4.3.EM-015a.
//
// # Payload fields (event-model.md §8.1.1)
//
// RunID, WorkflowID, WorkflowVersion, and WorkspacePath are always required.
// BeadID is required only for bead-tied runs (execution-model.md §4.3.EM-014);
// it is nil for standalone-input runs. InputRef is the workspace reference
// string carried on the Run at dispatch time.
//
// # BeadID optionality
//
// BeadID is *BeadID (pointer) to distinguish "run is bead-tied" (non-nil) from
// "run is not bead-tied" (nil). Set-but-empty is a validation error per Valid().
//
// # WorkspacePath and InputRef
//
// Both fields are declared as plain strings in the event-model.md §6.3 YAML
// schema (workspace_path: <String>, input_ref: <String>). No typed alias
// exists yet; they are kept as string per the typed-alias-deferral pattern.
type RunStartedPayload struct {
	// RunID is the stable UUIDv7 run identifier (execution-model.md §6.1).
	// Required (must not be zero).
	RunID RunID `json:"run_id"`

	// WorkflowID is the resolved workflow identifier pinned at dispatch time
	// (execution-model.md §6.1 Run.workflow_id).
	// Required (must not be zero).
	WorkflowID WorkflowID `json:"workflow_id"`

	// WorkflowVersion is the pinned version of the workflow at dispatch time
	// (execution-model.md §6.1 Run.workflow_version).
	// Required (non-empty).
	WorkflowVersion WorkflowVersion `json:"workflow_version"`

	// BeadID carries the bead identifier for bead-tied runs
	// (beads-integration.md §4 BI-017; execution-model.md §4.3.EM-014).
	// Nil for standalone-input runs. Non-nil and non-empty for bead-tied runs.
	// Set-but-empty is rejected by Valid().
	BeadID *BeadID `json:"bead_id,omitempty"`

	// WorkspacePath is the filesystem path of the run's leased workspace
	// (workspace-model.md §4.1; event-model.md §8.1.1 workspace_path).
	// Required (non-empty). Plain string per event-model.md §6.3 YAML schema.
	WorkspacePath string `json:"workspace_path"`

	// InputRef is the workspace reference string carried on the Run at dispatch
	// (execution-model.md §6.1 Run.input; event-model.md §8.1.1 input_ref).
	// Required (non-empty). Plain string per event-model.md §6.3 YAML schema.
	InputRef string `json:"input_ref"`
}

// Valid reports whether p is a well-formed RunStartedPayload.
//
// Rules per event-model.md §8.1.1 and execution-model.md §4.3.EM-015a:
//   - RunID must not be the zero UUID.
//   - WorkflowID must not be the zero UUID.
//   - WorkflowVersion must be non-empty.
//   - BeadID, when non-nil, must dereference to a non-empty value.
//   - WorkspacePath must be non-empty.
//   - InputRef must be non-empty.
func (p RunStartedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.WorkflowID) == uuid.Nil {
		return false
	}
	if p.WorkflowVersion == "" {
		return false
	}
	if p.BeadID != nil && *p.BeadID == "" {
		return false
	}
	if p.WorkspacePath == "" {
		return false
	}
	if p.InputRef == "" {
		return false
	}
	return true
}
