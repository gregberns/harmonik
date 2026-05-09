package core

// GuardPayload is the per-attachment payload for a Guard control point
// (specs/control-points.md §6.1.3 RECORD GuardPayload).
//
// Guard payload is intentionally thin: the reorder logic lives entirely in
// the evaluator (mechanism-only per §4.4.CP-020). GuardPayload carries only
// the optional node-scoping field that constrains which node the guard applies
// to; all ordering decisions belong to the evaluator, not the record.
//
// AppliesToNode is a *NodeID (String | None per §6.1.3): a nil value means
// the guard applies to all nodes; a non-nil value must be a non-empty NodeID
// that scopes the guard to a single node.
type GuardPayload struct {
	// AppliesToNode is the NodeID that scopes this guard to a single node.
	// None (nil) means the guard applies to all nodes.
	// When non-nil, the value MUST be a non-empty NodeID.
	//
	// Wire field name: applies_to_node (specs/control-points.md §6.1.3).
	AppliesToNode *NodeID `json:"applies_to_node,omitempty"`
}

// Valid reports whether p satisfies the structural invariant declared in
// specs/control-points.md §6.1.3:
//
//   - AppliesToNode, when non-nil, must dereference to a non-empty NodeID.
//     A nil pointer (None) is always valid — it means the guard applies to all
//     nodes.
func (p GuardPayload) Valid() bool {
	if p.AppliesToNode != nil && *p.AppliesToNode == "" {
		return false
	}
	return true
}
