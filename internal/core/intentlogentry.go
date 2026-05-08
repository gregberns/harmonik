package core

import (
	"time"

	"github.com/google/uuid"
)

// IntentLogEntry is the durable record written to the intent log before every
// Beads terminal-transition write (beads-integration.md §6.1 RECORD IntentLogEntry).
//
// The intent log lives under .harmonik/beads-intents/. One JSON file is written
// per pending operation, keyed by IdempotencyKey. The adapter creates the file
// before the br call and deletes it on success. After a crash, surviving files
// describe exactly the set of writes whose completion is ambiguous — inputs to the
// §4.10 BI-031 idempotency-recovery protocol.
//
// # Schema compatibility
//
// IntentLogEntry carries SchemaVersion, an integer under the N-1 readability
// contract of operator-nfr.md §4.5 (ON-018). A reader at schema version N-1 MUST
// successfully parse and interpret artifacts written by version N, treating additive
// fields as unknown but non-fatal. Breaking changes (rename or removal) MUST NOT be
// introduced without a migration release and an operator pause.
//
// The current schema version is 1. Additive-only changes to this struct are
// non-breaking and do not require a migration release; renaming or removing fields
// is breaking and must bump SchemaVersion.
type IntentLogEntry struct {
	// IdempotencyKey is the stable composite key "<run_id>:<transition_id>:<op>"
	// per beads-integration.md §4.10 BI-029. Required (non-empty).
	IdempotencyKey string

	// RunID is the UUIDv7 identifier of the harmonik run driving this write.
	// Must not be uuid.Nil.
	RunID RunID

	// TransitionID is the UUIDv7 identifier of the transition at which this
	// write is emitted. Must not be uuid.Nil.
	TransitionID TransitionID

	// Op is the terminal operation to be issued to Beads.
	// One of: claim, close, reopen.
	Op TerminalOp

	// BeadID is the target bead identifier (opaque per BI-008a).
	// Required (non-empty).
	BeadID BeadID

	// IntendedPostState is the Beads status that harmonik expects after the
	// write succeeds. Derived from (Op, current_pre_state):
	//   claim  -> in_progress
	//   close  -> closed
	//   reopen -> open
	// Must be a valid CoarseStatus.
	IntendedPostState CoarseStatus

	// RequestedAt is the RFC 3339 wall-clock time at which the intent was
	// recorded. Must be non-zero.
	RequestedAt time.Time

	// SchemaVersion is the schema version of this record. N-1 readable per
	// operator-nfr.md §4.5 ON-018: a reader at version N-1 MUST tolerate
	// records written at version N, treating unknown additive fields as
	// non-fatal. The current version is 1. Renaming or removing fields is a
	// breaking change and requires incrementing this value.
	// Must be > 0.
	SchemaVersion int
}

// Valid reports whether all required fields carry valid values.
//
// Rules:
//   - IdempotencyKey is non-empty
//   - RunID is not uuid.Nil
//   - TransitionID is not uuid.Nil
//   - Op satisfies Op.Valid()
//   - BeadID is non-empty
//   - IntendedPostState satisfies IntendedPostState.Valid()
//   - RequestedAt is non-zero
//   - SchemaVersion is > 0
func (e IntentLogEntry) Valid() bool {
	if e.IdempotencyKey == "" {
		return false
	}
	if uuid.UUID(e.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(e.TransitionID) == uuid.Nil {
		return false
	}
	if !e.Op.Valid() {
		return false
	}
	if e.BeadID == "" {
		return false
	}
	if !e.IntendedPostState.Valid() {
		return false
	}
	if e.RequestedAt.IsZero() {
		return false
	}
	if e.SchemaVersion <= 0 {
		return false
	}
	return true
}
