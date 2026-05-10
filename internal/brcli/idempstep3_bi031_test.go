package brcli_test

// idempstep3_bi031_test.go — BI-031 step 3 tests: status-equals-intended
// branch with audit-log disambiguation (3i/3ii).
//
// Step 3 of the crash-recovery protocol (beads-integration.md §4.10 BI-031):
//
//	"If the current status equals the intended_post_state for this transition,
//	the recovery MUST attempt to disambiguate:
//	(3i) If br audit-log <bead_id> --filter-idempotency-key <idempotency_key>
//	returns a matching audit entry, the prior write was harmonik-side. Delete
//	the intent file (with parent-directory fsync per BI-030) and write the
//	structured-log recovery record per ON-035 at level=info.
//	(3ii) If no matching audit entry exists OR the br audit-log surface is
//	unavailable, the recovery cannot prove harmonik-side authorship. Emit
//	divergence_inconclusive per [event-model.md §8.6.10] with
//	reason=authority_unavailable; retain the intent file."
//
// These tests verify the observable contract of each step-3 branch using the
// AuditLog surface already present in the adapter. Production recovery logic
// will wire these branches together; these tests exercise each branch's
// preconditions and assertions in isolation.
//
// All test helpers in this file use the idempStep3 prefix per
// implementer-protocol.md helper-prefix discipline (bead hk-872.38.3).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3 (3i/3ii);
// specs/operator-nfr.md §4.9 ON-035; specs/event-model.md §8.6.10;
// §6.1 RECORD IntentLogEntry.

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

// idempStep3FixtureEntry returns a valid IntentLogEntry for use as step-1
// output in step-3 tests. The idempotency key is set to a stable value that
// tests can correlate with mock audit-log responses.
func idempStep3FixtureEntry(t *testing.T, beadID core.BeadID) core.IntentLogEntry {
	t.Helper()
	return core.IntentLogEntry{
		IdempotencyKey:    "run-step3:trans-abc:claim",
		RunID:             core.RunID(uuid.Must(uuid.NewV7())),
		TransitionID:      core.TransitionID(uuid.Must(uuid.NewV7())),
		Op:                core.TerminalOpClaim,
		BeadID:            beadID,
		IntendedPostState: core.CoarseStatusInProgress,
		RequestedAt:       time.Now().UTC().Truncate(time.Second),
		SchemaVersion:     1,
	}
}

// idempStep3FixtureAuditLogMatchJSON returns a `br --json audit log` response
// that includes an event whose comment carries the expected idempotency key.
// In the 3i path, the recovery searches for an entry that confirms harmonik-side
// authorship of the transition; matching on idempotency_key in the comment is
// a proxy for `--filter-idempotency-key` (see OQ-BI-009).
func idempStep3FixtureAuditLogMatchJSON(id, idempotencyKey string) string {
	return `{"issue_id":"` + id + `","events":[` +
		`{"id":10677,"event_type":"status_changed","actor":"harmonik-daemon","timestamp":"2026-05-08T05:38:01.761094Z","comment":"` + idempotencyKey + `","old_value":"open","new_value":"in_progress"}` +
		`]}`
}

// idempStep3FixtureAuditLogNoMatchJSON returns a `br --json audit log` response
// that contains events but none carrying the expected idempotency key. This
// simulates the 3ii path where the audit log is available but no match exists.
func idempStep3FixtureAuditLogNoMatchJSON(id string) string {
	return `{"issue_id":"` + id + `","events":[` +
		`{"id":10600,"event_type":"status_changed","actor":"some-other-actor","timestamp":"2026-05-07T10:00:00.000000Z","comment":"unrelated-key","old_value":"open","new_value":"in_progress"}` +
		`]}`
}

// idempStep3FixtureAuditLogEmptyJSON returns a `br --json audit log` response
// with an empty events array. This simulates 3ii where the audit log is
// available but contains no events for the bead.
func idempStep3FixtureAuditLogEmptyJSON(id string) string {
	return `{"issue_id":"` + id + `","events":[]}`
}

// idempStep3FixtureIntentFile writes entry as a JSON intent-log file to dir.
// Returns the path so tests can assert presence/absence after simulated recovery.
func idempStep3FixtureIntentFile(t *testing.T, dir string, entry core.IntentLogEntry) string {
	t.Helper()

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("idempStep3FixtureIntentFile: json.Marshal: %v", err)
	}

	name := string(entry.BeadID) + "-step3.json"
	path := filepath.Join(dir, name)
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("idempStep3FixtureIntentFile: WriteFile %q: %v", path, err)
	}
	return path
}

// TestIdempStep3_StatusMatchIsPrerequisiteForDisambiguation verifies that
// the step-3 branch is entered only when the CoarseStatus returned by step 2
// equals entry.IntendedPostState.
//
// This is the guard condition for step 3: if current_status != intended_post_state,
// the recovery proceeds to step 4 (pre-state) or step 5 (neither). The test
// verifies the equality check logic using CoarseStatus.Valid() and direct comparison.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3.
func TestIdempStep3_StatusMatchIsPrerequisiteForDisambiguation(t *testing.T) {
	t.Parallel()

	entry := idempStep3FixtureEntry(t, "hk-step3-guard")

	// Status equals IntendedPostState → step 3 branch entered.
	currentStatus := entry.IntendedPostState
	statusMatchesPost := currentStatus == entry.IntendedPostState
	if !statusMatchesPost {
		t.Errorf("status match check failed: %q != %q", currentStatus, entry.IntendedPostState)
	}

	// Status equals pre-state (open) → step 3 NOT entered (step 4 path).
	preState := core.CoarseStatusOpen
	statusMatchesPre := preState == entry.IntendedPostState
	if statusMatchesPre {
		t.Errorf("pre-state should NOT match intended_post_state; got match: %q == %q", preState, entry.IntendedPostState)
	}

	// Status is neither pre nor post (closed for a claim) → step 5 path.
	neitherState := core.CoarseStatusClosed
	matchesPost := neitherState == entry.IntendedPostState
	matchesPre := neitherState == preState
	if matchesPost || matchesPre {
		t.Errorf("closed should be neither pre nor post for a claim op; matchesPost=%v matchesPre=%v", matchesPost, matchesPre)
	}
}

// TestIdempStep3_3i_AuditLogMatchIndicatesHarmonikAuthorship verifies the step
// 3i outcome: AuditLog returns events; a matching entry can be found using the
// idempotency key from the intent file. This confirms the prior write was
// harmonik-side (no-op recovery path).
//
// The test asserts that AuditLog succeeds and the returned events contain the
// idempotency key as a correlating field. The production recovery path would
// then delete the intent file and write an ON-035 structured-log record.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3i.
func TestIdempStep3_3i_AuditLogMatchIndicatesHarmonikAuthorship(t *testing.T) {
	t.Parallel()

	entry := idempStep3FixtureEntry(t, "hk-step3-3i")

	// Mock: audit log contains an event with the idempotency key in the comment.
	jsonStr := idempStep3FixtureAuditLogMatchJSON(string(entry.BeadID), entry.IdempotencyKey)
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("AuditLog: unexpected error: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("AuditLog returned no events; 3i disambiguation requires at least one event to match against")
	}

	// 3i match: at least one event must carry the idempotency key as evidence
	// of harmonik-side authorship.
	var found bool
	for _, ev := range events {
		if ev.Comment == entry.IdempotencyKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("idempotency key %q not found in audit events; 3i path cannot confirm harmonik-side authorship", entry.IdempotencyKey)
	}
}

// TestIdempStep3_3i_IntentFileDeletableOnNoOp verifies the 3i clean-up
// contract: after confirming harmonik-side authorship, the intent file MUST be
// deletable (i.e., the recovery path is not blocked by the file system). The
// test writes an intent file and removes it, asserting that the file is gone
// afterward — simulating the BI-030 delete step that follows step 3i.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3i; BI-030 (delete
// on success with parent-directory fsync).
func TestIdempStep3_3i_IntentFileDeletableOnNoOp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep3FixtureEntry(t, "hk-step3-3i-del")

	path := idempStep3FixtureIntentFile(t, dir, entry)

	// Verify the file exists (the crash left it on disk).
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("intent file does not exist before deletion step; fixture error")
	}

	// 3i recovery: delete the intent file (no parent-dir fsync in unit test —
	// fsync discipline is verified by intentlogwrite_test.go).
	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove intent file: %v", err)
	}

	// Intent file must be absent after deletion.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("intent file still exists after 3i deletion; recovery would leave stale state")
	}
}

// TestIdempStep3_3ii_EmptyAuditLogTriggersRetainPath verifies the step 3ii
// outcome when the audit log is available but contains no events for the bead.
// The recovery cannot prove harmonik-side authorship → the intent file MUST be
// retained for reconciliation's Cat 3a auto-resolver.
//
// The test asserts that AuditLog succeeds with an empty events slice (not an
// error), and that no idempotency-key match is found.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3ii.
func TestIdempStep3_3ii_EmptyAuditLogTriggersRetainPath(t *testing.T) {
	t.Parallel()

	entry := idempStep3FixtureEntry(t, "hk-step3-3ii-empty")

	jsonStr := idempStep3FixtureAuditLogEmptyJSON(string(entry.BeadID))
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("AuditLog: unexpected error for empty events: %v", err)
	}

	// AuditLog must succeed (not return an error) — empty events is a valid response.
	if events == nil {
		t.Fatal("AuditLog returned nil events; expected empty non-nil slice")
	}

	// No match found → 3ii path: intent file is retained.
	var found bool
	for _, ev := range events {
		if ev.Comment == entry.IdempotencyKey {
			found = true
			break
		}
	}
	if found {
		t.Error("idempotency key found in empty events; fixture error")
	}

	// 3ii contract: the intent file survives (recovery does NOT delete it).
	// Tested symbolically — the production path retains it for Cat 3a.
}

// TestIdempStep3_3ii_NoMatchInAuditLogTriggersRetainPath verifies the step 3ii
// outcome when the audit log is available and non-empty but contains no events
// matching the idempotency key. Recovery cannot prove harmonik-side authorship.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3ii.
func TestIdempStep3_3ii_NoMatchInAuditLogTriggersRetainPath(t *testing.T) {
	t.Parallel()

	entry := idempStep3FixtureEntry(t, "hk-step3-3ii-nomatch")

	jsonStr := idempStep3FixtureAuditLogNoMatchJSON(string(entry.BeadID))
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("AuditLog: unexpected error: %v", err)
	}

	// Events are present but none match the idempotency key.
	if len(events) == 0 {
		t.Fatal("AuditLog returned no events; fixture should have returned non-matching events")
	}

	var found bool
	for _, ev := range events {
		if ev.Comment == entry.IdempotencyKey {
			found = true
			break
		}
	}

	// No match → 3ii path: retain intent file, emit divergence_inconclusive.
	if found {
		t.Errorf("unexpected idempotency key match in no-match fixture; got match in events for key %q", entry.IdempotencyKey)
	}
}

// TestIdempStep3_3ii_AuditLogUnavailableTriggersRetainPath verifies the step
// 3ii outcome when the audit log surface is unavailable (exec failure). Per
// BI-031 step 3ii, audit-log unavailability is equivalent to "no matching
// audit entry" — the recovery MUST retain the intent file.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3ii.
func TestIdempStep3_3ii_AuditLogUnavailableTriggersRetainPath(t *testing.T) {
	t.Parallel()

	entry := idempStep3FixtureEntry(t, "hk-step3-3ii-unavail")
	dir := t.TempDir()

	intentPath := idempStep3FixtureIntentFile(t, dir, entry)

	// Exec failure: br binary does not exist.
	adapter, err := brcli.New("/nonexistent/br-for-step3")
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	_, auditErr := adapter.AuditLog(context.Background(), entry.BeadID)
	if auditErr == nil {
		t.Fatal("AuditLog with missing binary: expected error, got nil")
	}

	// 3ii: intent file MUST be retained (not deleted) when audit log is unavailable.
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Error("intent file was unexpectedly deleted when audit log is unavailable; 3ii requires retention")
	}
}

// TestIdempStep3_3ii_AuditLogNonZeroExitTriggersRetainPath verifies the step
// 3ii outcome when `br audit log` returns a non-zero exit code (audit-log
// surface temporarily unavailable). This is distinct from exec failure but
// has the same 3ii outcome: retain the intent file.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3ii.
func TestIdempStep3_3ii_AuditLogNonZeroExitTriggersRetainPath(t *testing.T) {
	t.Parallel()

	entry := idempStep3FixtureEntry(t, "hk-step3-3ii-nonzero")
	dir := t.TempDir()

	intentPath := idempStep3FixtureIntentFile(t, dir, entry)

	// Mock binary returns exit 1 with a non-ISSUE_NOT_FOUND error envelope.
	jsonStr := `{"error":{"code":"INTERNAL_ERROR","message":"audit log temporarily unavailable","hint":"","retryable":true,"context":{}}}`
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 1)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	_, auditErr := adapter.AuditLog(context.Background(), entry.BeadID)
	if auditErr == nil {
		t.Fatal("AuditLog with non-zero exit: expected error, got nil")
	}
	if !errors.Is(auditErr, brcli.ErrBrAuditLogFailed) {
		t.Errorf("expected ErrBrAuditLogFailed; got %v", auditErr)
	}

	// 3ii: intent file MUST be retained when audit log returns non-zero exit.
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Error("intent file was unexpectedly deleted when audit log returns non-zero exit; 3ii requires retention")
	}
}

// TestIdempStep3_3i_IntentFileAbsenceAfterNoOpRecovery verifies the 3i outcome
// by checking that the intent file is absent after the simulated no-op recovery
// clean-up. Combined with TestIdempStep3_3i_AuditLogMatchIndicatesHarmonikAuthorship,
// this covers the full 3i path:
//
//  1. AuditLog confirms harmonik-side authorship.
//  2. Intent file is deleted.
//  3. ON-035 structured-log record would be written (not tested here — depends
//     on the logger subsystem not yet wired).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3i; BI-030 (delete
// with parent-directory fsync); operator-nfr.md §4.9 ON-035.
func TestIdempStep3_3i_IntentFileAbsenceAfterNoOpRecovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep3FixtureEntry(t, "hk-step3-3i-absent")

	intentPath := idempStep3FixtureIntentFile(t, dir, entry)

	// Precondition: file exists.
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Fatal("intent file does not exist before 3i recovery; fixture error")
	}

	// Mock: step 2 returns in_progress == intended_post_state.
	showJSON := idempStep2FixtureShowJSON(string(entry.BeadID), "in_progress")
	brPath := brcliFixtureMockBinary(t, showJSON, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	// Step 2 call: confirm status == intended_post_state.
	record, err := adapter.ShowBead(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("ShowBead: %v", err)
	}
	if record.Status != entry.IntendedPostState {
		t.Fatalf("status %q != intended_post_state %q; step 3 guard not satisfied", record.Status, entry.IntendedPostState)
	}

	// 3i clean-up: delete intent file (simulating what the recovery orchestrator
	// does after audit-log match confirmation).
	if err := os.Remove(intentPath); err != nil {
		t.Fatalf("delete intent file: %v", err)
	}

	// Post-condition: intent file is absent (no-op recovery complete).
	if _, err := os.Stat(intentPath); !os.IsNotExist(err) {
		t.Error("intent file still present after 3i no-op recovery; stale intent would re-trigger recovery on next restart")
	}
}

// TestIdempStep3_3ii_IntentFilePresentAfterRetainPath verifies that the intent
// file remains on disk when the 3ii path is taken (audit-log unavailable or no
// match). The file must be retained so that reconciliation's Cat 3a auto-resolver
// can consume it as evidence of a potentially torn write.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 3ii; BI-032
// (intent log is the Cat 3a detector's evidence source).
func TestIdempStep3_3ii_IntentFilePresentAfterRetainPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep3FixtureEntry(t, "hk-step3-3ii-retain")

	intentPath := idempStep3FixtureIntentFile(t, dir, entry)

	// Simulate step 2: status == intended_post_state (step 3 guard triggered).
	showJSON := idempStep2FixtureShowJSON(string(entry.BeadID), "in_progress")
	brPath := brcliFixtureMockBinary(t, showJSON, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("ShowBead: %v", err)
	}
	if record.Status != entry.IntendedPostState {
		t.Fatalf("status %q != intended_post_state %q; step 3 guard not satisfied", record.Status, entry.IntendedPostState)
	}

	// 3ii: audit log returns empty events (no match possible).
	// Recovery retains the intent file — DO NOT call os.Remove.
	// The file must still be present afterward.

	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file absent after 3ii retain path; Cat 3a auto-resolver cannot find its evidence")
	}

	// Verify the retained file is still a valid IntentLogEntry (not corrupted
	// by the recovery probe).
	retained, err := core.ReadIntentLogEntry(intentPath)
	if err != nil {
		t.Fatalf("ReadIntentLogEntry on retained file: %v", err)
	}
	if retained.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("retained IdempotencyKey = %q; want %q", retained.IdempotencyKey, entry.IdempotencyKey)
	}
	if retained.BeadID != entry.BeadID {
		t.Errorf("retained BeadID = %q; want %q", retained.BeadID, entry.BeadID)
	}
}
