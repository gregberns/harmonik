// Package handler — ClaudeCodeAdapter (MVH-R8).
//
// This file provides the concrete handlercontract.Adapter implementation for
// the "claude-code" agent type (core.AgentTypeClaudeCode).  It is a stateless
// value that satisfies all four Adapter callbacks and exposes a Register helper
// for explicit registration into an AdapterRegistry at daemon startup.
//
// Spec: specs/handler-contract.md §4.3.HC-013, §4.6.HC-025, §4.9.HC-041.
// Bead: hk-prug5.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ErrSingleAccountOnly is returned by ClaudeCodeAdapter.RotateAccount to
// indicate that account rotation is not supported for the "claude-code"
// agent type in MVH.
//
// Account rotation is post-MVH; the interface stub exists so callers compile
// per the bead body. Callers SHOULD treat this as ErrDeterministic.
//
// Spec: specs/handler-contract.md §4.3.HC-013a.
// TODO: when multi-account rotation lands, remove this sentinel and wire in
// real rotation logic.
var ErrSingleAccountOnly = fmt.Errorf("handler: claude-code account rotation not supported in MVH: %w", ErrDeterministic)

// claudeCodeRateLimitedPayload is the minimal shape decoded from an
// agent_rate_limited progress-stream message to extract retry_after_seconds.
//
// The Claude Code headless protocol surfaces HTTP 429 responses as an
// agent_rate_limited NDJSON line carrying an optional retry_after_seconds
// field (handler-contract.md §4.6.HC-025).
type claudeCodeRateLimitedPayload struct {
	RetryAfterSeconds *int `json:"retry_after_seconds,omitempty"`
}

// ClaudeCodeAdapter is the concrete handlercontract.Adapter for the
// "claude-code" agent type (handlercontract.AgentTypeClaudeCode = "claude-code").
//
// It is a zero-value-usable struct; callers SHOULD use NewClaudeCodeAdapter
// for forward-compatibility but the zero value is equally valid.
//
// # No per-session state
//
// ClaudeCodeAdapter holds no per-session state and spawns no goroutines.
// Per-session state lives entirely in the watcher's stack or closure per
// HC-012 / HC-011.
//
// # Registration
//
// Use Register to add this adapter to an AdapterRegistry.  DO NOT use
// init() auto-registration — explicit registration only, called from
// daemon.Start later (per bead body: "out of scope until row #10").
//
// Spec: specs/handler-contract.md §4.3.HC-013, §6.1.
type ClaudeCodeAdapter struct{}

// NewClaudeCodeAdapter returns a ClaudeCodeAdapter ready for use.
//
// The returned value satisfies handlercontract.Adapter.
func NewClaudeCodeAdapter() handlercontract.Adapter {
	return ClaudeCodeAdapter{}
}

// Register adds a ClaudeCodeAdapter into reg under handlercontract.AgentTypeClaudeCode.
//
// Returns the error from AdapterRegistry.Register (duplicate registration,
// sealed registry, etc.) unchanged.  Callers MUST call Register before the
// first ForAgent lookup (i.e., before daemon dispatching begins).
//
// DO NOT call from init() — explicit registration only.
//
// Spec: specs/handler-contract.md §4.3.HC-012, §4.3.HC-013.
func Register(reg *handlercontract.AdapterRegistry) error {
	return reg.Register(handlercontract.AgentTypeClaudeCode, NewClaudeCodeAdapter())
}

// ─────────────────────────────────────────────────────────────────────────────
// handlercontract.Adapter implementation
// ─────────────────────────────────────────────────────────────────────────────

// DetectReady reports whether event is the agent_ready signal for a
// Claude Code session (handler-contract.md §4.9.HC-041).
//
// Returns true ONLY when event.Type is "agent_ready".  MUST NOT synthesize
// ready-state from any other signal (HC-041 hard rule).
func (ClaudeCodeAdapter) DetectReady(event handlercontract.EventEnvelope) bool {
	return event.Type == handlercontract.ProgressMsgTypeAgentReady
}

// DetectRateLimit reports whether event signals a rate-limit condition for a
// Claude Code session (handler-contract.md §4.6.HC-025).
//
// Returns (true, retryAfter) when event.Type is "agent_rate_limited".
// retryAfter is parsed from the retry_after_seconds field of the event
// payload; when absent or zero the duration is 0 (caller applies its own
// backoff policy).
//
// Returns (false, 0) for all other event types, including
// "agent_rate_limit_cleared".
func (ClaudeCodeAdapter) DetectRateLimit(event handlercontract.EventEnvelope) (bool, time.Duration) {
	if event.Type != handlercontract.ProgressMsgTypeAgentRateLimited {
		return false, 0
	}

	// Decode the optional retry_after_seconds from the payload.
	// Malformed payload: treat as limited with no explicit retry hint.
	var pl claudeCodeRateLimitedPayload
	if err := json.Unmarshal(event.Payload, &pl); err != nil {
		return true, 0
	}

	if pl.RetryAfterSeconds == nil || *pl.RetryAfterSeconds <= 0 {
		return true, 0
	}
	return true, time.Duration(*pl.RetryAfterSeconds) * time.Second
}

// CleanExitSequence performs orderly termination of a Claude Code session
// on normal cancellation (handler-contract.md §4.4.HC-018).
//
// Claude Code's headless protocol accepts a "/exit" line on stdin to request
// clean shutdown.  SendInput propagates ctx so any blocking write honours
// cancellation.
func (ClaudeCodeAdapter) CleanExitSequence(ctx context.Context, session handlercontract.Session) error {
	if err := session.SendInput(ctx, "/exit"); err != nil {
		return fmt.Errorf("handler: claude-code CleanExitSequence: %w", err)
	}
	return nil
}

// RotateAccount returns ErrSingleAccountOnly because account rotation is not
// supported for "claude-code" in MVH (handler-contract.md §4.3.HC-013a).
//
// This stub exists so callers compile.  Rotation is post-MVH.
func (ClaudeCodeAdapter) RotateAccount(_ context.Context) error {
	return ErrSingleAccountOnly
}
