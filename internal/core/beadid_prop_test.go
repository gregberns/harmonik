package core

// Property tests for BeadID using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. String round-trip: BeadID(s) → string → BeadID must recover the original
//     underlying value for any non-empty opaque string.
//
//  2. Equality symmetry: two BeadIDs constructed from the same raw string are
//     equal; two BeadIDs constructed from distinct strings are not.
//
//  3. Opacity — no parsing: BeadID values carry no structure that would allow
//     a consumer to extract a sub-field.  This test encodes that by verifying
//     that round-trip is the *only* operation (no prefix stripping, no length
//     constraint) — any string maps to a valid BeadID.
//
// See beads-integration.md BI-008, BI-008a.
// See hk-m084e for the bead that introduced this file.

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProp_BeadID_StringRoundTrip checks that converting a BeadID to string and
// back yields the same underlying value for any non-empty opaque string.
func TestProp_BeadID_StringRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		id := BeadID(raw)
		if got := string(id); got != raw {
			rt.Errorf("string round-trip failed: got %q, want %q", got, raw)
		}
	})
}

// TestProp_BeadID_EqualitySymmetry checks that two BeadIDs derived from the
// same raw string are equal, and two BeadIDs derived from distinct raw strings
// are not.
func TestProp_BeadID_EqualitySymmetry(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		a := rapid.StringN(1, 128, -1).Draw(rt, "a")
		b := rapid.StringN(1, 128, -1).Draw(rt, "b")

		idA1 := BeadID(a)
		idA2 := BeadID(a)
		if idA1 != idA2 {
			rt.Errorf("same-raw BeadIDs compare unequal: %q vs %q", idA1, idA2)
		}

		idB := BeadID(b)
		if a != b && idA1 == idB {
			rt.Errorf("distinct-raw BeadIDs compare equal: %q == %q", idA1, idB)
		}
	})
}
