package core

// BeadRecord is harmonik's in-memory representation of a bead read from the
// Beads task ledger via `br show` (beads-integration.md §6.1).
//
// All fields are owned by Beads; harmonik's consumption is read-only.  Any
// Beads schema change is absorbed by the br-CLI adapter per §4.8 BI-025.
//
// Fields:
//   - BeadID:        stable opaque identifier for the bead's lifetime (§4.3 BI-008)
//   - Title:         human-readable bead title (required; owned by Beads §4.3 BI-005)
//   - Description:   extended detail text (optional; owned by Beads)
//   - BeadType:      opaque bead-category string; harmonik treats as an opaque enum owned by Beads
//   - Status:        coarse lifecycle status from the Beads read surface (§6.1 ENUM CoarseStatus)
//   - Labels:        raw label strings from Beads (optional); includes workflow:<mode> per BI-009a
//   - Edges:         typed dependency edges connecting this bead to others (§6.1 RECORD DependencyEdge)
//   - AuditTrailRef: opaque handle used for `br audit-log` retrieval (§4.10 BI-029 / BI-031 step 3)
//   - Assignee:      crew or agent name assigned to this bead (optional; empty = unassigned)
type BeadRecord struct {
	BeadID        BeadID           // stable identifier for the bead's lifetime
	Title         string           // human-readable title (required)
	Description   string           // extended description (optional)
	BeadType      string           // opaque bead-category string owned by Beads
	Status        CoarseStatus     // coarse lifecycle status (read surface; §6.1 ENUM CoarseStatus)
	Labels        []string         // raw label strings from Beads; nil and empty are equivalent (optional)
	Edges         []DependencyEdge // typed dependency edges; may be empty for a freshly created bead
	AuditTrailRef string           // opaque handle for `br` audit-log retrieval
	Assignee      string           // crew/agent assignee (optional; empty = unassigned)
}

// Valid reports whether r is a well-formed BeadRecord.  A record is valid iff:
//   - BeadID is non-empty
//   - Title is non-empty
//   - BeadType is non-empty
//   - Status satisfies Status.Valid()
//   - Every element of Edges satisfies edge.Valid()
//   - AuditTrailRef is non-empty
//
// Description is optional and may be empty.
// An empty or nil Edges slice is valid (a freshly created bead has no dependencies).
func (r BeadRecord) Valid() bool {
	if r.BeadID == "" {
		return false
	}
	if r.Title == "" {
		return false
	}
	if r.BeadType == "" {
		return false
	}
	if !r.Status.Valid() {
		return false
	}
	for _, e := range r.Edges {
		if !e.Valid() {
			return false
		}
	}
	return r.AuditTrailRef != ""
}
