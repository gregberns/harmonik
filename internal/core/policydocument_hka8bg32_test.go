package core

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-032: Freedom profile is a per-state constraint bundle
// specs/control-points.md §4.6.CP-032
// ---------------------------------------------------------------------------

// freedomProfileValidYAML returns a policy YAML containing one freedom profile
// with all required fields set to valid values.
func freedomProfileValidYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp032-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles: []

freedom_profiles:
  - name: standard-agent
    tool_whitelist: [bash, read]
    writable_paths: ["output/**"]
    max_iterations: 10

gates: []
hooks: []
guards: []
budgets: []
`)
}

// freedomProfileEmptyNameYAML returns a policy YAML with a freedom profile
// whose name is empty — a CP-032 violation.
func freedomProfileEmptyNameYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp032-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles: []

freedom_profiles:
  - name: ""
    tool_whitelist: []
    writable_paths: []
    max_iterations: 5

gates: []
hooks: []
guards: []
budgets: []
`)
}

// freedomProfileZeroMaxIterationsYAML returns a policy YAML with a freedom
// profile whose max_iterations is 0 — a CP-032 violation.
func freedomProfileZeroMaxIterationsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp032-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles: []

freedom_profiles:
  - name: restricted-agent
    tool_whitelist: []
    writable_paths: []
    max_iterations: 0

gates: []
hooks: []
guards: []
budgets: []
`)
}

// freedomProfileNegativeMaxIterationsYAML returns a policy YAML with a freedom
// profile whose max_iterations is negative — a CP-032 violation.
func freedomProfileNegativeMaxIterationsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp032-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles: []

freedom_profiles:
  - name: bad-agent
    tool_whitelist: []
    writable_paths: []
    max_iterations: -1

gates: []
hooks: []
guards: []
budgets: []
`)
}

// TestValidateFreedomProfiles_ValidProfile verifies that a correctly-formed
// freedom profile passes ValidateFreedomProfiles (CP-032).
func TestValidateFreedomProfiles_ValidProfile(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(freedomProfileValidYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateFreedomProfiles(); err != nil {
		t.Errorf("ValidateFreedomProfiles() = %v, want nil (valid profile)", err)
	}
}

// TestValidateFreedomProfiles_EmptyProfilesList verifies that an empty
// freedom_profiles list passes ValidateFreedomProfiles (nothing to violate).
func TestValidateFreedomProfiles_EmptyProfilesList(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp032-empty
  version: "0.1.0"
  author: test
  schema_version: 2
roles: []
freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateFreedomProfiles(); err != nil {
		t.Errorf("ValidateFreedomProfiles() on empty list = %v, want nil", err)
	}
}

// TestValidateFreedomProfiles_EmptyName verifies that a freedom profile with
// an empty name triggers ErrFreedomProfileEmptyName (CP-032).
func TestValidateFreedomProfiles_EmptyName(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(freedomProfileEmptyNameYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateFreedomProfiles()
	if err == nil {
		t.Fatal("ValidateFreedomProfiles() = nil, want ErrFreedomProfileEmptyName for empty name")
	}
	if !errors.Is(err, ErrFreedomProfileEmptyName) {
		t.Errorf("ValidateFreedomProfiles() error = %v, want errors.Is(ErrFreedomProfileEmptyName)", err)
	}
}

// TestValidateFreedomProfiles_ZeroMaxIterations verifies that max_iterations=0
// triggers ErrFreedomProfileInvalidMaxIterations (CP-032).
func TestValidateFreedomProfiles_ZeroMaxIterations(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(freedomProfileZeroMaxIterationsYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateFreedomProfiles()
	if err == nil {
		t.Fatal("ValidateFreedomProfiles() = nil, want ErrFreedomProfileInvalidMaxIterations for max_iterations=0")
	}
	if !errors.Is(err, ErrFreedomProfileInvalidMaxIterations) {
		t.Errorf("ValidateFreedomProfiles() error = %v, want errors.Is(ErrFreedomProfileInvalidMaxIterations)", err)
	}
}

// TestValidateFreedomProfiles_NegativeMaxIterations verifies that a negative
// max_iterations triggers ErrFreedomProfileInvalidMaxIterations (CP-032).
func TestValidateFreedomProfiles_NegativeMaxIterations(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(freedomProfileNegativeMaxIterationsYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateFreedomProfiles()
	if err == nil {
		t.Fatal("ValidateFreedomProfiles() = nil, want ErrFreedomProfileInvalidMaxIterations for negative max_iterations")
	}
	if !errors.Is(err, ErrFreedomProfileInvalidMaxIterations) {
		t.Errorf("ValidateFreedomProfiles() error = %v, want errors.Is(ErrFreedomProfileInvalidMaxIterations)", err)
	}
}

// TestValidateFreedomProfiles_MaxIterationsOne verifies that max_iterations=1
// is the minimum valid value (boundary).
func TestValidateFreedomProfiles_MaxIterationsOne(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp032-min
  version: "0.1.0"
  author: test
  schema_version: 2
roles: []
freedom_profiles:
  - name: minimal-agent
    tool_whitelist: []
    writable_paths: []
    max_iterations: 1
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateFreedomProfiles(); err != nil {
		t.Errorf("ValidateFreedomProfiles() with max_iterations=1 = %v, want nil (minimum valid value)", err)
	}
}

// TestValidateFreedomProfiles_ErrorNamesProfile verifies that the error message
// includes the profile name, enabling operators to identify the violation.
func TestValidateFreedomProfiles_ErrorNamesProfile(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(freedomProfileZeroMaxIterationsYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateFreedomProfiles()
	if err == nil {
		t.Fatal("ValidateFreedomProfiles() = nil, want error")
	}
	if !strContains(err.Error(), "restricted-agent") {
		t.Errorf("ValidateFreedomProfiles() error = %q, want \"restricted-agent\" in message", err.Error())
	}
}

// TestValidateFreedomProfiles_MultiProfileSecondViolates verifies that the
// validator catches a CP-032 violation in the second profile when the first
// is valid.
func TestValidateFreedomProfiles_MultiProfileSecondViolates(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp032-multi
  version: "0.1.0"
  author: test
  schema_version: 2
roles: []
freedom_profiles:
  - name: first-agent
    tool_whitelist: [bash]
    writable_paths: []
    max_iterations: 5
  - name: second-agent
    tool_whitelist: []
    writable_paths: []
    max_iterations: 0
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateFreedomProfiles()
	if err == nil {
		t.Fatal("ValidateFreedomProfiles() = nil, want ErrFreedomProfileInvalidMaxIterations for second profile")
	}
	if !errors.Is(err, ErrFreedomProfileInvalidMaxIterations) {
		t.Errorf("ValidateFreedomProfiles() error = %v, want errors.Is(ErrFreedomProfileInvalidMaxIterations)", err)
	}
	if !strContains(err.Error(), "second-agent") {
		t.Errorf("ValidateFreedomProfiles() error = %q, want \"second-agent\" in message", err.Error())
	}
}

// TestValidateFreedomProfiles_OptionalFieldsAbsent verifies that a freedom
// profile with only required fields (no model_tier, no budget refs) is valid
// per CP-032 (optional fields must be absent-ok).
func TestValidateFreedomProfiles_OptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp032-optional
  version: "0.1.0"
  author: test
  schema_version: 2
roles: []
freedom_profiles:
  - name: required-only
    tool_whitelist: [bash]
    writable_paths: ["workspace/**"]
    max_iterations: 20
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateFreedomProfiles(); err != nil {
		t.Errorf("ValidateFreedomProfiles() with optional fields absent = %v, want nil", err)
	}
}

// TestValidateFreedomProfiles_WithBudgetRefs verifies that a freedom profile
// with optional token_budget_ref and wall_clock_budget_ref is accepted (CP-032:
// these fields are String | None).
func TestValidateFreedomProfiles_WithBudgetRefs(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp032-budgets
  version: "0.1.0"
  author: test
  schema_version: 2
roles: []
freedom_profiles:
  - name: budgeted-agent
    tool_whitelist: [bash]
    writable_paths: []
    token_budget_ref: token-budget
    wall_clock_budget_ref: wall-budget
    max_iterations: 50
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateFreedomProfiles(); err != nil {
		t.Errorf("ValidateFreedomProfiles() with budget refs = %v, want nil", err)
	}
}
