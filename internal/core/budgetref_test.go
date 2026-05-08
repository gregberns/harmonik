package core

import (
	"encoding/json"
	"testing"
)

// TestBudgetRefValid verifies that any non-empty string is valid and
// that an empty string is rejected.
func TestBudgetRefValid(t *testing.T) {
	t.Parallel()

	valid := []BudgetRef{
		"token-budget-default",
		"wall-clock-tight",
		"iterations-max",
		"a",
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	if BudgetRef("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestBudgetRefMarshalText verifies MarshalText accepts non-empty values and
// rejects the empty string.
func TestBudgetRefMarshalText(t *testing.T) {
	t.Parallel()

	got, err := BudgetRef("token-budget-default").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "token-budget-default" {
		t.Errorf("MarshalText = %q, want %q", string(got), "token-budget-default")
	}

	if _, err := BudgetRef("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestBudgetRefUnmarshalText verifies JSON round-trip behaviour.
func TestBudgetRefUnmarshalText(t *testing.T) {
	t.Parallel()

	type budgetFixtureWrapper struct {
		Ref BudgetRef `json:"budget_ref"`
	}

	tests := []struct {
		name    string
		input   string
		want    BudgetRef
		wantErr bool
	}{
		{name: "token-budget-default", input: `{"budget_ref":"token-budget-default"}`, want: "token-budget-default"},
		{name: "wall-clock-tight", input: `{"budget_ref":"wall-clock-tight"}`, want: "wall-clock-tight"},
		{name: "arbitrary non-empty", input: `{"budget_ref":"budget-x"}`, want: "budget-x"},
		{name: "empty rejected", input: `{"budget_ref":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w budgetFixtureWrapper
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
			if w.Ref != tc.want {
				t.Errorf("got %q, want %q", string(w.Ref), string(tc.want))
			}
		})
	}
}

// TestBudgetRefRoundTrip verifies a non-empty BudgetRef survives a
// json.Marshal / json.Unmarshal round-trip.
func TestBudgetRefRoundTrip(t *testing.T) {
	t.Parallel()

	budgetFixtureValue := BudgetRef("iterations-max")

	data, err := json.Marshal(budgetFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded BudgetRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != budgetFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, budgetFixtureValue)
	}
}
