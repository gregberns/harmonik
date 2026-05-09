package core

import (
	"encoding/json"
	"testing"
)

func TestBudgetResourceValid(t *testing.T) {
	t.Parallel()

	valid := []BudgetResource{
		BudgetResourceTokens,
		BudgetResourceWallClockSeconds,
		BudgetResourceIterations,
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	invalid := []BudgetResource{
		"",
		"Tokens",
		"TOKENS",
		"token",
		"Wall_Clock_Seconds",
		"WALL_CLOCK_SECONDS",
		"wall-clock-seconds",
		"wallclock",
		"Iterations",
		"ITERATIONS",
		"iteration",
		"unknown",
		"tokens|wall_clock_seconds",
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected %q to be invalid", r)
		}
	}
}

func TestBudgetResourceMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		resource BudgetResource
		want     string
	}{
		{BudgetResourceTokens, "tokens"},
		{BudgetResourceWallClockSeconds, "wall_clock_seconds"},
		{BudgetResourceIterations, "iterations"},
	}
	for _, tc := range tests {
		got, err := tc.resource.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.resource, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.resource, string(got), tc.want)
		}
	}

	if _, err := BudgetResource("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := BudgetResource("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestBudgetResourceUnmarshalText(t *testing.T) {
	t.Parallel()

	type budgetResourceFixtureWrapper struct {
		Resource BudgetResource `json:"resource"`
	}

	tests := []struct {
		name    string
		input   string
		want    BudgetResource
		wantErr bool
	}{
		{
			name:  "tokens",
			input: `{"resource":"tokens"}`,
			want:  BudgetResourceTokens,
		},
		{
			name:  "wall_clock_seconds",
			input: `{"resource":"wall_clock_seconds"}`,
			want:  BudgetResourceWallClockSeconds,
		},
		{
			name:  "iterations",
			input: `{"resource":"iterations"}`,
			want:  BudgetResourceIterations,
		},
		{
			name:    "mixed-case Tokens rejected",
			input:   `{"resource":"Tokens"}`,
			wantErr: true,
		},
		{
			name:    "uppercase TOKENS rejected",
			input:   `{"resource":"TOKENS"}`,
			wantErr: true,
		},
		{
			name:    "hyphenated wall-clock-seconds rejected",
			input:   `{"resource":"wall-clock-seconds"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"resource":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"resource":""}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"resource":"token"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w budgetResourceFixtureWrapper
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
			if w.Resource != tc.want {
				t.Errorf("got %q, want %q", string(w.Resource), string(tc.want))
			}
		})
	}
}
