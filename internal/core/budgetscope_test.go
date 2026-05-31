package core

import (
	"encoding/json"
	"testing"
)

func TestBudgetScopeValid(t *testing.T) {
	t.Parallel()

	valid := []BudgetScope{
		BudgetScopePerRole,
		BudgetScopePerRun,
		BudgetScopePerState,
		BudgetScopeHandlerAccount,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []BudgetScope{
		"",
		"per-role",
		"PerRole",
		"PER_ROLE",
		"per-run",
		"PerRun",
		"PER_RUN",
		"per-state",
		"PerState",
		"PER_STATE",
		"unknown",
		"per_role|per_run",
	}
	for _, s := range invalid {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestBudgetScopeMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope BudgetScope
		want  string
	}{
		{BudgetScopePerRole, "per_role"},
		{BudgetScopePerRun, "per_run"},
		{BudgetScopePerState, "per_state"},
		{BudgetScopeHandlerAccount, "handler_account"},
	}
	for _, tc := range tests {
		got, err := tc.scope.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.scope, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.scope, string(got), tc.want)
		}
	}

	if _, err := BudgetScope("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := BudgetScope("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestBudgetScopeUnmarshalText(t *testing.T) {
	t.Parallel()

	type budgetScopeFixtureWrapper struct {
		Scope BudgetScope `json:"scope"`
	}

	tests := []struct {
		name    string
		input   string
		want    BudgetScope
		wantErr bool
	}{
		{
			name:  "per_role",
			input: `{"scope":"per_role"}`,
			want:  BudgetScopePerRole,
		},
		{
			name:  "per_run",
			input: `{"scope":"per_run"}`,
			want:  BudgetScopePerRun,
		},
		{
			name:  "per_state",
			input: `{"scope":"per_state"}`,
			want:  BudgetScopePerState,
		},
		{
			name:  "handler_account",
			input: `{"scope":"handler_account"}`,
			want:  BudgetScopeHandlerAccount,
		},
		{
			name:    "hyphenated per-role rejected",
			input:   `{"scope":"per-role"}`,
			wantErr: true,
		},
		{
			name:    "PascalCase PerRole rejected",
			input:   `{"scope":"PerRole"}`,
			wantErr: true,
		},
		{
			name:    "uppercase PER_ROLE rejected",
			input:   `{"scope":"PER_ROLE"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"scope":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"scope":""}`,
			wantErr: true,
		},
		{
			name:    "partial match rejected",
			input:   `{"scope":"per"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w budgetScopeFixtureWrapper
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
			if w.Scope != tc.want {
				t.Errorf("got %q, want %q", string(w.Scope), string(tc.want))
			}
		})
	}
}
