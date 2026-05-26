package core

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-028: Every role MUST carry a permission_schema
// specs/control-points.md §4.6.CP-028
// ---------------------------------------------------------------------------

// roleWithPermissionSchemaYAML returns a minimal valid policy YAML with one
// role that has a permission_schema block present.
func roleWithPermissionSchemaYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp028-policy
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

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// roleWithoutPermissionSchemaYAML returns a policy YAML with a role that
// omits the permission_schema key entirely — a CP-028 violation.
func roleWithoutPermissionSchemaYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp028-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: orchestrator
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// multiRoleOneAbsentYAML returns a policy with two roles where the second
// omits permission_schema, to test that ValidateRoles catches non-first violations.
func multiRoleOneAbsentYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: cp028-policy
  version: "0.1.0"
  author: test
  schema_version: 2

roles:
  - name: planner
    permission_schema:
      allowed_tools: [bash]
      writable_paths: []
      readable_paths: ["**"]
      default_skills: [beads-cli]
      allowed_hooks: []
      invocable_by: []
    status: mvh-required
  - name: reviewer
    status: mvh-required

freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
}

// TestValidateRoles_Present verifies that ValidateRoles passes when every role
// has a permission_schema block in the YAML (CP-028).
func TestValidateRoles_Present(t *testing.T) {
	t.Parallel()

	data := roleWithPermissionSchemaYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateRoles(); err != nil {
		t.Errorf("ValidateRoles() = %v, want nil (permission_schema present)", err)
	}
}

// TestValidateRoles_Absent verifies that ValidateRoles returns
// ErrMissingPermissionSchema when a role omits permission_schema (CP-028).
func TestValidateRoles_Absent(t *testing.T) {
	t.Parallel()

	data := roleWithoutPermissionSchemaYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateRoles()
	if err == nil {
		t.Fatal("ValidateRoles() = nil, want ErrMissingPermissionSchema for absent permission_schema")
	}
	if !errors.Is(err, ErrMissingPermissionSchema) {
		t.Errorf("ValidateRoles() error = %v, want errors.Is(ErrMissingPermissionSchema)", err)
	}
}

// TestValidateRoles_ErrorNamesRole verifies that the error message includes the
// role name, enabling operators to identify which role violates CP-028.
func TestValidateRoles_ErrorNamesRole(t *testing.T) {
	t.Parallel()

	data := roleWithoutPermissionSchemaYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateRoles()
	if err == nil {
		t.Fatal("ValidateRoles() = nil, want error")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("ValidateRoles() error message is empty; want role name in message")
	}
	// The role name "orchestrator" must appear in the error.
	if !strContains(msg, "orchestrator") {
		t.Errorf("ValidateRoles() error = %q, want \"orchestrator\" in message", msg)
	}
}

// TestValidateRoles_MultiRoleSecondAbsent verifies that ValidateRoles catches
// a missing permission_schema in the second role, not just the first.
func TestValidateRoles_MultiRoleSecondAbsent(t *testing.T) {
	t.Parallel()

	data := multiRoleOneAbsentYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	err = doc.ValidateRoles()
	if err == nil {
		t.Fatal("ValidateRoles() = nil, want ErrMissingPermissionSchema for second role")
	}
	if !errors.Is(err, ErrMissingPermissionSchema) {
		t.Errorf("ValidateRoles() error = %v, want errors.Is(ErrMissingPermissionSchema)", err)
	}
	if !strContains(err.Error(), "reviewer") {
		t.Errorf("ValidateRoles() error = %q, want \"reviewer\" in message", err.Error())
	}
}

// TestValidateRoles_EmptyRolesList verifies that ValidateRoles returns nil when
// the roles list is empty (no roles to violate CP-028).
func TestValidateRoles_EmptyRolesList(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: cp028-policy
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
	if err := doc.ValidateRoles(); err != nil {
		t.Errorf("ValidateRoles() on empty roles = %v, want nil", err)
	}
}

// TestPolicyRole_PermissionSchemaPointerNilWhenAbsent verifies that parsing a
// role without a permission_schema key yields a nil PermissionSchema pointer,
// which is the CP-028 detection mechanism.
func TestPolicyRole_PermissionSchemaPointerNilWhenAbsent(t *testing.T) {
	t.Parallel()

	data := roleWithoutPermissionSchemaYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if len(doc.Roles) == 0 {
		t.Fatal("doc.Roles is empty")
	}
	if doc.Roles[0].PermissionSchema != nil {
		t.Errorf("PolicyRole.PermissionSchema = %+v, want nil when key absent", doc.Roles[0].PermissionSchema)
	}
}

// TestPolicyRole_PermissionSchemaPointerNonNilWhenPresent verifies that parsing
// a role with a permission_schema key yields a non-nil PermissionSchema pointer.
func TestPolicyRole_PermissionSchemaPointerNonNilWhenPresent(t *testing.T) {
	t.Parallel()

	data := roleWithPermissionSchemaYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if len(doc.Roles) == 0 {
		t.Fatal("doc.Roles is empty")
	}
	if doc.Roles[0].PermissionSchema == nil {
		t.Error("PolicyRole.PermissionSchema is nil, want non-nil when permission_schema key present")
	}
}

// strContains reports whether substr appears in s.
func strContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
