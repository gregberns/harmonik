package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM019a_ScratchMergeWorktreeLifecycle verifies the full lifecycle (create-use-remove)
// of the scratch merge-worktree per option (b):
//   - created at <repo>/.harmonik/worktrees/merge-<merge_id>/
//   - squash-merge executed inside the scratch worktree
//   - worktree removed after merge commit; directory does not exist after removal
//   - git worktree list --porcelain does not list the scratch worktree after removal
//   - the transient merge-<merge_id> branch is also deleted
//
// Spec ref: workspace-model.md §4.5 WM-019a option (b) — "The merge node creates a
// dedicated short-lived worktree at `<repo>/.harmonik/worktrees/merge-<merge_id>/`
// ... executes the merge inside that worktree, and removes the worktree after the
// merge commit is created. Lifecycle: (i) `git worktree add -b merge-<merge_id>
// <merge_path> <integration_tip>`; (ii) `git merge --squash --strategy=ort
// <task_branch>`; ... (iv) `git commit` with trailers per WM-019; (v) ... ref-update;
// (vi) `git worktree remove --force <merge_path>` and `git worktree prune`; (vii)
// delete the transient `merge-<merge_id>` branch. The scratch worktree ... is NOT
// leased — it has no lease-lock file and does not participate in the §4.3 lease state
// machine."
func TestWM019a_ScratchMergeWorktreeLifecycle(t *testing.T) {
	t.Parallel()

	runID := "0196b100-0000-7000-8000-00000000019a"
	repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{
		"checkpoint: task node one",
		"checkpoint: task node two",
	})

	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create the integration branch at sha (the integration tip).
	integBranch := "harmonik/integration"
	gitRun(repo, "branch", integBranch, sha)

	taskBranch := "run/" + runID

	// Mint a merge_id (UUIDv7-shaped; filesystem-safe per WM-002 regex).
	mergeID := "0196b200-0000-7000-8000-000000000001"

	// (i) Create scratch merge-worktree at <repo>/.harmonik/worktrees/merge-<merge_id>/.
	scratchPath := filepath.Join(repo, ".harmonik", "worktrees", "merge-"+mergeID)
	if err := os.MkdirAll(filepath.Dir(scratchPath), 0o755); err != nil {
		t.Fatalf("MkdirAll scratch parent: %v", err)
	}
	scratchBranch := "merge-" + mergeID
	gitRun(repo, "worktree", "add", "-b", scratchBranch, scratchPath, sha)

	// Assert scratch path was created.
	if _, err := os.Stat(scratchPath); os.IsNotExist(err) {
		t.Fatalf("WM-019a: scratch worktree directory %q does not exist after worktree add", scratchPath)
	}

	// Assert scratch worktree appears in git worktree list before removal.
	listBefore, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("WM-019a: git worktree list (before): %v", err)
	}
	if !strings.Contains(string(listBefore), scratchPath) {
		t.Errorf("WM-019a: scratch path %q not in worktree list before removal:\n%s",
			scratchPath, listBefore)
	}

	// Assert scratch worktree has NO lease-lock file (not leased per WM-019a).
	leaseLockPath := filepath.Join(scratchPath, ".harmonik", "lease.lock")
	if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
		t.Errorf("WM-019a: scratch worktree MUST NOT have lease-lock at %q", leaseLockPath)
	}

	// (ii) Execute squash-merge inside scratch worktree.
	mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
	mergeCmd.Dir = scratchPath
	if out, err := mergeCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-019a: git merge --squash in scratch: %v\n%s", err, out)
	}

	// (iv) Commit with trailers per WM-019.
	commitMsg := "squash: scratch merge\n\nHarmonik-Run-ID: " + runID
	daemonName := "Harmonik Daemon"
	daemonEmail := "no-reply@harmonik.local"
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = scratchPath
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+daemonName,
		"GIT_AUTHOR_EMAIL="+daemonEmail,
		"GIT_COMMITTER_NAME="+daemonName,
		"GIT_COMMITTER_EMAIL="+daemonEmail,
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-019a: git commit in scratch: %v\n%s", err, out)
	}

	// (v) Update the integration branch ref to point to the scratch branch tip
	// (equivalent ref-update: fast-forward integration to scratch commit).
	scratchTipOut, err := exec.Command("git", "-C", repo, "rev-parse", scratchBranch).Output()
	if err != nil {
		t.Fatalf("WM-019a: rev-parse scratch branch: %v", err)
	}
	scratchTip := strings.TrimSpace(string(scratchTipOut))
	gitRun(repo, "update-ref", "refs/heads/"+integBranch, scratchTip)

	// (vi) Remove scratch worktree and prune.
	gitRun(repo, "worktree", "remove", "--force", scratchPath)
	gitRun(repo, "worktree", "prune")

	// Assert scratch directory no longer exists on disk.
	if _, err := os.Stat(scratchPath); !os.IsNotExist(err) {
		t.Errorf("WM-019a: scratch worktree directory %q still exists after removal", scratchPath)
	}

	// Assert git worktree list --porcelain no longer lists the scratch path.
	listAfter, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("WM-019a: git worktree list (after): %v", err)
	}
	if strings.Contains(string(listAfter), scratchPath) {
		t.Errorf("WM-019a: scratch path %q still appears in worktree list after removal:\n%s",
			scratchPath, listAfter)
	}

	// (vii) Delete the transient merge-<merge_id> branch.
	gitRun(repo, "branch", "-D", scratchBranch)

	// Assert scratch branch is gone.
	out2, _ := exec.Command("git", "-C", repo, "rev-parse", "--verify", scratchBranch).Output()
	if strings.TrimSpace(string(out2)) != "" {
		t.Errorf("WM-019a: transient branch %q still exists after deletion", scratchBranch)
	}

	// Assert integration branch now has exactly ONE new commit relative to sha.
	countOut, err := exec.Command("git", "-C", repo, "rev-list", "--count",
		integBranch, "^"+sha).Output()
	if err != nil {
		t.Fatalf("WM-019a: rev-list count on integration: %v", err)
	}
	if strings.TrimSpace(string(countOut)) != "1" {
		t.Errorf("WM-019a: integration branch should have 1 new commit, got %s",
			strings.TrimSpace(string(countOut)))
	}
}
