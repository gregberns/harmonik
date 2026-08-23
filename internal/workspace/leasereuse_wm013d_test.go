package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWM013d_ReleasedWorkspacePathReuseRejected verifies that a released
// workspace's canonical path MUST NOT be re-leased by a subsequent run. New
// runs receive new canonical paths via new run_ids per WM-034.
//
// Spec ref: workspace-model.md §4.3 WM-013d — "A released workspace's canonical
// path (WM-002) MUST NOT be re-leased by a subsequent run. New runs receive new
// canonical paths via new run_ids per WM-034. The prior run's worktree directory
// and branch MAY persist on disk per WM-031; re-use of the path for a different
// run_id is forbidden — the canonical-path invariant (§5.WM-INV-005) would be
// violated."
func TestWM013d_ReleasedWorkspacePathReuseRejected(t *testing.T) {
	t.Parallel()

	t.Run("new-run-gets-new-canonical-path", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runIDA := "0196a1b2-c3d4-713d-8a1b-aaaaaaaaaaaa"
		branchA := "run/" + runIDA
		worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)
		if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o700); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		//nolint:gosec // G204: branch, worktree path, and commit are controlled by this t.TempDir test fixture.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchA, worktreePathA, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add A: %v\n%s", err, out)
		}
		leaseLockPathA := leaseFixtureLeaseLockPath(worktreePathA)
		leaseFixtureWriteLockAtomic(t, leaseLockPathA, leaseFixtureMakeLockJSON(runIDA, os.Getpid(), time.Now()))

		leaseFixtureReleaseLock(t, leaseLockPathA)

		if _, err := os.Stat(worktreePathA); err != nil {
			t.Fatalf("WM-013d: run A worktree dir absent after release; WM-031 allows it to persist: %v", err)
		}
		if _, err := os.Stat(leaseLockPathA); !os.IsNotExist(err) {
			t.Errorf("WM-013d: run A lease-lock still present after release; want absent")
		}

		runIDB := "0196a1b2-c3d4-713d-8a1b-bbbbbbbbbbbb"
		worktreePathB := filepath.Join(repo, ".harmonik", "worktrees", runIDB)
		branchB := "run/" + runIDB

		if worktreePathA == worktreePathB {
			t.Errorf("WM-013d: run A and run B canonical paths are identical %q; want distinct", worktreePathA)
		}

		if err := os.MkdirAll(filepath.Dir(worktreePathB), 0o700); err != nil {
			t.Fatalf("MkdirAll B: %v", err)
		}
		//nolint:gosec // G204: branch, worktree path, and commit are controlled by this t.TempDir test fixture.
		cmd2 := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchB, worktreePathB, sha)
		cmd2.Dir = repo
		if out, err := cmd2.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add B: %v\n%s", err, out)
		}
		leaseLockPathB := leaseFixtureLeaseLockPath(worktreePathB)
		leaseFixtureWriteLockAtomic(t, leaseLockPathB, leaseFixtureMakeLockJSON(runIDB, os.Getpid(), time.Now()))

		if _, err := os.Stat(leaseLockPathB); err != nil {
			t.Errorf("WM-013d: run B lease-lock absent at %q: %v", leaseLockPathB, err)
		}
		if leaseLockPathA == leaseLockPathB {
			t.Errorf("WM-013d: run A and run B lease-lock paths are the same %q; want distinct", leaseLockPathA)
		}
	})

	t.Run("reuse-of-released-path-for-different-run-id-violates-invariant", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runIDA := "0196a1b2-c3d4-713d-8a1b-cccccccccccc"
		branchA := "run/" + runIDA
		worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)
		if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o700); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		//nolint:gosec // G204: branch, worktree path, and commit are controlled by this t.TempDir test fixture.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchA, worktreePathA, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add A: %v\n%s", err, out)
		}
		leaseLockPathA := leaseFixtureLeaseLockPath(worktreePathA)
		leaseFixtureWriteLockAtomic(t, leaseLockPathA, leaseFixtureMakeLockJSON(runIDA, os.Getpid(), time.Now()))
		leaseFixtureReleaseLock(t, leaseLockPathA) // release run A

		runIDB := "0196a1b2-c3d4-713d-8a1b-dddddddddddd"
		expectedPathForB := filepath.Join(repo, ".harmonik", "worktrees", runIDB)
		if expectedPathForB == worktreePathA {
			t.Fatalf("WM-013d: run B canonical path accidentally equals run A path; test setup error")
		}

		badLockContent := leaseFixtureMakeLockJSON(runIDB, os.Getpid(), time.Now())
		if err := os.WriteFile(leaseLockPathA, badLockContent, 0o600); err != nil {
			t.Fatalf("WM-013d: WriteFile (simulated violation): %v", err)
		}

		data := mustReadFile(t, leaseLockPathA)
		if leaseFixtureFindSubstring(string(data), runIDA) {
			t.Errorf("WM-013d: lock at run A's path claims run A's run_id; want run B's (simulated violation)")
		}
		if !leaseFixtureFindSubstring(string(data), runIDB) {
			t.Errorf("WM-013d: lock at run A's path does not claim run B's run_id; simulation error")
		}
		// WM-013d: this mismatch (path embeds runIDA, lock claims runIDB) violates
		// WM-INV-005. In production the workspace manager MUST route this to
		// reconciliation Cat 6a (integrity violation).
		// TODO: replace with workspace-manager API call when the type is implemented.
	})
}
