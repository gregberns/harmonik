package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// intentEntryValid returns a fully-populated IntentLogEntry with all required
// fields set to valid values. Tests mutate individual fields to probe Valid().
func intentEntryValid(t *testing.T) IntentLogEntry {
	t.Helper()
	return IntentLogEntry{
		IdempotencyKey:    "run-abc:trans-xyz:claim",
		RunID:             RunID(uuid.Must(uuid.NewV7())),
		TransitionID:      TransitionID(uuid.Must(uuid.NewV7())),
		Op:                TerminalOpClaim,
		BeadID:            BeadID("hk-872"),
		IntendedPostState: CoarseStatusInProgress,
		RequestedAt:       time.Now(),
		SchemaVersion:     1,
	}
}

func TestIntentLogEntryValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	if !e.Valid() {
		t.Error("Valid() = false for fully-populated IntentLogEntry, want true")
	}
}

func TestIntentLogEntryValid_EmptyIdempotencyKey(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.IdempotencyKey = ""
	if e.Valid() {
		t.Error("Valid() = true with empty IdempotencyKey, want false")
	}
}

func TestIntentLogEntryValid_ZeroRunID(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.RunID = RunID(uuid.Nil)
	if e.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

func TestIntentLogEntryValid_ZeroTransitionID(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.TransitionID = TransitionID(uuid.Nil)
	if e.Valid() {
		t.Error("Valid() = true with zero TransitionID, want false")
	}
}

func TestIntentLogEntryValid_InvalidOp(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.Op = TerminalOp("unknown")
	if e.Valid() {
		t.Error("Valid() = true with invalid Op, want false")
	}
}

func TestIntentLogEntryValid_EmptyOp(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.Op = TerminalOp("")
	if e.Valid() {
		t.Error("Valid() = true with empty Op, want false")
	}
}

func TestIntentLogEntryValid_EmptyBeadID(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.BeadID = BeadID("")
	if e.Valid() {
		t.Error("Valid() = true with empty BeadID, want false")
	}
}

func TestIntentLogEntryValid_InvalidIntendedPostState(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.IntendedPostState = CoarseStatus("unknown_state")
	if e.Valid() {
		t.Error("Valid() = true with invalid IntendedPostState, want false")
	}
}

func TestIntentLogEntryValid_ZeroRequestedAt(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.RequestedAt = time.Time{}
	if e.Valid() {
		t.Error("Valid() = true with zero RequestedAt, want false")
	}
}

func TestIntentLogEntryValid_ZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.SchemaVersion = 0
	if e.Valid() {
		t.Error("Valid() = true with zero SchemaVersion, want false")
	}
}

func TestIntentLogEntryValid_NegativeSchemaVersion(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.SchemaVersion = -1
	if e.Valid() {
		t.Error("Valid() = true with negative SchemaVersion, want false")
	}
}

func TestIntentLogEntryValid_AllOps(t *testing.T) {
	t.Parallel()

	ops := []struct {
		op        TerminalOp
		postState CoarseStatus
	}{
		{TerminalOpClaim, CoarseStatusInProgress},
		{TerminalOpClose, CoarseStatusClosed},
		{TerminalOpReopen, CoarseStatusOpen},
	}
	for _, tc := range ops {
		t.Run(string(tc.op), func(t *testing.T) {
			t.Parallel()
			e := intentEntryValid(t)
			e.Op = tc.op
			e.IntendedPostState = tc.postState
			if !e.Valid() {
				t.Errorf("Valid() = false for op=%q postState=%q, want true", tc.op, tc.postState)
			}
		})
	}
}

// TestIntentLogEntryJSONRoundTrip verifies JSON marshal/unmarshal preserves all
// fields correctly. The SchemaVersion field is the N-1-readable sentinel: a
// reader at version 1 must parse records written at version 1 without error.
func TestIntentLogEntryJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := intentEntryValid(t)
	original.RequestedAt = original.RequestedAt.UTC().Truncate(time.Second)

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got IntentLogEntry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got.IdempotencyKey != original.IdempotencyKey {
		t.Errorf("IdempotencyKey = %q, want %q", got.IdempotencyKey, original.IdempotencyKey)
	}
	if uuid.UUID(got.RunID) != uuid.UUID(original.RunID) {
		t.Errorf("RunID = %v, want %v", got.RunID, original.RunID)
	}
	if uuid.UUID(got.TransitionID) != uuid.UUID(original.TransitionID) {
		t.Errorf("TransitionID = %v, want %v", got.TransitionID, original.TransitionID)
	}
	if got.Op != original.Op {
		t.Errorf("Op = %q, want %q", got.Op, original.Op)
	}
	if got.BeadID != original.BeadID {
		t.Errorf("BeadID = %q, want %q", got.BeadID, original.BeadID)
	}
	if got.IntendedPostState != original.IntendedPostState {
		t.Errorf("IntendedPostState = %q, want %q", got.IntendedPostState, original.IntendedPostState)
	}
	if !got.RequestedAt.Equal(original.RequestedAt) {
		t.Errorf("RequestedAt = %v, want %v", got.RequestedAt, original.RequestedAt)
	}
	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, original.SchemaVersion)
	}
	if !got.Valid() {
		t.Error("Valid() = false after JSON round-trip, want true")
	}
}

// TestIntentLogEntryJSONForwardCompat verifies the N-1 readability contract
// from operator-nfr.md §4.5 ON-018: a reader at the current schema version
// must tolerate JSON with an unknown additive field (as would be written by a
// newer N+1 writer). The unknown field is silently ignored; Valid() must pass.
func TestIntentLogEntryJSONForwardCompat(t *testing.T) {
	t.Parallel()

	e := intentEntryValid(t)
	e.RequestedAt = e.RequestedAt.UTC().Truncate(time.Second)

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	// Inject a hypothetical future field into the JSON blob.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}
	raw["future_field"] = "some_new_value"
	raw["schema_version"] = 2 // simulate N+1 writer

	enriched, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json.Marshal enriched error: %v", err)
	}

	var got IntentLogEntry
	if err := json.Unmarshal(enriched, &got); err != nil {
		t.Fatalf("json.Unmarshal of enriched record error: %v", err)
	}

	// SchemaVersion 2 is > 0 so Valid() must still return true.
	if !got.Valid() {
		t.Error("Valid() = false when parsing N+1 record with additive field, want true (N-1 compat)")
	}
}
