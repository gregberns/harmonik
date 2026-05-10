package core

// NamespaceNodeID rewrites a sub-workflow node ID to its namespaced form per
// execution-model.md §4.8.EM-034a:
//
//	"On expansion, every sub-workflow node's node_id MUST be rewritten to the
//	form <parent_node_id>/<sub_node_id>."
//
// parentNodeID is the NodeID of the sub-workflow node in the parent workflow
// whose expansion is being performed. subNodeID is the original NodeID of a
// node inside the sub-workflow definition.
//
// For nested expansions (a sub-workflow referencing another sub-workflow), the
// rule composes left-to-right by calling NamespaceNodeID repeatedly:
//
//	NamespaceNodeID("A/B", "C") → "A/B/C"
//
// This matches the spec's grandparent/parent/child example:
//
//	"a grandparent node A containing sub-workflow node B containing sub-workflow
//	node C yields expanded node ID A/B/C."
//
// Both parentNodeID and subNodeID MUST be non-empty; callers are responsible
// for ensuring non-empty inputs.
func NamespaceNodeID(parentNodeID, subNodeID NodeID) NodeID {
	return NodeID(string(parentNodeID) + "/" + string(subNodeID))
}
