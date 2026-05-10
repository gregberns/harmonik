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

// idempCrashRecoveryFixtureDir creates a temp directory representing
// .harmonik/beads-intents/ and returns its path. Used by umbrella
// crash-recovery tests (hk-872.38).
func idempCrashRecoveryFixtureDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// idempCrashRecoveryFixtureScan scans dir for *.json files (excluding *.tmp-*)
// and returns all successfully decoded IntentLogEntry values. Mirrors the
// scan logic described in BI-031: after a crash, surviving *.json files are the
// set of writes whose completion is ambiguous.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 — surviving intent files
// after crash.
func idempCrashRecoveryFixtureScan(t *testing.T, dir string) []IntentLogEntry {
	t.Helper()

	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("idempCrashRecoveryFixtureScan: ReadDir %q: %v", dir, err)
	}

	var entries []IntentLogEntry
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		// Skip pre-rename temp files — these represent writes that crashed
		// before rename completed; the write did not land.
		if len(name) > 5 && name[len(name)-5:] != ".json" {
			continue
		}
		// Explicitly skip .tmp- files per BI-031 scan discipline.
		isTmp := false
		for i := range name {
			if i+5 <= len(name) && name[i:i+5] == ".tmp-" {
				isTmp = true
				break
			}
		}
		if isTmp {
			continue
		}

		path := filepath.Join(dir, name)
		entry, err := ReadIntentLogEntry(path)
		if err != nil {
			continue // non-JSON or invalid files are skipped by the scanner
		}
		entries = append(entries, entry)
	}
	return entries
}

// TestIdempCrashRecovery_SingleEntryPersistsAfterCrash verifies that an
// IntentLogEntry written to the intent-log directory is recoverable via
// ReadIntentLogEntry after a simulated crash (i.e., the adapter simply stops
// without deleting the file). This is the core property of BI-031: fsynced
// intent files survive crashes and describe ambiguous writes at restart.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 (write durability);
// §4.10 BI-031 (recovery reads surviving files).
func TestIdempCrashRecovery_SingleEntryPersistsAfterCrash(t *testing.T) {
	t.Parallel()

	dir := idempCrashRecoveryFixtureDir(t)
	entry := intentEntryValid(t)
	entry.RequestedAt = entry.RequestedAt.UTC().Truncate(time.Second)

	// Simulate the adapter's pre-write step: write the intent file.
	path := intentReadFixtureWrite(t, dir, entry)

	// Simulate a crash: the adapter stops without deleting the file.
	// On restart, scan the directory — the file must be discoverable.
	surviving := idempCrashRecoveryFixtureScan(t, dir)

	if len(surviving) != 1 {
		t.Fatalf("scan found %d entries after crash, want 1 (path=%q)", len(surviving), path)
	}
	got := surviving[0]

	if got.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("recovered IdempotencyKey = %q, want %q", got.IdempotencyKey, entry.IdempotencyKey)
	}
	if got.Op != entry.Op {
		t.Errorf("recovered Op = %q, want %q", got.Op, entry.Op)
	}
	if got.BeadID != entry.BeadID {
		t.Errorf("recovered BeadID = %q, want %q", got.BeadID, entry.BeadID)
	}
	if got.IntendedPostState != entry.IntendedPostState {
		t.Errorf("recovered IntendedPostState = %q, want %q", got.IntendedPostState, entry.IntendedPostState)
	}
	if !got.Valid() {
		t.Error("Valid() = false on recovered entry, want true")
	}
}

// TestIdempCrashRecovery_MultipleEntriesAllSurvive verifies that when multiple
// intent files are written before a crash, all survive and are recovered by the
// directory scan. Each distinct terminal-transition write is an independent
// intent file keyed by its idempotency key.
//
// Spec ref: specs/beads-integration.md §6.2 on-disk layout (one file per pending
// operation, keyed by idempotency_key).
func TestIdempCrashRecovery_MultipleEntriesAllSurvive(t *testing.T) {
	t.Parallel()

	dir := idempCrashRecoveryFixtureDir(t)

	// Write three distinct entries with different ops and bead IDs.
	ops := []struct {
		op        TerminalOp
		postState CoarseStatus
		beadID    BeadID
	}{
		{TerminalOpClaim, CoarseStatusInProgress, BeadID("hk-001")},
		{TerminalOpClose, CoarseStatusClosed, BeadID("hk-002")},
		{TerminalOpReopen, CoarseStatusOpen, BeadID("hk-003")},
	}

	for _, tc := range ops {
		e := intentEntryValid(t)
		e.Op = tc.op
		e.IntendedPostState = tc.postState
		e.BeadID = tc.beadID
		e.IdempotencyKey = "run-abc:trans-xyz:" + string(tc.op)
		e.RequestedAt = e.RequestedAt.UTC().Truncate(time.Second)
		intentReadFixtureWrite(t, dir, e)
	}

	surviving := idempCrashRecoveryFixtureScan(t, dir)
	if len(surviving) != len(ops) {
		t.Fatalf("scan found %d entries, want %d", len(surviving), len(ops))
	}
}

// TestIdempCrashRecovery_TmpFileSkipped verifies that a .tmp- file (representing
// a write that crashed before the rename completed) is excluded from the
// recovery scan. Per BI-030, only the post-rename *.json files represent writes
// that landed atomically; a surviving .tmp- file is evidence of a crash during
// the write step itself (before the intent was durably committed).
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 — "rename(2) to
// .harmonik/beads-intents/<idempotency_key>.json. The rename is atomic."
func TestIdempCrashRecovery_TmpFileSkipped(t *testing.T) {
	t.Parallel()

	dir := idempCrashRecoveryFixtureDir(t)

	// Write a well-formed entry as a .tmp- file (simulate a crash before rename).
	entry := intentEntryValid(t)
	entry.RequestedAt = entry.RequestedAt.UTC().Truncate(time.Second)

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	tmpPath := filepath.Join(dir, "run-abc_trans-xyz_claim.json.tmp-deadbeef")
	//nolint:gosec // G306: test temp dir
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The scan must return zero entries: the .tmp- file is not a committed write.
	surviving := idempCrashRecoveryFixtureScan(t, dir)
	if len(surviving) != 0 {
		t.Errorf("scan returned %d entries for .tmp- only directory, want 0", len(surviving))
	}
}

// TestIdempCrashRecovery_CleanLogEmpty verifies that a clean intent-log directory
// (no surviving files) produces an empty scan result. This represents the normal
// steady state: all writes completed successfully and their intent files were
// deleted.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 (delete on success).
func TestIdempCrashRecovery_CleanLogEmpty(t *testing.T) {
	t.Parallel()

	dir := idempCrashRecoveryFixtureDir(t)
	surviving := idempCrashRecoveryFixtureScan(t, dir)
	if len(surviving) != 0 {
		t.Errorf("scan returned %d entries for empty intent-log dir, want 0", len(surviving))
	}
}

// cat3aEvidenceFixtureEntry returns a valid IntentLogEntry representing a
// pending claim write, used to simulate the intent-log evidence consumed by the
// Cat 3a detector per BI-032. The entry models the situation where the adapter
// wrote the intent file, crashed, and the bead's status is now ambiguous.
//
// Spec ref: specs/beads-integration.md §4.10 BI-032 — "The intent log and
// Beads's audit log MUST be the evidence sources consumed by the Cat 3a
// torn-Beads-write detector."
func cat3aEvidenceFixtureEntry(t *testing.T) IntentLogEntry {
	t.Helper()
	e := intentEntryValid(t)
	e.Op = TerminalOpClaim
	e.IntendedPostState = CoarseStatusInProgress
	e.RequestedAt = e.RequestedAt.UTC().Truncate(time.Second)
	return e
}

// TestCat3aEvidence_IntentLogEntryIsDetectorInput verifies that an
// IntentLogEntry carries all the fields the Cat 3a detector needs to determine
// whether a Beads write was torn:
//
//   - IdempotencyKey — uniquely identifies the write for audit-log correlation
//   - Op — identifies what operation was in flight
//   - BeadID — identifies the target bead for `br show <bead_id>`
//   - IntendedPostState — the expected post-write status; deviation signals torn write
//
// A non-empty IdempotencyKey with a surviving intent file and a bead status that
// is neither the pre-state nor the IntendedPostState is the Cat 3a signal per
// specs/reconciliation/spec.md §8.4a / specs/beads-integration.md §4.10 BI-032.
//
// Spec ref: specs/beads-integration.md §4.10 BI-032; §6.1 RECORD IntentLogEntry.
func TestCat3aEvidence_IntentLogEntryIsDetectorInput(t *testing.T) {
	t.Parallel()

	entry := cat3aEvidenceFixtureEntry(t)

	// All four evidence fields required by the Cat 3a detector must be populated.
	if entry.IdempotencyKey == "" {
		t.Error("IdempotencyKey is empty — Cat 3a detector cannot correlate audit log")
	}
	if !entry.Op.Valid() {
		t.Errorf("Op %q is not valid — detector cannot classify the write", entry.Op)
	}
	if entry.BeadID == "" {
		t.Error("BeadID is empty — detector cannot query br show <bead_id>")
	}
	if !entry.IntendedPostState.Valid() {
		t.Errorf("IntendedPostState %q is not valid — detector cannot check status deviation", entry.IntendedPostState)
	}
	if !entry.Valid() {
		t.Error("Valid() = false — entry is not a valid Cat 3a evidence record")
	}
}

// TestCat3aEvidence_ReconciliationCategoryLinkage verifies that
// ReconciliationCategoryCat3a is a valid, distinct ReconciliationCategory value
// and that it round-trips through JSON. The Cat 3a category is the classification
// emitted when a surviving intent file is present and the bead's current status
// is neither the pre-state nor the IntendedPostState.
//
// Spec ref: specs/reconciliation/schemas.md §6.1 ENUM ReconciliationCategory;
// specs/beads-integration.md §4.10 BI-032.
func TestCat3aEvidence_ReconciliationCategoryLinkage(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat3a

	if !cat.Valid() {
		t.Errorf("ReconciliationCategoryCat3a.Valid() = false, want true")
	}
	if string(cat) != "cat-3a" {
		t.Errorf("string(ReconciliationCategoryCat3a) = %q, want %q", string(cat), "cat-3a")
	}

	// Round-trip through JSON (MarshalText / UnmarshalText paths).
	data, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("json.Marshal(Cat3a): %v", err)
	}
	var got ReconciliationCategory
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(Cat3a): %v", err)
	}
	if got != cat {
		t.Errorf("round-trip: got %q, want %q", got, cat)
	}
}

// TestCat3aEvidence_IntentFilePresenceSignalsTornWrite verifies that the
// presence of an IntentLogEntry file in the intent-log directory after a crash
// constitutes the evidence of a potentially torn write — the first condition the
// Cat 3a detector checks per BI-032. An absent file means the write completed
// and the adapter deleted it; a present file means the write outcome is unknown.
//
// Spec ref: specs/beads-integration.md §4.10 BI-032; §4.10 BI-030 (delete on
// success means absence = completed).
func TestCat3aEvidence_IntentFilePresenceSignalsTornWrite(t *testing.T) {
	t.Parallel()

	dir := idempCrashRecoveryFixtureDir(t)
	entry := cat3aEvidenceFixtureEntry(t)

	// Before any write: no surviving files = no torn write evidence.
	before := idempCrashRecoveryFixtureScan(t, dir)
	if len(before) != 0 {
		t.Fatalf("pre-write scan: got %d entries, want 0", len(before))
	}

	// Write intent file (simulate adapter pre-write step).
	intentReadFixtureWrite(t, dir, entry)

	// After write + simulated crash: surviving file = torn write evidence.
	after := idempCrashRecoveryFixtureScan(t, dir)
	if len(after) != 1 {
		t.Fatalf("post-crash scan: got %d entries, want 1", len(after))
	}

	surviving := after[0]
	// The detector needs the IdempotencyKey to query the audit log.
	if surviving.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("IdempotencyKey = %q, want %q", surviving.IdempotencyKey, entry.IdempotencyKey)
	}
	// The detector needs IntendedPostState to classify the write outcome.
	if surviving.IntendedPostState != entry.IntendedPostState {
		t.Errorf("IntendedPostState = %q, want %q", surviving.IntendedPostState, entry.IntendedPostState)
	}
}
