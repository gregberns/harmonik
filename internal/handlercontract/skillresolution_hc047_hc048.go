package handlercontract

import (
	"fmt"
	"os"
	"path/filepath"
)

// skillResolution — per-bead helper prefix for test helpers in
// skillresolution_hc047_hc048_test.go (implementer-protocol.md
// §Helper-prefix discipline; bead hk-8i31.56).

// ─────────────────────────────────────────────────────────────────────────────
// HC-047 — Skill resolution is mechanism-tagged
// ─────────────────────────────────────────────────────────────────────────────

// ResolvedSkill is the result of a successful skill resolution: the skill name
// and the absolute path of the first matching package directory on the search
// path.
//
// Spec: specs/handler-contract.md §4.11.HC-047.
type ResolvedSkill struct {
	// Name is the skill name as declared in LaunchSpec.required_skills[].
	Name string

	// SourcePath is the absolute directory path of the resolved skill package:
	// the first directory within a skill_search_paths[] entry that contains a
	// sub-directory matching Name.
	//
	// Spec: handler-contract.md §4.11.HC-047 — "resolve the name against
	// LaunchSpec.skill_search_paths[] in order, take the first match."
	SourcePath string
}

// ResolveSkill resolves a single skill name against the ordered search paths
// per HC-047.
//
// Resolution is deterministic: the function walks searchPaths in order and
// returns the first directory path that contains a sub-directory whose name
// equals skillName.  No cognition participates.
//
// Returns (ResolvedSkill, nil) on success.
// Returns ("", ErrSkillProvisioningFailed) when skillName does not resolve
// against any search path — the structural fail-launch condition per HC-048.
//
// Spec: specs/handler-contract.md §4.11.HC-047.
func ResolveSkill(skillName string, searchPaths []string) (ResolvedSkill, error) {
	for _, searchDir := range searchPaths {
		candidate := filepath.Join(searchDir, skillName)
		info, err := os.Stat(candidate)
		if err != nil {
			// Not found in this search path; continue.
			continue
		}
		if info.IsDir() {
			return ResolvedSkill{Name: skillName, SourcePath: candidate}, nil
		}
	}
	return ResolvedSkill{}, fmt.Errorf(
		"handlercontract: skill %q not found in any search path: %w",
		skillName, ErrSkillProvisioningFailed,
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-048 — Fail-launch on unresolvable required skill (structural)
// ─────────────────────────────────────────────────────────────────────────────

// ResolveAllSkills resolves every name in requiredSkills against searchPaths
// per HC-047, returning the full set of resolved skills on success.
//
// If any name cannot be resolved, ResolveAllSkills returns nil and an error
// wrapping ErrSkillProvisioningFailed per HC-048.  The error message carries
// the first unresolvable skill name.  Resolution stops at the first failure
// (fail-fast per HC-048: "the run MUST NOT proceed").
//
// A nil or empty requiredSkills slice is valid; the function returns an empty
// slice without error (no provisioning obligation).
//
// Spec: specs/handler-contract.md §4.11.HC-048.
func ResolveAllSkills(requiredSkills, searchPaths []string) ([]ResolvedSkill, error) {
	if len(requiredSkills) == 0 {
		return []ResolvedSkill{}, nil
	}
	resolved := make([]ResolvedSkill, 0, len(requiredSkills))
	for _, name := range requiredSkills {
		r, err := ResolveSkill(name, searchPaths)
		if err != nil {
			// HC-048: first unresolvable skill → fail-launch immediately.
			return nil, fmt.Errorf(
				"handlercontract: HC-048: required skill %q unresolvable: %w",
				name, ErrSkillProvisioningFailed,
			)
		}
		resolved = append(resolved, r)
	}
	return resolved, nil
}
