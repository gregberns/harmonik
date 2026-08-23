package runmerge

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
// It returns a removal failure to its caller. The run can continue after that
// failure, but it must report a failed reclaim rather than retained evidence.
//
// hk-68pvl: the caller (beadRunOne via the deferred wtCleanup) MUST ensure the
// run's implementer/reviewer session has been force-torn-down
// (forceTeardownSession) before this runs, so the directory is never deleted
// out from under a live agent mid-`go test`.
func RemoveWorktree(ctx context.Context, repoRoot, wtPath string) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", "--force", wtPath)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove worktree %q: %w\n%s", wtPath, err, out)
	}

	if err := workspace.PruneWorktreeTrust(wtPath); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: runmerge: prune worktree trust for %s failed: %v\n", wtPath, err)
	}
	return nil
}
