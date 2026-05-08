package scenario

// AgentOverride selects a twin binary in place of the production agent for one
// agent role within a scenario, with optional CLI-arg merge.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD AgentOverride.
//
// Binary is interpreted per SH-009 (twin-search-path-relative name OR absolute
// path) and is subject to the HC-043 commit-hash check performed by the daemon
// before launch. Path resolution and hash verification are caller
// responsibilities; Valid does not perform them.
//
// Args, if non-nil, are APPENDED to the production composition root's default
// args (merge semantics: no replacement). A nil Args value and an empty
// []string are semantically equivalent at the value level (both append zero
// items); nil is the natural zero value and is preferred for "no additional
// args".
type AgentOverride struct {
	// Binary is the absolute path or twin-search-path-relative name of the
	// twin binary that replaces the production agent for this role.
	// Required (non-empty). Subject to SH-009 twin-search-path resolution
	// and HC-043 commit-hash verification at harness launch time.
	Binary string `json:"binary" yaml:"binary"`

	// Args holds additional CLI arguments appended to the production
	// composition root's default args (merge semantics per §6.1). nil means
	// "no additional args" (|None in the spec). Never replaces defaults.
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
}

// Valid reports whether the AgentOverride is structurally well-formed:
//   - Binary is non-empty.
//   - Args may be nil or a non-nil slice; both are valid per the List<String>|None spec.
//
// Path resolution (SH-009) and hash verification (HC-043) are caller
// responsibilities and are NOT checked here.
func (a AgentOverride) Valid() bool {
	return a.Binary != ""
}
