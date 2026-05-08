package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM018_MergeBackNodeInSameWorktree verifies that the merge-back operation is
// performed by a workflow node that executes INSIDE the run's already-leased worktree —
// the merge uses the same worktree (same canonical path + same lease) as the task
// nodes, not a new workspace.
//
// Spec ref: workspace-model.md §4.5 WM-018 — "The merge-back operation (merging the
// task branch onto the integration branch) MUST be performed by a workflow node that
// executes INSIDE the run's already-leased worktree. The merge is NOT a new workspace;
// it is another node in the same run consuming the same lease per WM-010. A design that
// creates a new workspace for the merge step is forbidden."
func TestWM018_MergeBackNodeInSameWorktree(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196b100-0000-7000-8000-000000000018"
	taskBranch := "run/" + runID
	taskPath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create the task worktree (same worktree that nodes execute inside).
	gitCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitCmd(repo, "worktree", "add", "-b", taskBranch, taskPath, sha)

	// Simulate node work: add 2 checkpoint commits in the task worktree.
	mergeBackFixture_writeFile(t, taskPath, "nodeA.txt", "node-A work\n")
	gitCmd(taskPath, "add", "nodeA.txt")
	gitCmd(taskPath, "commit", "-m", "checkpoint: node A work")

	mergeBackFixture_writeFile(t, taskPath, "nodeB.txt", "node-B work\n")
	gitCmd(taskPath, "add", "nodeB.txt")
	gitCmd(taskPath, "commit", "-m", "checkpoint: node B work")

	// Assert the task branch now has 2 additional commits beyond the initial.
	out, err := exec.Command("git", "-C", taskPath, "rev-list", "--count", "HEAD", "^"+sha).Output()
	if err != nil {
		t.Fatalf("rev-list --count: %v", err)
	}
	count := strings.TrimSpace(string(out))
	if count != "2" {
		t.Errorf("WM-018: expected 2 node commits on task branch, got %s", count)
	}

	// The merge-back node runs INSIDE the same worktree (same canonical path).
	// Create integration branch from main (sha), merge from the task worktree.
	integBranch := "harmonik/integration"
	integPath := filepath.Join(repo, ".harmonik", "worktrees", "integ-"+runID)
	if err := os.MkdirAll(filepath.Dir(integPath), 0o755); err != nil {
		t.Fatalf("MkdirAll integ: %v", err)
	}
	gitCmd(repo, "worktree", "add", "-b", integBranch, integPath, sha)

	// Perform the squash-merge from the integration worktree (same repo, same lease
	// group). This mirrors the merge-back node executing inside the existing lease.
	mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
	mergeCmd.Dir = integPath
	if out, err := mergeCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-018: git merge --squash: %v\n%s", err, out)
	}

	commitMsg := "squash: run " + runID + "\n\nHarmonik-Run-ID: " + runID
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = integPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-018: git commit: %v\n%s", err, out)
	}

	// Assert: integration branch has exactly ONE new commit relative to sha.
	out2, err := exec.Command("git", "-C", integPath, "rev-list", "--count", "HEAD", "^"+sha).Output()
	if err != nil {
		t.Fatalf("WM-018: rev-list integration: %v", err)
	}
	integCount := strings.TrimSpace(string(out2))
	if integCount != "1" {
		t.Errorf("WM-018: integration branch must have exactly 1 new commit, got %s", integCount)
	}

	// Assert: task branch tip is UNCHANGED (squash does not advance it).
	taskTip, err := exec.Command("git", "-C", repo, "rev-parse", taskBranch).Output()
	if err != nil {
		t.Fatalf("WM-018: rev-parse task branch: %v", err)
	}
	integTip, err := exec.Command("git", "-C", repo, "rev-parse", integBranch).Output()
	if err != nil {
		t.Fatalf("WM-018: rev-parse integration branch: %v", err)
	}
	if strings.TrimSpace(string(taskTip)) == strings.TrimSpace(string(integTip)) {
		t.Errorf("WM-018: task branch tip and integration tip must differ (squash, not ff)")
	}
}

// mergeBackFixture_writeFile writes content to a file in dir, fataling the test on error.
// Prefixed mergeBackFixture_ per the same-package shared-symbol discipline documented
// in hk-8mwo.68: each bead's new helpers carry a bead-specific prefix to avoid
// post-merge `redeclared in this block` collisions with sibling implementers.
func mergeBackFixture_writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}
