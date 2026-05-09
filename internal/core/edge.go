package core

// Edge is a directed connection between two workflow nodes (execution-model.md §6.1).
// Every Edge is a deterministic edge: cascade routing evaluates its Condition,
// Label, and Weight fields to select the next node per EM-041.
type Edge struct {
	// FromNode is the source node of this edge (required).
	FromNode NodeID

	// ToNode is the destination node of this edge (required).
	ToNode NodeID

	// Condition is an optional guard [PolicyExpression] evaluated by the
	// cascade evaluator before traversing the edge (control-points.md §6.4).
	// When nil the edge is traversed unconditionally.
	Condition *PolicyExpression

	// Label is an optional routing label used during cascade resolution.
	Label *string

	// PreferredLabel is an optional informative hint supplied by the upstream
	// node. The cascade resolver matches outcome.preferred_label against
	// Edge.Label (not against this field) per §4.10.EM-041; this field is
	// recorded for observability and human-readable workflow definitions.
	PreferredLabel *string

	// Weight is a numeric tie-breaker for edges that share the same Label.
	// The zero value (0) is the default per execution-model.md §6.1 and is valid.
	Weight int

	// OrderingKey is a lexical tie-break applied after Weight during cascade
	// resolution (required; must be non-empty).
	OrderingKey string

	// TraversalCap is an optional positive integer that bounds how many times
	// this edge may be traversed in a single run, enabling cycle-bounding per
	// §4.10.EM-043. When set it must be > 0.
	TraversalCap *int
}

// Valid reports whether e satisfies the minimum structural requirements for
// use in a workflow graph:
//   - FromNode must be non-empty
//   - ToNode must be non-empty
//   - OrderingKey must be non-empty
//   - TraversalCap, when set, must be positive (> 0) per EM-043
func (e Edge) Valid() bool {
	if e.FromNode == "" || e.ToNode == "" || e.OrderingKey == "" {
		return false
	}
	if e.TraversalCap != nil && *e.TraversalCap <= 0 {
		return false
	}
	return true
}
