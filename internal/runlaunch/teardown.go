package runlaunch

// teardown.go — the hk-68pvl kill-before-worktree-removal backstop.
//
// Carved out of internal/daemon/workloop.go by P2 unit E5 RT19b (pure move).

import (
	"context"

	"github.com/gregberns/harmonik/internal/handler"
)

// ForceTeardownSession force-terminates sess and blocks until the hosted
// process has been reaped, using a non-cancellable background context.
//
// This is the load-bearing guard for hk-68pvl: the worktree-removal cleanup
// (removeWorktree, run via the deferred wtCleanup in beadRunOne) must NEVER
// delete the worktree directory while the implementer/reviewer claude is still
// live inside it. On the tmux substrate path, tmuxSubstrateSession.Wait returns
// ctx.Err() the instant the run ctx is cancelled even though the hosted process
// may still be alive (runWait keeps polling in the background); the subsequent
// `git worktree remove --force` then races a live `go test`, the agent's
// `git add`/commit lands in a deleted directory, and the run is recorded as a
// false `no_commit_during_implementer ... exit=0`.
//
// sess.Kill blocks until the process group is terminated on both paths:
//   - substrate: killProcessWithGrace (SIGTERM → grace poll → SIGKILL) then
//     KillWindow — synchronous, idempotent via killOnce.
//   - exec: SIGTERM the process group, then await reap (escalating to SIGKILL
//     on the background ctx, which never expires, so it waits for exit).
//
// Kill is safe to call more than once (idempotent on substrate; harmless ESRCH
// on exec). Callers register this as a deferred backstop immediately after
// Launch so EVERY return path (success, failure, early error, ctx-cancel) tears
// the session down before the function returns — and therefore before the
// beadRunOne-level deferred wtCleanup runs.
//
// Bead: hk-68pvl.
// Origin: internal/daemon/workloop.go forceTeardownSession.
func ForceTeardownSession(sess handler.Session) {
	if sess == nil {
		return
	}
	_ = sess.Kill(context.Background()) //nolint:errcheck // best-effort reap: Kill is idempotent and the caller is a deferred backstop with no error channel; the worktree-removal ordering guarantee comes from Kill BLOCKING, not from its error
}
