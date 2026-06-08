package core

// DependencyEdge is a directed dependency relationship between two beads
// (beads-integration.md §6.1 RECORD DependencyEdge).
// Both IDs must be non-empty and EdgeKind must be one of the four declared constants.
//
// EndpointStatus carries the CoarseStatus of the endpoint bead as reported by
// `br show` on the edge. It is zero (empty string) when the inline status field
// was absent or empty in the br show JSON response; callers must treat zero as
// "unknown" rather than "closed". T2 epic-completion checks use this field to
// determine whether every child bead is closed without a second br show round-trip.
type DependencyEdge struct {
	FromBeadID     BeadID      // source bead
	ToBeadID       BeadID      // target bead
	EdgeKind       EdgeKind    // one of {parent-child, blocks, conditional-blocks, waits-for}
	EndpointStatus CoarseStatus // status of the endpoint bead; zero = unknown (not closed)
}

// Valid reports whether e is a well-formed DependencyEdge: both IDs non-empty
// and EdgeKind valid per EdgeKind.Valid().
func (e DependencyEdge) Valid() bool {
	return e.FromBeadID != "" && e.ToBeadID != "" && e.EdgeKind.Valid()
}
