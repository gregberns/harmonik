// Package handler — PiAdapter (codename:pilot, PI-010/060/071).
//
// This file provides the concrete handlercontract.Adapter implementation for
// the "pi" agent type (core.AgentTypePi).  It is a stateless value that
// satisfies all Adapter callbacks and exposes a RegisterPi helper for
// registration into an AdapterRegistry at daemon startup.
//
// Spec: specs/pi-harness.md §1 (PI-010), §6 (PI-060), §7 (PI-071).
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.1/§3.7/§8.
// Bead: hk-ro1dr [PI-010/060/071].
package handler

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// PiAdapter is the concrete handlercontract.Adapter for the "pi" agent type.
//
// It is a zero-value-usable struct.  Use RegisterPi to add it to an
// AdapterRegistry.
//
// # No per-session state
//
// PiAdapter holds no per-session state and spawns no goroutines.
type PiAdapter struct{}

// NewPiAdapter returns a PiAdapter ready for use.
func NewPiAdapter() handlercontract.Adapter {
	return PiAdapter{}
}

// RegisterPi adds a PiAdapter into reg under core.AgentTypePi.
//
// Returns the error from AdapterRegistry.Register (duplicate registration,
// sealed registry, etc.) unchanged.  Callers MUST call RegisterPi before
// the first ForAgent lookup (i.e., before daemon dispatching begins).
//
// DO NOT call from init() — explicit registration only.
//
// Spec: specs/pi-harness.md §1 PI-010; §6 PI-060.
func RegisterPi(reg *handlercontract.AdapterRegistry) error {
	return reg.Register(core.AgentTypePi, NewPiAdapter())
}

// ─────────────────────────────────────────────────────────────────────────────
// handlercontract.Adapter implementation
// ─────────────────────────────────────────────────────────────────────────────

// DetectReady reports whether event is the agent_ready signal for a Pi session.
//
// Returns true ONLY when event.Type is "agent_ready".  MUST NOT return true
// for "launch_initiated" (HC-041 hard rule).
//
// Pi runs as CompletionProcessExit, so the shared loop bypasses the
// agent-ready wait (workloop.go ~3999).  DetectReady is nevertheless required
// to be HC-041-correct (enforced by adapterreadydetect_hc041_test).  PI-013.
func (PiAdapter) DetectReady(event handlercontract.EventEnvelope) bool {
	if event.Type == handlercontract.ProgressMsgTypeLaunchInitiated {
		return false
	}
	return core.EventType(event.Type) == core.EventTypeAgentReady
}

// DetectRateLimit reports whether event signals a rate-limit condition for a
// Pi session.
//
// PI-071 (UNCONFIRMED channel — confirm-by-test before wiring):
//
//	The intended signal source is Pi's NDJSON stdout: `auto_retry_start`/`_end`
//	events and/or error events carrying the HTTP status (findings.md §7).
//	That channel is UNCONFIRMED at Phase 0; the spec mandates confirmation
//	before implementation.  Until confirmed, returns (false, 0) for all events
//	(like the codex adapter), so no Pi rate-limit signal reaches the global
//	bandwidthtuner — correct fail-safe since PI-073 requires isolation anyway.
//
// When the channel IS confirmed, the classification per PI-071:
//   - minute-window 429 (retry-after = seconds/minutes): return (true, retryAfter)
//     so the caller applies backoff+retry with delay coupled to retry-after.
//   - day-window 429 (retry-after = hours): return (true, retryAfter) with a
//     large duration; the caller must fail the run fast and escalate — MUST NOT
//     idle to the 90-minute commitHardCeiling.
//   - magnitude unavailable (Pi swallows the status; only "a 429 happened" is
//     known): safe degradation — treat as fail-fast (return true, 0 or a sentinel
//     large duration).
//   - 404 / "no endpoints": transient; re-submit once then escalate.
//
// PI-073: Pi's rate-limit signal MUST be isolated from the global bandwidthtuner.
// The isolation point is per-queue backoff in the workloop, not this adapter.
func (PiAdapter) DetectRateLimit(_ handlercontract.EventEnvelope) (bool, time.Duration) {
	// UNCONFIRMED channel (findings.md §7, PI-071): return (false, 0) until the
	// Pi NDJSON rate-limit event shape is confirmed by test.
	return false, 0
}

// CleanExitSequence performs orderly termination of a Pi session.
//
// Pi is a ProcessExit harness — it nominally self-terminates on turn
// completion via the agent_end watcher (PiHarness.Teardown / PI-014).
// Kill is idempotent and best-effort; a nil session is a no-op.
func (PiAdapter) CleanExitSequence(ctx context.Context, sess handlercontract.Session) error {
	if sess == nil {
		return nil
	}
	return sess.Kill(ctx)
}

// RotateAccount returns ErrDeterministic: account rotation is not supported
// for the Pi harness in Phase 0.
func (PiAdapter) RotateAccount(_ context.Context) error {
	return ErrDeterministic
}

// Diagnose returns ErrDeterministic: adapter-specific diagnostics are not
// supported for the Pi harness in Phase 0.
func (PiAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, ErrDeterministic
}
