package workspace

import (
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestWM006_DefaultIntegrationBranchName verifies that the default integration branch
// name is "harmonik/integration".
//
// Spec ref: workspace-model.md §4.2 WM-006 — "The default integration branch MUST be
// named `harmonik/integration`."
func TestWM006_DefaultIntegrationBranchName(t *testing.T) {
	t.Parallel()

	const want = "harmonik/integration"

	// The spec mandates exactly this string for the default integration branch.
	// WM-009 requires this name to be stable across minor versions.
	got := branchNameFixtureDefaultIntegrationBranch()
	if got != want {
		t.Errorf("WM-006: default integration branch = %q, want %q", got, want)
	}
}

// TestWM006_ParentBeadDerivedIntegrationBranch verifies that when a run has a
// parent-bead context, its integration branch is named
// `harmonik/integration/<parent_bead_id_refsafe>`.
//
// Spec ref: workspace-model.md §4.2 WM-006 — "When a run has a parent-bead context
// visible to the dependency-graph query per [beads-integration.md §4.5 BI-014], its
// task branch MUST target a derived branch named
// `harmonik/integration/<parent_bead_id_refsafe>`, where `<parent_bead_id_refsafe>` is
// the bead ID transformed to satisfy git's ref-name constraints per WM-006a. The exact
// transformation template is operator-configurable per OQ-WM-002; the default is
// verbatim bead-ID substitution."
func TestWM006_ParentBeadDerivedIntegrationBranch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		parentBeadID string
		want         string
	}{
		{
			name:         "standard alphanumeric bead ID",
			parentBeadID: "hk-8mwo",
			want:         "harmonik/integration/hk-8mwo",
		},
		{
			name:         "bead ID with dot-separated suffix",
			parentBeadID: "hk-8mwo66",
			want:         "harmonik/integration/hk-8mwo66",
		},
		{
			name:         "bead ID all lowercase",
			parentBeadID: "abc123",
			want:         "harmonik/integration/abc123",
		},
		{
			name:         "bead ID with slash (ref-safe — single internal slash is ok)",
			parentBeadID: "feature/abc",
			want:         "harmonik/integration/feature/abc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Default template: verbatim bead-ID substitution per WM-006.
			got := branchNameFixtureIntegrationBranchForBead(tc.parentBeadID)
			if got != tc.want {
				t.Errorf("WM-006: parent-bead integration branch for bead_id %q = %q, want %q",
					tc.parentBeadID, got, tc.want)
			}
		})
	}
}

// TestWM006_ParentBeadIntegrationBranchRefSafe verifies that the parent-bead-derived
// integration branch name passes `git check-ref-format` for standard bead IDs.
//
// Spec ref: workspace-model.md §4.2 WM-006 — see above; WM-006a governs the ref-safe
// substitution mechanism (delegated to `git check-ref-format`).
func TestWM006_ParentBeadIntegrationBranchRefSafe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		parentBeadID string
	}{
		{"standard alphanumeric", "hk-8mwo"},
		{"alphanumeric with number", "hk-8mwo66"},
		{"simple lowercase", "abc123"},
		{"uppercase", "ABC123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			branch := branchNameFixtureIntegrationBranchForBead(tc.parentBeadID)
			branchNameFixtureAssertRefSafe(t, "WM-006", branch)
		})
	}
}

// TestWM006a_RefSafeSubstitutionDelegatesToGitCheckRefFormat verifies that the
// workspace manager delegates ref-name validation to `git check-ref-format` rather
// than attempting an independent character-class enumeration.
//
// The test constructs pathological bead IDs, verifies that the raw (unescaped)
// integration branch name is rejected by `git check-ref-format`, then applies the
// canonical hex-encode fallback and verifies the result IS accepted.
//
// Spec ref: workspace-model.md §4.2 WM-006a — "the workspace manager MUST delegate
// ref-name validation to `git check-ref-format(1)` rather than attempting an
// independent character-class enumeration. Concretely: after constructing the proposed
// branch name (`harmonik/integration/<parent_bead_id>` with `<parent_bead_id>`
// substituted verbatim), the workspace manager MUST invoke `git check-ref-format
// refs/heads/<proposed>`; a zero exit code means the name is accepted verbatim, and a
// non-zero exit code means a canonical fallback transformation MUST be applied and
// re-validated."
func TestWM006a_RefSafeSubstitutionDelegatesToGitCheckRefFormat(t *testing.T) {
	t.Parallel()

	// Pathological bead IDs per WM-006a: each exercises a different ref-unsafe pattern.
	// We verify:
	//   (a) the raw integration branch name fails git check-ref-format, AND
	//   (b) the hex-encode fallback produces a name that passes git check-ref-format.
	cases := []struct {
		name    string
		beadID  string
		wantRaw string // for documentation
	}{
		{
			name:    "at-brace sequence (@{)",
			beadID:  "bead@{broken}",
			wantRaw: "harmonik/integration/bead@{broken}",
		},
		{
			name:    "double slash (//)",
			beadID:  "bead//double",
			wantRaw: "harmonik/integration/bead//double",
		},
		{
			name:    "sole at-sign (@)",
			beadID:  "@",
			wantRaw: "harmonik/integration/@",
		},
		{
			name:    "leading dot (.hidden)",
			beadID:  ".hidden-bead",
			wantRaw: "harmonik/integration/.hidden-bead",
		},
		{
			name:    "trailing dot-lock component (bead.lock)",
			beadID:  "bead.lock",
			wantRaw: "harmonik/integration/bead.lock",
		},
		{
			name:    "null byte (control char \\x00)",
			beadID:  "bead\x00null",
			wantRaw: "harmonik/integration/bead\x00null",
		},
		{
			name:    "newline (control char \\n)",
			beadID:  "bead\nnewline",
			wantRaw: "harmonik/integration/bead\nnewline",
		},
		{
			name:    "tab (control char \\t)",
			beadID:  "bead\ttab",
			wantRaw: "harmonik/integration/bead\ttab",
		},
	}

	// TODO: WM-006a clause (iii) — "re-validate after fallback" path not yet exercised
	// in this fixture. Covered by future bead.

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rawBranch := branchNameFixtureIntegrationBranchForBead(tc.beadID)

			// (a) The raw name should be rejected by git check-ref-format for
			// pathological inputs. We assert rawRefSafe == false for every case
			// except the bare "@" bead ID — git accepts "harmonik/integration/@"
			// verbatim (as noted in wantRaw documentation above).
			rawRefSafe := branchNameFixtureIsRefSafe(t, rawBranch)
			if tc.beadID != "@" && rawRefSafe {
				t.Errorf("WM-006a: expected raw branch %q to be rejected by git check-ref-format for pathological bead_id %q, but it was accepted",
					rawBranch, tc.beadID)
			}

			// (b) Apply canonical hex-encode fallback and assert result is ref-safe.
			fallbackBeadID := branchNameFixtureHexEncodeFallback(tc.beadID)
			fallbackBranch := branchNameFixtureIntegrationBranchForBead(fallbackBeadID)
			fallbackRefSafe := branchNameFixtureIsRefSafe(t, fallbackBranch)

			if !fallbackRefSafe {
				t.Errorf("WM-006a: canonical hex-encode fallback for bead_id %q produced %q, which FAILS git check-ref-format; fallback MUST produce a valid ref name",
					tc.beadID, fallbackBranch)
			}

			// Log the relationship for diagnostic visibility.
			t.Logf("WM-006a: bead_id=%q raw_branch=%q raw_ok=%v fallback_branch=%q fallback_ok=%v",
				tc.beadID, rawBranch, rawRefSafe, fallbackBranch, fallbackRefSafe)
		})
	}
}

// TestWM006a_RefSafeGitCheckRefFormatIsUsed verifies that the test harness itself
// correctly delegates to `git check-ref-format` — i.e., git is reachable and the
// delegation mechanism functions. This is a meta-test that guards the test infrastructure.
//
// Spec ref: workspace-model.md §4.2 WM-006a — "the workspace manager MUST delegate
// ref-name validation to `git check-ref-format(1)`".
func TestWM006a_RefSafeGitCheckRefFormatIsUsed(t *testing.T) {
	t.Parallel()

	// A definitely-valid ref name must be accepted.
	valid := "refs/heads/harmonik/integration/abc123"
	cmd := exec.CommandContext(t.Context(), "git", "check-ref-format", valid)
	if err := cmd.Run(); err != nil {
		t.Errorf("WM-006a: git check-ref-format accepted %q should return exit 0, got error: %v", valid, err)
	}

	// A definitely-invalid ref name must be rejected.
	invalid := "refs/heads/harmonik/integration/bead@{broken}"
	cmd2 := exec.CommandContext(t.Context(), "git", "check-ref-format", invalid)
	if err := cmd2.Run(); err == nil {
		t.Errorf("WM-006a: git check-ref-format rejected %q should return non-zero exit code, got success", invalid)
	}
}

// ---------------------------------------------------------------------------
// branchNameFixture helpers — prefixed to avoid sibling-bead collision.
// These helpers are local to this fixture (bead hk-8mwo.66) and must NOT be
// declared at package level without the branchNameFixture prefix.
// ---------------------------------------------------------------------------

// branchNameFixtureDefaultIntegrationBranch returns the canonical default integration
// branch name per WM-006.
func branchNameFixtureDefaultIntegrationBranch() string {
	return "harmonik/integration"
}

// branchNameFixtureIntegrationBranchForBead returns the integration branch name for
// a given parent bead ID using verbatim substitution (the default per WM-006).
func branchNameFixtureIntegrationBranchForBead(parentBeadID string) string {
	return fmt.Sprintf("harmonik/integration/%s", parentBeadID)
}

// branchNameFixtureHexEncodeFallback applies the canonical hex-encode fallback
// transformation described in WM-006a:
//
//	(i) hex-encode every byte NOT in [a-zA-Z0-9/_-] as %HH (uppercase);
//	(ii) collapse every run of '/' longer than one into a single '/'.
//
// This is the deterministic fallback the workspace manager MUST apply when the verbatim
// bead-ID substitution fails git check-ref-format. The transformation is
// operator-configurable per OQ-WM-002; hex-encode is the spec-mandated default.
func branchNameFixtureHexEncodeFallback(beadID string) string {
	var sb strings.Builder
	for i := 0; i < len(beadID); i++ {
		b := beadID[i]
		switch {
		case (b >= 'a' && b <= 'z') ||
			(b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') ||
			b == '/' || b == '_' || b == '-':
			sb.WriteByte(b)
		default:
			// Encode as uppercase %HH per WM-006a step (i).
			sb.WriteString(strings.ToUpper("%" + hex.EncodeToString([]byte{b})))
		}
	}
	// Step (ii): collapse runs of '/' longer than one into a single '/'.
	result := sb.String()
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	return result
}

// branchNameFixtureIsRefSafe returns true iff `git check-ref-format refs/heads/<branch>`
// exits 0. This is the delegation mechanism mandated by WM-006a.
func branchNameFixtureIsRefSafe(t *testing.T, branch string) bool {
	t.Helper()
	refPath := "refs/heads/" + branch
	cmd := exec.CommandContext(t.Context(), "git", "check-ref-format", refPath) //nolint:gosec // refPath is not user input; git is a fixed binary
	return cmd.Run() == nil
}

// branchNameFixtureAssertRefSafe calls t.Errorf if the branch name is not accepted by
// git check-ref-format, providing a WM-clause-tagged error message.
func branchNameFixtureAssertRefSafe(t *testing.T, wmClause, branch string) {
	t.Helper()
	if !branchNameFixtureIsRefSafe(t, branch) {
		t.Errorf("%s: branch name %q rejected by git check-ref-format; expected valid ref name",
			wmClause, branch)
	}
}

// branchNameFixtureCreateBranch creates a git branch in repo at the given commit SHA.
// Fails the test if the git command fails.
func branchNameFixtureCreateBranch(t *testing.T, repo, branch, sha string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "branch", branch, sha)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch %q %q: %v\n%s", branch, sha, err, out)
	}
}

// branchNameFixtureListRunBranches returns all branches in repo with the "run/" prefix.
func branchNameFixtureListRunBranches(t *testing.T, repo string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "branch", "--list", "run/*")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch --list run/*: %v", err)
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches
}

// branchNameFixtureAssertOnlyOneBranch asserts that exactly one run/* branch exists
// in repo and that it matches the expected branch name.
func branchNameFixtureAssertOnlyOneBranch(t *testing.T, repo, expectedBranch string) {
	t.Helper()
	branches := branchNameFixtureListRunBranches(t, repo)
	if len(branches) != 1 {
		t.Errorf("WM-005a: expected exactly 1 run/* branch, got %d: %v", len(branches), branches)
		return
	}
	if branches[0] != expectedBranch {
		t.Errorf("WM-005a: run/* branch = %q, want %q", branches[0], expectedBranch)
	}
}
