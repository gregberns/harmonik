package core

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
