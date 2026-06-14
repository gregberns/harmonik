package tmux

import (
	"context"
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
