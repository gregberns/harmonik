package core

import (
	"encoding/json"
	"fmt"
	"os"
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
	// IdempotencyKey is the stable composite key per beads-integration.md §4.10 BI-029.
	// For claim/close/reopen: "<run_id>:<transition_id>:<op>".
	// For reset (BI-010d): "<project_hash>:<bead_id>:reset:<daemon_start_ns>".
	// Required (non-empty).
	IdempotencyKey string `json:"idempotency_key"`

	// RunID is the UUIDv7 identifier of the harmonik run driving this write.
	// Must not be uuid.Nil — EXCEPT for TerminalOpReset entries, which are issued
	// by the daemon startup orphan-sweep (BI-010d) and carry no associated run.
	RunID RunID `json:"run_id"`

	// TransitionID is the UUIDv7 identifier of the transition at which this
	// write is emitted. Must not be uuid.Nil — EXCEPT for TerminalOpReset entries
	// (same exception as RunID; see BI-010d).
	TransitionID TransitionID `json:"transition_id"`

	// Op is the terminal operation to be issued to Beads.
	// One of: claim, close, reopen, reset.
	// Spec ref: specs/beads-integration.md §6.1 ENUM TerminalOp; §4.4 BI-010d (reset).
	Op TerminalOp `json:"op"`

	// BeadID is the target bead identifier (opaque per BI-008a).
	// Required (non-empty).
	BeadID BeadID `json:"bead_id"`

	// IntendedPostState is the Beads status that harmonik expects after the
	// write succeeds. Derived from (Op, current_pre_state):
	//   claim  -> in_progress
	//   close  -> closed
	//   reopen -> open
	//   reset  -> open  (BI-010d)
	// Must be a valid CoarseStatus.
	IntendedPostState CoarseStatus `json:"intended_post_state"`

	// RequestedAt is the RFC 3339 wall-clock time at which the intent was
	// recorded. Must be non-zero.
	RequestedAt time.Time `json:"requested_at"`

	// SchemaVersion is the schema version of this record. N-1 readable per
	// operator-nfr.md §4.5 ON-018: a reader at version N-1 MUST tolerate
	// records written at version N, treating unknown additive fields as
	// non-fatal. The current version is 1. Renaming or removing fields is a
	// breaking change and requires incrementing this value.
	// Must be > 0.
	SchemaVersion int `json:"schema_version"`
}

// ReadIntentLogEntry reads a single intent-log file from path and returns the
// decoded IntentLogEntry. It is the production counterpart to the testhelpers
// crash-harness reader and implements BI-031 step 1:
//
//	"Read the intent file's recorded transition fields: op, bead_id,
//	idempotency_key, intended_post_state."
//
// The file at path must contain a JSON-encoded IntentLogEntry (snake_case keys
// per §6.1 RECORD IntentLogEntry). Returns an error if the file cannot be
// read, cannot be decoded, or the decoded entry fails Valid().
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 1; §6.1 RECORD
// IntentLogEntry; §6.2 on-disk layout.
func ReadIntentLogEntry(path string) (IntentLogEntry, error) {
	//nolint:gosec // G304: path is an adapter-controlled intent-log path under .harmonik/beads-intents/
	data, err := os.ReadFile(path)
	if err != nil {
		return IntentLogEntry{}, fmt.Errorf("ReadIntentLogEntry: read %q: %w", path, err)
	}

	var entry IntentLogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return IntentLogEntry{}, fmt.Errorf("ReadIntentLogEntry: decode %q: %w", path, err)
	}

	if !entry.Valid() {
		return IntentLogEntry{}, fmt.Errorf("ReadIntentLogEntry: entry at %q failed Valid()", path)
	}

	return entry, nil
}

// Valid reports whether all required fields carry valid values.
//
// Rules:
//   - IdempotencyKey is non-empty
//   - RunID is not uuid.Nil (EXCEPT when Op == TerminalOpReset: reset is issued
//     by the daemon startup orphan-sweep, not by an in-flight run; RunID and
//     TransitionID are zero-valued per BI-010d)
//   - TransitionID is not uuid.Nil (same exception as RunID for TerminalOpReset)
//   - Op satisfies Op.Valid()
//   - BeadID is non-empty
//   - IntendedPostState satisfies IntendedPostState.Valid()
//   - RequestedAt is non-zero
//   - SchemaVersion is > 0
//
// Spec ref: specs/beads-integration.md §4.4 BI-010d; §6.1 RECORD IntentLogEntry.
func (e IntentLogEntry) Valid() bool {
	if e.IdempotencyKey == "" {
		return false
	}
	// BI-010d: reset is a startup-only write with no associated run or transition.
	// RunID and TransitionID are zero-valued for reset entries; all other ops require
	// non-nil UUIDs.
	if e.Op != TerminalOpReset {
		if uuid.UUID(e.RunID) == uuid.Nil {
			return false
		}
		if uuid.UUID(e.TransitionID) == uuid.Nil {
			return false
		}
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
