// Package handler — Substrate seam types (hk-gql20.11).
//
// Substrate is an optional alternative spawn mechanism for handler.Launch.
// When LaunchSpec.Substrate is non-nil, Launch calls Substrate.SpawnWindow
// instead of building an exec.Cmd. The concrete substrate implementation
// (e.g. tmuxsubstrate in internal/daemon) is injected by the daemon
// composition root so that internal/handler never imports internal/lifecycle/tmux
// (depguard: handler-tmux cross-import is forbidden).
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021b — "Substrate seam";
// specs/handler-contract.md HC-054.
package handler

import (
	"context"
	"io"

	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// Substrate is the interface through which the daemon composition root
// injects a subprocess-hosting mechanism into handler.Launch.
//
// The only method is SpawnWindow, which creates a new subprocess (e.g. a
// tmux window) and returns a SubstrateSession handle. The handler package
// defines this interface; concrete implementations live outside handler
// (typically internal/daemon/tmuxsubstrate.go) to avoid depguard violations.
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021b; design §4 "Substrate seam".
type Substrate interface {
	// SpawnWindow creates a new hosted window/session for the given parameters
	// and returns a handle to the running session.
	//
	// Returns a non-nil error if the spawn fails (e.g. tmux session missing,
	// window-name collision). Errors SHOULD be wrapped with ErrStructural for
	// daemon-level routing.
	SpawnWindow(ctx context.Context, in SubstrateSpawn) (SubstrateSession, error)
}

// SubstrateSpawn carries the per-window parameters for Substrate.SpawnWindow.
//
// All fields except Env may be empty only in tests. The Cwd field is the
// working directory for the subprocess (e.g. the bead's worktree path).
//
// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
type SubstrateSpawn struct {
	// WindowName is the pre-computed, deterministic window name.
	// Derived by tmux.WindowName (hk-gql20.8); opaque to the handler package.
	WindowName string

	// Cwd is the working directory for the hosted subprocess. Typically the
	// bead worktree path.
	Cwd string

	// Env is the full environment for the subprocess in "KEY=VALUE" form.
	// The substrate MUST forward these to the child process verbatim.
	Env []string

	// Argv is the binary and its arguments: Argv[0] is the binary path,
	// Argv[1:] are the arguments. MUST be non-empty.
	Argv []string

	// StdinDevNull, when true, instructs the substrate to redirect stdin from
	// /dev/null so the subprocess does not block waiting for terminal input.
	//
	// Required for ProcessExit harnesses (e.g. codex) that run inside a tmux
	// pane: the pane PTY never sends EOF, causing codex 0.139.0 to block on
	// "Reading additional input from stdin...". Redirecting stdin unblocks it.
	//
	// MUST NOT be set for claude (pane-paste harness): claude does not read
	// stdin and the paste-inject path is unaffected.
	//
	// Bead: hk-rpr6.
	StdinDevNull bool
}

// SubstrateSession is the daemon's handle on a subprocess hosted by a Substrate.
//
// It mirrors the lifecycle surface of Session but is scoped to the operations
// available for substrate-hosted (e.g. tmux-window) processes. Notably:
//   - Stdout() returns nil for tmux-hosted sessions — the bridge wire is a
//     Unix socket, not a stdout pipe. Handler.Launch must handle a nil Stdout.
//   - SendInput and CloseStdin are not part of this interface; the substrate
//     owns the child's stdin (typically the pty managed by tmux).
//
// Spec ref: handler-contract.md HC-054; design §4 "Substrate seam".
type SubstrateSession interface {
	// Kill terminates the hosted subprocess. For tmux-based substrates this
	// issues `tmux kill-window`. MUST be idempotent.
	Kill(ctx context.Context) error

	// Wait blocks until the hosted subprocess exits and is reaped.
	Wait(ctx context.Context) error

	// Outcome returns exit metadata once Wait has returned. Calling Outcome
	// before Wait returns a zero-value Outcome.
	Outcome() Outcome

	// PID returns the PID of the process running inside the hosted window
	// (pane_pid for tmux). Returns 0 if the PID is unknown.
	PID() int

	// Stdout returns the io.Reader attached to the subprocess stdout, or nil
	// when the substrate does not expose a stdout pipe (tmux-hosted sessions
	// use the Unix socket hook-relay instead). Callers MUST check for nil
	// before passing to SpawnWatcher.
	Stdout() io.Reader
}

// substrateSessionAdapter adapts a SubstrateSession to the handler.Session
// interface so that Handler.Launch can return a uniform Session regardless
// of whether an exec.Cmd or a Substrate was used.
//
// Fields that have no substrate equivalent (stdin write, stderr) are stubbed.
type substrateSessionAdapter struct {
	inner   SubstrateSession
	machine *hclifecycle.Machine
}

// Compile-time assertion: substrateSessionAdapter implements Session.
var _ Session = (*substrateSessionAdapter)(nil)

// SendInput is a no-op for substrate sessions: the pty is managed by the
// substrate (e.g. tmux); callers cannot write to the child's stdin via this
// handle. Returns nil silently.
func (a *substrateSessionAdapter) SendInput(_ context.Context, _ string) error {
	return nil
}

// Kill delegates to the inner SubstrateSession.
func (a *substrateSessionAdapter) Kill(ctx context.Context) error {
	return a.inner.Kill(ctx)
}

// Wait delegates to the inner SubstrateSession.
func (a *substrateSessionAdapter) Wait(ctx context.Context) error {
	return a.inner.Wait(ctx)
}

// Outcome delegates to the inner SubstrateSession.
func (a *substrateSessionAdapter) Outcome() Outcome {
	return a.inner.Outcome()
}

// Stdout delegates to the inner SubstrateSession, which may return nil for
// tmux-hosted sessions.
func (a *substrateSessionAdapter) Stdout() io.Reader {
	return a.inner.Stdout()
}

// Stderr returns nil for substrate sessions: stderr is captured inside the
// tmux pane and is not piped to the daemon.
func (a *substrateSessionAdapter) Stderr() io.Reader {
	return nil
}

// CloseStdin is a no-op for substrate sessions: stdin is owned by the pty
// managed by the substrate.
func (a *substrateSessionAdapter) CloseStdin() error {
	return nil
}

// Machine returns the per-session lifecycle Machine. The machine is eagerly
// initialised at adapter construction time (newSubstrateAdapter) so this
// method is always safe to call concurrently without a data race.
func (a *substrateSessionAdapter) Machine() *hclifecycle.Machine {
	return a.machine
}

// newSubstrateAdapter wraps subSess in a substrateSessionAdapter and eagerly
// initialises its lifecycle Machine (Spawning→Initializing). The sessID and
// runID are used to identify the machine in lifecycle_transition events.
func newSubstrateAdapter(subSess SubstrateSession, sessID, runID string) *substrateSessionAdapter {
	if sessID == "" {
		sessID = "substrate-unknown"
	}
	if runID == "" {
		runID = "unknown"
	}
	m := hclifecycle.New(sessID, runID)
	// Substrate sessions skip the exec.Cmd path, so we go directly to
	// Initializing (the substrate has already spawned the process).
	_ = m.Transition(hclifecycle.StateInitializing, hclifecycle.ReasonSpawnStarted, "", "")
	return &substrateSessionAdapter{inner: subSess, machine: m}
}
