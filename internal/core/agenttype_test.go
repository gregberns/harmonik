package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentTypeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AgentType
		want  bool
	}{
		// Reserved MVH consts — all must pass.
		{"claude-code", AgentTypeClaudeCode, true},
		{"pi", AgentTypePi, true},
		{"claude-twin", AgentTypeClaudeTwin, true},
		{"pi-twin", AgentTypePiTwin, true},

		// Boundary positives.
		{"min length 2 (ab)", "ab", true},
		{"max length 63", AgentType(strings.Repeat("a", 62) + "b"), true},

		// Single char — fails: {1,62} requires at least one char after the leading letter.
		{"single char (a)", "a", false},

		// Negatives.
		{"empty string", "", false},
		{"starts with digit", "1foo", false},
		{"uppercase letter", "Foo", false},
		{"underscore", "foo_bar", false},
		{"starts with hyphen", "-foo", false},
		{"64-char string (over limit)", AgentType(strings.Repeat("a", 64)), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("AgentType(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestAgentTypeReservedIdentifiers(t *testing.T) {
	t.Parallel()

	reserved := []AgentType{
		AgentTypeClaudeCode,
		AgentTypePi,
		AgentTypeClaudeTwin,
		AgentTypePiTwin,
	}
	for _, a := range reserved {
		if !a.Valid() {
			t.Errorf("reserved AgentType %q failed Valid()", string(a))
		}
	}
}

func TestAgentTypeMarshalText(t *testing.T) {
	t.Parallel()

	got, err := AgentTypeClaudeCode.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "claude-code" {
		t.Errorf("MarshalText = %q, want %q", string(got), "claude-code")
	}

	if _, err := AgentType("bogus!value").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := AgentType("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestAgentTypeUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Agent AgentType `json:"agent_type"`
	}

	tests := []struct {
		name    string
		input   string
		want    AgentType
		wantErr bool
	}{
		{
			name:  "claude-code",
			input: `{"agent_type":"claude-code"}`,
			want:  AgentTypeClaudeCode,
		},
		{
			name:  "pi",
			input: `{"agent_type":"pi"}`,
			want:  AgentTypePi,
		},
		{
			name:  "claude-twin",
			input: `{"agent_type":"claude-twin"}`,
			want:  AgentTypeClaudeTwin,
		},
		{
			name:  "pi-twin",
			input: `{"agent_type":"pi-twin"}`,
			want:  AgentTypePiTwin,
		},
		{
			name:  "valid custom identifier",
			input: `{"agent_type":"my-handler"}`,
			want:  "my-handler",
		},
		{
			name:    "empty string",
			input:   `{"agent_type":""}`,
			wantErr: true,
		},
		{
			name:    "starts with digit",
			input:   `{"agent_type":"1foo"}`,
			wantErr: true,
		},
		{
			name:    "uppercase",
			input:   `{"agent_type":"ClaudeCode"}`,
			wantErr: true,
		},
		{
			name:    "underscore",
			input:   `{"agent_type":"foo_bar"}`,
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
			if w.Agent != tc.want {
				t.Errorf("got %q, want %q", string(w.Agent), string(tc.want))
			}
		})
	}
}
