package core

import (
	"encoding/json"
	"testing"
)

// TestPolicyVersionValid verifies that any non-empty string is valid and that
// an empty string is rejected.
func TestPolicyVersionValid(t *testing.T) {
	t.Parallel()

	valid := []PolicyVersion{
		"v1.0.0",
		"v2.3.4-rc.1",
		"policy-20260501",
		"abc",
		"1",
	}
	for _, v := range valid {
		if !v.Valid() {
			t.Errorf("expected %q to be valid", v)
		}
	}

	if PolicyVersion("").Valid() {
		t.Error("expected empty string to be invalid")
	}
}

// TestPolicyVersionMarshalText verifies MarshalText accepts non-empty values and
// rejects the empty string.
func TestPolicyVersionMarshalText(t *testing.T) {
	t.Parallel()

	got, err := PolicyVersion("v1.0.0").MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "v1.0.0" {
		t.Errorf("MarshalText = %q, want %q", string(got), "v1.0.0")
	}

	if _, err := PolicyVersion("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestPolicyVersionUnmarshalText verifies JSON round-trip behaviour.
func TestPolicyVersionUnmarshalText(t *testing.T) {
	t.Parallel()

	type policyVerFixtureWrapper struct {
		Version PolicyVersion `json:"policy_version"`
	}

	tests := []struct {
		name    string
		input   string
		want    PolicyVersion
		wantErr bool
	}{
		{name: "semver", input: `{"policy_version":"v1.0.0"}`, want: "v1.0.0"},
		{name: "dated", input: `{"policy_version":"policy-20260501"}`, want: "policy-20260501"},
		{name: "arbitrary non-empty", input: `{"policy_version":"xyz"}`, want: "xyz"},
		{name: "empty rejected", input: `{"policy_version":""}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w policyVerFixtureWrapper
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
			if w.Version != tc.want {
				t.Errorf("got %q, want %q", string(w.Version), string(tc.want))
			}
		})
	}
}

// TestPolicyVersionRoundTrip verifies a non-empty PolicyVersion survives
// a json.Marshal / json.Unmarshal round-trip.
func TestPolicyVersionRoundTrip(t *testing.T) {
	t.Parallel()

	policyVerFixtureValue := PolicyVersion("v1.2.3-rc.1")

	data, err := json.Marshal(policyVerFixtureValue)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded PolicyVersion
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded != policyVerFixtureValue {
		t.Errorf("round-trip: got %q, want %q", decoded, policyVerFixtureValue)
	}
}

// TestTraceValid_EmptyPolicyVersionRejectedByValid verifies that a Trace with
// an empty PolicyVersion fails Valid().
func TestTraceValid_EmptyPolicyVersionRejectedByValid(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.PolicyVersion = PolicyVersion("")
	if tr.Valid() {
		t.Error("Valid() = true with empty PolicyVersion, want false")
	}
}
