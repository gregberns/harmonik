package daemon_test

// rundriverfixture_test.go — the shared project/worktree/run-id fixtures used by
// the run-driver scenario tests.
//
// These helpers were written for the review-loop driver tests (reviewloop_test.go,
// reviewloop_cycle_complete_hk7om2q24_test.go) and kept their rlFixture / rlcFixture
// names. The driver was retired (EM-015d) and its tests deleted with it, but the
// fixtures outlived it: the DOT scenario tests
// (scenario_commit_gate_cap_hki8g59_test.go, scenario_subworkflow_dispatch_hkx9l_test.go)
// build their project dirs and worktrees through exactly these functions. They live
// here now so the deletion of the driver tests does not take them down.
//
// Names are unchanged on purpose — a rename would touch every call site for no
// behavioural reason.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// rlFixtureProjectDir creates the minimal project directory tree for run-driver
// tests: .harmonik/events/, .harmonik/beads-intents/.
func rlFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

// rlFixtureGitRepo initialises a git repository with one initial commit in dir.
func rlFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rlFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("harmonik run-driver test repo\n"), 0o644); err != nil {
		t.Fatalf("rlFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// rlFixtureWorktree creates a detached git worktree, creates .harmonik/ inside
// it, and registers a cleanup. Returns the worktree path and the parent commit
// SHA (project HEAD at creation time).
func rlFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")

	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("rlFixtureWorktree: git worktree add: %v\n%s", err, out)
	}

	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlFixtureWorktree: mkdir .harmonik: %v", err)
	}

	t.Cleanup(func() {
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})

	return wtPath, parentSHA
}

// rlFixtureRunID generates a fresh test RunID using UUIDv7.
func rlFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlcFixtureSetup creates a fresh project dir, git repo, and worktree for one
// test case. Returns wtPath and parentSHA. Cleanup is registered on t.
func rlcFixtureSetup(t *testing.T) (projectDir, wtPath, parentSHA string) {
	t.Helper()
	projectDir = rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA = rlFixtureWorktree(t, projectDir)
	return projectDir, wtPath, parentSHA
}
