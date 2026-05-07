package core

import "github.com/google/uuid"

// Checkpoint records the durable evidence of a single successful state transition
// as a git commit on the run's task branch (execution-model.md §6.1).
//
// Every Checkpoint corresponds to exactly one checkpoint commit whose tree carries
// both the work product and a transition-record sibling file at the canonical path
// defined by EM-018: .harmonik/transitions/<run_id>/<transition_id>.json.
// Run-scoping of the path is a structural uniqueness guarantee: cross-run merges,
// cherry-picks, and replay-tree construction from distinct runs cannot collide
// because each run's transitions occupy a disjoint sub-directory.
type Checkpoint struct {
	// CommitHash is the git commit SHA on the task branch for this checkpoint.
	CommitHash string

	// RunID is the run this checkpoint belongs to; matches the Harmonik-Run-ID trailer.
	RunID RunID

	// StateID is the run state after the transition; matches the Harmonik-State-ID trailer.
	StateID StateID

	// TransitionID identifies the transition recorded by this commit;
	// matches the Harmonik-Transition-ID trailer.
	TransitionID TransitionID

	// BeadID is present when the run is tied to a bead per EM-014; absent otherwise.
	// Matches the Harmonik-Bead-ID trailer when present.
	BeadID *BeadID

	// SchemaVersion is an integer version counter for the transition-record sibling file;
	// matches the Harmonik-Schema-Version trailer and the sibling file's schema_version
	// field per §4.4.EM-022 (N-1 readable).
	SchemaVersion int

	// TransitionRecordPath is the path within the commit tree of the typed JSON file
	// carrying the full Transition record. Always ".harmonik/transitions/<run_id>/<transition_id>.json"
	// per §4.4.EM-018; stored as-is (path construction is a runtime concern).
	TransitionRecordPath string
}

// Valid reports whether all required fields carry non-zero values.
// A Checkpoint is considered valid iff:
//   - CommitHash is non-empty
//   - RunID is not the zero UUID
//   - StateID is not the zero UUID
//   - TransitionID is not the zero UUID
//   - BeadID, when non-nil, dereferences to a non-empty value
//   - SchemaVersion is non-zero
//   - TransitionRecordPath is non-empty
func (c Checkpoint) Valid() bool {
	if c.CommitHash == "" {
		return false
	}
	if uuid.UUID(c.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(c.StateID) == uuid.Nil {
		return false
	}
	if uuid.UUID(c.TransitionID) == uuid.Nil {
		return false
	}
	if c.BeadID != nil && *c.BeadID == "" {
		return false
	}
	if c.SchemaVersion == 0 {
		return false
	}
	if c.TransitionRecordPath == "" {
		return false
	}
	return true
}
