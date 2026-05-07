package core

// DependencyEdge is a directed dependency relationship between two beads
// (beads-integration.md §6.1 RECORD DependencyEdge).
// Both IDs must be non-empty and EdgeKind must be one of the four declared constants.
type DependencyEdge struct {
	FromBeadID BeadID   // source bead
	ToBeadID   BeadID   // target bead
	EdgeKind   EdgeKind // one of {parent-child, blocks, conditional-blocks, waits-for}
}

// Valid reports whether e is a well-formed DependencyEdge: both IDs non-empty
// and EdgeKind valid per EdgeKind.Valid().
func (e DependencyEdge) Valid() bool {
	return e.FromBeadID != "" && e.ToBeadID != "" && e.EdgeKind.Valid()
}
