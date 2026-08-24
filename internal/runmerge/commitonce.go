package runmerge

import (
	"context"

	"github.com/gregberns/harmonik/internal/gitprobe"
)

// runCommitOnce runs a commit-class git command — one that must not run twice —
// and decides a stopped child from the repository state instead of guessing.
//
// Almost every git command on the merge path can simply run again, because a
// second run means what the first run meant. A commit does not. Run it twice
// after a first run that committed and the branch gets a second commit nobody
// asked for, which then rides the merge to the target.
//
// So a stopped commit is decided by the worktree. HEAD is read before the
// attempt and again after it. A HEAD that moved means the command landed before
// the signal arrived, so the failure is not real. A HEAD that did not move means
// it is safe to run the command again. A worktree that cannot answer either
// question gets neither treatment: the original failure is reported and the
// merge fails cleanly, because a wrong commit is worse than a failed one.
//
// A HEAD that moved means OUR command landed only because nothing else commits
// in this worktree while the merge runs: the run's agent session is torn down
// before the merge starts. Break that and this function reports success for a
// commit that never happened.
//
// The caller supplies a single attempt that forks the child itself. Do not give
// this function a gitprobe call: gitprobe retries, and the whole point here is
// that this command must not be retried blind.
//
// Beads: hk-jbtj6 (the strip commit, where this shape was worked out),
// hk-7neu1 (the other commit-class commands on the merge path).
func runCommitOnce(ctx context.Context, wtPath string, attempt func() ([]byte, error)) ([]byte, error) {
	before, beforeErr := gitprobe.ResolveWorktreeHEAD(ctx, wtPath)

	out, err := attempt()
	if !gitprobe.ProcessDidNotRun(ctx, err) {
		return out, err
	}
	if beforeErr != nil {
		return out, err
	}

	after, afterErr := gitprobe.ResolveWorktreeHEAD(ctx, wtPath)
	if afterErr != nil {
		return out, err
	}
	if after != before {
		return out, nil
	}
	return attempt()
}
