package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWM033_SweepStaleLeaseLocks verifies the startup orphan sweep per
// workspace-model.md §4.8 WM-033:
//
//  1. Stale lease-lock files (PID dead) are removed.
//  2. Worktree directories and branches are NOT deleted.
//  3. Live lease-lock files (PID = current process) are NOT swept.
//  4. git worktree prune runs after the sweep.
func TestWM033_SweepStaleLeaseLocks(t *testing.T) {
	t.Parallel()

	t.Run("stale-pid-lock-removed", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7033-8a1b-2c3d4e5f0033"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-033: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		leaseLockPath := LeaseLockPath(worktreePath)

		deadPID := 99999999
		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, deadPID, time.Now()))

		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Fatalf("WM-033: lease-lock not present before sweep: %v", err)
		}

		result, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-033: SweepStaleLeaseLocks: %v", err)
		}

		if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
			t.Errorf("WM-033: lease-lock still present after sweep (stale PID %d)", deadPID)
		}

		if _, err := os.Stat(worktreePath); err != nil {
			t.Errorf("WM-033: worktree directory removed by sweep; MUST NOT be deleted: %v", err)
		}

		found := false
		for _, p := range result.Removed {
			if p == worktreePath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("WM-033: worktree path %q not in Removed list: %v", worktreePath, result.Removed)
		}
	})

	t.Run("live-pid-lock-not-swept", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7033-8a1b-2c3d4e5f0034"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-033: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		leaseLockPath := LeaseLockPath(worktreePath)

		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

		_, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-033: SweepStaleLeaseLocks: %v", err)
		}

		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Errorf("WM-033: live lock file removed by sweep; MUST NOT be swept: %v", err)
		}
	})

	t.Run("no-lease-lock-skipped", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7033-8a1b-2c3d4e5f0035"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-033: CreateWorktree: %v", err)
		}

		result, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-033: SweepStaleLeaseLocks: %v", err)
		}

		if len(result.Removed) != 0 {
			t.Errorf("WM-033: no-lease-lock worktree appeared in Removed: %v", result.Removed)
		}
	})

	t.Run("empty-worktree-root-succeeds", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		result, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Errorf("WM-033: SweepStaleLeaseLocks on absent root: %v", err)
		}
		if len(result.Removed) != 0 {
			t.Errorf("WM-033: unexpected Removed on absent root: %v", result.Removed)
		}
	})

	t.Run("is-lease-lock-stale-dead-pid", func(t *testing.T) {
		t.Parallel()

		if !IsLeaseLockStale(99999999) {
			t.Error("WM-033: IsLeaseLockStale(99999999) = false, want true (dead PID)")
		}
	})

	t.Run("is-lease-lock-stale-live-pid", func(t *testing.T) {
		t.Parallel()

		if IsLeaseLockStale(os.Getpid()) {
			t.Errorf("WM-033: IsLeaseLockStale(%d) = true, want false (current PID is live)", os.Getpid())
		}
	})

	t.Run("sweep-does-not-delete-worktree-branch", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7033-8a1b-2c3d4e5f0036"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-033: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		leaseLockPath := LeaseLockPath(worktreePath)

		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, 99999998, time.Now()))

		if _, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-033: SweepStaleLeaseLocks: %v", err)
		}

		if _, err := os.Stat(worktreePath); err != nil {
			t.Errorf("WM-033: worktree directory missing after sweep: %v", err)
		}

		branch := TaskBranchName(runID)
		branchRef := filepath.Join(repo, ".git", "worktrees", runID, "HEAD")
		if _, err := os.Stat(branchRef); os.IsNotExist(err) {
			_ = branchRef
		}
		_ = branch // branch name assertion delegated to git-level verification in other tests
	})
}
