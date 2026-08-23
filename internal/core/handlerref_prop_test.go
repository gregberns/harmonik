package core

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProp_HandlerRef_MarshalTextRoundTrip checks that MarshalText followed
// by UnmarshalText is the identity function for any non-empty HandlerRef.
func TestProp_HandlerRef_MarshalTextRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		h := HandlerRef(raw)

		text, err := h.MarshalText()
		if err != nil {
			rt.Fatalf("MarshalText failed: %v", err)
		}

		var recovered HandlerRef
		if err := recovered.UnmarshalText(text); err != nil {
			rt.Fatalf("UnmarshalText failed: %v", err)
		}

		if recovered != h {
			rt.Errorf("round-trip mismatch: got %q, want %q", recovered, h)
		}
	})
}

// TestProp_HandlerRef_EmptyRejected checks that both MarshalText and
// UnmarshalText reject the empty HandlerRef.
func TestProp_HandlerRef_EmptyRejected(t *testing.T) {
	var h HandlerRef

	if _, err := h.MarshalText(); err == nil {
		t.Error("MarshalText: expected error for empty HandlerRef, got nil")
	}

	var out HandlerRef
	if err := out.UnmarshalText([]byte("")); err == nil {
		t.Error("UnmarshalText: expected error for empty input, got nil")
	}
}
