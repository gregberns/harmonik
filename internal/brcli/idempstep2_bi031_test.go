package brcli_test

// idempstep2_bi031_test.go — BI-031 step 2 tests: query Beads via
// `br show <bead_id>` to read the bead's current coarse_status.
//
// Step 2 of the crash-recovery protocol (beads-integration.md §4.10 BI-031):
//
//	"Query Beads via `br show <bead_id>` (using the timeout discipline of
//	BI-025c and the JSON mode of BI-025b) to read the bead's current
//	coarse_status."
//
// Tests here verify the bridge between step 1 (ReadIntentLogEntry returns an
// entry with a BeadID) and step 2 (ShowBead called with that BeadID returns
// the current CoarseStatus). They also cover the two mandatory error paths:
// BrSchemaMismatch on JSON parse failure (per BI-031b) and BrUnavailable on
// exec failure (per BI-025c / BI-025a).
//
// All test helpers in this file use the idempStep2 prefix per
// implementer-protocol.md helper-prefix discipline (bead hk-872.38.2).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 2; BI-031b; BI-025a;
// BI-025b; BI-025c; §6.1 RECORD IntentLogEntry.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// idempStep2FixtureEntry returns a fully-populated core.IntentLogEntry for use
// as step-1 output in step-2 tests.  All required fields are populated; the
// BeadID is set to beadID so that tests can parameterise the target bead.
func idempStep2FixtureEntry(t *testing.T, beadID core.BeadID) core.IntentLogEntry {
	t.Helper()
	return core.IntentLogEntry{
		IdempotencyKey:    "run-step2:trans-xyz:claim",
		RunID:             core.RunID(uuid.Must(uuid.NewV7())),
		TransitionID:      core.TransitionID(uuid.Must(uuid.NewV7())),
		Op:                core.TerminalOpClaim,
		BeadID:            beadID,
		IntendedPostState: core.CoarseStatusInProgress,
		RequestedAt:       time.Now().UTC().Truncate(time.Second),
		SchemaVersion:     1,
	}
}

// idempStep2FixtureEntryFile writes entry as a JSON intent-log file to dir and
// returns the path.  It mirrors the production adapter's on-disk layout so that
// tests can exercise the ReadIntentLogEntry → ShowBead chain without a real
// daemon.
func idempStep2FixtureEntryFile(t *testing.T, dir string, entry core.IntentLogEntry) string {
	t.Helper()

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("idempStep2FixtureEntryFile: json.Marshal: %v", err)
	}

	name := string(entry.BeadID) + ".json"
	path := filepath.Join(dir, name)
	//nolint:gosec // G306: test fixture; intent files are readable by the daemon user only in production
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("idempStep2FixtureEntryFile: WriteFile %q: %v", path, err)
	}
	return path
}

// idempStep2FixtureShowJSON returns the canonical `br show --format json`
// output for a bead with the given ID and status.
func idempStep2FixtureShowJSON(id string, status string) string {
	return `[{"id":"` + id + `","title":"Test bead","description":"Step 2 fixture.","status":"` + status + `","issue_type":"task","dependencies":[],"dependents":[],"parent":""}]`
}

// TestIdempStep2_ShowBeadUsesEntryBeadID verifies the step-1 → step-2 chain:
// given an IntentLogEntry produced by step 1 (ReadIntentLogEntry), ShowBead is
// called with entry.BeadID and returns the current CoarseStatus.
//
// This is the core obligation of BI-031 step 2: the crash-recovery path reads
// the BeadID from the intent file (step 1) and uses it as the argument to
// `br show <bead_id>` (step 2).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 2.
func TestIdempStep2_ShowBeadUsesEntryBeadID(t *testing.T) {
	t.Parallel()

	targetID := core.BeadID("hk-step2-target")

	// Step 1 output: an IntentLogEntry describing the pending write.
	entry := idempStep2FixtureEntry(t, targetID)
	if !entry.Valid() {
		t.Fatal("fixture entry is not valid; test setup error")
	}

	// Step 2: query Beads with entry.BeadID.
	// The mock binary returns "open" (the pre-state for a claim operation).
	jsonStr := idempStep2FixtureShowJSON(string(entry.BeadID), "open")
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("ShowBead(entry.BeadID): unexpected error: %v", err)
	}

	// BeadID must round-trip: step 2 must query the exact ID from the intent file.
	if record.BeadID != entry.BeadID {
		t.Errorf("record.BeadID = %q; want %q (entry.BeadID)", record.BeadID, entry.BeadID)
	}

	// The returned CoarseStatus is the pre-state (open), confirming the prior
	// write did not land — step 2 read succeeded.
	if record.Status != core.CoarseStatusOpen {
		t.Errorf("record.Status = %q; want %q (open = pre-state for claim)", record.Status, core.CoarseStatusOpen)
	}
}

// TestIdempStep2_StatusEqualsIntendedPostState verifies that ShowBead correctly
// reports CoarseStatusInProgress when the bead is already in the intended
// post-state.  This is the status-match case (BI-031 step 3): the prior write
// landed before the crash.
//
// Step 2 must return the status faithfully so that step 3 can detect the match.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 2; step 3.
func TestIdempStep2_StatusEqualsIntendedPostState(t *testing.T) {
	t.Parallel()

	targetID := core.BeadID("hk-step2-post")
	entry := idempStep2FixtureEntry(t, targetID)

	// The bead's status is already in_progress — the prior claim write landed.
	jsonStr := idempStep2FixtureShowJSON(string(entry.BeadID), "in_progress")
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	// Step 2 returns in_progress: step 3 will detect this matches
	// entry.IntendedPostState and proceed to audit-log disambiguation.
	if record.Status != core.CoarseStatusInProgress {
		t.Errorf("record.Status = %q; want %q (in_progress = IntendedPostState for claim)", record.Status, core.CoarseStatusInProgress)
	}
	if record.Status != entry.IntendedPostState {
		t.Errorf("record.Status = %q; want entry.IntendedPostState = %q", record.Status, entry.IntendedPostState)
	}
}

// TestIdempStep2_AllCoarseStatusValues verifies that ShowBead correctly decodes
// every valid CoarseStatus value that `br show` may return.  The recovery path
// must faithfully surface whatever status Beads reports; step 3 and step 5
// branch on the returned value.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 2; step 3; step 5.
func TestIdempStep2_AllCoarseStatusValues(t *testing.T) {
	t.Parallel()

	statuses := []struct {
		jsonVal string
		want    core.CoarseStatus
	}{
		{"open", core.CoarseStatusOpen},
		{"in_progress", core.CoarseStatusInProgress},
		{"closed", core.CoarseStatusClosed},
	}

	for _, tc := range statuses {
		tc := tc
		t.Run(tc.jsonVal, func(t *testing.T) {
			t.Parallel()

			jsonStr := idempStep2FixtureShowJSON("hk-step2-all", tc.jsonVal)
			brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

			adapter, err := brcli.New(brPath)
			if err != nil {
				t.Fatalf("brcli.New: %v", err)
			}

			record, err := adapter.ShowBead(context.Background(), "hk-step2-all")
			if err != nil {
				t.Fatalf("ShowBead for status %q: unexpected error: %v", tc.jsonVal, err)
			}
			if record.Status != tc.want {
				t.Errorf("record.Status = %q; want %q", record.Status, tc.want)
			}
		})
	}
}

// TestIdempStep2_ParseFailureClassifiedAsBrSchemaMismatch verifies the BI-031b
// requirement: any parse failure on the `br show --format json` output MUST
// classify as BrSchemaMismatch, NOT as "status differs."  The recovery path
// must refuse the reissue on BrSchemaMismatch.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
func TestIdempStep2_ParseFailureClassifiedAsBrSchemaMismatch(t *testing.T) {
	t.Parallel()

	entry := idempStep2FixtureEntry(t, "hk-step2-schema")

	// Mock binary returns malformed JSON — simulates schema drift or a binary
	// that writes to stdout in an unexpected format.
	brPath := brcliFixtureMockBinary(t, `not-valid-json`, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), entry.BeadID)
	if err == nil {
		t.Fatal("ShowBead with malformed JSON: expected error, got nil")
	}

	// BI-031b: parse failure MUST classify as BrSchemaMismatch.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false; got %v; BI-031b requires BrSchemaMismatch classification", err)
	}
}

// TestIdempStep2_ExecFailureOnMissingBinary verifies that when `br` is
// unavailable (binary not found), ShowBead returns an error.  In the recovery
// path, this corresponds to BrUnavailable — the daemon retains the intent file
// and degrades per BI-031 step 4d.
//
// This test asserts only that an error is returned; the BrUnavailable
// classification for exec failures is verified by the RunWithTimeout tests.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 2; step 4d.
func TestIdempStep2_ExecFailureOnMissingBinary(t *testing.T) {
	t.Parallel()

	entry := idempStep2FixtureEntry(t, "hk-step2-unavail")

	adapter, err := brcli.New("/nonexistent/br-binary")
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), entry.BeadID)
	if err == nil {
		t.Fatal("ShowBead with missing binary: expected error, got nil")
	}
}

// TestIdempStep2_ChainFromIntentFile verifies the full step-1 → step-2 data
// flow: ReadIntentLogEntry reads a persisted file (step 1), then ShowBead is
// called with the resulting entry.BeadID (step 2).  The test confirms that the
// BeadID from the file is correctly threaded through to the `br show` call.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 1; step 2.
func TestIdempStep2_ChainFromIntentFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	beadID := core.BeadID("hk-step2-chain")
	entry := idempStep2FixtureEntry(t, beadID)

	// Step 1: write and read back the intent file (simulates adapter restart).
	intentPath := idempStep2FixtureEntryFile(t, dir, entry)
	recovered, err := core.ReadIntentLogEntry(intentPath)
	if err != nil {
		t.Fatalf("ReadIntentLogEntry: %v", err)
	}
	if recovered.BeadID != beadID {
		t.Fatalf("ReadIntentLogEntry: BeadID = %q; want %q", recovered.BeadID, beadID)
	}

	// Step 2: query Beads using recovered.BeadID.
	jsonStr := idempStep2FixtureShowJSON(string(recovered.BeadID), "open")
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), recovered.BeadID)
	if err != nil {
		t.Fatalf("ShowBead(recovered.BeadID): %v", err)
	}

	// The queried ID must match the recovered entry's BeadID — the chain is intact.
	if record.BeadID != recovered.BeadID {
		t.Errorf("record.BeadID = %q; want %q (recovered.BeadID)", record.BeadID, recovered.BeadID)
	}

	// The returned CoarseStatus must be a valid value so step 3 can branch on it.
	if !record.Status.Valid() {
		t.Errorf("record.Status = %q; Valid() = false; step 3 cannot branch on invalid status", record.Status)
	}
}
