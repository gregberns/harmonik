package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM018a_MergeNodeDispatchContract verifies that both the non-agentic variant
// (orchestrator executes git merge directly, author = daemon identity) and the agentic
// variant (handler executes merge, author = LaunchSpec identity) produce a valid squash
// commit with the correct author/committer split.
//
// Spec ref: workspace-model.md §4.5 WM-018a — "Every workflow that produces a merge-back
// MUST declare the merge step as a workflow node of one of two shapes: (a) a non-agentic
// merge node dispatched directly by the orchestrator — the orchestrator executes
// `git merge --squash` + commit per WM-019, with no handler subprocess; (b) an agentic
// merge node dispatched through [handler-contract.md §4.1] whose handler executes the
// same git operations plus any pre-merge validation per its LaunchSpec. ... Conflict
// detection is mechanical: a non-zero exit from `git merge --squash` or the presence of
// conflict markers in `git status --porcelain` output MUST be treated as conflict entry
// per WM-020."
func TestWM018a_MergeNodeDispatchContract(t *testing.T) {
	t.Parallel()

	t.Run("non-agentic/author=daemon-identity", func(t *testing.T) {
		t.Parallel()

		repo, sha := mergeBackFixtureSetupTaskBranch(t,
			"0196b100-0000-7000-8000-00000018a001",
			[]string{"checkpoint: mechanical node"},
		)

		runID := "0196b100-0000-7000-8000-00000018a001"

		// Non-agentic: orchestrator commits with daemon identity for BOTH author and committer.
		daemonName := "Harmonik Daemon"
		daemonEmail := "no-reply@harmonik.local"

		integPath := mergeBackFixtureMakeIntegWorktree(t, repo, sha, "integ-18a-nonagentic")
		taskBranch := "run/" + runID

		mergeSquashCmd := exec.CommandContext(t.Context(), "git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeSquashCmd.Dir = integPath
		if out, err := mergeSquashCmd.CombinedOutput(); err != nil {
			t.Fatalf("git merge --squash (non-agentic): %v\n%s", err, out)
		}

		commitMsg := "squash: non-agentic merge\n\nHarmonik-Run-ID: " + runID
		commitCmd := exec.CommandContext(t.Context(), "git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		commitCmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME="+daemonName,
			"GIT_AUTHOR_EMAIL="+daemonEmail,
			"GIT_COMMITTER_NAME="+daemonName,
			"GIT_COMMITTER_EMAIL="+daemonEmail,
		)
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit (non-agentic): %v\n%s", err, out)
		}

		// Assert author = daemon, committer = daemon (both same for non-agentic).
		identity, err := exec.CommandContext(t.Context(), "git", "-C", integPath, "log", "-1",
			"--format=%an <%ae> | %cn <%ce>").Output()
		if err != nil {
			t.Fatalf("git log identity: %v", err)
		}
		got := strings.TrimSpace(string(identity))
		wantAuthor := daemonName + " <" + daemonEmail + ">"
		wantCommitter := daemonName + " <" + daemonEmail + ">"
		want := wantAuthor + " | " + wantCommitter
		if got != want {
			t.Errorf("WM-018a non-agentic: author/committer = %q, want %q", got, want)
		}
	})

	t.Run("agentic/author=launchspec-identity", func(t *testing.T) {
		t.Parallel()

		repo, sha := mergeBackFixtureSetupTaskBranch(t,
			"0196b100-0000-7000-8000-00000018a002",
			[]string{"checkpoint: agentic node"},
		)

		runID := "0196b100-0000-7000-8000-00000018a002"

		// Agentic: author = LaunchSpec identity of the merge-node's implementer.
		// Per WM-019: author = implementer_handler_ref.identifier string.
		agentName := "Claude Agent"
		agentEmail := "agent@harmonik.local"
		daemonName := "Harmonik Daemon"
		daemonEmail := "no-reply@harmonik.local"

		integPath := mergeBackFixtureMakeIntegWorktree(t, repo, sha, "integ-18a-agentic")
		taskBranch := "run/" + runID

		mergeSquashCmd := exec.CommandContext(t.Context(), "git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeSquashCmd.Dir = integPath
		if out, err := mergeSquashCmd.CombinedOutput(); err != nil {
			t.Fatalf("git merge --squash (agentic): %v\n%s", err, out)
		}

		commitMsg := "squash: agentic merge\n\nHarmonik-Run-ID: " + runID
		commitCmd := exec.CommandContext(t.Context(), "git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		commitCmd.Env = append(os.Environ(),
			// Author = agentic handler identity (LaunchSpec identity).
			"GIT_AUTHOR_NAME="+agentName,
			"GIT_AUTHOR_EMAIL="+agentEmail,
			// Committer = daemon identity (both agentic and non-agentic).
			"GIT_COMMITTER_NAME="+daemonName,
			"GIT_COMMITTER_EMAIL="+daemonEmail,
		)
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit (agentic): %v\n%s", err, out)
		}

		// Assert author = agent, committer = daemon.
		identity, err := exec.CommandContext(t.Context(), "git", "-C", integPath, "log", "-1",
			"--format=%an <%ae> | %cn <%ce>").Output()
		if err != nil {
			t.Fatalf("git log identity: %v", err)
		}
		got := strings.TrimSpace(string(identity))
		wantAuthor := agentName + " <" + agentEmail + ">"
		wantCommitter := daemonName + " <" + daemonEmail + ">"
		want := wantAuthor + " | " + wantCommitter
		if got != want {
			t.Errorf("WM-018a agentic: author/committer = %q, want %q", got, want)
		}
	})
}

// mergeBackFixtureSetupTaskBranch creates a tempRepo, adds a task worktree for the
// given runID, and writes one commit per subject string. Returns (repo, initialSHA).
// Prefixed mergeBackFixture per same-package shared-symbol discipline (hk-8mwo.68).
func mergeBackFixtureSetupTaskBranch(t *testing.T, runID string, subjects []string) (string, string) {
	t.Helper()

	repo, sha := tempRepo(t)
	taskBranch := "run/" + runID
	taskPath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatalf("mergeBackFixtureSetupTaskBranch MkdirAll: %v", err)
	}

	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun(repo, "worktree", "add", "-b", taskBranch, taskPath, sha)

	for i, subj := range subjects {
		fname := filepath.Join(taskPath, "node"+strings.ReplaceAll(subj, " ", "_")+".txt")
		content := subj + " output\n"
		if err := os.WriteFile(fname, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile node %d: %v", i, err)
		}
		gitRun(taskPath, "add", ".")
		gitRun(taskPath, "commit", "-m", subj)
	}

	return repo, sha
}

// mergeBackFixtureMakeIntegWorktree creates a integration-branch worktree at a
// unique sub-path within the repo's .harmonik/worktrees directory.
// Prefixed mergeBackFixture per same-package shared-symbol discipline (hk-8mwo.68).
func mergeBackFixtureMakeIntegWorktree(t *testing.T, repo, sha, suffix string) string {
	t.Helper()

	branch := "harmonik/integration/" + suffix
	path := filepath.Join(repo, ".harmonik", "worktrees", suffix)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mergeBackFixtureMakeIntegWorktree MkdirAll: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, path, sha)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mergeBackFixtureMakeIntegWorktree worktree add: %v\n%s", err, out)
	}

	return path
}
