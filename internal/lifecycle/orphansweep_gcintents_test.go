package lifecycle

// orphansweep_gcintents_test.go — unit tests for GCRetiredIntents.
//
// Bead ref: hk-cizvu — orphan-sweep stale_intents_observed GC.
// Bead ref: hk-hf9i8 — retain/remove compare fix + per-boot cap.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// fakeIntentGCLedger is a deterministic IntentGCLedger fake for tests.
// It returns a pre-configured BeadRecord for each bead ID; unknown bead IDs
// return errForUnknown (if set) or fakeShowBeadNotFoundError (a generic
// transient-style error — NOT brcli.ErrBeadNotFound) by default.
type fakeIntentGCLedger struct {
	records       map[core.BeadID]core.BeadRecord
	errForUnknown error // if non-nil, returned for unrecognised bead IDs
}

func (f *fakeIntentGCLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if r, ok := f.records[id]; ok {
		return r, nil
	}
	if f.errForUnknown != nil {
		return core.BeadRecord{}, f.errForUnknown
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
// NOT deleted when the op has not yet landed.
//
// For a reset op (IntendedPostState=open), the bead being "closed" is the
// ambiguous case: the reset may not have run yet (or it ran and the bead was
// re-closed, but that is indistinguishable).  The conservative decision is to
// retain for Cat 3a.  (Note: "in_progress" would mean the reset landed and the
// bead was subsequently claimed — that is a remove case after hk-hf9i8.)
func TestGCRetiredIntents_RetainsWhenPending(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-test-retain")

	intentPath := gcIntentsFixtureWriteResetIntent(t, intentsDir, "proj_hk-test-retain_reset_1", beadID)

	// Ledger reports bead as "closed" — the reset (→ open) has NOT landed yet
	// (or it ran and the bead was re-closed, which is ambiguous).  Either way,
	// the conservative path retains the file for Cat 3a.
	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusClosed},
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
// file is retained when ShowBead returns a TRANSIENT (non-not-found) error
// (conservative Cat-3a path).  Only brcli.ErrBeadNotFound flips to remove;
// all other errors must retain.
func TestGCRetiredIntents_RetainsOnShowBeadError(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-test-showbead-err")

	gcIntentsFixtureWriteResetIntent(t, intentsDir, "proj_hk-test-showbead-err_reset_1", beadID)

	// Explicit transient error — distinct from brcli.ErrBeadNotFound so the
	// not-found special-case is NOT triggered and the intent is retained.
	ledger := &fakeIntentGCLedger{
		records:       map[core.BeadID]core.BeadRecord{},
		errForUnknown: errors.New("transient-br-error"),
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents showbead-err: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("GCRetiredIntents showbead-err: Removed = %d, want 0 (transient ShowBead error → retain)", result.Removed)
	}
	if result.Retained != 1 {
		t.Errorf("GCRetiredIntents showbead-err: Retained = %d, want 1", result.Retained)
	}
}

// TestGCRetiredIntents_RemovesWhenBeadNotFound verifies that a stale intent
// file is REMOVED when ShowBead returns brcli.ErrBeadNotFound (bead purged
// from the ledger).  A purged bead's terminal op is moot — nothing to
// reconcile — so the leftover file is garbage and must be GC'd.
//
// Bead ref: hk-6umeh — intents_gc_d=0 for purged-bead cohort.
func TestGCRetiredIntents_RemovesWhenBeadNotFound(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-purged-bead")

	// Use a close intent to match the real on-disk cohort (all op=close, bead purged).
	intentPath := gcIntentsFixtureWriteIntent(t, intentsDir, "key-close-purged", beadID, "close", "closed")

	// Ledger returns the production sentinel for a missing bead.
	ledger := &fakeIntentGCLedger{
		records:       map[core.BeadID]core.BeadRecord{},
		errForUnknown: brcli.ErrBeadNotFound,
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("GCRetiredIntents purged-bead: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("GCRetiredIntents purged-bead: Removed = %d, want 1 (bead purged → intent is garbage)", result.Removed)
	}
	if result.Retained != 0 {
		t.Errorf("GCRetiredIntents purged-bead: Retained = %d, want 0", result.Retained)
	}
	if _, statErr := os.Stat(intentPath); !os.IsNotExist(statErr) {
		t.Errorf("GCRetiredIntents purged-bead: intent file still exists at %q; must be removed", intentPath)
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
			landedA: {BeadID: landedA, Status: core.CoarseStatusOpen},
			landedB: {BeadID: landedB, Status: core.CoarseStatusOpen},
			// pendingC: reset (→ open) with bead "closed" — ambiguous/not-landed;
			// "in_progress" would now be treated as landed (hk-hf9i8).
			pendingC: {BeadID: pendingC, Status: core.CoarseStatusClosed},
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

// gcIntentsFixtureWriteIntent writes a valid IntentLogEntry of the given op
// to intentsDir/<key>.json with mtime set to past (before daemonStartTime).
// Returns the path of the created file.
func gcIntentsFixtureWriteIntent(t *testing.T, intentsDir, key string, beadID core.BeadID, op string, intendedPostState string) string {
	t.Helper()

	if err := os.MkdirAll(intentsDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("gcIntentsFixtureWriteIntent: MkdirAll: %v", err)
	}

	// For reset op, run_id and transition_id must be zero-valued per BI-010d.
	// For other ops, use non-nil UUIDs.
	runID := "01900000-0000-7000-0000-000000000001"
	transID := "01900000-0000-7000-0000-000000000002"
	if op == "reset" {
		runID = "00000000-0000-0000-0000-000000000000"
		transID = "00000000-0000-0000-0000-000000000000"
	}

	entry := map[string]any{
		"idempotency_key":     key,
		"run_id":              runID,
		"transition_id":       transID,
		"op":                  op,
		"bead_id":             string(beadID),
		"intended_post_state": intendedPostState,
		"requested_at":        "2024-01-01T00:00:00Z",
		"schema_version":      1,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("gcIntentsFixtureWriteIntent: marshal: %v", err)
	}

	path := filepath.Join(intentsDir, key+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("gcIntentsFixtureWriteIntent: WriteFile: %v", err)
	}

	past := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("gcIntentsFixtureWriteIntent: Chtimes: %v", err)
	}
	return path
}

// TestGCRetiredIntents_ClaimBeadAdvancedToClosed is the primary regression test
// for hk-hf9i8: a claim intent (IntendedPostState=in_progress) whose bead has
// since advanced to closed was wrongly RETAINED by the old exact-equality
// check.  After the fix, gcIntentOpLanded returns true for claim+closed and
// the file must be REMOVED.
func TestGCRetiredIntents_ClaimBeadAdvancedToClosed(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-claim-closed")

	intentPath := gcIntentsFixtureWriteIntent(t, intentsDir, "key-claim-closed", beadID, "claim", "in_progress")

	// Ledger reports bead as "closed" — it was claimed then closed.
	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusClosed},
		},
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1 (claim intent, bead advanced to closed → should remove)", result.Removed)
	}
	if result.Retained != 0 {
		t.Errorf("Retained = %d, want 0", result.Retained)
	}
	if _, statErr := os.Stat(intentPath); !os.IsNotExist(statErr) {
		t.Errorf("intent file still exists at %q; should have been deleted", intentPath)
	}
}

// TestGCRetiredIntents_ClaimBeadStillOpen verifies that a claim intent where
// the bead is still open (op has NOT landed) is RETAINED.
func TestGCRetiredIntents_ClaimBeadStillOpen(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	beadID := core.BeadID("hk-claim-open")

	intentPath := gcIntentsFixtureWriteIntent(t, intentsDir, "key-claim-open", beadID, "claim", "in_progress")

	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusOpen},
		},
	}

	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("Removed = %d, want 0 (bead still open → claim not landed, must retain)", result.Removed)
	}
	if result.Retained != 1 {
		t.Errorf("Retained = %d, want 1", result.Retained)
	}
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Errorf("intent file was removed; must be retained (claim not yet landed)")
	}
}

// TestGCRetiredIntents_Cap verifies that when more than gcRetiredIntentsMaxScan
// stale files exist, the excess are deferred (result.Skipped) and left on disk.
func TestGCRetiredIntents_Cap(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	// Create gcRetiredIntentsMaxScan + 5 stale intent files, all "landed"
	// (claim op, bead = closed).  The first gcRetiredIntentsMaxScan must be
	// removed; the remaining 5 must be skipped (deferred to next boot).
	total := gcRetiredIntentsMaxScan + 5
	ledgerRecords := make(map[core.BeadID]core.BeadRecord, total)
	for i := 0; i < total; i++ {
		id := core.BeadID(fmt.Sprintf("hk-cap-%04d", i))
		key := fmt.Sprintf("key-cap-%04d", i)
		gcIntentsFixtureWriteIntent(t, intentsDir, key, id, "claim", "in_progress")
		ledgerRecords[id] = core.BeadRecord{BeadID: id, Status: core.CoarseStatusClosed}
	}

	ledger := &fakeIntentGCLedger{records: ledgerRecords}
	daemonStart := time.Now()
	result, err := GCRetiredIntents(context.Background(), projectDir, daemonStart, ledger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != gcRetiredIntentsMaxScan {
		t.Errorf("Removed = %d, want %d", result.Removed, gcRetiredIntentsMaxScan)
	}
	if result.Skipped != 5 {
		t.Errorf("Skipped = %d, want 5", result.Skipped)
	}
	if result.Retained != 0 {
		t.Errorf("Retained = %d, want 0", result.Retained)
	}
}

// TestGcIntentOpLanded covers all op/status combinations for gcIntentOpLanded.
func TestGcIntentOpLanded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op                string
		currentStatus     core.CoarseStatus
		intendedPostState core.CoarseStatus
		wantLanded        bool
	}{
		// claim (→ in_progress): landed if bead ≠ open
		{"claim", core.CoarseStatusInProgress, core.CoarseStatusInProgress, true}, // exact match
		{"claim", core.CoarseStatusClosed, core.CoarseStatusInProgress, true},     // advanced past
		{"claim", core.CoarseStatusTombstone, core.CoarseStatusInProgress, true},  // advanced past
		{"claim", core.CoarseStatusOpen, core.CoarseStatusInProgress, false},      // not claimed yet
		// close (→ closed): landed if status = closed or tombstone
		{"close", core.CoarseStatusClosed, core.CoarseStatusClosed, true},      // exact match
		{"close", core.CoarseStatusTombstone, core.CoarseStatusClosed, true},   // advanced past
		{"close", core.CoarseStatusOpen, core.CoarseStatusClosed, false},       // ambiguous
		{"close", core.CoarseStatusInProgress, core.CoarseStatusClosed, false}, // not closed yet
		// reopen (→ open): landed if status = open, in_progress, or tombstone
		{"reopen", core.CoarseStatusOpen, core.CoarseStatusOpen, true},       // exact match
		{"reopen", core.CoarseStatusInProgress, core.CoarseStatusOpen, true}, // advanced past
		{"reopen", core.CoarseStatusTombstone, core.CoarseStatusOpen, true},  // advanced past
		{"reopen", core.CoarseStatusClosed, core.CoarseStatusOpen, false},    // ambiguous
		// reset (→ open): same rules as reopen
		{"reset", core.CoarseStatusOpen, core.CoarseStatusOpen, true},
		{"reset", core.CoarseStatusInProgress, core.CoarseStatusOpen, true},
		{"reset", core.CoarseStatusTombstone, core.CoarseStatusOpen, true},
		{"reset", core.CoarseStatusClosed, core.CoarseStatusOpen, false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("op=%s/status=%s", tc.op, tc.currentStatus), func(t *testing.T) {
			t.Parallel()
			got := gcIntentOpLanded(core.TerminalOp(tc.op), tc.currentStatus, tc.intendedPostState)
			if got != tc.wantLanded {
				t.Errorf("gcIntentOpLanded(op=%q, status=%q, intended=%q) = %v, want %v",
					tc.op, tc.currentStatus, tc.intendedPostState, got, tc.wantLanded)
			}
		})
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
