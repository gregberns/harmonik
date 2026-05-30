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
	"io"
	"os"
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

	// HandlerSpec is the JSON payload delivered to the subprocess via stdin
	// immediately after cmd.Start per specs/handler-contract.md §4.2.HC-005.
	// When non-nil, Launch encodes it as compact JSON, writes it followed by a
	// newline to the subprocess stdin pipe, and then closes the write end so the
	// subprocess sees EOF after reading the spec. When nil, stdin is left open
	// and no JSON is written (legacy / test-only path).
	//
	// The delivery runs in a goroutine bounded by ctx; if ctx expires before
	// the write completes the goroutine exits and the subprocess receives a
	// broken-pipe error on its next stdin read.
	HandlerSpec *handlercontract.LaunchSpec

	// StdoutWrapper is an optional function that wraps the subprocess stdout
	// io.Reader before it is passed to handlercontract.SpawnWatcher.
	//
	// When non-nil, Launch calls StdoutWrapper(sess.Stdout()) and passes the
	// returned io.Reader to SpawnWatcher as ProgressStream instead of the raw
	// pipe.  The wrapper MUST pass all bytes through to the caller's io.Reader
	// unchanged; it may observe bytes in-flight for side effects (e.g., extracting
	// a session ID from handler_capabilities per CHB-023).
	//
	// When nil, Launch passes sess.Stdout() directly to SpawnWatcher (default
	// behaviour; all existing callers are unaffected).
	//
	// Spec: specs/claude-hook-bridge.md §4.6.CHB-023.
	StdoutWrapper func(io.Reader) io.Reader

	// Substrate, when non-nil, indicates the subprocess MUST be hosted inside
	// a substrate-managed environment (e.g. a tmux window) rather than spawned
	// directly via exec.CommandContext.
	//
	// When nil, Launch preserves the current exec.CommandContext path (backward
	// compatible; all existing callers are unaffected).
	//
	// When non-nil, Launch calls Substrate.SpawnWindow with a SubstrateSpawn
	// built from Binary, Args, Env, and WorkDir. The returned SubstrateSession
	// is adapted to Session via substrateSessionAdapter. If the SubstrateSession
	// returns nil from Stdout(), SpawnWatcher is NOT wired (the bridge wire is
	// the daemon Unix socket in that case).
	//
	// The daemon composition root (internal/daemon) constructs the concrete
	// substrate (tmuxsubstrate.New) and injects it here; internal/handler MUST
	// NOT import internal/lifecycle/tmux (depguard).
	//
	// Spec ref: specs/process-lifecycle.md §4.7 PL-021b; handler-contract.md HC-054.
	// Bead: hk-gql20.11.
	Substrate Substrate
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
	registry   *handlercontract.AdapterRegistry
}

// NewHandler constructs a Handler whose Launch calls will forward events to
// publisher and route undeliverable events to deadLetter.
//
// All three arguments are required (non-nil); NewHandler panics if any is nil —
// that would be a daemon-configuration defect with no recovery path.
//
// registry is stored as a latent seam for post-MVH adapter-selection in Launch;
// it is not consulted at MVH (hk-gql20.16).
func NewHandler(publisher handlercontract.EventEmitter, deadLetter handlercontract.WatcherDeadLetterSink, registry *handlercontract.AdapterRegistry) Handler {
	if publisher == nil {
		panic("handler: NewHandler: publisher is nil — daemon defect")
	}
	if deadLetter == nil {
		panic("handler: NewHandler: deadLetter is nil — daemon defect")
	}
	if registry == nil {
		panic("handler: NewHandler: registry is nil — daemon defect")
	}
	return &handler{
		publisher:  publisher,
		deadLetter: deadLetter,
		registry:   registry,
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
//  5. If spec.HandlerSpec is non-nil, deliver the JSON-encoded LaunchSpec to
//     subprocess stdin in a goroutine bounded by ctx, then close the write end
//     per HC-005. Delivery errors are non-fatal to Launch but logged to stderr.
//  6. Call handlercontract.SpawnWatcher with sess.Stdout() as ProgressStream.
//  7. Return (sess, watcher, nil).
func (h *handler) Launch(ctx context.Context, spec LaunchSpec) (Session, *handlercontract.Watcher, error) {
	sessionID := handlercontract.NewSessionID()

	// CHB-007: refuse launch if spec.Args contains a forbidden Claude flag or
	// spec.Env contains a forbidden env var.  This guard runs before any
	// subprocess is started so neither the exec.CommandContext path nor the
	// substrate path can bypass it.
	//
	// Spec: specs/claude-hook-bridge.md §4.2 CHB-007.
	if err := CheckForbiddenFlags(spec.Args, spec.Env); err != nil {
		return nil, nil, fmt.Errorf("handler: Launch: %w", err)
	}

	// Substrate dispatch: when spec.Substrate is non-nil, delegate subprocess
	// hosting to the substrate (e.g. a tmux window) instead of exec.CommandContext.
	// The substrate path does not wire HandlerSpec delivery or SpawnWatcher when
	// Stdout() is nil — the bridge wire is the daemon Unix socket in that case.
	//
	// When spec.Substrate is nil, the current exec.CommandContext path is preserved
	// (backward compatible; all existing callers are unaffected).
	//
	// Spec ref: process-lifecycle.md §4.7 PL-021b; handler-contract.md HC-054.
	if spec.Substrate != nil {
		return h.launchViaSubstrate(ctx, sessionID, spec)
	}

	//nolint:gosec // G204: Binary is daemon-config-resolved; not user-controlled
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Dir = spec.WorkDir
	cmd.Env = spec.Env
	cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr(lifecycle.RecordedPGID())

	// Resolve runID for the lifecycle Machine: use HandlerSpec.RunID when
	// available; fall back to "unknown" for the legacy/test path.
	runIDStr := "unknown"
	if spec.HandlerSpec != nil {
		runIDStr = spec.HandlerSpec.RunID.String()
	}

	sess, err := newSessionWithIDs(ctx, cmd, string(sessionID), runIDStr)
	if err != nil {
		return nil, nil, fmt.Errorf("handler: Launch: NewSession: %w", err)
	}

	// HC-005: if a HandlerSpec is provided, encode it as compact JSON and write
	// it to subprocess stdin followed by a newline, then close the write end so
	// the subprocess sees EOF after reading exactly one JSON object. The delivery
	// runs in a goroutine bounded by ctx so that a slow subprocess cannot block
	// Launch indefinitely.
	if spec.HandlerSpec != nil {
		hs := spec.HandlerSpec
		go func() {
			// MarshalLaunchSpec validates the spec and returns compact JSON.
			// Validation or encoding failure is a programmer error; log and
			// close stdin so the subprocess sees EOF rather than hanging.
			encoded, encErr := handlercontract.MarshalLaunchSpec(hs)
			if encErr != nil {
				fmt.Fprintf(os.Stderr, "handler: Launch: MarshalLaunchSpec: %v\n", encErr)
				_ = sess.CloseStdin()
				return
			}
			// SendInput writes the compact JSON line + '\n' (NDJSON framing).
			// ctx bounds the write: if ctx is cancelled the subprocess stdin
			// pipe will return an error and the goroutine exits.
			if writeErr := sess.SendInput(ctx, string(encoded)); writeErr != nil {
				// Subprocess may have already exited; log and continue to close.
				fmt.Fprintf(os.Stderr, "handler: Launch: stdin write: %v\n", writeErr)
			}
			if closeErr := sess.CloseStdin(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "handler: Launch: CloseStdin: %v\n", closeErr)
			}
		}()
	}

	// Apply optional StdoutWrapper before wiring to SpawnWatcher (CHB-023).
	// When StdoutWrapper is nil the raw pipe is used directly (no-op for existing callers).
	progressStream := io.Reader(sess.Stdout())
	if spec.StdoutWrapper != nil {
		progressStream = spec.StdoutWrapper(progressStream)
	}

	watcher := handlercontract.SpawnWatcher(ctx, handlercontract.SpawnWatcherConfig{
		SessionID:      sessionID,
		ProgressStream: progressStream,
		Publisher:      h.publisher,
		DeadLetter:     h.deadLetter,
		Machine:        sess.Machine(),
	})

	return sess, watcher, nil
}

// launchViaSubstrate handles the non-nil Substrate path in Launch.
//
// It builds a SubstrateSpawn from spec and calls Substrate.SpawnWindow. The
// returned SubstrateSession is wrapped in a substrateSessionAdapter so it
// satisfies the Session interface. SpawnWatcher is wired only when
// SubstrateSession.Stdout() returns a non-nil io.Reader; for tmux-hosted
// sessions the bridge wire is the daemon Unix socket, so Stdout() returns nil
// and the watcher is nil (the caller uses HookSessionStore.WaitForOutcome
// for completion detection instead).
//
// HandlerSpec delivery is skipped for substrate sessions: the pty stdin is
// owned by the substrate (tmux) and the LaunchSpec is injected via env vars
// (CHB-006) or the hook-bridge socket instead.
//
// Spec ref: process-lifecycle.md §4.7 PL-021b.
func (h *handler) launchViaSubstrate(ctx context.Context, sessionID handlercontract.SessionID, spec LaunchSpec) (Session, *handlercontract.Watcher, error) {
	argv := append([]string{spec.Binary}, spec.Args...)
	spawn := SubstrateSpawn{
		WindowName: spec.WorkDir, // caller overrides via Substrate.SpawnWindow; opaque to handler
		Cwd:        spec.WorkDir,
		Env:        spec.Env,
		Argv:       argv,
	}

	subSess, err := spec.Substrate.SpawnWindow(ctx, spawn)
	if err != nil {
		return nil, nil, fmt.Errorf("handler: Launch: Substrate.SpawnWindow: %w", err)
	}

	// Resolve runID for the lifecycle Machine (same logic as the exec path).
	subRunIDStr := "unknown"
	if spec.HandlerSpec != nil {
		subRunIDStr = spec.HandlerSpec.RunID.String()
	}
	adapted := newSubstrateAdapter(subSess, string(sessionID), subRunIDStr)

	// Wire SpawnWatcher only when the substrate exposes a stdout pipe.
	// For tmux-hosted sessions Stdout() returns nil; in that case return a
	// nil watcher — callers use HookSessionStore.WaitForOutcome instead.
	stdout := subSess.Stdout()
	if stdout == nil {
		return adapted, nil, nil
	}

	progressStream := io.Reader(stdout)
	if spec.StdoutWrapper != nil {
		progressStream = spec.StdoutWrapper(progressStream)
	}

	watcher := handlercontract.SpawnWatcher(ctx, handlercontract.SpawnWatcherConfig{
		SessionID:      sessionID,
		ProgressStream: progressStream,
		Publisher:      h.publisher,
		DeadLetter:     h.deadLetter,
		Machine:        adapted.Machine(),
	})

	return adapted, watcher, nil
}
