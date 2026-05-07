package core

import (
	"encoding/json"
	"testing"
)

func TestNodeTypeUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Type NodeType `json:"type"`
	}

	tests := []struct {
		name    string
		input   string
		want    NodeType
		wantErr bool
	}{
		{
			name:    "valid agentic",
			input:   `{"type":"agentic"}`,
			want:    NodeTypeAgentic,
			wantErr: false,
		},
		{
			name:    "valid non-agentic",
			input:   `{"type":"non-agentic"}`,
			want:    NodeTypeNonAgentic,
			wantErr: false,
		},
		{
			name:    "valid gate",
			input:   `{"type":"gate"}`,
			want:    NodeTypeGate,
			wantErr: false,
		},
		{
			name:    "valid control-point",
			input:   `{"type":"control-point"}`,
			want:    NodeTypeControlPoint,
			wantErr: false,
		},
		{
			name:    "valid sub-workflow",
			input:   `{"type":"sub-workflow"}`,
			want:    NodeTypeSubWorkflow,
			wantErr: false,
		},
		{
			name:    "invalid unknown value",
			input:   `{"type":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "invalid empty string",
			input:   `{"type":""}`,
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
			if w.Type != tc.want {
				t.Errorf("got %q, want %q", w.Type, tc.want)
			}
		})
	}
}

func TestNodeTypeMarshalText(t *testing.T) {
	t.Parallel()

	got, err := NodeTypeAgentic.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "agentic" {
		t.Errorf("MarshalText = %q, want %q", string(got), "agentic")
	}

	if _, err := NodeType("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}
}

func TestNodeTypeValid(t *testing.T) {
	t.Parallel()

	valid := []NodeType{
		NodeTypeAgentic,
		NodeTypeNonAgentic,
		NodeTypeGate,
		NodeTypeControlPoint,
		NodeTypeSubWorkflow,
	}
	for _, nt := range valid {
		if !nt.Valid() {
			t.Errorf("expected %q to be valid", nt)
		}
	}

	invalid := []NodeType{"", "AGENTIC", "Agentic", "human", "decision", "fork"}
	for _, nt := range invalid {
		if nt.Valid() {
			t.Errorf("expected %q to be invalid", nt)
		}
	}
}
