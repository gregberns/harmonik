package core

import "github.com/google/uuid"

// SubWorkflowEnteredPayload is the typed event payload for the
// sub_workflow_entered lifecycle event (event-model.md §8.1.9).
//
// Tags: mechanism
//
// # Spec source (event-model.md §8.1.9)
//
// Emitted by orchestrator-core on entering a sub-workflow expansion, per
// execution-model.md §4.8.EM-036:
//
//	"On entering a sub-workflow expansion, the daemon MUST emit the
//	sub-workflow-entered lifecycle event declared in [event-model.md §8.1]."
//
// The entry checkpoint MUST carry the expansion pin per EM-034c. Both
// sub-workflow lifecycle events correlate via RunID and ParentNodeID per
// EM-036.
//
// # Payload fields (event-model.md §8.1.9)
//
// All four fields are required:
//   - RunID: the parent run's stable UUIDv7 identifier.
//   - ParentNodeID: the NodeID of the sub-workflow node in the parent
//     workflow whose expansion is being entered.
//   - SubWorkflowName: the sub-workflow reference name as declared in the
//     parent workflow's Node.sub_workflow_ref field. Corresponds to the
//     spec's sub_workflow_name field; typed as SubWorkflowRef (non-empty
//     string alias) to enforce the non-empty invariant.
//   - SubWorkflowVersion: the pinned version of the sub-workflow as resolved
//     at workflow-load time. Matches the version field of the durable
//     SubWorkflowExpansionPin (EM-034c).
//
// # Correlation with sub_workflow_exited
//
// SubWorkflowExitedPayload carries the same RunID, ParentNodeID,
// SubWorkflowName, and SubWorkflowVersion, plus TerminalOutcomeStatus.
// Consumers MAY correlate entry and exit events by matching all four shared
// fields.
type SubWorkflowEnteredPayload struct {
	// RunID is the parent run's UUIDv7 identifier (execution-model.md §6.1).
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// ParentNodeID is the NodeID of the sub-workflow node in the parent
	// workflow being expanded (execution-model.md §4.8.EM-034a).
	// Required (non-empty).
	ParentNodeID NodeID `json:"parent_node_id"`

	// SubWorkflowName is the reference name used to address the sub-workflow
	// in the parent workflow definition (event-model.md §8.1.9 sub_workflow_name;
	// execution-model.md §6.1 Node.sub_workflow_ref). Non-empty string.
	SubWorkflowName SubWorkflowRef `json:"sub_workflow_name"`

	// SubWorkflowVersion is the pinned version of the sub-workflow at load
	// time (event-model.md §8.1.9 sub_workflow_version; EM-034c expansion pin).
	// Required (non-empty).
	SubWorkflowVersion WorkflowVersion `json:"sub_workflow_version"`
}

// Valid reports whether p is a well-formed SubWorkflowEnteredPayload.
//
// Rules per event-model.md §8.1.9 and execution-model.md §4.8.EM-036:
//   - RunID must not be the zero UUID.
//   - ParentNodeID must be non-empty.
//   - SubWorkflowName must be Valid() (non-empty SubWorkflowRef).
//   - SubWorkflowVersion must be non-empty.
func (p SubWorkflowEnteredPayload) Valid() bool {
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
	return true
}
