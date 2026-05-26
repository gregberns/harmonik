package core

// DefaultMVHRoles returns the three concrete default Role values for the
// MVH-required roles (Planner, Builder, Reviewer) per CP-029.
//
// These defaults are shipped at harmonik init and form the lowest-precedence
// layer in the §4.7 config-precedence stack. Higher-precedence layers
// (operator policy, workflow def, runtime override) may override any field.
//
// Each role satisfies:
//   - CP-028: permission_schema present (non-nil schema pointer equivalents)
//   - CP-029: concrete permission set (non-empty AllowedTools and WritablePaths
//     where the role's purpose requires write access)
//   - CP-031: "beads-cli" present in DefaultSkills
//
// Role semantics (purpose and what each role does not do) are owned by
// [specs/architecture.md §4.8]; this function owns only the permission surface.
//
// Spec: specs/control-points.md §4.6.CP-029.
func DefaultMVHRoles() []Role {
	return []Role{
		defaultPlannerRole(),
		defaultBuilderRole(),
		defaultReviewerRole(),
	}
}

// defaultPlannerRole returns the concrete default Role for the Planner.
//
// The Planner reads the full workspace and produces workflow artifacts
// (specs, DOT files, plan documents). It does not write source code.
func defaultPlannerRole() Role {
	ps := NewPermissionSchema()
	ps.AllowedTools = []ToolName{"Read", "Bash", "WebSearch", "WebFetch", "Agent"}
	ps.WritablePaths = []PathGlob{"specs/**", "docs/**", "*.dot", ".kerf/**"}
	ps.DefaultSkills = []SkillName{"beads-cli", "session-resume"}
	ps.InvocableBy = []RoleName{}
	return Role{
		Name:             "Planner",
		Status:           RoleStatusMVHRequired,
		PermissionSchema: ps,
	}
}

// defaultBuilderRole returns the concrete default Role for the Builder.
//
// The Builder implements changes across the full workspace. It may write to
// any path and invoke all implementation tools. Invocable by Planner.
func defaultBuilderRole() Role {
	ps := NewPermissionSchema()
	ps.AllowedTools = []ToolName{"Read", "Edit", "Write", "Bash", "Agent"}
	ps.WritablePaths = []PathGlob{"**"}
	ps.DefaultSkills = []SkillName{"beads-cli"}
	ps.InvocableBy = []RoleName{"Planner"}
	return Role{
		Name:             "Builder",
		Status:           RoleStatusMVHRequired,
		PermissionSchema: ps,
	}
}

// defaultReviewerRole returns the concrete default Role for the Reviewer.
//
// The Reviewer reads the full workspace to evaluate work and writes only to
// the review-verdict path. It does not modify source code or specs.
// Invocable by Planner and Builder.
func defaultReviewerRole() Role {
	ps := NewPermissionSchema()
	ps.AllowedTools = []ToolName{"Read", "Bash"}
	ps.WritablePaths = []PathGlob{".harmonik/verdicts/**", ".harmonik/reviews/**"}
	ps.DefaultSkills = []SkillName{"beads-cli", "agent-reviewer"}
	ps.InvocableBy = []RoleName{"Planner", "Builder"}
	return Role{
		Name:             "Reviewer",
		Status:           RoleStatusMVHRequired,
		PermissionSchema: ps,
	}
}
