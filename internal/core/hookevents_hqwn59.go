package core

import "github.com/google/uuid"

// hookevents_hqwn59.go — event-bus payload types for §8.2.1-§8.2.3 hook-system
// lifecycle events: hook_fired, hook_failed, hook_verdict_persisted.
//
// These are DISTINCT from HookPayload (specs/control-points.md §6.1.2), which
// is the configuration payload embedded in a ControlPoint. The types in this
// file are the event-bus wire payloads emitted on the cross-subsystem bus.
//
// Spec ref: specs/event-model.md §8.2.1, §8.2.2, §8.2.3.
// Bead refs: hk-hqwn.59.12, hk-hqwn.59.13, hk-hqwn.59.14.

// HookFiredPayload is the typed event payload for the hook_fired event
// (event-model.md §8.2.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability stream).
//
// Emitted by the hook-system when a hook evaluator fires (begins execution).
// The triggering_event_id provides the causal link back to the event that
// caused the hook to fire (per event-model.md §6.1 note on triggering_event_id).
//
// # Payload fields (event-model.md §8.2.1)
//
//   - run_id                — the run in whose context the hook fired; absent for
//     run-scope-exempt hooks
//   - hook_name             — the registered hook name (HookName)
//   - triggering_event_id   — EventID of the event that triggered this hook
//   - side_effect_descriptor — the SideEffect descriptor the hook produced
type HookFiredPayload struct {
	// RunID is the run in whose context the hook fired.
	// Corresponds to run_id? in event-model.md §8.2.1. Nil for
	// run-scope-exempt hooks. Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// HookName is the registered hook name. Required (non-empty).
	HookName HookName `json:"hook_name"`

	// TriggeringEventID is the EventID of the event that caused this hook to
	// fire. Provides causal linkage per event-model.md §6.1. Required (must
	// not be uuid.Nil).
	TriggeringEventID EventID `json:"triggering_event_id"`

	// SideEffectDescriptor is the SideEffect record produced by the hook
	// evaluator. Required (must satisfy SideEffect.Valid()).
	SideEffectDescriptor SideEffect `json:"side_effect_descriptor"`
}

// Valid reports whether p is a well-formed HookFiredPayload.
//
// Rules per event-model.md §8.2.1:
//   - RunID, when non-nil, must not be uuid.Nil.
//   - HookName must be non-empty.
//   - TriggeringEventID must not be uuid.Nil.
//   - SideEffectDescriptor must satisfy SideEffect.Valid().
func (p HookFiredPayload) Valid() bool {
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.HookName == "" {
		return false
	}
	if uuid.UUID(p.TriggeringEventID) == uuid.Nil {
		return false
	}
	if !p.SideEffectDescriptor.Valid() {
		return false
	}
	return true
}

// HookFailedPayload is the typed event payload for the hook_failed event
// (event-model.md §8.2.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability, audit, and reconciliation input).
//
// Emitted by the hook-system when a hook evaluator fails. The error_category
// and reason fields carry the failure details.
//
// # Payload fields (event-model.md §8.2.2)
//
//   - run_id              — the run in whose context the hook failed; absent for
//     run-scope-exempt hooks
//   - hook_name           — the registered hook name
//   - triggering_event_id — EventID of the event that triggered the hook
//   - error_category      — handler-origin sentinel per handler-contract.md §4.5
//   - reason              — human-readable failure description
type HookFailedPayload struct {
	// RunID is the run in whose context the hook failed.
	// Corresponds to run_id? in event-model.md §8.2.2. Nil for
	// run-scope-exempt hooks. Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// HookName is the registered hook name. Required (non-empty).
	HookName HookName `json:"hook_name"`

	// TriggeringEventID is the EventID of the event that caused this hook to
	// run. Required (must not be uuid.Nil).
	TriggeringEventID EventID `json:"triggering_event_id"`

	// ErrorCategory is the failure sentinel per handler-contract.md §4.5.
	// Required (must be a valid ErrorCategory constant).
	ErrorCategory ErrorCategory `json:"error_category"`

	// Reason is a human-readable description of the failure.
	// Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed HookFailedPayload.
//
// Rules per event-model.md §8.2.2:
//   - RunID, when non-nil, must not be uuid.Nil.
//   - HookName must be non-empty.
//   - TriggeringEventID must not be uuid.Nil.
//   - ErrorCategory must be a valid declared constant.
//   - Reason must be non-empty.
func (p HookFailedPayload) Valid() bool {
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.HookName == "" {
		return false
	}
	if uuid.UUID(p.TriggeringEventID) == uuid.Nil {
		return false
	}
	if !p.ErrorCategory.Valid() {
		return false
	}
	if p.Reason == "" {
		return false
	}
	return true
}

// HookVerdictPersistedPayload is the typed event payload for the
// hook_verdict_persisted event (event-model.md §8.2.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — audit and observability; the authoritative
// verdict lives in the persisted HookVerdictRecord file per CP-037).
//
// Emitted by the hook-system after a HookVerdictRecord is persisted to the
// run's durable trace (control-points.md §4.7.CP-037). The verdict_path and
// commit_hash fields allow consumers to locate the persisted verdict in git.
//
// # Payload fields (event-model.md §8.2.3)
//
//   - run_id               — the run in whose context the verdict was persisted
//   - hook_invocation_id   — the opaque per-invocation identifier; string per
//     typed-alias-deferral (TODO: hoist to HookInvocationID)
//   - hook_name            — the registered hook name
//   - verdict_path         — the workspace-relative path to the persisted
//     HookVerdictRecord file
//   - commit_hash          — the git commit SHA that contains verdict_path
type HookVerdictPersistedPayload struct {
	// RunID is the run in whose context the verdict was persisted.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// HookInvocationID is the per-invocation identifier for this hook execution.
	// Corresponds to hook_invocation_id in event-model.md §8.2.3. Required (non-empty).
	//
	// TODO(hk-hqwn.59.14): hoist to typed HookInvocationID alias when that type
	// lands; currently plain string per the typed-alias-deferral pattern.
	HookInvocationID string `json:"hook_invocation_id"`

	// HookName is the registered hook name. Required (non-empty).
	HookName HookName `json:"hook_name"`

	// VerdictPath is the workspace-relative path of the persisted
	// HookVerdictRecord file (control-points.md §4.7.CP-037;
	// workspace-model.md §4.2). Required (non-empty).
	VerdictPath string `json:"verdict_path"`

	// CommitHash is the git commit SHA that contains VerdictPath.
	// Required (non-empty). Consumers use this to locate the verdict file via
	// git show.
	CommitHash string `json:"commit_hash"`
}

// Valid reports whether p is a well-formed HookVerdictPersistedPayload.
//
// Rules per event-model.md §8.2.3:
//   - RunID must not be uuid.Nil.
//   - HookInvocationID must be non-empty.
//   - HookName must be non-empty.
//   - VerdictPath must be non-empty.
//   - CommitHash must be non-empty.
func (p HookVerdictPersistedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.HookInvocationID == "" {
		return false
	}
	if p.HookName == "" {
		return false
	}
	if p.VerdictPath == "" {
		return false
	}
	if p.CommitHash == "" {
		return false
	}
	return true
}
