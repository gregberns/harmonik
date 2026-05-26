package core

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-031 / CP-052: Every mvh-required role MUST include "beads-cli" in
// default_skills.
// specs/control-points.md §4.6.CP-031, §4.11.CP-052
// ---------------------------------------------------------------------------

// mvhRoleWithBeadsCLIYAML returns a policy with one mvh-required role that
// correctly includes "beads-cli" in default_skills.
func mvhRoleWithBeadsCLIYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp031-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: builder
    permission_schema:
      allowed_tools: [bash]
      writable_paths: ["workspace/**"]
      readable_paths: ["**"]
      default_skills: [beads-cli]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// mvhRoleWithExtraSkillsYAML returns a policy where the mvh-required role
// has additional skills beyond "beads-cli" — still valid.
func mvhRoleWithExtraSkillsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp031-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: planner
    permission_schema:
      allowed_tools: [bash, read]
      writable_paths: []
      readable_paths: ["**"]
      default_skills: [agent-reviewer, beads-cli, session-resume]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// mvhRoleMissingBeadsCLIYAML returns a policy where the mvh-required role
// omits "beads-cli" from default_skills — a CP-031 violation.
func mvhRoleMissingBeadsCLIYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp031-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: reviewer
    permission_schema:
      allowed_tools: [read]
      writable_paths: []
      readable_paths: ["**"]
      default_skills: [agent-reviewer]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// mvhRoleEmptyDefaultSkillsYAML returns a policy where the mvh-required role
// has an empty default_skills list — a CP-031 violation.
func mvhRoleEmptyDefaultSkillsYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp031-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: builder
    permission_schema:
      allowed_tools: [bash]
      writable_paths: ["workspace/**"]
      readable_paths: ["**"]
      default_skills: []
      allowed_hooks: []
      invocable_by: []
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// deferredRoleWithoutBeadsCLIYAML returns a policy with a declared-but-deferred
// role that has an empty default_skills — exempt from CP-031.
func deferredRoleWithoutBeadsCLIYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp031-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: researcher
    permission_schema:
      allowed_tools: []
      writable_paths: []
      readable_paths: ["**"]
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

// multiMVHRoleSecondMissingYAML has two mvh-required roles where only the
// second omits "beads-cli".
func multiMVHRoleSecondMissingYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp031-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: planner
    permission_schema:
      allowed_tools: [read]
      writable_paths: []
      readable_paths: ["**"]
      default_skills: [beads-cli]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required
  - name: builder
    permission_schema:
      allowed_tools: [bash]
      writable_paths: ["workspace/**"]
      readable_paths: ["**"]
      default_skills: [session-resume]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// TestValidateMVHRoleDefaultSkills_Present verifies that a role with
// "beads-cli" in default_skills passes CP-031 validation.
func TestValidateMVHRoleDefaultSkills_Present(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(mvhRoleWithBeadsCLIYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateMVHRoleDefaultSkills(); err != nil {
		t.Errorf("ValidateMVHRoleDefaultSkills() = %v, want nil", err)
	}
}

// TestValidateMVHRoleDefaultSkills_ExtraSkillsOK verifies that extra skills
// alongside "beads-cli" do not trigger CP-031.
func TestValidateMVHRoleDefaultSkills_ExtraSkillsOK(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(mvhRoleWithExtraSkillsYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateMVHRoleDefaultSkills(); err != nil {
		t.Errorf("ValidateMVHRoleDefaultSkills() = %v, want nil (extra skills alongside beads-cli are valid)", err)
	}
}

// TestValidateMVHRoleDefaultSkills_Missing verifies that an mvh-required role
// without "beads-cli" returns ErrMissingBeadsCLISkill (CP-031).
func TestValidateMVHRoleDefaultSkills_Missing(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(mvhRoleMissingBeadsCLIYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateMVHRoleDefaultSkills()
	if err == nil {
		t.Fatal("ValidateMVHRoleDefaultSkills() = nil, want ErrMissingBeadsCLISkill")
	}
	if !errors.Is(err, ErrMissingBeadsCLISkill) {
		t.Errorf("ValidateMVHRoleDefaultSkills() error = %v, want errors.Is(ErrMissingBeadsCLISkill)", err)
	}
}

// TestValidateMVHRoleDefaultSkills_EmptyDefaultSkills verifies that an
// mvh-required role with an empty default_skills list triggers CP-031.
func TestValidateMVHRoleDefaultSkills_EmptyDefaultSkills(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(mvhRoleEmptyDefaultSkillsYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateMVHRoleDefaultSkills()
	if err == nil {
		t.Fatal("ValidateMVHRoleDefaultSkills() = nil, want ErrMissingBeadsCLISkill for empty default_skills")
	}
	if !errors.Is(err, ErrMissingBeadsCLISkill) {
		t.Errorf("ValidateMVHRoleDefaultSkills() error = %v, want errors.Is(ErrMissingBeadsCLISkill)", err)
	}
}

// TestValidateMVHRoleDefaultSkills_ErrorNamesRole verifies that the error
// message includes the role name for operator diagnosis.
func TestValidateMVHRoleDefaultSkills_ErrorNamesRole(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(mvhRoleMissingBeadsCLIYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateMVHRoleDefaultSkills()
	if err == nil {
		t.Fatal("ValidateMVHRoleDefaultSkills() = nil, want error")
	}
	if !strContains(err.Error(), "reviewer") {
		t.Errorf("ValidateMVHRoleDefaultSkills() error = %q, want \"reviewer\" in message", err.Error())
	}
}

// TestValidateMVHRoleDefaultSkills_DeferredRoleExempt verifies that
// declared-but-deferred roles are exempt from CP-031 (they carry empty shells
// per CP-030).
func TestValidateMVHRoleDefaultSkills_DeferredRoleExempt(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(deferredRoleWithoutBeadsCLIYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateMVHRoleDefaultSkills(); err != nil {
		t.Errorf("ValidateMVHRoleDefaultSkills() = %v, want nil (deferred roles exempt from CP-031)", err)
	}
}

// TestValidateMVHRoleDefaultSkills_MultiRoleSecondMissing verifies that the
// validator catches a CP-031 violation in the second role when the first passes.
func TestValidateMVHRoleDefaultSkills_MultiRoleSecondMissing(t *testing.T) {
	t.Parallel()

	doc, err := ParsePolicyDocument(multiMVHRoleSecondMissingYAML(t))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateMVHRoleDefaultSkills()
	if err == nil {
		t.Fatal("ValidateMVHRoleDefaultSkills() = nil, want ErrMissingBeadsCLISkill for second role")
	}
	if !errors.Is(err, ErrMissingBeadsCLISkill) {
		t.Errorf("ValidateMVHRoleDefaultSkills() error = %v, want errors.Is(ErrMissingBeadsCLISkill)", err)
	}
	if !strContains(err.Error(), "builder") {
		t.Errorf("ValidateMVHRoleDefaultSkills() error = %q, want \"builder\" in message", err.Error())
	}
}

// TestValidateMVHRoleDefaultSkills_EmptyRolesList verifies that an empty roles
// list passes (nothing to violate CP-031).
func TestValidateMVHRoleDefaultSkills_EmptyRolesList(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp031-policy
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
	if err := doc.ValidateMVHRoleDefaultSkills(); err != nil {
		t.Errorf("ValidateMVHRoleDefaultSkills() on empty roles = %v, want nil", err)
	}
}
