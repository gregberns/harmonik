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
			name:    "valid parent-child",
			input:   `{"kind":"parent-child"}`,
			want:    EdgeKindParentChild,
			wantErr: false,
		},
		{
			name:    "valid blocks",
			input:   `{"kind":"blocks"}`,
			want:    EdgeKindBlocks,
			wantErr: false,
		},
		{
			name:    "valid conditional-blocks",
			input:   `{"kind":"conditional-blocks"}`,
			want:    EdgeKindConditionalBlocks,
			wantErr: false,
		},
		{
			name:    "valid waits-for",
			input:   `{"kind":"waits-for"}`,
			want:    EdgeKindWaitsFor,
			wantErr: false,
		},
		{
			name:    "invalid parent_child underscore",
			input:   `{"kind":"parent_child"}`,
			wantErr: true,
		},
		{
			name:    "invalid PARENT-CHILD uppercase",
			input:   `{"kind":"PARENT-CHILD"}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown value",
			input:   `{"kind":"unknown"}`,
			wantErr: true,
		},
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

func TestEdgeKindMarshalText(t *testing.T) {
	t.Parallel()

	got, err := EdgeKindParentChild.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "parent-child" {
		t.Errorf("MarshalText = %q, want %q", string(got), "parent-child")
	}

	// All four valid values should marshal without error.
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

	// Bogus value must be rejected.
	if _, err := EdgeKind("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}
