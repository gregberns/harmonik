package core

import "testing"

// subwfNamespaceFixtureParent returns the NodeID of a sub-workflow node in a
// parent workflow, used as the parentNodeID argument in NamespaceNodeID tests
// (EM-034a).
func subwfNamespaceFixtureParent() NodeID {
	return NodeID("dispatch")
}

// subwfNamespaceFixtureSub returns a source-level NodeID from inside a
// sub-workflow definition, used as the subNodeID argument in NamespaceNodeID
// tests (EM-034a).
func subwfNamespaceFixtureSub() NodeID {
	return NodeID("step-one")
}

// TestNamespaceNodeID_Basic verifies the base case: a single expansion level
// produces <parent>/<sub> per EM-034a.
func TestNamespaceNodeID_Basic(t *testing.T) {
	t.Parallel()

	parent := subwfNamespaceFixtureParent()
	sub := subwfNamespaceFixtureSub()
	got := NamespaceNodeID(parent, sub)
	want := NodeID("dispatch/step-one")
	if got != want {
		t.Errorf("NamespaceNodeID(%q, %q) = %q, want %q", parent, sub, got, want)
	}
}

// TestNamespaceNodeID_Nested verifies that repeated application composes
// left-to-right for nested sub-workflow expansions per EM-034a:
//
//	A containing B containing C → "A/B/C"
func TestNamespaceNodeID_Nested(t *testing.T) {
	t.Parallel()

	// Grandparent node "A" expands sub-workflow node "B".
	// The already-namespaced ID of B within the parent expansion is "A/B".
	// Expanding B further for sub-node "C" must yield "A/B/C".
	grandparentSub := NamespaceNodeID(NodeID("A"), NodeID("B")) // "A/B"
	got := NamespaceNodeID(grandparentSub, NodeID("C"))         // "A/B/C"
	want := NodeID("A/B/C")
	if got != want {
		t.Errorf("nested NamespaceNodeID = %q, want %q", got, want)
	}
}

// TestNamespaceNodeID_PreservesParent verifies that the parent's own NodeID
// is unchanged by the rewriting operation per EM-034a:
//
//	"The parent's node_id remains unchanged."
func TestNamespaceNodeID_PreservesParent(t *testing.T) {
	t.Parallel()

	parent := subwfNamespaceFixtureParent()
	original := parent
	_ = NamespaceNodeID(parent, subwfNamespaceFixtureSub())
	if parent != original {
		t.Errorf("NamespaceNodeID mutated parentNodeID: got %q, want %q", parent, original)
	}
}

// TestNamespaceNodeID_MultipleSubNodes verifies that all sub-workflow nodes
// are rewritten independently when called for each sub node, and that distinct
// sub nodes yield distinct namespaced IDs per EM-034a.
func TestNamespaceNodeID_MultipleSubNodes(t *testing.T) {
	t.Parallel()

	parent := subwfNamespaceFixtureParent()
	subNodes := []NodeID{"start", "middle", "end"}
	seen := make(map[NodeID]bool)
	for _, sub := range subNodes {
		ns := NamespaceNodeID(parent, sub)
		if seen[ns] {
			t.Errorf("NamespaceNodeID produced collision for sub node %q: %q", sub, ns)
		}
		seen[ns] = true
	}
}

// TestNamespaceNodeID_SpecExample verifies the exact spec example from EM-034a:
//
//	grandparent A, sub-workflow node B, sub-sub-workflow node C → "A/B/C"
func TestNamespaceNodeID_SpecExample(t *testing.T) {
	t.Parallel()

	// Step 1: expand grandparent A's sub-workflow node B.
	ab := NamespaceNodeID(NodeID("A"), NodeID("B"))
	if ab != NodeID("A/B") {
		t.Fatalf("step 1: got %q, want %q", ab, "A/B")
	}

	// Step 2: within that expansion, expand B's sub-workflow node C.
	abc := NamespaceNodeID(ab, NodeID("C"))
	if abc != NodeID("A/B/C") {
		t.Fatalf("step 2: got %q, want %q", abc, "A/B/C")
	}
}
