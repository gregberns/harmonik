package scenario

// FixtureSetup holds the workspace seed instructions applied before scenario
// orchestration begins. All three fields are optional (List|None / Map|None per
// the spec); a zero-value FixtureSetup (all nil) is valid and means "no
// seeding — start from a bare synthetic project root."
//
// Nil vs. empty-slice / empty-map semantics (Go idiom, consistent with
// AgentOverride.Args and other List|None fields in this package):
//   - nil slice / nil map → None in the spec: caller did not declare this field.
//   - non-nil but zero-length → caller declared the field as an empty collection.
//
// Both nil and empty are accepted by Valid. Callers that need to distinguish
// "not set" from "empty" should inspect the nil-ness directly.
//
// skill_search_paths values are additional filesystem paths injected into
// LaunchSpec per specs/handler-contract.md §6.1 LaunchSpec (define LaunchSpec:
// bead hk-8i31.74, open). The field type is plain []string; no typed alias is
// used at v0.1 — typed-alias upgrade to LaunchSpec.SkillSearchPaths tracks
// under bead hk-8i31.74.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD FixtureSetup.
type FixtureSetup struct {
	// GitSeed is the ordered list of git operations applied to the synthetic
	// project root before orchestration. nil means no git seeding (|None in
	// the spec). See GitSeedOp for per-op semantics.
	GitSeed []GitSeedOp `json:"git_seed,omitempty" yaml:"git_seed,omitempty"`

	// Files is the path → file-seed map applied to the synthetic project root
	// before orchestration. Keys are repo-relative paths. nil means no file
	// seeding (|None in the spec). See FileSeed for per-file semantics.
	Files map[string]FileSeed `json:"files,omitempty" yaml:"files,omitempty"`

	// SkillSearchPaths holds additional skill search paths injected into
	// LaunchSpec per specs/handler-contract.md §6.1 LaunchSpec
	// (bead hk-8i31.74, open). nil means no additional search paths
	// (|None in the spec).
	SkillSearchPaths []string `json:"skill_search_paths,omitempty" yaml:"skill_search_paths,omitempty"`
}

// Valid reports whether the FixtureSetup is structurally well-formed:
//   - A nil or empty GitSeed is valid (all ops are optional per §6.1).
//   - Each non-nil GitSeedOp element must satisfy GitSeedOp.Valid().
//   - A nil or empty Files map is valid (all fields are optional per §6.1).
//   - Each FileSeed value must satisfy FileSeed.Valid(); map keys are not
//     validated here (path-resolution is a caller responsibility at SH-012).
//   - A nil or empty SkillSearchPaths is valid; individual path strings are
//     not validated here (resolution against LaunchSpec is caller-side per
//     specs/handler-contract.md §6.1).
func (f FixtureSetup) Valid() bool {
	for _, op := range f.GitSeed {
		if !op.Valid() {
			return false
		}
	}
	for _, seed := range f.Files {
		if !seed.Valid() {
			return false
		}
	}
	return true
}
