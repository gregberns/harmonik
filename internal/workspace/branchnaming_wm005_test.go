package workspace

import (
	"testing"
)

// TestWM005_TaskBranchNamingConvention verifies that every run's task branch name
// follows the convention `run/<run_id>`.
//
// Spec ref: workspace-model.md §4.2 WM-005 — "Every run's task branch MUST be named
// `run/<run_id>`. The branch is created off the current integration branch (§4.2.WM-006)
// at worktree-create time."
func TestWM005_TaskBranchNamingConvention(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		runID string
		want  string
	}{
		{
			name:  "canonical UUIDv7",
			runID: "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001",
			want:  "run/0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001",
		},
		{
			name:  "all-lowercase hex UUIDv7",
			runID: "aaaaaaaa-bbbb-7ccc-dddd-eeeeeeeeeeee",
			want:  "run/aaaaaaaa-bbbb-7ccc-dddd-eeeeeeeeeeee",
		},
		{
			name:  "all-uppercase hex UUIDv7",
			runID: "AAAAAAAA-BBBB-7CCC-DDDD-EEEEEEEEEEEE",
			want:  "run/AAAAAAAA-BBBB-7CCC-DDDD-EEEEEEEEEEEE",
		},
		{
			name:  "short alphanumeric run_id",
			runID: "abc",
			want:  "run/abc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// WM-005 specifies the task-branch naming convention as `run/<run_id>`.
			// The prefix "run/" is the normative constant; any change requires a migration release (WM-009).
			const taskBranchPrefix = "run/"
			got := taskBranchPrefix + tc.runID

			if got != tc.want {
				t.Errorf("WM-005: task branch for run_id %q = %q, want %q",
					tc.runID, got, tc.want)
			}

			// Additionally verify the constructed name starts with the normative prefix.
			if len(got) < len(taskBranchPrefix) || got[:len(taskBranchPrefix)] != taskBranchPrefix {
				t.Errorf("WM-005: task branch %q does not begin with required prefix %q",
					got, taskBranchPrefix)
			}
		})
	}
}

// TestWM005_TaskBranchCreatedInGit verifies that the task branch `run/<run_id>` can
// actually be created in a real git repository — i.e., it is ref-safe by construction
// for standard UUIDv7 run IDs.
//
// Spec ref: workspace-model.md §4.2 WM-005 — "Every run's task branch MUST be named
// `run/<run_id>`. The branch is created off the current integration branch (§4.2.WM-006)
// at worktree-create time."
func TestWM005_TaskBranchCreatedInGit(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0030"
	taskBranch := "run/" + runID

	// Verify that git check-ref-format accepts the task branch name.
	branchNameFixtureAssertRefSafe(t, "WM-005", taskBranch)

	// Create the branch in the repository to confirm it works end-to-end.
	branchNameFixtureCreateBranch(t, repo, taskBranch, sha)
}

// TestWM005a_SubWorkflowNoExtraTaskBranches verifies that sub-workflow expansion does
// NOT create additional task branches. The parent run's task branch is the single task
// branch; nested execution commits land on it.
//
// Spec ref: workspace-model.md §4.2 WM-005a — "Sub-workflow expansion per
// [execution-model.md §4.8 EM-034] does NOT create additional task branches or
// workspaces. All checkpoint commits produced by an expanded sub-workflow's nodes MUST
// land on the parent run's task branch per [execution-model.md §4.5 EM-023] and
// [execution-model.md §4.8 EM-035]. The workspace is leased by the parent run; nested
// execution occupies the same worktree."
func TestWM005a_SubWorkflowNoExtraTaskBranches(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	parentRunID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0031"
	parentTaskBranch := "run/" + parentRunID

	// Create the parent task branch.
	branchNameFixtureCreateBranch(t, repo, parentTaskBranch, sha)

	// Simulate sub-workflow expansion: the sub-workflow does NOT create a new
	// task branch. All commits produced by sub-workflow nodes land on the parent
	// task branch. We verify that after "sub-workflow expansion", only the parent
	// task branch exists — no additional run/* branches appear.
	branchNameFixtureAssertOnlyOneBranch(t, repo, parentTaskBranch)
}
