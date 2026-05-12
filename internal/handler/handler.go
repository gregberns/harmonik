// Package handler — Handler.Launch (MVH_ROADMAP row #7).
//
// Handler is the daemon-side entry point for spawning a Claude Code (or twin)
// subprocess, wiring its stdout to the per-session Watcher, and returning both
// handles to the caller.
//
// # Lifecycle
//
// NewHandler constructs a Handler bound to the provided event-bus Publisher and
// DeadLetterSink; these are session-scoped dependencies forwarded to
// handlercontract.SpawnWatcher on each Launch call.
//
// Launch:
//  1. Generates a new SessionID (UUIDv4).
//  2. Builds the exec.Cmd via exec.CommandContext — exec.Command is forbidden
//     per implementer-protocol.md (noctx linter rule).
//  3. Sets cmd.Dir, cmd.Env, and cmd.SysProcAttr via lifecycle.SpawnChildSysProcAttr
//     so the child lands in the daemon's process group (HC-044 / PL-006a).
//  4. Calls NewSession(ctx, cmd) which opens pipes, starts the subprocess, and
//     returns the Session handle.
//  5. Calls handlercontract.SpawnWatcher to attach an NDJSON read-loop to
//     sess.Stdout().
//  6. Returns (Session, *Watcher, nil).
//
// Cite: MVH_ROADMAP.md row #7; specs/handler-contract.md §4.3.HC-011, §4.5, §4.10.HC-044.
package handler

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// LaunchSpec carries the per-session subprocess configuration supplied by the
// caller (work-loop or test) to Handler.Launch.
//
// All fields are required unless noted optional.
type LaunchSpec struct {
	// Binary is the absolute path to the handler binary to execute.
	// Required (non-empty). Use ResolveLaunchPath to resolve a repo-relative
	// or system-handler reference before constructing a LaunchSpec.
	Binary string

	// Args are the command-line arguments forwarded to the binary.
	// May be nil or empty.
	Args []string

	// Env is the full environment for the subprocess in "KEY=VALUE" form.
	// If nil, the child inherits no environment (not the parent's os.Environ).
	// The caller is responsible for injecting HARMONIK_PROJECT_HASH via
	// lifecycle.ProvenanceEnvVar.
	Env []string

	// WorkDir is the working-directory path for the subprocess.
	// Required (non-empty). Typically the worktree path assigned to the bead.
	WorkDir string

	// Role is the handler-role string forwarded to SpawnWatcher as SessionID
	// annotation. Callers SHOULD use a stable human-readable value (e.g.
	// "implementer", "reviewer"). May be empty.
	Role string
}

// Handler is the daemon-side factory for handler sessions.
//
// Acquire via NewHandler; the zero value is not usable.
type Handler interface {
	// Launch starts a subprocess described by spec, wires its stdout to a
	// per-session Watcher, and returns both handles.
	//
	// The caller owns the Session lifecycle (Kill, Wait) and the Watcher
	// lifecycle (<-watcher.Done()). Typical usage:
	//
	//   sess, watcher, err := h.Launch(ctx, spec)
	//   if err != nil { ... }
	//   defer sess.Kill(ctx)
	//   <-watcher.Done()
	//   _ = sess.Wait(ctx)
	//
	// Returns a non-nil ErrStructural-wrapping error on any setup failure
	// (command build, pipe open, subprocess start).
	Launch(ctx context.Context, spec LaunchSpec) (Session, *handlercontract.Watcher, error)
}

// handler is the concrete implementation of Handler.
type handler struct {
	publisher  handlercontract.EventEmitter
	deadLetter handlercontract.WatcherDeadLetterSink
}

// NewHandler constructs a Handler whose Launch calls will forward events to
// publisher and route undeliverable events to deadLetter.
//
// Both arguments are required (non-nil); NewHandler panics if either is nil —
// that would be a daemon-configuration defect with no recovery path.
func NewHandler(publisher handlercontract.EventEmitter, deadLetter handlercontract.WatcherDeadLetterSink) Handler {
	if publisher == nil {
		panic("handler: NewHandler: publisher is nil — daemon defect")
	}
	if deadLetter == nil {
		panic("handler: NewHandler: deadLetter is nil — daemon defect")
	}
	return &handler{
		publisher:  publisher,
		deadLetter: deadLetter,
	}
}

// Launch starts the subprocess described by spec and attaches a Watcher to its
// stdout.
//
// Steps:
//  1. Generate a SessionID (UUIDv4 string).
//  2. Build the exec.Cmd via exec.CommandContext.
//  3. Apply cmd.Dir, cmd.Env, and SysProcAttr from lifecycle.SpawnChildSysProcAttr.
//  4. Call NewSession(ctx, cmd) to open pipes and start the child.
//  5. Call handlercontract.SpawnWatcher with sess.Stdout() as ProgressStream.
//  6. Return (sess, watcher, nil).
func (h *handler) Launch(ctx context.Context, spec LaunchSpec) (Session, *handlercontract.Watcher, error) {
	sessionID := handlercontract.NewSessionID()

	//nolint:gosec // G204: Binary is daemon-config-resolved; not user-controlled
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Dir = spec.WorkDir
	cmd.Env = spec.Env
	cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr(lifecycle.RecordedPGID())

	sess, err := NewSession(ctx, cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("handler: Launch: NewSession: %w", err)
	}

	watcher := handlercontract.SpawnWatcher(ctx, handlercontract.SpawnWatcherConfig{
		SessionID:      sessionID,
		ProgressStream: sess.Stdout(),
		Publisher:      h.publisher,
		DeadLetter:     h.deadLetter,
	})

	return sess, watcher, nil
}
