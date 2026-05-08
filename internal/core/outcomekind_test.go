package core

import (
	"encoding/json"
	"testing"
)

func TestOutcomeKindValid(t *testing.T) {
	t.Parallel()

	valid := []OutcomeKind{
		OutcomeKindDefault,
		OutcomeKindReconciliationVerdict,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("expected %q to be valid", k)
		}
	}

	invalid := []OutcomeKind{
		"",
		"DEFAULT",
		"Default",
		"RECONCILIATION_VERDICT",
		"unknown",
		"reconciliation",
		"verdict",
	}
	for _, k := range invalid {
		if k.Valid() {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func TestOutcomeKindMarshalText(t *testing.T) {
	t.Parallel()

	got, err := OutcomeKindDefault.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "default" {
		t.Errorf("MarshalText = %q, want %q", string(got), "default")
	}

	got, err = OutcomeKindReconciliationVerdict.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "reconciliation_verdict" {
		t.Errorf("MarshalText = %q, want %q", string(got), "reconciliation_verdict")
	}

	if _, err := OutcomeKind("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := OutcomeKind("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestOutcomeKindUnmarshalText(t *testing.T) {
	t.Parallel()

	type outcomeKindFixtureWrapper struct {
		Kind OutcomeKind `json:"outcome_kind"`
	}

	tests := []struct {
		name    string
		input   string
		want    OutcomeKind
		wantErr bool
	}{
		{
			name:  "default",
			input: `{"outcome_kind":"default"}`,
			want:  OutcomeKindDefault,
		},
		{
			name:  "reconciliation_verdict",
			input: `{"outcome_kind":"reconciliation_verdict"}`,
			want:  OutcomeKindReconciliationVerdict,
		},
		{
			name:    "unknown value routes to error (Cat 6a per §8.11)",
			input:   `{"outcome_kind":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"outcome_kind":""}`,
			wantErr: true,
		},
		{
			name:    "uppercase DEFAULT rejected",
			input:   `{"outcome_kind":"DEFAULT"}`,
			wantErr: true,
		},
		{
			name:    "partial match reconciliation rejected",
			input:   `{"outcome_kind":"reconciliation"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w outcomeKindFixtureWrapper
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
