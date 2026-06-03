package core

// Property tests for WorkspaceID using pgregory.net/rapid.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Invariants under test:
//
//  1. MarshalText/UnmarshalText round-trip: any WorkspaceID marshals to its
//     canonical UUID string and unmarshals back to the same value.
//
//  2. String parse-back: String() returns the canonical UUID form accepted
//     by UnmarshalText.
//
// See workspaceid.go and event-model.md §8.5.1–§8.5.6.

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// TestProp_WorkspaceID_MarshalTextRoundTrip checks that MarshalText followed
// by UnmarshalText is the identity function for any WorkspaceID value.
func TestProp_WorkspaceID_MarshalTextRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		rawBytes := rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, "bytes")
		var u uuid.UUID
		copy(u[:], rawBytes)
		original := WorkspaceID(u)

		text, err := original.MarshalText()
		if err != nil {
			rt.Fatalf("MarshalText failed: %v", err)
		}

		var recovered WorkspaceID
		if err := recovered.UnmarshalText(text); err != nil {
			rt.Fatalf("UnmarshalText failed: %v", err)
		}

		if recovered != original {
			rt.Errorf("round-trip mismatch: got %v, want %v", recovered, original)
		}
	})
}

// TestProp_WorkspaceID_StringParseBack checks that String() returns a form
// accepted by UnmarshalText and that the recovered value equals the original.
func TestProp_WorkspaceID_StringParseBack(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		rawBytes := rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, "bytes")
		var u uuid.UUID
		copy(u[:], rawBytes)
		original := WorkspaceID(u)

		s := original.String()

		var recovered WorkspaceID
		if err := recovered.UnmarshalText([]byte(s)); err != nil {
			rt.Fatalf("UnmarshalText of String() failed: %v", err)
		}

		if recovered != original {
			rt.Errorf("String parse-back mismatch: got %v, want %v", recovered, original)
		}
	})
}
