package core

import (
	"encoding/json"
	"testing"
)

func TestSideEffectKindValid(t *testing.T) {
	t.Parallel()

	valid := []SideEffectKind{
		SideEffectKindEmitEvent,
		SideEffectKindStateMutation,
		SideEffectKindExternalAction,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("expected %q to be valid", k)
		}
	}

	invalid := []SideEffectKind{
		"",
		"emit-Event",
		"EmitEvent",
		"EMIT-EVENT",
		"state-Mutation",
		"StateMutation",
		"STATE-MUTATION",
		"external-Action",
		"ExternalAction",
		"EXTERNAL-ACTION",
		"unknown",
		"emit-event|state-mutation",
	}
	for _, k := range invalid {
		if k.Valid() {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func TestSideEffectKindMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind SideEffectKind
		want string
	}{
		{SideEffectKindEmitEvent, "emit-event"},
		{SideEffectKindStateMutation, "state-mutation"},
		{SideEffectKindExternalAction, "external-action"},
	}
	for _, tc := range tests {
		got, err := tc.kind.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.kind, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.kind, string(got), tc.want)
		}
	}

	if _, err := SideEffectKind("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := SideEffectKind("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestSideEffectKindUnmarshalText(t *testing.T) {
	t.Parallel()

	type sideEffectKindFixtureWrapper struct {
		Kind SideEffectKind `json:"kind"`
	}

	tests := []struct {
		name    string
		input   string
		want    SideEffectKind
		wantErr bool
	}{
		{
			name:  "emit-event",
			input: `{"kind":"emit-event"}`,
			want:  SideEffectKindEmitEvent,
		},
		{
			name:  "state-mutation",
			input: `{"kind":"state-mutation"}`,
			want:  SideEffectKindStateMutation,
		},
		{
			name:  "external-action",
			input: `{"kind":"external-action"}`,
			want:  SideEffectKindExternalAction,
		},
		{
			name:    "mixed-case emit rejected",
			input:   `{"kind":"emit-Event"}`,
			wantErr: true,
		},
		{
			name:    "uppercase EMIT-EVENT rejected",
			input:   `{"kind":"EMIT-EVENT"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"kind":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"kind":""}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"kind":"emit"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w sideEffectKindFixtureWrapper
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
				t.Errorf("got %q, want %q", string(w.Kind), string(tc.want))
			}
		})
	}
}
