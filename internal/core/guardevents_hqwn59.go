package core

import "github.com/google/uuid"

// guardevents_hqwn59.go — event-bus payload types for §8.2.7-§8.2.8 guard-lifecycle
// events: guard_reordered, guard_failed.
//
// These are DISTINCT from GuardPayload (specs/control-points.md §6.1.3), which
// is the configuration payload embedded in a ControlPoint. The types in this
// file are the event-bus wire payloads emitted on the cross-subsystem bus when
// guard evaluation produces a reorder or failure outcome.
//
// Spec ref: specs/event-model.md §8.2.7, §8.2.8.
// Bead refs: hk-hqwn.59.18, hk-hqwn.59.19.

// GuardReorderedPayload is the typed event payload for the guard_reordered event
// (event-model.md §8.2.7).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability and audit).
//
// Emitted by orchestrator-core when a guard evaluator reorders the outgoing edge
// set. The edge_set_before and edge_set_after fields capture the edge ordering
// before and after the guard's reorder action; both are slices of edge label
// strings in execution order.
//
// # Payload fields (event-model.md §8.2.7)
//
//   - run_id           — the run whose guard reordered edges (required)
//   - guard_name       — the registered guard name (required)
//   - edge_set_before  — edge labels in execution order before reorder (required)
//   - edge_set_after   — edge labels in execution order after reorder (required)
type GuardReorderedPayload struct {
	// RunID is the run whose guard reordered edges. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// GuardName is the registered guard name (control-points.md §6.1.3).
	// Required (non-empty). Plain string per typed-alias-deferral — no GuardRef
	// typed alias exists at MVH.
	//
	// TODO(hk-hqwn.59.18): hoist to typed GuardRef alias when that type lands.
	GuardName string `json:"guard_name"`

	// EdgeSetBefore is the slice of outgoing edge labels in execution order
	// BEFORE the guard's reorder action. Required; may be empty slice (no
	// outgoing edges to reorder, which would be a no-op guard). The slice
	// preserves insertion order of the original edge set.
	//
	// TODO(hk-hqwn.59.18): type elements as EdgeLabel once that alias lands;
	// currently []string per the typed-alias-deferral pattern.
	EdgeSetBefore []string `json:"edge_set_before"`

	// EdgeSetAfter is the slice of outgoing edge labels in execution order
	// AFTER the guard's reorder action. Required; must have the same length as
	// EdgeSetBefore (a guard reorders, it does not add or remove edges).
	//
	// TODO(hk-hqwn.59.18): type elements as EdgeLabel once that alias lands.
	EdgeSetAfter []string `json:"edge_set_after"`
}

// Valid reports whether p is a well-formed GuardReorderedPayload.
//
// Rules per event-model.md §8.2.7:
//   - RunID must not be uuid.Nil.
//   - GuardName must be non-empty.
//   - EdgeSetBefore must be non-nil.
//   - EdgeSetAfter must be non-nil.
//   - EdgeSetAfter and EdgeSetBefore must have equal length (a guard may only
//     reorder, not add or remove edges).
func (p GuardReorderedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.GuardName == "" {
		return false
	}
	if p.EdgeSetBefore == nil {
		return false
	}
	if p.EdgeSetAfter == nil {
		return false
	}
	if len(p.EdgeSetBefore) != len(p.EdgeSetAfter) {
		return false
	}
	return true
}

// GuardFailedPayload is the typed event payload for the guard_failed event
// (event-model.md §8.2.8).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability, audit, and reconciliation input).
//
// Emitted by orchestrator-core when a guard evaluator fails (e.g., evaluator
// panic or a policy expression error). Consumers such as reconciliation use this
// event to detect evaluation failures that may need manual review.
//
// # Payload fields (event-model.md §8.2.8)
//
//   - run_id          — the run whose guard failed (required)
//   - guard_name      — the registered guard name (required)
//   - error_category  — handler-origin sentinel per handler-contract.md §4.5 (required)
//   - reason          — human-readable failure description (required)
type GuardFailedPayload struct {
	// RunID is the run whose guard failed. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// GuardName is the registered guard name. Required (non-empty).
	// TODO(hk-hqwn.59.19): hoist to typed GuardRef alias when that type lands.
	GuardName string `json:"guard_name"`

	// ErrorCategory is the failure sentinel per handler-contract.md §4.5.
	// Required (must be a valid ErrorCategory constant).
	ErrorCategory ErrorCategory `json:"error_category"`

	// Reason is a human-readable description of the failure.
	// Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed GuardFailedPayload.
//
// Rules per event-model.md §8.2.8:
//   - RunID must not be uuid.Nil.
//   - GuardName must be non-empty.
//   - ErrorCategory must be a valid declared constant.
//   - Reason must be non-empty.
func (p GuardFailedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.GuardName == "" {
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
