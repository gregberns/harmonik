package lifecycle

// orphansweep_gcintents_test.go — unit tests for GCRetiredIntents.
//
// Bead ref: hk-cizvu — orphan-sweep stale_intents_observed GC.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// fakeIntentGCLedger is a deterministic IntentGCLedger fake for tests.
// It returns a pre-configured BeadRecord for each bead ID; unknown bead IDs
// return an error.
type fakeIntentGCLedger struct {
	records map[core.BeadID]core.BeadRecord
}

func (f *fakeIntentGCLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if r, ok := f.records[id]; ok {
		return r, nil
	}
	return core.BeadRecord{}, &fakeShowBeadNotFoundError{id: id}
}

type fakeShowBeadNotFoundError struct{ id core.BeadID }

func (e *fakeShowBeadNotFoundError) Error() string {
	return "fakeIntentGCLedger: bead " + string(e.id) + " not found"
}

// gcIntentsFixtureWriteResetIntent writes a valid TerminalOpReset IntentLogEntry
// to intentsDir/<key>.json with mtime set to past (before daemonStartTime).
// Returns the path of the created file.
func gcIntentsFixtureWriteResetIntent(t *testing.T, intentsDir, key string, beadID core.BeadID) string {
	t.Helper()

	if err := os.MkdirAll(intentsDir, 0o755); err != nil { //nolint:gosec // G301: 0755 matches .harmonik dir conventions
		t.Fatalf("gcIntentsFixture: MkdirAll: %v", err)
	}

	entry := map[string]any{
		"idempotency_key":     key,
		"run_id":              "00000000-0000-0000-0000-000000000000",
		"transition_id":       "00000000-0000-0000-0000-000000000000",
		"op":                  "reset",
		"bead_id":             string(beadID),
		"intended_post_state": "open",
		"requested_at":        "2024-01-01T00:00:00Z",
		"schema_version":      1,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("gcIntentsFixture: marshal: %v", err)
	}

	encodedKey := key
	path := filepath.Join(intentsDir, encodedKey+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("gcIntentsFixture: WriteFile: %v", err)
	}

	// Set mtime to past so file is stale relative to daemonStartTime.
	past := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("gcIntentsFixture: Chtimes: %v", err)
	}

	return path
}

// TestGCRetiredIntents_Empty verifies that GCRetiredIntents returns
// a zero result when the beads-intents directory does not exist.
func TestGCRetiredIntents_Empty(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ledger := &fakeIntentGCLedger{records: map[core.BeadID]core.BeadRecord{}}

	result, err := GCRetiredIntents(context.Background(), projectDir, time.Now(), ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents empty dir: unexpected error: %v", err)
	}
	if result.Removed != 0 || result.Retained != 0 {
		t.Errorf("GCRetiredIntents empty dir: want {0,0}, got {%d,%d}", result.Removed, result.Retained)
	}
}

// TestGCRetiredIntents_RemovesWhenLanded verifies that a stale intent file is
// deleted when the bead has already reached its IntendedPostState (the op landed).
func TestGCRetiredIntents_RemovesWhenLanded(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-test-remove")

	intentPath := gcIntentsFixtureWriteResetIntent(t, intentsDir, "proj_hk-test-remove_reset_1", beadID)

	// Ledger reports bead as "open" (== IntendedPostState "open" for reset).
	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusOpen},
		},
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents remove: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("GCRetiredIntents remove: Removed = %d, want 1", result.Removed)
	}
	if result.Retained != 0 {
		t.Errorf("GCRetiredIntents remove: Retained = %d, want 0", result.Retained)
	}

	// File must have been deleted.
	if _, statErr := os.Stat(intentPath); !os.IsNotExist(statErr) {
		t.Errorf("GCRetiredIntents remove: intent file still exists at %q; should have been deleted", intentPath)
	}
}

// TestGCRetiredIntents_RetainsWhenPending verifies that a stale intent file is
// NOT deleted when the bead has NOT yet reached its IntendedPostState.
func TestGCRetiredIntents_RetainsWhenPending(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-test-retain")

	intentPath := gcIntentsFixtureWriteResetIntent(t, intentsDir, "proj_hk-test-retain_reset_1", beadID)

	// Ledger reports bead as "in_progress" (NOT the intended "open" for reset).
	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusInProgress},
		},
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents retain: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("GCRetiredIntents retain: Removed = %d, want 0", result.Removed)
	}
	if result.Retained != 1 {
		t.Errorf("GCRetiredIntents retain: Retained = %d, want 1", result.Retained)
	}

	// File must still exist.
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Errorf("GCRetiredIntents retain: intent file was removed at %q; must be retained for Cat 3a", intentPath)
	}
}

// TestGCRetiredIntents_SkipsNewFiles verifies that intent files with mtime
// >= daemonStartTime are not touched (they are live, not stale).
func TestGCRetiredIntents_SkipsNewFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-test-new")

	// Record daemon start BEFORE creating the file — so file is NOT stale.
	daemonStart := time.Now()

	// Write the intent file AFTER daemonStart.
	if err := os.MkdirAll(intentsDir, 0o755); err != nil { //nolint:gosec // G301
		t.Fatalf("GCRetiredIntents new: MkdirAll: %v", err)
	}
	entry := map[string]any{
		"idempotency_key":     "proj_hk-test-new_reset_1",
		"run_id":              "00000000-0000-0000-0000-000000000000",
		"transition_id":       "00000000-0000-0000-0000-000000000000",
		"op":                  "reset",
		"bead_id":             string(beadID),
		"intended_post_state": "open",
		"requested_at":        "2024-01-01T00:00:00Z",
		"schema_version":      1,
	}
	data, jsonErr := json.Marshal(entry)
	if jsonErr != nil {
		t.Fatalf("GCRetiredIntents new: marshal: %v", jsonErr)
	}
	newPath := filepath.Join(intentsDir, "proj_hk-test-new_reset_1.json")
	if err := os.WriteFile(newPath, data, 0o600); err != nil {
		t.Fatalf("GCRetiredIntents new: WriteFile: %v", err)
	}
	// mtime is "now" which is >= daemonStart.

	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusOpen},
		},
	}

	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents new: unexpected error: %v", err)
	}
	// New file: not stale, must not be touched.
	if result.Removed != 0 || result.Retained != 0 {
		t.Errorf("GCRetiredIntents new: want {0,0}, got {%d,%d}", result.Removed, result.Retained)
	}
	if _, statErr := os.Stat(newPath); os.IsNotExist(statErr) {
		t.Errorf("GCRetiredIntents new: new file was removed; must be kept (not stale)")
	}
}

// TestGCRetiredIntents_RetainsMalformed verifies that a stale intent file with
// invalid/malformed JSON is retained (conservative path).
func TestGCRetiredIntents_RetainsMalformed(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	if err := os.MkdirAll(intentsDir, 0o755); err != nil { //nolint:gosec // G301
		t.Fatalf("GCRetiredIntents malformed: MkdirAll: %v", err)
	}
	badPath := filepath.Join(intentsDir, "bad-intent.json")
	if err := os.WriteFile(badPath, []byte(`not valid json{`), 0o600); err != nil {
		t.Fatalf("GCRetiredIntents malformed: WriteFile: %v", err)
	}
	past := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(badPath, past, past); err != nil {
		t.Fatalf("GCRetiredIntents malformed: Chtimes: %v", err)
	}

	daemonStart := time.Now()
	ledger := &fakeIntentGCLedger{records: map[core.BeadID]core.BeadRecord{}}

	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents malformed: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("GCRetiredIntents malformed: Removed = %d, want 0 (malformed file must not be removed)", result.Removed)
	}
	if result.Retained != 1 {
		t.Errorf("GCRetiredIntents malformed: Retained = %d, want 1", result.Retained)
	}
	if _, statErr := os.Stat(badPath); os.IsNotExist(statErr) {
		t.Errorf("GCRetiredIntents malformed: file was removed; must be retained")
	}
}

// TestGCRetiredIntents_RetainsOnShowBeadError verifies that a stale intent
// file is retained when ShowBead returns an error (conservative path).
func TestGCRetiredIntents_RetainsOnShowBeadError(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-test-showbead-err")

	gcIntentsFixtureWriteResetIntent(t, intentsDir, "proj_hk-test-showbead-err_reset_1", beadID)

	// Ledger has no record for this bead — ShowBead will return error.
	ledger := &fakeIntentGCLedger{records: map[core.BeadID]core.BeadRecord{}}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents showbead-err: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("GCRetiredIntents showbead-err: Removed = %d, want 0 (ShowBead error → retain)", result.Removed)
	}
	if result.Retained != 1 {
		t.Errorf("GCRetiredIntents showbead-err: Retained = %d, want 1", result.Retained)
	}
}

// TestGCRetiredIntents_Mixed verifies correct counts across a mix of stale
// intent files in different states.
func TestGCRetiredIntents_Mixed(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	// landed-a and landed-b: bead already in IntendedPostState → removed.
	landedA := core.BeadID("hk-landed-a")
	landedB := core.BeadID("hk-landed-b")
	// pending-c: bead NOT in IntendedPostState → retained.
	pendingC := core.BeadID("hk-pending-c")
	// no-ledger-d: ShowBead fails → retained.
	noLedgerD := core.BeadID("hk-no-ledger-d")

	gcIntentsFixtureWriteResetIntent(t, intentsDir, "key-a", landedA)
	gcIntentsFixtureWriteResetIntent(t, intentsDir, "key-b", landedB)
	gcIntentsFixtureWriteResetIntent(t, intentsDir, "key-c", pendingC)
	gcIntentsFixtureWriteResetIntent(t, intentsDir, "key-d", noLedgerD)

	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			landedA:  {BeadID: landedA, Status: core.CoarseStatusOpen},
			landedB:  {BeadID: landedB, Status: core.CoarseStatusOpen},
			pendingC: {BeadID: pendingC, Status: core.CoarseStatusInProgress},
			// noLedgerD intentionally absent → ShowBead error → retained
		},
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents mixed: unexpected error: %v", err)
	}
	if result.Removed != 2 {
		t.Errorf("GCRetiredIntents mixed: Removed = %d, want 2", result.Removed)
	}
	if result.Retained != 2 {
		t.Errorf("GCRetiredIntents mixed: Retained = %d, want 2", result.Retained)
	}
}

// TestGCRetiredIntents_SkipsTmpFiles verifies that mid-rename temp files
// (containing ".tmp-" in their name) are skipped regardless of mtime.
func TestGCRetiredIntents_SkipsTmpFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	if err := os.MkdirAll(intentsDir, 0o755); err != nil { //nolint:gosec // G301
		t.Fatalf("GCRetiredIntents tmp: MkdirAll: %v", err)
	}
	tmpPath := filepath.Join(intentsDir, "some_key.json.tmp-abcdef12")
	if err := os.WriteFile(tmpPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("GCRetiredIntents tmp: WriteFile: %v", err)
	}
	past := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(tmpPath, past, past); err != nil {
		t.Fatalf("GCRetiredIntents tmp: Chtimes: %v", err)
	}

	ledger := &fakeIntentGCLedger{records: map[core.BeadID]core.BeadRecord{}}
	daemonStart := time.Now()

	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents tmp: unexpected error: %v", err)
	}
	if result.Removed != 0 || result.Retained != 0 {
		t.Errorf("GCRetiredIntents tmp: want {0,0}, got {%d,%d} (tmp file must be skipped)", result.Removed, result.Retained)
	}
}
