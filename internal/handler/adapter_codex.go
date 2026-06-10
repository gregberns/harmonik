// Package handler — CodexAdapter (codex-harness C5/T12, hk-xhawy).
//
// This file provides the concrete handlercontract.Adapter implementation for
// the "codex" agent type (core.AgentTypeCodex).  It is a stateless value that
// satisfies all Adapter callbacks and exposes a RegisterCodex helper for
// registration into an AdapterRegistry at daemon startup.
//
// Spec: specs/handler-contract.md §4.3.HC-013, §6.1.
// Bead: hk-xhawy [C5/T12].
package handler

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// CodexAdapter is the concrete handlercontract.Adapter for the "codex" agent type.
//
// It is a zero-value-usable struct.  Use RegisterCodex to add it to an
// AdapterRegistry.
//
// # No per-session state
//
// CodexAdapter holds no per-session state and spawns no goroutines.
type CodexAdapter struct{}

// NewCodexAdapter returns a CodexAdapter ready for use.
func NewCodexAdapter() handlercontract.Adapter {
	return CodexAdapter{}
}

// RegisterCodex adds a CodexAdapter into reg under core.AgentTypeCodex.
//
// Returns the error from AdapterRegistry.Register (duplicate registration,
// sealed registry, etc.) unchanged.  Callers MUST call RegisterCodex before
// the first ForAgent lookup (i.e., before daemon dispatching begins).
//
// DO NOT call from init() — explicit registration only.
//
// Spec: specs/handler-contract.md §4.3.HC-012, §4.3.HC-013.
func RegisterCodex(reg *handlercontract.AdapterRegistry) error {
	return reg.Register(core.AgentTypeCodex, NewCodexAdapter())
}

// ─────────────────────────────────────────────────────────────────────────────
// handlercontract.Adapter implementation
// ─────────────────────────────────────────────────────────────────────────────

// DetectReady reports whether event is the agent_ready signal for a codex session.
//
// Returns true ONLY when event.Type is "agent_ready".  MUST NOT return true
// for "launch_initiated" (HC-041 hard rule).
//
// codex has no distinct readiness handshake; the harmonik infrastructure emits
// a synthetic agent_ready event once the process start is confirmed.
func (CodexAdapter) DetectReady(event handlercontract.EventEnvelope) bool {
	if event.Type == handlercontract.ProgressMsgTypeLaunchInitiated {
		return false
	}
	return core.EventType(event.Type) == core.EventTypeAgentReady
}

// DetectRateLimit reports whether event signals a rate-limit condition for a
// codex session.
//
// codex does not surface rate-limit events via the harmonik event envelope;
// returns (false, 0) for all event types.
func (CodexAdapter) DetectRateLimit(_ handlercontract.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

// CleanExitSequence performs orderly termination of a codex session.
//
// codex self-terminates on turn completion (CompletionProcessExit), so under
// normal operation the session has already exited by the time this is called.
// Kill is idempotent and best-effort; a nil session is a no-op.
func (CodexAdapter) CleanExitSequence(ctx context.Context, sess handlercontract.Session) error {
	if sess == nil {
		return nil
	}
	return sess.Kill(ctx)
}

// RotateAccount returns ErrDeterministic: account rotation is not supported for
// the codex harness.
func (CodexAdapter) RotateAccount(_ context.Context) error {
	return ErrDeterministic
}

// Diagnose returns ErrDeterministic: adapter-specific diagnostics are not
// supported for the codex harness.
func (CodexAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, ErrDeterministic
}
