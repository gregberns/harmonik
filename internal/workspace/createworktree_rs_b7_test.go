package workspace

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestRSB7_CreateWorktreeRunner verifies that CreateWorktree routes git commands
// through the CommandRunner embedded in WorktreeRootConfig (remote-substrate B7).
//
// Key assertions:
//   - Local (default) runner: argv is `git -C <repoRoot> worktree add -b ...`
//   - SSHRunner: argv is `ssh <host> -- git -C <repoRoot> worktree add -b ...`
//   - No runner set: behaviour is byte-for-byte identical to LocalRunner.
func TestRSB7_CreateWorktreeRunner(t *testing.T) {
	t.Parallel()

	t.Run("local-runner-git-minus-C-form", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "019ec83c-rsb7-7001-0001-000000000001"
		branch := TaskBranchName(runID)
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		rr := &tmux.RecordingRunner{
			// nil CmdFunc → delegates to exec.CommandContext so the real git runs
			// and the worktree is created (argv shape is what we verify here).
		}
		cfg := NoWorktreeRootOverride().WithRunner(rr)

		if err := CreateWorktree(t.Context(), repo, runID, sha, cfg); err != nil {
			t.Fatalf("RSB7: CreateWorktree: %v", err)
		}

		if len(rr.Calls) == 0 {
			t.Fatal("RSB7: no Command calls recorded")
		}
		first := rr.Calls[0]

		// Must be `git -C <repo> worktree add -b <branch> <path> <sha>`
		if first.Name != "git" {
			t.Errorf("RSB7: Command name = %q, want git", first.Name)
		}
		if len(first.Args) < 2 || first.Args[0] != "-C" || first.Args[1] != repo {
			t.Errorf("RSB7: expected args[0]='-C' args[1]=%q, got %v", repo, first.Args)
		}
		wantSub := []string{"worktree", "add", "-b", branch, worktreePath, sha}
		gotSub := first.Args[2:]
		if strings.Join(gotSub, " ") != strings.Join(wantSub, " ") {
			t.Errorf("RSB7: git subcommand args = %v, want %v", gotSub, wantSub)
		}
	})

	t.Run("no-runner-default-is-local-and-creates-worktree", func(t *testing.T) {
		t.Parallel()

		// Zero-value WorktreeRootConfig (no runner set) must behave identically
		// to passing tmux.LocalRunner{} — the worktree is actually created.
		repo, sha := tempRepo(t)
		runID := "019ec83c-rsb7-7001-0001-000000000003"

		cfg := NoWorktreeRootOverride() // no WithRunner call

		if err := CreateWorktree(t.Context(), repo, runID, sha, cfg); err != nil {
			t.Fatalf("RSB7/default: CreateWorktree without runner: %v", err)
		}

		wantPath := WorktreePath(repo, runID, cfg)
		if _, err := os.Stat(wantPath); err != nil {
			t.Errorf("RSB7/default: worktree not found at %q: %v", wantPath, err)
		}
	})
}

// TestRSB7_SSHRunnerArgvShape verifies the exact argv recorded when a
// WorktreeRootConfig carries an SSHRunner.
//
// Expected git args recorded by RecordingRunner (before SSH wrapping):
//
//	git -C <repoRoot> worktree add -b <branch> <path> <sha>
//
// The SSHRunner then turns that into `ssh [opts] host -- git -C ...` when the
// Cmd is Run — but since we never Run (no real ssh), we validate recorded args.
func TestRSB7_SSHRunnerArgvShape(t *testing.T) {
	t.Parallel()

	// Use a real temp git repo so os.MkdirAll inside CreateWorktree succeeds
	// and the runner's Command is reached.  We use a bogus SHA so git fails
	// immediately after the first Command call — which is all we need.
	repo, _ := tempRepo(t)
	runID := "019ec83c-rsb7-7001-0001-000000000004"
	branch := TaskBranchName(runID)
	wantPath := WorktreePath(repo, runID, NoWorktreeRootOverride())

	sshHost := "deploy@builder.internal"
	ssh := tmux.SSHRunner{Host: sshHost, Opts: []string{"-p", "2222"}}

	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Produce the ssh Cmd; we never actually run it over the network.
			return ssh.Command(ctx, name, args...)
		},
	}

	cfg := NoWorktreeRootOverride().WithRunner(rr)

	// Bogus SHA → git worktree add fails, but the Command is called first.
	bogus := strings.Repeat("0", 40)
	_ = CreateWorktree(context.Background(), repo, runID, bogus, cfg)

	if len(rr.Calls) == 0 {
		t.Fatal("RSB7/argv: no calls recorded")
	}
	call := rr.Calls[0]

	// RecordingRunner records (name, args) as passed to it — before SSHRunner
	// wraps them.  name="git", args=["-C", repo, "worktree", "add", "-b", ...]
	if call.Name != "git" {
		t.Errorf("RSB7/argv: name=%q, want git", call.Name)
	}
	if len(call.Args) < 6 {
		t.Fatalf("RSB7/argv: too few args: %v", call.Args)
	}
	if call.Args[0] != "-C" {
		t.Errorf("RSB7/argv: args[0]=%q, want -C", call.Args[0])
	}
	if call.Args[1] != repo {
		t.Errorf("RSB7/argv: args[1]=%q, want %q", call.Args[1], repo)
	}
	if call.Args[2] != "worktree" || call.Args[3] != "add" || call.Args[4] != "-b" {
		t.Errorf("RSB7/argv: subcommand = %v, want [worktree add -b ...]", call.Args[2:])
	}
	if call.Args[5] != branch {
		t.Errorf("RSB7/argv: args[5]=%q, want branch %q", call.Args[5], branch)
	}
	if len(call.Args) < 7 || call.Args[6] != wantPath {
		t.Errorf("RSB7/argv: args[6]=%q, want worktree path %q", safeArgIdx(call.Args, 6), wantPath)
	}
}

func safeArgIdx(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<missing>"
}
