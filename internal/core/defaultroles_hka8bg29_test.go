package core

import (
	"testing"
)

// ---------------------------------------------------------------------------
// CP-029: MVH-required roles carry concrete default permission sets.
// specs/control-points.md §4.6.CP-029
// ---------------------------------------------------------------------------

// TestDefaultMVHRoles_ReturnsThreeRoles verifies that DefaultMVHRoles returns
// exactly three entries — one per MVH-required role.
func TestDefaultMVHRoles_ReturnsThreeRoles(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	if len(roles) != 3 {
		t.Fatalf("DefaultMVHRoles() len = %d, want 3", len(roles))
	}
}

// TestDefaultMVHRoles_Names verifies the three returned roles are Planner,
// Builder, and Reviewer in that order.
func TestDefaultMVHRoles_Names(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	want := []RoleName{"Planner", "Builder", "Reviewer"}
	for i, r := range roles {
		if r.Name != want[i] {
			t.Errorf("DefaultMVHRoles()[%d].Name = %q, want %q", i, r.Name, want[i])
		}
	}
}

// TestDefaultMVHRoles_StatusMVHRequired verifies every returned role has
// status mvh-required (not declared-but-deferred).
func TestDefaultMVHRoles_StatusMVHRequired(t *testing.T) {
	t.Parallel()

	for _, r := range DefaultMVHRoles() {
		if r.Status != RoleStatusMVHRequired {
			t.Errorf("role %q: Status = %q, want %q", r.Name, r.Status, RoleStatusMVHRequired)
		}
	}
}

// TestDefaultMVHRoles_Validate verifies every returned role passes Role.Validate.
func TestDefaultMVHRoles_Validate(t *testing.T) {
	t.Parallel()

	for _, r := range DefaultMVHRoles() {
		if err := r.Validate(); err != nil {
			t.Errorf("role %q: Validate() = %v, want nil", r.Name, err)
		}
	}
}

// TestDefaultMVHRoles_ReadablePathsDefault verifies every role has the §6.2
// default ReadablePaths = ["**"].
func TestDefaultMVHRoles_ReadablePathsDefault(t *testing.T) {
	t.Parallel()

	for _, r := range DefaultMVHRoles() {
		ps := r.PermissionSchema
		if len(ps.ReadablePaths) != 1 || ps.ReadablePaths[0] != "**" {
			t.Errorf("role %q: ReadablePaths = %v, want [\"**\"]", r.Name, ps.ReadablePaths)
		}
	}
}

// TestDefaultMVHRoles_CP031_BeadsCLI verifies every role includes "beads-cli"
// in DefaultSkills, satisfying CP-031.
func TestDefaultMVHRoles_CP031_BeadsCLI(t *testing.T) {
	t.Parallel()

	for _, r := range DefaultMVHRoles() {
		found := false
		for _, s := range r.PermissionSchema.DefaultSkills {
			if s == "beads-cli" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("role %q: DefaultSkills %v does not contain \"beads-cli\" (CP-031)", r.Name, r.PermissionSchema.DefaultSkills)
		}
	}
}

// TestDefaultMVHRoles_CP029_ConcreteTools verifies that mvh-required roles
// carry a non-empty AllowedTools list — CP-029 "concrete default permission set".
// A role with no tools would be a hollow shell, not a concrete set.
func TestDefaultMVHRoles_CP029_ConcreteTools(t *testing.T) {
	t.Parallel()

	for _, r := range DefaultMVHRoles() {
		if len(r.PermissionSchema.AllowedTools) == 0 {
			t.Errorf("role %q: AllowedTools is empty; CP-029 requires a concrete permission set", r.Name)
		}
	}
}

// TestDefaultMVHRoles_Planner_NoWriteToSourceCode verifies that the Planner's
// WritablePaths do not include "**" — the Planner reads and plans but does not
// write source code.
func TestDefaultMVHRoles_Planner_NoWriteToSourceCode(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	planner := roles[0]
	if planner.Name != "Planner" {
		t.Fatalf("roles[0].Name = %q, want \"Planner\"", planner.Name)
	}
	for _, p := range planner.PermissionSchema.WritablePaths {
		if p == "**" {
			t.Error("Planner.WritablePaths contains \"**\"; Planner must not have blanket write access")
		}
	}
}

// TestDefaultMVHRoles_Builder_WritableAll verifies that the Builder's
// WritablePaths includes "**" — the Builder must be able to write anywhere.
func TestDefaultMVHRoles_Builder_WritableAll(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	builder := roles[1]
	if builder.Name != "Builder" {
		t.Fatalf("roles[1].Name = %q, want \"Builder\"", builder.Name)
	}
	found := false
	for _, p := range builder.PermissionSchema.WritablePaths {
		if p == "**" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Builder.WritablePaths = %v, want to contain \"**\"", builder.PermissionSchema.WritablePaths)
	}
}

// TestDefaultMVHRoles_Reviewer_LimitedWritePaths verifies that the Reviewer's
// WritablePaths are restricted to review-artifact directories and do not
// include the source tree.
func TestDefaultMVHRoles_Reviewer_LimitedWritePaths(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	reviewer := roles[2]
	if reviewer.Name != "Reviewer" {
		t.Fatalf("roles[2].Name = %q, want \"Reviewer\"", reviewer.Name)
	}
	for _, p := range reviewer.PermissionSchema.WritablePaths {
		if p == "**" {
			t.Error("Reviewer.WritablePaths contains \"**\"; Reviewer must not have blanket write access")
		}
	}
	if len(reviewer.PermissionSchema.WritablePaths) == 0 {
		t.Error("Reviewer.WritablePaths is empty; Reviewer must be able to write review artifacts")
	}
}

// TestDefaultMVHRoles_Builder_InvocableByPlanner verifies that the Builder
// declares Planner in InvocableBy, encoding the canonical delegation chain.
func TestDefaultMVHRoles_Builder_InvocableByPlanner(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	builder := roles[1]
	if builder.Name != "Builder" {
		t.Fatalf("roles[1].Name = %q, want \"Builder\"", builder.Name)
	}
	found := false
	for _, r := range builder.PermissionSchema.InvocableBy {
		if r == "Planner" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Builder.InvocableBy = %v, want to contain \"Planner\"", builder.PermissionSchema.InvocableBy)
	}
}

// TestDefaultMVHRoles_Reviewer_AgentReviewerSkill verifies that the Reviewer
// role includes "agent-reviewer" in DefaultSkills, encoding the delegation
// path for cognition-tagged review evaluators.
func TestDefaultMVHRoles_Reviewer_AgentReviewerSkill(t *testing.T) {
	t.Parallel()

	roles := DefaultMVHRoles()
	reviewer := roles[2]
	if reviewer.Name != "Reviewer" {
		t.Fatalf("roles[2].Name = %q, want \"Reviewer\"", reviewer.Name)
	}
	found := false
	for _, s := range reviewer.PermissionSchema.DefaultSkills {
		if s == "agent-reviewer" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Reviewer.DefaultSkills = %v, want to contain \"agent-reviewer\"", reviewer.PermissionSchema.DefaultSkills)
	}
}

// TestDefaultMVHRoles_AllListFieldsNonNil verifies that no list field in any
// default role's PermissionSchema is nil — nil vs empty-slice semantics must
// be explicit per NewPermissionSchema contract.
func TestDefaultMVHRoles_AllListFieldsNonNil(t *testing.T) {
	t.Parallel()

	for _, r := range DefaultMVHRoles() {
		ps := r.PermissionSchema
		name := string(r.Name)
		if ps.AllowedTools == nil {
			t.Errorf("role %q: AllowedTools is nil, want non-nil slice", name)
		}
		if ps.WritablePaths == nil {
			t.Errorf("role %q: WritablePaths is nil, want non-nil slice", name)
		}
		if ps.ReadablePaths == nil {
			t.Errorf("role %q: ReadablePaths is nil, want non-nil slice", name)
		}
		if ps.DefaultSkills == nil {
			t.Errorf("role %q: DefaultSkills is nil, want non-nil slice", name)
		}
		if ps.AllowedHooks == nil {
			t.Errorf("role %q: AllowedHooks is nil, want non-nil slice", name)
		}
		if ps.InvocableBy == nil {
			t.Errorf("role %q: InvocableBy is nil, want non-nil slice", name)
		}
	}
}
