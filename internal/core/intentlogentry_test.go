package core

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// intentReadFixtureWrite writes entry as JSON to a file under dir named
// <idempotency_key_encoded>.json (colons encoded as underscores per OQ-BI-003)
// and returns the file path. Used by ReadIntentLogEntry tests (hk-872.38.1).
func intentReadFixtureWrite(t *testing.T, dir string, entry IntentLogEntry) string {
	t.Helper()

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("intentReadFixtureWrite: json.Marshal: %v", err)
	}

	// Encode colons in key to underscores per OQ-BI-003 (filesystem portability).
	encoded := ""
	for _, ch := range entry.IdempotencyKey {
		if ch == ':' {
			encoded += "_"
		} else {
			encoded += string(ch)
		}
	}
	name := encoded + ".json"
	path := filepath.Join(dir, name)

	//nolint:gosec // G306: intent files are readable by the daemon user only; 0o600 is correct
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("intentReadFixtureWrite: WriteFile %q: %v", path, err)
	}
	return path
}

// TestReadIntentLogEntry_RoundTrip verifies that ReadIntentLogEntry decodes a
// file written by json.Marshal(IntentLogEntry) and returns an equal, valid entry.
// Covers BI-031 step 1: read op, bead_id, idempotency_key, intended_post_state.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 1; §6.1 IntentLogEntry.
func TestReadIntentLogEntry_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := intentEntryValid(t)
	original.RequestedAt = original.RequestedAt.UTC().Truncate(time.Second)

	path := intentReadFixtureWrite(t, dir, original)

	got, err := ReadIntentLogEntry(path)
	if err != nil {
		t.Fatalf("ReadIntentLogEntry error: %v", err)
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
		t.Error("Valid() = false after ReadIntentLogEntry, want true")
	}
}

// TestReadIntentLogEntry_FileMissing verifies that ReadIntentLogEntry returns an
// error when the file does not exist.
func TestReadIntentLogEntry_FileMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")

	_, err := ReadIntentLogEntry(path)
	if err == nil {
		t.Error("ReadIntentLogEntry returned nil error for missing file, want error")
	}
}

// TestReadIntentLogEntry_InvalidJSON verifies that ReadIntentLogEntry returns an
// error when the file contains malformed JSON.
func TestReadIntentLogEntry_InvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	//nolint:gosec // G306: test file
	if err := os.WriteFile(path, []byte("{not valid json}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadIntentLogEntry(path)
	if err == nil {
		t.Error("ReadIntentLogEntry returned nil error for invalid JSON, want error")
	}
}

// TestReadIntentLogEntry_InvalidEntry verifies that ReadIntentLogEntry returns an
// error when the JSON is valid but the decoded entry fails Valid() (e.g., empty
// IdempotencyKey).
func TestReadIntentLogEntry_InvalidEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Construct an entry with an empty IdempotencyKey — Valid() returns false.
	entry := intentEntryValid(t)
	entry.IdempotencyKey = ""

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	path := filepath.Join(dir, "invalid_entry.json")
	//nolint:gosec // G306: test file
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = ReadIntentLogEntry(path)
	if err == nil {
		t.Error("ReadIntentLogEntry returned nil error for invalid entry, want error")
	}
}

// TestReadIntentLogEntry_SnakeCaseKeys verifies that ReadIntentLogEntry correctly
// decodes intent files written with snake_case JSON keys (the on-disk format per
// §6.1 RECORD IntentLogEntry). This ensures the production adapter's writer and
// reader are compatible.
//
// Spec ref: specs/beads-integration.md §6.1 RECORD IntentLogEntry (field names).
func TestReadIntentLogEntry_SnakeCaseKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	original := intentEntryValid(t)
	original.RequestedAt = original.RequestedAt.UTC().Truncate(time.Second)

	// Write using explicit snake_case keys to simulate what the production
	// adapter writer will produce.
	snakeCaseJSON := `{
		"idempotency_key":     "` + original.IdempotencyKey + `",
		"run_id":              "` + original.RunID.String() + `",
		"transition_id":       "` + original.TransitionID.String() + `",
		"op":                  "` + string(original.Op) + `",
		"bead_id":             "` + string(original.BeadID) + `",
		"intended_post_state": "` + string(original.IntendedPostState) + `",
		"requested_at":        "` + original.RequestedAt.Format(time.RFC3339) + `",
		"schema_version":      1
	}`

	path := filepath.Join(dir, "snake_case.json")
	//nolint:gosec // G306: test file
	if err := os.WriteFile(path, []byte(snakeCaseJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadIntentLogEntry(path)
	if err != nil {
		t.Fatalf("ReadIntentLogEntry error: %v", err)
	}

	if got.IdempotencyKey != original.IdempotencyKey {
		t.Errorf("IdempotencyKey = %q, want %q", got.IdempotencyKey, original.IdempotencyKey)
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
	if !got.Valid() {
		t.Error("Valid() = false after decoding snake_case intent file, want true")
	}
}
