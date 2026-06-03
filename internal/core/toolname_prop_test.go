package core

// Property tests for ToolName using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. String round-trip: converting a ToolName to string and back preserves
//     the underlying value for any non-empty input.
//
//  2. Valid matches non-empty: Valid() returns true iff the ToolName is
//     non-empty.
//
// See toolname.go and control-points.md §6.2.

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProp_ToolName_StringRoundTrip checks that any ToolName value survives
// conversion to string and back.
func TestProp_ToolName_StringRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		tn := ToolName(raw)
		if got := string(tn); got != raw {
			rt.Errorf("string round-trip failed: got %q, want %q", got, raw)
		}
	})
}

// TestProp_ToolName_ValidMatchesNonEmpty checks that Valid() returns true for
// all non-empty ToolName values and false for the zero value.
func TestProp_ToolName_ValidMatchesNonEmpty(t *testing.T) {
	if ToolName("").Valid() {
		t.Error("Valid(): expected false for empty ToolName")
	}

	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		tn := ToolName(raw)
		if !tn.Valid() {
			rt.Errorf("Valid() returned false for non-empty ToolName %q", raw)
		}
	})
}
