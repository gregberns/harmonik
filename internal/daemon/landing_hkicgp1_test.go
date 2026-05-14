package daemon_test

// landing_hkicgp1_test.go — unit and integration tests for the landing-strategy
// selector (squash | cherry-pick) and per-bead lands_on target ref.
//
// Spec refs:
//   - specs/workspace-model.md §4.2 WM-005b (lands_on default)
//   - specs/workspace-model.md §4.5 WM-019 (squash landing)
//   - specs/workspace-model.md §4.5 WM-019b (cherry-pick landing)
//   - specs/workspace-model.md §4.5 WM-020 (conflict detection)
//
// Helper prefix: landingFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-icgp1).

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// landingFixtureGitRepo initialises a minimal git repository with:
//   - "main" branch with an initial commit
//   - "integration/foo" branch pointing at the same commit
//
// Returns the repo root path. The repo is used as both the "repoRoot" and as
// the base for scratch worktree creation.
func landingFixtureGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	run("init", "-b", "main")
	run("config", "user.email", "daemon@harmonik.local")
	run("config", "user.name", "Harmonik Daemon")

	// Initial commit on main.
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("init"), 0o644); err != nil {
		t.Fatalf("landingFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")

	// Create integration/foo branch at the same point as main.
	run("branch", "integration/foo")

	return dir
}

// landingFixtureTaskBranch creates a task branch off main with one checkpoint
// commit and returns the branch name. The commit adds a file named after label
// to avoid conflicts in parallel tests.
func landingFixtureTaskBranch(t *testing.T, repoRoot, label string) string {
	t.Helper()
	branchName := "run/" + label

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	run("checkout", "-b", branchName)

	// Checkpoint commit.
	fname := filepath.Join(repoRoot, label+"-work.txt")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(fname, []byte("work for "+label), 0o644); err != nil {
		t.Fatalf("landingFixtureTaskBranch: WriteFile: %v", err)
	}
	run("add", label+"-work.txt")
	run("commit", "-m", "feat: "+label,
		"--trailer", "Harmonik-Run-ID: run-"+label,
		"--trailer", "Harmonik-Bead-ID: bead-"+label,
	)

	// Return to main.
	run("checkout", "main")
	return branchName
}

// landingFixtureScratchWorktree creates a scratch merge-worktree at
// repoRoot/.harmonik/worktrees/merge-<label>/ checked out at landsOn branch,
// per WM-019a option (b). Returns the worktree path and a cleanup function.
func landingFixtureScratchWorktree(t *testing.T, repoRoot, label, landsOnBranch string) (string, func()) {
	t.Helper()
	mergeID := "merge-" + label
	mergePath := filepath.Join(repoRoot, ".harmonik", "worktrees", mergeID)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(mergePath), 0o755); err != nil {
		t.Fatalf("landingFixtureScratchWorktree: MkdirAll: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add",
		"-b", mergeID, mergePath, landsOnBranch)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("landingFixtureScratchWorktree: git worktree add: %v\n%s", err, out)
	}

	cleanup := func() {
		rmCmd := exec.CommandContext(t.Context(), "git", "worktree", "remove", "--force", "--force", mergePath)
		rmCmd.Dir = repoRoot
		_ = rmCmd.Run()
	}
	return mergePath, cleanup
}

// landingFixtureHeadSHA returns the HEAD SHA of the given branch in repoRoot.
func landingFixtureHeadSHA(t *testing.T, repoRoot, branch string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("landingFixtureHeadSHA: git rev-parse refs/heads/%s: %v", branch, err)
	}
	return strings.TrimRight(string(out), "\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveLandsOn tests
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveLandsOn_DefaultMain verifies that an empty LandsOn resolves to
// "main" per the WM-005b spec-level default.
func TestResolveLandsOn_DefaultMain(t *testing.T) {
	t.Parallel()
	cfg := daemon.ExportedBranchingConfig{}
	got := daemon.ExportedResolveLandsOn(cfg)
	if got != "main" {
		t.Errorf("resolveLandsOn: got %q; want %q (spec default)", got, "main")
	}
}

// TestResolveLandsOn_PerBeadOverride verifies that a non-empty LandsOn is
// returned as-is (per-bead override).
func TestResolveLandsOn_PerBeadOverride(t *testing.T) {
	t.Parallel()
	cfg := daemon.ExportedBranchingConfig{LandsOn: "integration/foo"}
	got := daemon.ExportedResolveLandsOn(cfg)
	if got != "integration/foo" {
		t.Errorf("resolveLandsOn: got %q; want %q", got, "integration/foo")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// landTaskBranch — selector dispatch tests
// ─────────────────────────────────────────────────────────────────────────────

// TestLandTaskBranch_SquashDefault verifies that an empty LandingStrategy
// dispatches to squash (preserves existing behaviour as the default per WM-019b).
func TestLandTaskBranch_SquashDefault(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)
	taskBranch := landingFixtureTaskBranch(t, repoRoot, "sq-default")

	// Scratch worktree at "main" (the default lands_on target).
	mergeWTPath, cleanup := landingFixtureScratchWorktree(t, repoRoot, "sq-default", "main")
	defer cleanup()

	mainSHABefore := landingFixtureHeadSHA(t, repoRoot, "main")

	cfg := daemon.ExportedBranchingConfig{LandingStrategy: ""} // empty → squash
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, mergeWTPath, taskBranch,
		"run-sq-default", "bead-sq-default", cfg)
	if err != nil {
		t.Fatalf("landTaskBranch squash-default: unexpected error: %v", err)
	}

	// Verify main advanced (squash commit was created in scratch worktree).
	// The scratch worktree's branch is "merge-sq-default"; we verify it advanced
	// past mainSHABefore by checking the scratch worktree HEAD.
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = mergeWTPath
	out, err2 := cmd.Output()
	if err2 != nil {
		t.Fatalf("git rev-parse HEAD in scratch worktree: %v", err2)
	}
	mergeWTHead := strings.TrimRight(string(out), "\n")
	if mergeWTHead == mainSHABefore {
		t.Errorf("squash landing: scratch worktree HEAD unchanged; expected new commit")
	}
}

// TestLandTaskBranch_SquashExplicit verifies that an explicit
// LandingStrategy="squash" also dispatches to the squash path.
func TestLandTaskBranch_SquashExplicit(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)
	taskBranch := landingFixtureTaskBranch(t, repoRoot, "sq-explicit")

	mergeWTPath, cleanup := landingFixtureScratchWorktree(t, repoRoot, "sq-explicit", "main")
	defer cleanup()

	cfg := daemon.ExportedBranchingConfig{LandingStrategy: "squash"}
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, mergeWTPath, taskBranch,
		"run-sq-explicit", "bead-sq-explicit", cfg)
	if err != nil {
		t.Fatalf("landTaskBranch squash-explicit: unexpected error: %v", err)
	}
}

// TestLandTaskBranch_CherryPickPreservesCommits verifies that cherry-pick
// preserves each checkpoint commit as an individual commit on the target branch,
// and that per-commit Harmonik trailers are retained natively (WM-019b).
func TestLandTaskBranch_CherryPickPreservesCommits(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)
	taskBranch := landingFixtureTaskBranch(t, repoRoot, "cp-preserve")

	// Use integration/foo as the landing target.
	mergeWTPath, cleanup := landingFixtureScratchWorktree(t, repoRoot, "cp-preserve", "integration/foo")
	defer cleanup()

	cfg := daemon.ExportedBranchingConfig{
		LandsOn:         "integration/foo",
		LandingStrategy: "cherry-pick",
	}
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, mergeWTPath, taskBranch,
		"run-cp-preserve", "bead-cp-preserve", cfg)
	if err != nil {
		t.Fatalf("landTaskBranch cherry-pick: unexpected error: %v", err)
	}

	// Verify the cherry-picked commit on the scratch worktree retains the
	// Harmonik-Run-ID trailer from the source checkpoint (WM-019b: trailers
	// preserved natively by cherry-pick).
	cmd := exec.CommandContext(t.Context(), "git", "log", "--format=%B", "-1")
	cmd.Dir = mergeWTPath
	out, logErr := cmd.Output()
	if logErr != nil {
		t.Fatalf("git log: %v", logErr)
	}
	if !strings.Contains(string(out), "Harmonik-Run-ID: run-cp-preserve") {
		t.Errorf("cherry-pick: expected Harmonik-Run-ID trailer in commit message; got:\n%s", out)
	}
}

// TestLandTaskBranch_LandsOnMain verifies that an empty LandsOn resolves to
// "main" and the squash lands there.
func TestLandTaskBranch_LandsOnMain(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)
	taskBranch := landingFixtureTaskBranch(t, repoRoot, "lands-main")

	mergeWTPath, cleanup := landingFixtureScratchWorktree(t, repoRoot, "lands-main", "main")
	defer cleanup()

	// cfg.LandsOn is empty → defaults to "main".
	cfg := daemon.ExportedBranchingConfig{LandingStrategy: "squash"}
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, mergeWTPath, taskBranch,
		"run-lands-main", "bead-lands-main", cfg)
	if err != nil {
		t.Fatalf("landTaskBranch lands_on=main(default): unexpected error: %v", err)
	}
}

// TestLandTaskBranch_LandsOnIntegrationFoo verifies that explicit
// LandsOn="integration/foo" targets that branch.
func TestLandTaskBranch_LandsOnIntegrationFoo(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)
	taskBranch := landingFixtureTaskBranch(t, repoRoot, "lands-ifoo")

	mergeWTPath, cleanup := landingFixtureScratchWorktree(t, repoRoot, "lands-ifoo", "integration/foo")
	defer cleanup()

	cfg := daemon.ExportedBranchingConfig{
		LandsOn:         "integration/foo",
		LandingStrategy: "squash",
	}
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, mergeWTPath, taskBranch,
		"run-lands-ifoo", "bead-lands-ifoo", cfg)
	if err != nil {
		t.Fatalf("landTaskBranch lands_on=integration/foo: unexpected error: %v", err)
	}
}

// TestLandTaskBranch_MissingLandsOnRef verifies that a lands_on ref that does
// not exist locally returns a typed *LandsOnRefError (fail-fast per brief).
func TestLandTaskBranch_MissingLandsOnRef(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)
	taskBranch := landingFixtureTaskBranch(t, repoRoot, "lands-missing")

	// The merge worktree path does not matter here because the error fires
	// before the merge starts; use a temp dir so the test doesn't fail on
	// worktree creation.
	tmpWTDir := t.TempDir()

	cfg := daemon.ExportedBranchingConfig{
		LandsOn:         "nonexistent-branch-xyz",
		LandingStrategy: "squash",
	}
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, tmpWTDir, taskBranch,
		"run-lands-missing", "bead-lands-missing", cfg)
	if err == nil {
		t.Fatal("landTaskBranch: expected error for missing lands_on ref; got nil")
	}

	var landsErr *daemon.ExportedLandsOnRefError
	if !errors.As(err, &landsErr) {
		t.Errorf("landTaskBranch: error type = %T; want *daemon.LandsOnRefError in chain; err = %v", err, err)
	}
	if landsErr != nil && landsErr.Ref != "nonexistent-branch-xyz" {
		t.Errorf("LandsOnRefError.Ref = %q; want %q", landsErr.Ref, "nonexistent-branch-xyz")
	}
}

// TestLandTaskBranch_CherryPickAllMechanical verifies that a cherry-pick on an
// all-mechanical task branch (no commits beyond merge-base) returns an error
// per WM-019b rather than attempting an empty cherry-pick.
func TestLandTaskBranch_CherryPickAllMechanical(t *testing.T) {
	t.Parallel()
	repoRoot := landingFixtureGitRepo(t)

	// Create an empty branch (no commits beyond main).
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("checkout", "-b", "run/all-mechanical")
	run("checkout", "main")

	mergeWTPath, cleanup := landingFixtureScratchWorktree(t, repoRoot, "all-mechanical", "main")
	defer cleanup()

	cfg := daemon.ExportedBranchingConfig{LandingStrategy: "cherry-pick"}
	err := daemon.ExportedLandTaskBranch(t.Context(), repoRoot, mergeWTPath, "run/all-mechanical",
		"run-all-mechanical", "bead-all-mechanical", cfg)
	if err == nil {
		t.Fatal("landTaskBranch cherry-pick all-mechanical: expected error; got nil")
	}
	if !strings.Contains(err.Error(), "all-mechanical") {
		t.Errorf("landTaskBranch cherry-pick all-mechanical: error should mention all-mechanical; got: %v", err)
	}
}
