package core

// lifecycletransitionpayload_hxrygh.go — event-bus payload type for the
// lifecycle_transition event (event-model.md §8.3.14; handler-contract.md §4.13).
//
// Emitted by the watcher goroutine on every LifecycleState machine transition
// per HC-064..HC-067. Class O (ordinary — reconstructible from the per-session
// transition-history ring in the daemon's in-memory state per HC-067).
//
// Spec ref: event-model.md §8.3.14; handler-contract.md §4.13 HC-064..HC-067.
// Bead ref: hk-xrygh.

// LifecycleTransitionPayload is the typed event payload for the
// lifecycle_transition event (event-model.md §8.3.14).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — in-process transition-history ring is the
// authoritative surface per HC-067; the event is the durable cross-bus record).
//
// Emitted by the watcher goroutine and daemon workloop on every LifecycleState
// machine transition per handler-contract.md §4.13 HC-064..HC-067. The
// from_state and to_state values are LifecycleState.String() labels.
//
// # Payload fields (event-model.md §8.3.14)
//
//   - session_id      — harmonik session_id for correlation (non-empty)
//   - from_state      — LifecycleState label before the transition (e.g. "Spawning")
//   - to_state        — LifecycleState label after the transition (e.g. "Initializing")
//   - reason          — TransitionReason string from HC-065 (e.g. "spawn_started")
//   - transitioned_at — RFC 3339 wall-clock at the transition instant
//   - err_code        — optional error code; non-empty only when to_state == "Failed"
//   - err_msg         — optional error message; non-empty only when to_state == "Failed"
type LifecycleTransitionPayload struct {
	// SessionID is the harmonik session_id for correlation. Required (non-empty).
	SessionID SessionID `json:"session_id"`

	// FromState is the LifecycleState.String() label of the state before the
	// transition (e.g. "Spawning", "Initializing", "Ready"). Required (non-empty).
	FromState string `json:"from_state"`

	// ToState is the LifecycleState.String() label of the state after the
	// transition (e.g. "Initializing", "Ready", "Failed"). Required (non-empty).
	ToState string `json:"to_state"`

	// Reason is the TransitionReason string from HC-065 (e.g. "spawn_started",
	// "init_complete", "silent_hang"). Required (non-empty).
	Reason string `json:"reason"`

	// TransitionedAt is the RFC 3339 wall-clock timestamp at the transition
	// instant. Required (non-empty).
	TransitionedAt string `json:"transitioned_at"`

	// ErrCode is the optional error code. Non-empty only when ToState == "Failed".
	ErrCode string `json:"err_code,omitempty"`

	// ErrMsg is the optional error message. Non-empty only when ToState == "Failed".
	ErrMsg string `json:"err_msg,omitempty"`
}

// Valid reports whether p is a well-formed LifecycleTransitionPayload.
//
// Rules per event-model.md §8.3.14:
//   - SessionID must be non-empty.
//   - FromState must be non-empty.
//   - ToState must be non-empty.
//   - Reason must be non-empty.
//   - TransitionedAt must be non-empty.
func (p LifecycleTransitionPayload) Valid() bool {
	if p.SessionID == "" {
		return false
	}
	if p.FromState == "" {
		return false
	}
	if p.ToState == "" {
		return false
	}
	if p.Reason == "" {
		return false
	}
	if p.TransitionedAt == "" {
		return false
	}
	return true
}
