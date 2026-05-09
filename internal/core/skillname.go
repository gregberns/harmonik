package core

// SkillName is a typed alias for a skill name string referenced in a
// PermissionSchema (specs/control-points.md §6.2 RECORD PermissionSchema
// field default_skills).
//
// A SkillName identifies a skill injected into every handler session running
// under a Role. An empty SkillName is invalid; Valid() returns false for the
// zero value.
//
// Per CP-031, any MVH-required role MUST include the SkillName "beads-cli"
// in its DefaultSkills. Enforcement lives in the policy validator, not at the
// type level.
type SkillName string

// Valid reports whether s is a non-empty skill name.
//
// Rules per specs/control-points.md §6.2:
//   - SkillName must be non-empty.
func (s SkillName) Valid() bool {
	return s != ""
}
