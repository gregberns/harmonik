package core

import (
	"testing"

	"github.com/google/uuid"
)

// b3f72WorkflowValid returns a fully-populated Workflow with all required
// fields set to valid values. Tests mutate individual fields to probe Valid().
// Bead prefix: b3f72 (per implementer-protocol.md helper-prefix discipline).
func b3f72WorkflowValid(t *testing.T) Workflow {
	t.Helper()
	wfID := WorkflowID(uuid.MustParse("018f1e2a-0000-7000-8000-000000000001"))
	startNode := Node{
		NodeID:           NodeID("start"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	termNode := Node{
		NodeID:           NodeID("done"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	return Workflow{
		WorkflowID:      wfID,
		Name:            "test-workflow",
		Version:         "1.0.0",
		Nodes:           []Node{startNode, termNode},
		Edges:           []Edge{},
		StartNodeID:     NodeID("start"),
		TerminalNodeIDs: []NodeID{NodeID("done")},
		Policies:        []PolicyRef{},
		Metadata:        map[string]string{},
		WorkflowClass:   nil,
		SchemaVersion:   1,
	}
}

// b3f72WorkflowReconciliation returns a valid reconciliation-class Workflow.
func b3f72WorkflowReconciliation(t *testing.T) Workflow {
	t.Helper()
	wf := b3f72WorkflowValid(t)
	cls := WorkflowClassReconciliation
	wf.WorkflowClass = &cls
	return wf
}

func TestWorkflowValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	if !wf.Valid() {
		t.Error("Valid() = false for fully-populated Workflow, want true")
	}
}

func TestWorkflowValid_NilWorkflowID(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.WorkflowID = WorkflowID(uuid.Nil)
	if wf.Valid() {
		t.Error("Valid() = true with nil WorkflowID (uuid.Nil), want false")
	}
}

func TestWorkflowValid_EmptyName(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Name = ""
	if wf.Valid() {
		t.Error("Valid() = true with empty Name, want false")
	}
}

func TestWorkflowValid_EmptyVersion(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Version = ""
	if wf.Valid() {
		t.Error("Valid() = true with empty Version, want false")
	}
}

func TestWorkflowValid_EmptyStartNodeID(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.StartNodeID = ""
	if wf.Valid() {
		t.Error("Valid() = true with empty StartNodeID, want false")
	}
}

func TestWorkflowValid_StartNodeIDNotInNodes(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.StartNodeID = NodeID("nonexistent-node")
	if wf.Valid() {
		t.Error("Valid() = true with StartNodeID not in Nodes, want false")
	}
}

func TestWorkflowValid_EmptyTerminalNodeIDs(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.TerminalNodeIDs = []NodeID{}
	if wf.Valid() {
		t.Error("Valid() = true with empty TerminalNodeIDs, want false (EM-001: non-empty required)")
	}
}

func TestWorkflowValid_NilTerminalNodeIDs(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.TerminalNodeIDs = nil
	if wf.Valid() {
		t.Error("Valid() = true with nil TerminalNodeIDs, want false (EM-001: non-empty required)")
	}
}

func TestWorkflowValid_TerminalNodeIDNotInNodes(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.TerminalNodeIDs = []NodeID{NodeID("ghost-node")}
	if wf.Valid() {
		t.Error("Valid() = true with terminal node ID absent from Nodes, want false")
	}
}

func TestWorkflowValid_MultipleTerminalNodes(t *testing.T) {
	t.Parallel()

	// Add a second terminal node to Nodes and TerminalNodeIDs.
	extra := Node{
		NodeID:           NodeID("done-alt"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	wf := b3f72WorkflowValid(t)
	wf.Nodes = append(wf.Nodes, extra)
	wf.TerminalNodeIDs = append(wf.TerminalNodeIDs, NodeID("done-alt"))
	if !wf.Valid() {
		t.Error("Valid() = false for Workflow with multiple valid terminal nodes, want true")
	}
}

func TestWorkflowValid_MultipleTerminalNodesOneAbsent(t *testing.T) {
	t.Parallel()

	// One terminal node is valid, the other is not in Nodes.
	wf := b3f72WorkflowValid(t)
	wf.TerminalNodeIDs = append(wf.TerminalNodeIDs, NodeID("ghost"))
	if wf.Valid() {
		t.Error("Valid() = true when one terminal node ID is absent from Nodes, want false")
	}
}

func TestWorkflowValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.SchemaVersion = 0
	if wf.Valid() {
		t.Error("Valid() = true with SchemaVersion=0, want false")
	}
}

func TestWorkflowValid_NegativeSchemaVersion(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.SchemaVersion = -1
	if wf.Valid() {
		t.Error("Valid() = true with negative SchemaVersion, want false")
	}
}

func TestWorkflowValid_SchemaVersionOne(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.SchemaVersion = 1
	if !wf.Valid() {
		t.Error("Valid() = false for SchemaVersion=1, want true")
	}
}

func TestWorkflowValid_NilWorkflowClass(t *testing.T) {
	t.Parallel()

	// Absence of WorkflowClass = ordinary workflow; always valid per §4.9.EM-038.
	wf := b3f72WorkflowValid(t)
	wf.WorkflowClass = nil
	if !wf.Valid() {
		t.Error("Valid() = false for nil WorkflowClass (ordinary workflow), want true")
	}
}

func TestWorkflowValid_ReconciliationClass(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowReconciliation(t)
	if !wf.Valid() {
		t.Error("Valid() = false for WorkflowClass=reconciliation, want true")
	}
}

// TestWorkflowValid_InvalidWorkflowClass verifies that §4.9.EM-038 rejects any
// non-nil WorkflowClass value other than "reconciliation" at MVH.
func TestWorkflowValid_InvalidWorkflowClass(t *testing.T) {
	t.Parallel()

	cases := []WorkflowClass{
		"improvement-loop",
		"unknown",
		"",
		"RECONCILIATION",
	}
	for _, cls := range cases {
		cls := cls
		t.Run(string(cls), func(t *testing.T) {
			t.Parallel()
			wf := b3f72WorkflowValid(t)
			wf.WorkflowClass = &cls
			if wf.Valid() {
				t.Errorf("Valid() = true for WorkflowClass=%q, want false (EM-038 rejects non-reconciliation values)", cls)
			}
		})
	}
}

func TestWorkflowValid_NilPolicies(t *testing.T) {
	t.Parallel()

	// nil Policies slice is valid (no resolved policies).
	wf := b3f72WorkflowValid(t)
	wf.Policies = nil
	if !wf.Valid() {
		t.Error("Valid() = false with nil Policies, want true")
	}
}

func TestWorkflowValid_PopulatedPolicies(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Policies = []PolicyRef{"policies/default", "policies/budget"}
	if !wf.Valid() {
		t.Error("Valid() = false with populated Policies, want true")
	}
}

func TestWorkflowValid_NilMetadata(t *testing.T) {
	t.Parallel()

	// nil Metadata is equivalent to empty map — valid.
	wf := b3f72WorkflowValid(t)
	wf.Metadata = nil
	if !wf.Valid() {
		t.Error("Valid() = false with nil Metadata, want true")
	}
}

func TestWorkflowValid_PopulatedMetadata(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Metadata = map[string]string{"owner": "team-a", "purpose": "demo"}
	if !wf.Valid() {
		t.Error("Valid() = false with populated Metadata, want true")
	}
}

func TestWorkflowValid_WithEdges(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Edges = []Edge{
		{
			FromNode:    NodeID("start"),
			ToNode:      NodeID("done"),
			OrderingKey: "a",
			Weight:      0,
		},
	}
	if !wf.Valid() {
		t.Error("Valid() = false with a valid Edge in Edges, want true")
	}
}

// TestWorkflowValid_EdgeFromNodeAbsent verifies that an edge whose FromNode is
// not in Nodes fails Valid() (well-formed directed graph, §4.1.EM-001).
// Helper prefix: hk-b3f1impl (per implementer-protocol.md).
func TestWorkflowValid_EdgeFromNodeAbsent(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Edges = []Edge{
		{
			FromNode:    NodeID("ghost"),
			ToNode:      NodeID("done"),
			OrderingKey: "a",
		},
	}
	if wf.Valid() {
		t.Error("Valid() = true with edge FromNode absent from Nodes, want false (EM-001 well-formed graph)")
	}
}

// TestWorkflowValid_EdgeToNodeAbsent verifies that an edge whose ToNode is not
// in Nodes fails Valid() (well-formed directed graph, §4.1.EM-001).
func TestWorkflowValid_EdgeToNodeAbsent(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Edges = []Edge{
		{
			FromNode:    NodeID("start"),
			ToNode:      NodeID("ghost"),
			OrderingKey: "a",
		},
	}
	if wf.Valid() {
		t.Error("Valid() = true with edge ToNode absent from Nodes, want false (EM-001 well-formed graph)")
	}
}

// TestWorkflowValid_EdgeInvalidStructure verifies that a structurally invalid
// edge (empty OrderingKey) fails Valid() via Edge.Valid() delegation.
func TestWorkflowValid_EdgeInvalidStructure(t *testing.T) {
	t.Parallel()

	wf := b3f72WorkflowValid(t)
	wf.Edges = []Edge{
		{
			FromNode:    NodeID("start"),
			ToNode:      NodeID("done"),
			OrderingKey: "", // invalid: OrderingKey must be non-empty per Edge.Valid()
		},
	}
	if wf.Valid() {
		t.Error("Valid() = true with structurally invalid Edge (empty OrderingKey), want false")
	}
}

func TestWorkflowValid_StartNodeEqualToTerminal(t *testing.T) {
	t.Parallel()

	// A single-node workflow where start == terminal is structurally valid.
	node := Node{
		NodeID:           NodeID("only"),
		Type:             NodeTypeNonAgentic,
		IdempotencyClass: IdempotencyClassIdempotent,
		Axes:             BaselineAxisTags,
		ModeTag:          "mechanism",
	}
	wfID := WorkflowID(uuid.MustParse("018f1e2a-0000-7000-8000-000000000002"))
	wf := Workflow{
		WorkflowID:      wfID,
		Name:            "trivial",
		Version:         "1.0.0",
		Nodes:           []Node{node},
		Edges:           []Edge{},
		StartNodeID:     NodeID("only"),
		TerminalNodeIDs: []NodeID{NodeID("only")},
		SchemaVersion:   1,
	}
	if !wf.Valid() {
		t.Error("Valid() = false for single-node workflow where start==terminal, want true")
	}
}
