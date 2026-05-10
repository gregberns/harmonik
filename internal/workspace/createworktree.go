package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CreateWorktree materialises a git worktree at the canonical path for runID,
// creating the task branch atomically in a single `git worktree add -b` call,
// per workspace-model.md §4.1 WM-003.
//
// # Mandatory form (WM-003)
//
// The command issued is:
//
//	git worktree add -b <branch> <path> <parentCommit>
//
// where:
//   - <branch> is the task branch (e.g., "run/<run_id>") produced by
//     [TaskBranchName].
//   - <path> is the canonical worktree path produced by [WorktreePath].
//   - <parentCommit> is the explicit start-point SHA; MUST be provided by the
//     caller (omitting it would default to HEAD and race with operator activity
//     in the main worktree per WM-003).
//
// The -b form is REQUIRED because the task branch does not yet exist at
// worktree-create time. Using `git worktree add <path> <branch>` (no -b)
// requires the branch to pre-exist and will fail with
// "fatal: invalid reference: <branch>".
//
// # Parent directory
//
// CreateWorktree creates the parent directory (the worktree root) if it does
// not exist, using [os.MkdirAll] at 0755 per the .harmonik dir convention.
// It does NOT create the worktree directory itself — git does that.
//
// # No provisioning at MVH
//
// No provisioning layer (adze, devbox, container build) participates in MVH
// worktree creation. The worktree is a plain subfolder per WM-003.
//
// # Error handling
//
// Returns [ErrWorktreeCreationFailed] (wrapped) when git exits non-zero. The
// combined stdout+stderr from git is embedded in the error for diagnostics.
//
// # Context
//
// ctx is passed to exec.CommandContext; the caller may impose a deadline.
// On context cancellation, git is killed and an error is returned.
//
// Spec refs:
//   - workspace-model.md §4.1 WM-003 — git worktree add -b mandate.
//   - workspace-model.md §4.1 WM-002 — canonical path convention.
//   - workspace-model.md §4.2 WM-005 — task branch naming.
//   - workspace-model.md §4.a WM-ENV-002 — git minimum version.
func CreateWorktree(ctx context.Context, repoRoot, runID, parentCommit string, cfg WorktreeRootConfig) error {
	worktreePath := WorktreePath(repoRoot, runID, cfg)
	branch := TaskBranchName(runID)

	// Create the parent directory (worktree root) if absent.
	parentDir := filepath.Dir(worktreePath)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("workspace: CreateWorktree: MkdirAll %q: %w", parentDir, err)
	}

	// Issue `git worktree add -b <branch> <path> <parentCommit>`.
	// exec.Command is forbidden (noctx); use exec.CommandContext.
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, worktreePath, parentCommit)
	cmd.Dir = repoRoot

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: git worktree add -b %q %q %q: %v\ngit output: %s",
			ErrWorktreeCreationFailed, branch, worktreePath, parentCommit, err, out)
	}

	return nil
}
