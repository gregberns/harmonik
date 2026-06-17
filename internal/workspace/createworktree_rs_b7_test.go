package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

		// After hk-eodo fix: call[0]=mkdir -p, call[1]=git worktree add.
		if len(rr.Calls) < 2 {
			t.Fatalf("RSB7: expected ≥2 calls (mkdir + git), got %d: %v", len(rr.Calls), rr.Calls)
		}
		if rr.Calls[0].Name != "mkdir" {
			t.Errorf("RSB7: call[0] name=%q, want mkdir", rr.Calls[0].Name)
		}
		git := rr.Calls[1]

		// Must be `git -C <repo> worktree add -b <branch> <path> <sha>`
		if git.Name != "git" {
			t.Errorf("RSB7: call[1] name = %q, want git", git.Name)
		}
		if len(git.Args) < 2 || git.Args[0] != "-C" || git.Args[1] != repo {
			t.Errorf("RSB7: expected args[0]='-C' args[1]=%q, got %v", repo, git.Args)
		}
		wantSub := []string{"worktree", "add", "-b", branch, worktreePath, sha}
		gotSub := git.Args[2:]
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
// After the hk-eodo fix, CreateWorktree routes the parent-directory creation
// through the runner (mkdir -p) when a remote runner is set.  Expected call
// sequence recorded by RecordingRunner (before SSH wrapping):
//
//  1. mkdir -p <parentDir>
//  2. git -C <repoRoot> worktree add -b <branch> <path> <sha>
//
// The SSHRunner then turns each into `ssh [opts] host -- <cmd> <args...>` when
// Run — but since we never Run (no real ssh), we validate recorded args.
func TestRSB7_SSHRunnerArgvShape(t *testing.T) {
	t.Parallel()

	// Use a real temp git repo so the runner's Commands are reached.
	// We use a bogus SHA so git fails after the mkdir succeeds — which is all we need.
	repo, _ := tempRepo(t)
	runID := "019ec83c-rsb7-7001-0001-000000000004"
	branch := TaskBranchName(runID)
	wantPath := WorktreePath(repo, runID, NoWorktreeRootOverride())
	wantParentDir := filepath.Dir(wantPath)

	sshHost := "deploy@builder.internal"
	ssh := tmux.SSHRunner{Host: sshHost, Opts: []string{"-p", "2222"}}

	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "mkdir" {
				// Run mkdir locally so it succeeds and we reach the git call.
				return exec.CommandContext(ctx, name, args...)
			}
			// For git: produce the ssh Cmd shape for argv validation.
			// We never actually run it over the network.
			return ssh.Command(ctx, name, args...)
		},
	}

	cfg := NoWorktreeRootOverride().WithRunner(rr)

	// Bogus SHA → git worktree add (via SSH) fails, but mkdir and the first git
	// Command are both called — which is all we need for argv validation.
	bogus := strings.Repeat("0", 40)
	_ = CreateWorktree(context.Background(), repo, runID, bogus, cfg)

	if len(rr.Calls) < 2 {
		t.Fatalf("RSB7/argv: expected ≥2 calls (mkdir + git), got %d: %v", len(rr.Calls), rr.Calls)
	}

	// Call 0: mkdir -p <parentDir> — the hk-eodo fix ensures remote mkdir.
	mkdir := rr.Calls[0]
	if mkdir.Name != "mkdir" {
		t.Errorf("RSB7/argv: call[0] name=%q, want mkdir", mkdir.Name)
	}
	if len(mkdir.Args) < 2 || mkdir.Args[0] != "-p" || mkdir.Args[1] != wantParentDir {
		t.Errorf("RSB7/argv: call[0] args=%v, want [-p %q]", mkdir.Args, wantParentDir)
	}

	// Call 1: git -C <repo> worktree add -b <branch> <path> <sha>
	call := rr.Calls[1]
	// RecordingRunner records (name, args) as passed to it — before SSHRunner
	// wraps them.  name="git", args=["-C", repo, "worktree", "add", "-b", ...]
	if call.Name != "git" {
		t.Errorf("RSB7/argv: call[1] name=%q, want git", call.Name)
	}
	if len(call.Args) < 6 {
		t.Fatalf("RSB7/argv: call[1] too few args: %v", call.Args)
	}
	if call.Args[0] != "-C" {
		t.Errorf("RSB7/argv: call[1] args[0]=%q, want -C", call.Args[0])
	}
	if call.Args[1] != repo {
		t.Errorf("RSB7/argv: call[1] args[1]=%q, want %q", call.Args[1], repo)
	}
	if call.Args[2] != "worktree" || call.Args[3] != "add" || call.Args[4] != "-b" {
		t.Errorf("RSB7/argv: call[1] subcommand = %v, want [worktree add -b ...]", call.Args[2:])
	}
	if call.Args[5] != branch {
		t.Errorf("RSB7/argv: call[1] args[5]=%q, want branch %q", call.Args[5], branch)
	}
	if len(call.Args) < 7 || call.Args[6] != wantPath {
		t.Errorf("RSB7/argv: call[1] args[6]=%q, want worktree path %q", safeArgIdx(call.Args, 6), wantPath)
	}
}

// TestRSB7_RemoteMkdirCreatesParentOnWorker verifies that CreateWorktree calls
// mkdir -p via the runner for the parent directory when a remote runner is set
// (hk-eodo: TOCTOU fix — remote substrate worktree-isolation loss).
//
// Without the fix, os.MkdirAll runs locally while git worktree add runs
// remotely; the worker's .harmonik/worktrees/ dir may not exist.
func TestRSB7_RemoteMkdirCreatesParentOnWorker(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "019ec83c-rsb7-eodo-0001-000000000005"
	wantPath := WorktreePath(repo, runID, NoWorktreeRootOverride())
	wantParentDir := filepath.Dir(wantPath)

	var mkdirCalls []tmux.RecordingCall
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// For mkdir: succeed via /bin/mkdir so the parent is created.
			// For git: run the real binary (sha is valid so worktree is created).
			return exec.CommandContext(ctx, name, args...)
		},
	}

	cfg := NoWorktreeRootOverride().WithRunner(rr)

	if err := CreateWorktree(t.Context(), repo, runID, sha, cfg); err != nil {
		t.Fatalf("RSB7/eodo: CreateWorktree: %v", err)
	}

	// Collect mkdir calls.
	for _, c := range rr.Calls {
		if c.Name == "mkdir" {
			mkdirCalls = append(mkdirCalls, c)
		}
	}

	if len(mkdirCalls) == 0 {
		t.Fatal("RSB7/eodo: no mkdir call recorded — remote parent dir not created via runner")
	}
	mk := mkdirCalls[0]
	if len(mk.Args) < 2 || mk.Args[0] != "-p" || mk.Args[1] != wantParentDir {
		t.Errorf("RSB7/eodo: mkdir args=%v, want [-p %q]", mk.Args, wantParentDir)
	}
}

func safeArgIdx(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<missing>"
}
