package core

// Property tests for HandlerRef using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. MarshalText/UnmarshalText round-trip: any non-empty HandlerRef marshals
//     to its string bytes and unmarshals back to the same value.
//
//  2. Empty rejected: MarshalText and UnmarshalText both return errors for the
//     empty string.
//
// See handlerref.go and handler-contract.md §6.1.

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
