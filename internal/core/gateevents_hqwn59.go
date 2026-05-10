package core

import "github.com/google/uuid"

// gateevents_hqwn59.go — event-bus payload types for §8.2.4-§8.2.6 gate-lifecycle
// events: gate_allowed, gate_denied, gate_escalated.
//
// These are DISTINCT from GatePayload (specs/control-points.md §6.1.1), which
// is the configuration payload embedded in a ControlPoint. The types in this
// file are the event-bus wire payloads emitted on the cross-subsystem bus when
// a gate evaluation produces a verdict.
//
// Per event-model.md §8.9(h) note: gate verdicts are terminal-distinct outcomes,
// NOT sequential phases of the same lifecycle, and MUST remain as three separate
// event types (gate_allowed, gate_denied, gate_escalated). This is the correct
// model per control-points §6.5.
//
// Spec ref: specs/event-model.md §8.2.4, §8.2.5, §8.2.6.
// Bead refs: hk-hqwn.59.15, hk-hqwn.59.16, hk-hqwn.59.17.

// GateAllowedPayload is the typed event payload for the gate_allowed event
// (event-model.md §8.2.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Emitted by orchestrator-core when a gate evaluation produces an ALLOW verdict.
//
// # Payload fields (event-model.md §8.2.4)
//
//   - run_id      — the run whose gate was evaluated (required)
//   - gate_name   — the registered gate name (required)
//   - reason      — optional human-readable explanation for the allow verdict
type GateAllowedPayload struct {
	// RunID is the run whose gate was evaluated. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// GateName is the registered gate name (control-points.md §6.2).
	// Required (non-empty). Typed as GateRef since gate names are gate_ref values.
	GateName GateRef `json:"gate_name"`

	// Reason is an optional human-readable explanation for the allow verdict.
	// Corresponds to reason? in event-model.md §8.2.4. Nil when omitted;
	// non-nil must be non-empty.
	Reason *string `json:"reason,omitempty"`
}

// Valid reports whether p is a well-formed GateAllowedPayload.
//
// Rules per event-model.md §8.2.4:
//   - RunID must not be uuid.Nil.
//   - GateName must be a valid (non-empty) GateRef.
//   - Reason, when non-nil, must be non-empty.
func (p GateAllowedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.GateName.Valid() {
		return false
	}
	if p.Reason != nil && *p.Reason == "" {
		return false
	}
	return true
}

// GateDeniedPayload is the typed event payload for the gate_denied event
// (event-model.md §8.2.5).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability, audit, and reconciliation input).
//
// Emitted by orchestrator-core when a gate evaluation produces a DENY verdict.
// The reason field is required for gate_denied (unlike gate_allowed where it is
// optional) to ensure consumers can always determine why a gate was denied.
//
// # Payload fields (event-model.md §8.2.5)
//
//   - run_id    — the run whose gate was denied (required)
//   - gate_name — the registered gate name (required)
//   - reason    — human-readable explanation for the deny verdict (required)
type GateDeniedPayload struct {
	// RunID is the run whose gate was denied. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// GateName is the registered gate name. Required (non-empty GateRef).
	GateName GateRef `json:"gate_name"`

	// Reason is a human-readable explanation for the deny verdict.
	// Required (non-empty) per event-model.md §8.2.5 (reason is required
	// for gate_denied, unlike the optional reason in gate_allowed / gate_escalated).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed GateDeniedPayload.
//
// Rules per event-model.md §8.2.5:
//   - RunID must not be uuid.Nil.
//   - GateName must be a valid (non-empty) GateRef.
//   - Reason must be non-empty.
func (p GateDeniedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.GateName.Valid() {
		return false
	}
	if p.Reason == "" {
		return false
	}
	return true
}

// GateEscalatedPayload is the typed event payload for the gate_escalated event
// (event-model.md §8.2.6).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and operator-facing audit).
//
// Emitted by orchestrator-core when a gate evaluation escalates to operator
// attention (e.g., an approval-gate that cannot be auto-resolved). The escalation
// triggers an operator_escalation_required event downstream per §8.6.9.
//
// # Payload fields (event-model.md §8.2.6)
//
//   - run_id    — the run whose gate was escalated (required)
//   - gate_name — the registered gate name (required)
//   - reason    — optional human-readable explanation for the escalation
type GateEscalatedPayload struct {
	// RunID is the run whose gate was escalated. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// GateName is the registered gate name. Required (non-empty GateRef).
	GateName GateRef `json:"gate_name"`

	// Reason is an optional human-readable explanation for the escalation.
	// Corresponds to reason? in event-model.md §8.2.6. Nil when omitted;
	// non-nil must be non-empty.
	Reason *string `json:"reason,omitempty"`
}

// Valid reports whether p is a well-formed GateEscalatedPayload.
//
// Rules per event-model.md §8.2.6:
//   - RunID must not be uuid.Nil.
//   - GateName must be a valid (non-empty) GateRef.
//   - Reason, when non-nil, must be non-empty.
func (p GateEscalatedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.GateName.Valid() {
		return false
	}
	if p.Reason != nil && *p.Reason == "" {
		return false
	}
	return true
}
