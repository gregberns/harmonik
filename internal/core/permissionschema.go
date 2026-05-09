package core

// PermissionSchema is the permission surface declared on a Role in a policy
// document (specs/control-points.md §6.2 RECORD PermissionSchema).
//
// Every Role MUST carry a PermissionSchema; see specs/control-points.md §4.6
// for the full role-permission contract.
//
// # Default: ReadablePaths
//
// ReadablePaths defaults to ["**"] per §6.2. Use [NewPermissionSchema] to
// obtain a zero-value PermissionSchema with this default applied. Callers
// constructing a PermissionSchema via struct literal MUST set ReadablePaths
// explicitly; the zero value (nil slice) is NOT equivalent to ["**"].
//
// # CP-031 MVH constraint
//
// DefaultSkills MUST include "beads-cli" for any role that is designated
// mvh-required (specs/control-points.md §4.6.CP-031). Enforcement lives in
// the policy validator, not at the type level.
//
// # Typed-alias deferral
//
// The following fields use string as a placeholder pending typed-alias
// implementation:
//   - AllowedHooks  []string  — TODO hk-a8bg.89 (HookName)
//   - InvocableBy   []string  — TODO hk-a8bg.91 (RoleName)
type PermissionSchema struct {
	// AllowedTools is the closed list of tool names this role may invoke.
	// An empty slice means no tools are permitted.
	// Spec: specs/control-points.md §6.2 RECORD PermissionSchema field allowed_tools.
	AllowedTools []ToolName `json:"allowed_tools"`

	// WritablePaths is the list of workspace-relative globs this role may
	// write. An empty slice means no paths are writable.
	// Spec: specs/control-points.md §6.2 RECORD PermissionSchema field writable_paths.
	WritablePaths []PathGlob `json:"writable_paths"`

	// ReadablePaths is the list of workspace-relative globs this role may
	// read. Defaults to ["**"] (all paths readable) per §6.2; use
	// [NewPermissionSchema] to get the correct default.
	// Spec: specs/control-points.md §6.2 RECORD PermissionSchema field readable_paths.
	ReadablePaths []PathGlob `json:"readable_paths"`

	// ModelTier is the optional model tier name allocated to this role.
	// When nil, no tier constraint applies.
	// Spec: specs/control-points.md §6.2 (model_tier : String | None).
	ModelTier *string `json:"model_tier,omitempty"`

	// DefaultSkills is the list of skill names injected into every handler
	// session running under this role. MUST include "beads-cli" for any
	// mvh-required role per CP-031.
	// Spec: specs/control-points.md §6.2 RECORD PermissionSchema field default_skills.
	DefaultSkills []SkillName `json:"default_skills"`

	// AllowedHooks is the list of Hook names that may modify this role's
	// behavior. An empty slice means no hooks may modify behavior.
	// TODO hk-a8bg.89: replace string with HookName typed alias.
	AllowedHooks []string `json:"allowed_hooks"`

	// InvocableBy is the list of role names permitted to spawn this role.
	// An empty slice means no role may spawn this role directly.
	// TODO hk-a8bg.91: replace string with RoleName typed alias.
	InvocableBy []string `json:"invocable_by"`
}

// NewPermissionSchema returns a PermissionSchema with the spec-mandated default
// applied: ReadablePaths is initialised to ["**"] per §6.2.
//
// All other list fields are initialised to empty (non-nil) slices so callers
// can append without a nil-slice check.
func NewPermissionSchema() PermissionSchema {
	return PermissionSchema{
		AllowedTools:  []ToolName{},
		WritablePaths: []PathGlob{},
		ReadablePaths: []PathGlob{"**"},
		DefaultSkills: []SkillName{},
		AllowedHooks:  []string{},
		InvocableBy:   []string{},
	}
}
