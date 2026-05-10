package handlercontract_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// skillResolution — per-bead helper prefix for test helpers in this file.
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.56)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// skillResolutionFixtureDir creates a temporary directory containing the
// specified skill-name sub-directories.  Returns the parent directory path.
// The directory (and all contents) is cleaned up via t.Cleanup.
func skillResolutionFixtureDir(t *testing.T, skills ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, skill := range skills {
		if err := os.MkdirAll(filepath.Join(dir, skill), 0o755); err != nil {
			t.Fatalf("skillResolutionFixtureDir: mkdir %q: %v", skill, err)
		}
	}
	return dir
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-047 — ResolveSkill tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHC047_ResolveSkill_FirstMatchReturned verifies that ResolveSkill returns
// the first matching search-path entry.
//
// Spec ref: handler-contract.md §4.11.HC-047 — "resolve the name against
// LaunchSpec.skill_search_paths[] in order, take the first match."
func TestHC047_ResolveSkill_FirstMatchReturned(t *testing.T) {
	t.Parallel()

	dir1 := skillResolutionFixtureDir(t, "my-skill")
	dir2 := skillResolutionFixtureDir(t, "my-skill")

	resolved, err := handlercontract.ResolveSkill("my-skill", []string{dir1, dir2})
	if err != nil {
		t.Fatalf("HC-047: ResolveSkill: got err %v, want nil", err)
	}
	want := filepath.Join(dir1, "my-skill")
	if resolved.SourcePath != want {
		t.Errorf("HC-047: ResolveSkill: SourcePath = %q, want %q", resolved.SourcePath, want)
	}
	if resolved.Name != "my-skill" {
		t.Errorf("HC-047: ResolveSkill: Name = %q, want %q", resolved.Name, "my-skill")
	}
}

// TestHC047_ResolveSkill_SecondPathWhenFirstLacks verifies that ResolveSkill
// advances to the second search path when the skill is absent from the first.
//
// Spec ref: handler-contract.md §4.11.HC-047.
func TestHC047_ResolveSkill_SecondPathWhenFirstLacks(t *testing.T) {
	t.Parallel()

	dir1 := skillResolutionFixtureDir(t) // no skills
	dir2 := skillResolutionFixtureDir(t, "beads-cli")

	resolved, err := handlercontract.ResolveSkill("beads-cli", []string{dir1, dir2})
	if err != nil {
		t.Fatalf("HC-047: ResolveSkill: second path: got err %v, want nil", err)
	}
	want := filepath.Join(dir2, "beads-cli")
	if resolved.SourcePath != want {
		t.Errorf("HC-047: ResolveSkill: SourcePath = %q, want %q", resolved.SourcePath, want)
	}
}

// TestHC047_ResolveSkill_NotFoundReturnsErrSkillProvisioningFailed verifies
// that when a skill cannot be found in any search path, ResolveSkill returns
// ErrSkillProvisioningFailed.
//
// Spec ref: handler-contract.md §4.11.HC-048 — "cannot be resolved →
// ErrSkillProvisioningFailed (wrapping ErrStructural)."
func TestHC047_ResolveSkill_NotFoundReturnsErrSkillProvisioningFailed(t *testing.T) {
	t.Parallel()

	dir := skillResolutionFixtureDir(t) // empty — no skills

	_, err := handlercontract.ResolveSkill("missing-skill", []string{dir})
	if err == nil {
		t.Fatal("HC-047: ResolveSkill: expected non-nil error for missing skill, got nil")
	}
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-047: ResolveSkill: error = %v, want wrapping ErrSkillProvisioningFailed", err)
	}
}

// TestHC047_ResolveSkill_ErrSkillProvisioningFailed_WrapsErrStructural verifies
// that the ErrSkillProvisioningFailed returned on resolution failure wraps
// ErrStructural (per HC-022 / HC-048).
//
// Spec ref: handler-contract.md §4.5.HC-022 + §4.11.HC-048.
func TestHC047_ResolveSkill_ErrSkillProvisioningFailed_WrapsErrStructural(t *testing.T) {
	t.Parallel()

	dir := skillResolutionFixtureDir(t)

	_, err := handlercontract.ResolveSkill("absent", []string{dir})
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("HC-047: ResolveSkill not-found error must wrap ErrStructural; got %v", err)
	}
}

// TestHC047_ResolveSkill_EmptySearchPaths verifies that an empty search-paths
// list results in ErrSkillProvisioningFailed (nothing to search).
//
// Spec ref: handler-contract.md §4.11.HC-047.
func TestHC047_ResolveSkill_EmptySearchPaths(t *testing.T) {
	t.Parallel()

	_, err := handlercontract.ResolveSkill("any-skill", nil)
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-047: ResolveSkill with empty search paths: got %v, want ErrSkillProvisioningFailed", err)
	}
}

// TestHC047_ResolveSkill_FileNotDirectory verifies that a file named after the
// skill (not a directory) does NOT count as a match.
//
// Spec ref: handler-contract.md §4.11.HC-047 — skill package MUST be a
// directory (directory layout per components.md §10).
func TestHC047_ResolveSkill_FileNotDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a file (not a directory) with the skill name.
	filePath := filepath.Join(dir, "not-a-dir-skill")
	if err := os.WriteFile(filePath, []byte("not a skill"), 0o644); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	_, err := handlercontract.ResolveSkill("not-a-dir-skill", []string{dir})
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-047: ResolveSkill: file-not-dir: got %v, want ErrSkillProvisioningFailed", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-048 — ResolveAllSkills tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHC048_ResolveAllSkills_EmptyRequired verifies that a nil or empty
// required_skills list returns an empty slice without error.
//
// Spec ref: handler-contract.md §4.11.HC-048.
func TestHC048_ResolveAllSkills_EmptyRequired(t *testing.T) {
	t.Parallel()

	cases := [][]string{nil, {}}
	for _, required := range cases {
		resolved, err := handlercontract.ResolveAllSkills(required, nil)
		if err != nil {
			t.Errorf("HC-048: ResolveAllSkills(nil/empty): got err %v, want nil", err)
		}
		if len(resolved) != 0 {
			t.Errorf("HC-048: ResolveAllSkills(nil/empty): got %d resolved skills, want 0", len(resolved))
		}
	}
}

// TestHC048_ResolveAllSkills_AllResolvable verifies that all skills in
// required_skills are resolved and returned when present.
//
// Spec ref: handler-contract.md §4.11.HC-047 + HC-048.
func TestHC048_ResolveAllSkills_AllResolvable(t *testing.T) {
	t.Parallel()

	dir := skillResolutionFixtureDir(t, "skill-a", "skill-b", "skill-c")

	resolved, err := handlercontract.ResolveAllSkills(
		[]string{"skill-a", "skill-b", "skill-c"},
		[]string{dir},
	)
	if err != nil {
		t.Fatalf("HC-048: ResolveAllSkills all-resolvable: got err %v, want nil", err)
	}
	if len(resolved) != 3 {
		t.Fatalf("HC-048: ResolveAllSkills: got %d resolved, want 3", len(resolved))
	}

	names := make([]string, len(resolved))
	for i, r := range resolved {
		names[i] = r.Name
	}
	for _, want := range []string{"skill-a", "skill-b", "skill-c"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("HC-048: ResolveAllSkills: missing resolved skill %q", want)
		}
	}
}

// TestHC048_ResolveAllSkills_UnresolvableReturnsErrSkillProvisioningFailed
// verifies that when any skill cannot be resolved, ResolveAllSkills returns
// ErrSkillProvisioningFailed immediately (fail-fast per HC-048).
//
// Spec ref: handler-contract.md §4.11.HC-048 — "the run MUST NOT proceed."
func TestHC048_ResolveAllSkills_UnresolvableReturnsErrSkillProvisioningFailed(t *testing.T) {
	t.Parallel()

	dir := skillResolutionFixtureDir(t, "skill-a") // skill-b is absent

	_, err := handlercontract.ResolveAllSkills(
		[]string{"skill-a", "skill-b"},
		[]string{dir},
	)
	if err == nil {
		t.Fatal("HC-048: ResolveAllSkills with unresolvable skill: expected non-nil error, got nil")
	}
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-048: error = %v, want wrapping ErrSkillProvisioningFailed", err)
	}
}

// TestHC048_ResolveAllSkills_FailFastOnFirstUnresolvable verifies that
// ResolveAllSkills stops at the first unresolvable skill and does not continue.
//
// Spec ref: handler-contract.md §4.11.HC-048.
func TestHC048_ResolveAllSkills_FailFastOnFirstUnresolvable(t *testing.T) {
	t.Parallel()

	dir := skillResolutionFixtureDir(t, "skill-a", "skill-c") // skill-b absent

	// skill-a resolves; skill-b does not → fail-fast; skill-c never tried.
	_, err := handlercontract.ResolveAllSkills(
		[]string{"skill-a", "skill-b", "skill-c"},
		[]string{dir},
	)
	if !errors.Is(err, handlercontract.ErrSkillProvisioningFailed) {
		t.Errorf("HC-048: fail-fast: got %v, want ErrSkillProvisioningFailed", err)
	}
}

// TestHC048_ResolveAllSkills_SourcePathPopulated verifies that each resolved
// skill carries the absolute SourcePath of its package directory.
//
// Spec ref: handler-contract.md §4.11.HC-047 — "source_path" in the resolved
// entry MUST be the absolute path of the first matching skill directory.
func TestHC048_ResolveAllSkills_SourcePathPopulated(t *testing.T) {
	t.Parallel()

	dir := skillResolutionFixtureDir(t, "beads-cli")

	resolved, err := handlercontract.ResolveAllSkills([]string{"beads-cli"}, []string{dir})
	if err != nil {
		t.Fatalf("HC-048: ResolveAllSkills: got err %v, want nil", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("HC-048: ResolveAllSkills: got %d results, want 1", len(resolved))
	}

	wantSourcePath := filepath.Join(dir, "beads-cli")
	if resolved[0].SourcePath != wantSourcePath {
		t.Errorf("HC-048: ResolvedSkill.SourcePath = %q, want %q", resolved[0].SourcePath, wantSourcePath)
	}
}
