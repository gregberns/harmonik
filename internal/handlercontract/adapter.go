package handlercontract

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// Adapter is the per-agent-type callback surface the Agent Runner (S04) exposes
// to the watcher (specs/handler-contract.md §6.1, §4.3.HC-013).
//
// Every registered agent_type MUST have exactly one Adapter. The Adapter's
// methods are synchronous callbacks invoked by the watcher on specific
// lifecycle events. Adapters MUST NOT hold per-session state or spawn
// per-session goroutines; per-session state MUST live in the watcher's stack
// or closure (§4.3.HC-011).
//
// Adding a method to this surface requires a foundation amendment
// (§4.3.HC-013).
type Adapter interface {
	// DetectReady reports whether event is this session's agent_ready signal
	// (specs/handler-contract.md §6.1, §4.9.HC-041).
	//
	// Returns true exactly when the adapter has observed an agent_ready event
	// for the session in question. Adapters MUST NOT synthesize ready-state
	// from other signals (e.g., first output chunk).
	DetectReady(event core.EventEnvelope) bool

	// DetectRateLimit reports whether event signals a rate-limit condition
	// (specs/handler-contract.md §6.1, §4.6).
	//
	// Returns (limited, retry_after). A non-zero retry_after implies
	// limited=true. When limited=true the watcher MUST emit agent_rate_limited
	// carrying retry_after; the session is NOT a failure.
	DetectRateLimit(event core.EventEnvelope) (limited bool, retryAfter time.Duration)

	// CleanExitSequence performs orderly termination of session on normal
	// cancellation (specs/handler-contract.md §6.1, §4.4.HC-018).
	//
	// Called by the watcher when the enclosing context is cancelled (operator
	// stop or policy-driven cancellation). Implementations MUST propagate ctx
	// to any blocking operations. Returns a typed sentinel on failure.
	CleanExitSequence(ctx context.Context, session Session) error

	// RotateAccount rotates the provider account for this agent type
	// (specs/handler-contract.md §6.1, §4.3.HC-013a).
	//
	// MAY return ErrDeterministic if account rotation is not supported for
	// this agent type. MUST NOT be invoked mid-turn; the watcher schedules
	// rotation at the next clean turn boundary. If no quiescent boundary is
	// observed within the configurable window, implementations MUST return
	// ErrTransient.
	RotateAccount(ctx context.Context) error
}
