package core

// SubWorkflowExpansion is the typed in-memory record produced by the daemon
// when it expands a sub-workflow node in place within the parent run's execution
// graph, per execution-model.md §4.8.EM-034.
//
// # Spec source
//
// execution-model.md §4.8.EM-034:
//
//	"A node of type sub-workflow MUST expand in place at runtime: the
//	sub-workflow's nodes and edges become part of the parent run's execution
//	graph. The sub-workflow MUST NOT spawn a child run; the parent run_id is
//	the sole run identifier for the entire nested execution."
//
// Expansion is keyed on the sub-workflow's version as resolved at
// workflow-load time; a sub-workflow registry update between load and runtime
// expansion MUST NOT change the expanded shape. The load-time pin survives
// until run terminal state. Durable backing for the pin is normative per
// §4.8.EM-034c.
//
// # Node-ID namespacing (EM-034a)
//
// Every node ID in ExpandedNodes and every node reference in ExpandedEdges
// MUST be rewritten to the form "<parent_node_id>/<sub_node_id>" before the
// expansion result is used by the runtime. StartNodeID and TerminalNodeIDs
// carry the already-namespaced forms. The ParentNodeID field identifies the
// sub-workflow node in the parent workflow whose position in the graph the
// expanded nodes replace.
//
// # No child run (EM-034)
//
// The daemon MUST execute the ExpandedNodes within the parent run identified
// by the single parent run_id. No child RunID is allocated; the parent
// run_id is the sole run identifier for the entire nested execution.
//
// # Pin durability (EM-034c)
//
// Pin carries the SubWorkflowExpansionPin that MUST appear on the entry
// checkpoint's Transition record (under EvidenceKeySubWorkflowPin). On
// daemon restart the Pin is reconstructed from the most recent
// sub_workflow_entered transition record on the run's task branch; the
// sub-workflow registry is NOT re-consulted.
type SubWorkflowExpansion struct {
	// ParentNodeID is the NodeID of the sub-workflow node in the parent
	// workflow that is being expanded. Required (non-empty). Must match the
	// node whose Type == NodeTypeSubWorkflow in the parent graph.
	ParentNodeID NodeID

	// ExpandedNodes is the set of nodes contributed by the sub-workflow
	// definition. Every NodeID in this slice MUST already be rewritten to the
	// namespaced form "<ParentNodeID>/<sub_node_id>" per §4.8.EM-034a.
	// Required; must be non-nil and non-empty.
	ExpandedNodes []Node

	// ExpandedEdges is the set of directed edges contributed by the
	// sub-workflow definition. Every FromNode and ToNode reference MUST already
	// be in the namespaced form per §4.8.EM-034a.
	// Required; must be non-nil (empty slice is valid for a single-node
	// sub-workflow that has no internal edges).
	ExpandedEdges []Edge

	// StartNodeID is the namespaced NodeID of the sub-workflow's designated
	// start node within the expanded graph. Required (non-empty). Must be
	// present in ExpandedNodes.
	StartNodeID NodeID

	// TerminalNodeIDs is the non-empty list of namespaced NodeIDs that
	// represent the sub-workflow's terminal nodes within the expanded graph.
	// The last terminal node reached during execution is the source of the
	// sub-workflow's terminal Outcome per §4.8.EM-036a.
	// Required; must be non-empty and every ID must be present in ExpandedNodes.
	TerminalNodeIDs []NodeID

	// Pin is the load-time sub-workflow expansion pin carried on the entry
	// checkpoint's Transition record per §4.8.EM-034c. Must satisfy
	// Pin.Valid() == true.
	Pin SubWorkflowExpansionPin
}

// Valid reports whether e is a well-formed SubWorkflowExpansion.
//
// Rules per execution-model.md §4.8.EM-034 and §4.8.EM-034c:
//   - ParentNodeID must be non-empty.
//   - ExpandedNodes must be non-nil and non-empty.
//   - ExpandedEdges must be non-nil (empty is allowed for single-node sub-workflows).
//   - StartNodeID must be non-empty and present in ExpandedNodes.
//   - TerminalNodeIDs must be non-empty and every ID must be present in ExpandedNodes.
//   - Pin must satisfy Pin.Valid() == true.
func (e SubWorkflowExpansion) Valid() bool {
	if e.ParentNodeID == "" {
		return false
	}
	if len(e.ExpandedNodes) == 0 {
		return false
	}
	if e.ExpandedEdges == nil {
		return false
	}
	if e.StartNodeID == "" {
		return false
	}
	if !b3f43nodeIDInExpandedNodes(e.StartNodeID, e.ExpandedNodes) {
		return false
	}
	if len(e.TerminalNodeIDs) == 0 {
		return false
	}
	for _, tid := range e.TerminalNodeIDs {
		if !b3f43nodeIDInExpandedNodes(tid, e.ExpandedNodes) {
			return false
		}
	}
	if !e.Pin.Valid() {
		return false
	}
	return true
}

// b3f43nodeIDInExpandedNodes reports whether id appears as the NodeID of any
// node in nodes.
func b3f43nodeIDInExpandedNodes(id NodeID, nodes []Node) bool {
	for i := range nodes {
		if nodes[i].NodeID == id {
			return true
		}
	}
	return false
}
