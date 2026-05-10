package workspace

import (
	"fmt"
	"os"
)

// WorkspaceRef is the minimal workspace reference reconstructed from a run_id
// via deterministic construction, per workspace-model.md §4.3 WM-013.
//
// No separate run-to-workspace index is required: all fields are derived from
// repoRoot and runID alone (path from WM-002, workspaceID from WM-004, branch
// from WM-005), plus a filesystem existence check (WM-013c).
//
// This type is distinct from [Workspace] — it carries only the fields
// resolvable from the filesystem without consulting Beads or a JSONL store.
// The full [Workspace] record is constructed by the workspace manager after
// verifying the lease-lock via [DiscoverWorktrees] (WM-013c).
type WorkspaceRef struct {
	// RunID is the run's stable identifier (input to the lookup).
	RunID string

	// WorkspaceID is the deterministically derived workspace identifier per
	// WM-004: "ws-" + run_id.
	WorkspaceID string

	// Path is the absolute filesystem path to the worktree directory per WM-002.
	Path string

	// Branch is the task branch name per WM-005: "run/" + run_id.
	Branch string

	// ExistsOnDisk is true iff the worktree directory exists on disk at Path.
	// A false value indicates the workspace has not been created yet (pre-create)
	// or has been cleaned up (post-discard per WM-031).
	ExistsOnDisk bool
}

// LookupWorkspace resolves a workspace reference from a run_id by deterministic
// construction, per workspace-model.md §4.3 WM-013:
//
// "Given a run_id, the workspace manager MUST be able to resolve the workspace
// record (path, branch, state) by deterministic construction per WM-002 and
// WM-004 plus a filesystem check per WM-013c. No separate run-to-workspace
// index MAY be required as the authoritative lookup path."
//
// The returned [WorkspaceRef] carries the derived path (WM-002), workspace_id
// (WM-004), and task branch (WM-005). The ExistsOnDisk field reports whether
// the worktree directory is present on disk (filesystem stat; does NOT confirm
// git registration — use [DiscoverWorktrees] for the full WM-013c discovery).
//
// cfg carries the operator-configurable worktree root per
// [control-points.md §4.7 CP-037]; use [NoWorktreeRootOverride] for the
// default per WM-002.
//
// LookupWorkspace does NOT validate runID — validation is the caller's
// responsibility at run-create time (WM-002: [A-Za-z0-9-]+).
//
// Returns an error only when the stat itself fails for a reason other than
// non-existence (e.g., permission denied). A missing path is not an error —
// it is expressed as ExistsOnDisk == false.
//
// Spec refs:
//   - workspace-model.md §4.3 WM-013 — deterministic workspace resolution rule.
//   - workspace-model.md §4.1 WM-002 — canonical path convention.
//   - workspace-model.md §4.1 WM-004 — workspace_id derivation.
//   - workspace-model.md §4.2 WM-005 — task branch naming.
//   - control-points.md §4.7 CP-037 — worktree-root operator override.
func LookupWorkspace(repoRoot, runID string, cfg WorktreeRootConfig) (WorkspaceRef, error) {
	path := WorktreePath(repoRoot, runID, cfg)
	wsID := WorkspaceIDFromRunID(runID)
	branch := TaskBranchName(runID)

	existsOnDisk := false
	_, err := os.Stat(path)
	switch {
	case err == nil:
		existsOnDisk = true
	case os.IsNotExist(err):
		// Not on disk — ExistsOnDisk stays false; not an error.
	default:
		return WorkspaceRef{}, fmt.Errorf("workspace: LookupWorkspace: Stat %q: %w", path, err)
	}

	return WorkspaceRef{
		RunID:        runID,
		WorkspaceID:  wsID,
		Path:         path,
		Branch:       branch,
		ExistsOnDisk: existsOnDisk,
	}, nil
}
