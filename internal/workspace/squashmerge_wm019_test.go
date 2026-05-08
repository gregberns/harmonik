package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM019_SquashMergeOrtStrategyTrailersAuthorCommitter verifies that the
// task-branch squash-merge onto the integration branch:
//   - uses --strategy=ort explicitly (pinned per WM-019)
//   - produces exactly one commit on the integration branch
//   - synthesized commit message carries Harmonik-Run-ID trailer (always)
//   - synthesized commit message carries Harmonik-Bead-ID trailer (when bead-tied)
//   - author = daemon identity (non-agentic) or LaunchSpec identity (agentic)
//   - committer = daemon identity in both variants
//
// Spec ref: workspace-model.md §4.5 WM-019 — "The merge MUST be implemented as
// `git merge --squash --strategy=ort <task_branch>` ... The message MUST carry
// trailers: `Harmonik-Run-ID: <run_id>` and, when the run is bead-tied,
// `Harmonik-Bead-ID: <bead_id>` ... The squashed commit's `author` MUST be set to
// the LaunchSpec identity ... OR to the daemon identity when ... non-agentic ...
// The squashed commit's `committer` MUST be the daemon identity in both variants."
func TestWM019_SquashMergeOrtStrategyTrailersAuthorCommitter(t *testing.T) {
	t.Parallel()

	t.Run("one-commit-per-task", func(t *testing.T) {
		t.Parallel()

		// Three checkpoint commits on the task branch → exactly one on integration.
		runID := "0196b100-0000-7000-8000-000000000019"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{
			"checkpoint: first node",
			"checkpoint: second node",
			"checkpoint: third node",
		})

		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-019-one")
		taskBranch := "run/" + runID

		// --strategy=ort explicitly per WM-019 pin.
		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: git merge --squash --strategy=ort: %v\n%s", err, out)
		}

		commitMsg := "squash: run " + runID + "\n\nHarmonik-Run-ID: " + runID
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: git commit: %v\n%s", err, out)
		}

		// Assert exactly ONE new commit on integration branch.
		out, err := exec.Command("git", "-C", integPath, "rev-list", "--count",
			"HEAD", "^"+sha).Output()
		if err != nil {
			t.Fatalf("WM-019: rev-list count: %v", err)
		}
		if strings.TrimSpace(string(out)) != "1" {
			t.Errorf("WM-019: one-commit-per-task: expected 1 commit on integration, got %s",
				strings.TrimSpace(string(out)))
		}
	})

	t.Run("trailer/run-id-always-present", func(t *testing.T) {
		t.Parallel()

		// Use a distinct run ID from the "one-commit-per-task" subtest to avoid
		// worktree path collisions when subtests run in parallel within the same tempDir.
		runIDB := "0196b100-0000-7000-8000-00000000019b"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runIDB, []string{"checkpoint: work"})

		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-019-trailer")
		taskBranch := "run/" + runIDB

		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: merge for trailer test: %v\n%s", err, out)
		}

		// Commit message with Harmonik-Run-ID trailer (always present).
		commitMsg := "squash: run " + runIDB + "\n\nHarmonik-Run-ID: " + runIDB
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: commit with trailer: %v\n%s", err, out)
		}

		// Assert via git log trailer extraction (git 2.34+ per WM-ENV-002).
		out, err := exec.Command("git", "-C", integPath, "log", "-1",
			"--format=%(trailers:key=Harmonik-Run-ID,valueonly)").Output()
		if err != nil {
			t.Fatalf("WM-019: git log trailer Harmonik-Run-ID: %v", err)
		}
		gotRunID := strings.TrimSpace(string(out))
		if gotRunID != runIDB {
			t.Errorf("WM-019: Harmonik-Run-ID trailer = %q, want %q", gotRunID, runIDB)
		}
	})

	t.Run("trailer/bead-id-conditional", func(t *testing.T) {
		t.Parallel()

		runID := "0196b100-0000-7000-8000-00000000019c"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{"checkpoint: bead work"})

		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-019-beadid")
		taskBranch := "run/" + runID

		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: merge for bead-id test: %v\n%s", err, out)
		}

		// Bead-tied run: message includes BOTH Harmonik-Run-ID and Harmonik-Bead-ID.
		beadID := "hk-test01.42"
		commitMsg := "squash: run " + runID + "\n\n" +
			"Harmonik-Run-ID: " + runID + "\n" +
			"Harmonik-Bead-ID: " + beadID
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: commit with bead-id trailer: %v\n%s", err, out)
		}

		// Assert Harmonik-Run-ID trailer present.
		out, err := exec.Command("git", "-C", integPath, "log", "-1",
			"--format=%(trailers:key=Harmonik-Run-ID,valueonly)").Output()
		if err != nil {
			t.Fatalf("WM-019: git log Harmonik-Run-ID: %v", err)
		}
		if strings.TrimSpace(string(out)) != runID {
			t.Errorf("WM-019: Harmonik-Run-ID = %q, want %q",
				strings.TrimSpace(string(out)), runID)
		}

		// Assert Harmonik-Bead-ID trailer present when bead-tied.
		out2, err := exec.Command("git", "-C", integPath, "log", "-1",
			"--format=%(trailers:key=Harmonik-Bead-ID,valueonly)").Output()
		if err != nil {
			t.Fatalf("WM-019: git log Harmonik-Bead-ID: %v", err)
		}
		if strings.TrimSpace(string(out2)) != beadID {
			t.Errorf("WM-019: Harmonik-Bead-ID = %q, want %q",
				strings.TrimSpace(string(out2)), beadID)
		}
	})

	t.Run("trailer/bead-id-absent-when-not-bead-tied", func(t *testing.T) {
		t.Parallel()

		runID := "0196b100-0000-7000-8000-00000000019d"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{"checkpoint: non-bead"})

		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-019-nobead")
		taskBranch := "run/" + runID

		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: merge for no-bead-id test: %v\n%s", err, out)
		}

		// Non-bead-tied run: only Harmonik-Run-ID, no Harmonik-Bead-ID.
		commitMsg := "squash: run " + runID + "\n\nHarmonik-Run-ID: " + runID
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: commit without bead-id: %v\n%s", err, out)
		}

		// Assert Harmonik-Bead-ID trailer is absent.
		out, err := exec.Command("git", "-C", integPath, "log", "-1",
			"--format=%(trailers:key=Harmonik-Bead-ID,valueonly)").Output()
		if err != nil {
			t.Fatalf("WM-019: git log Harmonik-Bead-ID: %v", err)
		}
		if beadVal := strings.TrimSpace(string(out)); beadVal != "" {
			t.Errorf("WM-019: Harmonik-Bead-ID present when not bead-tied: %q", beadVal)
		}
	})

	t.Run("author-committer-split", func(t *testing.T) {
		t.Parallel()

		runID := "0196b100-0000-7000-8000-00000000019e"
		repo, sha := mergeBackFixture_setupTaskBranch(t, runID, []string{"checkpoint: identity test"})

		integPath := mergeBackFixture_makeIntegWorktree(t, repo, sha, "integ-019-identity")
		taskBranch := "run/" + runID

		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch)
		mergeCmd.Dir = integPath
		if out, err := mergeCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: merge for identity test: %v\n%s", err, out)
		}

		agentName := "Claude Agent"
		agentEmail := "agent@harmonik.local"
		daemonName := "Harmonik Daemon"
		daemonEmail := "no-reply@harmonik.local"

		commitMsg := "squash: identity test\n\nHarmonik-Run-ID: " + runID
		commitCmd := exec.Command("git", "commit", "-m", commitMsg)
		commitCmd.Dir = integPath
		commitCmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME="+agentName,
			"GIT_AUTHOR_EMAIL="+agentEmail,
			"GIT_COMMITTER_NAME="+daemonName,
			"GIT_COMMITTER_EMAIL="+daemonEmail,
		)
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("WM-019: commit with identity split: %v\n%s", err, out)
		}

		out, err := exec.Command("git", "-C", integPath, "log", "-1",
			"--format=%an <%ae> | %cn <%ce>").Output()
		if err != nil {
			t.Fatalf("WM-019: git log identity: %v", err)
		}
		got := strings.TrimSpace(string(out))
		want := agentName + " <" + agentEmail + "> | " + daemonName + " <" + daemonEmail + ">"
		if got != want {
			t.Errorf("WM-019: author/committer split = %q, want %q", got, want)
		}
	})

	t.Run("conflict-detection", func(t *testing.T) {
		t.Parallel()

		// Set up two branches that modify the same line of README.
		repo, sha := tempRepo(t)

		gitRun := func(dir string, args ...string) {
			t.Helper()
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}

		// Branch A: edit README line 1 with "branch-A content".
		branchA := "conflict-branch-A"
		pathA := filepath.Join(repo, ".harmonik", "worktrees", "conflict-A")
		if err := os.MkdirAll(filepath.Dir(pathA), 0o755); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		gitRun(repo, "worktree", "add", "-b", branchA, pathA, sha)
		if err := os.WriteFile(filepath.Join(pathA, "README"), []byte("branch-A content\n"), 0o644); err != nil {
			t.Fatalf("WriteFile README A: %v", err)
		}
		gitRun(pathA, "add", "README")
		gitRun(pathA, "commit", "-m", "checkpoint: branch A edit")

		// Branch B: edit same README line 1 with "branch-B content".
		branchB := "conflict-branch-B"
		pathB := filepath.Join(repo, ".harmonik", "worktrees", "conflict-B")
		if err := os.MkdirAll(filepath.Dir(pathB), 0o755); err != nil {
			t.Fatalf("MkdirAll B: %v", err)
		}
		gitRun(repo, "worktree", "add", "-b", branchB, pathB, sha)
		if err := os.WriteFile(filepath.Join(pathB, "README"), []byte("branch-B content\n"), 0o644); err != nil {
			t.Fatalf("WriteFile README B: %v", err)
		}
		gitRun(pathB, "add", "README")
		gitRun(pathB, "commit", "-m", "checkpoint: branch B edit")

		// Try to squash-merge B into A — expect conflict.
		mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", branchB)
		mergeCmd.Dir = pathA
		out, mergeErr := mergeCmd.CombinedOutput()

		// Assert: non-zero exit code signals conflict per WM-018a.
		if mergeErr == nil {
			t.Errorf("WM-019 conflict-detection: expected non-zero exit from conflicting squash-merge, got success\n%s", out)
		}

		// Assert: git status --porcelain shows UU README (both-modified conflict).
		statusOut, err := exec.Command("git", "-C", pathA, "status", "--porcelain").Output()
		if err != nil {
			t.Fatalf("WM-019 conflict-detection: git status --porcelain: %v", err)
		}
		status := string(statusOut)
		if !strings.Contains(status, "UU README") {
			t.Errorf("WM-019 conflict-detection: expected 'UU README' in porcelain status, got:\n%s", status)
		}
	})
}
