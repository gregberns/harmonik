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
// # EM-025 — failed transitions MUST NOT create checkpoint commits (Core MVH)
//
// A Checkpoint is created ONLY for a successful, durable state transition
// (outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS} per §4.5.EM-023a). A failed
// transition — i.e., outcome.status = FAIL, or a classifier verdict of
// transient|structural|deterministic|canceled|budget_exhausted|compilation_loop
// per §8 — MUST emit a failure event (execution-model.md §4.5.EM-025, §8) but
// MUST NOT create a Checkpoint or advance the task branch. The failure event
// MUST carry the last successful checkpoint's commit SHA in its last_checkpoint
// correlation field, providing an anchor to the git trail.
//
// Post-MVH introduction of failure commits (to support git bisect over failures
// for the improvement loop) is an additive change and does not alter this
// contract (execution-model.md §4.5.EM-025 additive note, §10.2).
//
// # EM-025a — emission ordering: update-ref MUST precede transition_event
//
// When a checkpoint write succeeds, the daemon MUST emit events in the
// following order (execution-model.md §4.5.EM-025a, §7.2):
//
//  1. git update-ref returns success (the commit becomes observable)
//  2. transition_event emitted to the event bus (execution-model.md §4.6.EM-028)
//  3. checkpoint_written event emitted
//  4. state-entered event emitted
//
// A pre-commit transition_event — emitted before update-ref completes — would
// leave observers with a transition reference whose commit is never durable if
// the reference advance fails (e.g., ENOSPC between commit-tree and update-ref),
// producing divergence-evidence false positives in reconciliation detectors.
//
// ENOSPC or EIO during the checkpoint sequence MUST be classified as transient
// (execution-model.md §8.1) with a bounded retry cap; on cap exhaustion the
// class reclassifies to structural (execution-model.md §8.2). On retry, a new
// transition_id MUST be generated (execution-model.md §4.4.EM-018a; daemon-local
// UUIDv7 per TransitionIDGenerator); evidence files written under
// .harmonik/transitions/<run_id>/<failed_transition_id>/evidence/* by the failed
// attempt MUST be removed before the retry, or MAY be reclaimed by a periodic
// sweeper of unreferenced <transition_id> sub-directories (i.e., those whose
// transition_id is not referenced by any trailer on any commit reachable from
// the task branch).
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
