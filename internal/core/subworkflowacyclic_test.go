package core

import "testing"

func subwfAcyclicFixtureLinear() *SubWorkflowRefGraph {
	g := NewSubWorkflowRefGraph()
	g.AddEdge("root", "child")
	g.AddEdge("child", "grandchild")
	return g
}

func subwfAcyclicFixtureSelfRef() *SubWorkflowRefGraph {
	g := NewSubWorkflowRefGraph()
	g.AddEdge("A", "A")
	return g
}

func subwfAcyclicFixtureMutual() *SubWorkflowRefGraph {
	g := NewSubWorkflowRefGraph()
	g.AddEdge("A", "B")
	g.AddEdge("B", "A")
	return g
}

func subwfAcyclicFixtureDiamondAcyclic() *SubWorkflowRefGraph {
	g := NewSubWorkflowRefGraph()
	g.AddEdge("A", "B")
	g.AddEdge("A", "C")
	g.AddEdge("B", "D")
	g.AddEdge("C", "D")
	return g
}

// TestSubWorkflowRefGraphHasCycle_EmptyGraph verifies that an empty graph is
// acyclic per EM-034b.
func TestSubWorkflowRefGraphHasCycle_EmptyGraph(t *testing.T) {
	t.Parallel()

	g := NewSubWorkflowRefGraph()
	if g.HasCycle() {
		t.Error("HasCycle() = true for empty graph, want false")
	}
}

// TestSubWorkflowRefGraphHasCycle_SingleNode verifies that a single isolated
// workflow node with no references is acyclic.
func TestSubWorkflowRefGraphHasCycle_SingleNode(t *testing.T) {
	t.Parallel()

	g := NewSubWorkflowRefGraph()
	g.AddEdge("only", "leaf")
	if g.HasCycle() {
		t.Error("HasCycle() = true for linear two-node graph, want false")
	}
}

// TestSubWorkflowRefGraphHasCycle_LinearChain verifies that a strictly linear
// reference chain (root → child → grandchild) is acyclic per EM-034b.
func TestSubWorkflowRefGraphHasCycle_LinearChain(t *testing.T) {
	t.Parallel()

	g := subwfAcyclicFixtureLinear()
	if g.HasCycle() {
		t.Error("HasCycle() = true for linear 3-node graph, want false")
	}
}

// TestSubWorkflowRefGraphHasCycle_SelfReference verifies that a self-referencing
// workflow (A → A) is detected as a cycle per EM-034b.
func TestSubWorkflowRefGraphHasCycle_SelfReference(t *testing.T) {
	t.Parallel()

	g := subwfAcyclicFixtureSelfRef()
	if !g.HasCycle() {
		t.Error("HasCycle() = false for self-referencing workflow, want true (EM-034b)")
	}
}

// TestSubWorkflowRefGraphHasCycle_MutualReference verifies that a mutual
// reference (A → B, B → A) is detected as a cycle per EM-034b.
func TestSubWorkflowRefGraphHasCycle_MutualReference(t *testing.T) {
	t.Parallel()

	g := subwfAcyclicFixtureMutual()
	if !g.HasCycle() {
		t.Error("HasCycle() = false for mutually-referencing workflows, want true (EM-034b)")
	}
}

// TestSubWorkflowRefGraphHasCycle_Diamond verifies that a diamond-shaped
// acyclic graph (shared sink) is correctly classified as acyclic.
func TestSubWorkflowRefGraphHasCycle_Diamond(t *testing.T) {
	t.Parallel()

	g := subwfAcyclicFixtureDiamondAcyclic()
	if g.HasCycle() {
		t.Error("HasCycle() = true for diamond-shaped acyclic graph, want false")
	}
}

// TestSubWorkflowRefGraphHasCycle_LongCycle verifies that a cycle through
// three nodes (A → B → C → A) is detected per EM-034b.
func TestSubWorkflowRefGraphHasCycle_LongCycle(t *testing.T) {
	t.Parallel()

	g := NewSubWorkflowRefGraph()
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	g.AddEdge("C", "A")
	if !g.HasCycle() {
		t.Error("HasCycle() = false for 3-node cycle A→B→C→A, want true (EM-034b)")
	}
}

// TestSubWorkflowRefGraphHasCycle_DisconnectedCycle verifies that a cycle in
// one component is detected even when another component is acyclic.
func TestSubWorkflowRefGraphHasCycle_DisconnectedCycle(t *testing.T) {
	t.Parallel()

	g := NewSubWorkflowRefGraph()
	g.AddEdge("X", "Y")
	g.AddEdge("A", "B")
	g.AddEdge("B", "A")
	if !g.HasCycle() {
		t.Error("HasCycle() = false for graph with disconnected cycle, want true (EM-034b)")
	}
}

// TestSubWorkflowRefGraphHasCycle_AddEdgeIdempotent verifies that adding the
// same edge twice does not convert an acyclic graph into a cyclic one.
// Duplicate edges are not back-edges.
func TestSubWorkflowRefGraphHasCycle_AddEdgeIdempotent(t *testing.T) {
	t.Parallel()

	g := NewSubWorkflowRefGraph()
	g.AddEdge("P", "Q")
	g.AddEdge("P", "Q") // duplicate
	if g.HasCycle() {
		t.Error("HasCycle() = true after adding duplicate edge, want false")
	}
}
