package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReviewerWorktreePath returns the canonical path for a reviewer's isolated
// worktree: <repo>/.harmonik/worktrees/<run-id>-reviewer-<iter>/.
//
// Reviewer worktrees are short-lived: created just before the reviewer launches
// and removed once the daemon has read the verdict.  They are NOT leased (no
// lease-lock file) and are NOT tracked by the workspace state machine — they
// are scratch paths analogous to the scratch merge-worktrees of WM-019a.
func ReviewerWorktreePath(repoRoot, runID string, iterationCount int, cfg WorktreeRootConfig) string {
	name := fmt.Sprintf("%s-reviewer-%d", runID, iterationCount)
	return filepath.Join(WorktreeRootPath(repoRoot, cfg), name)
}

// CreateReviewerWorktree creates a short-lived, isolated git worktree for the
// reviewer agent at [ReviewerWorktreePath].
//
// The worktree is checked out in detached-HEAD mode at headSHA so the reviewer
// sees the full committed state of the task branch without holding a named
// branch reference.  Detached HEAD also means a `git checkout <sha>` inside
// the reviewer's session only affects the reviewer's own worktree — it cannot
// corrupt the implementer's tracked task branch.
//
// The returned cleanup function calls `git worktree remove --force --force`
// followed by `git worktree prune` and must be deferred by the caller.
// Cleanup is safe to call more than once (idempotent).
//
// Bead: hk-dut6b — reviewer isolation requirement.
func CreateReviewerWorktree(ctx context.Context, repoRoot, runID string, iterationCount int, headSHA string, cfg WorktreeRootConfig) (path string, cleanup func(), err error) {
	wtPath := ReviewerWorktreePath(repoRoot, runID, iterationCount, cfg)

	parentDir := filepath.Dir(wtPath)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
		return "", nil, fmt.Errorf("workspace: CreateReviewerWorktree: MkdirAll %q: %w", parentDir, mkErr)
	}

	runner := cfg.commandRunner()

	// `git -C <repoRoot> worktree add --detach <path> <sha>` — no branch created;
	// detached HEAD at headSHA.  Multiple worktrees may reference the same SHA
	// (unlike branches, which git prevents from being checked out in two worktrees
	// simultaneously).
	cmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "add", "--detach", wtPath, headSHA)
	out, gitErr := cmd.CombinedOutput()
	if gitErr != nil {
		return "", nil, fmt.Errorf("%w: git worktree add --detach %q %q: %v\ngit output: %s",
			ErrWorktreeCreationFailed, wtPath, headSHA, gitErr, out)
	}

	var cleanedUp bool
	cleanupFn := func() {
		if cleanedUp {
			return
		}
		cleanedUp = true
		rmCmd := runner.Command(context.Background(), "git", "-C", repoRoot, "worktree", "remove", "--force", "--force", wtPath)
		_ = rmCmd.Run()
		pruneCmd := runner.Command(context.Background(), "git", "-C", repoRoot, "worktree", "prune")
		_ = pruneCmd.Run()
	}

	return wtPath, cleanupFn, nil
}

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
//
// worktreeAddMaxRetries is the maximum number of retries for a transient
// git worktree add failure caused by the macOS/APFS commondir race.
const worktreeAddMaxRetries = 3

// isTransientWorktreeAddRace reports whether git output indicates the known
// macOS/APFS race that occurs when multiple concurrent "git worktree add"
// calls race to read each other's in-progress .git/worktrees/<id>/commondir
// metadata. The signature is: "failed to read .git/worktrees/<id>/commondir:
// Undefined error: 0" (errno=0 on APFS under concurrent creates).
func isTransientWorktreeAddRace(output []byte) bool {
	s := string(output)
	return strings.Contains(s, "commondir") && strings.Contains(s, "Undefined error")
}

func CreateWorktree(ctx context.Context, repoRoot, runID, parentCommit string, cfg WorktreeRootConfig) error {
	worktreePath := WorktreePath(repoRoot, runID, cfg)
	branch := TaskBranchName(runID)

	runner := cfg.commandRunner()

	// Create the parent directory (worktree root) if absent.
	// For remote runs (cfg.runner != nil), the git commands execute on the
	// worker via SSHRunner, so we must create the directory on the worker too.
	// Using os.MkdirAll here would create the path locally while git runs
	// remotely — the TOCTOU race reported as hk-eodo. Route mkdir through the
	// same runner so local and remote paths are both handled correctly.
	parentDir := filepath.Dir(worktreePath)
	if cfg.runner != nil {
		mkdirCmd := runner.Command(ctx, "mkdir", "-p", parentDir)
		if out, mkErr := mkdirCmd.CombinedOutput(); mkErr != nil {
			return fmt.Errorf("workspace: CreateWorktree: remote mkdir -p %q: %v\noutput: %s", parentDir, mkErr, out)
		}
	} else {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return fmt.Errorf("workspace: CreateWorktree: MkdirAll %q: %w", parentDir, err)
		}
	}

	// Issue `git -C <repoRoot> worktree add -b <branch> <path> <parentCommit>`
	// with bounded retry for the transient macOS/APFS commondir race (hk-gq3my).
	var (
		out []byte
		err error
	)
	for attempt := 0; attempt <= worktreeAddMaxRetries; attempt++ {
		cmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "add", "-b", branch, worktreePath, parentCommit)
		out, err = cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			// Context cancelled; do not retry.
			break
		}
		if attempt < worktreeAddMaxRetries && isTransientWorktreeAddRace(out) {
			// Clean up any partial state so the next attempt starts fresh.
			_ = os.RemoveAll(worktreePath)
			// Remove the branch if git created it before failing.
			delBranch := runner.Command(ctx, "git", "-C", repoRoot, "branch", "-D", branch)
			_ = delBranch.Run()
			// Prune stale worktree metadata entries.
			pruneCmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "prune")
			_ = pruneCmd.Run()

			delay := time.Duration(50*(1<<attempt)) * time.Millisecond // 50ms, 100ms, 200ms
			select {
			case <-ctx.Done():
				break
			case <-time.After(delay):
			}
			continue
		}
		break
	}

	return fmt.Errorf("%w: git worktree add -b %q %q %q: %v\ngit output: %s",
		ErrWorktreeCreationFailed, branch, worktreePath, parentCommit, err, out)
}
