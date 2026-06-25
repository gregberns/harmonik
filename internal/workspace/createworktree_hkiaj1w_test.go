package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestHKIAJ1W_RemoteEmptyHEADRaceRetries verifies the hk-iaj1w fix: when a REMOTE
// run's `git worktree add` exits 0 but leaves a worktree whose HEAD does not
// resolve ("git rev-parse HEAD returned empty"), CreateWorktree treats it as a
// transient race — it cleans the partial state THROUGH THE RUNNER and retries,
// rather than letting the empty-HEAD worktree slip past create and fast-fail
// downstream ~2s after run_started.
//
// Symptom (hk-iaj1w): 3 concurrent remote dispatches each created their worktree
// dir but git left HEAD uninitialised; the empty HEAD was only caught later at
// the daemon's resolveWorktreeHEADVia, after run_started, so the queue auto-paused
// on group_failure (fail_count=3) with no retry.
//
// Assertions (first add empty-HEAD, retry valid):
//   - HEAD was validated via the runner (`git -C <wtPath> rev-parse HEAD`).
//   - Cleanup ran via the runner (`rm -rf <wtPath>`, not os.RemoveAll).
//   - `git worktree add` was retried (≥2 calls).
//   - CreateWorktree returns nil and the worktree exists on disk after retry.
func TestHKIAJ1W_RemoteEmptyHEADRaceRetries(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "019ec83c-iaj1-7001-0001-000000000001"
	branch := TaskBranchName(runID)
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

	// FAITHFUL fake: the first `git worktree add` runs the REAL git, so a real
	// task branch AND a registered worktree are actually created on disk — exactly
	// the on-worker state after `add -b` exits 0 in the race. We do NOT truncate
	// the on-disk HEAD (that would break git's branch↔worktree association and let
	// `branch -D` wrongly succeed, masking the cleanup-ordering defect); instead we
	// make only the HEAD-VALIDATION rev-parse report empty. The retry therefore has
	// to tear down a LIVE branch + registered worktree, which is what surfaces the
	// cleanup-ordering bug: `git branch -D` is refused while the worktree is still
	// registered, so it MUST be pruned first (hk-iaj1w cleanup reorder).
	var worktreeAddCalls int
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// `git worktree add ...` — run the real git on every attempt so a real
			// branch + registered worktree exists for the cleanup path to remove.
			if name == "git" && containsArg(args, "worktree") && containsArg(args, "add") {
				worktreeAddCalls++
				return exec.CommandContext(ctx, name, args...)
			}
			// HEAD validation after the first (real) add: report an EMPTY HEAD —
			// exit 0 with no stdout, exactly the "returned empty" symptom — so the
			// empty-HEAD race path fires even though the worktree HEAD is fine on
			// disk. The post-retry rev-parse (worktreeAddCalls==2) runs the real git
			// against the recreated worktree and returns a valid SHA.
			if name == "git" && containsArg(args, "rev-parse") && worktreeAddCalls == 1 {
				return exec.CommandContext(ctx, "sh", "-c", "exit 0")
			}
			// mkdir, rm -rf, branch -D, prune, and the post-retry rev-parse: real.
			return exec.CommandContext(ctx, name, args...)
		},
	}

	// A non-nil runner marks this as a REMOTE run for the empty-HEAD/cleanup path.
	cfg := NoWorktreeRootOverride().WithRunner(rr)

	if err := CreateWorktree(t.Context(), repo, runID, sha, cfg); err != nil {
		t.Fatalf("hk-iaj1w: CreateWorktree (with empty-HEAD retry) returned error: %v", err)
	}

	// HEAD must have been validated through the runner (not os.Stat / bare-local).
	if !hasRunnerCall(rr, "git", []string{"-C", worktreePath, "rev-parse", "HEAD"}) {
		t.Errorf("hk-iaj1w: expected runner call `git -C %q rev-parse HEAD` (post-add HEAD validation); recorded: %v", worktreePath, rr.Calls)
	}

	// The retry must have actually happened (≥2 worktree-add calls).
	if worktreeAddCalls < 2 {
		t.Fatalf("hk-iaj1w: expected ≥2 `git worktree add` calls (empty-HEAD race + retry), got %d", worktreeAddCalls)
	}

	// Cleanup must have removed the partial worktree dir VIA THE RUNNER — the same
	// remote-aware cleanup the hk-3vbc commondir-race path uses (not os.RemoveAll,
	// a local no-op on a remote worker).
	if !hasRunnerCall(rr, "rm", []string{"-rf", worktreePath}) {
		t.Errorf("hk-iaj1w: expected runner call `rm -rf %q` in cleanup; recorded: %v", worktreePath, rr.Calls)
	}
	if !hasRunnerCall(rr, "git", []string{"-C", repo, "branch", "-D", branch}) {
		t.Errorf("hk-iaj1w: expected runner call `git -C %q branch -D %q`; recorded: %v", repo, branch, rr.Calls)
	}

	// After a successful retry the worktree must exist on disk with a real HEAD.
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("hk-iaj1w: worktree not present after successful retry at %q: %v", worktreePath, err)
	}
}

// TestHKIAJ1W_RemoteEmptyHEADAlwaysExhaustsRetries verifies the failure mode is
// bounded: when EVERY `git worktree add` leaves an unresolvable HEAD,
// CreateWorktree exhausts its retry budget and returns a clear
// ErrWorktreeCreationFailed — never an infinite loop and never a silent
// empty-HEAD "success" reaching dispatch.
//
// FAITHFUL fake (as in the retry test): every `git worktree add` runs the REAL
// git so each attempt creates a live branch + registered worktree that the
// cleanup must fully tear down before the next attempt. This also guards the
// cleanup-ordering bug across the whole budget: with branch -D before prune the
// second add would die "already exists" and the loop would stop at 2 attempts
// (not exhaust the full budget). Only the HEAD-VALIDATION rev-parse is faked to
// report empty so the empty-HEAD path fires on every attempt.
func TestHKIAJ1W_RemoteEmptyHEADAlwaysExhaustsRetries(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "019ec83c-iaj1-7001-0001-000000000002"

	var worktreeAddCalls int
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "git" && containsArg(args, "worktree") && containsArg(args, "add") {
				worktreeAddCalls++
				// Run the real git so a live branch + registered worktree exists for
				// the cleanup path to tear down on every attempt.
				return exec.CommandContext(ctx, name, args...)
			}
			if name == "git" && containsArg(args, "rev-parse") {
				// HEAD validation never resolves: exit 0 with empty stdout, every time.
				return exec.CommandContext(ctx, "sh", "-c", "exit 0")
			}
			// mkdir, rm -rf, branch -D, prune: real.
			return exec.CommandContext(ctx, name, args...)
		},
	}

	cfg := NoWorktreeRootOverride().WithRunner(rr)

	err := CreateWorktree(t.Context(), repo, runID, sha, cfg)
	if err == nil {
		t.Fatal("hk-iaj1w: expected an error when HEAD never resolves, got nil (empty-HEAD silently reached dispatch)")
	}
	if !errors.Is(err, ErrWorktreeCreationFailed) {
		t.Errorf("hk-iaj1w: expected error wrapping ErrWorktreeCreationFailed, got: %v", err)
	}

	// Bounded: one initial attempt + worktreeAddMaxRetries retries.
	if want := worktreeAddMaxRetries + 1; worktreeAddCalls != want {
		t.Errorf("hk-iaj1w: expected exactly %d `git worktree add` attempts (bounded retry), got %d", want, worktreeAddCalls)
	}
}
