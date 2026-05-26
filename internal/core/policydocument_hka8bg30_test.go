package core

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-030: Declared-but-deferred roles carry empty shells
// specs/control-points.md §4.6.CP-030
// ---------------------------------------------------------------------------

// deferredRoleEmptyShellYAML returns a valid policy YAML containing one
// declared-but-deferred role with an empty permission shell (CP-030 compliant).
func deferredRoleEmptyShellYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp030-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: Researcher
    permission_schema:
      allowed_tools: []
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// deferredRoleNonEmptyAllowedToolsYAML returns a policy YAML with a
// declared-but-deferred role that has non-empty allowed_tools — a CP-030 violation.
func deferredRoleNonEmptyAllowedToolsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp030-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: Verifier
    permission_schema:
      allowed_tools: [bash]
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// deferredRoleNonEmptyWritablePathsYAML returns a policy YAML with a
// declared-but-deferred role that has non-empty writable_paths — a CP-030 violation.
func deferredRoleNonEmptyWritablePathsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp030-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: Scheduler
    permission_schema:
      allowed_tools: []
      writable_paths: ["**"]
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// deferredRoleNonEmptyDefaultSkillsYAML returns a policy YAML with a
// declared-but-deferred role that has non-empty default_skills — a CP-030 violation.
func deferredRoleNonEmptyDefaultSkillsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp030-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: Governor
    permission_schema:
      allowed_tools: []
      writable_paths: []
      default_skills: [beads-cli]
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// mixedRolesSecondDeferredViolationYAML returns a policy with an mvh-required
// role followed by a declared-but-deferred role that violates CP-030.
func mixedRolesSecondDeferredViolationYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp030-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: orchestrator
    permission_schema:
      allowed_tools: [bash]
      writable_paths: ["**"]
      readable_paths: ["**"]
      default_skills: [beads-cli]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required
  - name: Researcher
    permission_schema:
      allowed_tools: [bash]
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// TestValidateDeferredRoleShells_EmptyShellPasses verifies that a
// declared-but-deferred role with empty allowed_tools, writable_paths, and
// default_skills passes ValidateDeferredRoleShells (CP-030).
func TestValidateDeferredRoleShells_EmptyShellPasses(t *testing.T) {
	t.Parallel()

	data := deferredRoleEmptyShellYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateDeferredRoleShells(); err != nil {
		t.Errorf("ValidateDeferredRoleShells() = %v, want nil (empty shell compliant)", err)
	}
}

// TestValidateDeferredRoleShells_NonEmptyAllowedTools verifies that a
// declared-but-deferred role with non-empty allowed_tools triggers
// ErrNonEmptyDeferredRoleShell (CP-030).
func TestValidateDeferredRoleShells_NonEmptyAllowedTools(t *testing.T) {
	t.Parallel()

	data := deferredRoleNonEmptyAllowedToolsYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateDeferredRoleShells()
	if err == nil {
		t.Fatal("ValidateDeferredRoleShells() = nil, want ErrNonEmptyDeferredRoleShell for non-empty allowed_tools")
	}
	if !errors.Is(err, ErrNonEmptyDeferredRoleShell) {
		t.Errorf("ValidateDeferredRoleShells() error = %v, want errors.Is(ErrNonEmptyDeferredRoleShell)", err)
	}
}

// TestValidateDeferredRoleShells_NonEmptyWritablePaths verifies that a
// declared-but-deferred role with non-empty writable_paths triggers
// ErrNonEmptyDeferredRoleShell (CP-030).
func TestValidateDeferredRoleShells_NonEmptyWritablePaths(t *testing.T) {
	t.Parallel()

	data := deferredRoleNonEmptyWritablePathsYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateDeferredRoleShells()
	if err == nil {
		t.Fatal("ValidateDeferredRoleShells() = nil, want ErrNonEmptyDeferredRoleShell for non-empty writable_paths")
	}
	if !errors.Is(err, ErrNonEmptyDeferredRoleShell) {
		t.Errorf("ValidateDeferredRoleShells() error = %v, want errors.Is(ErrNonEmptyDeferredRoleShell)", err)
	}
}

// TestValidateDeferredRoleShells_NonEmptyDefaultSkills verifies that a
// declared-but-deferred role with non-empty default_skills triggers
// ErrNonEmptyDeferredRoleShell (CP-030).
func TestValidateDeferredRoleShells_NonEmptyDefaultSkills(t *testing.T) {
	t.Parallel()

	data := deferredRoleNonEmptyDefaultSkillsYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateDeferredRoleShells()
	if err == nil {
		t.Fatal("ValidateDeferredRoleShells() = nil, want ErrNonEmptyDeferredRoleShell for non-empty default_skills")
	}
	if !errors.Is(err, ErrNonEmptyDeferredRoleShell) {
		t.Errorf("ValidateDeferredRoleShells() error = %v, want errors.Is(ErrNonEmptyDeferredRoleShell)", err)
	}
}

// TestValidateDeferredRoleShells_MVHRequiredRoleIgnored verifies that
// ValidateDeferredRoleShells does not reject mvh-required roles with non-empty
// fields — the empty-shell rule applies only to declared-but-deferred roles (CP-030).
func TestValidateDeferredRoleShells_MVHRequiredRoleIgnored(t *testing.T) {
	t.Parallel()

	data := roleWithPermissionSchemaYAML(t) // mvh-required role with non-empty fields
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateDeferredRoleShells(); err != nil {
		t.Errorf("ValidateDeferredRoleShells() = %v, want nil (mvh-required roles not subject to CP-030)", err)
	}
}

// TestValidateDeferredRoleShells_MixedRolesSecondViolates verifies that
// ValidateDeferredRoleShells catches a CP-030 violation in the second role
// when the first is mvh-required and valid.
func TestValidateDeferredRoleShells_MixedRolesSecondViolates(t *testing.T) {
	t.Parallel()

	data := mixedRolesSecondDeferredViolationYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateDeferredRoleShells()
	if err == nil {
		t.Fatal("ValidateDeferredRoleShells() = nil, want ErrNonEmptyDeferredRoleShell for second role violation")
	}
	if !errors.Is(err, ErrNonEmptyDeferredRoleShell) {
		t.Errorf("ValidateDeferredRoleShells() error = %v, want errors.Is(ErrNonEmptyDeferredRoleShell)", err)
	}
	if !strContains(err.Error(), "Researcher") {
		t.Errorf("ValidateDeferredRoleShells() error = %q, want \"Researcher\" in message", err.Error())
	}
}

// TestValidateDeferredRoleShells_AllFourDeferredRolesEmpty verifies that all
// four declared-but-deferred roles (Researcher, Verifier, Scheduler, Governor)
// with empty shells pass ValidateDeferredRoleShells (CP-030).
func TestValidateDeferredRoleShells_AllFourDeferredRolesEmpty(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp030-all-deferred
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: Researcher
    permission_schema:
      allowed_tools: []
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred
  - name: Verifier
    permission_schema:
      allowed_tools: []
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred
  - name: Scheduler
    permission_schema:
      allowed_tools: []
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred
  - name: Governor
    permission_schema:
      allowed_tools: []
      writable_paths: []
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: declared-but-deferred

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
	if err := doc.ValidateDeferredRoleShells(); err != nil {
		t.Errorf("ValidateDeferredRoleShells() = %v, want nil (all four deferred roles have empty shells)", err)
	}
}

// TestValidateDeferredRoleShells_ErrorNamesViolatingRole verifies that the
// error message includes the name of the violating role, enabling operators
// to identify which role violates CP-030.
func TestValidateDeferredRoleShells_ErrorNamesViolatingRole(t *testing.T) {
	t.Parallel()

	data := deferredRoleNonEmptyAllowedToolsYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateDeferredRoleShells()
	if err == nil {
		t.Fatal("ValidateDeferredRoleShells() = nil, want error")
	}
	if !strContains(err.Error(), "Verifier") {
		t.Errorf("ValidateDeferredRoleShells() error = %q, want \"Verifier\" in message", err.Error())
	}
}
