package handlercontract

import (
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/core"
)

// Session represents a single instantiation of an agent subprocess produced
// by a Handler.Launch call (specs/handler-contract.md §6.1, HC-002).
//
// The session object's lifetime begins with Launch return and ends when Wait
// returns. All methods MUST be safe to call from any goroutine.
//
// Typed errors returned by Session methods are the sentinel classes declared in
// sentinels.go (ErrTransient, ErrStructural, ErrDeterministic, ErrCanceled,
// ErrBudget) or their structural sub-sentinels.
type Session interface {
	// ID returns the stable identifier for this session, assigned at Launch
	// return (specs/handler-contract.md §6.1).
	//
	// The value is daemon-assigned (unique within a daemon generation) and is
	// carried on every handler-lifecycle event in the session's lifetime.
	ID() core.SessionID

	// SendInput delivers input to the running agent subprocess.
	//
	// Returns a typed sentinel (ErrTransient, ErrStructural, ErrCanceled, etc.)
	// on failure. Context cancellation is honoured; callers SHOULD propagate ctx
	// from the enclosing operation.
	SendInput(ctx context.Context, input string) error

	// Attach returns a reader over the session's tmux or log tail for
	// operator-facing observability (specs/handler-contract.md §6.1).
	//
	// The returned io.Reader streams live session output; callers are responsible
	// for draining and closing the reader. Returns a typed sentinel on failure.
	Attach(ctx context.Context) (io.Reader, error)

	// Kill signals the subprocess to exit.
	//
	// Safe to call multiple times; subsequent calls after the subprocess has
	// already exited MUST be no-ops. Context cancellation is honoured.
	// Returns a typed sentinel on failure.
	Kill(ctx context.Context) error

	// Wait blocks until the subprocess terminates and returns the Outcome from
	// the final outcome_emitted event (specs/handler-contract.md §6.1,
	// HC-002, HC-008).
	//
	// Safe to call multiple times; subsequent calls MUST return the same result
	// as the first. On subprocess crash or missing outcome_emitted delivery,
	// Wait returns a typed sentinel error (ErrStructural) and a zero Outcome.
	Wait(ctx context.Context) (core.Outcome, error)

	// LogLocation returns the absolute session-log path emitted in the
	// session_log_location progress-stream message
	// (specs/handler-contract.md §4.2.HC-010, §6.1).
	//
	// The returned path is set before agent_ready fires; callers that invoke
	// LogLocation before that event may receive an empty string.
	LogLocation() string
}
