package core

import "github.com/google/uuid"

// TransitionEventPayload is the event-bus payload for the transition event
// (execution-model.md §4.6.EM-028).
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
//	git show <CheckpointCommitHash>:.harmonik/transitions/<RunID>/<TransitionID>.json
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
type TransitionEventPayload struct {
	// TransitionID is the UUIDv7 identifier for the transition being projected.
	// Must not be uuid.Nil.
	TransitionID TransitionID

	// RunID is the run that contains this transition.
	// Must not be uuid.Nil.
	RunID RunID

	// CheckpointCommitHash is the full git commit SHA of the checkpoint commit
	// that contains the transition record sibling file.
	// Required (non-empty). Consumers use this to locate the sibling file via
	// git show per EM-019.
	CheckpointCommitHash string
}

// Valid reports whether all required fields carry non-zero values.
//
// Rules (per execution-model.md §4.6.EM-028):
//   - TransitionID is not uuid.Nil
//   - RunID is not uuid.Nil
//   - CheckpointCommitHash is non-empty
func (p TransitionEventPayload) Valid() bool {
	if uuid.UUID(p.TransitionID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.CheckpointCommitHash == "" {
		return false
	}
	return true
}
