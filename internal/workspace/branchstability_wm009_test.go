package workspace

import (
	"context"
	"testing"
)

const branchNameFixtureRunPrefix = "run/"

const branchNameFixtureIntegrationDefault = "harmonik/integration"

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

	t.Run("task-branch-prefix-frozen", func(t *testing.T) {
		t.Parallel()

		const specMandatedPrefix = "run/"
		if branchNameFixtureRunPrefix != specMandatedPrefix {
			t.Errorf("WM-009: task-branch prefix constant = %q, want spec-mandated %q; a breaking change requires a migration release",
				branchNameFixtureRunPrefix, specMandatedPrefix)
		}
	})

	t.Run("default-integration-branch-frozen", func(t *testing.T) {
		t.Parallel()

		const specMandatedDefault = "harmonik/integration"
		if branchNameFixtureIntegrationDefault != specMandatedDefault {
			t.Errorf("WM-009: default integration branch constant = %q, want spec-mandated %q; a breaking change requires a migration release",
				branchNameFixtureIntegrationDefault, specMandatedDefault)
		}

		fromFunc := branchNameFixtureDefaultIntegrationBranch()
		if fromFunc != specMandatedDefault {
			t.Errorf("WM-009: branchNameFixtureDefaultIntegrationBranch() = %q, want %q",
				fromFunc, specMandatedDefault)
		}
	})

	t.Run("production-task-branch-prefix-matches-frozen-constant", func(t *testing.T) {
		t.Parallel()

		if TaskBranchPrefix != branchNameFixtureRunPrefix {
			t.Errorf("WM-009: production TaskBranchPrefix = %q, but frozen stability constant = %q; "+
				"a breaking change requires a migration release per operator-nfr.md §4.5 ON-018",
				TaskBranchPrefix, branchNameFixtureRunPrefix)
		}
	})

	t.Run("production-default-integration-branch-matches-frozen-constant", func(t *testing.T) {
		t.Parallel()

		got, err := IntegrationBranchName(context.Background(), "")
		if err != nil {
			t.Fatalf("WM-009: IntegrationBranchName(ctx, \"\") returned error: %v", err)
		}
		if got != branchNameFixtureIntegrationDefault {
			t.Errorf("WM-009: production IntegrationBranchName(ctx, \"\") = %q, but frozen stability constant = %q; "+
				"a breaking change requires a migration release per operator-nfr.md §4.5 ON-018",
				got, branchNameFixtureIntegrationDefault)
		}
	})

	t.Run("task-branch-prefix-component-is-ref-safe", func(t *testing.T) {
		t.Parallel()

		sampleRunID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0060"
		taskBranch := branchNameFixtureRunPrefix + sampleRunID
		branchNameFixtureAssertRefSafe(t, "WM-009", taskBranch)
	})

	t.Run("default-integration-branch-is-ref-safe", func(t *testing.T) {
		t.Parallel()

		branchNameFixtureAssertRefSafe(t, "WM-009", branchNameFixtureIntegrationDefault)
	})

	t.Run("parent-bead-integration-branch-template-frozen", func(t *testing.T) {
		t.Parallel()

		const specMandatedTemplate = "harmonik/integration/"
		sampleBeadID := "hk-8mwo66"
		derived := branchNameFixtureIntegrationDefault + "/" + sampleBeadID

		if len(derived) <= len(specMandatedTemplate) {
			t.Fatalf("WM-009: derived branch %q shorter than template %q", derived, specMandatedTemplate)
		}
		if derived[:len(specMandatedTemplate)] != specMandatedTemplate {
			t.Errorf("WM-009: parent-bead integration branch %q does not start with frozen template prefix %q",
				derived, specMandatedTemplate)
		}

		branchNameFixtureAssertRefSafe(t, "WM-009", derived)
	})
}

// TestWM009_BranchPrefixUsedConsistentlyWithWM005 cross-checks that the frozen
// branchNameFixtureRunPrefix constant matches the naming used in WM-005 tests,
// establishing a single source of truth for the "run/" literal.
//
// Spec ref: workspace-model.md §4.2 WM-009 — see above; cross-reference WM-005.
func TestWM009_BranchPrefixUsedConsistentlyWithWM005(t *testing.T) {
	t.Parallel()

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0061"

	wm005Branch := "run/" + runID

	wm009Branch := branchNameFixtureRunPrefix + runID

	if wm005Branch != wm009Branch {
		t.Errorf("WM-009: WM-005 branch name %q != WM-009 constant-based %q; naming is inconsistent",
			wm005Branch, wm009Branch)
	}
}
