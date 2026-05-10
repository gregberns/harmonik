package core

import "github.com/google/uuid"

// SubWorkflowExitedPayload is the typed event payload for the
// sub_workflow_exited lifecycle event (event-model.md §8.1.10).
//
// Tags: mechanism
//
// # Spec source (event-model.md §8.1.10)
//
// Emitted by orchestrator-core on exiting a sub-workflow expansion, per
// execution-model.md §4.8.EM-036:
//
//	"On exiting, it MUST emit the sub-workflow-exited lifecycle event declared
//	in [event-model.md §8.1]; the exit event's terminal-outcome correlation
//	field is composed per §4.8.EM-036a."
//
// # Payload fields (event-model.md §8.1.10)
//
// All five fields are required:
//   - RunID: the parent run's stable UUIDv7 identifier.
//   - ParentNodeID: the NodeID of the sub-workflow node in the parent
//     workflow whose expansion has exited.
//   - SubWorkflowName: the sub-workflow reference name (same value as the
//     corresponding SubWorkflowEnteredPayload).
//   - SubWorkflowVersion: the pinned version (same value as the corresponding
//     SubWorkflowEnteredPayload; matches the durable expansion pin per EM-034c).
//   - TerminalOutcomeStatus: the OutcomeStatus of the last expanded node
//     executed before the sub-workflow reached a terminal_node_id, per
//     EM-036a. This is the same Outcome that escapes to the parent's
//     edge-selection cascade (§4.10.EM-041). Required: must be Valid().
//
// # Terminal outcome correlation (EM-036a)
//
// The TerminalOutcomeStatus carries the outcome_status of the Outcome produced
// by the last expanded node, matching EM-036a:
//
//	"The sub-workflow-exited event's terminal-outcome correlation field MUST
//	carry the same Outcome."
//
// # Correlation with sub_workflow_entered
//
// SubWorkflowEnteredPayload carries the same RunID, ParentNodeID,
// SubWorkflowName, and SubWorkflowVersion. Consumers MAY correlate entry
// and exit events by matching these four shared fields.
type SubWorkflowExitedPayload struct {
	// RunID is the parent run's UUIDv7 identifier (execution-model.md §6.1).
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// ParentNodeID is the NodeID of the sub-workflow node in the parent
	// workflow that has completed expansion (execution-model.md §4.8.EM-034a).
	// Required (non-empty).
	ParentNodeID NodeID `json:"parent_node_id"`

	// SubWorkflowName is the reference name used to address the sub-workflow
	// in the parent workflow definition (event-model.md §8.1.10 sub_workflow_name;
	// execution-model.md §6.1 Node.sub_workflow_ref). Non-empty string.
	SubWorkflowName SubWorkflowRef `json:"sub_workflow_name"`

	// SubWorkflowVersion is the pinned version of the sub-workflow at load
	// time (event-model.md §8.1.10 sub_workflow_version; EM-034c expansion pin).
	// Required (non-empty).
	SubWorkflowVersion WorkflowVersion `json:"sub_workflow_version"`

	// TerminalOutcomeStatus is the OutcomeStatus of the last expanded node
	// executed before the sub-workflow reached a terminal node (EM-036a).
	// The parent's edge-selection cascade (§4.10.EM-041) observes this same
	// Outcome on the outgoing edges of the sub-workflow node.
	// Required: must be Valid() (one of SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS).
	TerminalOutcomeStatus OutcomeStatus `json:"terminal_outcome_status"`
}

// Valid reports whether p is a well-formed SubWorkflowExitedPayload.
//
// Rules per event-model.md §8.1.10 and execution-model.md §4.8.EM-036 / EM-036a:
//   - RunID must not be the zero UUID.
//   - ParentNodeID must be non-empty.
//   - SubWorkflowName must be Valid() (non-empty SubWorkflowRef).
//   - SubWorkflowVersion must be non-empty.
//   - TerminalOutcomeStatus must be Valid() (declared OutcomeStatus constant).
func (p SubWorkflowExitedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.ParentNodeID == "" {
		return false
	}
	if !p.SubWorkflowName.Valid() {
		return false
	}
	if p.SubWorkflowVersion == "" {
		return false
	}
	if !p.TerminalOutcomeStatus.Valid() {
		return false
	}
	return true
}
