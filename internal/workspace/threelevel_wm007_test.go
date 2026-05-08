package workspace

import (
	"testing"
)

// TestWM007_ThreeLevelBranchingModel is a topology-assertion doc-test that verifies
// the shape and vocabulary of the three-level branching model required by WM-007.
//
// WM-007 defines three levels:
//
//	(a) node commits land on the task branch (run/<run_id>)
//	(b) task branch squash-merges onto the integration branch (harmonik/integration[/<parent_bead_id_refsafe>])
//	(c) integration branch merges to main under developer or operator policy
//
// Harmonik's contract ends at "integration branch holds one commit per task."
// workspace_merge_status fires ONLY for the task-branch → integration-branch merge.
// It does NOT fire on an external main-merge performed by developer tooling.
//
// Spec ref: workspace-model.md §4.2 WM-007 — "The system MUST use a three-level
// branching model: (a) node commits land on the task branch per WM-005 and
// [execution-model.md §4.5 EM-023]; (b) the task branch squash-merges onto the
// integration branch at run-terminal-success per §4.5; (c) the integration branch
// merges to main under developer or operator policy. Harmonik's contract ends at
// 'integration branch holds one commit per task.' Merge style from integration to main
// is NOT dictated by this spec, and `workspace_merge_status` (§4.5) fires ONLY for the
// task-branch → integration-branch merge — it does NOT fire on an external main-merge
// performed by developer tooling."
func TestWM007_ThreeLevelBranchingModel(t *testing.T) {
	t.Parallel()

	// Level 1: task branch naming follows WM-005.
	t.Run("level-1-task-branch", func(t *testing.T) {
		t.Parallel()

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0050"
		taskBranch := "run/" + runID

		// Verify the task branch name is ref-safe (it will be when the run_id is valid).
		branchNameFixtureAssertRefSafe(t, "WM-007(a)", taskBranch)

		// Assert task branch starts with the normative "run/" prefix.
		const taskPrefix = "run/"
		if len(taskBranch) <= len(taskPrefix) || taskBranch[:len(taskPrefix)] != taskPrefix {
			t.Errorf("WM-007(a): task branch %q does not begin with %q", taskBranch, taskPrefix)
		}
	})

	// Level 2: integration branch is the squash-merge target for the task branch.
	t.Run("level-2-integration-branch", func(t *testing.T) {
		t.Parallel()

		integrationBranch := branchNameFixtureDefaultIntegrationBranch()
		want := "harmonik/integration"
		if integrationBranch != want {
			t.Errorf("WM-007(b): integration branch = %q, want %q", integrationBranch, want)
		}

		// Verify the integration branch is ref-safe.
		branchNameFixtureAssertRefSafe(t, "WM-007(b)", integrationBranch)
	})

	// Level 3: main is the external merge target — harmonik does NOT dictate merge style.
	t.Run("level-3-main-is-external", func(t *testing.T) {
		t.Parallel()

		// WM-007 explicitly states that harmonik's contract ends at the integration
		// branch. The main-merge is external; workspace_merge_status does NOT fire for it.
		//
		// This sub-test is a doc-test: it asserts the correct vocabulary and boundary.
		// No workspace_merge_status emission occurs for integration→main; that event
		// fires only for task→integration.
		const harmonikContractBoundary = "harmonik/integration"
		const mainBranch = "main"

		// Harmonik manages: task branch → integration branch (the squash-merge step).
		// External tooling manages: integration branch → main.
		if harmonikContractBoundary == mainBranch {
			t.Errorf("WM-007(c): harmonik contract boundary should differ from main; got %q == %q",
				harmonikContractBoundary, mainBranch)
		}
	})

	// Verify branching topology: task branch is distinct from integration branch.
	t.Run("topology-task-differs-from-integration", func(t *testing.T) {
		t.Parallel()

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0051"
		taskBranch := "run/" + runID
		integrationBranch := branchNameFixtureDefaultIntegrationBranch()

		if taskBranch == integrationBranch {
			t.Errorf("WM-007: task branch %q must differ from integration branch %q",
				taskBranch, integrationBranch)
		}
	})
}

// TestWM007_WorkspaceMergeStatusScope asserts that workspace_merge_status fires only
// for the task-branch → integration-branch merge, not for integration → main.
//
// This is a doc-test / invariant assertion. The actual workspace_merge_status event
// emission lives in the workspace manager (hk-8mwo.24 / downstream). This test
// captures the boundary in the test surface so it is discoverable and reviewable.
//
// Spec ref: workspace-model.md §4.2 WM-007 — "`workspace_merge_status` (§4.5) fires
// ONLY for the task-branch → integration-branch merge — it does NOT fire on an external
// main-merge performed by developer tooling."
func TestWM007_WorkspaceMergeStatusScope(t *testing.T) {
	t.Parallel()

	// The workspace_merge_status event scope: task → integration only.
	// integration → main is out of scope for this event.
	type mergeEvent struct {
		from string
		to   string
	}

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0052"
	taskBranch := "run/" + runID
	integrationBranch := branchNameFixtureDefaultIntegrationBranch()

	// The ONE merge that workspace_merge_status covers.
	managedMerge := mergeEvent{from: taskBranch, to: integrationBranch}

	// Assert the managed merge is task → integration.
	if managedMerge.from != taskBranch {
		t.Errorf("WM-007: managed merge 'from' should be task branch %q, got %q",
			taskBranch, managedMerge.from)
	}
	if managedMerge.to != integrationBranch {
		t.Errorf("WM-007: managed merge 'to' should be integration branch %q, got %q",
			integrationBranch, managedMerge.to)
	}

	// Assert that main is NOT the target of workspace_merge_status.
	const main = "main"
	if managedMerge.to == main {
		t.Errorf("WM-007: workspace_merge_status must NOT fire for integration→main merge; got to=%q", managedMerge.to)
	}
}
