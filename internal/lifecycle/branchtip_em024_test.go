package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func durableFixtureInitRepo(t *testing.T, repoDir string) {
	t.Helper()

	runGitRepo(t, repoDir, "init", "-b", "main")
	runGitRepo(t, repoDir, "config", "user.email", "test@harmonik")
	runGitRepo(t, repoDir, "config", "user.name", "Harmonik Test")

	root := filepath.Join(repoDir, "README")
	if err := os.WriteFile(root, []byte("harmonik durable-state test repo\n"), 0o600); err != nil {
		t.Fatalf("durableFixtureInitRepo: WriteFile README: %v", err)
	}
	runGitRepo(t, repoDir, "add", "README")
	runGitRepo(t, repoDir, "commit", "-m", "root")
}

func durableFixtureCreateTaskBranch(t *testing.T, repoDir, runID string) {
	t.Helper()
	runGitRepo(t, repoDir, "checkout", "-b", "run/"+runID)
}

func durableFixtureCommitCheckpoint(t *testing.T, repoDir, runID, nodeID string) string {
	t.Helper()

	stateFile := filepath.Join(repoDir, "state.txt")
	if err := os.WriteFile(stateFile, []byte(fmt.Sprintf("run=%s node=%s\n", runID, nodeID)), 0o600); err != nil {
		t.Fatalf("durableFixtureCommitCheckpoint: WriteFile state.txt: %v", err)
	}
	runGitRepo(t, repoDir, "add", "state.txt")

	msg := fmt.Sprintf("checkpoint: %s\n\nHarmonik-Run-ID: %s\n", nodeID, runID)
	runGitRepo(t, repoDir, "commit", "-m", msg)

	return durableFixtureReadTip(t, repoDir, "run/"+runID)
}

func durableFixtureReadTip(t *testing.T, repoDir, branchRef string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"rev-parse", "--verify", branchRef,
	).Output()
	if err != nil {
		t.Fatalf("durableFixtureReadTip: git rev-parse %s: %v", branchRef, err)
	}
	return strings.TrimSpace(string(out))
}

func durableFixtureRunID(n int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-00000000%04d", n)
}

func runGitRepo(t *testing.T, repoDir string, args ...string) {
	t.Helper()

	cmdArgs := append([]string{"-C", repoDir}, args...)
	//nolint:gosec // G204: args are test-only constants and repoDir is t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("runGitRepo: git %v: %v: %s", args, err, out)
	}
}

// TestEM024_BranchTipEqualsCheckpointSHA asserts the core EM-024 invariant:
// after each durable transition, the task branch tip MUST equal the checkpoint
// commit SHA.
//
// The test simulates a two-step run (node-a → node-b), commits one checkpoint per
// step, and verifies that the branch tip advances to exactly the checkpoint SHA
// after each landing.
//
// Spec ref: execution-model.md §4.5 EM-024 — "at any time, for every in-flight
// run, the tip of the run's task branch MUST be the run's last-durable-state
// checkpoint commit."
func TestEM024_BranchTipEqualsCheckpointSHA(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := durableFixtureRunID(1)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	sha1 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-a")
	tip1 := durableFixtureReadTip(t, repoDir, "run/"+runID)
	if tip1 != sha1 {
		t.Errorf("EM-024 violation after node-a: branch tip = %q, want checkpoint SHA %q", tip1, sha1)
	}

	sha2 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-b")
	tip2 := durableFixtureReadTip(t, repoDir, "run/"+runID)
	if tip2 != sha2 {
		t.Errorf("EM-024 violation after node-b: branch tip = %q, want checkpoint SHA %q", tip2, sha2)
	}

	if sha1 == sha2 {
		t.Errorf("EM-023 violation: sha1 == sha2 (%q); each durable transition must produce a distinct commit", sha1)
	}
}

// TestEM024_NonDurableTransitionDoesNotAdvanceTip asserts that a non-durable
// transition (RETRY) MUST NOT advance the task branch tip.
//
// The test lands one checkpoint commit, then asserts that additional work that
// does NOT land a commit (simulating a RETRY non-durable outcome) leaves the
// branch tip unchanged.
//
// Spec ref: execution-model.md §4.5 EM-024 (tip = last-durable-state commit);
// §4.5 EM-023a (RETRY is NOT durable; §4.5 EM-025 (failed transitions MUST NOT
// create checkpoint commits).
func TestEM024_NonDurableTransitionDoesNotAdvanceTip(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := durableFixtureRunID(2)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	sha := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-a")
	tipBeforeRetry := durableFixtureReadTip(t, repoDir, "run/"+runID)
	if tipBeforeRetry != sha {
		t.Fatalf("precondition: branch tip %q != checkpoint SHA %q", tipBeforeRetry, sha)
	}

	tipAfterRetry := durableFixtureReadTip(t, repoDir, "run/"+runID)
	if tipAfterRetry != sha {
		t.Errorf("EM-024 violation: RETRY must not advance tip; got %q, want %q", tipAfterRetry, sha)
	}
}

// TestEM024_MultiRunIsolation asserts that EM-024 is per-run: advancing the task
// branch of run A MUST NOT affect the branch tip of an independent run B.
//
// Spec ref: execution-model.md §4.5 EM-024 — "for every in-flight run" (each
// run is isolated on its own task branch per workspace-model.md §4.2 WM-005).
func TestEM024_MultiRunIsolation(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runIDA := durableFixtureRunID(3)
	runIDB := durableFixtureRunID(4)

	durableFixtureCreateTaskBranch(t, repoDir, runIDA)
	runGitRepo(t, repoDir, "checkout", "main")
	durableFixtureCreateTaskBranch(t, repoDir, runIDB)

	runGitRepo(t, repoDir, "checkout", "run/"+runIDA)
	shaA := durableFixtureCommitCheckpoint(t, repoDir, runIDA, "node-a")

	runGitRepo(t, repoDir, "checkout", "run/"+runIDB)
	shaB := durableFixtureCommitCheckpoint(t, repoDir, runIDB, "node-x")

	tipA := durableFixtureReadTip(t, repoDir, "run/"+runIDA)
	if tipA != shaA {
		t.Errorf("EM-024 isolation violation: run A tip = %q; want %q (run B's commit must not affect run A)", tipA, shaA)
	}

	tipB := durableFixtureReadTip(t, repoDir, "run/"+runIDB)
	if tipB != shaB {
		t.Errorf("EM-024 violation: run B tip = %q; want %q", tipB, shaB)
	}

	if tipA == tipB {
		t.Errorf("EM-024 isolation violation: run A tip == run B tip (%q); distinct runs must have distinct branch tips", tipA)
	}
}

// TestEM024_CheckpointSHAIsAncestorOfSubsequentTip asserts that each successive
// checkpoint SHA is a fast-forward descendant of the prior one, satisfying the
// monotonicity half of EM-024 that EM-024a formalises.
//
// The test uses `git merge-base --is-ancestor` to verify the ancestor relationship,
// which is the same check the daemon performs per §4.5 EM-024a.
//
// Spec ref: execution-model.md §4.5 EM-024 (tip = last-durable-state commit),
// §4.5 EM-024a (fast-forward ancestry check).
func TestEM024_CheckpointSHAIsAncestorOfSubsequentTip(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	runID := durableFixtureRunID(5)
	durableFixtureCreateTaskBranch(t, repoDir, runID)

	sha1 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-a")
	sha2 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-b")
	sha3 := durableFixtureCommitCheckpoint(t, repoDir, runID, "node-c")

	durableFixtureAssertAncestor(t, repoDir, sha1, sha2,
		"EM-024a: sha1 (node-a) must be an ancestor of sha2 (node-b)")

	durableFixtureAssertAncestor(t, repoDir, sha2, sha3,
		"EM-024a: sha2 (node-b) must be an ancestor of sha3 (node-c)")

	durableFixtureAssertAncestor(t, repoDir, sha1, sha3,
		"EM-024a: sha1 (node-a) must be a transitive ancestor of sha3 (node-c)")
}

func durableFixtureAssertAncestor(t *testing.T, repoDir, ancestor, descendant, label string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "-C", repoDir,
		"merge-base", "--is-ancestor", ancestor, descendant,
	)
	if err := cmd.Run(); err != nil {
		t.Errorf("%s: git merge-base --is-ancestor %s %s: %v (ancestor check failed)",
			label, ancestor, descendant, err)
	}
}
