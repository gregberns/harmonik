package handler

// runtime.go — nested-run hook for sub-workflow nodes in the handler runtime.
//
// When the daemon's dispatch loop encounters a node of type NodeTypeSubWorkflow,
// it MUST NOT call Handler.Launch — sub-workflow nodes carry no handler_ref per
// the EM-007 amendment. Instead the daemon calls SubWorkflowRunner.Run, which
// executes the expanded sub-workflow graph within the parent run context (no new
// subprocess, no new RunID per EM-034).
//
// SubWorkflowRunner is the integration point between the handler runtime layer
// and the sub-workflow dispatch logic in internal/workflow/sub_workflow.go.
// The daemon composition root wires a concrete SubWorkflowRunner at startup.
//
// Spec refs:
//
//	specs/workflow-graph.md §4 WG-006     — sub-workflow node: sub_workflow_ref, workflow_version.
//	specs/workflow-graph.md §4 WG-006     — no handler_ref on sub-workflow nodes (EM-007 amendment).
//	specs/execution-model.md §4.8.EM-034  — expansion in place; parent run_id is sole identifier.
//	specs/execution-model.md §4.8.EM-034a — node-ID namespacing: <parentNodeID>/<subNodeID>.
//	specs/execution-model.md §4.8.EM-036  — sub_workflow_entered / sub_workflow_exited events.
//	specs/execution-model.md §4.8.EM-036a — terminal outcome escapes to parent cascade.
//	specs/execution-model.md §7.5 EM-007 amendment — daemon dispatches sub-workflow directly.
//
// Bead ref: hk-n51yp (T-IMPL-011).
// Tags: mechanism

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
)

// SubWorkflowRunSpec carries the per-node dispatch configuration for a single
// sub-workflow node execution. The daemon's dispatch loop populates this when
// the current node's Type is core.NodeTypeSubWorkflow.
type SubWorkflowRunSpec struct {
	// Run is the parent run in whose context the sub-workflow executes.
	// Required (non-nil). The sub-workflow MUST NOT allocate a new RunID
	// per EM-034; all expanded-node checkpoints use Run.RunID as the sole
	// run identifier.
	Run *core.Run

	// ParentNodeID is the NodeID of the sub-workflow node in the parent graph.
	// Required (non-empty). All expanded node IDs are namespaced to the form
	// <ParentNodeID>/<subNodeID> per EM-034a.
	ParentNodeID core.NodeID

	// SubWorkflowRef is the reference name used to address the sub-workflow in
	// the parent workflow definition (sub_workflow_ref attribute per WG-006).
	// Required (must satisfy SubWorkflowRef.Valid()).
	SubWorkflowRef core.SubWorkflowRef

	// SubWorkflowVersion is the pinned version of the sub-workflow as resolved
	// at workflow-load time (workflow_version attribute per WG-006; expansion
	// pin per EM-034c). A registry update after load MUST NOT change this value.
	// Required (non-empty).
	SubWorkflowVersion core.WorkflowVersion
}

// Valid reports whether s is a well-formed SubWorkflowRunSpec.
//
// Rules:
//   - Run must not be nil.
//   - ParentNodeID must be non-empty.
//   - SubWorkflowRef must be valid (non-empty).
//   - SubWorkflowVersion must be non-empty.
func (s SubWorkflowRunSpec) Valid() bool {
	if s.Run == nil {
		return false
	}
	if s.ParentNodeID == "" {
		return false
	}
	if !s.SubWorkflowRef.Valid() {
		return false
	}
	if s.SubWorkflowVersion == "" {
		return false
	}
	return true
}

// SubWorkflowRunner is the interface the daemon composition root wires to
// handle sub-workflow node dispatch. When the dispatch loop encounters a node
// of type core.NodeTypeSubWorkflow, it calls Run instead of Handler.Launch.
//
// The implementation is responsible for:
//  1. Loading the target sub-workflow graph from the registry.
//  2. Checking acyclicity (ValidateSubWorkflowAcyclicity per WG-029/EM-034b).
//  3. Building the SubWorkflowExpansion (ExpandSubWorkflowGraph per EM-034a).
//  4. Emitting sub_workflow_entered and sub_workflow_exited events (EM-036).
//  5. Running the cascade for the expanded nodes (DispatchSubWorkflow).
//  6. Returning the terminal Outcome for the parent cascade (EM-036a).
//
// Structural failures within the sub-workflow (cascade failures, missing edges)
// are surfaced as Outcomes with Status=FAIL and an appropriate failure_class
// so the parent cascade can route accordingly. Errors returned from Run indicate
// unrecoverable infrastructure failures (event emission, graph load) that the
// daemon should map to run_failed.
type SubWorkflowRunner interface {
	// Run executes the sub-workflow identified by spec and returns the terminal
	// Outcome for the parent cascade to observe (EM-036a).
	Run(ctx context.Context, spec SubWorkflowRunSpec) (core.Outcome, error)
}
