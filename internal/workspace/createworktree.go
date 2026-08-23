package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
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
	if mkErr := os.MkdirAll(parentDir, core.HarmonikDirMode); mkErr != nil {
		return "", nil, fmt.Errorf("workspace: CreateReviewerWorktree: MkdirAll %q: %w", parentDir, mkErr)
	}

	runner := cfg.commandRunner()

	cmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "add", "--detach", wtPath, headSHA)
	out, gitErr := cmd.CombinedOutput()
	if gitErr != nil {
		return "", nil, fmt.Errorf("%w: git worktree add --detach %q %q: %w\ngit output: %s",
			ErrWorktreeCreationFailed, wtPath, headSHA, gitErr, out)
	}

	var cleanedUp bool
	cleanupFn := func() {
		if cleanedUp {
			return
		}
		cleanedUp = true
		cleanupCtx := context.WithoutCancel(ctx)
		rmCmd := runner.Command(cleanupCtx, "git", "-C", repoRoot, "worktree", "remove", "--force", "--force", wtPath)
		if out, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "workspace: reviewer worktree cleanup remove %q: %v\ngit output: %s\n", wtPath, rmErr, out)
		}
		pruneCmd := runner.Command(cleanupCtx, "git", "-C", repoRoot, "worktree", "prune")
		if out, pruneErr := pruneCmd.CombinedOutput(); pruneErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "workspace: reviewer worktree cleanup prune %q: %v\ngit output: %s\n", repoRoot, pruneErr, out)
		}
	}

	return wtPath, cleanupFn, nil
}

const worktreeAddMaxRetries = 3

func isTransientWorktreeAddRace(output []byte) bool {
	s := string(output)
	return strings.Contains(s, "commondir") && strings.Contains(s, "Undefined error")
}

func resolveWorktreeHEADViaRunner(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	out, err := runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func CreateWorktree(ctx context.Context, repoRoot, runID, parentCommit string, cfg WorktreeRootConfig) error {
	if cfg.createMu != nil {
		cfg.createMu.Lock()
		defer cfg.createMu.Unlock()
	}

	worktreePath := WorktreePath(repoRoot, runID, cfg)
	branch := TaskBranchName(runID)

	runner := cfg.commandRunner()

	parentDir := filepath.Dir(worktreePath)
	if cfg.runner != nil {
		mkdirCmd := runner.Command(ctx, "mkdir", "-p", parentDir)
		if out, mkErr := mkdirCmd.CombinedOutput(); mkErr != nil {
			return fmt.Errorf("workspace: CreateWorktree: remote mkdir -p %q: %w\noutput: %s", parentDir, mkErr, out)
		}
	} else {
		if err := os.MkdirAll(parentDir, core.HarmonikDirMode); err != nil {
			return fmt.Errorf("workspace: CreateWorktree: MkdirAll %q: %w", parentDir, err)
		}
	}

	var cleanupErrs error

	var (
		out []byte
		err error
	)
	for attempt := 0; attempt <= worktreeAddMaxRetries; attempt++ {
		cmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "add", "-b", branch, worktreePath, parentCommit)
		out, err = cmd.CombinedOutput()

		emptyHEADRace := false
		if err == nil {
			if cfg.runner == nil {
				return nil
			}
			head, headErr := resolveWorktreeHEADViaRunner(ctx, runner, worktreePath)
			if headErr == nil && head != "" {
				return nil
			}
			emptyHEADRace = true
			err = fmt.Errorf("git worktree add exited 0 but HEAD did not resolve in %q (concurrent remote create race)",
				worktreePath)
			if headErr != nil {
				err = fmt.Errorf("%w: %w", err, headErr)
			}
			out = []byte("(empty HEAD after git worktree add — hk-iaj1w)")
		} else if ctx.Err() != nil {
			break
		}

		if attempt < worktreeAddMaxRetries && (emptyHEADRace || isTransientWorktreeAddRace(out)) {
			cleanupErrs = errors.Join(cleanupErrs,
				cleanupPartialWorktreeState(ctx, runner, cfg.runner != nil, repoRoot, worktreePath, branch))

			delay := time.Duration(50*(1<<attempt)) * time.Millisecond // 50ms, 100ms, 200ms
			select {
			case <-ctx.Done():
			case <-time.After(delay):
			}
			if ctx.Err() != nil {
				break
			}
			continue
		}
		break
	}

	return withCleanupErrs(
		fmt.Errorf("%w: git worktree add -b %q %q %q: %w\ngit output: %s",
			ErrWorktreeCreationFailed, branch, worktreePath, parentCommit, err, out),
		cleanupErrs)
}

// CreateDispatchWorktree makes one non-destructive attempt to create the exact
// intent-owned worktree. It never removes a path, prunes Git metadata, or
// deletes a branch after an uncertain result. Replay must observe the resulting
// authority before it tries again.
func CreateDispatchWorktree(
	ctx context.Context,
	repoRoot string,
	runID string,
	parentCommit string,
	cfg WorktreeRootConfig,
) error {
	if err := validateDispatchWorktreeCreate(repoRoot, runID, parentCommit); err != nil {
		return err
	}
	if cfg.createMu != nil {
		cfg.createMu.Lock()
		defer cfg.createMu.Unlock()
	}

	worktreePath := WorktreePath(repoRoot, runID, cfg)
	parentDir := filepath.Dir(worktreePath)
	runner := cfg.commandRunner()
	if err := createDispatchWorktreeRoot(ctx, runner, cfg.runner != nil, parentDir); err != nil {
		return err
	}

	branch := TaskBranchName(runID)
	out, err := runner.Command(
		ctx, "git", "-C", repoRoot, "worktree", "add", "-b", branch, worktreePath, parentCommit,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("workspace: CreateDispatchWorktree: git worktree add: %w\noutput: %s", err, out)
	}
	if cfg.runner != nil {
		head, headErr := resolveWorktreeHEADViaRunner(ctx, runner, worktreePath)
		if headErr != nil {
			return fmt.Errorf("workspace: CreateDispatchWorktree: read HEAD after create: %w", headErr)
		}
		if head != parentCommit {
			return fmt.Errorf("workspace: CreateDispatchWorktree: HEAD after create is %q, want %q", head, parentCommit)
		}
	}
	return nil
}

func validateDispatchWorktreeCreate(repoRoot, runID, parentCommit string) error {
	id, idErr := uuid.Parse(runID)
	if idErr != nil || id.Version() != 7 || id.String() != runID {
		return fmt.Errorf("workspace: CreateDispatchWorktree: invalid run_id %q", runID)
	}
	if repoRoot == "" || !filepath.IsAbs(repoRoot) || filepath.Clean(repoRoot) != repoRoot {
		return fmt.Errorf("workspace: CreateDispatchWorktree: repo root must be a clean absolute path")
	}
	if !isFullGitObjectID(parentCommit) || strings.ToLower(parentCommit) != parentCommit {
		return fmt.Errorf("workspace: CreateDispatchWorktree: parent commit must be a full lowercase Git object ID")
	}
	return nil
}

func createDispatchWorktreeRoot(
	ctx context.Context,
	runner tmux.CommandRunner,
	remote bool,
	parentDir string,
) error {
	if !remote {
		if err := os.MkdirAll(parentDir, core.HarmonikDirMode); err != nil {
			return fmt.Errorf("workspace: CreateDispatchWorktree: create owning root %q: %w", parentDir, err)
		}
		return nil
	}
	out, err := runner.Command(ctx, "mkdir", "-p", parentDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("workspace: CreateDispatchWorktree: create owning root %q: %w\noutput: %s", parentDir, err, out)
	}
	return nil
}

func cleanupPartialWorktreeState(ctx context.Context, runner tmux.CommandRunner, remoteFS bool, repoRoot, worktreePath, branch string) error {
	cleanupCtx := context.WithoutCancel(ctx)

	var rmErr error
	if remoteFS {
		rmErr = runner.Command(cleanupCtx, "rm", "-rf", worktreePath).Run()
	} else {
		rmErr = os.RemoveAll(worktreePath)
	}
	pruneErr := runner.Command(cleanupCtx, "git", "-C", repoRoot, "worktree", "prune").Run()
	delErr := runner.Command(cleanupCtx, "git", "-C", repoRoot, "branch", "-D", branch).Run()

	return errors.Join(rmErr, pruneErr, delErr)
}
