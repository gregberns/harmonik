package gitprobe_test

// gitprobe_test.go — unit tests for the shared git probes.
//
// The two ResolveWorktreeHEADVia cases moved here verbatim-in-substance from
// internal/daemon/pasteinject_hkrsb9_test.go (hk-rs-b9-liveness-1m9n) when P2
// unit E1a lifted the probes out of the daemon: a moved function's tests move
// with it, or the new package ships untested and the old package keeps testing
// code it no longer owns.
//
// The nil-runner delegation and RunnerIsLocalFS cases are NEW. Both behaviours
// were load-bearing before the move — the nil path is the NFR7 byte-identical
// local guarantee, and RunnerIsLocalFS decides whether remote worktree paths get
// stat-ed on the wrong box — and neither had a direct test; daemon tests only
// referenced them in comments.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// initGitRepo makes a temp git repo with one commit and returns its path and HEAD.
func initGitRepo(t *testing.T) (repoPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

func TestResolveWorktreeHEADVia_RealGit(t *testing.T) {
	t.Parallel()
	repoPath, wantSHA := initGitRepo(t)

	rr := &tmux.RecordingRunner{} // nil CmdFunc → real git runs
	got, err := gitprobe.ResolveWorktreeHEADVia(context.Background(), rr, repoPath)
	if err != nil {
		t.Fatalf("ResolveWorktreeHEADVia: %v", err)
	}
	if got != wantSHA {
		t.Errorf("HEAD = %q, want %q", got, wantSHA)
	}
	// The probe must use `git -C <path> rev-parse HEAD` — the -C form is what
	// makes the same argv work over an SSH runner, where cmd.Dir means nothing.
	if len(rr.Calls) < 1 || rr.Calls[0].Name != "git" {
		t.Fatalf("expected a git call, got %v", rr.Calls)
	}
	args := rr.Calls[0].Args
	if len(args) < 4 || args[0] != "-C" || args[1] != repoPath || args[2] != "rev-parse" || args[3] != "HEAD" {
		t.Errorf("git args = %v, want [-C %s rev-parse HEAD]", args, repoPath)
	}
}

func TestResolveWorktreeHEADVia_SSHArgv(t *testing.T) {
	t.Parallel()
	ssh := tmux.SSHRunner{Host: "worker@remote.internal"}
	rr := &tmux.RecordingRunner{CmdFunc: ssh.Command}
	// The probe itself fails (no real ssh host) — only the recorded argv is under
	// test, so the error is expected and reported rather than asserted on.
	if _, err := gitprobe.ResolveWorktreeHEADVia(context.Background(), rr, "/remote/path/wt"); err != nil {
		t.Logf("ssh probe failed as expected against an unreachable host: %v", err)
	}

	if len(rr.Calls) < 1 {
		t.Fatal("ssh: no calls recorded")
	}
	call := rr.Calls[0]
	if call.Name != "git" {
		t.Errorf("ssh: name = %q, want git", call.Name)
	}
	if len(call.Args) < 4 || call.Args[0] != "-C" || call.Args[2] != "rev-parse" || call.Args[3] != "HEAD" {
		t.Errorf("ssh: git args = %v, want [-C <path> rev-parse HEAD]", call.Args)
	}
}

// TestResolveWorktreeHEADVia_NilRunnerDelegates pins NFR7: a nil runner must take
// the bare-local path and return the same SHA, so callers can pass the per-run
// runner unconditionally without changing local behaviour.
func TestResolveWorktreeHEADVia_NilRunnerDelegates(t *testing.T) {
	t.Parallel()
	repoPath, wantSHA := initGitRepo(t)

	got, err := gitprobe.ResolveWorktreeHEADVia(context.Background(), nil, repoPath)
	if err != nil {
		t.Fatalf("ResolveWorktreeHEADVia(nil): %v", err)
	}
	if got != wantSHA {
		t.Errorf("nil-runner HEAD = %q, want %q — the nil path must match the bare-local probe", got, wantSHA)
	}
	direct, err := gitprobe.ResolveWorktreeHEAD(context.Background(), repoPath)
	if err != nil {
		t.Fatalf("ResolveWorktreeHEAD: %v", err)
	}
	if direct != got {
		t.Errorf("ResolveWorktreeHEAD = %q but Via(nil) = %q; the two local paths must agree", direct, got)
	}
}

// TestResolveWorktreeHEAD_NotARepo pins the error path: a directory that is not a
// git repo must return an error, never an empty SHA that a caller could mistake
// for a real HEAD.
func TestResolveWorktreeHEAD_NotARepo(t *testing.T) {
	t.Parallel()
	got, err := gitprobe.ResolveWorktreeHEAD(context.Background(), t.TempDir())
	if err == nil {
		t.Fatalf("want an error for a non-repo dir, got sha %q", got)
	}
	if got != "" {
		t.Errorf("sha = %q on error, want empty", got)
	}
}

func TestIsAncestor(t *testing.T) {
	t.Parallel()
	repoPath, headSHA := initGitRepo(t)
	treeCmd := exec.CommandContext(t.Context(), "git", "write-tree")
	treeCmd.Dir = repoPath
	treeOut, err := treeCmd.Output()
	if err != nil {
		t.Fatalf("git write-tree: %v", err)
	}
	//nolint:gosec // G204: tree ID comes from git write-tree in this test-only repository.
	orphanCmd := exec.CommandContext(t.Context(), "git", "commit-tree", strings.TrimSpace(string(treeOut)), "-m", "unrelated")
	orphanCmd.Dir = repoPath
	orphanOut, err := orphanCmd.Output()
	if err != nil {
		t.Fatalf("git commit-tree: %v", err)
	}
	orphanSHA := strings.TrimSpace(string(orphanOut))

	cases := []struct {
		name       string
		ancestor   string
		descendant string
		want       bool
	}{
		{name: "self", ancestor: headSHA, descendant: headSHA, want: true},
		{name: "not_ancestor", ancestor: headSHA, descendant: orphanSHA, want: false},
		{name: "git_failure", ancestor: "0000000000000000000000000000000000000000", descendant: headSHA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gitprobe.IsAncestor(t.Context(), repoPath, tc.ancestor, tc.descendant)
			if tc.name == "git_failure" {
				if err == nil {
					t.Fatal("IsAncestor with a missing commit returned nil error")
				}
				if got {
					t.Error("IsAncestor with a missing commit = true, want false")
				}
				return
			}
			if err != nil {
				t.Fatalf("IsAncestor: %v", err)
			}
			if got != tc.want {
				t.Errorf("IsAncestor = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunnerIsLocalFS is the classification the remote path depends on: only a
// nil runner and LocalRunner name box-A paths that os.Stat can read. Any other
// transport points at a worktree on another machine, where a box-A stat would
// silently read an unrelated file.
func TestRunnerIsLocalFS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		runner tmux.CommandRunner
		want   bool
	}{
		{"nil", nil, true},
		{"local", tmux.LocalRunner{}, true},
		{"ssh", tmux.SSHRunner{Host: "worker@remote.internal"}, false},
		{"recording", &tmux.RecordingRunner{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := gitprobe.RunnerIsLocalFS(tc.runner); got != tc.want {
				t.Errorf("RunnerIsLocalFS(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
