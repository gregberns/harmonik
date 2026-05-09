package core

import "fmt"

// AttachPoint is the discriminator for where a Gate fires within the
// execution lifecycle (control-points.md §6.1.1 ENUM AttachPoint, v0.3.2).
//
// A Gate MUST declare an attach point at registration; a Gate without one
// fails registration per §4.1.CP-007. When multiple Gates are registered at
// the same attach point, the S01 invocation layer MUST honor declaration order
// and MUST short-circuit on the first non-allow verdict (CP-007: declared-order
// + first-non-allow is a record-side ordering property consumed by S01, not an
// S01-internal choice).
//
// See also specs/control-points.md §4.1.CP-007 for the full Gate-ordering rule.
type AttachPoint string

// AttachPoint values per control-points.md §6.1.1 ENUM AttachPoint (v0.3.2).
const (
	// AttachPointNodePreEntry fires before the workflow enters a node. A Gate
	// at this attach point MAY inspect run.next_node (the candidate target) and
	// return allow or deny before execution begins.
	AttachPointNodePreEntry AttachPoint = "node-pre-entry"

	// AttachPointNodePostExit fires after the workflow exits a node, before
	// edge selection. A Gate here observes the completed handler outcome.
	AttachPointNodePostExit AttachPoint = "node-post-exit"

	// AttachPointEdgeBeforeSelection fires during edge evaluation before the
	// edge cascade selects a candidate transition. Gates here influence which
	// edges are considered.
	AttachPointEdgeBeforeSelection AttachPoint = "edge-before-selection"

	// AttachPointEdgeAfterSelection fires after the edge cascade has selected
	// a candidate transition but before the transition is committed. A Gate
	// here may deny a specific selected edge.
	AttachPointEdgeAfterSelection AttachPoint = "edge-after-selection"
)

// Valid reports whether ap is one of the four declared AttachPoint constants.
// An unknown AttachPoint MUST be rejected at registration per
// [control-points.md §4.9]; callers MUST NOT silently fall back to a default.
func (ap AttachPoint) Valid() bool {
	switch ap {
	case AttachPointNodePreEntry, AttachPointNodePostExit,
		AttachPointEdgeBeforeSelection, AttachPointEdgeAfterSelection:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so AttachPoint serialises
// correctly in JSON and YAML policy documents (control-points.md §6.3).
// It rejects any value that is not one of the four declared constants.
func (ap AttachPoint) MarshalText() ([]byte, error) {
	if !ap.Valid() {
		return nil, fmt.Errorf("attachpoint: unknown value %q", string(ap))
	}
	return []byte(ap), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the four declared constants.
// Per control-points.md §4.9, unknown AttachPoint values must be rejected at
// registration; callers MUST NOT silently degrade to a default attach point.
func (ap *AttachPoint) UnmarshalText(text []byte) error {
	v := AttachPoint(text)
	if !v.Valid() {
		return fmt.Errorf(
			"attachpoint: unknown value %q; must be one of node-pre-entry, node-post-exit, edge-before-selection, edge-after-selection",
			string(text),
		)
	}
	*ap = v
	return nil
}
