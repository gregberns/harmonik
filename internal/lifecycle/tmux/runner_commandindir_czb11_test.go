package tmux

// runner_commandindir_czb11_test.go — hk-czb11: SSHRunner.CommandInDir applies the
// working directory on the REMOTE worker (via `cd … && exec …`) and leaves the
// LOCAL exec.Cmd.Dir unset, so a remote worktree path is never assigned to the
// local ssh process's cwd (which would fork/exec-ENOENT).

import (
	"context"
	"strings"
	"testing"
)

func TestSSHRunner_CommandInDir_RemoteCwd_czb11(t *testing.T) {
	t.Parallel()

	sr := SSHRunner{Host: "worker-mac-1"}
	// A remote worktree path with a space, to exercise quoting.
	const remoteCwd = "/box-b/.harmonik/worktrees/run-x/work dir"
	cmd := sr.CommandInDir(context.Background(), remoteCwd, "codex", "app-server", "--flag=v")
	if cmd == nil {
		t.Fatal("CommandInDir: got nil cmd")
	}

	// The remote path must NEVER become the LOCAL process cwd (the hk-czb11 bug).
	if cmd.Dir != "" {
		t.Errorf("local cmd.Dir = %q; want unset — the remote cwd must not be a local Dir", cmd.Dir)
	}

	// Shape: ssh host -- <one remote operand>.
	argv := cmd.Args
	if len(argv) != 4 {
		t.Fatalf("argv = %v; want exactly 4 (ssh host -- <remote>)", argv)
	}
	if argv[0] != "ssh" || argv[1] != "worker-mac-1" || argv[2] != "--" {
		t.Fatalf("ssh prefix wrong: %v", argv)
	}
	remote := argv[3]

	// cd/&&/exec are LITERAL shell syntax; dir/name/args single-quoted token-by-token.
	want := "cd " + shellQuoteArg(remoteCwd) +
		" && exec " + shellQuoteArg("codex") +
		" " + shellQuoteArg("app-server") +
		" " + shellQuoteArg("--flag=v")
	if remote != want {
		t.Fatalf("remote command mismatch:\n got=%q\nwant=%q", remote, want)
	}

	// The && must be an UNQUOTED operator (else cd wouldn't chain to the child),
	// and the space-containing cwd must be single-quoted (one literal word).
	if !strings.Contains(remote, " && exec ") {
		t.Errorf("remote %q: missing unquoted `&& exec` — cd would not chain to the child", remote)
	}
	if strings.Contains(remote, shellQuoteArg("&&")) {
		t.Errorf("remote %q: `&&` is quoted (a literal arg to cd) — the chain would break", remote)
	}
	if !strings.Contains(remote, shellQuoteArg(remoteCwd)) {
		t.Errorf("remote %q: space-containing cwd not single-quoted", remote)
	}
}

// TestSSHRunner_CommandInDir_OptsAndExec confirms Opts are prepended before the
// host and `exec` is present so the remote child replaces the login shell.
func TestSSHRunner_CommandInDir_OptsAndExec_czb11(t *testing.T) {
	t.Parallel()

	sr := SSHRunner{Host: "w1", Opts: []string{"-p", "2222"}}
	cmd := sr.CommandInDir(context.Background(), "/wt", "codex")
	argv := cmd.Args
	// ssh -p 2222 w1 -- <remote>
	if len(argv) != 6 || argv[0] != "ssh" || argv[1] != "-p" || argv[2] != "2222" || argv[3] != "w1" || argv[4] != "--" {
		t.Fatalf("argv = %v; want [ssh -p 2222 w1 -- <remote>]", argv)
	}
	if want := "cd " + shellQuoteArg("/wt") + " && exec " + shellQuoteArg("codex"); argv[5] != want {
		t.Fatalf("remote = %q; want %q", argv[5], want)
	}
}
