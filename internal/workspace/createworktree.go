package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// resolveWorktreeHEADViaRunner resolves `git -C <wtPath> rev-parse HEAD` through
// the same runner used for worktree creation and returns the trimmed SHA. It
// mirrors the daemon-side resolveWorktreeHEADVia check so the post-add HEAD
// validation in CreateWorktree (hk-iaj1w) shares identical semantics: an error
// or an empty SHA both indicate an un-checked-out / uninitialised worktree.
func resolveWorktreeHEADViaRunner(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	out, err := runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func CreateWorktree(ctx context.Context, repoRoot, runID, parentCommit string, cfg WorktreeRootConfig) error {
	// hk-5qp7z: serialize the worktree-add + HEAD-resolve retry loop under the
	// caller-supplied create mutex when present. This prevents N concurrent
	// "git worktree add" calls against the same shared worker repo from racing
	// on HEAD/index resolution — a race the per-attempt retry cannot recover from
	// because each retry also sees concurrent sibling adds. Holding the mutex for
	// the full retry loop (not just one attempt) ensures cleanup + backoff + retry
	// are also serialised, preventing a sibling's prune/add from interleaving with
	// this goroutine's cleanup state.
	if cfg.createMu != nil {
		cfg.createMu.Lock()
		defer cfg.createMu.Unlock()
	}

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

	// cleanupPartialState removes any partial worktree dir, stale worktree
	// metadata, and leftover branch so the next retry attempt starts from a clean
	// slate. For remote runs (cfg.runner != nil) the worktree dir lives on the
	// worker, so os.RemoveAll (which runs LOCALLY on box A) is a no-op and leaves
	// the stale dir behind — the next dispatch then collides with "branch/reference
	// already exists", which is NOT matched as a transient race, so no retry fires
	// and the dispatch fails silently. Route the directory removal through the same
	// runner as the sibling worktree prune / branch -D calls so remote partial
	// state is actually cleaned. Best-effort: ignore errors, matching the siblings.
	// (hk-3vbc; reused by the empty-HEAD retry path in hk-iaj1w.)
	//
	// ORDER MATTERS (hk-iaj1w): rm dir → worktree prune → branch -D. In the
	// empty-HEAD case `git worktree add -b` exited 0, so the branch ref AND the
	// worktree registration both exist (only HEAD is unusable). `git branch -D`
	// REFUSES a branch still referenced by a registered worktree — and removing
	// the dir alone does NOT deregister it; only `git worktree prune` does. So the
	// prune MUST run before branch -D, or branch -D fails (swallowed), the branch
	// survives, and the retry's `git worktree add -b <branch>` dies with "already
	// exists" — un-retried, defeating the whole retry. (For the hk-3vbc commondir
	// path the failing add exits before creating a branch, so this order is a
	// harmless no-op there.)
	cleanupPartialState := func() {
		if cfg.runner != nil {
			rmCmd := runner.Command(ctx, "rm", "-rf", worktreePath)
			_ = rmCmd.Run()
		} else {
			_ = os.RemoveAll(worktreePath)
		}
		// Deregister the now-removed worktree FIRST so the branch is no longer
		// "used by worktree" and branch -D can succeed.
		pruneCmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "prune")
		_ = pruneCmd.Run()
		// Remove the branch git created for the failed / empty-HEAD attempt.
		delBranch := runner.Command(ctx, "git", "-C", repoRoot, "branch", "-D", branch)
		_ = delBranch.Run()
	}

	// Issue `git -C <repoRoot> worktree add -b <branch> <path> <parentCommit>`
	// with bounded retry for the transient macOS/APFS commondir race (hk-gq3my)
	// and for the empty-HEAD remote create race (hk-iaj1w).
	var (
		out []byte
		err error
	)
	for attempt := 0; attempt <= worktreeAddMaxRetries; attempt++ {
		cmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "add", "-b", branch, worktreePath, parentCommit)
		out, err = cmd.CombinedOutput()

		// emptyHEADRace flags the hk-iaj1w condition: `git worktree add` exits 0 but
		// leaves a worktree whose HEAD never resolves. Treated like the commondir
		// race below — same cleanup, same bounded retry.
		emptyHEADRace := false
		if err == nil {
			// LOCAL runs (nil runner) keep byte-identical behaviour: a successful
			// add is sufficient, return immediately without the extra rev-parse.
			if cfg.runner == nil {
				return nil
			}
			// REMOTE path (hk-iaj1w): concurrent `git worktree add` on a worker can
			// race and leave the worktree dir created but un-checked-out, so a later
			// `git -C <wtPath> rev-parse HEAD` returns empty and the run fast-fails
			// ~2s after run_started ("git rev-parse HEAD returned empty"). Validate
			// HEAD here, at create time, where the failure is still retryable —
			// reusing the same rev-parse-HEAD check as the daemon. A non-empty HEAD
			// means the worktree is genuinely ready; anything else is the race.
			head, headErr := resolveWorktreeHEADViaRunner(ctx, runner, worktreePath)
			if headErr == nil && head != "" {
				return nil
			}
			emptyHEADRace = true
			// Synthesise an error/output pair so an exhausted-retry exit returns a
			// clear, attributable error instead of a downstream silent symptom.
			err = fmt.Errorf("git worktree add exited 0 but HEAD did not resolve in %q (concurrent remote create race): %v",
				worktreePath, headErr)
			out = []byte("(empty HEAD after git worktree add — hk-iaj1w)")
		} else if ctx.Err() != nil {
			// Context cancelled; do not retry.
			break
		}

		if attempt < worktreeAddMaxRetries && (emptyHEADRace || isTransientWorktreeAddRace(out)) {
			cleanupPartialState()

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
