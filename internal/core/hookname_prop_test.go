package core

// Property tests for HookName using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. String round-trip: converting a HookName to string and back preserves
//     the underlying value for any non-empty input.
//
//  2. Valid matches non-empty: Valid() returns true iff the HookName is
//     non-empty.
//
// See hookname.go and control-points.md §6.2.

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProp_HookName_StringRoundTrip checks that any HookName value survives
// conversion to string and back.
func TestProp_HookName_StringRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		hn := HookName(raw)
		if got := string(hn); got != raw {
			rt.Errorf("string round-trip failed: got %q, want %q", got, raw)
		}
	})
}

// TestProp_HookName_ValidMatchesNonEmpty checks that Valid() returns true for
// all non-empty HookName values and false for the zero value.
func TestProp_HookName_ValidMatchesNonEmpty(t *testing.T) {
	if HookName("").Valid() {
		t.Error("Valid(): expected false for empty HookName")
	}

	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		hn := HookName(raw)
		if !hn.Valid() {
			rt.Errorf("Valid() returned false for non-empty HookName %q", raw)
		}
	})
}
