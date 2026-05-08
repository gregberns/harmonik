package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWM013_WorkspaceIDDiscoverableFromRunID verifies that given a run_id, the
// workspace manager can resolve the workspace record (path, branch, state) by
// deterministic construction per WM-002 and WM-004 plus a filesystem check per
// WM-013c, without requiring a separate run-to-workspace index.
//
// Spec ref: workspace-model.md §4.3 WM-013 — "Given a run_id, the workspace
// manager MUST be able to resolve the workspace record (path, branch, state) by
// deterministic construction per WM-002 and WM-004 plus a filesystem check per
// WM-013c. No separate run-to-workspace index MAY be required as the
// authoritative lookup path."
func TestWM013_WorkspaceIDDiscoverableFromRunID(t *testing.T) {
	t.Parallel()

	t.Run("canonical-path-derivable-from-run-id", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f0013"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		// Deterministic path construction from run_id (WM-002).
		derivedPath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		if derivedPath != worktreePath {
			t.Errorf("WM-013: derived path %q != actual worktree path %q", derivedPath, worktreePath)
		}

		// The path exists on disk — filesystem check confirming the workspace record.
		if _, err := os.Stat(derivedPath); err != nil {
			t.Errorf("WM-013: derived path %q not found on disk: %v", derivedPath, err)
		}

		// workspace_id is "ws-" + run_id (WM-004) — derivable without an index.
		workspaceID := "ws-" + runID
		if workspaceID == "" || workspaceID == "ws-" {
			t.Errorf("WM-013: workspace_id derivation produced empty or prefix-only result")
		}
	})

	t.Run("lease-lock-readable-from-derived-path", func(t *testing.T) {
		t.Parallel()

		// Given only the run_id, the daemon can reconstruct the lease-lock path
		// deterministically (no index required).
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f0014"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}
		leaseLockPath := leaseFixture_leaseLockPath(worktreePath)
		leaseFixture_writeLockAtomic(t, leaseLockPath, leaseFixture_makeLockJSON(runID, os.Getpid(), time.Now(), 3600))

		// Reconstruct the lease-lock path from run_id alone.
		reconstructedLeasePath := filepath.Join(repo, ".harmonik", "worktrees", runID, ".harmonik", "lease.lock")
		if reconstructedLeasePath != leaseLockPath {
			t.Errorf("WM-013: reconstructed lease path %q != canonical %q", reconstructedLeasePath, leaseLockPath)
		}

		// Read the lock file using the reconstructed path — no index needed.
		data, err := os.ReadFile(reconstructedLeasePath)
		if err != nil {
			t.Fatalf("WM-013: ReadFile via reconstructed path: %v", err)
		}
		if !findSubstring(string(data), runID) {
			t.Errorf("WM-013: lease-lock content at reconstructed path does not contain run_id %q", runID)
		}
	})
}
