// Package handler defines the handler-contract types for the harmonik daemon.
// It declares the sentinel error classes (HC-020) that every subsystem boundary
// must use for error classification and routing.
package handler

import (
	"errors"
	"fmt"
)

// ErrTransient is the primary sentinel for transient handler failures.
// A transient failure is one that may succeed on retry without any plan change.
// Examples: rate-limiting, socket-level I/O errors on first occurrence (HC-024a),
// and transient skill-provisioning failures resolved via retry-with-backoff
// (HC-048a).
//
// Usage: every error returned across a subsystem boundary MUST wrap exactly one
// primary sentinel via fmt.Errorf("...: %w", ErrXxx).  Consumers MUST use
// errors.Is or errors.As for class detection — string matching is forbidden
// per HC-020 (specs/handler-contract.md §4.5).
var ErrTransient = errors.New("handler: transient error")

// ErrStructural is the primary sentinel for structural handler failures.
// A structural failure means the plan cannot proceed as specified; a different
// plan (different handler_ref, different required_skills, etc.) might succeed.
// Silent-hang (HC-026), protocol-mismatch (HC-021), and unresolvable
// skill-injection failures (HC-048) all route through this class.
//
// ErrProtocolMismatch and ErrSkillProvisioningFailed are structural sub-sentinels
// that wrap ErrStructural (see below).  errors.Is(err, ErrStructural) returns
// true for both of those sub-sentinels.
//
// Usage: wrap via fmt.Errorf("...: %w", ErrStructural) or via a sub-sentinel.
// Consumers MUST use errors.Is / errors.As — string matching is forbidden
// per HC-020 (specs/handler-contract.md §4.5).
var ErrStructural = errors.New("handler: structural error")

// ErrDeterministic is the primary sentinel for deterministic handler failures.
// A deterministic failure will produce the same outcome on any retry; retrying
// the same plan is futile.  The orchestrator MUST NOT retry deterministic
// failures without a plan change.
//
// Usage: wrap via fmt.Errorf("...: %w", ErrDeterministic).
// Consumers MUST use errors.Is / errors.As — string matching is forbidden
// per HC-020 (specs/handler-contract.md §4.5).
var ErrDeterministic = errors.New("handler: deterministic error")

// ErrCanceled is the primary sentinel for canceled handler sessions.
// A session is canceled when an operator or orchestrator policy explicitly
// terminates it before a natural outcome is reached (e.g., drain, budget
// ceiling, explicit cancel signal).
//
// Usage: wrap via fmt.Errorf("...: %w", ErrCanceled).
// Consumers MUST use errors.Is / errors.As — string matching is forbidden
// per HC-020 (specs/handler-contract.md §4.5).
var ErrCanceled = errors.New("handler: canceled")

// ErrBudget is the primary sentinel for handler sessions that exhaust their
// token, time, or cost budget before reaching a natural outcome.  Unlike
// ErrCanceled (operator-initiated), ErrBudget signals a policy ceiling
// reached autonomously.
//
// Usage: wrap via fmt.Errorf("...: %w", ErrBudget).
// Consumers MUST use errors.Is / errors.As — string matching is forbidden
// per HC-020 (specs/handler-contract.md §4.5).
var ErrBudget = errors.New("handler: budget exhausted")

// ErrProtocolMismatch is a structural sub-sentinel that wraps ErrStructural.
// It is emitted on version-negotiation failure (HC-009), on absence of
// handler_capabilities within 5 s, or on an NDJSON line exceeding the HC-007a
// size cap.  Because it wraps ErrStructural, errors.Is(err, ErrStructural)
// returns true for any error that wraps ErrProtocolMismatch — callers that
// need handler-specific routing MUST check the sub-sentinel first
// (narrowest-first dispatch per HC-020).
//
// The retry policy MUST distinguish protocol-mismatch re-plans from transient
// re-plans to avoid retry-spin against the same pinned binary (HC-021,
// specs/handler-contract.md §4.5).
//
// errors.Is(err, ErrProtocolMismatch) == true  → sub-sentinel matched
// errors.Is(err, ErrStructural)       == true  → structural routing applies
var ErrProtocolMismatch = fmt.Errorf("handler: protocol mismatch: %w", ErrStructural)

// ErrSkillProvisioningFailed is a structural sub-sentinel that wraps ErrStructural.
// It is emitted on skill-injection structural failure (HC-048): the plan cannot
// proceed as specified because a required skill could not be provisioned, but a
// different plan (e.g., a node with different required_skills) might succeed.
// Callers that need skill-specific failure reporting MUST check this sub-sentinel
// before checking ErrStructural (narrowest-first dispatch per HC-020).
//
// Transient provisioning failures (HC-048a, resolved via retry-with-backoff) do
// NOT wrap this sentinel — they wrap ErrTransient directly.
//
// errors.Is(err, ErrSkillProvisioningFailed) == true  → sub-sentinel matched
// errors.Is(err, ErrStructural)              == true  → structural routing applies
var ErrSkillProvisioningFailed = fmt.Errorf("handler: skill provisioning failed: %w", ErrStructural)
