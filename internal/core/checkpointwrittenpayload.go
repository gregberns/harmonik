package core

import "github.com/google/uuid"

// CheckpointWrittenPayload is the typed event payload for the checkpoint_written
// event (event-model.md §8.1.7; execution-model.md §4.5.EM-025).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — the checkpoint_written event is emitted
// immediately after the checkpoint commit, which is itself an fsync boundary per
// execution-model.md §4.5.EM-025).
//
// # Spec source (event-model.md §8.1.7)
//
// Emitted by orchestrator-core after a checkpoint commit succeeds. Consumers
// (reconciliation, audit) use commit_hash to locate the checkpoint in git and
// verify the transition record and workspace state.
//
// # Payload fields (event-model.md §8.1.7 and §6.3 `checkpoint_written`)
//
//   - run_id         — the run that produced this checkpoint (required)
//   - state_id       — the StateID of the state checkpointed (required)
//   - transition_id  — the TransitionID of the transition committed (required)
//   - commit_hash    — the full git commit SHA of the checkpoint commit (required)
//   - bead_id        — the BeadID when the run is bead-tied (beads-integration.md
//     §4.3 BI-009); nil for standalone-input runs
type CheckpointWrittenPayload struct {
	// RunID is the run that produced this checkpoint. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// StateID is the StateID of the state that was checkpointed.
	// Required (must not be uuid.Nil).
	StateID StateID `json:"state_id"`

	// TransitionID is the transition committed in this checkpoint.
	// Required (must not be uuid.Nil).
	TransitionID TransitionID `json:"transition_id"`

	// CommitHash is the full git commit SHA of the checkpoint commit.
	// Required (non-empty). Consumers use this to locate the checkpoint via
	// git show per execution-model.md §4.5.EM-025.
	CommitHash string `json:"commit_hash"`

	// BeadID carries the bead identifier for bead-tied runs (beads-integration.md
	// §4.3 BI-009). Nil for standalone-input runs. Non-nil and non-empty for
	// bead-tied runs. Corresponds to bead_id? in event-model.md §8.1.7.
	BeadID *BeadID `json:"bead_id,omitempty"`
}

// Valid reports whether p is a well-formed CheckpointWrittenPayload.
//
// Rules per event-model.md §8.1.7 and §6.3:
//   - RunID must not be uuid.Nil.
//   - StateID must not be uuid.Nil.
//   - TransitionID must not be uuid.Nil.
//   - CommitHash must be non-empty.
//   - BeadID, when non-nil, must dereference to a non-empty value.
func (p CheckpointWrittenPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.StateID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.TransitionID) == uuid.Nil {
		return false
	}
	if p.CommitHash == "" {
		return false
	}
	if p.BeadID != nil && *p.BeadID == "" {
		return false
	}
	return true
}
