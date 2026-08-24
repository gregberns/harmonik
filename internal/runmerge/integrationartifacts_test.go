package runmerge_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

func integArtifactSetup(t *testing.T) (wtPath string) {
	t.Helper()

	mainRepo := t.TempDir()
	integArtifactGit(t, mainRepo, "init", "--initial-branch=main", ".")
	integArtifactGit(t, mainRepo, "config", "user.email", "t@t.com")
	integArtifactGit(t, mainRepo, "config", "user.name", "t")

	writeFile(t, filepath.Join(mainRepo, "code.txt"), "initial code\n")
	writeFile(t, filepath.Join(mainRepo, "tracker.txt"), "tracker v1\n")
	integArtifactGit(t, mainRepo, "add", "-A")
	integArtifactGit(t, mainRepo, "commit", "-m", "init")
	baseSHA := integArtifactGit(t, mainRepo, "rev-parse", "HEAD")

	wtPath = filepath.Join(t.TempDir(), "wt")
	integArtifactGit(t, mainRepo, "worktree", "add", "-b", "runbranch", wtPath, baseSHA)

	writeFile(t, filepath.Join(wtPath, "code.txt"), "initial code\nagent fix\n")
	integArtifactGit(t, wtPath, "add", "code.txt")
	integArtifactGit(t, wtPath, "commit", "-m", "agent: fix code.txt")

	writeFile(t, filepath.Join(mainRepo, "artifact.bin"), "binary content\n")
	integArtifactGit(t, mainRepo, "add", "artifact.bin")
	integArtifactGit(t, mainRepo, "commit", "-m", "main: add artifact.bin")

	return wtPath
}

// TestCleanUntrackedFiles_AllowsRebase is the hk-g9zz regression: an untracked
// non-gitignored file in the run worktree at a path that would be overwritten
// by a main-branch commit causes `git rebase main` to abort with "The following
// untracked working tree files would be overwritten by checkout". After
// cleanUntrackedFiles removes the file, the rebase succeeds.
func TestCleanUntrackedFiles_AllowsRebase(t *testing.T) {
	t.Parallel()

	wtPath := integArtifactSetup(t)

	writeFile(t, filepath.Join(wtPath, "artifact.bin"), "stale integration artifact\n")

	status := integArtifactGit(t, wtPath, "status", "--porcelain")
	if !strings.Contains(status, "artifact.bin") {
		t.Fatalf("precondition: expected untracked artifact.bin; got status:\n%s", status)
	}

	rebaseBefore := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseBefore.Dir = wtPath
	if out, err := rebaseBefore.CombinedOutput(); err == nil {
		t.Fatal("precondition: expected rebase to FAIL with untracked artifact.bin; it succeeded unexpectedly")
	} else if !strings.Contains(string(out), "artifact.bin") {
		t.Fatalf("precondition: expected rebase failure to mention artifact.bin; got:\n%s", out)
	}
	abortCmd := exec.CommandContext(t.Context(), "git", "rebase", "--abort")
	abortCmd.Dir = wtPath
	if out, abortErr := abortCmd.CombinedOutput(); abortErr != nil &&
		!strings.Contains(string(out), "no rebase in progress") {
		t.Errorf("git rebase --abort: %v\n%s", abortErr, out)
	}

	cleanUntrackedInTest(t, wtPath)

	if _, err := os.Stat(filepath.Join(wtPath, "artifact.bin")); err == nil {
		t.Fatal("cleanUntrackedFiles should have removed artifact.bin; it still exists")
	}

	if status := integArtifactGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after cleanUntrackedFiles: expected clean worktree; got:\n%s", status)
	}

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

	wtPath := integArtifactSetup(t)

	beforeStatus := integArtifactGit(t, wtPath, "status", "--porcelain")
	if beforeStatus != "" {
		t.Fatalf("precondition: expected clean worktree; got:\n%s", beforeStatus)
	}

	cleanUntrackedInTest(t, wtPath)

	afterStatus := integArtifactGit(t, wtPath, "status", "--porcelain")
	if beforeStatus != afterStatus {
		t.Errorf("cleanUntrackedFiles must be a no-op on a clean worktree; before=%q after=%q",
			beforeStatus, afterStatus)
	}

	//nolint:gosec // G304: path is constructed from t.TempDir() in test, not user input
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

	wtPath := integArtifactSetup(t)

	writeFile(t, filepath.Join(wtPath, ".gitignore"), "keeper.test\n")
	integArtifactGit(t, wtPath, "add", ".gitignore")
	integArtifactGit(t, wtPath, "commit", "-m", "add .gitignore")

	writeFile(t, filepath.Join(wtPath, "keeper.test"), "test binary\n")
	writeFile(t, filepath.Join(wtPath, "artifact.txt"), "integration artifact\n")

	cleanUntrackedInTest(t, wtPath)

	if _, err := os.Stat(filepath.Join(wtPath, "artifact.txt")); err == nil {
		t.Error("cleanUntrackedFiles should have removed non-gitignored artifact.txt; it still exists")
	}

	if _, err := os.Stat(filepath.Join(wtPath, "keeper.test")); err != nil {
		t.Error("cleanUntrackedFiles must NOT remove gitignored keeper.test; it was deleted")
	}
}
