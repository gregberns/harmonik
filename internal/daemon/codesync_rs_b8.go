package daemon

// codesync_rs_b8.go — DD1 code-sync sequence for remote-placed runs
// (remote-substrate B8; box-A direct-fetch rework hk-7bwx).
//
// When a bead is dispatched to a remote worker the commit lands in the
// worker's local repo clone, not box A's.  Box A must pull that commit over to
// its own repo before the UNCHANGED mergeRunBranchToMain can merge it.
//
// ORIGINAL design routed the branch through the shared GitHub origin:
//   (b) ssh worker -- git -C <wtPath> push origin run/<runID>   (worker→GitHub)
//   (c) git -C <projectDir> fetch origin run/<runID>            (GitHub→box A)
// This FAILS in production: the worker's GitHub push credential is invalid, so
// the branch never reaches GitHub and box A's `fetch origin run/<id>` dies with
// `couldn't find remote ref run/<id>` (hk-7bwx).
//
// CURRENT design fetches the branch DIRECTLY from the worker repo over the same
// SSH transport box A already uses to drive the worker (Tailscale-reachable):
//
//   (a) fetch-base  — ssh worker -- git -C <repoPath> fetch origin <baseSHA>
//                     ensures baseSHA is present on the worker BEFORE the
//                     worktree is created there (via CreateWorktree+SSHRunner,
//                     already wired in B7). UNCHANGED.
//
//   (c) box-A fetch — git -C <projectDir> fetch ssh://<host><repoPath>
//                       run/<runID>:refs/heads/run/<runID>
//                     pulls the run branch straight from the worker's local repo
//                     into box A's ref namespace (the branch exists there because
//                     the worktree was created with `worktree add -b run/<id>`).
//                     No GitHub round-trip; box A needs only its OWN credentials,
//                     and only at the final `push origin main` after the merge.
//
// The old (b) worker→GitHub push is GONE: nothing else depends on the run branch
// being on GitHub. The final merge pushes box A's MAIN to GitHub using box A's
// valid credentials (mergeRunBranchToMain), which is unaffected.
//
// Local runs add NO new steps (NFR7): fetchBaseOnWorker / fetchRunBranchBoxA are
// guarded by their callers and never invoked when no worker is selected.
//
// Spec: remote-substrate B8 (hk-rs-b8-codesync-3fk0); rework hk-7bwx.

import (
	"context"
	"fmt"
	"strings"

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

// workerSSHURL builds the git transport URL box A uses to fetch directly from a
// worker's local repo over SSH:
//
//	ssh://<host>/<abs/repo/path>
//
// git's ssh:// syntax is `ssh://[user@]host[:port]/path`; the path component is
// taken verbatim after the first slash, so an absolute worker repo path
// (/Users/gb/harmonik-worker/repo) yields ssh://host/Users/gb/harmonik-worker/repo.
// We normalise to exactly one slash between host and an absolute path.
func workerSSHURL(host, repoPath string) string {
	return "ssh://" + host + "/" + strings.TrimPrefix(repoPath, "/")
}

// fetchRunBranchBoxA fetches the run branch DIRECTLY from the worker's local repo
// into box A's local ref namespace over SSH:
//
//	git -C <projectDir> fetch ssh://<workerHost><workerRepoPath> \
//	    run/<runID>:refs/heads/run/<runID>
//
// The run branch exists in the worker's repo because the worktree was created
// there with `git worktree add -b run/<runID>` (B7). This replaces the old
// worker→GitHub→box-A round-trip, which failed when the worker lacked a valid
// GitHub push credential (hk-7bwx).
//
// r is the CommandRunner used for the local git command (it runs ON box A, NOT
// on the worker — git itself dials the worker via the ssh:// URL); when nil,
// tmux.LocalRunner{} is used (production default). If sshOpts are present (extra
// `ssh` flags, e.g. ["-p","2222"]), they are threaded into git's transport via
// `-c core.sshCommand` so the direct fetch dials the worker exactly like the
// rest of the remote path. In production SSHRunner carries no Opts, so the
// common path is a bare `fetch ssh://host/path`.
//
// Step (c) of the DD1 code-sync sequence; MUST run before mergeRunBranchToMain
// so the merge can resolve refs/heads/run/<runID> locally.
func fetchRunBranchBoxA(ctx context.Context, r tmux.CommandRunner, projectDir, runID, workerHost, workerRepoPath string, sshOpts []string) error {
	if r == nil {
		r = tmux.LocalRunner{}
	}
	branch := workspace.TaskBranchName(runID)
	refspec := branch + ":refs/heads/" + branch
	url := workerSSHURL(workerHost, workerRepoPath)

	args := []string{"-C", projectDir}
	if len(sshOpts) > 0 {
		// Mirror SSHRunner's `ssh <opts> host` dialing for git's own ssh transport.
		args = append(args, "-c", "core.sshCommand=ssh "+strings.Join(sshOpts, " "))
	}
	args = append(args, "fetch", url, refspec)

	cmd := r.Command(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesync: fetchRunBranchBoxA (project=%s url=%s refspec=%s): %w\ngit: %s",
			projectDir, url, refspec, err, out)
	}
	return nil
}
