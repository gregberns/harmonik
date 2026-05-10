package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWM013c_DiscoverWorktrees verifies that DiscoverWorktrees performs the
// four startup discovery steps mandated by workspace-model.md §4.3 WM-013c:
//
//	(a) enumerate subdirectories matching the run_id regex,
//	(b) confirm registration in git via `git worktree list --porcelain`,
//	(c) read the lease-lock file to recover run_id/pid/created_at,
//	(d) stat ${path}/.harmonik/sessions/ to detect any started session.
func TestWM013c_DiscoverWorktrees(t *testing.T) {
	t.Parallel()

	t.Run("discovers-registered-worktrees-with-lease-lock", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0001"
		branch := TaskBranchName(runID)
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		// Create the worktree and write a lease lock.
		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}
		leaseLockPath := LeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now(), 3600))

		_ = branch // TaskBranchName is exercised by CreateWorktree

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}

		dw := discovered[0]

		// Step (a): run_id matches.
		if dw.RunID != runID {
			t.Errorf("WM-013c: RunID = %q, want %q", dw.RunID, runID)
		}

		// Step (b): registered in git.
		if !dw.RegisteredInGit {
			t.Errorf("WM-013c: RegisteredInGit = false, want true")
		}

		// Step (c): lease lock present with correct run_id and pid.
		if dw.LeaseLock == nil {
			t.Fatalf("WM-013c: LeaseLock = nil, want non-nil")
		}
		if dw.LeaseLock.RunID != runID {
			t.Errorf("WM-013c: LeaseLock.RunID = %q, want %q", dw.LeaseLock.RunID, runID)
		}
		if dw.LeaseLock.PID != os.Getpid() {
			t.Errorf("WM-013c: LeaseLock.PID = %d, want %d", dw.LeaseLock.PID, os.Getpid())
		}

		// Step (d): no sessions dir yet.
		if dw.HasSessionsDir {
			t.Errorf("WM-013c: HasSessionsDir = true, want false (no sessions created)")
		}
	})

	t.Run("step-d-true-when-sessions-dir-exists", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0002"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}

		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		// Pre-create the sessions root (simulating session start).
		sessionsRoot := SessionLogRootPath(worktreePath)
		if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll sessions root: %v", err)
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}
		if !discovered[0].HasSessionsDir {
			t.Errorf("WM-013c: HasSessionsDir = false, want true after sessions dir creation")
		}
	})

	t.Run("orphan-directory-flagged-registered-false", func(t *testing.T) {
		t.Parallel()

		// A directory under .harmonik/worktrees/ that is NOT registered in git
		// must be returned with RegisteredInGit == false.
		repo, _ := tempRepo(t)
		orphanRunID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0003"

		// Manually create the directory without going through `git worktree add`.
		orphanPath := filepath.Join(repo, ".harmonik", "worktrees", orphanRunID)
		if err := os.MkdirAll(orphanPath, 0o755); err != nil {
			t.Fatalf("MkdirAll orphan: %v", err)
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}
		if discovered[0].RegisteredInGit {
			t.Errorf("WM-013c: orphan directory marked RegisteredInGit = true, want false")
		}
	})

	t.Run("empty-root-returns-nil-slice", func(t *testing.T) {
		t.Parallel()

		// When the worktree root does not exist, DiscoverWorktrees must return
		// (nil, nil) — no error, no results.
		repo, _ := tempRepo(t)

		// Do NOT create .harmonik/worktrees/.
		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Errorf("WM-013c: DiscoverWorktrees on absent root: want nil error, got %v", err)
		}
		if len(discovered) != 0 {
			t.Errorf("WM-013c: DiscoverWorktrees on absent root: want 0 results, got %d", len(discovered))
		}
	})

	t.Run("non-matching-directories-skipped", func(t *testing.T) {
		t.Parallel()

		// Directories with names that do NOT match the [A-Za-z0-9-]+ regex are
		// skipped (e.g., .gitkeep, __pycache__, names with dots or underscores).
		repo, _ := tempRepo(t)

		worktreeRoot := filepath.Join(repo, ".harmonik", "worktrees")
		for _, name := range []string{".gitkeep", "not_valid", "also.invalid"} {
			if err := os.MkdirAll(filepath.Join(worktreeRoot, name), 0o755); err != nil {
				t.Fatalf("MkdirAll %q: %v", name, err)
			}
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 0 {
			t.Errorf("WM-013c: expected 0 discovered worktrees (all invalid names), got %d: %+v",
				len(discovered), discovered)
		}
	})

	t.Run("multiple-worktrees-discovered", func(t *testing.T) {
		t.Parallel()

		// Two registered worktrees both appear in the result.
		repo, sha := tempRepo(t)
		runIDs := []string{
			"0196a1b2-c3d4-713c-8a1b-2c3d4e5f0004",
			"0196a1b2-c3d4-713c-8a1b-2c3d4e5f0005",
		}

		for _, runID := range runIDs {
			if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
				t.Fatalf("CreateWorktree %q: %v", runID, err)
			}
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 2 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 2", len(discovered))
		}

		seenIDs := map[string]bool{}
		for _, dw := range discovered {
			seenIDs[dw.RunID] = true
			if !dw.RegisteredInGit {
				t.Errorf("WM-013c: worktree %q: RegisteredInGit = false, want true", dw.RunID)
			}
		}
		for _, runID := range runIDs {
			if !seenIDs[runID] {
				t.Errorf("WM-013c: run_id %q not found in discovered worktrees", runID)
			}
		}
	})

	t.Run("run-id-valid-filters-regex", func(t *testing.T) {
		t.Parallel()

		// RunIDValid is the production filter used by DiscoverWorktrees.
		// Verify its contract directly: [A-Za-z0-9-]+ only.
		cases := []struct {
			s    string
			want bool
		}{
			{"0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001", true},
			{"abcdef", true},
			{"ABC-123", true},
			{"", false},
			{"has_underscore", false},
			{"has.dot", false},
			{"has space", false},
			{".hidden", false},
		}
		for _, tc := range cases {
			got := RunIDValid(tc.s)
			if got != tc.want {
				t.Errorf("RunIDValid(%q) = %v, want %v", tc.s, got, tc.want)
			}
		}
	})

	t.Run("worktree-path-matches-canonical-construction", func(t *testing.T) {
		t.Parallel()

		// WorktreePath in DiscoveredWorktree must equal WorktreePath(repo, runID, NoWorktreeRootOverride()).
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0006"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}
		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}

		wantPath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		if discovered[0].WorktreePath != wantPath {
			t.Errorf("WM-013c: WorktreePath = %q, want %q", discovered[0].WorktreePath, wantPath)
		}
	})
}

// TestWM013c_DiscoverWorktreesBranchConvention verifies that DiscoverWorktrees
// and CreateWorktree together satisfy the WM-013c step (b) + step (c) integration:
// a worktree created via CreateWorktree is immediately discoverable with its
// lease-lock, branch, and path all consistent.
func TestWM013c_DiscoverWorktreesBranchConvention(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0007"

	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
	leaseLockPath := LeaseLockPath(worktreePath)
	leaseFixtureWriteLockAtomic(t, leaseLockPath,
		leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now(), 3600))

	// Confirm the branch exists at the expected task-branch name.
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo,
		"rev-parse", "--verify", TaskBranchName(runID)).Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("WM-013c: task branch %q not found: %v", TaskBranchName(runID), err)
	}

	discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
	}
	if len(discovered) != 1 || discovered[0].RunID != runID {
		t.Errorf("WM-013c: integration: discovered %+v, want run_id %q", discovered, runID)
	}
}
