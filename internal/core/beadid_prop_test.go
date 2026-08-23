package core

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
