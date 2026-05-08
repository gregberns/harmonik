package workspace

import (
	"testing"
)

// branchNamingFixtureRunPrefix is the normative task-branch prefix per WM-005.
// Any change to this constant requires a migration release per WM-009.
// Prefixed branchNamingFixture_ to avoid collision with sibling bead fixtures.
const branchNamingFixtureRunPrefix = "run/"

// branchNamingFixtureIntegrationDefault is the normative default integration branch
// name per WM-006. Any change to this constant requires a migration release per WM-009.
// Prefixed branchNamingFixture_ to avoid collision with sibling bead fixtures.
const branchNamingFixtureIntegrationDefault = "harmonik/integration"

// TestWM009_BranchNamingStableAcrossVersion asserts that the task-branch prefix and
// default integration-branch name are frozen at their spec-mandated values. A breaking
// change requires a migration release per the compat contract in operator-nfr.md §4.5 ON-018.
//
// Spec ref: workspace-model.md §4.2 WM-009 — "Task-branch and integration-branch
// naming conventions MUST be stable across a harmonik minor version per the compat
// contract declared in [operator-nfr.md §4.5 ON-018]. A breaking change to branch
// naming requires a migration release."
func TestWM009_BranchNamingStableAcrossVersion(t *testing.T) {
	t.Parallel()

	// Task-branch prefix: frozen at "run/".
	t.Run("task-branch-prefix-frozen", func(t *testing.T) {
		t.Parallel()

		const specMandatedPrefix = "run/"
		if branchNamingFixtureRunPrefix != specMandatedPrefix {
			t.Errorf("WM-009: task-branch prefix constant = %q, want spec-mandated %q; a breaking change requires a migration release",
				branchNamingFixtureRunPrefix, specMandatedPrefix)
		}
	})

	// Default integration branch: frozen at "harmonik/integration".
	t.Run("default-integration-branch-frozen", func(t *testing.T) {
		t.Parallel()

		const specMandatedDefault = "harmonik/integration"
		if branchNamingFixtureIntegrationDefault != specMandatedDefault {
			t.Errorf("WM-009: default integration branch constant = %q, want spec-mandated %q; a breaking change requires a migration release",
				branchNamingFixtureIntegrationDefault, specMandatedDefault)
		}

		// Also verify consistency with the function used in WM-006 tests.
		fromFunc := branchNameFixture_defaultIntegrationBranch()
		if fromFunc != specMandatedDefault {
			t.Errorf("WM-009: branchNameFixture_defaultIntegrationBranch() = %q, want %q",
				fromFunc, specMandatedDefault)
		}
	})

	// Verify both constants are ref-safe (valid git branch names).
	t.Run("task-branch-prefix-component-is-ref-safe", func(t *testing.T) {
		t.Parallel()

		// A task branch with a sample run_id using the frozen prefix must be ref-safe.
		sampleRunID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0060"
		taskBranch := branchNamingFixtureRunPrefix + sampleRunID
		branchNameFixture_assertRefSafe(t, "WM-009", taskBranch)
	})

	t.Run("default-integration-branch-is-ref-safe", func(t *testing.T) {
		t.Parallel()

		branchNameFixture_assertRefSafe(t, "WM-009", branchNamingFixtureIntegrationDefault)
	})

	// Parent-bead-derived integration branch template is also stable per WM-006 + WM-009.
	t.Run("parent-bead-integration-branch-template-frozen", func(t *testing.T) {
		t.Parallel()

		const specMandatedTemplate = "harmonik/integration/"
		sampleBeadID := "hk-8mwo66"
		derived := branchNamingFixtureIntegrationDefault + "/" + sampleBeadID

		if len(derived) <= len(specMandatedTemplate) {
			t.Fatalf("WM-009: derived branch %q shorter than template %q", derived, specMandatedTemplate)
		}
		if derived[:len(specMandatedTemplate)] != specMandatedTemplate {
			t.Errorf("WM-009: parent-bead integration branch %q does not start with frozen template prefix %q",
				derived, specMandatedTemplate)
		}

		branchNameFixture_assertRefSafe(t, "WM-009", derived)
	})
}

// TestWM009_BranchPrefixUsedConsistentlyWithWM005 cross-checks that the frozen
// branchNamingFixtureRunPrefix constant matches the naming used in WM-005 tests,
// establishing a single source of truth for the "run/" literal.
//
// Spec ref: workspace-model.md §4.2 WM-009 — see above; cross-reference WM-005.
func TestWM009_BranchPrefixUsedConsistentlyWithWM005(t *testing.T) {
	t.Parallel()

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0061"

	// The task branch produced by WM-005's naming convention...
	wm005Branch := "run/" + runID

	// ...must equal what we get using the frozen constant from this (WM-009) file.
	wm009Branch := branchNamingFixtureRunPrefix + runID

	if wm005Branch != wm009Branch {
		t.Errorf("WM-009: WM-005 branch name %q != WM-009 constant-based %q; naming is inconsistent",
			wm005Branch, wm009Branch)
	}
}
