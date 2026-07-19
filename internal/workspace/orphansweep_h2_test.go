package workspace

// orphansweep_h2_test.go — H2 / H2b regressions.
//
// H2: a corrupt/truncated lease.lock must be treated as "lock present, state
// unknown" (fail-safe), NEVER as absent. Mistaking it for absent routes the
// worktree to NoLock → RemoveAgedNoLockWorktrees force-removes a possibly-live
// worktree.
//
// H2b: RemoveAgedNoLockWorktrees must derive activity from the newest file mtime
// WITHIN the tree, not the top-dir mtime, so an in-place file edit (which never
// bumps the top-dir mtime) protects a worktree from age-based removal.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestH2_DiscoverWorktrees_CorruptLock_Unreadable verifies a corrupt lease.lock
// yields LeaseLockUnreadable=true, LeaseLock=nil.
func TestH2_DiscoverWorktrees_CorruptLock_Unreadable(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-7h20-8a1b-2c3d4e5f0h20"
	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

	// Write a truncated / non-JSON lease.lock (corrupt).
	leaseLockPath := LeaseLockPath(worktreePath)
	if err := os.MkdirAll(filepath.Dir(leaseLockPath), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(leaseLockPath, []byte(`{"run_id":"trunc`), 0o600); err != nil {
		t.Fatalf("write corrupt lock: %v", err)
	}

	discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("DiscoverWorktrees: %v", err)
	}
	var found *DiscoveredWorktree
	for i := range discovered {
		if discovered[i].RunID == runID {
			found = &discovered[i]
		}
	}
	if found == nil {
		t.Fatalf("worktree %s not discovered", runID)
	}
	if !found.LeaseLockUnreadable {
		t.Errorf("LeaseLockUnreadable = false; want true for a corrupt lock")
	}
	if found.LeaseLock != nil {
		t.Errorf("LeaseLock = %+v; want nil for a corrupt lock", found.LeaseLock)
	}
}

// TestH2_SweepStaleLeaseLocks_CorruptLock_NotNoLock verifies the sweep does NOT
// classify a corrupt-lock worktree as NoLock (which would enable age-removal).
func TestH2_SweepStaleLeaseLocks_CorruptLock_NotNoLock(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-7h21-8a1b-2c3d4e5f0h21"
	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
	leaseLockPath := LeaseLockPath(worktreePath)
	if err := os.MkdirAll(filepath.Dir(leaseLockPath), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(leaseLockPath, []byte("not json at all"), 0o600); err != nil {
		t.Fatalf("write corrupt lock: %v", err)
	}

	result, err := SweepStaleLeaseLocks(t.Context(), repo, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("SweepStaleLeaseLocks: %v", err)
	}
	for _, p := range result.NoLock {
		if p == worktreePath {
			t.Errorf("corrupt-lock worktree %q was classified NoLock; want quarantined (skipped, not NoLock)", worktreePath)
		}
	}
	for _, p := range result.Removed {
		if p == worktreePath {
			t.Errorf("corrupt-lock worktree %q lease-lock was removed; want left intact", worktreePath)
		}
	}
	// The lock file must still be present (not removed).
	if _, statErr := os.Stat(leaseLockPath); statErr != nil {
		t.Errorf("corrupt lock file removed by sweep: %v", statErr)
	}
}

// setTreeMTime sets every entry (dir + files) under root to modTime.
func setTreeMTime(t *testing.T, root string, modTime time.Time) {
	t.Helper()
	err := filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, modTime, modTime)
	})
	if err != nil {
		t.Fatalf("setTreeMTime: %v", err)
	}
}

// TestH2b_RemoveAgedNoLockWorktrees_InPlaceEditProtects verifies an in-place file
// edit (recent file mtime, stale top-dir mtime) prevents age-based removal.
func TestH2b_RemoveAgedNoLockWorktrees_InPlaceEditProtects(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-7h2b-8a1b-2c3d4e5f0h2b"
	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

	// Age the ENTIRE tree (including the top dir) to 2h ago.
	old := time.Now().Add(-2 * time.Hour)
	setTreeMTime(t, worktreePath, old)

	// Simulate an in-place edit: write to an existing file WITHOUT touching the
	// top-dir mtime (writing to an existing file does not change the parent dir
	// mtime). README exists from tempRepo's initial commit.
	editPath := filepath.Join(worktreePath, "README")
	if err := os.WriteFile(editPath, []byte("edited in place\n"), 0o600); err != nil {
		t.Fatalf("in-place edit: %v", err)
	}

	// maxAge=1h: top-dir mtime (2h) is older, but the in-place edit is recent, so
	// the worktree MUST be protected (not removed).
	result := RemoveAgedNoLockWorktrees(t.Context(), repo, []string{worktreePath}, time.Hour, nil)
	if len(result.Removed) != 0 {
		t.Errorf("in-place-edited worktree removed: %v; want protected by recent file mtime", result.Removed)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("worktree directory removed: %v; want intact", err)
	}
}

// TestH2b_RemoveAgedNoLockWorktrees_FullyAgedRemoved verifies a genuinely-idle
// worktree (all mtimes old) is still removed.
func TestH2b_RemoveAgedNoLockWorktrees_FullyAgedRemoved(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-7h2c-8a1b-2c3d4e5f0h2c"
	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

	setTreeMTime(t, worktreePath, time.Now().Add(-2*time.Hour))

	result := RemoveAgedNoLockWorktrees(t.Context(), repo, []string{worktreePath}, time.Hour, nil)
	if len(result.Removed) != 1 || result.Removed[0] != worktreePath {
		t.Errorf("fully-aged worktree not removed: Removed=%v Failed=%v; want [%q]", result.Removed, result.Failed, worktreePath)
	}
}
