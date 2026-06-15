package tmux

import (
	"context"
	"errors"
	"os/exec"
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
//	ssh [Opts...] <Host> -- <name> <args...>
//
// Arguments are passed as discrete argv tokens (never shell-concatenated) so
// remote tmux/git receive exactly one token per argument. This preserves spaces
// inside working-directory paths and slashes inside window names without any
// quoting.
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

// Command returns an *exec.Cmd that runs `ssh [Opts...] <Host> -- <name> <args...>`.
func (s SSHRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	sshArgs := make([]string, 0, len(s.Opts)+2+1+len(args))
	sshArgs = append(sshArgs, s.Opts...)
	sshArgs = append(sshArgs, s.Host, "--", name)
	sshArgs = append(sshArgs, args...)
	return exec.CommandContext(ctx, "ssh", sshArgs...)
}

// IsSSHConnectionFailure reports whether err is an SSH transport failure.
// SSH exits with code 255 on connection errors (refused, timeout, host-key
// mismatch) to distinguish transport failures from remote-command exit codes.
//
// Bead: hk-rs-b11-offline-dh57.
func IsSSHConnectionFailure(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == 255
	}
	return false
}
