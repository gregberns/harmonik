package daemon

// codesync_rs_b8.go — DD1 GitHub code-sync sequence for remote-placed runs
// (remote-substrate B8).
//
// When a bead is dispatched to a remote worker the commit lands in the
// worker's local repo clone, not box A's.  Three synchronisation steps wrap
// the existing local dispatch sequence so box A can merge the work:
//
//   (a) fetch-base  — ssh worker -- git -C <repoPath> fetch origin <baseSHA>
//                     ensures baseSHA is present on the worker BEFORE the
//                     worktree is created there (via CreateWorktree+SSHRunner,
//                     already wired in B7).
//
//   (b) push-branch — ssh worker -- git -C <wtPath> push origin run/<runID>
//                     called after commit-detect; uploads the committed run
//                     branch from the worker to the shared GitHub origin.
//
//   (c) box-A fetch — git -C <projectDir> fetch origin run/<runID>
//                     brings the pushed branch into box A's ref namespace so
//                     the UNCHANGED mergeRunBranchToMain can find it locally.
//
// Local runs add NO new steps (NFR7): all three functions are guarded by
// their callers and are never invoked when no worker is selected.
//
// Spec: remote-substrate B8 (hk-rs-b8-codesync-3fk0).

import (
	"context"
	"fmt"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// fetchBaseOnWorker ensures baseSHA is present in the worker's repo clone by
// running:
//
//	git -C <repoPath> fetch origin <baseSHA>
//
// through r (typically an SSHRunner that tunnels the command to the worker).
// Step (a) of the DD1 code-sync sequence; MUST run before worktree-add.
func fetchBaseOnWorker(ctx context.Context, r tmux.CommandRunner, repoPath, baseSHA string) error {
	cmd := r.Command(ctx, "git", "-C", repoPath, "fetch", "origin", baseSHA)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesync: fetchBaseOnWorker (repo=%s sha=%s): %w\ngit: %s",
			repoPath, baseSHA, err, out)
	}
	return nil
}

// pushRunBranchOnWorker pushes the completed run branch from the worker's
// worktree to the shared GitHub origin:
//
//	git -C <wtPath> push origin run/<runID>
//
// through r (typically an SSHRunner).  Step (b) of the DD1 code-sync
// sequence; MUST run after commit-detect and before box-A fetch.
func pushRunBranchOnWorker(ctx context.Context, r tmux.CommandRunner, wtPath, runID string) error {
	branch := workspace.TaskBranchName(runID)
	cmd := r.Command(ctx, "git", "-C", wtPath, "push", "origin", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesync: pushRunBranchOnWorker (wt=%s branch=%s): %w\ngit: %s",
			wtPath, branch, err, out)
	}
	return nil
}

// fetchRunBranchBoxA fetches the run branch from the shared GitHub origin into
// box A's local ref namespace:
//
//	git -C <projectDir> fetch origin run/<runID>
//
// r is the CommandRunner used for the local git command; when nil,
// tmux.LocalRunner{} is used (production default).  Step (c) of the DD1
// code-sync sequence; MUST run before mergeRunBranchToMain so the merge can
// resolve the branch locally.
func fetchRunBranchBoxA(ctx context.Context, r tmux.CommandRunner, projectDir, runID string) error {
	if r == nil {
		r = tmux.LocalRunner{}
	}
	branch := workspace.TaskBranchName(runID)
	refspec := branch + ":refs/heads/" + branch
	cmd := r.Command(ctx, "git", "-C", projectDir, "fetch", "origin", refspec)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesync: fetchRunBranchBoxA (project=%s refspec=%s): %w\ngit: %s",
			projectDir, refspec, err, out)
	}
	return nil
}
