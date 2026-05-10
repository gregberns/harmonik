package core

// SubWorkflowRefGraph is the directed graph of sub-workflow references used to
// enforce the acyclicity requirement declared in execution-model.md
// §4.8.EM-034b:
//
//	"The directed graph whose vertices are workflows and whose edges are
//	sub-workflow references (A → B if workflow A contains a sub-workflow node
//	referencing workflow B) MUST be acyclic."
//
// Vertices are workflow identifiers (strings); edges are sub-workflow
// references from a parent workflow to a child workflow. Self-reference
// (A → A) and mutual reference (A → B → A) are both cycles and MUST be
// rejected by the pre-run validator per §4.9.EM-038.
//
// Use AddEdge to populate the graph, then call HasCycle to detect violations.
// Detection MUST occur during the transitive resolution pass; a cycle MUST
// fail validation before any node executes (EM-034b).
type SubWorkflowRefGraph struct {
	edges map[string][]string
}

// NewSubWorkflowRefGraph returns an empty, ready-to-use SubWorkflowRefGraph.
func NewSubWorkflowRefGraph() *SubWorkflowRefGraph {
	return &SubWorkflowRefGraph{edges: make(map[string][]string)}
}

// AddEdge records a directed edge from parent to child in the reference graph.
// parent is the referencing workflow's identifier; child is the referenced
// sub-workflow's identifier (the sub_workflow_ref value resolved to a workflow
// ID). Both values must be non-empty.
func (g *SubWorkflowRefGraph) AddEdge(parent, child string) {
	g.edges[parent] = append(g.edges[parent], child)
	// Ensure child has a vertex even if it has no outgoing edges.
	if _, ok := g.edges[child]; !ok {
		g.edges[child] = nil
	}
}

// HasCycle reports whether the reference graph contains any cycle.
//
// The algorithm is a depth-first search (DFS) with a three-colour marking
// scheme (white/grey/black) that detects back-edges, the canonical DFS cycle
// check for directed graphs. Grey nodes are those currently on the DFS stack;
// a grey-to-grey edge is a back-edge and indicates a cycle.
//
// This is the function the pre-run validator calls after building the graph
// via transitive resolution per EM-034b.
func (g *SubWorkflowRefGraph) HasCycle() bool {
	// colour: 0 = white (unvisited), 1 = grey (on stack), 2 = black (done)
	colour := make(map[string]int, len(g.edges))

	var dfs func(v string) bool
	dfs = func(v string) bool {
		colour[v] = 1 // grey
		for _, w := range g.edges[v] {
			switch colour[w] {
			case 1: // back-edge: cycle detected
				return true
			case 0: // unvisited
				if dfs(w) {
					return true
				}
			}
			// colour 2 = already fully explored, safe to skip
		}
		colour[v] = 2 // black
		return false
	}

	for v := range g.edges {
		if colour[v] == 0 {
			if dfs(v) {
				return true
			}
		}
	}
	return false
}
