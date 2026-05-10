package workspace

import (
	"path/filepath"
	"testing"
)

// TestWM002_WorktreePath locks the canonical worktree path shape required by
// workspace-model.md §4.1 WM-002 and §6.2:
//
//	<repo>/.harmonik/worktrees/<run_id>/
//
// Spec ref: workspace-model.md §4.1 WM-002 — "Every workspace MUST be created
// at the canonical path `<repo>/.harmonik/worktrees/<run_id>/`."
func TestWM002_WorktreePath(t *testing.T) {
	t.Parallel()

	t.Run("canonical-path-shape-default-root", func(t *testing.T) {
		t.Parallel()

		// WM-002: default root is <repo>/.harmonik/worktrees/
		repoRoot := "/home/op/repos/myproject"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0002"

		wantPath := filepath.Join(repoRoot, ".harmonik", "worktrees", runID)
		gotPath := WorktreePath(repoRoot, runID, nil)

		if gotPath != wantPath {
			t.Errorf("WM-002: WorktreePath(default) = %q, want %q", gotPath, wantPath)
		}
	})

	t.Run("canonical-path-shape-override-root-absolute", func(t *testing.T) {
		t.Parallel()

		// WM-002 + CP-037: operator-configurable worktree root (absolute override).
		repoRoot := "/home/op/repos/myproject"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0003"
		override := "/mnt/fast-ssd/harmonik-worktrees"

		wantPath := filepath.Join(override, runID)
		gotPath := WorktreePath(repoRoot, runID, &override)

		if gotPath != wantPath {
			t.Errorf("WM-002: WorktreePath(abs override) = %q, want %q", gotPath, wantPath)
		}
	})

	t.Run("canonical-path-shape-override-root-relative", func(t *testing.T) {
		t.Parallel()

		// WM-002 + CP-037: operator-configurable worktree root (relative override
		// is joined against repoRoot).
		repoRoot := "/home/op/repos/myproject"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0004"
		override := "scratch/worktrees"

		wantPath := filepath.Join(repoRoot, override, runID)
		gotPath := WorktreePath(repoRoot, runID, &override)

		if gotPath != wantPath {
			t.Errorf("WM-002: WorktreePath(rel override) = %q, want %q", gotPath, wantPath)
		}
	})

	t.Run("empty-override-is-treated-as-nil", func(t *testing.T) {
		t.Parallel()

		// An empty string override falls through to the default.
		repoRoot := "/home/op/repos/myproject"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0005"
		empty := ""

		wantPath := filepath.Join(repoRoot, ".harmonik", "worktrees", runID)
		gotPath := WorktreePath(repoRoot, runID, &empty)

		if gotPath != wantPath {
			t.Errorf("WM-002: WorktreePath(empty override) = %q, want %q", gotPath, wantPath)
		}
	})

	t.Run("run-id-embedded-at-final-segment", func(t *testing.T) {
		t.Parallel()

		// The run_id MUST be the final path segment (per-run subdirectory is fixed).
		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0006"

		gotPath := WorktreePath(repoRoot, runID, nil)
		gotBase := filepath.Base(gotPath)

		if gotBase != runID {
			t.Errorf("WM-002: WorktreePath base = %q, want run_id %q as final segment", gotBase, runID)
		}
	})

	t.Run("default-root-contains-harmonik-worktrees", func(t *testing.T) {
		t.Parallel()

		// The default root segment sequence MUST be .harmonik/worktrees under repo root.
		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0007"

		wantRoot := filepath.Join(repoRoot, ".harmonik", "worktrees")
		gotRoot := WorktreeRootPath(repoRoot, nil)

		if gotRoot != wantRoot {
			t.Errorf("WM-002: WorktreeRootPath(default) = %q, want %q", gotRoot, wantRoot)
		}

		// WorktreePath must start with the default root.
		gotPath := WorktreePath(repoRoot, runID, nil)
		if filepath.Dir(gotPath) != wantRoot {
			t.Errorf("WM-002: WorktreePath parent dir = %q, want %q", filepath.Dir(gotPath), wantRoot)
		}
	})

	t.Run("path-is-per-leaselock-sibling-convention", func(t *testing.T) {
		t.Parallel()

		// WorktreePath output feeds into LeaseLockPath; the composite path must
		// equal <repo>/.harmonik/worktrees/<run_id>/.harmonik/lease.lock.
		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7001-8a1b-2c3d4e5f0008"

		worktreePath := WorktreePath(repoRoot, runID, nil)
		leasePath := LeaseLockPath(worktreePath)

		wantLeasePath := filepath.Join(repoRoot, ".harmonik", "worktrees", runID, ".harmonik", "lease.lock")
		if leasePath != wantLeasePath {
			t.Errorf("WM-002+WM-013a composite: LeaseLockPath = %q, want %q", leasePath, wantLeasePath)
		}
	})
}
