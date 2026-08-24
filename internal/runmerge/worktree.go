package runmerge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"github.com/gregberns/harmonik/internal/gitprobe"
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
	if out, err := removeWorktreeOnce(ctx, repoRoot, wtPath); err != nil {
		return fmt.Errorf("remove worktree %q: %w\n%s", wtPath, err, out)
	}

	if err := workspace.PruneWorktreeTrust(ctx, wtPath); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: runmerge: prune worktree trust for %s failed: %v\n", wtPath, err)
	}
	return nil
}

// removeWorktreeOnce runs the removal and decides a stopped child from the
// filesystem, not by running the command again blind. The signal can arrive
// AFTER git removed the worktree, and a second `git worktree remove` on a path
// that is gone fails. The caller would then report a failed reclaim for a
// worktree that is already reclaimed.
//
// So the path is looked at again. A path that is gone means the removal landed.
// Anything else means the removal can run once more — a path that is still
// there, and also a path the box cannot answer for, because a stat that fails
// for its own reasons is not evidence that the worktree went away.
//
// Bead: hk-7neu1.
func removeWorktreeOnce(ctx context.Context, repoRoot, wtPath string) ([]byte, error) {
	remove := func() ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", "--force", wtPath)
		cmd.Dir = repoRoot
		return cmd.CombinedOutput()
	}

	out, err := remove()
	if !gitprobe.ProcessDidNotRun(ctx, err) {
		return out, err
	}
	if _, statErr := os.Stat(wtPath); errors.Is(statErr, fs.ErrNotExist) {
		return nil, nil
	}
	return remove()
}
