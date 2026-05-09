package core

import (
	"encoding/json"
	"testing"
)

func TestGateSubtypeValid(t *testing.T) {
	t.Parallel()

	valid := []GateSubtype{
		GateSubtypeGoal,
		GateSubtypeApproval,
		GateSubtypeQuality,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []GateSubtype{
		"",
		"goal",
		"approval",
		"quality",
		"Goal-gate",
		"Approval-gate",
		"Quality-gate",
		"GOAL-GATE",
		"APPROVAL-GATE",
		"QUALITY-GATE",
		"unknown",
		"gate",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestGateSubtypeMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		subtype GateSubtype
		want    string
	}{
		{GateSubtypeGoal, "goal-gate"},
		{GateSubtypeApproval, "approval-gate"},
		{GateSubtypeQuality, "quality-gate"},
	}
	for _, tc := range tests {
		got, err := tc.subtype.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.subtype, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.subtype, string(got), tc.want)
		}
	}

	if _, err := GateSubtype("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := GateSubtype("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestGateSubtypeUnmarshalText(t *testing.T) {
	t.Parallel()

	type gateSubtypeFixtureWrapper struct {
		Subtype GateSubtype `json:"subtype"`
	}

	tests := []struct {
		name    string
		input   string
		want    GateSubtype
		wantErr bool
	}{
		{
			name:  "goal-gate",
			input: `{"subtype":"goal-gate"}`,
			want:  GateSubtypeGoal,
		},
		{
			name:  "approval-gate",
			input: `{"subtype":"approval-gate"}`,
			want:  GateSubtypeApproval,
		},
		{
			name:  "quality-gate",
			input: `{"subtype":"quality-gate"}`,
			want:  GateSubtypeQuality,
		},
		{
			name:    "bare goal rejected",
			input:   `{"subtype":"goal"}`,
			wantErr: true,
		},
		{
			name:    "uppercase GOAL-GATE rejected",
			input:   `{"subtype":"GOAL-GATE"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"subtype":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"subtype":""}`,
			wantErr: true,
		},
		{
			name:    "partial match approval rejected",
			input:   `{"subtype":"approval"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w gateSubtypeFixtureWrapper
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
			if w.Subtype != tc.want {
				t.Errorf("got %q, want %q", string(w.Subtype), string(tc.want))
			}
		})
	}
}
