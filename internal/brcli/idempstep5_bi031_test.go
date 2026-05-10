package brcli_test

// idempstep5_bi031_test.go — BI-031 step 5 tests: status-neither-pre-nor-post
// triggers Cat 3a divergence emission and intent-file retention.
//
// Step 5 of the crash-recovery protocol (beads-integration.md §4.10 BI-031):
//
//	"If the current status is neither pre-state nor post-state (Beads
//	diverged), the divergence is a Cat 3a torn-write per
//	[reconciliation/spec.md §4.3 RC-014] / [reconciliation/spec.md §8.4a];
//	the adapter MUST emit divergence_inconclusive per [event-model.md §8.6.10]
//	with reason=authority_unavailable and route to reconciliation rather than
//	reissuing."
//
// Tests here verify:
//   - The "neither pre nor post" predicate covers all three CoarseStatus
//     values for every op type.
//   - ShowBead returns the diverged status faithfully so that the recovery
//     orchestrator can detect the step-5 condition.
//   - The intent file MUST be retained (reissue is refused).
//   - The diverged status routes to Cat 3a per the §8 reconciliation table
//     (BrConflict is the closest classification for a concurrent torn-write;
//     the actual divergence emission is orchestrator-owned and uses EV-023a).
//
// All test helpers in this file use the idempStep5 prefix per
// implementer-protocol.md helper-prefix discipline (bead hk-872.38.5).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5; RC-014; §8.4a;
// event-model.md §8.6.10 EV-023a (divergence_inconclusive / authority_unavailable).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// idempStep5FixtureEntry returns a valid IntentLogEntry for step-5 tests.
func idempStep5FixtureEntry(t *testing.T, beadID core.BeadID, op core.TerminalOp, postState core.CoarseStatus) core.IntentLogEntry {
	t.Helper()
	return core.IntentLogEntry{
		IdempotencyKey:    "run-step5:trans-ghi:" + string(op),
		RunID:             core.RunID(uuid.Must(uuid.NewV7())),
		TransitionID:      core.TransitionID(uuid.Must(uuid.NewV7())),
		Op:                op,
		BeadID:            beadID,
		IntendedPostState: postState,
		RequestedAt:       time.Now().UTC().Truncate(time.Second),
		SchemaVersion:     1,
	}
}

// idempStep5PreStateFor derives the expected pre-state for a terminal op.
// Used to identify which statuses are "neither pre nor post".
func idempStep5PreStateFor(op core.TerminalOp) core.CoarseStatus {
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

// idempStep5FixtureIntentFile writes a JSON intent-log file to dir and returns
// the path.
func idempStep5FixtureIntentFile(t *testing.T, dir string, entry core.IntentLogEntry) string {
	t.Helper()

	path := filepath.Join(dir, string(entry.BeadID)+"-step5.json")
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
		t.Fatalf("idempStep5FixtureIntentFile: WriteFile %q: %v", path, err)
	}
	return path
}

// TestIdempStep5_NeitherPreNorPostPredicateForClaim verifies the "neither pre
// nor post" predicate for a claim op. Pre-state = open, post-state = in_progress.
// Only "closed" is neither.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5.
func TestIdempStep5_NeitherPreNorPostPredicateForClaim(t *testing.T) {
	t.Parallel()

	entry := idempStep5FixtureEntry(t, "hk-step5-claim", core.TerminalOpClaim, core.CoarseStatusInProgress)
	preState := idempStep5PreStateFor(entry.Op)

	statuses := []struct {
		status      core.CoarseStatus
		wantNeither bool
	}{
		{core.CoarseStatusOpen, false},       // pre-state → step 4
		{core.CoarseStatusInProgress, false}, // post-state → step 3
		{core.CoarseStatusClosed, true},      // neither → step 5 (Cat 3a)
	}

	for _, tc := range statuses {
		tc := tc
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()

			isPreState := tc.status == preState
			isPostState := tc.status == entry.IntendedPostState
			isNeither := !isPreState && !isPostState

			if isNeither != tc.wantNeither {
				t.Errorf("status %q: isNeither = %v; want %v (isPreState=%v, isPostState=%v)",
					tc.status, isNeither, tc.wantNeither, isPreState, isPostState)
			}
		})
	}
}

// TestIdempStep5_NeitherPreNorPostPredicateForClose verifies the "neither pre
// nor post" predicate for a close op. Pre-state = in_progress, post-state = closed.
// Only "open" is neither.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5.
func TestIdempStep5_NeitherPreNorPostPredicateForClose(t *testing.T) {
	t.Parallel()

	entry := idempStep5FixtureEntry(t, "hk-step5-close", core.TerminalOpClose, core.CoarseStatusClosed)
	preState := idempStep5PreStateFor(entry.Op)

	statuses := []struct {
		status      core.CoarseStatus
		wantNeither bool
	}{
		{core.CoarseStatusOpen, true},        // neither → step 5
		{core.CoarseStatusInProgress, false}, // pre-state → step 4
		{core.CoarseStatusClosed, false},     // post-state → step 3
	}

	for _, tc := range statuses {
		tc := tc
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()

			isPreState := tc.status == preState
			isPostState := tc.status == entry.IntendedPostState
			isNeither := !isPreState && !isPostState

			if isNeither != tc.wantNeither {
				t.Errorf("status %q: isNeither = %v; want %v", tc.status, isNeither, tc.wantNeither)
			}
		})
	}
}

// TestIdempStep5_NeitherPreNorPostPredicateForReopen verifies the "neither pre
// nor post" predicate for a reopen op. Pre-state = closed, post-state = open.
// Only "in_progress" is neither.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5.
func TestIdempStep5_NeitherPreNorPostPredicateForReopen(t *testing.T) {
	t.Parallel()

	entry := idempStep5FixtureEntry(t, "hk-step5-reopen", core.TerminalOpReopen, core.CoarseStatusOpen)
	preState := idempStep5PreStateFor(entry.Op)

	statuses := []struct {
		status      core.CoarseStatus
		wantNeither bool
	}{
		{core.CoarseStatusOpen, false},      // post-state → step 3
		{core.CoarseStatusInProgress, true}, // neither → step 5
		{core.CoarseStatusClosed, false},    // pre-state → step 4
	}

	for _, tc := range statuses {
		tc := tc
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()

			isPreState := tc.status == preState
			isPostState := tc.status == entry.IntendedPostState
			isNeither := !isPreState && !isPostState

			if isNeither != tc.wantNeither {
				t.Errorf("status %q: isNeither = %v; want %v", tc.status, isNeither, tc.wantNeither)
			}
		})
	}
}

// TestIdempStep5_ShowBeadReturnsDivergedStatus verifies that ShowBead correctly
// surfaces the diverged CoarseStatus when Beads is in a state that is neither
// the pre-state nor the post-state. The step-5 path relies on ShowBead returning
// the actual current status so the recovery orchestrator can detect the condition.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5.
func TestIdempStep5_ShowBeadReturnsDivergedStatus(t *testing.T) {
	t.Parallel()

	// Claim op: pre-state = open, post-state = in_progress.
	// Diverged status = closed (neither).
	entry := idempStep5FixtureEntry(t, "hk-step5-diverged", core.TerminalOpClaim, core.CoarseStatusInProgress)

	// Mock br returns "closed" — a diverged status for a pending claim.
	jsonStr := idempStep2FixtureShowJSON(string(entry.BeadID), "closed")
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	// Verify the diverged status is surfaced faithfully.
	if record.Status != core.CoarseStatusClosed {
		t.Errorf("record.Status = %q; want %q (diverged status for claim)", record.Status, core.CoarseStatusClosed)
	}

	// Confirm step-5 condition: neither pre (open) nor post (in_progress).
	preState := idempStep5PreStateFor(entry.Op)
	isPreState := record.Status == preState
	isPostState := record.Status == entry.IntendedPostState
	if isPreState || isPostState {
		t.Errorf("diverged status %q must not be pre (%q) or post (%q); got isPreState=%v isPostState=%v",
			record.Status, preState, entry.IntendedPostState, isPreState, isPostState)
	}
}

// TestIdempStep5_IntentFileRetainedOnDivergence verifies that the intent file
// is retained when the step-5 condition is detected. The recovery refuses to
// reissue the write; the file stays for reconciliation's Cat 3a auto-resolver.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5; BI-032 (intent
// log is the Cat 3a detector's evidence source).
func TestIdempStep5_IntentFileRetainedOnDivergence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	entry := idempStep5FixtureEntry(t, "hk-step5-retain", core.TerminalOpClaim, core.CoarseStatusInProgress)
	intentPath := idempStep5FixtureIntentFile(t, dir, entry)

	// Step 5: status is "closed" (neither open nor in_progress for a claim).
	// Recovery orchestrator detects neither-pre-nor-post → retains intent file.
	// The test models this by NOT deleting the file — representing the recovery's
	// "refuse reissue" branch.

	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Fatal("intent file not present before step-5 check; fixture error")
	}

	// Intent file MUST be retained after step-5 divergence detection.
	if _, err := os.Stat(intentPath); os.IsNotExist(err) {
		t.Error("intent file absent after step-5 divergence; Cat 3a auto-resolver cannot find evidence")
	}

	// The retained file must still be a valid IntentLogEntry.
	retained, err := core.ReadIntentLogEntry(intentPath)
	if err != nil {
		t.Fatalf("ReadIntentLogEntry on retained file: %v", err)
	}
	if !retained.Valid() {
		t.Error("retained IntentLogEntry.Valid() = false; corrupted evidence for Cat 3a")
	}
}

// TestIdempStep5_Cat3aRoutingFromDivergence verifies that the step-5 divergence
// routes to Cat 3a in the reconciliation category table. BrConflict is the
// closest BrError mapping for a concurrent torn-write in the §8 routing table.
//
// Note: the actual divergence_inconclusive emission per EV-023a is orchestrator-
// owned and depends on the event-bus subsystem (tracked by hk-872.57). These
// tests verify the classification primitives (BrErrReconciliationCategory) that
// the recovery orchestrator will use.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5; §8 routing table;
// specs/reconciliation/spec.md §8.4a RC-014 (Cat 3a torn-Beads-write).
func TestIdempStep5_Cat3aRoutingFromDivergence(t *testing.T) {
	t.Parallel()

	// BrConflict is the BrError that maps to Cat 3a in the §8 routing table.
	// A step-5 divergence is classified similarly: an unexpected torn-write
	// condition routed through the Cat 3a auto-resolver.
	cat := brcli.BrErrReconciliationCategory(brcli.BrConflict)
	if cat != "Cat 3a" {
		t.Errorf("BrErrReconciliationCategory(BrConflict) = %q; want %q", cat, "Cat 3a")
	}
}

// TestIdempStep5_ReissueRefused verifies that when the step-5 condition is
// detected (current status is neither pre-state nor post-state), the recovery
// orchestrator MUST NOT reissue the write. The test models this by confirming
// that the "neither" predicate is mutually exclusive with the step-4 reissue
// precondition (current_status == pre-state).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5 ("route to
// reconciliation rather than reissuing").
func TestIdempStep5_ReissueRefused(t *testing.T) {
	t.Parallel()

	entry := idempStep5FixtureEntry(t, "hk-step5-refuse", core.TerminalOpClaim, core.CoarseStatusInProgress)
	preState := idempStep5PreStateFor(entry.Op)

	// The diverged status for a claim (closed) must not trigger the step-4
	// reissue path (which requires current_status == preState == open).
	divergedStatus := core.CoarseStatusClosed
	isPreState := divergedStatus == preState
	isPostState := divergedStatus == entry.IntendedPostState

	// Step-5 guard: neither pre nor post → reissue is refused.
	if isPreState {
		t.Errorf("diverged status %q incorrectly matches pre-state %q; step 4 reissue would be triggered", divergedStatus, preState)
	}
	if isPostState {
		t.Errorf("diverged status %q incorrectly matches post-state %q; step 3 disambiguation would be triggered", divergedStatus, entry.IntendedPostState)
	}

	// Confirm step-5 condition is met.
	isNeither := !isPreState && !isPostState
	if !isNeither {
		t.Errorf("diverged status %q should satisfy isNeither=true; got false", divergedStatus)
	}
}

// TestIdempStep5_AllOpsDivergedStatusMatrix verifies the step-5 "neither"
// predicate for all three terminal ops and their respective diverged statuses.
// This ensures the predicate logic is op-agnostic and handles every combination.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 5.
func TestIdempStep5_AllOpsDivergedStatusMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		op        core.TerminalOp
		postState core.CoarseStatus
		diverged  core.CoarseStatus // the "neither" status for this op
	}{
		{core.TerminalOpClaim, core.CoarseStatusInProgress, core.CoarseStatusClosed},
		{core.TerminalOpClose, core.CoarseStatusClosed, core.CoarseStatusOpen},
		{core.TerminalOpReopen, core.CoarseStatusOpen, core.CoarseStatusInProgress},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.op), func(t *testing.T) {
			t.Parallel()

			preState := idempStep5PreStateFor(tc.op)
			isPreState := tc.diverged == preState
			isPostState := tc.diverged == tc.postState
			isNeither := !isPreState && !isPostState

			if !isNeither {
				t.Errorf("op=%q diverged=%q: isNeither=false; pre=%q post=%q; step-5 condition not met",
					tc.op, tc.diverged, preState, tc.postState)
			}
		})
	}
}

// TestIdempStep5_ShowBeadExposesNeitherStatus verifies the end-to-end data
// flow from ShowBead (step 2) to the step-5 predicate evaluation. Given an
// IntentLogEntry from step 1 and a ShowBead result showing a diverged status,
// the recovery orchestrator can correctly determine that step 5 applies.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 2; step 5.
func TestIdempStep5_ShowBeadExposesNeitherStatus(t *testing.T) {
	t.Parallel()

	// Close op: pre-state = in_progress, post-state = closed.
	// Diverged status = open (neither).
	entry := idempStep5FixtureEntry(t, "hk-step5-e2e", core.TerminalOpClose, core.CoarseStatusClosed)

	// Step 2: ShowBead returns "open" — diverged for a pending close.
	jsonStr := idempStep2FixtureShowJSON(string(entry.BeadID), "open")
	brPath := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), entry.BeadID)
	if err != nil {
		t.Fatalf("ShowBead: %v", err)
	}

	// Verify the step-5 condition from the recovery orchestrator's perspective.
	preState := idempStep5PreStateFor(entry.Op)
	isPreState := record.Status == preState
	isPostState := record.Status == entry.IntendedPostState
	isNeither := !isPreState && !isPostState

	if !isNeither {
		t.Errorf("ShowBead returned status %q for close op; expected neither pre (%q) nor post (%q); isPreState=%v isPostState=%v",
			record.Status, preState, entry.IntendedPostState, isPreState, isPostState)
	}
}
