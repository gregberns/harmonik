package core

import (
	"encoding/json"
	"testing"
)

func TestTransitionKindValid(t *testing.T) {
	t.Parallel()

	valid := []TransitionKind{
		TransitionKindForward,
		TransitionKindLocalPatchback,
		TransitionKindArchitecturalRollback,
		TransitionKindPolicyRollback,
		TransitionKindContextRestore,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("expected %q to be valid", k)
		}
	}

	invalid := []TransitionKind{
		"",
		"Forward",
		"FORWARD",
		"local_patchback",
		"architectural_rollback",
		"policy_rollback",
		"context_restore",
		"rollback",
		"patchback",
	}
	for _, k := range invalid {
		if k.Valid() {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func TestTransitionKindUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Kind TransitionKind `json:"kind"`
	}

	tests := []struct {
		name    string
		input   string
		want    TransitionKind
		wantErr bool
	}{
		{
			name:  "valid forward",
			input: `{"kind":"forward"}`,
			want:  TransitionKindForward,
		},
		{
			name:  "valid local-patchback",
			input: `{"kind":"local-patchback"}`,
			want:  TransitionKindLocalPatchback,
		},
		{
			name:  "valid architectural-rollback",
			input: `{"kind":"architectural-rollback"}`,
			want:  TransitionKindArchitecturalRollback,
		},
		{
			name:  "valid policy-rollback",
			input: `{"kind":"policy-rollback"}`,
			want:  TransitionKindPolicyRollback,
		},
		{
			name:  "valid context-restore",
			input: `{"kind":"context-restore"}`,
			want:  TransitionKindContextRestore,
		},
		{
			name:    "invalid empty string",
			input:   `{"kind":""}`,
			wantErr: true,
		},
		{
			name:    "invalid unknown value",
			input:   `{"kind":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "invalid rollback no qualifier",
			input:   `{"kind":"rollback"}`,
			wantErr: true,
		},
		{
			name:    "invalid uppercase Forward",
			input:   `{"kind":"Forward"}`,
			wantErr: true,
		},
		{
			name:    "invalid underscore variant local_patchback",
			input:   `{"kind":"local_patchback"}`,
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

func TestTransitionKindMarshalText(t *testing.T) {
	t.Parallel()

	got, err := TransitionKindForward.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "forward" {
		t.Errorf("MarshalText = %q, want %q", string(got), "forward")
	}

	if _, err := TransitionKind("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}
