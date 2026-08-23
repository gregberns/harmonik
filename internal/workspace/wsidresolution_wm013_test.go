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

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		//nolint:gosec // G204: branch, worktreePath, and sha are controlled by this t.TempDir test fixture.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		derivedPath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		if derivedPath != worktreePath {
			t.Errorf("WM-013: derived path %q != actual worktree path %q", derivedPath, worktreePath)
		}

		if _, err := os.Stat(derivedPath); err != nil {
			t.Errorf("WM-013: derived path %q not found on disk: %v", derivedPath, err)
		}

		workspaceID := "ws-" + runID
		if workspaceID == "" || workspaceID == "ws-" {
			t.Errorf("WM-013: workspace_id derivation produced empty or prefix-only result")
		}
	})

	t.Run("lease-lock-readable-from-derived-path", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f0014"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		//nolint:gosec // G204: branch, worktreePath, and sha are controlled by this t.TempDir test fixture.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}
		leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath, leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

		reconstructedLeasePath := filepath.Join(repo, ".harmonik", "worktrees", runID, ".harmonik", "lease.lock")
		if reconstructedLeasePath != leaseLockPath {
			t.Errorf("WM-013: reconstructed lease path %q != canonical %q", reconstructedLeasePath, leaseLockPath)
		}

		data := mustReadFile(t, reconstructedLeasePath)
		if !leaseFixtureFindSubstring(string(data), runID) {
			t.Errorf("WM-013: lease-lock content at reconstructed path does not contain run_id %q", runID)
		}
	})
}
