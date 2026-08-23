package core

import (
	"encoding/json"
	"testing"
)

func TestEdgeKindValid(t *testing.T) {
	t.Parallel()

	valid := []EdgeKind{
		EdgeKindParentChild,
		EdgeKindBlocks,
		EdgeKindConditionalBlocks,
		EdgeKindWaitsFor,
	}
	for _, ek := range valid {
		if !ek.Valid() {
			t.Errorf("expected %q to be valid", ek)
		}
	}

	invalid := []EdgeKind{
		"",
		"parent_child",       // underscore vs hyphen
		"PARENT-CHILD",       // wrong case
		"conditional_blocks", // underscore vs hyphen
		"waits_for",          // underscore vs hyphen
		"unknown",
		"fork",
		"related", // Beads read-surface value; Valid() false but UnmarshalText accepts it
	}
	for _, ek := range invalid {
		if ek.Valid() {
			t.Errorf("expected %q to be invalid", ek)
		}
	}
}

func TestEdgeKindUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Kind EdgeKind `json:"kind"`
	}

	tests := []struct {
		name    string
		input   string
		want    EdgeKind
		wantErr bool
	}{
		{
			name:  "valid parent-child",
			input: `{"kind":"parent-child"}`,
			want:  EdgeKindParentChild,
		},
		{
			name:  "valid blocks",
			input: `{"kind":"blocks"}`,
			want:  EdgeKindBlocks,
		},
		{
			name:  "valid conditional-blocks",
			input: `{"kind":"conditional-blocks"}`,
			want:  EdgeKindConditionalBlocks,
		},
		{
			name:  "valid waits-for",
			input: `{"kind":"waits-for"}`,
			want:  EdgeKindWaitsFor,
		},
		// Read-surface tolerance (hk-872.55): Beads-exposed values not yet in the
		// spec ENUM are accepted verbatim.  Valid() will return false for these.
		{
			name:  "pass-through related",
			input: `{"kind":"related"}`,
			want:  EdgeKind("related"),
		},
		{
			name:  "pass-through parent_child underscore variant",
			input: `{"kind":"parent_child"}`,
			want:  EdgeKind("parent_child"),
		},
		{
			name:  "pass-through arbitrary future value",
			input: `{"kind":"PARENT-CHILD"}`,
			want:  EdgeKind("PARENT-CHILD"),
		},
		{
			name:  "pass-through unknown",
			input: `{"kind":"unknown"}`,
			want:  EdgeKind("unknown"),
		},
		// Only the empty string is still rejected — it cannot be a valid dep-type.
		{
			name:    "invalid empty string",
			input:   `{"kind":""}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w wrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.Kind != tc.want {
				t.Errorf("got %q, want %q", w.Kind, tc.want)
			}
		})
	}
}

func edgeKindToleranceFixtureKnownValues() []EdgeKind {
	return []EdgeKind{
		EdgeKindParentChild,
		EdgeKindBlocks,
		EdgeKindConditionalBlocks,
		EdgeKindWaitsFor,
	}
}

// TestEdgeKindReadSurfaceTolerance verifies the two-surface contract introduced
// by hk-872.55: UnmarshalText is permissive (read surface) while MarshalText is
// strict (write surface).
func TestEdgeKindReadSurfaceTolerance(t *testing.T) {
	t.Parallel()

	beadsValues := []string{"related", "blocks", "parent-child"}
	for _, v := range beadsValues {
		var ek EdgeKind
		if err := ek.UnmarshalText([]byte(v)); err != nil {
			t.Errorf("UnmarshalText(%q) should not error on read surface: %v", v, err)
		}
		if string(ek) != v {
			t.Errorf("UnmarshalText(%q) stored %q, want %q", v, ek, v)
		}
	}

	passThrough := EdgeKind("related")
	if passThrough.Valid() {
		t.Errorf("Valid() should return false for pass-through value %q", passThrough)
	}
	if _, err := passThrough.MarshalText(); err == nil {
		t.Errorf("MarshalText should reject pass-through value %q to protect write surface", passThrough)
	}

	for _, ek := range edgeKindToleranceFixtureKnownValues() {
		if !ek.Valid() {
			t.Errorf("Valid() should return true for declared constant %q", ek)
		}
		if _, err := ek.MarshalText(); err != nil {
			t.Errorf("MarshalText(%q) unexpected error: %v", ek, err)
		}
	}
}

func TestEdgeKindMarshalText(t *testing.T) {
	t.Parallel()

	got, err := EdgeKindParentChild.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "parent-child" {
		t.Errorf("MarshalText = %q, want %q", string(got), "parent-child")
	}

	validKinds := []EdgeKind{
		EdgeKindParentChild,
		EdgeKindBlocks,
		EdgeKindConditionalBlocks,
		EdgeKindWaitsFor,
	}
	for _, ek := range validKinds {
		b, err := ek.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%q) unexpected error: %v", ek, err)
		}
		if string(b) != string(ek) {
			t.Errorf("MarshalText(%q) = %q, want %q", ek, string(b), string(ek))
		}
	}

	unknownValues := []EdgeKind{
		"bogus",
		"related",
		"",
	}
	for _, ek := range unknownValues {
		if _, err := ek.MarshalText(); err == nil {
			t.Errorf("MarshalText(%q) should reject unknown value", ek)
		}
	}
}
