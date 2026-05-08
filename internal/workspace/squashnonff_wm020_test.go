package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM020_SquashMergeIsNonFastForwardByConstruction verifies that a squash-merge is
// non-fast-forward by construction:
//   - the integration branch tip advances to a NEW commit (not the task-branch tip)
//   - the task-branch tip is UNCHANGED after the squash
//   - the new integration tip is NOT an ancestor of the task-branch tip
//   - the task-branch tip is NOT an ancestor of the integration tip (it is a new commit)
//
// Spec ref: workspace-model.md §4.5 WM-020 — "A squash-merge (§4.5.WM-019) is
// non-fast-forward by its nature: `git merge --squash` produces a single new commit
// on the integration branch whose tree equals the merged result but whose parent is
// the integration-branch tip (not the task-branch tip). The integration-branch tip
// therefore ADVANCES to a new commit containing the squashed content; the task-branch
// tip is UNCHANGED and is NOT an ancestor of the new integration-branch tip."
func TestWM020_SquashMergeIsNonFastForwardByConstruction(t *testing.T) {
	t.Parallel()

	runID := "0196b100-0000-7000-8000-000000000020"
	repo, sha := mergeBackFixtureSetupTaskBranch(t, runID, []string{
		"checkpoint: node one",
		"checkpoint: node two",
	})

	taskBranch := "run/" + runID
	integPath := mergeBackFixtureMakeIntegWorktree(t, repo, sha, "integ-020-nonff")

	// Record task branch tip before merge (task branch has 2 checkpoint commits above sha).
	taskTipBefore, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", taskBranch).Output()
	if err != nil {
		t.Fatalf("WM-020: rev-parse task branch before merge: %v", err)
	}
	taskTip := strings.TrimSpace(string(taskTipBefore))

	// Task branch tip must differ from sha (it has commits on top).
	if taskTip == sha {
		t.Fatalf("WM-020: task branch tip equals sha — fixture did not add commits")
	}

	// Record integration branch tip before merge (starts at sha = integration-branch origin).
	integBranch := mergeBackFixtureIntegBranchName("integ-020-nonff")
	integTipBefore, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", integBranch).Output()
	if err != nil {
		t.Fatalf("WM-020: rev-parse integration before merge: %v", err)
	}
	integBefore := strings.TrimSpace(string(integTipBefore))

	// Integration branch starts at sha (no commits beyond the initial one).
	if integBefore != sha {
		t.Errorf("WM-020: integration tip before merge = %q, want sha %q", integBefore, sha)
	}

	// Perform squash-merge.
	mergeCmd := exec.CommandContext(t.Context(), "git", "merge", "--squash", "--strategy=ort", taskBranch)
	mergeCmd.Dir = integPath
	if out, err := mergeCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-020: git merge --squash: %v\n%s", err, out)
	}

	commitMsg := "squash: non-ff test\n\nHarmonik-Run-ID: " + runID
	commitCmd := exec.CommandContext(t.Context(), "git", "commit", "-m", commitMsg)
	commitCmd.Dir = integPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-020: git commit: %v\n%s", err, out)
	}

	// Assert: task branch tip is UNCHANGED after squash.
	taskTipAfter, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", taskBranch).Output()
	if err != nil {
		t.Fatalf("WM-020: rev-parse task branch after merge: %v", err)
	}
	if strings.TrimSpace(string(taskTipAfter)) != taskTip {
		t.Errorf("WM-020: task branch tip changed after squash-merge: before=%q after=%q",
			taskTip, strings.TrimSpace(string(taskTipAfter)))
	}

	// Assert: integration tip ADVANCED to a new commit (not sha).
	integTipAfter, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", integBranch).Output()
	if err != nil {
		t.Fatalf("WM-020: rev-parse integration after merge: %v", err)
	}
	integAfter := strings.TrimSpace(string(integTipAfter))
	if integAfter == sha {
		t.Errorf("WM-020: integration tip did not advance (still at sha=%q)", sha)
	}

	// Assert: integration tip != task branch tip (squash creates a new commit).
	if integAfter == taskTip {
		t.Errorf("WM-020: integration tip == task branch tip %q — "+
			"squash-merge must produce a new commit, not point at task tip", taskTip)
	}

	// Assert: task branch tip is NOT an ancestor of integration tip.
	// git merge-base --is-ancestor <commit> <branch> exits 0 if it IS an ancestor.
	// We expect it to exit non-zero (task tip is not an ancestor of new integ tip).
	ancestorCmd := exec.CommandContext(t.Context(), "git", "-C", repo, "merge-base", "--is-ancestor",
		taskTip, integBranch)
	if err := ancestorCmd.Run(); err == nil {
		t.Errorf("WM-020: task branch tip %q is an ancestor of integration %q — "+
			"squash-merge should not produce a ff-able relationship", taskTip, integBranch)
	}

	// Assert: integration tip has exactly one parent = sha (the pre-merge integ tip).
	parentOut, err := exec.CommandContext(t.Context(), "git", "-C", repo, "log", "-1",
		"--format=%P", integBranch).Output()
	if err != nil {
		t.Fatalf("WM-020: git log parent of integration tip: %v", err)
	}
	parent := strings.TrimSpace(string(parentOut))
	if parent != sha {
		t.Errorf("WM-020: squash commit parent = %q, want initial sha %q", parent, sha)
	}

	// Verify scratch worktree for the integration branch was created with MkdirAll.
	if _, err := os.Stat(integPath); os.IsNotExist(err) {
		t.Errorf("WM-020: integration worktree path %q does not exist", integPath)
	}
}

// mergeBackFixtureIntegBranchName returns the integration branch name for a given suffix,
// matching the naming used by mergeBackFixtureMakeIntegWorktree.
// Prefixed mergeBackFixture per same-package shared-symbol discipline (hk-8mwo.68).
func mergeBackFixtureIntegBranchName(suffix string) string {
	return "harmonik/integration/" + suffix
}

// mergeBackFixtureMakeWorktreePath returns the worktree path for a given repo and suffix,
// matching the path used by mergeBackFixtureMakeIntegWorktree.
// Prefixed mergeBackFixture per same-package shared-symbol discipline (hk-8mwo.68).
func mergeBackFixtureMakeWorktreePath(repo, suffix string) string {
	return filepath.Join(repo, ".harmonik", "worktrees", suffix)
}
