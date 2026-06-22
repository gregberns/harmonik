package workspace

import (
	"os"
	"testing"
	"time"
)

// TestHkLdzp_RemoveStaleWorktrees verifies that [RemoveStaleWorktrees] calls
// `git worktree remove --force --force` on each supplied path, deregistering
// the worktree from git and deleting its directory (hk-ldzp).
//
// Background: `git worktree prune` only removes worktree metadata for paths
// that are already absent from disk. Stale harmonik worktrees remain registered
// in git, so prune silently skips them. An explicit `git worktree remove` is
// required to deregister AND delete atomically.
func TestHkLdzp_RemoveStaleWorktrees(t *testing.T) {
	t.Parallel()

	t.Run("removes-stale-pid-worktree", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7ld2-8a1b-2c3d4e5fld2p"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		// Write a stale lease-lock with a dead PID.
		leaseLockPath := LeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, 99999999, time.Now(), 3600))

		// Confirm the worktree directory exists before removal.
		if _, err := os.Stat(worktreePath); err != nil {
			t.Fatalf("worktree directory missing before RemoveStaleWorktrees: %v", err)
		}

		result := RemoveStaleWorktrees(t.Context(), repo, []string{worktreePath}, nil)

		// Must be in Removed, not Failed.
		if len(result.Failed) > 0 {
			t.Errorf("RemoveStaleWorktrees: unexpected failures: %v", result.Failed)
		}
		if len(result.Removed) != 1 || result.Removed[0] != worktreePath {
			t.Errorf("RemoveStaleWorktrees: Removed = %v, want [%q]", result.Removed, worktreePath)
		}

		// The worktree directory MUST be gone.
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("worktree directory still present after RemoveStaleWorktrees; want deleted")
		}
	})

	t.Run("empty-paths-is-noop", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)

		result := RemoveStaleWorktrees(t.Context(), repo, nil, nil)

		if len(result.Removed) != 0 {
			t.Errorf("RemoveStaleWorktrees(nil paths): Removed = %v, want empty", result.Removed)
		}
		if len(result.Failed) != 0 {
			t.Errorf("RemoveStaleWorktrees(nil paths): Failed = %v, want empty", result.Failed)
		}
	})

	t.Run("nonexistent-path-goes-to-failed", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		bogusPath := WorktreePath(repo, "0196a1b2-c3d4-7ld2-8a1b-2c3d4e5fbogx", NoWorktreeRootOverride())

		result := RemoveStaleWorktrees(t.Context(), repo, []string{bogusPath}, nil)

		// git worktree remove on a non-existent unregistered path fails.
		if len(result.Failed) != 1 {
			t.Errorf("RemoveStaleWorktrees(bogus path): Failed = %v, want 1 entry", result.Failed)
		}
		if len(result.Removed) != 0 {
			t.Errorf("RemoveStaleWorktrees(bogus path): Removed = %v, want empty", result.Removed)
		}
	})

	t.Run("sweep-then-remove-leaves-no-dir", func(t *testing.T) {
		t.Parallel()

		// End-to-end: SweepStaleLeaseLocks identifies the stale path, then
		// RemoveStaleWorktrees deletes it. Verifies the two-step integration.
		// Run_id must be a valid UUID because ReadLeaseLock calls uuid.Parse.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7dd2-8a1b-2c3d4e5fd004"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		leaseLockPath := LeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, 99999998, time.Now(), 3600))

		sweepResult, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("SweepStaleLeaseLocks: %v", err)
		}
		if len(sweepResult.Removed) == 0 {
			t.Fatal("SweepStaleLeaseLocks: expected stale path in Removed, got none")
		}

		gcResult := RemoveStaleWorktrees(t.Context(), repo, sweepResult.Removed, nil)

		if len(gcResult.Failed) > 0 {
			t.Errorf("RemoveStaleWorktrees: unexpected failures: %v", gcResult.Failed)
		}

		// Worktree directory must be gone.
		if _, statErr := os.Stat(worktreePath); !os.IsNotExist(statErr) {
			t.Errorf("worktree directory still present after sweep+remove; want deleted")
		}
	})
}
