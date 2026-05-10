package core

// SkillVersion is a typed alias for the optional version string in the
// skills_provisioned event payload (event-model.md §8.3.8, skills[].version?).
//
// A SkillVersion is an opaque version string in the provisioned skill package's
// canonical format. Empty SkillVersion means the version was not recorded at
// provisioning time (the field is optional per §8.3.8).
//
// Spec ref: event-model.md §8.3.8 — "skills[] (each: name, source_path, version?)."
type SkillVersion string

// IsZero reports whether the SkillVersion is the zero value (empty string),
// meaning no version was recorded.
func (v SkillVersion) IsZero() bool {
	return v == ""
}

// String returns the SkillVersion as a plain string for logging and serialisation.
func (v SkillVersion) String() string {
	return string(v)
}
