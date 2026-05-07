package core

import (
	"time"

	"github.com/google/uuid"
)

// Run records the runtime state of a single workflow invocation against a single
// input (execution-model.md §4.3 EM-012 and §6.1).
//
// A Run executes EXACTLY ONE workflow against EXACTLY ONE input; multi-workflow or
// multi-input runs are not permitted per EM-012. Transition records for the run are
// discoverable via the task-branch commit range whose commits carry the run's
// Harmonik-Run-ID trailer; no separate transitions field is required here.
type Run struct {
	// RunID is the stable UUIDv7 run identifier (execution-model.md §6.1; unique across project).
	RunID RunID

	// WorkflowID is the resolved workflow identifier pinned at dispatch time
	// (execution-model.md §6.1 Run.workflow_id; UUID-backed named type).
	WorkflowID WorkflowID

	// WorkflowVersion is the pinned version of the workflow at dispatch time
	// (execution-model.md §6.1 Run.workflow_version; string-backed, semver-ish).
	WorkflowVersion WorkflowVersion

	// Input is the workspace reference for this run (workspace-model.md §4.1;
	// execution-model.md §6.1 Run.input).
	Input WorkspaceRef

	// BeadID is present when the run is tied to a bead per EM-014 and
	// beads-integration.md §4.3 BI-008; absent otherwise.
	BeadID *BeadID

	// State is the current state of the run (StateID of the current run-state).
	// The current state must always be set per EM-012.
	State StateID

	// Context is the shared key-value map for this run, updated per §4.10.EM-041a.
	// The map must be non-nil; an empty (zero-length) map is valid.
	Context map[string]any

	// StartTime is the RFC 3339 wall clock at the moment the run was started.
	StartTime time.Time

	// EndTime is set on terminal transition (RFC 3339 wall clock); absent for
	// in-progress runs.
	EndTime *time.Time
}

// Valid reports whether all required fields carry non-zero values.
// A Run is considered valid iff:
//   - RunID is not the zero UUID
//   - WorkflowID is non-empty
//   - WorkflowVersion is non-empty
//   - Input is non-empty
//   - State is not the zero UUID (current state must be set per EM-012)
//   - BeadID, when non-nil, dereferences to a non-empty value
//   - Context is non-nil (an empty map is valid; nil is not)
//   - StartTime is not the zero time
//   - EndTime, when non-nil, is not the zero time
func (r Run) Valid() bool {
	if uuid.UUID(r.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(r.WorkflowID) == uuid.Nil {
		return false
	}
	if r.WorkflowVersion == "" {
		return false
	}
	if r.Input == "" {
		return false
	}
	if uuid.UUID(r.State) == uuid.Nil {
		return false
	}
	if r.BeadID != nil && *r.BeadID == "" {
		return false
	}
	if r.Context == nil {
		return false
	}
	if r.StartTime.IsZero() {
		return false
	}
	if r.EndTime != nil && r.EndTime.IsZero() {
		return false
	}
	return true
}
