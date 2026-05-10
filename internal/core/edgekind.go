package core

import "fmt"

// EdgeKind is the kind of a dependency edge between beads (beads-integration.md §6.1).
//
// Read-surface tolerance (BI-007 precedent, hk-872.55): UnmarshalText accepts any
// non-empty value that Beads exposes — including "related" and future extensions —
// so that ShowBead never rejects a bead record solely because its edge type is not
// yet in harmonik's write-surface enum. Valid() returns false for unknown values so
// callers can distinguish spec-declared kinds from pass-through unknowns.
// MarshalText rejects unknown values; harmonik MUST NOT introduce edges of kinds
// outside the spec subset until a spec amendment lands for §6.1 ENUM EdgeKind.
type EdgeKind string

// EdgeKind values per beads-integration.md §6.1 ENUM declaration.
// These are the four harmonik WRITE-surface kinds. Beads may expose additional
// kinds (e.g. "related") on the READ surface; those pass through but are not
// declared as constants here.
const (
	EdgeKindParentChild       EdgeKind = "parent-child"
	EdgeKindBlocks            EdgeKind = "blocks"
	EdgeKindConditionalBlocks EdgeKind = "conditional-blocks"
	EdgeKindWaitsFor          EdgeKind = "waits-for"
)

// Valid reports whether e is one of the four spec-declared EdgeKind constants.
// Unknown values read from Beads (e.g. "related") return false but are NOT errors
// on the read surface; use UnmarshalText for ingestion and MarshalText for egress.
func (e EdgeKind) Valid() bool {
	switch e {
	case EdgeKindParentChild, EdgeKindBlocks, EdgeKindConditionalBlocks, EdgeKindWaitsFor:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so EdgeKind serialises
// correctly in JSON and YAML workflow definitions.
// Only spec-declared constants are accepted; unknown pass-through values are
// rejected to prevent harmonik from writing unapproved edge kinds to Beads.
func (e EdgeKind) MarshalText() ([]byte, error) {
	if !e.Valid() {
		return nil, fmt.Errorf("edgekind: unknown value %q; harmonik write surface is {parent-child, blocks, conditional-blocks, waits-for}", string(e))
	}
	return []byte(e), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// Read-surface tolerance: any non-empty value is accepted and stored verbatim so
// that ShowBead can parse bead records containing edge kinds that Beads exposes
// but harmonik's spec has not yet declared (e.g. "related"). Callers that need to
// act on the edge kind SHOULD check Valid() to distinguish known from unknown values.
// An empty string is still rejected as it cannot be a valid Beads dep-type.
func (e *EdgeKind) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return fmt.Errorf("edgekind: empty string is not a valid edge kind")
	}
	*e = EdgeKind(text)
	return nil
}
