package evaltoposort_test

import (
	"testing"

	evaltoposort "github.com/gregberns/harmonik/evaltasks/eval-topo-sort"
)

// validOrder checks that result is a valid topological ordering for edges.
// edges maps each node to its dependencies (prerequisite nodes).
func validOrder(result []string, edges map[string][]string) bool {
	pos := make(map[string]int, len(result))
	for i, n := range result {
		pos[n] = i
	}
	for node, deps := range edges {
		nodePos, nodeOk := pos[node]
		if !nodeOk {
			return false
		}
		for _, dep := range deps {
			depPos, depOk := pos[dep]
			if !depOk || depPos >= nodePos {
				return false
			}
		}
	}
	return true
}

func TestTopoSort(t *testing.T) {
	t.Parallel()

	t.Run("simple_chain", func(t *testing.T) {
		t.Parallel()
		// a → b → c
		edges := map[string][]string{
			"b": {"a"},
			"c": {"b"},
		}
		got, err := evaltoposort.TopoSort([]string{"a", "b", "c"}, edges)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !validOrder(got, edges) {
			t.Errorf("invalid topological order: %v", got)
		}
	})

	t.Run("diamond", func(t *testing.T) {
		t.Parallel()
		// a → b, a → c, b → d, c → d
		edges := map[string][]string{
			"b": {"a"},
			"c": {"a"},
			"d": {"b", "c"},
		}
		nodes := []string{"a", "b", "c", "d"}
		got, err := evaltoposort.TopoSort(nodes, edges)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !validOrder(got, edges) {
			t.Errorf("invalid topological order: %v", got)
		}
	})

	t.Run("cycle_detected", func(t *testing.T) {
		t.Parallel()
		// a → b → c → a (cycle)
		edges := map[string][]string{
			"b": {"a"},
			"c": {"b"},
			"a": {"c"},
		}
		_, err := evaltoposort.TopoSort([]string{"a", "b", "c"}, edges)
		if err == nil {
			t.Fatal("expected error for cyclic graph, got nil")
		}
	})

	t.Run("empty_graph", func(t *testing.T) {
		t.Parallel()
		got, err := evaltoposort.TopoSort([]string{}, map[string][]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("empty graph: got %v, want empty", got)
		}
	})

	t.Run("single_node", func(t *testing.T) {
		t.Parallel()
		got, err := evaltoposort.TopoSort([]string{"x"}, map[string][]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "x" {
			t.Errorf("single node: got %v, want [x]", got)
		}
	})
}
