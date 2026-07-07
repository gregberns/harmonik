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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// errBaseSHAAbsent is returned by fetchBaseOnWorker when git fetch origin exits
// 0 but the base SHA is not present in the worker's ODB afterward. The caller
// (ensureBaseOnWorker) uses this to trigger the push-from-box-A fallback (hk-2hfyt).
var errBaseSHAAbsent = errors.New("base SHA absent on worker after fetch")

// fetchBaseOnWorker ensures baseSHA is present in the worker's repo clone by
// running:
//
//	git -C <repoPath> fetch origin <baseSHA>
//
// through r (typically an SSHRunner that tunnels the command to the worker).
// Step (a) of the DD1 code-sync sequence; MUST run before worktree-add.
//
// After the fetch, a git cat-file -t check verifies the object actually landed.
// git fetch origin <sha> can exit 0 without delivering the commit when the sha
// is absent from origin (e.g. the base commit is unpushed from box A; hk-2hfyt).
// Returns errBaseSHAAbsent when the fetch exits 0 but the object is absent, so
// the caller (ensureBaseOnWorker) can trigger the push-from-box-A fallback.
func fetchBaseOnWorker(ctx context.Context, r tmux.CommandRunner, repoPath, baseSHA string) error {
	cmd := r.Command(ctx, "git", "-C", repoPath, "fetch", "origin", baseSHA)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesync: fetchBaseOnWorker (repo=%s sha=%s): %w\ngit: %s",
			repoPath, baseSHA, err, out)
	}
	// Verify the SHA actually landed — git fetch origin <sha> exits 0 even when
	// the remote does not carry the SHA (unpushed commit; hk-2hfyt). cat-file -t
	// exits non-zero when the object is absent.
	catOut, catErr := r.Command(ctx, "git", "-C", repoPath, "cat-file", "-t", baseSHA).CombinedOutput()
	if catErr != nil {
		return fmt.Errorf("%w: codesync: fetchBaseOnWorker (repo=%s sha=%s): SHA absent after fetch (base commit unpushed from box A?)\ngit cat-file: %s",
			errBaseSHAAbsent, repoPath, baseSHA, catOut)
	}
	_ = catOut
	return nil
}

// pushBaseToWorker transfers baseSHA from box A's local repo to the worker's
// clone via a git push over SSH, bypassing origin. Used as the fallback when
// git fetch origin <sha> exits 0 but delivers nothing (the SHA is unpushed from
// box A to origin; hk-2hfyt).
//
// The commit is pushed to refs/harmonik/base on the worker — a stable scratch
// ref that git-receive-pack accepts on a non-bare repo (it's not the checked-out
// branch). A nil localRunner uses tmux.LocalRunner{}.
func pushBaseToWorker(ctx context.Context, localRunner tmux.CommandRunner, boxAProjectDir, workerHost, workerRepoPath, baseSHA string, sshOpts []string) error {
	if localRunner == nil {
		localRunner = tmux.LocalRunner{}
	}
	url := workerSSHURL(workerHost, workerRepoPath)
	refspec := baseSHA + ":refs/harmonik/base"

	args := []string{"-C", boxAProjectDir}
	if len(sshOpts) > 0 {
		args = append(args, "-c", "core.sshCommand=ssh "+strings.Join(sshOpts, " "))
	}
	args = append(args, "push", url, refspec)

	cmd := localRunner.Command(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesync: pushBaseToWorker (boxA=%s worker=%s sha=%s): %w\ngit: %s",
			boxAProjectDir, url, baseSHA, err, out)
	}
	return nil
}

// ensureBaseOnWorker ensures baseSHA is present on the worker before worktree
// creation (DD1 code-sync step a; hk-2hfyt). It first tries git fetch origin
// <sha> on the worker (fast path when the commit is pushed to origin). When the
// SHA is absent after fetch (errBaseSHAAbsent; base commit is unpushed from box
// A), it falls back to pushing the commit directly from box A to the worker over
// SSH, bypassing origin.
//
// r runs commands on the worker (typically SSHRunner). localRunner runs commands
// on box A (for the push fallback); nil uses tmux.LocalRunner{} (production
// default). workerHost and sshOpts are used to construct the ssh:// push URL and
// mirror the transport used elsewhere in the remote path.
func ensureBaseOnWorker(ctx context.Context, r tmux.CommandRunner, workerRepoPath, baseSHA string,
	localRunner tmux.CommandRunner, boxAProjectDir, workerHost string, sshOpts []string) error {
	fetchErr := fetchBaseOnWorker(ctx, r, workerRepoPath, baseSHA)
	if fetchErr == nil {
		return nil
	}
	if !errors.Is(fetchErr, errBaseSHAAbsent) {
		return fetchErr
	}
	// SHA absent after fetch — base commit is unpushed from box A to origin.
	// Push directly from box A to the worker, bypassing origin (hk-2hfyt).
	fmt.Fprintf(os.Stderr, "codesync: ensureBaseOnWorker: SHA %s absent on worker after fetch origin; pushing directly from box A\n", baseSHA)
	if pushErr := pushBaseToWorker(ctx, localRunner, boxAProjectDir, workerHost, workerRepoPath, baseSHA, sshOpts); pushErr != nil {
		return fmt.Errorf("codesync: ensureBaseOnWorker: fetch origin absent (%v); direct push also failed: %w",
			fetchErr, pushErr)
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

// isRefNotFoundError reports whether git's combined output indicates a transient
// "couldn't find remote ref" condition. This specific message is produced by
// git-fetch when git-upload-pack successfully serves the remote but the
// requested ref is absent from the advertised list — distinct from SSH
// connection errors (which appear before this string). The condition is
// transient for remote-substrate runs: the agent has committed and exited (so
// the ref exists on the worker's disk) but git-upload-pack may not yet serve
// the newly-written ref due to filesystem flush timing across a real network
// link (hk-zsn7 push/visibility gap). A short retry bridges the gap.
func isRefNotFoundError(out []byte) bool {
	return bytes.Contains(out, []byte("couldn't find remote ref"))
}

// fetchRunBranchRetryCount is the number of additional attempts after an
// initial "couldn't find remote ref" failure in fetchRunBranchBoxA. Three
// retries (four total attempts) with delays 2 s / 4 s / 8 s cover the
// observed visibility window (≤ ~14 s) while bounding the worst case under
// 30 s (hk-zsn7).
const fetchRunBranchRetryCount = 3

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
//
// Transient "couldn't find remote ref" errors are retried with exponential
// backoff (2 s / 4 s / 8 s, up to fetchRunBranchRetryCount additional
// attempts). This bridges the push/visibility gap where the agent has committed
// and exited on the worker but git-upload-pack has not yet flushed the new ref
// to its advertisement (hk-zsn7). Hard errors (connection failure, wrong path)
// are not retried.
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

	var (
		out []byte
		err error
	)
	for attempt := 0; attempt <= fetchRunBranchRetryCount; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2 s, 4 s, 8 s. The agent has committed and
			// exited; the branch exists on the worker but git-upload-pack may not
			// yet advertise the newly-created ref (hk-zsn7 push/visibility gap).
			delay := time.Duration(2<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return fmt.Errorf("codesync: fetchRunBranchBoxA (project=%s url=%s refspec=%s): context cancelled on retry %d: %w",
					projectDir, url, refspec, attempt, ctx.Err())
			case <-time.After(delay):
			}
		}
		cmd := r.Command(ctx, "git", args...)
		out, err = cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if !isRefNotFoundError(out) {
			// Hard error (connection failure, wrong path, etc.): do not retry.
			break
		}
	}
	return fmt.Errorf("codesync: fetchRunBranchBoxA (project=%s url=%s refspec=%s): %w\ngit: %s",
		projectDir, url, refspec, err, out)
}
