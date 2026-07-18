package tmux

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
)

// CommandRunner is the seam for executing external commands in the tmux package.
// Production code uses LocalRunner (exec.CommandContext); tests and future ssh
// transports supply their own implementation.
//
// Implementations MUST be safe for concurrent use.
type CommandRunner interface {
	Command(ctx context.Context, name string, args ...string) *exec.Cmd
}

// LocalRunner is the production CommandRunner. It delegates directly to
// exec.CommandContext with no additional wrapping.
type LocalRunner struct{}

// Command returns exec.CommandContext(ctx, name, args...).
func (LocalRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// RecordingCall holds the name and arguments for a single CommandRunner.Command
// invocation. Exported so downstream test packages can inspect call records.
type RecordingCall struct {
	Name string
	Args []string
}

// RecordingRunner is an exported test helper for the tmux package and
// downstream packages. It records each Command call's argv and produces the
// *exec.Cmd via CmdFunc (when non-nil) or exec.CommandContext (when nil).
// Concurrent-safe.
//
// Typical usage:
//
//	rr := &RecordingRunner{}
//	a := tmux.OSAdapter{}.WithRunner(rr)
//	a.ListSessions(ctx)
//	// inspect rr.Calls[0].Args
type RecordingRunner struct {
	mu sync.Mutex

	// Calls accumulates one entry per Command invocation in call order.
	Calls []RecordingCall

	// CmdFunc, when non-nil, is called to produce the *exec.Cmd. When nil,
	// exec.CommandContext is used, so the binary must be on PATH.
	CmdFunc func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// Command records the invocation and returns a *exec.Cmd via CmdFunc or
// exec.CommandContext.
func (r *RecordingRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cp := make([]string, len(args))
	copy(cp, args)
	r.mu.Lock()
	r.Calls = append(r.Calls, RecordingCall{Name: name, Args: cp})
	r.mu.Unlock()
	if r.CmdFunc != nil {
		return r.CmdFunc(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...)
}

// SSHRunner is a CommandRunner that tunnels every command through ssh. Each
// call produces:
//
//	ssh [Opts...] <Host> -- <shell-quoted: name args...>
//
// IMPORTANT: OpenSSH does NOT deliver the post-host operands as a discrete argv
// vector — it space-joins them into a single string and runs that via the
// remote LOGIN SHELL ($SHELL -c "<joined>"). So the remote command must be
// shell-quoted by us, or the remote shell re-parses it: spaces re-split, and —
// the bug this guards against — a tmux token like `-F #{pane_id}` makes the
// remote shell treat `#{pane_id}` as the start of a `#` COMMENT, truncating the
// command (e.g. `tmux new-window … #{pane_id} …` collapses to `tmux new-window`,
// so the agent window — and its claude process — is never created on the worker
// → agent_ready never fires → 90s timeout). We single-quote every token so each
// arrives as one literal word, which also preserves spaces in paths/`-e K=V`
// values and slashes in window names. (hk-fxy9/hk-538l remote-launch family.)
//
// Callers may set cmd.Stdin after the call returns; SSHRunner does not touch
// it.  The daemon's LoadBuffer path relies on this to stream payload bytes into
// `tmux load-buffer -` over the ssh connection.
type SSHRunner struct {
	// Host is the SSH destination (user@host or bare host).
	Host string
	// Opts are extra flags passed to ssh before the host (e.g. ["-p", "2222"]).
	Opts []string
}

// shellQuoteArg wraps s in single quotes so a POSIX remote login shell receives
// it as exactly one literal word — neutralising spaces, `#`-comments, tmux
// format strings (`#{pane_id}`), and every other shell metacharacter. Embedded
// single quotes are escaped via the standard `'\”` idiom.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Command returns an *exec.Cmd that runs `ssh [Opts...] <Host> -- <quoted cmd>`,
// where the remote command (name + args) is shell-quoted token-by-token so the
// remote login shell reconstructs the exact argv. See the SSHRunner doc comment
// for why discrete-argv delivery does NOT happen over ssh.
func (s SSHRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	quoted := make([]string, 0, 1+len(args))
	quoted = append(quoted, shellQuoteArg(name))
	for _, a := range args {
		quoted = append(quoted, shellQuoteArg(a))
	}
	sshArgs := make([]string, 0, len(s.Opts)+3)
	sshArgs = append(sshArgs, s.Opts...)
	sshArgs = append(sshArgs, s.Host, "--", strings.Join(quoted, " "))
	return exec.CommandContext(ctx, "ssh", sshArgs...)
}

// IsSSHConnectionFailure reports whether err is an SSH transport failure.
// SSH exits with code 255 on connection errors (refused, timeout, host-key
// mismatch) to distinguish transport failures from remote-command exit codes.
//
// The failure surfaces in two shapes depending on the call path: a bare
// *exec.ExitError (255) when the caller inspects the ssh process directly, or a
// wrapped *ErrTmuxFailure{ExitCode:255} when a tmux OSAdapter command runs over
// the SSHRunner and captures the ssh exit code (ErrTmuxFailure does not unwrap
// to the underlying exec.ExitError, so both forms must be checked — hk-cjqyn).
//
// Bead: hk-rs-b11-offline-dh57.
func IsSSHConnectionFailure(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 255 {
		return true
	}
	var tmuxErr *ErrTmuxFailure
	if errors.As(err, &tmuxErr) && tmuxErr.ExitCode == 255 {
		return true
	}
	return false
}
