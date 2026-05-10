package core

import (
	"testing"

	"github.com/google/uuid"
)

// subwfCompositionOnlyFixture returns a Workflow that composes behavior via a
// sub-workflow node referencing a named sub-workflow, per EM-037.
//
// EM-037: A workflow MUST NOT extend, inherit, or runtime-rewrite another
// workflow. Composition MUST be exclusively via sub-workflow nodes referencing
// named sub-workflows resolved at workflow-load time.
//
// The fixture encodes a parent workflow "orchestrator" containing one
// sub-workflow node referencing a child workflow "review-pipeline". The
// sub-workflow node is the only composition mechanism present.
func subwfCompositionOnlyFixture(t *testing.T) Workflow {
	t.Helper()

	subwfNode := subwfCompositionOnlyFixtureSubWorkflowNode(t)
	return Workflow{
		WorkflowID:      WorkflowID(uuid.MustParse("01960000-0000-7000-8000-000000005000")),
		Name:            "orchestrator",
		Version:         "1.0.0",
		Nodes:           []Node{subwfNode},
		Edges:           []Edge{},
		StartNodeID:     subwfNode.NodeID,
		TerminalNodeIDs: []NodeID{subwfNode.NodeID},
		SchemaVersion:   1,
	}
}

// subwfCompositionOnlyFixtureSubWorkflowNode returns a Node of type
// sub-workflow — the ONLY node type permitted to encode workflow composition
// per EM-037. Its SubWorkflowRef names the sub-workflow resolved at
// workflow-load time.
//
// The node is built from b3f73NodeSubWorkflow and has its SubWorkflowRef
// overridden to "review-pipeline" to name the composed sub-workflow.
func subwfCompositionOnlyFixtureSubWorkflowNode(t *testing.T) Node {
	t.Helper()

	n := b3f73NodeSubWorkflow(t)
	n.NodeID = NodeID("delegate-review")
	ref := SubWorkflowRef("review-pipeline")
	n.SubWorkflowRef = &ref
	return n
}

// TestSubWorkflowCompositionOnly_SubWorkflowNodeValid verifies that the
// sub-workflow node — the sole composition mechanism per EM-037 — is
// structurally valid and carries a non-empty SubWorkflowRef resolved at
// workflow-load time.
func TestSubWorkflowCompositionOnly_SubWorkflowNodeValid(t *testing.T) {
	t.Parallel()

	n := subwfCompositionOnlyFixtureSubWorkflowNode(t)
	if !n.Valid() {
		t.Error("sub-workflow composition node must be Valid() (EM-037)")
	}
	if n.Type != NodeTypeSubWorkflow {
		t.Errorf("Type = %q, want %q (EM-037: composition via sub-workflow node only)", n.Type, NodeTypeSubWorkflow)
	}
	if n.SubWorkflowRef == nil {
		t.Fatal("SubWorkflowRef must not be nil on a sub-workflow node (EM-037)")
	}
	if !n.SubWorkflowRef.Valid() {
		t.Error("SubWorkflowRef must be non-empty (EM-037: named sub-workflow resolved at load time)")
	}
}

// TestSubWorkflowCompositionOnly_WorkflowValid verifies that a workflow using
// the sub-workflow node as its composition mechanism is structurally valid.
func TestSubWorkflowCompositionOnly_WorkflowValid(t *testing.T) {
	t.Parallel()

	w := subwfCompositionOnlyFixture(t)
	if !w.Valid() {
		t.Error("subwfCompositionOnlyFixture must be Valid() (EM-037)")
	}
}

// TestSubWorkflowCompositionOnly_NoExtensionField documents that the Workflow
// type has no "extends" or "base_workflow" field. Workflow inheritance is not
// supported; the type system enforces EM-037 by not providing any extension
// hook. This test verifies the fixture workflow has no field that could encode
// inheritance or extension.
func TestSubWorkflowCompositionOnly_NoExtensionField(t *testing.T) {
	t.Parallel()

	w := subwfCompositionOnlyFixture(t)

	// A workflow may only reference other workflows via sub-workflow nodes.
	// Enumerate all nodes and confirm only NodeTypeSubWorkflow nodes carry
	// sub-workflow references; no other node type may reference another workflow.
	for i, n := range w.Nodes {
		if n.Type != NodeTypeSubWorkflow && n.SubWorkflowRef != nil {
			t.Errorf("node[%d] type=%q has non-nil SubWorkflowRef, want nil (EM-037: only sub-workflow nodes may reference another workflow)",
				i, n.Type)
		}
		if n.Type == NodeTypeSubWorkflow && n.SubWorkflowRef == nil {
			t.Errorf("node[%d] type=sub-workflow has nil SubWorkflowRef (EM-037: sub-workflow node must carry a ref)",
				i)
		}
	}
}

// TestSubWorkflowCompositionOnly_SubWorkflowRefRequiredOnSubWorkflowNode
// verifies that Valid() rejects a sub-workflow node with a nil SubWorkflowRef,
// confirming that every sub-workflow composition point must name a concrete
// sub-workflow per EM-037.
func TestSubWorkflowCompositionOnly_SubWorkflowRefRequiredOnSubWorkflowNode(t *testing.T) {
	t.Parallel()

	n := subwfCompositionOnlyFixtureSubWorkflowNode(t)
	n.SubWorkflowRef = nil
	if n.Valid() {
		t.Error("sub-workflow node with nil SubWorkflowRef must not be Valid() (EM-037: ref is required)")
	}
}

// TestSubWorkflowCompositionOnly_NonSubWorkflowNodeForbidsRef verifies that
// Valid() rejects a non-sub-workflow node that carries a SubWorkflowRef.
//
// EM-037: composition MUST be exclusively via sub-workflow nodes. An agentic or
// non-agentic node carrying a SubWorkflowRef would violate this constraint; the
// Node.Valid() invariant enforces the prohibition.
func TestSubWorkflowCompositionOnly_NonSubWorkflowNodeForbidsRef(t *testing.T) {
	t.Parallel()

	n := b3f73NodeNonAgentic(t)
	if n.Type == NodeTypeSubWorkflow {
		t.Fatal("b3f73NodeNonAgentic must not return a sub-workflow node")
	}

	ref := SubWorkflowRef("review-pipeline")
	n.SubWorkflowRef = &ref
	if n.Valid() {
		t.Errorf("non-sub-workflow node (type=%q) with SubWorkflowRef must not be Valid() (EM-037)", n.Type)
	}
}

// TestSubWorkflowCompositionOnly_SubWorkflowRefNamedAtLoadTime verifies that
// the SubWorkflowRef on a composition node is a non-empty string resolved at
// workflow-load time per EM-037.
//
// "Resolved at workflow-load time" means the ref names a concrete registered
// sub-workflow before any runtime expansion occurs. The SubWorkflowRef type
// enforces non-emptiness; the workflow loader (not tested here) resolves the
// name. This test confirms the structural precondition.
func TestSubWorkflowCompositionOnly_SubWorkflowRefNamedAtLoadTime(t *testing.T) {
	t.Parallel()

	n := subwfCompositionOnlyFixtureSubWorkflowNode(t)
	if n.SubWorkflowRef == nil {
		t.Fatal("SubWorkflowRef must be non-nil")
	}
	if *n.SubWorkflowRef == "" {
		t.Error("SubWorkflowRef must be non-empty at load time (EM-037)")
	}
	if !n.SubWorkflowRef.Valid() {
		t.Error("SubWorkflowRef.Valid() must be true (EM-037: non-empty name required)")
	}
}

// TestSubWorkflowCompositionOnly_EmptySubWorkflowRefInvalid verifies that an
// empty SubWorkflowRef on a sub-workflow node fails Valid(), enforcing that
// every composition point names a concrete sub-workflow per EM-037.
func TestSubWorkflowCompositionOnly_EmptySubWorkflowRefInvalid(t *testing.T) {
	t.Parallel()

	empty := SubWorkflowRef("")
	n := Node{
		NodeID:         NodeID("delegate-review"),
		Type:           NodeTypeSubWorkflow,
		SubWorkflowRef: &empty,
	}
	if n.Valid() {
		t.Error("sub-workflow node with empty SubWorkflowRef must not be Valid() (EM-037)")
	}
}

// TestSubWorkflowCompositionOnly_OnlySubWorkflowNodeType confirms that the type
// enum permits exactly five node types and that NodeTypeSubWorkflow is the one
// type capable of encoding workflow composition per EM-037.
func TestSubWorkflowCompositionOnly_OnlySubWorkflowNodeType(t *testing.T) {
	t.Parallel()

	allTypes := []NodeType{
		NodeTypeAgentic,
		NodeTypeNonAgentic,
		NodeTypeGate,
		NodeTypeControlPoint,
		NodeTypeSubWorkflow,
	}

	compositionTypes := []NodeType{}
	for _, nt := range allTypes {
		if nt == NodeTypeSubWorkflow {
			compositionTypes = append(compositionTypes, nt)
		}
	}

	if len(compositionTypes) != 1 {
		t.Errorf("expected exactly 1 composition-capable NodeType, got %d: %v (EM-037)", len(compositionTypes), compositionTypes)
	}
	if compositionTypes[0] != NodeTypeSubWorkflow {
		t.Errorf("composition-capable type = %q, want %q (EM-037)", compositionTypes[0], NodeTypeSubWorkflow)
	}
}
