package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWM012_OneRunPerBeadAtATime verifies that at most one run is in flight for
// a given bead at any instant, enforced at the worktree layer by the lease-lock
// file: a second run for the same bead is only permitted after the first has
// reached a terminal state.
//
// Spec ref: workspace-model.md §4.3 WM-012 — "At any instant, AT MOST ONE run
// MAY be in flight for a given bead. A second run for the same bead is permitted
// ONLY after the first has reached a terminal state (completed, failed, or
// canceled) per [execution-model.md §4.3]. Re-claim semantics are defined in
// §4.9."
func TestWM012_OneRunPerBeadAtATime(t *testing.T) {
	t.Parallel()

	t.Run("first-run-lease-present-blocks-second", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runIDA := "0196a1b2-c3d4-7012-8a1b-aaaaaaaaaaaa"
		branchA := "run/" + runIDA
		worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)
		if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o700); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchA, worktreePathA, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add A: %v\n%s", err, out)
		}
		leaseLockPathA := leaseFixtureLeaseLockPath(worktreePathA)
		leaseFixtureWriteLockAtomic(t, leaseLockPathA, leaseFixtureMakeLockJSON(runIDA, os.Getpid(), time.Now()))

		if _, err := os.Stat(leaseLockPathA); err != nil {
			t.Fatalf("WM-012: run A lease-lock absent; want present to block run B: %v", err)
		}
	})

	t.Run("second-run-permitted-after-first-terminal", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runIDA := "0196a1b2-c3d4-7012-8a1b-aaaabbbbcccc"
		branchA := "run/" + runIDA
		worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)
		if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o700); err != nil {
			t.Fatalf("MkdirAll A: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchA, worktreePathA, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add A: %v\n%s", err, out)
		}
		leaseLockPathA := leaseFixtureLeaseLockPath(worktreePathA)
		leaseFixtureWriteLockAtomic(t, leaseLockPathA, leaseFixtureMakeLockJSON(runIDA, os.Getpid(), time.Now()))

		leaseFixtureReleaseLock(t, leaseLockPathA)
		if _, err := os.Stat(leaseLockPathA); !os.IsNotExist(err) {
			t.Fatalf("WM-012: run A lease-lock still present after terminal; want absent")
		}

		runIDB := "0196a1b2-c3d4-7012-8a1b-bbbbccccdddd"
		branchB := "run/" + runIDB
		worktreePathB := filepath.Join(repo, ".harmonik", "worktrees", runIDB)
		if worktreePathA == worktreePathB {
			t.Fatalf("WM-012: run A and run B have the same canonical path; want distinct paths")
		}
		if err := os.MkdirAll(filepath.Dir(worktreePathB), 0o700); err != nil {
			t.Fatalf("MkdirAll B: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed by this test.
		cmd2 := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchB, worktreePathB, sha)
		cmd2.Dir = repo
		if out, err := cmd2.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add B: %v\n%s", err, out)
		}
		leaseLockPathB := leaseFixtureLeaseLockPath(worktreePathB)
		leaseFixtureWriteLockAtomic(t, leaseLockPathB, leaseFixtureMakeLockJSON(runIDB, os.Getpid(), time.Now()))

		if _, err := os.Stat(leaseLockPathB); err != nil {
			t.Errorf("WM-012: run B lease-lock absent; want present: %v", err)
		}
		if _, err := os.Stat(leaseLockPathA); !os.IsNotExist(err) {
			t.Errorf("WM-012: run A lease-lock still present; want absent after terminal")
		}
	})
}
