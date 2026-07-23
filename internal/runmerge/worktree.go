package runmerge

// worktree.go — per-run git worktree removal.
//
// Moved out of internal/daemon/workloop.go by P2 unit E5 RT13 (pure move).

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/gregberns/harmonik/internal/workspace"
)

// RemoveWorktree removes the git worktree at wtPath and prunes stale metadata
// from the repository at repoRoot. It uses `git worktree remove --force` twice
// to handle locked worktrees (the second --force overrides the lock).
//
// Errors are non-fatal: the work loop continues even if cleanup fails (orphan
// sweep at next startup will recover stale worktrees per PL-006).
//
// hk-68pvl: the caller (beadRunOne via the deferred wtCleanup) MUST ensure the
// run's implementer/reviewer session has been force-torn-down
// (forceTeardownSession) before this runs, so the directory is never deleted
// out from under a live agent mid-`go test`.
func RemoveWorktree(ctx context.Context, repoRoot, wtPath string) {
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", "--force", wtPath)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		// Non-fatal by contract, but not silent: a removal that keeps failing is
		// how worktrees accumulate until the next startup sweep, and without this
		// line the only evidence is the leftover directory itself.
		fmt.Fprintf(os.Stderr, "daemon: runmerge: git worktree remove %s failed: %v\n%s", wtPath, err, out)
	}

	// hk-bfvby: GC the per-worktree trust key from ~/.claude.json. harmonik
	// creates one ephemeral worktree per bead and never reuses the path, so
	// without this the trust "projects" map grows unbounded (observed 36.6k
	// leaked keys / 8.6MB bloat that, with the per-call rewrite, produced the
	// ~16-min spawn stall). Best-effort: cleanup failure is non-fatal — the
	// bounded lock inside PruneWorktreeTrust ensures it can never wedge the loop.
	if err := workspace.PruneWorktreeTrust(wtPath); err != nil {
		// Also non-fatal, and also worth seeing: the leak this prunes is what
		// produced the ~16-min spawn stall above, so a persistent failure here is
		// the early warning for its return.
		fmt.Fprintf(os.Stderr, "daemon: runmerge: prune worktree trust for %s failed: %v\n", wtPath, err)
	}
}
