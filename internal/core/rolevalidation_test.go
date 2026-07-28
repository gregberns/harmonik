package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// roleFixtureDoc renders a minimal policy YAML carrying only a roles: section.
// ValidateRoles and ValidateRequiredRoleDefaultSkills inspect roles only; the
// other six required sections are CP-035's concern (ValidateSections), so they
// are deliberately absent here.
// Helper prefix: roleFixture.
func roleFixtureDoc(t *testing.T, rolesYAML string) PolicyDocument {
	t.Helper()
	src := "roles:\n" + rolesYAML
	doc, err := ParsePolicyDocument([]byte(src))
	if err != nil {
		t.Fatalf("ParsePolicyDocument(%q): unexpected error: %v", src, err)
	}
	return doc
}

// roleFixtureEntry renders one roles[] entry with the given name, status, and
// default_skills list. A nil skills slice still emits the permission_schema
// block (an empty list), which is what CP-030 shells look like.
// Helper prefix: roleFixture.
func roleFixtureEntry(name, status string, skills []string) string {
	return roleFixtureShell(name, status, nil, nil, skills)
}

// roleFixtureShell renders one roles[] entry with all three CP-030 shell fields
// spelled out, so a test can populate exactly the field under examination.
// Helper prefix: roleFixture.
func roleFixtureShell(name, status string, tools, paths, skills []string) string {
	return fmt.Sprintf(
		"  - name: %s\n    status: %s\n    permission_schema:\n"+
			"      allowed_tools: [%s]\n      writable_paths: [%s]\n      default_skills: [%s]\n",
		name, status,
		strings.Join(tools, ", "),
		strings.Join(paths, ", "),
		strings.Join(skills, ", "),
	)
}

// TestValidateRequiredRoleDefaultSkills covers CP-031: every role whose status
// is RoleStatusRequired MUST carry "beads-cli" in default_skills. Deferred
// roles are exempt (CP-030 empty shells), and a role with no permission_schema
// at all is left to CP-028 so violations are not double-reported.
func TestValidateRequiredRoleDefaultSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		roles    string
		wantErr  error
		wantName string // substring the error must identify the offending role by
	}{
		{
			name:  "required role declaring beads-cli passes",
			roles: roleFixtureEntry("Builder", string(RoleStatusRequired), []string{"beads-cli"}),
		},
		{
			name:  "required role declaring beads-cli among others passes",
			roles: roleFixtureEntry("Reviewer", string(RoleStatusRequired), []string{"agent-reviewer", "beads-cli", "watch"}),
		},
		{
			name:     "required role omitting beads-cli is rejected",
			roles:    roleFixtureEntry("Builder", string(RoleStatusRequired), []string{"session-resume"}),
			wantErr:  ErrMissingBeadsCLISkill,
			wantName: "Builder",
		},
		{
			name:     "required role with empty default_skills is rejected",
			roles:    roleFixtureEntry("Planner", string(RoleStatusRequired), nil),
			wantErr:  ErrMissingBeadsCLISkill,
			wantName: "Planner",
		},
		{
			name:  "deferred role omitting beads-cli is exempt",
			roles: roleFixtureEntry("Scheduler", string(RoleStatusDeclaredButDeferred), nil),
		},
		{
			name: "violation in a later role is reported",
			roles: roleFixtureEntry("Planner", string(RoleStatusRequired), []string{"beads-cli"}) +
				roleFixtureEntry("Builder", string(RoleStatusRequired), []string{"beads-cli"}) +
				roleFixtureEntry("Reviewer", string(RoleStatusRequired), []string{"agent-reviewer"}),
			wantErr:  ErrMissingBeadsCLISkill,
			wantName: "Reviewer",
		},
		{
			name:     "unnamed offending role is identified by index",
			roles:    roleFixtureEntry("Planner", string(RoleStatusRequired), []string{"beads-cli"}) + "  - status: required\n    permission_schema:\n      default_skills: []\n",
			wantErr:  ErrMissingBeadsCLISkill,
			wantName: "roles[1]",
		},
		{
			name:  "role without permission_schema is left to CP-028",
			roles: "  - name: Builder\n    status: required\n",
		},
		{
			name:  "empty roles list passes",
			roles: "  []\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := roleFixtureDoc(t, tc.roles)
			err := doc.ValidateRequiredRoleDefaultSkills()

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateRequiredRoleDefaultSkills() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateRequiredRoleDefaultSkills() = %v, want error wrapping %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantName) {
				t.Errorf("error %q does not identify offending role %q", err, tc.wantName)
			}
		})
	}
}

// TestRoleStatusWireValues pins the on-the-wire spelling of the enum against
// specs/control-points.md §6.2, which declares
// `status : RoleStatus -- required | declared-but-deferred`. Every other test
// here builds its YAML from the constants, so without this the whole file
// would follow a spec-diverging rename rather than catch it.
func TestRoleStatusWireValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status RoleStatus
		want   string
	}{
		{RoleStatusRequired, "required"},
		{RoleStatusDeclaredButDeferred, "declared-but-deferred"},
	}
	for _, tc := range cases {
		if string(tc.status) != tc.want {
			t.Errorf("RoleStatus = %q, want %q (specs/control-points.md §6.2)", tc.status, tc.want)
		}
	}
}

// TestValidateRolesRejectsUnrecognisedStatus pins the single enforcement site
// for RoleStatus membership. YAML decoding accepts any string into the named
// type, so without this check an unrecognised status would slip past both
// CP-030 and CP-031 unnoticed — including the retired "mvh-required" spelling,
// which must now fail loudly rather than be silently accepted.
func TestValidateRolesRejectsUnrecognisedStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roles   string
		wantErr error
	}{
		{
			name:  "required is accepted",
			roles: roleFixtureEntry("Builder", string(RoleStatusRequired), []string{"beads-cli"}),
		},
		{
			name:  "declared-but-deferred is accepted",
			roles: roleFixtureEntry("Scheduler", string(RoleStatusDeclaredButDeferred), nil),
		},
		{
			name:    "retired mvh-required spelling is rejected",
			roles:   roleFixtureEntry("Builder", "mvh-required", []string{"beads-cli"}),
			wantErr: ErrInvalidRoleStatus,
		},
		{
			name:    "unknown status is rejected",
			roles:   roleFixtureEntry("Builder", "provisional", []string{"beads-cli"}),
			wantErr: ErrInvalidRoleStatus,
		},
		{
			name:    "absent status is rejected",
			roles:   "  - name: Builder\n    permission_schema:\n      default_skills: [beads-cli]\n",
			wantErr: ErrInvalidRoleStatus,
		},
		{
			name:    "missing permission_schema is reported before status",
			roles:   "  - name: Builder\n    status: bogus\n",
			wantErr: ErrMissingPermissionSchema,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := roleFixtureDoc(t, tc.roles)
			err := doc.ValidateRoles()

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateRoles() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateRoles() = %v, want error wrapping %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateDeferredRoleShells covers CP-030: every role whose status is
// RoleStatusDeclaredButDeferred MUST carry an empty permission shell. The
// "required role with a populated schema passes" case is load-bearing — it is
// what proves the status comparison discriminates on the constant rather than
// matching nothing at all, which is how a stale literal would fail silently.
func TestValidateDeferredRoleShells(t *testing.T) {
	t.Parallel()

	deferred := string(RoleStatusDeclaredButDeferred)

	tests := []struct {
		name     string
		roles    string
		wantErr  error
		wantName string
	}{
		{
			name:  "deferred role with an empty shell passes",
			roles: roleFixtureShell("Scheduler", deferred, nil, nil, nil),
		},
		{
			name:     "deferred role with non-empty allowed_tools is rejected",
			roles:    roleFixtureShell("Scheduler", deferred, []string{"Read"}, nil, nil),
			wantErr:  ErrNonEmptyDeferredRoleShell,
			wantName: "Scheduler",
		},
		{
			name:     "deferred role with non-empty writable_paths is rejected",
			roles:    roleFixtureShell("Verifier", deferred, nil, []string{`"**"`}, nil),
			wantErr:  ErrNonEmptyDeferredRoleShell,
			wantName: "Verifier",
		},
		{
			name:     "deferred role with non-empty default_skills is rejected",
			roles:    roleFixtureShell("Governor", deferred, nil, nil, []string{"beads-cli"}),
			wantErr:  ErrNonEmptyDeferredRoleShell,
			wantName: "Governor",
		},
		{
			name:  "required role with a fully populated schema is exempt",
			roles: roleFixtureShell("Builder", string(RoleStatusRequired), []string{"Read", "Edit"}, []string{`"**"`}, []string{"beads-cli"}),
		},
		{
			name:  "deferred role without permission_schema is left to CP-028",
			roles: "  - name: Scheduler\n    status: " + deferred + "\n",
		},
		{
			name:     "unnamed offending role is identified by index",
			roles:    roleFixtureShell("Scheduler", deferred, nil, nil, nil) + "  - status: " + deferred + "\n    permission_schema:\n      allowed_tools: [Read]\n",
			wantErr:  ErrNonEmptyDeferredRoleShell,
			wantName: "roles[1]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := roleFixtureDoc(t, tc.roles)
			err := doc.ValidateDeferredRoleShells()

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateDeferredRoleShells() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateDeferredRoleShells() = %v, want error wrapping %v", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantName) {
				t.Errorf("error %q does not identify offending role %q", err, tc.wantName)
			}
		})
	}
}

// TestDefaultRequiredRolesSatisfyCP031 asserts the shipped defaults are not
// themselves a CP-031 violation: every role DefaultRequiredRoles returns is
// RoleStatusRequired and carries "beads-cli".
func TestDefaultRequiredRolesSatisfyCP031(t *testing.T) {
	t.Parallel()

	roles := DefaultRequiredRoles()
	if len(roles) == 0 {
		t.Fatal("DefaultRequiredRoles() returned no roles")
	}

	for _, r := range roles {
		t.Run(string(r.Name), func(t *testing.T) {
			t.Parallel()
			if r.Status != RoleStatusRequired {
				t.Errorf("status = %q, want %q", r.Status, RoleStatusRequired)
			}
			found := false
			for _, s := range r.PermissionSchema.DefaultSkills {
				if s == "beads-cli" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("default_skills %v missing \"beads-cli\" (CP-031)", r.PermissionSchema.DefaultSkills)
			}
		})
	}
}
