package core

import "github.com/google/uuid"

// TransitionEventPayload is the event-bus payload for the transition_event event
// (event-model.md §8.1.6; execution-model.md §4.6.EM-028).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — the checkpoint that creates the
// TransitionRecord is a durability boundary per execution-model.md §4.5.EM-025).
//
// The transition event is a projection of the authoritative Transition record
// stored at .harmonik/transitions/<run_id>/<transition_id>.json (EM-018).
// It MUST NOT duplicate the full AlphaGo trace payload (EM-029): fields
// candidate_actions, evidence, and verifier_metrics live only in the sibling
// file and are deliberately absent from this type.
//
// # Projection contract (EM-028)
//
// The event payload cites the transition by transition_id, run_id, and the
// checkpoint commit hash so that any consumer can recover the full record via:
//
//	git show <CommitHash>:.harmonik/transitions/<RunID>/<TransitionID>.json
//
// This satisfies EM-019 (record discoverable by git-show) without requiring
// consumers to carry the full trace. Streaming consumers that need only
// event-level metadata MAY read the transition event from the bus (EM-030);
// consumers requiring complete audit fidelity MUST read the sibling file from
// git (EM-030).
//
// # Canonical vs projection authority
//
// TransitionEventPayload is observational only. It is NOT the authoritative
// record; the Transition sibling file is (EM-028). On any disagreement between
// an event payload field and the sibling file, the sibling file wins.
// Divergence between the two is a reconciliation signal per [reconciliation/
// spec.md §8.4 Cat 3] and EM-INV-005.
//
// # §6.3 schema fields (event-model.md §6.3 `transition_event`)
//
// All six fields are required:
//   - run_id          — the run containing this transition
//   - transition_id   — the UUIDv7 transition identifier
//   - from_state_id   — the StateID of the source state
//   - to_state_id     — the StateID of the destination state
//   - commit_hash     — the git commit SHA of the checkpoint (EM-019 locator)
//   - transition_kind — the TransitionKind enum value per §3 / execution-model.md §6.1
type TransitionEventPayload struct {
	// RunID is the run that contains this transition. Must not be uuid.Nil.
	RunID RunID `json:"run_id"`

	// TransitionID is the UUIDv7 identifier for the transition being projected.
	// Must not be uuid.Nil.
	TransitionID TransitionID `json:"transition_id"`

	// FromStateID is the StateID of the source state (the state the run was in
	// before this transition). Must not be uuid.Nil.
	FromStateID StateID `json:"from_state_id"`

	// ToStateID is the StateID of the destination state (the state the run
	// transitions into). Must not be uuid.Nil.
	ToStateID StateID `json:"to_state_id"`

	// CommitHash is the full git commit SHA of the checkpoint commit that
	// contains the transition record sibling file (event-model.md §6.3;
	// execution-model.md §4.5.EM-025). Required (non-empty). Consumers use
	// this to locate the sibling file via git show per EM-019.
	// NOTE: Field was previously named CheckpointCommitHash in the pre-§8.1.6
	// stub; renamed to CommitHash to match the §6.3 wire-field name commit_hash.
	CommitHash string `json:"commit_hash"`

	// TransitionKind is the kind of the transition per execution-model.md §6.1
	// and §4.10.EM-044. Must be a declared TransitionKind constant.
	TransitionKind TransitionKind `json:"transition_kind"`
}

// Valid reports whether all required fields carry non-zero values.
//
// Rules (per event-model.md §8.1.6 and execution-model.md §4.6.EM-028):
//   - RunID is not uuid.Nil
//   - TransitionID is not uuid.Nil
//   - FromStateID is not uuid.Nil
//   - ToStateID is not uuid.Nil
//   - CommitHash is non-empty
//   - TransitionKind is a declared constant
func (p TransitionEventPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.TransitionID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.FromStateID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.ToStateID) == uuid.Nil {
		return false
	}
	if p.CommitHash == "" {
		return false
	}
	if !p.TransitionKind.Valid() {
		return false
	}
	return true
}
