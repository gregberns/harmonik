// Package evaltoposort provides Kahn's-algorithm topological sort for eval grading.
// This is the pre-committed reference; the model-under-test overwrites it.
package evaltoposort

import "fmt"

// TopoSort returns a topological ordering of nodes given their dependency edges.
// edges maps each node to the set of nodes it depends on (prerequisites).
// Returns an error if the graph contains a cycle.
func TopoSort(nodes []string, edges map[string][]string) ([]string, error) {
	// Build in-degree map and adjacency list (dep → dependents).
	inDeg := make(map[string]int, len(nodes))
	adj := make(map[string][]string)
	for _, n := range nodes {
		inDeg[n] = 0
	}
	for node, deps := range edges {
		for _, dep := range deps {
			adj[dep] = append(adj[dep], node)
			inDeg[node]++
		}
	}

	// Kahn's algorithm: enqueue nodes with in-degree 0.
	queue := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if inDeg[n] == 0 {
			queue = append(queue, n)
		}
	}

	out := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, cur)
		for _, dep := range adj[cur] {
			inDeg[dep]--
			if inDeg[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(out) != len(nodes) {
		return nil, fmt.Errorf("cycle detected: only %d of %d nodes ordered", len(out), len(nodes))
	}
	return out, nil
}
