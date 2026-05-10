package core

import (
	"testing"

	"github.com/google/uuid"
)

// subwfExpandFixturePin returns a valid SubWorkflowExpansionPin for use in
// SubWorkflowExpansion fixtures (EM-034c).
func subwfExpandFixturePin() SubWorkflowExpansionPin {
	return SubWorkflowExpansionPin{
		SubWorkflowRef:     SubWorkflowRef("workflows/reconciliation-v1"),
		SubWorkflowVersion: WorkflowVersion("1.0.0"),
		ResolvedWorkflowID: WorkflowID(uuid.MustParse("01960000-0000-7000-8000-000000000043")),
	}
}

// subwfExpandFixtureNode returns a valid Node of the given type, with a
// namespaced NodeID of the form "<parentNodeID>/<subNodeID>" per EM-034a.
func subwfExpandFixtureNode(t *testing.T, nodeID NodeID) Node {
	t.Helper()
	n := b3f73NodeNonAgentic(t)
	n.NodeID = nodeID
	return n
}

// subwfExpandFixture returns a fully-populated SubWorkflowExpansion with all
// required fields set to valid non-zero values (EM-034).
//
// The expansion represents a sub-workflow node "dispatch" in the parent
// workflow, expanded into two namespaced nodes:
//
//	"dispatch/start-node" and "dispatch/end-node"
func subwfExpandFixture(t *testing.T) SubWorkflowExpansion {
	t.Helper()

	startID := NodeID("dispatch/start-node")
	endID := NodeID("dispatch/end-node")

	return SubWorkflowExpansion{
		ParentNodeID: NodeID("dispatch"),
		ExpandedNodes: []Node{
			subwfExpandFixtureNode(t, startID),
			subwfExpandFixtureNode(t, endID),
		},
		ExpandedEdges: []Edge{
			{FromNode: startID, ToNode: endID, OrderingKey: "0"},
		},
		StartNodeID:     startID,
		TerminalNodeIDs: []NodeID{endID},
		Pin:             subwfExpandFixturePin(),
	}
}

// TestSubWorkflowExpansionValid_AllFields verifies that a fully-populated
// SubWorkflowExpansion passes Valid() per EM-034.
func TestSubWorkflowExpansionValid_AllFields(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	if !e.Valid() {
		t.Error("Valid() = false for fully-populated SubWorkflowExpansion, want true")
	}
}

// TestSubWorkflowExpansionValid_EmptyParentNodeID verifies that Valid() rejects
// an expansion with no ParentNodeID (EM-034).
func TestSubWorkflowExpansionValid_EmptyParentNodeID(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.ParentNodeID = ""
	if e.Valid() {
		t.Error("Valid() = true for empty ParentNodeID, want false")
	}
}

// TestSubWorkflowExpansionValid_NilExpandedNodes verifies that Valid() rejects
// an expansion with a nil ExpandedNodes slice.
func TestSubWorkflowExpansionValid_NilExpandedNodes(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.ExpandedNodes = nil
	if e.Valid() {
		t.Error("Valid() = true for nil ExpandedNodes, want false")
	}
}

// TestSubWorkflowExpansionValid_EmptyExpandedNodes verifies that Valid()
// rejects an expansion with an empty ExpandedNodes slice (sub-workflow must
// contribute at least one node per EM-034).
func TestSubWorkflowExpansionValid_EmptyExpandedNodes(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.ExpandedNodes = []Node{}
	if e.Valid() {
		t.Error("Valid() = true for empty ExpandedNodes, want false")
	}
}

// TestSubWorkflowExpansionValid_NilExpandedEdges verifies that Valid() rejects
// an expansion with a nil ExpandedEdges slice (empty slice is acceptable;
// nil is not, as it signals an uninitialized expansion).
func TestSubWorkflowExpansionValid_NilExpandedEdges(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.ExpandedEdges = nil
	if e.Valid() {
		t.Error("Valid() = true for nil ExpandedEdges, want false")
	}
}

// TestSubWorkflowExpansionValid_EmptyExpandedEdges verifies that Valid() accepts
// an expansion with an empty (non-nil) ExpandedEdges slice. A single-node
// sub-workflow has no internal edges.
func TestSubWorkflowExpansionValid_EmptyExpandedEdges(t *testing.T) {
	t.Parallel()

	startID := NodeID("solo/step")
	e := SubWorkflowExpansion{
		ParentNodeID:    NodeID("solo"),
		ExpandedNodes:   []Node{subwfExpandFixtureNode(t, startID)},
		ExpandedEdges:   []Edge{},
		StartNodeID:     startID,
		TerminalNodeIDs: []NodeID{startID},
		Pin:             subwfExpandFixturePin(),
	}
	if !e.Valid() {
		t.Error("Valid() = false for single-node expansion with empty ExpandedEdges, want true")
	}
}

// TestSubWorkflowExpansionValid_EmptyStartNodeID verifies that Valid() rejects
// an expansion with no StartNodeID.
func TestSubWorkflowExpansionValid_EmptyStartNodeID(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.StartNodeID = ""
	if e.Valid() {
		t.Error("Valid() = true for empty StartNodeID, want false")
	}
}

// TestSubWorkflowExpansionValid_StartNodeIDNotInExpandedNodes verifies that
// Valid() rejects an expansion whose StartNodeID is not present in ExpandedNodes.
func TestSubWorkflowExpansionValid_StartNodeIDNotInExpandedNodes(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.StartNodeID = NodeID("dispatch/non-existent")
	if e.Valid() {
		t.Error("Valid() = true for StartNodeID not in ExpandedNodes, want false")
	}
}

// TestSubWorkflowExpansionValid_EmptyTerminalNodeIDs verifies that Valid()
// rejects an expansion with no TerminalNodeIDs.
func TestSubWorkflowExpansionValid_EmptyTerminalNodeIDs(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.TerminalNodeIDs = nil
	if e.Valid() {
		t.Error("Valid() = true for nil TerminalNodeIDs, want false")
	}
}

// TestSubWorkflowExpansionValid_TerminalNodeIDNotInExpandedNodes verifies that
// Valid() rejects an expansion whose TerminalNodeIDs contains a node not in
// ExpandedNodes.
func TestSubWorkflowExpansionValid_TerminalNodeIDNotInExpandedNodes(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.TerminalNodeIDs = []NodeID{NodeID("dispatch/ghost")}
	if e.Valid() {
		t.Error("Valid() = true for TerminalNodeID not in ExpandedNodes, want false")
	}
}

// TestSubWorkflowExpansionValid_InvalidPin verifies that Valid() rejects an
// expansion whose Pin fails Pin.Valid() (EM-034c).
func TestSubWorkflowExpansionValid_InvalidPin(t *testing.T) {
	t.Parallel()

	e := subwfExpandFixture(t)
	e.Pin = SubWorkflowExpansionPin{
		SubWorkflowRef:     SubWorkflowRef("workflows/reconciliation-v1"),
		SubWorkflowVersion: WorkflowVersion("1.0.0"),
		ResolvedWorkflowID: WorkflowID(uuid.Nil), // nil UUID makes Pin invalid
	}
	if e.Valid() {
		t.Error("Valid() = true for expansion with invalid Pin, want false")
	}
}

// TestSubWorkflowExpansionValid_MultipleTerminalNodes verifies that Valid()
// accepts an expansion with more than one terminal node, since sub-workflows
// may have multiple terminal nodes per EM-036a.
func TestSubWorkflowExpansionValid_MultipleTerminalNodes(t *testing.T) {
	t.Parallel()

	startID := NodeID("branch/start")
	endA := NodeID("branch/end-a")
	endB := NodeID("branch/end-b")

	e := SubWorkflowExpansion{
		ParentNodeID: NodeID("branch"),
		ExpandedNodes: []Node{
			subwfExpandFixtureNode(t, startID),
			subwfExpandFixtureNode(t, endA),
			subwfExpandFixtureNode(t, endB),
		},
		ExpandedEdges: []Edge{
			{FromNode: startID, ToNode: endA, OrderingKey: "0"},
			{FromNode: startID, ToNode: endB, OrderingKey: "1"},
		},
		StartNodeID:     startID,
		TerminalNodeIDs: []NodeID{endA, endB},
		Pin:             subwfExpandFixturePin(),
	}
	if !e.Valid() {
		t.Error("Valid() = false for expansion with multiple terminal nodes, want true")
	}
}
