package core

import "regexp"

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

// skillNameShapeRegex enforces the CP-049 shape for ingest-time syntactic
// validation: a lowercase-hyphenated identifier (like agent_type), optionally
// suffixed with @<version>.
//
// Pattern:
//   - Name part: ^[a-z][a-z0-9-]{0,62} — starts with a letter, then
//     alphanumeric or hyphen, at most 63 chars total.
//   - Optional version suffix: @[a-zA-Z0-9._+-]+ — the "@" followed by at
//     least one version character.
//
// Spec: specs/control-points.md §4.11.CP-049 — "lowercase hyphenated
// identifier, optionally suffixed with @<version>."
var skillNameShapeRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}(@[a-zA-Z0-9._+\-]+)?$`)

// ValidShape reports whether s matches the CP-049 skill-name shape:
// a lowercase-hyphenated identifier, optionally suffixed with @<version>.
//
// This is the ingest-time syntactic validity check per CP-049. A SkillName
// that fails ValidShape() MUST be rejected at workflow-ingest time with
// ErrDeterministic.
//
// Spec: specs/control-points.md §4.11.CP-049.
func (s SkillName) ValidShape() bool {
	return skillNameShapeRegex.MatchString(string(s))
}
