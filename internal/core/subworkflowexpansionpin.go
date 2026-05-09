package core

import "github.com/google/uuid"

// SubWorkflowExpansionPin is the structured pin stored in a Transition
// record's Evidence map under EvidenceKeySubWorkflowPin on the sub-workflow
// entry checkpoint.
//
// # Spec source
//
// execution-model.md §4.8.EM-034c:
//
//	"the entry checkpoint (the checkpoint commit whose state transitions the run
//	from the sub-workflow node to its expanded start_node_id) MUST carry the
//	resolved sub-workflow pin in the Transition record's evidence map under the
//	reserved key evidence.sub_workflow_pin with the shape
//	{ sub_workflow_ref: String, sub_workflow_version: String,
//	  resolved_workflow_id: UUID }."
//
// # Durability protocol
//
// The pin MUST appear on the Transition record of the sub_workflow_entered
// entry checkpoint commit — the specific checkpoint commit whose state
// transitions the run into the sub-workflow node's expanded start_node_id.
// This is NOT a separate commit; it is the entry checkpoint's own Transition
// record (EM-034c). The pin therefore inherits the atomicity boundary of
// §4.4.EM-016: the Transition record file and the checkpoint commit are a
// single git write-tree / commit-tree / update-ref unit. A crash before the
// commit completes leaves no pin, which is correct — on restart the daemon
// finds no sub_workflow_entered transition record and re-expands from the
// registry (crash before first expansion is idempotent). A crash after the
// commit leaves a readable pin — on restart the daemon MUST reconstruct the
// pinned expansion by reading this key from the most recent
// sub_workflow_entered transition record on the run's task branch, NOT by
// re-consulting the registry (EM-034c).
//
// # Nested expansions
//
// For nested sub-workflows (sub-workflow containing sub-workflow) each entry
// checkpoint carries its own SubWorkflowExpansionPin. The outer run's
// expansion at restart is reconstructed by walking the checkpoint trail in
// commit order and applying each pin at its entry boundary (EM-034c).
//
// # JSON serialization
//
// Field names follow the spec's snake_case keys:
//
//	sub_workflow_ref, sub_workflow_version, resolved_workflow_id
type SubWorkflowExpansionPin struct {
	// SubWorkflowRef is the name used to reference the sub-workflow in the
	// parent workflow definition. Matches Node.sub_workflow_ref at the
	// sub-workflow node.
	SubWorkflowRef SubWorkflowRef `json:"sub_workflow_ref"`

	// SubWorkflowVersion is the version string of the sub-workflow as resolved
	// at workflow-load time. A registry update after load MUST NOT change this
	// value; the pin makes that invariant machine-checkable (EM-034c).
	SubWorkflowVersion WorkflowVersion `json:"sub_workflow_version"`

	// ResolvedWorkflowID is the stable UUID of the resolved sub-workflow
	// definition at load time. Used on restart to look up the correct
	// definition even if the ref name is aliased to multiple versions.
	ResolvedWorkflowID WorkflowID `json:"resolved_workflow_id"`
}

// Valid reports whether p carries non-empty, non-nil-UUID values for all
// required fields.
//
// Rules:
//   - SubWorkflowRef must be non-empty (SubWorkflowRef.Valid() == true).
//   - SubWorkflowVersion must be non-empty.
//   - ResolvedWorkflowID must not be uuid.Nil.
func (p SubWorkflowExpansionPin) Valid() bool {
	if !p.SubWorkflowRef.Valid() {
		return false
	}
	if p.SubWorkflowVersion == "" {
		return false
	}
	if uuid.UUID(p.ResolvedWorkflowID) == uuid.Nil {
		return false
	}
	return true
}
