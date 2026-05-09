package core

import (
	"encoding/json"
	"testing"
)

func TestGateActionValid(t *testing.T) {
	t.Parallel()

	valid := []GateAction{
		GateActionAllow,
		GateActionDeny,
		GateActionEscalateToHuman,
	}
	for _, a := range valid {
		if !a.Valid() {
			t.Errorf("expected %q to be valid", a)
		}
	}

	invalid := []GateAction{
		"",
		"Allow",
		"ALLOW",
		"Deny",
		"DENY",
		"escalate-To-Human",
		"ESCALATE-TO-HUMAN",
		"escalate_to_human",
		"escalateToHuman",
		"unknown",
		"allow|deny",
	}
	for _, a := range invalid {
		if a.Valid() {
			t.Errorf("expected %q to be invalid", a)
		}
	}
}

func TestGateActionMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action GateAction
		want   string
	}{
		{GateActionAllow, "allow"},
		{GateActionDeny, "deny"},
		{GateActionEscalateToHuman, "escalate-to-human"},
	}
	for _, tc := range tests {
		got, err := tc.action.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.action, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.action, string(got), tc.want)
		}
	}

	if _, err := GateAction("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := GateAction("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestGateActionUnmarshalText(t *testing.T) {
	t.Parallel()

	type gateActionFixtureWrapper struct {
		Action GateAction `json:"action"`
	}

	tests := []struct {
		name    string
		input   string
		want    GateAction
		wantErr bool
	}{
		{
			name:  "allow",
			input: `{"action":"allow"}`,
			want:  GateActionAllow,
		},
		{
			name:  "deny",
			input: `{"action":"deny"}`,
			want:  GateActionDeny,
		},
		{
			name:  "escalate-to-human",
			input: `{"action":"escalate-to-human"}`,
			want:  GateActionEscalateToHuman,
		},
		{
			name:    "mixed-case Allow rejected",
			input:   `{"action":"Allow"}`,
			wantErr: true,
		},
		{
			name:    "uppercase DENY rejected",
			input:   `{"action":"DENY"}`,
			wantErr: true,
		},
		{
			name:    "camelCase escalateToHuman rejected",
			input:   `{"action":"escalateToHuman"}`,
			wantErr: true,
		},
		{
			name:    "underscore form escalate_to_human rejected",
			input:   `{"action":"escalate_to_human"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"action":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"action":""}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"action":"esc"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w gateActionFixtureWrapper
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
			if w.Action != tc.want {
				t.Errorf("got %q, want %q", string(w.Action), string(tc.want))
			}
		})
	}
}
