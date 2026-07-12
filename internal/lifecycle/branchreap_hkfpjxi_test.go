package lifecycle

// branchreap_hkfpjxi_test.go — unit tests for ReapBranches (hk-fpjxi).
//
// Each test drives the production ReapBranches implementation against a real
// git repository created in t.TempDir(). No fakes: merge detection, active-
// worktree guards, and branch deletion all run against real git.
//
// Properties under test (reviewer-requested coverage):
//
//  1. Merged branch → deleted (reason "merged").
//  2. Unmerged run/* older than MaxAge → deleted (reason "orphaned_run").
//  3. worktree-agent-* older than MaxAge → deleted (reason "orphaned_agent").
//  4. Branch checked out in an active registered worktree → never reaped.
//  5. --dry-run mode → candidates identified but not deleted.
//  6. Unmerged run/* below MaxAge → skipped (too recent).
//
// Helper prefix: branchReapFixture (per implementer-protocol.md).
// Bead ref: hk-fpjxi.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fixture helpers — branchReapFixture prefix
// ---------------------------------------------------------------------------

// branchReapFixtureInitRepo creates a minimal git repo at repoDir with a root
// commit on main, configured with a test identity.
func branchReapFixtureInitRepo(t *testing.T, repoDir string) {
	t.Helper()
	runGitRepo(t, repoDir, "init", "-b", "main")
	runGitRepo(t, repoDir, "config", "user.email", "test@harmonik")
	runGitRepo(t, repoDir, "config", "user.name", "Harmonik Test")
	readme := filepath.Join(repoDir, "README")
	//nolint:gosec // G306: 0644 is fine for a test file; path is t.TempDir()
	if err := os.WriteFile(readme, []byte("branchreap test repo\n"), 0o644); err != nil {
		t.Fatalf("branchReapFixtureInitRepo: %v", err)
	}
	runGitRepo(t, repoDir, "add", "README")
	runGitRepo(t, repoDir, "commit", "-m", "root")
}

// branchReapFixtureCreateBranch creates a branch at HEAD with one commit.
// If epochSecs > 0, the commit is backdated to that unix timestamp via
// GIT_COMMITTER_DATE / GIT_AUTHOR_DATE so listBranchCandidates sees an old
// creatordate. epochSecs == 0 means "use current time".
func branchReapFixtureCreateBranch(t *testing.T, repoDir, branchName string, epochSecs int64) {
	t.Helper()
	runGitRepo(t, repoDir, "checkout", "-b", branchName)

	state := filepath.Join(repoDir, "state.txt")
	//nolint:gosec // G306: 0644 test file; path is t.TempDir()
	if err := os.WriteFile(state, []byte("branch="+branchName+"\n"), 0o644); err != nil {
		t.Fatalf("branchReapFixtureCreateBranch: WriteFile: %v", err)
	}
	runGitRepo(t, repoDir, "add", "state.txt")

	if epochSecs > 0 {
		dateStr := time.Unix(epochSecs, 0).UTC().Format("2006-01-02T15:04:05Z")
		branchReapFixtureCommitWithDate(t, repoDir, "branch "+branchName, dateStr)
	} else {
		runGitRepo(t, repoDir, "commit", "-m", "branch "+branchName)
	}

	runGitRepo(t, repoDir, "checkout", "main")
}

// branchReapFixtureCommitWithDate runs git commit with backdated committer/author
// dates. dateStr must be in RFC3339 format, e.g. "2026-01-01T00:00:00Z".
func branchReapFixtureCommitWithDate(t *testing.T, repoDir, msg, dateStr string) {
	t.Helper()
	cmdArgs := []string{"-C", repoDir, "commit", "-m", msg}
	//nolint:gosec // G204: args are test-only constants and repoDir is t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", cmdArgs...)
	cmd.Env = append(os.Environ(),
		"GIT_COMMITTER_DATE="+dateStr,
		"GIT_AUTHOR_DATE="+dateStr,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branchReapFixtureCommitWithDate: git commit -m %q: %v: %s", msg, err, out)
	}
}

// branchReapFixtureMergeBranch fast-forward-merges branchName into main.
func branchReapFixtureMergeBranch(t *testing.T, repoDir, branchName string) {
	t.Helper()
	runGitRepo(t, repoDir, "checkout", "main")
	runGitRepo(t, repoDir, "merge", "--ff-only", branchName)
}

// branchReapFixtureBranchExists reports whether shortName is a local branch.
func branchReapFixtureBranchExists(t *testing.T, repoDir, shortName string) bool {
	t.Helper()
	//nolint:gosec // G204: args are hard-coded constants and repoDir is t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git",
		"-C", repoDir,
		"show-ref", "--verify", "--quiet",
		"refs/heads/"+shortName,
	).CombinedOutput()
	_ = out
	return err == nil
}

// oldEpoch returns a unix timestamp 60 days in the past, suitable for
// backdating commits so they exceed the default OrphanMaxAge.
func oldEpoch() int64 {
	return time.Now().Add(-60 * 24 * time.Hour).Unix()
}

// recentEpoch returns a unix timestamp 1 day in the past — recent enough that
// a default 30-day OrphanMaxAge would not trigger orphan deletion.
func recentEpoch() int64 {
	return time.Now().Add(-24 * time.Hour).Unix()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestBranchReap_MergedBranchDeleted verifies that a run/* branch fully merged
// into main is reaped with reason "merged" regardless of its age.
func TestBranchReap_MergedBranchDeleted(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	const branch = "run/01900000-0000-7000-8000-000000000001"
	branchReapFixtureCreateBranch(t, dir, branch, recentEpoch()) // recent — age alone wouldn't trigger
	branchReapFixtureMergeBranch(t, dir, branch)

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ReapBranches: %v", err)
	}

	if !branchReapFixtureContains(result.Reaped, branch) {
		t.Errorf("expected %q in Reaped; got %v", branch, result.Reaped)
	}
	if branchReapFixtureBranchExists(t, dir, branch) {
		t.Errorf("branch %q still exists after reap", branch)
	}
	if reason := branchReapFixtureReasonFor(result.Events, branch); reason != "merged" {
		t.Errorf("expected reason %q for %q; got %q", "merged", branch, reason)
	}
}

// TestBranchReap_OrphanedRunBranchDeleted verifies that an unmerged run/*
// branch older than OrphanMaxAge (with no active worktree) is reaped with
// reason "orphaned_run".
func TestBranchReap_OrphanedRunBranchDeleted(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	const branch = "run/01900000-0000-7000-8000-000000000002"
	branchReapFixtureCreateBranch(t, dir, branch, oldEpoch()) // 60 days old

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ReapBranches: %v", err)
	}

	if !branchReapFixtureContains(result.Reaped, branch) {
		t.Errorf("expected %q in Reaped; got %v", branch, result.Reaped)
	}
	if branchReapFixtureBranchExists(t, dir, branch) {
		t.Errorf("branch %q still exists after reap", branch)
	}
	if reason := branchReapFixtureReasonFor(result.Events, branch); reason != "orphaned_run" {
		t.Errorf("expected reason %q for %q; got %q", "orphaned_run", branch, reason)
	}
}

// TestBranchReap_OrphanedAgentBranchDeleted verifies that a worktree-agent-*
// branch older than OrphanMaxAge is reaped with reason "orphaned_agent".
func TestBranchReap_OrphanedAgentBranchDeleted(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	const branch = "worktree-agent-abc123def456"
	branchReapFixtureCreateBranch(t, dir, branch, oldEpoch()) // 60 days old

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ReapBranches: %v", err)
	}

	if !branchReapFixtureContains(result.Reaped, branch) {
		t.Errorf("expected %q in Reaped; got %v", branch, result.Reaped)
	}
	if branchReapFixtureBranchExists(t, dir, branch) {
		t.Errorf("branch %q still exists after reap", branch)
	}
	if reason := branchReapFixtureReasonFor(result.Events, branch); reason != "orphaned_agent" {
		t.Errorf("expected reason %q for %q; got %q", "orphaned_agent", branch, reason)
	}
}

// TestBranchReap_ActiveWorktreeBranchNeverReaped verifies that a run/* branch
// currently checked out in a registered git worktree is never deleted, even
// when it qualifies as merged or aged.
func TestBranchReap_ActiveWorktreeBranchNeverReaped(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	const branch = "run/01900000-0000-7000-8000-000000000003"
	branchReapFixtureCreateBranch(t, dir, branch, oldEpoch())
	branchReapFixtureMergeBranch(t, dir, branch) // merged AND old

	// Add a registered git worktree checked out on the branch.
	// No explicit cleanup: t.TempDir() removes dir (and worktreePath inside it)
	// when the test ends; git worktree metadata cleanup is unnecessary here.
	worktreePath := filepath.Join(dir, "wt-active")
	runGitRepo(t, dir, "worktree", "add", worktreePath, branch)

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ReapBranches: %v", err)
	}

	if branchReapFixtureContains(result.Reaped, branch) {
		t.Errorf("branch %q in active worktree must not be reaped; got Reaped=%v", branch, result.Reaped)
	}
	if !branchReapFixtureBranchExists(t, dir, branch) {
		t.Errorf("branch %q was deleted despite being checked out in a registered worktree", branch)
	}
}

// TestBranchReap_DryRunNoDelete verifies that dry-run mode identifies merged
// and orphaned candidates but performs no deletions.
func TestBranchReap_DryRunNoDelete(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	const merged = "run/01900000-0000-7000-8000-000000000004"
	const orphaned = "run/01900000-0000-7000-8000-000000000005"
	branchReapFixtureCreateBranch(t, dir, merged, recentEpoch())
	branchReapFixtureMergeBranch(t, dir, merged)
	branchReapFixtureCreateBranch(t, dir, orphaned, oldEpoch())

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("ReapBranches dry-run: %v", err)
	}

	// Candidates must appear in Reaped (would-delete list).
	if !branchReapFixtureContains(result.Reaped, merged) {
		t.Errorf("dry-run: expected %q in would-delete list; got %v", merged, result.Reaped)
	}
	if !branchReapFixtureContains(result.Reaped, orphaned) {
		t.Errorf("dry-run: expected %q in would-delete list; got %v", orphaned, result.Reaped)
	}

	// But the branches must still exist on disk.
	if !branchReapFixtureBranchExists(t, dir, merged) {
		t.Errorf("dry-run deleted %q — should not have", merged)
	}
	if !branchReapFixtureBranchExists(t, dir, orphaned) {
		t.Errorf("dry-run deleted %q — should not have", orphaned)
	}
}

// TestBranchReap_RecentUnmergedBranchSkipped verifies that a run/* branch
// younger than OrphanMaxAge that is NOT merged is left untouched.
func TestBranchReap_RecentUnmergedBranchSkipped(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	const branch = "run/01900000-0000-7000-8000-000000000006"
	branchReapFixtureCreateBranch(t, dir, branch, recentEpoch()) // 1 day old, unmerged

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ReapBranches: %v", err)
	}

	if branchReapFixtureContains(result.Reaped, branch) {
		t.Errorf("branch %q is recent and unmerged; must not be reaped", branch)
	}
	if !branchReapFixtureBranchExists(t, dir, branch) {
		t.Errorf("recent unmerged branch %q was deleted", branch)
	}
	if result.Skipped == 0 {
		t.Errorf("expected Skipped > 0 for the recent unmerged branch; got %d", result.Skipped)
	}
}

// TestBranchReap_ScannedCount verifies that Scanned counts all run/* and
// worktree-agent-* branches regardless of disposition.
func TestBranchReap_ScannedCount(t *testing.T) {
	dir := t.TempDir()
	branchReapFixtureInitRepo(t, dir)

	branches := []struct {
		name      string
		epochSecs int64
		merge     bool
	}{
		{"run/01900000-0000-7000-8000-000000000010", recentEpoch(), true},  // merged
		{"run/01900000-0000-7000-8000-000000000011", oldEpoch(), false},    // orphaned_run
		{"worktree-agent-deadbeef0011", oldEpoch(), false},                 // orphaned_agent
		{"run/01900000-0000-7000-8000-000000000012", recentEpoch(), false}, // skipped (too recent)
	}

	for _, b := range branches {
		branchReapFixtureCreateBranch(t, dir, b.name, b.epochSecs)
		if b.merge {
			branchReapFixtureMergeBranch(t, dir, b.name)
		}
	}

	result, err := ReapBranches(context.Background(), BranchReapOptions{
		RepoDir:      dir,
		TargetBranch: "main",
		OrphanMaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ReapBranches: %v", err)
	}

	if result.Scanned != 4 {
		t.Errorf("expected Scanned=4; got %d", result.Scanned)
	}
	if len(result.Reaped) != 3 {
		t.Errorf("expected 3 reaped (merged+orphaned_run+orphaned_agent); got %d: %v", len(result.Reaped), result.Reaped)
	}
	if result.Skipped != 1 {
		t.Errorf("expected Skipped=1 (recent unmerged); got %d", result.Skipped)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// branchReapFixtureContains reports whether s is present in list.
func branchReapFixtureContains(list []string, s string) bool {
	for _, v := range list {
		if strings.TrimSpace(v) == s {
			return true
		}
	}
	return false
}

// branchReapFixtureReasonFor returns the Reason field of the BranchReapEvent
// for the given branch name, or "" when not found.
func branchReapFixtureReasonFor(events []BranchReapEvent, branch string) string {
	for _, ev := range events {
		if ev.Branch == branch {
			return ev.Reason
		}
	}
	return ""
}
