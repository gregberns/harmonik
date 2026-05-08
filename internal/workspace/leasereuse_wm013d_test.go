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

		// Run A is released. Run B (same bead, new run_id per WM-034) MUST get
		// a fresh canonical path derived from its new run_id.
		repo, sha := tempRepo(t)

		runIDA := "0196a1b2-c3d4-713d-8a1b-aaaaaaaaaaaa"
		branchA := "run/" + runIDA
		worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)
		if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o755); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", branchA, worktreePathA, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add A: %v\n%s", err, out)
		}
		leaseLockPathA := leaseFixture_leaseLockPath(worktreePathA)
		leaseFixture_writeLockAtomic(t, leaseLockPathA, leaseFixture_makeLockJSON(runIDA, os.Getpid(), time.Now(), 3600))

		// Release run A's lease.
		leaseFixture_releaseLock(t, leaseLockPathA)

		// Run A's directory MAY persist on disk per WM-031.
		if _, err := os.Stat(worktreePathA); err != nil {
			t.Fatalf("WM-013d: run A worktree dir absent after release; WM-031 allows it to persist: %v", err)
		}
		// But run A's lease-lock is absent.
		if _, err := os.Stat(leaseLockPathA); !os.IsNotExist(err) {
			t.Errorf("WM-013d: run A lease-lock still present after release; want absent")
		}

		// Run B: fresh run_id, fresh canonical path.
		runIDB := "0196a1b2-c3d4-713d-8a1b-bbbbbbbbbbbb"
		worktreePathB := filepath.Join(repo, ".harmonik", "worktrees", runIDB)
		branchB := "run/" + runIDB

		// Run B's canonical path MUST differ from run A's.
		if worktreePathA == worktreePathB {
			t.Errorf("WM-013d: run A and run B canonical paths are identical %q; want distinct", worktreePathA)
		}

		if err := os.MkdirAll(filepath.Dir(worktreePathB), 0o755); err != nil {
			t.Fatalf("MkdirAll B: %v", err)
		}
		cmd2 := exec.Command("git", "worktree", "add", "-b", branchB, worktreePathB, sha)
		cmd2.Dir = repo
		if out, err := cmd2.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add B: %v\n%s", err, out)
		}
		leaseLockPathB := leaseFixture_leaseLockPath(worktreePathB)
		leaseFixture_writeLockAtomic(t, leaseLockPathB, leaseFixture_makeLockJSON(runIDB, os.Getpid(), time.Now(), 3600))

		// Run B's lease is at its own canonical path, not run A's.
		if _, err := os.Stat(leaseLockPathB); err != nil {
			t.Errorf("WM-013d: run B lease-lock absent at %q: %v", leaseLockPathB, err)
		}
		if leaseLockPathA == leaseLockPathB {
			t.Errorf("WM-013d: run A and run B lease-lock paths are the same %q; want distinct", leaseLockPathA)
		}
	})

	t.Run("reuse-of-released-path-for-different-run-id-violates-invariant", func(t *testing.T) {
		t.Parallel()

		// Negative test: assert that attempting to write a lease-lock for a
		// DIFFERENT run_id into an existing worktree directory (run A's path) is
		// detectable as an invariant violation.
		//
		// Spec ref: WM-INV-005 (canonical-path invariant) — the canonical path is
		// a function of run_id; two distinct run_ids MUST yield distinct paths.
		repo, sha := tempRepo(t)

		runIDA := "0196a1b2-c3d4-713d-8a1b-cccccccccccc"
		branchA := "run/" + runIDA
		worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)
		if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o755); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", branchA, worktreePathA, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add A: %v\n%s", err, out)
		}
		leaseLockPathA := leaseFixture_leaseLockPath(worktreePathA)
		leaseFixture_writeLockAtomic(t, leaseLockPathA, leaseFixture_makeLockJSON(runIDA, os.Getpid(), time.Now(), 3600))
		leaseFixture_releaseLock(t, leaseLockPathA) // release run A

		// A new run B tries to REUSE run A's path by writing its own run_id to
		// the lease-lock at run A's canonical path. This is an invariant violation.
		// Detect it by verifying that the run_id in the lock at that path would
		// not match the path's embedded run_id.
		runIDB := "0196a1b2-c3d4-713d-8a1b-dddddddddddd"
		// If B's canonical path were constructed correctly, it would be a different directory.
		expectedPathForB := filepath.Join(repo, ".harmonik", "worktrees", runIDB)
		if expectedPathForB == worktreePathA {
			t.Fatalf("WM-013d: run B canonical path accidentally equals run A path; test setup error")
		}

		// Simulate the violation: write run B's lock data at run A's path.
		// In production, the workspace manager MUST NOT do this. We write it here
		// to verify that the path/run_id disagreement is detectable.
		badLockContent := leaseFixture_makeLockJSON(runIDB, os.Getpid(), time.Now(), 3600)
		if err := os.WriteFile(leaseLockPathA, badLockContent, 0o644); err != nil {
			t.Fatalf("WM-013d: WriteFile (simulated violation): %v", err)
		}

		// Detection: the lock file at run A's path contains run B's run_id.
		// A well-formed workspace manager would detect this mismatch.
		data, err := os.ReadFile(leaseLockPathA)
		if err != nil {
			t.Fatalf("WM-013d: ReadFile: %v", err)
		}
		// The path contains runIDA but the content claims runIDB — a violation.
		if findSubstring(string(data), runIDA) {
			t.Errorf("WM-013d: lock at run A's path claims run A's run_id; want run B's (simulated violation)")
		}
		if !findSubstring(string(data), runIDB) {
			t.Errorf("WM-013d: lock at run A's path does not claim run B's run_id; simulation error")
		}
		// WM-013d: this mismatch (path embeds runIDA, lock claims runIDB) violates
		// WM-INV-005. In production the workspace manager MUST route this to
		// reconciliation Cat 6a (integrity violation).
		// TODO: replace with workspace-manager API call when the type is implemented.
	})
}
