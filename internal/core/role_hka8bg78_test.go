package core

import (
	"encoding/json"
	"errors"
	"testing"
)

// roleFixture returns a fully-populated Role with all fields set to non-zero
// values, suitable for structural tests (hk-a8bg.78).
func roleFixture(t *testing.T) Role {
	t.Helper()

	r := NewRole("orchestrator", RoleStatusMVHRequired)
	r.PermissionSchema = permissionSchemaFixture(t)
	return r
}

// TestRoleStatus_Valid verifies that the two normative status values are
// accepted by RoleStatus.Valid.
func TestRoleStatus_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status RoleStatus
		want   bool
	}{
		{RoleStatusMVHRequired, true},
		{RoleStatusDeclaredButDeferred, true},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range cases {
		got := tc.status.Valid()
		if got != tc.want {
			t.Errorf("RoleStatus(%q).Valid() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestRoleStatus_UnmarshalJSON_Valid verifies that valid status strings are
// accepted by JSON unmarshal.
func TestRoleStatus_UnmarshalJSON_Valid(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`"mvh-required"`, `"declared-but-deferred"`} {
		var s RoleStatus
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Errorf("json.Unmarshal(%s): unexpected error: %v", raw, err)
		}
	}
}

// TestRoleStatus_UnmarshalJSON_Invalid verifies that an unrecognised status
// string is rejected with ErrInvalidRoleStatus.
func TestRoleStatus_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()

	var s RoleStatus
	err := json.Unmarshal([]byte(`"unknown-status"`), &s)
	if err == nil {
		t.Fatal("json.Unmarshal of invalid status: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidRoleStatus) {
		t.Errorf("json.Unmarshal of invalid status: got %v, want errors.Is ErrInvalidRoleStatus", err)
	}
}

// TestNewRole_DefaultReadablePaths verifies that NewRole returns a Role whose
// PermissionSchema has ReadablePaths = ["**"] per §6.2.
func TestNewRole_DefaultReadablePaths(t *testing.T) {
	t.Parallel()

	r := NewRole("implementer", RoleStatusMVHRequired)
	if len(r.PermissionSchema.ReadablePaths) != 1 || r.PermissionSchema.ReadablePaths[0] != "**" {
		t.Errorf("NewRole().PermissionSchema.ReadablePaths = %v, want [\"**\"]", r.PermissionSchema.ReadablePaths)
	}
}

// TestNewRole_NameAndStatus verifies that NewRole sets Name and Status correctly.
func TestNewRole_NameAndStatus(t *testing.T) {
	t.Parallel()

	r := NewRole("reviewer", RoleStatusDeclaredButDeferred)
	if r.Name != "reviewer" {
		t.Errorf("NewRole().Name = %q, want %q", r.Name, "reviewer")
	}
	if r.Status != RoleStatusDeclaredButDeferred {
		t.Errorf("NewRole().Status = %q, want %q", r.Status, RoleStatusDeclaredButDeferred)
	}
}

// TestRole_Validate_Valid verifies that a well-formed Role passes validation.
func TestRole_Validate_Valid(t *testing.T) {
	t.Parallel()

	r := roleFixture(t)
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() on valid Role: unexpected error: %v", err)
	}
}

// TestRole_Validate_EmptyName verifies that an empty RoleName is rejected.
func TestRole_Validate_EmptyName(t *testing.T) {
	t.Parallel()

	r := NewRole("", RoleStatusMVHRequired)
	if err := r.Validate(); err == nil {
		t.Error("Validate() with empty Name: expected error, got nil")
	}
}

// TestRole_Validate_InvalidStatus verifies that an invalid status is rejected.
func TestRole_Validate_InvalidStatus(t *testing.T) {
	t.Parallel()

	r := NewRole("orchestrator", "bad-status")
	err := r.Validate()
	if err == nil {
		t.Fatal("Validate() with invalid Status: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidRoleStatus) {
		t.Errorf("Validate() invalid status: got %v, want errors.Is ErrInvalidRoleStatus", err)
	}
}

// TestRole_JSONRoundTrip verifies that a fully-populated Role survives a JSON
// marshal/unmarshal round-trip with all fields intact.
func TestRole_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := roleFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Role
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Name != orig.Name {
		t.Errorf("Name round-trip: got %q, want %q", got.Name, orig.Name)
	}
	if got.Status != orig.Status {
		t.Errorf("Status round-trip: got %q, want %q", got.Status, orig.Status)
	}
	if len(got.PermissionSchema.AllowedTools) != len(orig.PermissionSchema.AllowedTools) {
		t.Errorf("PermissionSchema.AllowedTools length: got %d, want %d",
			len(got.PermissionSchema.AllowedTools), len(orig.PermissionSchema.AllowedTools))
	}
}

// TestRole_JSONRoundTrip_DeclaredButDeferred verifies that declared-but-deferred
// status survives a round-trip.
func TestRole_JSONRoundTrip_DeclaredButDeferred(t *testing.T) {
	t.Parallel()

	orig := NewRole("shell-role", RoleStatusDeclaredButDeferred)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Role
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Status != RoleStatusDeclaredButDeferred {
		t.Errorf("Status round-trip: got %q, want %q", got.Status, RoleStatusDeclaredButDeferred)
	}
}

// TestRole_JSONRejectsInvalidStatus verifies that a JSON payload with an
// unrecognised status string is rejected.
func TestRole_JSONRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	raw := `{"name":"orchestrator","permission_schema":{},"status":"bad-value"}`
	var r Role
	err := json.Unmarshal([]byte(raw), &r)
	if err == nil {
		t.Fatal("json.Unmarshal with invalid status: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidRoleStatus) {
		t.Errorf("json.Unmarshal with invalid status: got %v, want errors.Is ErrInvalidRoleStatus", err)
	}
}
