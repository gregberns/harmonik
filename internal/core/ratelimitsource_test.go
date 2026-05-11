package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRateLimitSourceValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input RateLimitSource
		want  bool
	}{
		// Declared constants — all must pass.
		{"anthropic", RateLimitSourceAnthropic, true},
		{"openai", RateLimitSourceOpenAI, true},

		// Additional valid identifiers (open vocabulary).
		{"vertex-ai", "vertex-ai", true},
		{"anthropic-tier-1", "anthropic-tier-1", true},
		{"single-char (a)", "a", true},
		{"alphanumeric with hyphen", "abc123-def", true},

		// Boundary: starts with letter followed by digits.
		{"a0", "a0", true},

		// Negatives.
		{"empty string", "", false},
		{"starts with digit", "1provider", false},
		{"starts with hyphen", "-provider", false},
		{"uppercase letter", "Anthropic", false},
		{"underscore", "anthropic_api", false},
		{"space", "anthropic api", false},
		{"trailing hyphen allowed by regex", "anthropic-", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("RateLimitSource(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestRateLimitSourceKnownConstants(t *testing.T) {
	t.Parallel()

	known := []RateLimitSource{
		RateLimitSourceAnthropic,
		RateLimitSourceOpenAI,
	}
	for _, s := range known {
		if !s.Valid() {
			t.Errorf("declared constant RateLimitSource(%q) failed Valid()", string(s))
		}
	}
}

func TestRateLimitSourceMarshalText(t *testing.T) {
	t.Parallel()

	got, err := RateLimitSourceAnthropic.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "anthropic" {
		t.Errorf("MarshalText = %q, want %q", string(got), "anthropic")
	}

	if _, err := RateLimitSource("Invalid!Value").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := RateLimitSource("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestRateLimitSourceUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Source RateLimitSource `json:"source"`
	}

	tests := []struct {
		name    string
		input   string
		want    RateLimitSource
		wantErr bool
	}{
		{
			name:  "anthropic",
			input: `{"source":"anthropic"}`,
			want:  RateLimitSourceAnthropic,
		},
		{
			name:  "openai",
			input: `{"source":"openai"}`,
			want:  RateLimitSourceOpenAI,
		},
		{
			name:  "open vocabulary value vertex-ai",
			input: `{"source":"vertex-ai"}`,
			want:  "vertex-ai",
		},
		{
			name:  "open vocabulary value anthropic-tier-1",
			input: `{"source":"anthropic-tier-1"}`,
			want:  "anthropic-tier-1",
		},
		{
			name:    "empty string rejected",
			input:   `{"source":""}`,
			wantErr: true,
		},
		{
			name:    "starts with digit rejected",
			input:   `{"source":"1provider"}`,
			wantErr: true,
		},
		{
			name:    "uppercase rejected",
			input:   `{"source":"Anthropic"}`,
			wantErr: true,
		},
		{
			name:    "underscore rejected",
			input:   `{"source":"anthropic_api"}`,
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
			if w.Source != tc.want {
				t.Errorf("got %q, want %q", string(w.Source), string(tc.want))
			}
		})
	}
}

func TestRateLimitSourceRoundTrip(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Source RateLimitSource `json:"source"`
	}

	values := []RateLimitSource{
		RateLimitSourceAnthropic,
		RateLimitSourceOpenAI,
		"vertex-ai",
		"anthropic-tier-1",
	}

	for _, s := range values {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			in := wrapper{Source: s}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", s, err)
			}
			var out wrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal(%q): %v", string(data), err)
			}
			if out.Source != s {
				t.Errorf("round-trip: got %q, want %q", out.Source, s)
			}
		})
	}
}

func TestRateLimitSourceUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an invalid value must mention the regex shape.
	var s RateLimitSource
	err := s.UnmarshalText([]byte("Invalid!Value"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "^[a-z][a-z0-9-]*$") {
		t.Errorf("error message %q does not contain the regex shape", msg)
	}
}
