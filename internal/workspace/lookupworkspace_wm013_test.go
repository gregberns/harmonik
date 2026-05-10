package workspace

import (
	"path/filepath"
	"testing"
)

// TestWM013_LookupWorkspace verifies that LookupWorkspace resolves the workspace
// reference from a run_id by deterministic construction per workspace-model.md
// §4.3 WM-013, without consulting any separate index.
//
// Spec ref: workspace-model.md §4.3 WM-013 — "Given a run_id, the workspace
// manager MUST be able to resolve the workspace record (path, branch, state) by
// deterministic construction per WM-002 and WM-004 plus a filesystem check per
// WM-013c. No separate run-to-workspace index MAY be required as the
// authoritative lookup path."
func TestWM013_LookupWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("path-derivable-from-run-id-wm-002", func(t *testing.T) {
		t.Parallel()

		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f0018"

		ref, err := LookupWorkspace(repoRoot, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: LookupWorkspace: %v", err)
		}

		wantPath := filepath.Join(repoRoot, ".harmonik", "worktrees", runID)
		if ref.Path != wantPath {
			t.Errorf("WM-013: ref.Path = %q, want %q", ref.Path, wantPath)
		}
	})

	t.Run("workspace-id-derivable-from-run-id-wm-004", func(t *testing.T) {
		t.Parallel()

		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f0019"

		ref, err := LookupWorkspace(repoRoot, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: LookupWorkspace: %v", err)
		}

		wantWsID := "ws-" + runID
		if ref.WorkspaceID != wantWsID {
			t.Errorf("WM-013: ref.WorkspaceID = %q, want %q", ref.WorkspaceID, wantWsID)
		}
	})

	t.Run("branch-derivable-from-run-id-wm-005", func(t *testing.T) {
		t.Parallel()

		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f001a"

		ref, err := LookupWorkspace(repoRoot, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: LookupWorkspace: %v", err)
		}

		wantBranch := "run/" + runID
		if ref.Branch != wantBranch {
			t.Errorf("WM-013: ref.Branch = %q, want %q", ref.Branch, wantBranch)
		}
	})

	t.Run("exists-on-disk-false-when-absent", func(t *testing.T) {
		t.Parallel()

		// Before CreateWorktree, ExistsOnDisk must be false.
		repo, _ := tempRepo(t)
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f001b"

		ref, err := LookupWorkspace(repo, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: LookupWorkspace: %v", err)
		}
		if ref.ExistsOnDisk {
			t.Errorf("WM-013: ExistsOnDisk = true before worktree creation, want false")
		}
	})

	t.Run("exists-on-disk-true-after-create", func(t *testing.T) {
		t.Parallel()

		// After CreateWorktree, ExistsOnDisk must be true.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f001c"

		if err := CreateWorktree(t.Context(), repo, runID, sha, nil); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}

		ref, err := LookupWorkspace(repo, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: LookupWorkspace: %v", err)
		}
		if !ref.ExistsOnDisk {
			t.Errorf("WM-013: ExistsOnDisk = false after CreateWorktree, want true")
		}
	})

	t.Run("run-id-echoed-in-ref", func(t *testing.T) {
		t.Parallel()

		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f001d"

		ref, err := LookupWorkspace(repoRoot, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: LookupWorkspace: %v", err)
		}
		if ref.RunID != runID {
			t.Errorf("WM-013: ref.RunID = %q, want %q", ref.RunID, runID)
		}
	})

	t.Run("derivation-is-deterministic-no-index-needed", func(t *testing.T) {
		t.Parallel()

		// WM-013: calling LookupWorkspace twice for the same run_id must produce
		// identical results — no external state consulted.
		repoRoot := "/srv/harmonik"
		runID := "0196a1b2-c3d4-7013-8a1b-2c3d4e5f001e"

		ref1, err := LookupWorkspace(repoRoot, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: first LookupWorkspace: %v", err)
		}
		ref2, err := LookupWorkspace(repoRoot, runID, nil)
		if err != nil {
			t.Fatalf("WM-013: second LookupWorkspace: %v", err)
		}

		if ref1.Path != ref2.Path || ref1.WorkspaceID != ref2.WorkspaceID || ref1.Branch != ref2.Branch {
			t.Errorf("WM-013: LookupWorkspace non-deterministic: %+v vs %+v", ref1, ref2)
		}
	})
}
