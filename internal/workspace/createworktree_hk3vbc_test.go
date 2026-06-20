package workspace

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestHK3VBC_RemoteTransientRaceCleansViaRunner verifies that, when a REMOTE run
// (cfg.runner != nil) hits the transient macOS/APFS commondir race on a
// `git worktree add`, the retry-cleanup block removes the partial worktree
// directory THROUGH THE RUNNER (`rm -rf <path>`) rather than via os.RemoveAll.
//
// Root cause (hk-3vbc): os.RemoveAll runs LOCALLY on box A, but for a remote
// run the worktree dir lives on the worker. A local no-op left the stale dir +
// branch behind, so the next dispatch collided with "branch/reference already
// exists" — which is not matched as a transient race — and the dispatch failed
// silently. The fix routes the dir removal through the runner.
//
// Assertions:
//   - The first `git worktree add` fails with the commondir signature → retry.
//   - The cleanup issues `rm -rf <worktreePath>` via the runner.
//   - The cleanup issues `git branch -D <branch>` and `git worktree prune`.
//   - The second `git worktree add` succeeds → CreateWorktree returns nil.
func TestHK3VBC_RemoteTransientRaceCleansViaRunner(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "019ec83c-3vbc-7001-0001-000000000001"
	branch := TaskBranchName(runID)
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

	const commondirRaceOutput = "fatal: failed to read .git/worktrees/abc/commondir: Undefined error: 0"

	var worktreeAddCalls int
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Detect the `git ... worktree add ...` invocation.
			if name == "git" && containsArg(args, "worktree") && containsArg(args, "add") {
				worktreeAddCalls++
				if worktreeAddCalls == 1 {
					// First attempt: emit the transient commondir race to stderr
					// and exit non-zero so the retry path fires.
					return exec.CommandContext(ctx, "sh", "-c",
						"echo '"+commondirRaceOutput+"' 1>&2; exit 128")
				}
				// Retry: run the real git so the worktree is actually created.
				return exec.CommandContext(ctx, name, args...)
			}
			// mkdir, branch -D, worktree prune, rm -rf: run the real binary.
			return exec.CommandContext(ctx, name, args...)
		},
	}

	// A non-nil runner marks this as a REMOTE run for the cleanup branch.
	cfg := NoWorktreeRootOverride().WithRunner(rr)

	if err := CreateWorktree(t.Context(), repo, runID, sha, cfg); err != nil {
		t.Fatalf("hk-3vbc: CreateWorktree (with retry) returned error: %v", err)
	}

	// The retry must have actually happened (≥2 worktree-add calls).
	if worktreeAddCalls < 2 {
		t.Fatalf("hk-3vbc: expected ≥2 `git worktree add` calls (transient race + retry), got %d", worktreeAddCalls)
	}

	// Assert the cleanup removed the partial worktree dir VIA THE RUNNER:
	// a recorded `rm -rf <worktreePath>` call must exist. This is the core
	// regression guard — without the fix the removal was os.RemoveAll (never
	// recorded by the runner) and the remote dir survived.
	if !hasRunnerCall(rr, "rm", []string{"-rf", worktreePath}) {
		t.Errorf("hk-3vbc: expected runner call `rm -rf %q` in cleanup; recorded calls: %v", worktreePath, rr.Calls)
	}

	// Sibling cleanup calls must also route through the runner.
	if !hasRunnerCall(rr, "git", []string{"-C", repo, "branch", "-D", branch}) {
		t.Errorf("hk-3vbc: expected runner call `git -C %q branch -D %q`; recorded calls: %v", repo, branch, rr.Calls)
	}
	if !hasRunnerCall(rr, "git", []string{"-C", repo, "worktree", "prune"}) {
		t.Errorf("hk-3vbc: expected runner call `git -C %q worktree prune`; recorded calls: %v", repo, rr.Calls)
	}

	// After a successful retry the worktree must exist on disk (the retry ran
	// the real git locally in this test).
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("hk-3vbc: worktree not present after successful retry at %q: %v", worktreePath, err)
	}

	// No stale duplicate branch should linger: `git branch --list <branch>`
	// must report exactly one entry (the retry's branch), not a collision.
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", branch).CombinedOutput()
	if n := strings.Count(string(out), branch); n != 1 {
		t.Errorf("hk-3vbc: expected exactly one branch %q after retry, got %d (output: %q)", branch, n, string(out))
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// hasRunnerCall reports whether the recorder captured a call with the given name
// whose args BEGIN with the given prefix args (exact match on the prefix).
func hasRunnerCall(rr *tmux.RecordingRunner, name string, wantArgs []string) bool {
	for _, c := range rr.Calls {
		if c.Name != name || len(c.Args) < len(wantArgs) {
			continue
		}
		match := true
		for i, w := range wantArgs {
			if c.Args[i] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
