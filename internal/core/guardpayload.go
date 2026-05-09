package core

// GuardPayload is the per-attachment payload for a Guard control point
// (specs/control-points.md §6.1.3 RECORD GuardPayload).
//
// Guard payload is intentionally thin: the reorder logic lives entirely in
// the evaluator (mechanism-only per §4.4.CP-020). GuardPayload carries only
// the optional node-scoping field that constrains which node the guard applies
// to; all ordering decisions belong to the evaluator, not the record.
//
// AppliestoNode is a *string (String | None per §6.1.3): a nil value means
// the guard applies to all nodes; a non-nil value must be a non-empty node_id
// string that scopes the guard to a single node.
//
// TODO(hk-a8bg.65-followup): replace *string with NodeID typed alias once a
// dedicated bead upgrades the node_id field to the NodeID type per
// specs/control-points.md §6.1.3.
type GuardPayload struct {
	// AppliestoNode is the node_id that scopes this guard to a single node.
	// None (nil) means the guard applies to all nodes.
	// When non-nil, the value MUST be a non-empty string.
	//
	// Wire field name: applies_to_node (specs/control-points.md §6.1.3).
	AppliestoNode *string `json:"applies_to_node,omitempty"`
}

// Valid reports whether p satisfies the structural invariant declared in
// specs/control-points.md §6.1.3:
//
//   - AppliestoNode, when non-nil, must dereference to a non-empty string.
//     A nil pointer (None) is always valid — it means the guard applies to all
//     nodes.
func (p GuardPayload) Valid() bool {
	if p.AppliestoNode != nil && *p.AppliestoNode == "" {
		return false
	}
	return true
}
