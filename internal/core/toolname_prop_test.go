package core

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
