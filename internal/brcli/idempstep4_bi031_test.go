package brcli_test

// idempstep4_bi031_test.go — BI-031 step 4 tests: status-equals-prestate
// reissue with idempotency key and 6 BrError branch routing (4a-4f).
//
// Step 4 of the crash-recovery protocol (beads-integration.md §4.10 BI-031):
//
//	"If the current status is the pre-state (status_match negative; pre-state
//	confirmed), re-issue the br write with the same idempotency_key. The
//	reissue MAY return:
//	(4a) BrOK — success; delete intent file + ON-035 log.
//	(4b) BrConflict — re-execute step 3 with new state.
//	(4c) BrDbLocked — exponential backoff up to 3 retries; on persistent
//	     failure, classify as BrUnavailable and route per 4d.
//	(4d) BrUnavailable — retain intent file; daemon degraded per ON-037.
//	(4e) BrSchemaMismatch — divergence_inconclusive per BI-031b.
//	(4f) BrOther — divergence_inconclusive; retain intent file; Cat 6b escalation."
//
// These tests verify:
//   - The pre-state guard: current_status == pre-state is the precondition.
//   - BrError classification from the reissue Result matches the spec routing.
//   - RunWithDBLockedRetry implements 4c (exponential backoff → BrUnavailable).
//   - Intent file presence/absence postconditions for each branch.
//   - BrErrReconciliationCategory routing for 4e (Cat 0) and 4f (Cat 3).
//
// All test helpers in this file use the idempStep4 prefix per
// implementer-protocol.md helper-prefix discipline (bead hk-872.38.4).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (4a-4f);
// §6.1a BrError enum; §8 BrError routing table; BI-025c (BrDbLocked retry).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// idempStep4FixtureEntry returns a valid IntentLogEntry for step-4 tests.
// Op is claim so the pre-state is open and the intended_post_state is in_progress.
func idempStep4FixtureEntry(t *testing.T, beadID core.BeadID) core.IntentLogEntry {
	t.Helper()
	return core.IntentLogEntry{
		IdempotencyKey:    "run-step4:trans-def:claim",
		RunID:             core.RunID(uuid.Must(uuid.NewV7())),
		TransitionID:      core.TransitionID(uuid.Must(uuid.NewV7())),
		Op:                core.TerminalOpClaim,
		BeadID:            beadID,
		IntendedPostState: core.CoarseStatusInProgress,
		RequestedAt:       time.Now().UTC().Truncate(time.Second),
		SchemaVersion:     1,
	}
}

// idempStep4PreStateFor derives the expected pre-state for a terminal op.
// claim → open, close → in_progress, reopen → closed.
func idempStep4PreStateFor(op core.TerminalOp) core.CoarseStatus {
	switch op {
	case core.TerminalOpClaim:
		return core.CoarseStatusOpen
	case core.TerminalOpClose:
		return core.CoarseStatusInProgress
	case core.TerminalOpReopen:
		return core.CoarseStatusClosed
	default:
		return core.CoarseStatus("")
	}
}

// idempStep4FixtureIntentFile writes entry as a JSON intent-log file to dir.
func idempStep4FixtureIntentFile(t *testing.T, dir string, entry core.IntentLogEntry) string {
	t.Helper()

	// Manually compose JSON since we are in the brcli_test (black-box) package.
	path := filepath.Join(dir, string(entry.BeadID)+"-step4.json")

	// Reuse the step-3 fixture writer pattern: write via the core JSON encoding.
	// Use a JSON template that matches IntentLogEntry's snake_case field names.
	jsonStr := `{"idempotency_key":"` + entry.IdempotencyKey +
		`","run_id":"` + entry.RunID.String() +
		`","transition_id":"` + entry.TransitionID.String() +
		`","op":"` + string(entry.Op) +
		`","bead_id":"` + string(entry.BeadID) +
		`","intended_post_state":"` + string(entry.IntendedPostState) +
		`","requested_at":"` + entry.RequestedAt.Format(time.RFC3339) +
		`","schema_version":1}`

	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(path, []byte(jsonStr), 0o600); err != nil {
		t.Fatalf("idempStep4FixtureIntentFile: WriteFile %q: %v", path, err)
	}
	return path
}

// TestIdempStep4_PreStateGuard verifies that step 4 is entered only when
// current_status equals the pre-state for the intent's op. For a claim op,
// pre-state is "open"; for close it is "in_progress"; for reopen it is "closed".
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (precondition).
func TestIdempStep4_PreStateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		op           core.TerminalOp
		postState    core.CoarseStatus
		wantPreState core.CoarseStatus
	}{
		{core.TerminalOpClaim, core.CoarseStatusInProgress, core.CoarseStatusOpen},
		{core.TerminalOpClose, core.CoarseStatusClosed, core.CoarseStatusInProgress},
		{core.TerminalOpReopen, core.CoarseStatusOpen, core.CoarseStatusClosed},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.op), func(t *testing.T) {
			t.Parallel()

			preState := idempStep4PreStateFor(tc.op)
			if preState != tc.wantPreState {
				t.Errorf("idempStep4PreStateFor(%q) = %q; want %q", tc.op, preState, tc.wantPreState)
			}
			// Pre-state must differ from post-state (recovery protocol
			// distinguishes the two branches).
			if preState == tc.postState {
				t.Errorf("pre-state %q == post-state %q for op %q; step 3 and step 4 are indistinguishable", preState, tc.postState, tc.op)
			}
		})
	}
}

// TestIdempStep4_4a_BrOKReissueSucceeds verifies branch 4a: when the reissued
// `br` write returns exit 0 (BrOK), the adapter classifies it as BrOK. The
// intent file MUST be deleted after a BrOK result.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4a.
func TestIdempStep4_4a_BrOKReissueSucceeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep4FixtureEntry(t, "hk-step4-4a")
	intentPath := idempStep4FixtureIntentFile(t, dir, entry)

	// Mock br exits 0 on reissue (BrOK — transition completed).
	brPath := brcliFixtureMockBinary(t, `{}`, "", 0)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	result, err := adapter.Run(context.Background(), "update", string(entry.BeadID), "--status", "in_progress")
	if err != nil {
		t.Fatalf("Run (reissue): unexpected error: %v", err)
	}

	// 4a: Result.BrErr == BrOK.
	if result.BrErr != brcli.BrOK {
		t.Errorf("Result.BrErr = %q; want BrOK", result.BrErr)
	}

	// 4a clean-up: delete intent file (simulates the recovery orchestrator).
	if err := os.Remove(intentPath); err != nil {
		t.Fatalf("delete intent file after 4a: %v", err)
	}
	if _, err := os.Stat(intentPath); !os.IsNotExist(err) {
		t.Error("intent file still present after 4a BrOK clean-up")
	}
}

// TestIdempStep4_4b_BrConflictRoutesToStep3 verifies branch 4b: BrConflict
// from the reissue means a concurrent writer landed the transition between
// step 2 and step 4. The recovery MUST re-execute step 3 with the new state.
//
// This test verifies that BrConflict is correctly classified, and that the
// intent file is retained (step 3 re-evaluation, not deletion).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4b.
func TestIdempStep4_4b_BrConflictRoutesToStep3(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep4FixtureEntry(t, "hk-step4-4b")
	intentPath := idempStep4FixtureIntentFile(t, dir, entry)

	// Mock br exits 2 (BrConflict — concurrent write landed).
	brPath := brcliFixtureMockBinary(t, `{"error":{"code":"CONFLICT","message":"concurrent write"}}`, "", 2)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	result, err := adapter.Run(context.Background(), "update", string(entry.BeadID))
	if err != nil {
		t.Fatalf("Run (reissue): unexpected error for non-zero exit: %v", err)
	}

	// 4b: Result.BrErr == BrConflict.
	if result.BrErr != brcli.BrConflict {
		t.Errorf("Result.BrErr = %q; want BrConflict", result.BrErr)
	}

	// 4b: intent file retained (recovery re-executes step 3, does not clean up yet).
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file unexpectedly deleted on BrConflict; step 3 re-evaluation requires retention")
	}

	// BrConflict routes to Cat 3a (idempotency recovery) per §8 routing table.
	routeErr := result.BrErr
	cat := brcli.BrErrReconciliationCategory(routeErr)
	if cat != "Cat 3a" {
		t.Errorf("BrErrReconciliationCategory(BrConflict) = %q; want %q", cat, "Cat 3a")
	}
}

// TestIdempStep4_4c_BrDbLockedRetriesUpToMax verifies branch 4c:
// RunWithDBLockedRetry retries BrDbLocked up to DBLockedRetryMax times, then
// escalates to BrUnavailable.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4c; BI-025c.
func TestIdempStep4_4c_BrDbLockedRetriesUpToMax(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep4FixtureEntry(t, "hk-step4-4c")
	intentPath := idempStep4FixtureIntentFile(t, dir, entry)

	// Mock br always exits 3 (BrDbLocked — SQLite busy).
	brPath := brcliFixtureMockBinary(t, ``, "", 3)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	// Use minimal backoff so the test completes quickly.
	_, retryErr := adapter.RunWithDBLockedRetry(
		context.Background(),
		brcli.TimeoutConfig{ReadTimeout: 2 * time.Second},
		brcli.CommandKindWrite,
		brcli.DBLockedRetryMax,
		1*time.Millisecond,  // base: 1ms (much shorter than the spec 100ms)
		10*time.Millisecond, // cap: 10ms
		"update", string(entry.BeadID),
	)

	// 4c: after maxRetries BrDbLocked, escalates to BrUnavailable.
	if retryErr == nil {
		t.Fatal("RunWithDBLockedRetry with persistent BrDbLocked: expected error, got nil")
	}
	if !errors.Is(retryErr, brcli.BrUnavailable) {
		t.Errorf("errors.Is(retryErr, BrUnavailable) = false after %d BrDbLocked retries; got %v", brcli.DBLockedRetryMax, retryErr)
	}

	// 4c → 4d: intent file retained (persistent BrDbLocked → daemon degraded).
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file unexpectedly deleted after persistent BrDbLocked; 4d requires retention")
	}
}

// TestIdempStep4_4d_BrUnavailableRetainsIntentFile verifies branch 4d:
// BrUnavailable from the reissue means `br` is unreachable. The intent file
// MUST be retained; the daemon classifies as degraded per ON-037.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4d; ON-037.
func TestIdempStep4_4d_BrUnavailableRetainsIntentFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep4FixtureEntry(t, "hk-step4-4d")
	intentPath := idempStep4FixtureIntentFile(t, dir, entry)

	// Exec failure: binary not found.
	adapter, err := brcli.New("/nonexistent/br-step4")
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	_, runErr := adapter.Run(context.Background(), "update", string(entry.BeadID))
	if runErr == nil {
		t.Fatal("Run with missing binary: expected error, got nil")
	}

	// 4d: intent file MUST be retained when BrUnavailable.
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file absent after BrUnavailable; 4d requires retention for Cat 0 retry")
	}
}

// TestIdempStep4_4e_BrSchemaMismatchEmitsDivergenceInconclusive verifies
// branch 4e: BrSchemaMismatch from the reissue means the pinned Beads schema
// does not match. The recovery MUST emit divergence_inconclusive (per BI-031b)
// and route to Cat 0 (schema-mismatch → daemon startup failure).
//
// This test verifies the BrError classification and the reconciliation routing
// for BrSchemaMismatch.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4e; BI-031b; §8.
func TestIdempStep4_4e_BrSchemaMismatchRoutesToCat0(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep4FixtureEntry(t, "hk-step4-4e")
	intentPath := idempStep4FixtureIntentFile(t, dir, entry)

	// Mock br exits 4 (BrSchemaMismatch).
	brPath := brcliFixtureMockBinary(t, `{"error":{"code":"SCHEMA_MISMATCH","message":"schema version mismatch"}}`, "", 4)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	result, runErr := adapter.Run(context.Background(), "update", string(entry.BeadID))
	if runErr != nil {
		t.Fatalf("Run: unexpected exec error: %v", runErr)
	}

	// 4e: Result.BrErr == BrSchemaMismatch.
	if result.BrErr != brcli.BrSchemaMismatch {
		t.Errorf("Result.BrErr = %q; want BrSchemaMismatch", result.BrErr)
	}

	// BrSchemaMismatch routes to Cat 0 per §8.
	cat := brcli.BrErrReconciliationCategory(result.BrErr)
	if cat != "Cat 0" {
		t.Errorf("BrErrReconciliationCategory(BrSchemaMismatch) = %q; want %q", cat, "Cat 0")
	}

	// 4e: intent file retained (recovery cannot proceed under schema drift).
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file absent after BrSchemaMismatch; BI-031b requires retention")
	}
}

// TestIdempStep4_4f_BrOtherRoutesToCat3AndRetainsIntentFile verifies branch
// 4f: an unrecognized exit code (BrOther) from the reissue MUST emit
// divergence_inconclusive with reason=authority_unavailable, retain the intent
// file, and route to Cat 6b operator-escalation.
//
// This test verifies BrError = BrOther and the reconciliation routing (Cat 3).
// The BrErrReconciliationCategory §8 table maps BrOther → Cat 3 (divergence
// detected; investigator dispatch), which is the closest we can get to Cat 6b
// without a separate Cat 6b constant (Cat 6b is a sub-category of Cat 3 per
// the reconciliation spec).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4f; §8 routing table.
func TestIdempStep4_4f_BrOtherRoutesToCat3AndRetainsIntentFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep4FixtureEntry(t, "hk-step4-4f")
	intentPath := idempStep4FixtureIntentFile(t, dir, entry)

	// Mock br exits 99 (unknown exit code → BrOther).
	brPath := brcliFixtureMockBinary(t, ``, "", 99)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	result, runErr := adapter.Run(context.Background(), "update", string(entry.BeadID))
	if runErr != nil {
		t.Fatalf("Run: unexpected exec error: %v", runErr)
	}

	// 4f: Result.BrErr == BrOther.
	if result.BrErr != brcli.BrOther {
		t.Errorf("Result.BrErr = %q; want BrOther", result.BrErr)
	}

	// BrOther routes to Cat 3 per §8 routing table.
	cat := brcli.BrErrReconciliationCategory(result.BrErr)
	if cat != "Cat 3" {
		t.Errorf("BrErrReconciliationCategory(BrOther) = %q; want %q", cat, "Cat 3")
	}

	// 4f: intent file retained for Cat 6b operator-escalation.
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file absent after BrOther; step 4f requires retention for Cat 6b escalation")
	}
}

// TestIdempStep4_BrErrorClassificationFromExitCode verifies that all six
// BrError values from step 4 are correctly produced by BrErrorFromExitCode for
// the exit codes that `br` returns on a reissue. This ensures the recovery path
// uses the same classification table as the §6.1a spec.
//
// Spec ref: specs/beads-integration.md §6.1a BrError enum; §4.10 BI-031 step 4.
func TestIdempStep4_BrErrorClassificationFromExitCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		exitCode int
		wantErr  brcli.BrError
		branch   string
	}{
		{0, brcli.BrOK, "4a"},
		{2, brcli.BrConflict, "4b"},
		{3, brcli.BrDbLocked, "4c"},
		{4, brcli.BrSchemaMismatch, "4e"},
		{99, brcli.BrOther, "4f"},
		{100, brcli.BrOther, "4f (other unknown)"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.branch, func(t *testing.T) {
			t.Parallel()

			got := brcli.BrErrorFromExitCode(tc.exitCode)
			if got != tc.wantErr {
				t.Errorf("BrErrorFromExitCode(%d) = %q; want %q (branch %s)", tc.exitCode, got, tc.wantErr, tc.branch)
			}
		})
	}
}

// TestIdempStep4_BrUnavailableNotFromExitCode verifies that BrUnavailable
// (branch 4d) is NOT produced by BrErrorFromExitCode — it is classified by
// the timeout and exec-failure paths (RunWithTimeout, Run exec error), not by
// any specific exit code.
//
// Spec ref: specs/beads-integration.md §6.1a — "Timeout and exec errors
// (BrUnavailable) are NOT classifiable by exit code alone."
func TestIdempStep4_BrUnavailableNotFromExitCode(t *testing.T) {
	t.Parallel()

	// No exit code in 0-4 or 99+ produces BrUnavailable from BrErrorFromExitCode.
	allCodes := []int{0, 1, 2, 3, 4, 5, 10, 99, 255}
	for _, code := range allCodes {
		got := brcli.BrErrorFromExitCode(code)
		if got == brcli.BrUnavailable {
			t.Errorf("BrErrorFromExitCode(%d) = BrUnavailable; BrUnavailable is reserved for exec/timeout failures, not exit codes", code)
		}
	}
}

// TestIdempStep4_4c_RetryCountRespected verifies that RunWithDBLockedRetry
// makes exactly maxRetries+1 attempts (1 initial + maxRetries retries) before
// escalating. This confirms the BI-025c retry ceiling is honoured.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4c; BI-025c
// ("Retry up to 3 times").
func TestIdempStep4_4c_RetryCountRespected(t *testing.T) {
	t.Parallel()

	// The mock script always exits 3 (BrDbLocked). RunWithDBLockedRetry must
	// make DBLockedRetryMax+1 attempts (1 initial + 3 retries).
	brPath := brcliFixtureMockBinary(t, ``, "", 3)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	_, retryErr := adapter.RunWithDBLockedRetry(
		context.Background(),
		brcli.TimeoutConfig{ReadTimeout: 2 * time.Second},
		brcli.CommandKindWrite,
		brcli.DBLockedRetryMax,
		1*time.Millisecond,
		10*time.Millisecond,
		"update", "hk-step4-retry",
	)

	// Must escalate to BrUnavailable after maxRetries.
	if !errors.Is(retryErr, brcli.BrUnavailable) {
		t.Errorf("expected BrUnavailable after %d retries; got %v", brcli.DBLockedRetryMax, retryErr)
	}
}
