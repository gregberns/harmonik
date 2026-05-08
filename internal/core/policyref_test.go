package core

import (
	"encoding/json"
	"testing"
)

// TestPolicyRefValid verifies that any non-empty string is valid and that an
// empty string is rejected per control-points.md §6.4 / execution-model.md §6.1.
func TestPolicyRefValid(t *testing.T) {
	t.Parallel()

	valid := []PolicyRef{
		"policies/p001",
		"my-policy",
		"a",
		"policy-with-dashes",
		"UPPERCASE",
	}
	for _, p := range valid {
		if !p.Valid() {
			t.Errorf("expected %q to be valid", p)
		}
	}

	if PolicyRef("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestPolicyRefMarshalText verifies MarshalText accepts non-empty values and
// rejects the empty string.
func TestPolicyRefMarshalText(t *testing.T) {
	t.Parallel()

	got, err := PolicyRef("policies/p001").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "policies/p001" {
		t.Errorf("MarshalText = %q, want %q", string(got), "policies/p001")
	}

	if _, err := PolicyRef("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestPolicyRefUnmarshalText verifies JSON round-trip behaviour and that an
// empty value is rejected.
func TestPolicyRefUnmarshalText(t *testing.T) {
	t.Parallel()

	type policyrefFixtureWrapper struct {
		Ref PolicyRef `json:"policy_ref"`
	}

	tests := []struct {
		name    string
		input   string
		want    PolicyRef
		wantErr bool
	}{
		{name: "named policy", input: `{"policy_ref":"policies/p001"}`, want: "policies/p001"},
		{name: "simple name", input: `{"policy_ref":"my-policy"}`, want: "my-policy"},
		{name: "arbitrary non-empty", input: `{"policy_ref":"x"}`, want: "x"},
		{name: "empty rejected", input: `{"policy_ref":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w policyrefFixtureWrapper
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

// TestPolicyRefRoundTrip verifies a non-empty PolicyRef survives a
// json.Marshal / json.Unmarshal round-trip.
func TestPolicyRefRoundTrip(t *testing.T) {
	t.Parallel()

	policyrefFixtureValue := PolicyRef("policies/base-policy")

	data, err := json.Marshal(policyrefFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded PolicyRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != policyrefFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, policyrefFixtureValue)
	}
}

// TestPolicyRefSliceRoundTrip verifies a []PolicyRef (Workflow.policies list)
// survives a json.Marshal / json.Unmarshal round-trip.
func TestPolicyRefSliceRoundTrip(t *testing.T) {
	t.Parallel()

	policyrefFixtureSlice := []PolicyRef{"policy-a", "policy-b", "policy-c"}

	data, err := json.Marshal(policyrefFixtureSlice)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded []PolicyRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(decoded) != len(policyrefFixtureSlice) {
		t.Fatalf("length: got %d, want %d", len(decoded), len(policyrefFixtureSlice))
	}
	for i, want := range policyrefFixtureSlice {
		if decoded[i] != want {
			t.Errorf("[%d]: got %q, want %q", i, decoded[i], want)
		}
	}
}
