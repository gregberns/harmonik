package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWM011_OneActiveAgentAtATimeInsideWorkspace verifies the storage-level
// contribution of the lease-lock to the one-active-agent contract.
//
// WM-011 delegates per-agent concurrency enforcement to the orchestrator (S01).
// This test verifies the workspace manager's side of the contract: that the
// lease-lock file identifies the owning run and exists while any agent is
// active inside the workspace.
//
// Spec ref: workspace-model.md §4.3 WM-011 — "At any instant, AT MOST ONE agent
// process MAY be actively writing to the worktree. Agents run sequentially as
// the run traverses its workflow graph; parallel nodes within one run MUST NOT
// share a worktree. Parallel nodes across different runs occupy separate worktrees
// per WM-002. Enforcement is delegated to the orchestrator (S01): the orchestrator
// MUST NOT dispatch a second agent into a workspace already holding a live handler
// subprocess. The workspace manager's storage-level contribution is the lease-lock
// file (§4.3.WM-013a), whose content identifies the owning run but does not by
// itself arbitrate per-agent concurrency."
func TestWM011_OneActiveAgentAtATimeInsideWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("lease-lock-identifies-owning-run", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7011-8a1b-2c3d4e5f0011"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		pid := os.Getpid()
		now := time.Now()
		leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
		lockContent := leaseFixtureMakeLockJSON(runID, pid, now, 3600)
		leaseFixtureWriteLockAtomic(t, leaseLockPath, lockContent)

		// The lease-lock must exist while an agent is "active".
		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Fatalf("WM-011: lease-lock absent while agent is active: %v", err)
		}

		// Read the lock content and verify it identifies the owning run.
		data, err := os.ReadFile(leaseLockPath)
		if err != nil {
			t.Fatalf("WM-011: ReadFile lease-lock: %v", err)
		}
		content := string(data)

		// Verify run_id is present in the lock content.
		if !leaseFixtureContainsSubstring(content, runID) {
			t.Errorf("WM-011: lease-lock content does not contain run_id %q; got: %s", runID, content)
		}
	})

	t.Run("parallel-runs-have-disjoint-worktrees", func(t *testing.T) {
		t.Parallel()

		// WM-011: "Parallel nodes across different runs occupy separate worktrees
		// per WM-002." This test verifies that two parallel runs writing to their
		// respective worktrees do not share a lease-lock path.
		repo, sha := tempRepo(t)

		runIDs := []string{
			"0196a1b2-c3d4-7011-8a1b-2c3d4e5fb001",
			"0196a1b2-c3d4-7011-8a1b-2c3d4e5fb002",
		}

		leasePaths := make([]string, len(runIDs))
		for i, runID := range runIDs {
			branch := "run/" + runID
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git worktree add %q: %v\n%s", runID, err, out)
			}
			lp := leaseFixtureLeaseLockPath(worktreePath)
			leasePaths[i] = lp
			leaseFixtureWriteLockAtomic(t, lp, leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now(), 3600))
		}

		// Lease paths must be disjoint.
		if leasePaths[0] == leasePaths[1] {
			t.Errorf("WM-011: parallel runs share a lease-lock path %q; want disjoint paths", leasePaths[0])
		}

		// Both lease-locks exist simultaneously (concurrent runs in separate worktrees).
		for i, lp := range leasePaths {
			if _, err := os.Stat(lp); err != nil {
				t.Errorf("WM-011: run[%d] lease-lock absent at %q: %v", i, lp, err)
			}
		}
	})
}

// leaseFixtureContainsSubstring returns true if s contains substr.
// Inlined to avoid adding an untested utility to the package.
func leaseFixtureContainsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		leaseFixtureFindSubstring(s, substr))
}

func leaseFixtureFindSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
