package core

import "github.com/google/uuid"

// StateEnteredPayload is the typed event payload for the state_entered event
// (event-model.md §8.1.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — observability stream).
//
// # Spec source (event-model.md §8.1.4)
//
// Emitted by orchestrator-core each time a run enters a new execution state
// (a position in the workflow graph). This event is the observability anchor
// for state-level timing and improvement-loop analysis.
//
// # Payload fields (event-model.md §8.1.4)
//
//   - run_id     — the run entering the state (required)
//   - state_id   — the StateID of the newly entered state (required)
//   - node_id    — the NodeID of the workflow node associated with this state (required)
//   - entered_at — RFC 3339 wall-clock timestamp at state entry (required)
type StateEnteredPayload struct {
	// RunID is the run that entered the state. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// StateID is the identifier of the newly entered state.
	// Required (must not be uuid.Nil).
	StateID StateID `json:"state_id"`

	// NodeID is the workflow node associated with this state
	// (execution-model.md §6.1 RECORD State). Required (non-empty).
	NodeID NodeID `json:"node_id"`

	// EnteredAt is the RFC 3339 wall-clock timestamp at state entry.
	// Required (non-empty). Kept as string to avoid silent timezone normalization
	// and JSON round-trip drift; mirrors RunCompletedPayload.EndedAt rationale.
	EnteredAt string `json:"entered_at"`
}

// Valid reports whether p is a well-formed StateEnteredPayload.
//
// Rules per event-model.md §8.1.4:
//   - RunID must not be uuid.Nil.
//   - StateID must not be uuid.Nil.
//   - NodeID must be non-empty.
//   - EnteredAt must be non-empty.
func (p StateEnteredPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.StateID) == uuid.Nil {
		return false
	}
	if p.NodeID == "" {
		return false
	}
	if p.EnteredAt == "" {
		return false
	}
	return true
}

// StateExitedPayload is the typed event payload for the state_exited event
// (event-model.md §8.1.5).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability stream).
//
// # Spec source (event-model.md §8.1.5)
//
// Emitted by orchestrator-core when a run exits a state (a position in the
// workflow graph). Paired with state_entered (§8.1.4); the two events bracket
// the time spent in each state. transition_id is present when the exit is the
// result of a forward or rollback transition; it is absent for cancellation
// or error cases where no TransitionRecord is created.
//
// # Payload fields (event-model.md §8.1.5)
//
//   - run_id         — the run exiting the state (required)
//   - state_id       — the StateID of the state being exited (required)
//   - node_id        — the NodeID of the workflow node associated with this state (required)
//   - exited_at      — RFC 3339 wall-clock timestamp at state exit (required)
//   - transition_id  — the TransitionID that caused the exit; absent when no
//     TransitionRecord is created (e.g., cancellation)
type StateExitedPayload struct {
	// RunID is the run that exited the state. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// StateID is the identifier of the state being exited.
	// Required (must not be uuid.Nil).
	StateID StateID `json:"state_id"`

	// NodeID is the workflow node associated with this state
	// (execution-model.md §6.1 RECORD State). Required (non-empty).
	NodeID NodeID `json:"node_id"`

	// ExitedAt is the RFC 3339 wall-clock timestamp at state exit.
	// Required (non-empty). Kept as string; mirrors StateEnteredPayload.EnteredAt rationale.
	ExitedAt string `json:"exited_at"`

	// TransitionID is the transition that caused the exit (event-model.md §8.1.5
	// transition_id?). Nil when no TransitionRecord is created (e.g., cancellation,
	// error-path exit). Non-nil must not be uuid.Nil.
	TransitionID *TransitionID `json:"transition_id,omitempty"`
}

// Valid reports whether p is a well-formed StateExitedPayload.
//
// Rules per event-model.md §8.1.5:
//   - RunID must not be uuid.Nil.
//   - StateID must not be uuid.Nil.
//   - NodeID must be non-empty.
//   - ExitedAt must be non-empty.
//   - TransitionID, when non-nil, must not be uuid.Nil.
func (p StateExitedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.StateID) == uuid.Nil {
		return false
	}
	if p.NodeID == "" {
		return false
	}
	if p.ExitedAt == "" {
		return false
	}
	if p.TransitionID != nil && uuid.UUID(*p.TransitionID) == uuid.Nil {
		return false
	}
	return true
}
