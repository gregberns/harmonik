// Package core — BI-006 requirement-traceable sensors.
//
// This file provides named requirement-traceable sensors for the edge-kind set
// and read-only consumption discipline defined in beads-integration.md §4.2
// BI-006. Each test is a sensor: it fails when the implementation drifts from
// the spec, and must be updated in lockstep with any spec amendment.
//
// BI-006 citation: "The supported edge kinds MUST include parent-child, blocks,
// conditional-blocks, and waits-for. Harmonik consumes these edges read-only
// per §4.5."
package core

import "testing"

// TestEdgeKindBI006_FourSupportedKinds asserts that every one of the four
// canonical EdgeKind values declared in BI-006 (beads-integration.md §4.2)
// reports Valid() == true.
//
// BI-006: "The supported edge kinds MUST include parent-child, blocks,
// conditional-blocks, and waits-for."
func TestEdgeKindBI006_FourSupportedKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind EdgeKind
		wire string
	}{
		{name: "parent-child", kind: EdgeKindParentChild, wire: "parent-child"},
		{name: "blocks", kind: EdgeKindBlocks, wire: "blocks"},
		{name: "conditional-blocks", kind: EdgeKindConditionalBlocks, wire: "conditional-blocks"},
		{name: "waits-for", kind: EdgeKindWaitsFor, wire: "waits-for"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !tc.kind.Valid() {
				t.Errorf("BI-006: EdgeKind %q must be valid; Valid() returned false", tc.wire)
			}
			if string(tc.kind) != tc.wire {
				t.Errorf("BI-006: EdgeKind wire value = %q, want %q", string(tc.kind), tc.wire)
			}
		})
	}
}

// TestEdgeKindBI006_ExhaustiveSet is a soft-boundary sensor for the BI-006
// canonical set. It asserts that:
//   - each of the four BI-006 wire strings produces a Valid() EdgeKind, and
//   - a value outside the set ("sibling") is NOT valid.
//
// This test does not enumerate all possible strings, but it acts as a
// reconciliation trigger: if a fifth EdgeKind constant is added to the
// implementation, this test must be updated in lockstep with a corresponding
// spec amendment to BI-006 (beads-integration.md §4.2).
//
// BI-006: "The supported edge kinds MUST include parent-child, blocks,
// conditional-blocks, and waits-for."
func TestEdgeKindBI006_ExhaustiveSet(t *testing.T) {
	t.Parallel()

	// canonicalBI006 is the complete set of wire strings mandated by BI-006.
	// If a new EdgeKind is added, update this slice AND amend BI-006 in the spec.
	canonicalBI006 := []string{
		"parent-child",
		"blocks",
		"conditional-blocks",
		"waits-for",
	}

	for _, wire := range canonicalBI006 {
		ek := EdgeKind(wire)
		if !ek.Valid() {
			t.Errorf("BI-006: canonical wire string %q must produce Valid() == true", wire)
		}
	}

	// A value outside the canonical set must be invalid.
	// Update this sentinel if "sibling" is ever added to BI-006 (which would
	// require a spec amendment).
	sentinel := EdgeKind("sibling")
	if sentinel.Valid() {
		t.Errorf("BI-006: non-canonical EdgeKind %q must NOT be valid; update BI-006 if this kind is intentional", sentinel)
	}
}

// TestDependencyEdgeBI006_HarmonikReadsOnly documents the read-only
// consumption discipline from BI-006 (beads-integration.md §4.2 and §4.5).
//
// This test carries no runtime assertions. It exists so that future
// contributors who attempt to add Persist, Write, Save, or similar mutating
// methods to DependencyEdge are reviewed against BI-006 before merging.
//
// BI-006: "Harmonik consumes these edges read-only per §4.5."
func TestDependencyEdgeBI006_HarmonikReadsOnly(t *testing.T) {
	t.Log("BI-006: harmonik MUST consume DependencyEdge read-only; no Persist/Write/Save methods may be added.")
	t.Log("Spec reference: beads-integration.md §4.2 BI-006 and §4.5.")

	t.Run("read-only-discipline-marker", func(t *testing.T) {
		t.Log("If you are here because a Persist/Write/Save method was added to DependencyEdge:")
		t.Log("  1. Check beads-integration.md BI-006 — harmonik consumes edges read-only.")
		t.Log("  2. Coordinate with the spec author before adding write methods.")
		t.SkipNow()
	})
}
