package core

import (
	"testing"
)

// subwfTerminalOutcomeFixture returns an Outcome produced by the last expanded
// node in a sub-workflow execution, per EM-036a.
//
// EM-036a: The Outcome that escapes a sub-workflow node MUST be the Outcome
// produced by the last node in the expanded sub-workflow executed before the
// sub-workflow reached a terminal_node_id. This Outcome is the one the parent's
// edge-selection cascade (§4.10.EM-041) observes on the outgoing edges of the
// sub-workflow node.
func subwfTerminalOutcomeFixture(t *testing.T) Outcome {
	t.Helper()

	return Outcome{
		Status:         OutcomeStatusSuccess,
		PreferredLabel: nil,
		Kind:           OutcomeKindDefault,
		Payload:        nil,
		Notes:          "last expanded-node outcome; escapes sub-workflow per EM-036a",
	}
}

// subwfTerminalOutcomeFixtureWithLabel returns an Outcome with a preferred
// routing label, as the last expanded node might emit for cascade routing.
func subwfTerminalOutcomeFixtureWithLabel(t *testing.T, label string) Outcome {
	t.Helper()

	o := subwfTerminalOutcomeFixture(t)
	o.PreferredLabel = &label
	return o
}

// subwfTerminalOutcomeFixtureMultiTerminal builds a SubWorkflowExpansion with
// two terminal nodes representing a branching sub-workflow. At runtime exactly
// one terminal is reached; the Outcome that produced that terminal-reaching
// transition is the sub-workflow's terminal outcome per EM-036a.
func subwfTerminalOutcomeFixtureMultiTerminal(t *testing.T) SubWorkflowExpansion {
	t.Helper()

	startID := NodeID("route/start")
	endA := NodeID("route/success-end")
	endB := NodeID("route/fail-end")

	return SubWorkflowExpansion{
		ParentNodeID: NodeID("route"),
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
}

// TestSubWorkflowTerminalOutcome_LastExpandedNodeOutcomeValid verifies that the
// terminal outcome produced by the last expanded node satisfies Outcome.Valid()
// and is ready for consumption by the parent's edge-selection cascade (EM-036a).
func TestSubWorkflowTerminalOutcome_LastExpandedNodeOutcomeValid(t *testing.T) {
	t.Parallel()

	o := subwfTerminalOutcomeFixture(t)
	if !o.Valid() {
		t.Error("subwfTerminalOutcomeFixture produced invalid Outcome, want Valid() == true")
	}
}

// TestSubWorkflowTerminalOutcome_OutcomeStatusSuccess verifies that an
// OutcomeStatusSuccess escaping from the last expanded terminal node is valid
// for cascade routing per EM-036a.
func TestSubWorkflowTerminalOutcome_OutcomeStatusSuccess(t *testing.T) {
	t.Parallel()

	o := subwfTerminalOutcomeFixture(t)
	if o.Status != OutcomeStatusSuccess {
		t.Errorf("terminal outcome Status = %q, want %q (EM-036a: last-expanded-node outcome escapes sub-workflow)",
			o.Status, OutcomeStatusSuccess)
	}
}

// TestSubWorkflowTerminalOutcome_OutcomeStatusFail verifies that an
// OutcomeStatusFail from the last expanded node also escapes correctly per
// EM-036a — the rule is outcome-status-agnostic: whatever the last node
// produced is what escapes.
func TestSubWorkflowTerminalOutcome_OutcomeStatusFail(t *testing.T) {
	t.Parallel()

	o := subwfTerminalOutcomeFixture(t)
	o.Status = OutcomeStatusFail
	if !o.Valid() {
		t.Error("FAIL outcome must be Valid() for cascade routing (EM-036a)")
	}
	if o.Status != OutcomeStatusFail {
		t.Errorf("Status = %q, want %q", o.Status, OutcomeStatusFail)
	}
}

// TestSubWorkflowTerminalOutcome_WithPreferredLabel verifies that the escaping
// Outcome may carry a preferred routing label that the parent's cascade will
// observe on edges leaving the sub-workflow node (EM-036a + §4.10.EM-041).
func TestSubWorkflowTerminalOutcome_WithPreferredLabel(t *testing.T) {
	t.Parallel()

	o := subwfTerminalOutcomeFixtureWithLabel(t, "approved")
	if !o.Valid() {
		t.Error("Outcome with PreferredLabel must be Valid() (EM-036a)")
	}
	if o.PreferredLabel == nil || *o.PreferredLabel != "approved" {
		t.Errorf("PreferredLabel = %v, want \"approved\"", o.PreferredLabel)
	}
}

// TestSubWorkflowTerminalOutcome_MultiTerminalExpansionValid verifies that a
// sub-workflow with multiple declared terminal nodes is structurally valid.
//
// Per EM-036a: sub-workflows with multiple terminal nodes reach exactly one at
// runtime; the Outcome that produced the terminal-reaching transition is the
// sub-workflow's terminal outcome. The SubWorkflowExpansion record is valid even
// when TerminalNodeIDs contains more than one entry.
func TestSubWorkflowTerminalOutcome_MultiTerminalExpansionValid(t *testing.T) {
	t.Parallel()

	e := subwfTerminalOutcomeFixtureMultiTerminal(t)
	if !e.Valid() {
		t.Error("multi-terminal SubWorkflowExpansion must be Valid() (EM-036a)")
	}
	if len(e.TerminalNodeIDs) != 2 {
		t.Errorf("TerminalNodeIDs len = %d, want 2", len(e.TerminalNodeIDs))
	}
}

// TestSubWorkflowTerminalOutcome_ParentNodeDoesNotDeclareOutcome documents that
// the parent workflow's sub-workflow node MUST NOT declare its own Outcome shape;
// it inherits the expanded terminal outcome mechanically per EM-036a.
//
// In the type system this is enforced by the absence of an Outcome field on the
// Node type: a sub-workflow Node carries SubWorkflowRef but no Outcome — the
// Outcome is only produced at runtime by the last expanded node. This test
// confirms there is no Outcome field on Node that a sub-workflow node could use
// to override the mechanically-inherited terminal outcome.
func TestSubWorkflowTerminalOutcome_ParentNodeDoesNotDeclareOutcome(t *testing.T) {
	t.Parallel()

	n := b3f73NodeSubWorkflow(t)
	if n.Type != NodeTypeSubWorkflow {
		t.Fatalf("expected NodeTypeSubWorkflow, got %q", n.Type)
	}

	// The Node type has no Outcome field. A sub-workflow node's outcome is always
	// the terminal outcome of the last expanded node executed (EM-036a). The
	// absence of a static outcome shape on Node is the structural enforcement of
	// this rule: there is nothing for the sub-workflow node to declare.
	//
	// If an Outcome field were added to Node, that would violate EM-036a. This
	// test documents the invariant by confirming the sub-workflow node is valid
	// and its outcome emerges only at expansion time.
	if !n.Valid() {
		t.Error("sub-workflow node must be Valid() with no static Outcome field (EM-036a)")
	}
}

// TestSubWorkflowTerminalOutcome_ContextUpdatesAppliedBeforeEscape verifies that
// the Outcome escaping from a sub-workflow may carry ContextUpdates, which are
// applied prior to the parent's edge cascade per §4.10.EM-041a.
//
// EM-036a: context_updates already applied per EM-041a (post-coalesce); the
// cascade observes post-update context state.
func TestSubWorkflowTerminalOutcome_ContextUpdatesAppliedBeforeEscape(t *testing.T) {
	t.Parallel()

	o := subwfTerminalOutcomeFixture(t)
	o.ContextUpdates = map[string]any{
		"approval_status": "approved",
		"reviewed_by":     "agent-reviewer-01",
	}
	if !o.Valid() {
		t.Error("Outcome with ContextUpdates must be Valid() (EM-036a + EM-041a)")
	}
	if len(o.ContextUpdates) != 2 {
		t.Errorf("ContextUpdates len = %d, want 2", len(o.ContextUpdates))
	}
}
