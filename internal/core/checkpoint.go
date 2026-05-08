package core

import "github.com/google/uuid"

// Checkpoint records the durable evidence of a single successful state transition
// as a git commit on the run's task branch (execution-model.md §6.1).
//
// # EM-016 atomicity contract
//
// A Checkpoint MUST be represented as a single git commit whose tree contains
// (a) the work product and (b) the transition-record sibling file at the
// canonical path .harmonik/transitions/<run_id>/<transition_id>.json
// (execution-model.md §4.4.EM-016, §4.4.EM-018).
//
// The commit sequence is write-tree → commit-tree → update-ref. The atomicity
// boundary is the update-ref step: before update-ref completes, the transition
// is NOT observable to any subsystem. A crash between commit-tree and update-ref
// MAY leave loose objects in .git/objects/; those objects carry no reference and
// MUST NOT be treated as observable state — they are eligible for reclamation by
// git gc. The atomicity boundary covers reference visibility only; it does NOT
// cover the loose-object writes themselves.
//
// The task branch MUST exist before any checkpoint is attempted; branch-creation
// lifecycle is owned by workspace-model.md §4.2.
//
// # Path coherence
//
// TransitionRecordPath MUST equal
// ".harmonik/transitions/<RunID>/<TransitionID>.json" — the run_id and
// transition_id path components MUST match this record's RunID and TransitionID
// fields respectively (execution-model.md §4.4.EM-018). Run-scoping of the path
// is a structural uniqueness guarantee: cross-run merges, cherry-picks, and
// replay-tree construction from distinct runs cannot collide because each run's
// transitions occupy a disjoint sub-directory. Valid() enforces this invariant.
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

// Valid reports whether all required fields carry non-zero values and the
// path-coherence invariant of EM-016/EM-018 holds.
//
// A Checkpoint is valid iff:
//   - CommitHash is non-empty
//   - RunID is not the zero UUID
//   - StateID is not the zero UUID
//   - TransitionID is not the zero UUID
//   - BeadID, when non-nil, dereferences to a non-empty value
//   - SchemaVersion is non-zero (positive)
//   - TransitionRecordPath equals
//     ".harmonik/transitions/<RunID>/<TransitionID>.json"
//     (run_id and transition_id components MUST match this record's
//     RunID and TransitionID per execution-model.md §4.4.EM-018)
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
	// EM-018: TransitionRecordPath MUST be the canonical run-scoped path.
	// The run_id and transition_id path components MUST match this record's
	// RunID and TransitionID fields; a mismatch indicates a construction error.
	want := TransitionRecordPath(c.RunID, c.TransitionID)
	return c.TransitionRecordPath == want
}
