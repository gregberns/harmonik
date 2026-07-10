package lifecycle

// gitmergecommitscanner_hk420yr7_test.go — hk-420yr.7 B2b: reconcile-only
// acceptance suite for GitMergeCommitScanner.HasMergeCommitForBead.
//
// Spec ref: specs/process-lifecycle.md §4.5 PL-006 exclusion (c) — Cat 3c
// condition; orphansweepbeads.go GitMergeCommitScanner.
//
// These are real-git acceptance tests: each test creates a temp git repository
// on disk, optionally lands commits bearing Harmonik-Bead-ID trailers, and
// drives the production GitMergeCommitScanner implementation. No fakes.
//
// Two non-negotiable properties under test:
//
//  1. DETECTED — a target-branch commit carrying "Harmonik-Bead-ID: <beadID>"
//     returns (true, nil). The sweep can safely close or skip the bead (Cat 3c).
//
//  2. FALSE-CLOSE GUARD — when no such commit exists for a given bead ID, the
//     scanner returns (false, nil). Misidentifying an open bead as "merged" would
//     trigger a Cat 3c close on a bead whose implementation has NOT landed —
//     the hk-vbv3b/whru3 false-close family. The guard test is mandatory.
//
// Conservative error behavior: git errors (missing dir, missing branch) MUST
// return (false, nil), never (true, _). A false negative on a scan error is
// safe — the bead reset fires on the next restart. A false positive would
// trigger an incorrect Cat 3c close.
//
// Helper prefix: gitMCSFixture (per implementer-protocol.md §Helper-prefix).
// Bead ref: hk-420yr.7.
//
// Together with promote_cmd_b2a_subsystem_test.go and
// promote_cmd_hkpk3p1_test.go (cmd/harmonik), this suite fully covers the
// scope of the parent umbrella bead hk-420yr.2 ("B2: promote/reconcile
// acceptance suite on temp git repo"). No further implementation is needed
// for hk-420yr.2.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// Fixture helpers — gitMCSFixture prefix
// ---------------------------------------------------------------------------

// gitMCSFixtureBeadID returns a stable bead ID string for GitMergeCommitScanner
// acceptance tests.
func gitMCSFixtureBeadID(n int) string {
	return fmt.Sprintf("hk-420yr7.%d", n)
}

// gitMCSFixtureInitRepo creates a minimal git repo at repoDir with a root
// commit on main, configured with a test user identity.
func gitMCSFixtureInitRepo(t *testing.T, repoDir string) {
	t.Helper()

	runGitRepo(t, repoDir, "init", "-b", "main")
	runGitRepo(t, repoDir, "config", "user.email", "test@harmonik")
	runGitRepo(t, repoDir, "config", "user.name", "Harmonik Test")

	root := repoDir + "/README"
	//nolint:gosec // G306: 0644 matches test convention; path is t.TempDir()
	if err := os.WriteFile(root, []byte("harmonik gitmergecommitscanner test repo\n"), 0o644); err != nil {
		t.Fatalf("gitMCSFixtureInitRepo: WriteFile README: %v", err)
	}
	runGitRepo(t, repoDir, "add", "README")
	runGitRepo(t, repoDir, "commit", "-m", "root")
}

// gitMCSFixtureLandBeadCommit lands a commit on the current branch of repoDir
// carrying the "Harmonik-Bead-ID: <beadID>" trailer. This simulates a merge
// commit that closes the Cat 3c condition (exclusion c in PL-006).
func gitMCSFixtureLandBeadCommit(t *testing.T, repoDir string, beadID core.BeadID) {
	t.Helper()

	stateFile := repoDir + "/state.txt"
	content := fmt.Sprintf("merged bead=%s\n", beadID)
	//nolint:gosec // G306: 0644 is correct for a state file in a test repo; path is t.TempDir()
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatalf("gitMCSFixtureLandBeadCommit: WriteFile state.txt: %v", err)
	}
	runGitRepo(t, repoDir, "add", "state.txt")

	// The commit message format mirrors production merge commits: subject line,
	// blank line, then the Harmonik-Bead-ID trailer. Git --grep searches the
	// full message body including trailers.
	msg := fmt.Sprintf("merge: bead %s implementation\n\nHarmonik-Bead-ID: %s\n", beadID, beadID)
	runGitRepo(t, repoDir, "commit", "-m", msg)
}

// ---------------------------------------------------------------------------
// Acceptance suite
// ---------------------------------------------------------------------------

// TestGitMergeCommitScanner_TrailerBearingCommit_Detected is the primary
// "detected + closeable" acceptance case (PROPERTY 1).
//
// Setup: a temp repo with one commit on main carrying
// "Harmonik-Bead-ID: <beadID>". The scanner MUST return (true, nil), indicating
// the Cat 3c condition is met and the bead can be closed by the sweep.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 exclusion (c); hk-lgtq2 Cat 3c.
func TestGitMergeCommitScanner_TrailerBearingCommit_Detected(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	gitMCSFixtureInitRepo(t, repoDir)
	bid := core.BeadID(gitMCSFixtureBeadID(1))
	gitMCSFixtureLandBeadCommit(t, repoDir, bid)

	s := GitMergeCommitScanner{
		ProjectDir:   repoDir,
		TargetBranch: "main",
	}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: unexpected error: %v", err)
	}
	if !got {
		t.Errorf("HasMergeCommitForBead(%q) = false; want true — trailer-bearing commit on main must be detected", bid)
	}
}

// TestGitMergeCommitScanner_NoMergeCommit_ReturnsFalse is the FALSE-CLOSE
// GUARD case (PROPERTY 2). This is the non-negotiable regression guard for
// the hk-vbv3b/whru3 false-close family.
//
// Setup: same repo with a bead commit for bidA on main. A second bead ID
// (bidB) has NO commit. The scanner MUST return (false, nil) for bidB.
// Returning (true, nil) for bidB would trigger a Cat 3c close on an open bead
// whose implementation has NOT landed — the false-close defect this test guards.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 exclusion (c).
// Bug family: hk-vbv3b / hk-whru3 false-close.
func TestGitMergeCommitScanner_NoMergeCommit_ReturnsFalse(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	gitMCSFixtureInitRepo(t, repoDir)

	// Land a commit for bidA — bidB has no commit.
	bidA := core.BeadID(gitMCSFixtureBeadID(2))
	bidB := core.BeadID(gitMCSFixtureBeadID(3))
	gitMCSFixtureLandBeadCommit(t, repoDir, bidA)

	s := GitMergeCommitScanner{
		ProjectDir:   repoDir,
		TargetBranch: "main",
	}
	got, err := s.HasMergeCommitForBead(context.Background(), bidB)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead(%q): unexpected error: %v", bidB, err)
	}
	if got {
		t.Errorf(
			"HasMergeCommitForBead(%q) = true; want false — "+
				"no Harmonik-Bead-ID: %q commit exists on main; "+
				"returning true would trigger a false Cat 3c close (hk-vbv3b/whru3 family)",
			bidB, bidB,
		)
	}
}

// TestGitMergeCommitScanner_EmptyTargetBranch_DefaultsToMain verifies that
// when TargetBranch is empty, the scanner defaults to "main" and correctly
// detects a trailer-bearing commit on that branch.
//
// Spec ref: orphansweepbeads.go GitMergeCommitScanner.HasMergeCommitForBead —
// "branch := s.TargetBranch; if branch == "" { branch = "main" }".
func TestGitMergeCommitScanner_EmptyTargetBranch_DefaultsToMain(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	gitMCSFixtureInitRepo(t, repoDir)
	bid := core.BeadID(gitMCSFixtureBeadID(4))
	gitMCSFixtureLandBeadCommit(t, repoDir, bid)

	// TargetBranch left empty — must default to "main".
	s := GitMergeCommitScanner{
		ProjectDir: repoDir,
	}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: unexpected error: %v", err)
	}
	if !got {
		t.Errorf("HasMergeCommitForBead(%q): TargetBranch='' should default to 'main'; got false, want true", bid)
	}
}

// TestGitMergeCommitScanner_CommitOnOtherBranch_NotDetected verifies that the
// scanner scopes its search to the configured target branch only. A commit
// carrying Harmonik-Bead-ID on an off-target branch MUST NOT cause a false
// positive on the target branch.
//
// This prevents Cat 3c from firing on a bead whose merge commit landed on an
// unrelated branch (e.g., a work branch not yet merged to main).
//
// Spec ref: process-lifecycle.md §4.5 PL-006 exclusion (c).
func TestGitMergeCommitScanner_CommitOnOtherBranch_NotDetected(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	gitMCSFixtureInitRepo(t, repoDir)
	bid := core.BeadID(gitMCSFixtureBeadID(5))

	// Land the bead commit on "work/branch", NOT on "main".
	runGitRepo(t, repoDir, "checkout", "-b", "work/branch")
	gitMCSFixtureLandBeadCommit(t, repoDir, bid)
	// Return to main (work/branch commit is NOT on main).
	runGitRepo(t, repoDir, "checkout", "main")

	s := GitMergeCommitScanner{
		ProjectDir:   repoDir,
		TargetBranch: "main",
	}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: unexpected error: %v", err)
	}
	if got {
		t.Errorf(
			"HasMergeCommitForBead(%q) = true; want false — "+
				"commit exists only on work/branch, not on main; "+
				"scanner must not search off-target branches",
			bid,
		)
	}
}

// TestGitMergeCommitScanner_NonExistentBranch_Conservative verifies the
// conservative error behavior: when the target branch does not exist in the
// repo, the scanner returns (false, nil) rather than an error or true.
//
// Conservative behavior is correct: a false negative causes the sweep to
// proceed with a reset (safe — the Cat 3c condition will be re-evaluated on
// the next daemon restart). A false positive would trigger an incorrect Cat 3c
// close on an open bead.
//
// Spec ref: orphansweepbeads.go GitMergeCommitScanner — "A scan error ... is
// treated as 'no merge commit found'."
func TestGitMergeCommitScanner_NonExistentBranch_Conservative(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	gitMCSFixtureInitRepo(t, repoDir)
	bid := core.BeadID(gitMCSFixtureBeadID(6))

	s := GitMergeCommitScanner{
		ProjectDir:   repoDir,
		TargetBranch: "does-not-exist",
	}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: non-existent branch must not return error; got: %v", err)
	}
	if got {
		t.Errorf("HasMergeCommitForBead: non-existent branch must return false (conservative); got true")
	}
}

// TestGitMergeCommitScanner_NonExistentProjectDir_Conservative verifies the
// conservative error behavior when the project directory does not exist (git
// is absent or the repo is gone). The scanner MUST return (false, nil).
//
// Spec ref: orphansweepbeads.go GitMergeCommitScanner — "A scan error
// (git absent, branch missing, etc.) is treated as 'no merge commit found'."
func TestGitMergeCommitScanner_NonExistentProjectDir_Conservative(t *testing.T) {
	t.Parallel()

	bid := core.BeadID(gitMCSFixtureBeadID(7))
	s := GitMergeCommitScanner{
		ProjectDir:   "/nonexistent-repo-path-for-test",
		TargetBranch: "main",
	}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: non-existent dir must not return error; got: %v", err)
	}
	if got {
		t.Errorf("HasMergeCommitForBead: non-existent dir must return false (conservative); got true")
	}
}

// TestGitMergeCommitScanner_MultipleBeads_DetectsOnlyMatching verifies that
// when multiple bead commits exist on the target branch, the scanner correctly
// identifies only the matching bead — neither confusing IDs nor returning true
// for all beads.
//
// This is a disambiguation guard: a prefix-match or substring-match bug in the
// git --grep query could cause bidA="hk-1" to match a commit for bidB="hk-10".
//
// Spec ref: process-lifecycle.md §4.5 PL-006 exclusion (c).
func TestGitMergeCommitScanner_MultipleBeads_DetectsOnlyMatching(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	gitMCSFixtureInitRepo(t, repoDir)

	bidA := core.BeadID(gitMCSFixtureBeadID(8))
	bidB := core.BeadID(gitMCSFixtureBeadID(9))
	bidC := core.BeadID(gitMCSFixtureBeadID(10)) // not committed — stays open

	gitMCSFixtureLandBeadCommit(t, repoDir, bidA)
	gitMCSFixtureLandBeadCommit(t, repoDir, bidB)

	s := GitMergeCommitScanner{
		ProjectDir:   repoDir,
		TargetBranch: "main",
	}

	for _, tc := range []struct {
		bid  core.BeadID
		want bool
		desc string
	}{
		{bidA, true, "bidA commit exists on main"},
		{bidB, true, "bidB commit exists on main"},
		{bidC, false, "bidC has no commit — must not be false-closed"},
	} {
		got, err := s.HasMergeCommitForBead(context.Background(), tc.bid)
		if err != nil {
			t.Errorf("HasMergeCommitForBead(%q): unexpected error: %v", tc.bid, err)
			continue
		}
		if got != tc.want {
			t.Errorf("HasMergeCommitForBead(%q) = %v; want %v — %s", tc.bid, got, tc.want, tc.desc)
		}
	}
}
