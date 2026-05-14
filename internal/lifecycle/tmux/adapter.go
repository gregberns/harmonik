package tmux

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// ──────────────────────────────────────────────────────────────────────────────
// Phase
// ──────────────────────────────────────────────────────────────────────────────

// Phase identifies the agent-session phase that the tmux window hosts.
// Mirrors the HARMONIK_PHASE env-var values defined in claude-hook-bridge.md §4.2.
//
// Spec ref: process-lifecycle.md §4.5 PL-021b; workspace-model.md §4.1 WM-002a
// (window-name derivation uses phase to form the deterministic window name).
type Phase string

const (
	// PhaseSingle is the single-mode workflow (no review loop).
	PhaseSingle Phase = "single"

	// PhaseImplementerInitial is the first implementer turn in a review loop.
	PhaseImplementerInitial Phase = "implementer-initial"

	// PhaseImplementerResume is a resumed implementer turn (--resume flag path).
	PhaseImplementerResume Phase = "implementer-resume"

	// PhaseReviewer is the reviewer turn in a review loop.
	PhaseReviewer Phase = "reviewer"
)

// ──────────────────────────────────────────────────────────────────────────────
// WindowHandle
// ──────────────────────────────────────────────────────────────────────────────

// WindowHandle is an opaque reference to a live tmux window returned by
// [Adapter.NewWindowIn]. Callers pass it to [Adapter.KillWindow] and
// [Adapter.WindowPanePID].
//
// The internal representation is "session:window-index" in tmux notation, but
// callers MUST treat it as an opaque string — the format may change when the
// OSAdapter is extended to support multiple panes (post-MVH).
type WindowHandle string

// ──────────────────────────────────────────────────────────────────────────────
// Outcome
// ──────────────────────────────────────────────────────────────────────────────

// Outcome carries the result of a [Adapter.NewWindowIn] call.
//
// On success, Handle is the opaque reference to the created window and Err is
// nil. On failure, Handle is empty and Err wraps one of the error sentinels
// declared in this package.
type Outcome struct {
	Handle WindowHandle
	Err    error
}

// ──────────────────────────────────────────────────────────────────────────────
// NewWindowIn input
// ──────────────────────────────────────────────────────────────────────────────

// NewWindowIn carries the parameters for [Adapter.NewWindowIn].
type NewWindowIn struct {
	// Session is the name of the existing tmux session in which the new window
	// will be created. MUST be non-empty.
	Session string

	// WindowName is the deterministic name to assign the new window.
	// Derived by [WindowName] (hk-gql20.8). MUST be non-empty.
	//
	// Spec ref: workspace-model.md §4.1 WM-002a.
	WindowName string

	// Env is the set of "KEY=VALUE" environment variables to inject into the
	// window's pane via tmux -e flags (requires tmux ≥ 3.0).
	// May be nil or empty.
	Env []string

	// WorkDir is the starting directory for the window's pane. When non-empty,
	// tmux new-window is called with -c <WorkDir>.
	WorkDir string

	// Command is the command to run inside the new window (the shell command
	// passed as the trailing argument to tmux new-window). When empty the
	// default shell is used.
	Command string
}

// ──────────────────────────────────────────────────────────────────────────────
// Adapter interface
// ──────────────────────────────────────────────────────────────────────────────

// bufferNameRe is the compiled regex for validating tmux buffer names used by
// the daemon pane-write mechanism. The required format is:
//
//	harmonik-<session-id>-<purpose>
//
// where <session-id> and <purpose> are lowercase alphanumeric slugs with
// optional internal hyphens.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d — buffer-name discipline.
var bufferNameRe = regexp.MustCompile(`^harmonik-[a-z0-9-]+-[a-z0-9-]+$`)

// Adapter is the tmux window-management interface consumed by the daemon and
// the handler launch path.
//
// Production implementations (hk-gql20.7) delegate to the tmux binary via
// exec.CommandContext. Test implementations inject a deterministic fake.
//
// All methods MUST be safe for concurrent use.
//
// Spec ref: process-lifecycle.md §4.5 PL-021b — "ntm adapter consumes
// process/tmux surface only"; the window operations exposed here are that
// surface. PL-021c — window-level orphan sweep consumes [ListWindows] and
// [KillWindow].
type Adapter interface {
	// ProbeTmux checks whether tmux is present and meets the minimum version
	// requirement (≥ 3.0 for -e env-injection). Returns [ErrTmuxMissing] when
	// the tmux binary is absent from PATH.
	//
	// The daemon MUST call ProbeTmux at startup (PL-021b absence-detection path)
	// before dispatching any session that requires a tmux window.
	ProbeTmux(ctx context.Context) error

	// ListSessions returns the names of all live tmux sessions. When tmux is
	// not running or has no sessions, it returns (nil, nil) — not an error.
	ListSessions(ctx context.Context) ([]string, error)

	// ListWindows returns the names of all windows in the named session. Returns
	// [ErrNoSession] when the session does not exist.
	//
	// Spec ref: process-lifecycle.md §4.5 PL-021c — window-level orphan sweep
	// enumerates windows per session.
	ListWindows(ctx context.Context, session string) ([]string, error)

	// NewWindowIn creates a new named tmux window inside the session described
	// by params.Session using the name params.WindowName. Returns an [Outcome]
	// carrying the [WindowHandle] on success.
	//
	// Returns [ErrNoSession] when params.Session does not exist.
	// Returns [ErrWindowCollision] when a window named params.WindowName already
	// exists in the session.
	// Returns [*ErrTmuxFailure] on tmux invocation errors.
	//
	// Spec ref: workspace-model.md §4.1 WM-002a (window-name derivation is the
	// caller's responsibility; this method enforces the creation contract).
	NewWindowIn(ctx context.Context, params NewWindowIn) Outcome

	// KillWindow destroys the window identified by handle. Returns nil if the
	// window has already been destroyed (idempotent).
	//
	// Returns [ErrNoSession] when the session referenced by the handle does not
	// exist.
	// Returns [*ErrTmuxFailure] on unexpected tmux errors.
	//
	// Spec ref: process-lifecycle.md §4.5 PL-021c — orphan sweep calls KillWindow
	// for each matched window.
	KillWindow(ctx context.Context, handle WindowHandle) error

	// WindowPanePID returns the PID of the process running in the first pane of
	// the window identified by handle. Used by the orphan sweep to cross-check
	// whether the underlying process has exited.
	//
	// Returns [ErrNoSession] when the session is gone.
	// Returns [*ErrTmuxFailure] when display-message fails.
	WindowPanePID(ctx context.Context, handle WindowHandle) (int, error)

	// KillSession destroys the named tmux session and all windows it contains.
	// Returns nil if the session has already been destroyed (idempotent).
	//
	// Returns [ErrNoSession] when the session does not exist (already gone).
	// Returns [*ErrTmuxFailure] on unexpected tmux errors.
	//
	// Spec ref: process-lifecycle.md §4.2 PL-006 — session-level orphan sweep
	// kills each matching session via tmux kill-session.
	KillSession(ctx context.Context, sessionName string) error

	// LoadBuffer loads payload into the named tmux buffer via
	// `tmux load-buffer -b <bufferName> -` (reading from stdin).
	//
	// bufferName MUST match the format `harmonik-<session-id>-<purpose>`
	// (validated by [bufferNameRe]); returns [ErrStructural] on malformed input.
	//
	// Callers MUST follow LoadBuffer with [PasteBuffer] (preferred) or delete
	// the buffer manually to avoid buffer accumulation. For full audit compliance
	// with the daemon_pane_write structured-log event use [OSAdapter.WriteToPane],
	// which combines load, paste, and logging in one call.
	//
	// Spec ref: process-lifecycle.md §4.7 PL-021d — daemon→pane write mechanism.
	LoadBuffer(ctx context.Context, bufferName string, payload []byte) error

	// PasteBuffer pastes the named buffer into paneTarget and deletes the buffer
	// atomically via `tmux paste-buffer -b <bufferName> -t <paneTarget> -d`.
	// The -d flag satisfies the cleanup obligation (PL-021d §cleanup) in one shot.
	//
	// bufferName MUST match the format `harmonik-<session-id>-<purpose>`
	// (validated by [bufferNameRe]); returns [ErrStructural] on malformed input.
	//
	// For full audit compliance (daemon_pane_write structured log with
	// payload_bytes) use [OSAdapter.WriteToPane] instead.
	//
	// Spec ref: process-lifecycle.md §4.7 PL-021d — daemon→pane write mechanism.
	PasteBuffer(ctx context.Context, bufferName, paneTarget string) error

	// SendKeysLiteral sends text literally to paneTarget via
	// `tmux send-keys -l -t <paneTarget> <text>`.
	//
	// This is the PL-021d fallback path: ONLY use for payloads strictly shorter
	// than 512 bytes with no newline characters. For all other payloads use
	// [LoadBuffer] + [PasteBuffer] (the preferred path per PL-021d). The bare
	// send-keys form (without -l) is FORBIDDEN for daemon-injected payloads
	// because it interprets shell metacharacters.
	//
	// Returns [ErrStructural] when text exceeds 512 bytes or contains a newline.
	//
	// Spec ref: process-lifecycle.md §4.7 PL-021d — send-keys -l fallback.
	SendKeysLiteral(ctx context.Context, paneTarget, text string) error
}

// ──────────────────────────────────────────────────────────────────────────────
// Error sentinels
// ──────────────────────────────────────────────────────────────────────────────

// ErrTmuxMissing is returned by [Adapter.ProbeTmux] when the tmux binary is
// absent from PATH. This corresponds to the PL-021a/PL-021b absence-detection
// path; the daemon MUST surface this as ON §8 exit code 22 (ntm-unavailable)
// when tmux is a required dependency.
var ErrTmuxMissing = errors.New("tmux: binary not found in PATH")

// ErrStructural is returned when a caller-supplied parameter violates a
// structural invariant enforced before any tmux invocation occurs. Examples:
// a buffer name that does not match the required harmonik- prefix format
// (PL-021d buffer-name discipline), or a [SendKeysLiteral] payload that
// exceeds 512 bytes or contains a newline.
//
// Callers can check for this sentinel with [errors.Is]:
//
//	if errors.Is(err, ErrStructural) { /* fix the caller */ }
var ErrStructural = errors.New("tmux: structural invariant violated")

// ErrNoSession is returned by window operations when the target tmux session
// does not exist. Callers that received the session name from [Adapter.ListSessions]
// and observe ErrNoSession SHOULD treat the session as a TOCTOU race and
// proceed without error.
var ErrNoSession = errors.New("tmux: session does not exist")

// ErrWindowCollision is returned by [Adapter.NewWindowIn] when a window with
// the requested name already exists in the target session. Callers MUST NOT
// create a second window with the same deterministic name — the collision
// indicates a programming error in the caller's state machine (a prior
// NewWindowIn was not tracked) or a daemon-restart with a stale session.
var ErrWindowCollision = errors.New("tmux: window name already exists in session")

// ErrTmuxFailure is returned when a tmux command exits with a non-zero status
// for a reason not covered by the typed sentinels above. It carries the raw
// tmux stderr output for operator diagnostics.
//
// Use [errors.As] to extract *ErrTmuxFailure from a wrapped error chain:
//
//	var tf *ErrTmuxFailure
//	if errors.As(err, &tf) {
//	    log.Printf("tmux stderr: %s", tf.Stderr)
//	}
type ErrTmuxFailure struct {
	// Op is the tmux subcommand that failed (e.g., "new-window", "kill-window").
	Op string

	// ExitCode is the process exit code returned by tmux.
	ExitCode int

	// Stderr is the combined stderr output from the tmux invocation.
	Stderr string
}

// Error implements the error interface.
func (e *ErrTmuxFailure) Error() string {
	return fmt.Sprintf("tmux: %s exited %d: %s", e.Op, e.ExitCode, e.Stderr)
}
