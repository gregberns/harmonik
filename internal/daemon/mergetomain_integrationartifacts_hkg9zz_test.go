package daemon_test

// mergetomain_integrationartifacts_hkg9zz_test.go — regression test for the
// pre-rebase integration-artifact cleanup added in hk-g9zz.
//
// Bug (hk-g9zz): a live DOT-mode run for bead hk-nlio
// (TestIntegration_TwinE2E_OperatorRealEnv, 10.9s real tmux) completed with all
// 4 reviewers APPROVE, but the daemon merge then FAILED with:
//
//	merge-failed (dot): rebase_conflict: exit status 1
//	error: The following untracked working tree files would be overwritten by
//	checkout: <file>
//	Please move or remove them before you switch branches.
//
// ROOT CAUSE: the bead's //go:build integration test booths a real tmux pane
// and may build helper binaries (e.g. go build -o harmonik-twin-session) in
// the run worktree. These untracked files survive discardDirtyChurn (which only
// restores tracked churn paths) and commitResidualDelta (which commits genuine
// authored NEW files but may not commit all artifacts). When the rebase tries
// to replay a main-branch commit that adds a file at the same path, git aborts.
//
// FIX: cleanUntrackedFiles runs `git clean -fd` after commitResidualDelta and
// before the rebase, removing any remaining untracked non-gitignored files.
// At that point all genuine authored work is already committed, so the only
// surviving untracked files are integration-test artifacts.
//
// Bead: hk-g9zz.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// integArtifactGit runs a git command in dir and fails the test on error.
func integArtifactGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// integArtifactSetup creates:
//
//	main repo  — initial commit with code.txt + tracker.txt
//	run worktree — branched from main; agent commits code.txt change
//	main advance  — main gains a new commit that adds artifact.bin
//	               (simulating a file at the same path the integration test
//	               binary might leave untracked in the worktree)
//
// Returns (mainRepo, wtPath).
func integArtifactSetup(t *testing.T) (string, string) {
	t.Helper()

	mainRepo := t.TempDir()
	integArtifactGit(t, mainRepo, "init", "--initial-branch=main", ".")
	integArtifactGit(t, mainRepo, "config", "user.email", "t@t.com")
	integArtifactGit(t, mainRepo, "config", "user.name", "t")

	// Initial commit: code.txt + tracker.txt (two tracked files to make the
	// rebase have real content to replay).
	writeFile(t, filepath.Join(mainRepo, "code.txt"), "initial code\n")
	writeFile(t, filepath.Join(mainRepo, "tracker.txt"), "tracker v1\n")
	integArtifactGit(t, mainRepo, "add", "-A")
	integArtifactGit(t, mainRepo, "commit", "-m", "init")
	baseSHA := integArtifactGit(t, mainRepo, "rev-parse", "HEAD")

	// Run worktree branched from the base commit.
	wtPath := filepath.Join(t.TempDir(), "wt")
	integArtifactGit(t, mainRepo, "worktree", "add", "-b", "runbranch", wtPath, baseSHA)

	// Agent commit on the run branch (code-only, clean).
	writeFile(t, filepath.Join(wtPath, "code.txt"), "initial code\nagent fix\n")
	integArtifactGit(t, wtPath, "add", "code.txt")
	integArtifactGit(t, wtPath, "commit", "-m", "agent: fix code.txt")

	// Advance main: add artifact.bin — a file whose name collides with the
	// integration-test artifact we will leave untracked in the run worktree.
	writeFile(t, filepath.Join(mainRepo, "artifact.bin"), "binary content\n")
	integArtifactGit(t, mainRepo, "add", "artifact.bin")
	integArtifactGit(t, mainRepo, "commit", "-m", "main: add artifact.bin")

	return mainRepo, wtPath
}

// TestCleanUntrackedFiles_AllowsRebase is the hk-g9zz regression: an untracked
// non-gitignored file in the run worktree at a path that would be overwritten
// by a main-branch commit causes `git rebase main` to abort with "The following
// untracked working tree files would be overwritten by checkout". After
// cleanUntrackedFiles removes the file, the rebase succeeds.
func TestCleanUntrackedFiles_AllowsRebase(t *testing.T) {
	t.Parallel()

	_, wtPath := integArtifactSetup(t)

	// Simulate an integration-test artifact: leave artifact.bin untracked in
	// the run worktree. This is the file that main's latest commit also adds,
	// so rebase would overwrite it and git aborts.
	writeFile(t, filepath.Join(wtPath, "artifact.bin"), "stale integration artifact\n")

	// Sanity: the worktree has an untracked artifact.bin.
	status := integArtifactGit(t, wtPath, "status", "--porcelain")
	if !strings.Contains(status, "artifact.bin") {
		t.Fatalf("precondition: expected untracked artifact.bin; got status:\n%s", status)
	}

	// Without the fix, `git rebase main` would fail because artifact.bin is
	// untracked and would be overwritten by the main-branch commit that adds it.
	rebaseBefore := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseBefore.Dir = wtPath
	if out, err := rebaseBefore.CombinedOutput(); err == nil {
		t.Fatal("precondition: expected rebase to FAIL with untracked artifact.bin; it succeeded unexpectedly")
	} else if !strings.Contains(string(out), "artifact.bin") {
		t.Fatalf("precondition: expected rebase failure to mention artifact.bin; got:\n%s", out)
	}
	// Abort the failed rebase so the worktree is not left mid-rebase.
	abortCmd := exec.CommandContext(t.Context(), "git", "rebase", "--abort")
	abortCmd.Dir = wtPath
	_ = abortCmd.Run()

	// Apply the fix.
	daemon.ExportedCleanUntrackedFiles(context.Background(), wtPath)

	// After clean, artifact.bin must be gone from the worktree.
	if _, err := os.Stat(filepath.Join(wtPath, "artifact.bin")); err == nil {
		t.Fatal("cleanUntrackedFiles should have removed artifact.bin; it still exists")
	}

	// The worktree must now be clean (no untracked non-gitignored files).
	if status := integArtifactGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after cleanUntrackedFiles: expected clean worktree; got:\n%s", status)
	}

	// And the rebase must now succeed.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, err := rebaseCmd.CombinedOutput(); err != nil {
		t.Fatalf("git rebase main after cleanUntrackedFiles: %v\n%s", err, out)
	}
}

// TestCleanUntrackedFiles_NoOpOnCleanWorktree verifies that cleanUntrackedFiles
// is a no-op when the worktree has no untracked files and does not perturb a
// committed worktree state.
func TestCleanUntrackedFiles_NoOpOnCleanWorktree(t *testing.T) {
	t.Parallel()

	_, wtPath := integArtifactSetup(t)

	// Worktree has a committed agent change but no untracked files.
	beforeStatus := integArtifactGit(t, wtPath, "status", "--porcelain")
	if beforeStatus != "" {
		t.Fatalf("precondition: expected clean worktree; got:\n%s", beforeStatus)
	}

	daemon.ExportedCleanUntrackedFiles(context.Background(), wtPath)

	afterStatus := integArtifactGit(t, wtPath, "status", "--porcelain")
	if beforeStatus != afterStatus {
		t.Errorf("cleanUntrackedFiles must be a no-op on a clean worktree; before=%q after=%q",
			beforeStatus, afterStatus)
	}

	// Committed work must be intact.
	content, err := os.ReadFile(filepath.Join(wtPath, "code.txt"))
	if err != nil {
		t.Fatalf("ReadFile code.txt: %v", err)
	}
	if !strings.Contains(string(content), "agent fix") {
		t.Errorf("cleanUntrackedFiles must NOT discard committed agent work; code.txt=%q", string(content))
	}
}

// TestCleanUntrackedFiles_PreservesGitignored verifies that cleanUntrackedFiles
// leaves gitignored files in place (only non-gitignored files are cleaned).
// Gitignored artifacts (*.test, build outputs) do not affect the rebase and
// must not be removed by `git clean -fd` (which honours .gitignore).
func TestCleanUntrackedFiles_PreservesGitignored(t *testing.T) {
	t.Parallel()

	_, wtPath := integArtifactSetup(t)

	// Write a .gitignore that ignores keeper.test.
	writeFile(t, filepath.Join(wtPath, ".gitignore"), "keeper.test\n")
	integArtifactGit(t, wtPath, "add", ".gitignore")
	integArtifactGit(t, wtPath, "commit", "-m", "add .gitignore")

	// Leave a gitignored file keeper.test and a non-gitignored artifact.txt.
	writeFile(t, filepath.Join(wtPath, "keeper.test"), "test binary\n")
	writeFile(t, filepath.Join(wtPath, "artifact.txt"), "integration artifact\n")

	daemon.ExportedCleanUntrackedFiles(context.Background(), wtPath)

	// Non-gitignored artifact.txt must be removed.
	if _, err := os.Stat(filepath.Join(wtPath, "artifact.txt")); err == nil {
		t.Error("cleanUntrackedFiles should have removed non-gitignored artifact.txt; it still exists")
	}

	// Gitignored keeper.test must remain (git clean -fd honours .gitignore).
	if _, err := os.Stat(filepath.Join(wtPath, "keeper.test")); err != nil {
		t.Error("cleanUntrackedFiles must NOT remove gitignored keeper.test; it was deleted")
	}
}
