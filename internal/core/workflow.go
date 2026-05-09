package core

import "github.com/google/uuid"

// Workflow is the 11-field named, versioned directed graph that describes a
// harmonik execution workflow (execution-model.md §6.1 RECORD Workflow,
// §4.1.EM-001).
//
// A Workflow is a static definition; runtime execution state lives in Run
// (§6.1 RECORD Run). On-disk representation is a DOT document per
// architecture.md §4.10 three-artifact separation — the Go record is the
// in-memory canonical form after parsing.
//
// # Invariants enforced by Valid()
//
//   - WorkflowID is not uuid.Nil
//   - Name is non-empty
//   - Version is non-empty
//   - Nodes is non-nil (empty slice is valid for an in-construction workflow,
//     but ValidForDispatch() requires at least one node)
//   - Edges is non-nil (empty slice is valid)
//   - StartNodeID is non-empty and present in Nodes
//   - TerminalNodeIDs is non-empty (§4.1.EM-001: "non-empty terminal_node_ids list")
//   - every NodeID in TerminalNodeIDs is present in Nodes
//   - every Edge satisfies Edge.Valid() and its FromNode/ToNode are present in
//     Nodes (well-formed directed graph per §4.1.EM-001)
//   - WorkflowClass, when non-nil, equals "reconciliation" (the only MVH value;
//     §4.1.EM-001, §6.2 Harmonik-Workflow-Class trailer; validator per
//     §4.9.EM-038 MUST reject any other non-None value)
//   - SchemaVersion > 0 (N-1 readable per §4.4.EM-022)
//
// # Schema compatibility
//
// Workflow carries SchemaVersion under the N-1 readability contract of
// operator-nfr.md §4.5 (ON-018). The current version is 1.
type Workflow struct {
	// WorkflowID is the stable UUID identifier for the workflow definition
	// (execution-model.md §6.1). Must not be uuid.Nil.
	WorkflowID WorkflowID

	// Name is the human-readable name for this workflow.
	// Required; must be non-empty.
	Name string

	// Version is the semver-ish version string for this workflow definition
	// (execution-model.md §6.1 Workflow.version; see also WorkflowVersion for
	// the pinned dispatch-time form on Run).
	// Required; must be non-empty.
	Version string

	// Nodes is the list of graph vertices for this workflow (execution-model.md
	// §6.1 RECORD Node). An empty slice is valid structurally; dispatch
	// validation (§4.9.EM-038) requires at least one reachable node.
	Nodes []Node

	// Edges is the list of directed connections between nodes
	// (execution-model.md §6.1 RECORD Edge). An empty slice is valid for
	// single-node terminal workflows.
	Edges []Edge

	// StartNodeID is the designated entry node for this workflow
	// (execution-model.md §6.1 Workflow.start_node_id; validated per §4.9.EM-038).
	// Must be non-empty and present in Nodes.
	StartNodeID NodeID

	// TerminalNodeIDs is the non-empty list of node IDs that end a run when
	// reached (execution-model.md §6.1 Workflow.terminal_node_ids, §4.9 EM-038).
	// A run enters terminal state when its current node is in this list per §4.3.
	// Must be non-empty; every ID must appear in Nodes.
	TerminalNodeIDs []NodeID

	// Policies is the ordered list of resolved policy references for this workflow,
	// loaded at workflow-load time (execution-model.md §6.1 Workflow.policies,
	// control-points.md §6.4).
	Policies []PolicyRef

	// Metadata is the free-form key/value annotation map for this workflow.
	// A nil map is equivalent to an empty map; both are valid.
	Metadata map[string]string

	// WorkflowClass is an optional class tag for this workflow
	// (reconciliation/schemas.md §6.5). At MVH the only accepted value is
	// WorkflowClassReconciliation, which flags the §4.5.EM-026 checkpoint
	// exception and the §6.2 Harmonik-Workflow-Class trailer. Absence (nil)
	// means an ordinary (unclassed) workflow. Valid() rejects any non-nil value
	// other than WorkflowClassReconciliation per §4.9.EM-038.
	//
	// EM-026 exception (execution-model.md §4.5.EM-026): when WorkflowClass ==
	// WorkflowClassReconciliation, the run MUST emit exactly one checkpoint
	// commit per reconciliation-run (the verdict commit) and MUST NOT emit
	// intermediate checkpoints. This overrides the one-commit-per-durable-
	// transition rule of EM-023 for that run. Absence of the field (nil) means
	// an ordinary workflow that obeys EM-023 unchanged.
	WorkflowClass *WorkflowClass

	// SchemaVersion is the schema version of this record under the N-1
	// readability contract of operator-nfr.md §4.5 ON-018. The current version
	// is 1. Must be > 0.
	SchemaVersion int
}

// Valid reports whether w satisfies all structural invariants declared in
// execution-model.md §6.1 and §4.1.EM-001:
//
//   - WorkflowID is not uuid.Nil
//   - Name is non-empty
//   - Version is non-empty
//   - StartNodeID is non-empty and present in Nodes
//   - TerminalNodeIDs is non-empty
//   - every NodeID in TerminalNodeIDs is present in Nodes
//   - every Edge is structurally valid (Edge.Valid()) and its FromNode and
//     ToNode are present in Nodes (well-formed directed graph per EM-001)
//   - WorkflowClass, when non-nil, equals "reconciliation"
//   - SchemaVersion > 0
func (w Workflow) Valid() bool {
	if uuid.UUID(w.WorkflowID) == uuid.Nil {
		return false
	}
	if w.Name == "" {
		return false
	}
	if w.Version == "" {
		return false
	}
	if w.StartNodeID == "" {
		return false
	}
	// StartNodeID must be present in Nodes.
	if !b3f72nodeIDInList(w.StartNodeID, w.Nodes) {
		return false
	}
	// TerminalNodeIDs: non-empty (EM-001).
	if len(w.TerminalNodeIDs) == 0 {
		return false
	}
	// Every terminal node ID must be present in Nodes.
	for _, tid := range w.TerminalNodeIDs {
		if !b3f72nodeIDInList(tid, w.Nodes) {
			return false
		}
	}
	// Every edge must be structurally valid and its FromNode/ToNode must be
	// present in Nodes (well-formed directed graph per EM-001).
	for _, e := range w.Edges {
		if !e.Valid() {
			return false
		}
		if !b3f72nodeIDInList(e.FromNode, w.Nodes) {
			return false
		}
		if !b3f72nodeIDInList(e.ToNode, w.Nodes) {
			return false
		}
	}
	// WorkflowClass: when set, must be a valid WorkflowClass (MVH only value;
	// EM-038, reconciliation/schemas.md §6.5).
	if w.WorkflowClass != nil && !w.WorkflowClass.Valid() {
		return false
	}
	if w.SchemaVersion <= 0 {
		return false
	}
	return true
}

// b3f72nodeIDInList reports whether id appears as the NodeID of any node in nodes.
func b3f72nodeIDInList(id NodeID, nodes []Node) bool {
	for i := range nodes {
		if nodes[i].NodeID == id {
			return true
		}
	}
	return false
}
