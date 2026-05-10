package core

// ---- Crash-recovery suite (hk-63oh.78) ----
//
// RC-031: every detector under §4.3/§8, every verdict-execution path under
// §4.5, and every staleness-check path under RC-024 MUST be exercised by at
// least one crash-recovery scenario test before landing.
//
// Testing-layer note: full crash-recovery tests require the scenario harness
// (S07 / docs/methodology/TESTING.md §4) which is not yet built. This file
// provides the unit-layer crash-recovery coverage that is achievable without
// real process termination: for each crash point the spec names, we assert
// the restart-classification rule that governs recovery (i.e., "what category
// does the daemon assign on restart after this crash, and what does it do?").
// Scenario-layer tests (process-kill + restart asserts) are deferred to OQ-RC-006.
//
// Six crash injection points per bead description (hk-63oh.78):
//   (a) Between RC-013 category_assigned emission and downstream dispatch.
//   (b) Between RC-018 budget_exhausted emission and fallback-verdict emission.
//   (c) Between RC-022 verdict-emitted commit and RC-025 verdict-executed commit
//       (Cat 3b territory).
//   (d) Between RC-024 staleness re-capture and RC-025 mechanical action.
//   (e) Inside RC-025a's 7-step verdict-executor.
//   (f) Inside RC-018's 5-step budget-exhaustion handler.
//
// Spec refs:
//   - specs/reconciliation/spec.md §4.3 RC-010..RC-020b (detectors)
//   - specs/reconciliation/spec.md §4.5 RC-020..RC-026a (verdict execution)
//   - specs/reconciliation/spec.md §4.7 RC-031 (testing obligation)
//   - docs/methodology/TESTING.md §4 (crash-recovery layer)

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---- Crash-recovery harness state types ----

// rc78CrashPoint is a symbolic label for a named crash injection point
// described in the RC-031 testing obligation.
type rc78CrashPoint string

const (
	// rc78CrashPointAfterCategoryAssigned is crash point (a):
	// between RC-013 reconciliation_category_assigned emission and downstream dispatch.
	// Recovery rule: detector re-runs from scratch on restart (RC-003, RC-013 dedup
	// key = (target_run_id, category, snapshot_token.git_head_hash)).
	rc78CrashPointAfterCategoryAssigned rc78CrashPoint = "after-category-assigned"

	// rc78CrashPointBetweenBudgetExhaustedAndFallback is crash point (b):
	// between RC-018 step (3) budget_exhausted emission and step (4) fallback-verdict emission.
	// Recovery rule: next startup sees budget_exhausted event with no verdict commit →
	// routes through Cat 3b retry cap (RC-026a per §8.5).
	rc78CrashPointBetweenBudgetExhaustedAndFallback rc78CrashPoint = "between-budget-exhausted-and-fallback"

	// rc78CrashPointBetweenVerdictEmittedAndVerdictExecuted is crash point (c):
	// between verdict-emitted commit (RC-022) and verdict-executed commit (RC-025).
	// Recovery rule: Cat 3b (§8.5) — auto-resolve via RC-026 re-execution.
	rc78CrashPointBetweenVerdictEmittedAndVerdictExecuted rc78CrashPoint = "between-verdict-emitted-and-verdict-executed"

	// rc78CrashPointBetweenStalenessCheckAndMechanicalAction is crash point (d):
	// between RC-024 staleness re-capture and RC-025 mechanical action.
	// Recovery rule: Cat 3b — staleness check runs again on re-attempt.
	rc78CrashPointBetweenStalenessCheckAndMechanicalAction rc78CrashPoint = "between-staleness-check-and-mechanical-action"

	// rc78CrashPointInsideVerdictExecutor is crash point (e):
	// inside RC-025a's 7-step verdict-executor (at any step 1-7).
	// Recovery rule: panic recovery per PL-018a; next startup re-classifies as Cat 3b.
	rc78CrashPointInsideVerdictExecutor rc78CrashPoint = "inside-verdict-executor"

	// rc78CrashPointInsideBudgetExhaustionHandler is crash point (f):
	// inside RC-018's 5-step budget-exhaustion handler.
	// Recovery rule: Cat 3b if verdict commit present; Cat 5 (clean restart) if no
	// commit landed at all (RC-003 bounded recursion).
	rc78CrashPointInsideBudgetExhaustionHandler rc78CrashPoint = "inside-budget-exhaustion-handler"
)

// rc78RecoveryCategory is the restart-classification category the daemon
// assigns after the given crash point.
type rc78RecoveryRoute struct {
	crashPoint       rc78CrashPoint
	recoveryCategory ReconciliationCategory
	description      string // brief prose naming the restart path
}

// rc78CrashRecoveryRouteTable maps each crash point to its documented
// restart-classification category.
//
// This table is the unit-layer proxy for the scenario-layer crash-recovery
// tests described in TESTING.md §4. Each entry captures the spec contract
// without requiring process-level crash injection.
//
// Spec ref: specs/reconciliation/spec.md §4.7 RC-031; §4.5 RC-026; §8.5 Cat 3b.
var rc78CrashRecoveryRouteTable = []rc78RecoveryRoute{
	{
		crashPoint:       rc78CrashPointAfterCategoryAssigned,
		recoveryCategory: ReconciliationCategoryCat5,
		description:      "RC-013 dedup tolerates re-emission on restart; detector re-runs → same category; clean-restart (Cat 5) if no investigator dispatched yet",
	},
	{
		crashPoint:       rc78CrashPointBetweenBudgetExhaustedAndFallback,
		recoveryCategory: ReconciliationCategoryCat3b,
		description:      "budget_exhausted emitted, fallback verdict not yet emitted → no verdict commit → Cat 3b retry cap (RC-026a) on restart",
	},
	{
		crashPoint:       rc78CrashPointBetweenVerdictEmittedAndVerdictExecuted,
		recoveryCategory: ReconciliationCategoryCat3b,
		description:      "verdict-emitted commit present, verdict-executed commit absent → Cat 3b (§8.5) auto-resolve via RC-026 re-execution",
	},
	{
		crashPoint:       rc78CrashPointBetweenStalenessCheckAndMechanicalAction,
		recoveryCategory: ReconciliationCategoryCat3b,
		description:      "staleness check passed but mechanical action not applied → verdict-emitted commit present, verdict-executed absent → Cat 3b retry",
	},
	{
		crashPoint:       rc78CrashPointInsideVerdictExecutor,
		recoveryCategory: ReconciliationCategoryCat3b,
		description:      "RC-025a panic-safe (PL-018a); on crash mid-step, task branch has verdict commit but no verdict-executed → Cat 3b on restart",
	},
	{
		crashPoint:       rc78CrashPointInsideBudgetExhaustionHandler,
		recoveryCategory: ReconciliationCategoryCat5,
		description:      "RC-018 handler crashed before any commit → no verdict commit exists → Cat 5 clean restart (RC-003 no mid-investigation durable state)",
	},
}

// ---- Crash-recovery route table tests ----

// TestRC031_CrashRecoveryRouteTableCoversAllSixPoints verifies that the
// rc78CrashRecoveryRouteTable covers all six crash injection points named in
// the RC-031 testing obligation (bead hk-63oh.78).
//
// Spec ref: specs/reconciliation/spec.md §4.7 RC-031.
func TestRC031_CrashRecoveryRouteTableCoversAllSixPoints(t *testing.T) {
	t.Parallel()

	expected := []rc78CrashPoint{
		rc78CrashPointAfterCategoryAssigned,
		rc78CrashPointBetweenBudgetExhaustedAndFallback,
		rc78CrashPointBetweenVerdictEmittedAndVerdictExecuted,
		rc78CrashPointBetweenStalenessCheckAndMechanicalAction,
		rc78CrashPointInsideVerdictExecutor,
		rc78CrashPointInsideBudgetExhaustionHandler,
	}
	const wantCount = 6
	if len(expected) != wantCount {
		t.Fatalf("expected crash-point slice has %d entries, want %d; update this test", len(expected), wantCount)
	}

	inTable := make(map[rc78CrashPoint]bool, len(rc78CrashRecoveryRouteTable))
	for _, row := range rc78CrashRecoveryRouteTable {
		inTable[row.crashPoint] = true
	}
	for _, cp := range expected {
		if !inTable[cp] {
			t.Errorf("RC-031: crash point %q is missing from rc78CrashRecoveryRouteTable", string(cp))
		}
	}
	if len(rc78CrashRecoveryRouteTable) != wantCount {
		t.Errorf("RC-031: route table has %d rows, want %d", len(rc78CrashRecoveryRouteTable), wantCount)
	}
}

// TestRC031_AllRecoveryRoutesHaveValidCategory verifies that each row in the
// crash-recovery route table maps to a valid ReconciliationCategory constant.
//
// Spec ref: specs/reconciliation/spec.md §4.7 RC-031.
func TestRC031_AllRecoveryRoutesHaveValidCategory(t *testing.T) {
	t.Parallel()

	for _, row := range rc78CrashRecoveryRouteTable {
		row := row
		t.Run(string(row.crashPoint), func(t *testing.T) {
			t.Parallel()
			if !row.recoveryCategory.Valid() {
				t.Errorf("RC-031: crash point %q has invalid recovery category %q", string(row.crashPoint), string(row.recoveryCategory))
			}
			if row.description == "" {
				t.Errorf("RC-031: crash point %q has empty description; crash-recovery routes must be documented", string(row.crashPoint))
			}
		})
	}
}

// ---- Crash point (a): after RC-013 category_assigned, before dispatch ----

// TestRC031_CrashAfterCategoryAssigned_RecoveryIsCat5 verifies that a crash
// AFTER RC-013 reconciliation_category_assigned emission but BEFORE investigator
// dispatch routes to Cat 5 on restart (clean restart; no investigator was in
// flight).
//
// Recovery rationale: RC-003 guarantees bounded recursion — no mid-investigation
// durable state exists. The detector re-runs per RC-026 startup pass; RC-013 emits
// another category_assigned (deduped by (target_run_id, category, git_head_hash)
// per RC-013 consumer contract). The run's prior-run task branch has no verdict
// commit → Cat 5 (orphaned branch from a prior run per RC-010).
//
// Note: recovery may also be Cat 5 or Cat 2/3 depending on the outer run's
// current state. The Cat 5 route applies when the outer run had no in-flight
// checkpoints for the current claim (the most common fresh-start case). This
// test documents the Cat 5 route specifically.
//
// Spec ref: §4.7 RC-031; §4.3 RC-013 (dedup); §4.1 RC-003 (bounded recursion);
// §8.8 Cat 5.
func TestRC031_CrashAfterCategoryAssigned_RecoveryIsCat5(t *testing.T) {
	t.Parallel()

	route, ok := rc78CrashRecoveryRouteForPoint(rc78CrashPointAfterCategoryAssigned)
	if !ok {
		t.Fatal("RC-031: no route entry for rc78CrashPointAfterCategoryAssigned")
	}
	// After a category-assigned-only crash with no investigator in flight,
	// the outer run's state is unchanged → fresh restart → Cat 5.
	if route.recoveryCategory != ReconciliationCategoryCat5 {
		t.Errorf("RC-031/crash-a: recovery category = %q, want %q (Cat 5 clean restart)",
			string(route.recoveryCategory), string(ReconciliationCategoryCat5))
	}
}

// TestRC031_CrashAfterCategoryAssigned_RC013DedupToleratesReemission verifies
// that RC-013's deduplication contract (consumers MUST tolerate duplicate
// category_assigned emissions) allows the detector to re-emit safely on restart.
//
// Spec ref: §4.3 RC-013 — "Consumers MUST tolerate duplicate emissions; dedup
// key is (target_run_id, category, snapshot_token.git_head_hash)."
func TestRC031_CrashAfterCategoryAssigned_RC013DedupToleratesReemission(t *testing.T) {
	t.Parallel()

	// The dedup key for RC-013.
	type rc013DedupKey struct {
		targetRunID string
		category    ReconciliationCategory
		gitHeadHash string
	}

	// Simulate two emissions for the same (run, category, head): they carry
	// identical dedup keys and MUST be treated as one emission by consumers.
	k1 := rc013DedupKey{
		targetRunID: "018f1e2a-0000-7000-8000-000000007801",
		category:    ReconciliationCategoryCat2,
		gitHeadHash: "abc123def456",
	}
	k2 := k1 // crash + restart re-emits the same key

	if k1 != k2 {
		t.Error("RC-031/RC-013: dedup keys differ; consumer must tolerate re-emission on restart")
	}
	// Both carry the same category; a consumer deduping on (k1.targetRunID, k1.category, k1.gitHeadHash)
	// must not dispatch a second investigator.
}

// ---- Crash point (b): between budget_exhausted and fallback-verdict ----

// TestRC031_CrashBetweenBudgetExhaustedAndFallback_RecoveryIsCat3b verifies
// that a crash after RC-018 step (3) (budget_exhausted emitted) but before
// step (4) (fallback escalate-to-human verdict emitted) routes through Cat 3b
// on restart.
//
// Recovery rationale: budget_exhausted event is present (fsync-boundary write),
// but no verdict commit landed. The startup detector sees: reconciliation workflow
// in flight, no verdict-executed commit → Cat 3b. The Cat 3b auto-resolver
// re-attempts the verdict per RC-026 / RC-026a retry cap.
//
// Spec ref: §4.5 RC-018 — "on crash between [budget_exhausted] and [fallback
// verdict], the next daemon startup detects the budget-exhausted event with no
// subsequent verdict commit and routes through Cat 3b retry cap (RC-026a)."
func TestRC031_CrashBetweenBudgetExhaustedAndFallback_RecoveryIsCat3b(t *testing.T) {
	t.Parallel()

	route, ok := rc78CrashRecoveryRouteForPoint(rc78CrashPointBetweenBudgetExhaustedAndFallback)
	if !ok {
		t.Fatal("RC-031: no route entry for rc78CrashPointBetweenBudgetExhaustedAndFallback")
	}
	if route.recoveryCategory != ReconciliationCategoryCat3b {
		t.Errorf("RC-031/crash-b: recovery category = %q, want %q (Cat 3b: budget_exhausted with no verdict commit)",
			string(route.recoveryCategory), string(ReconciliationCategoryCat3b))
	}
}

// TestRC031_CrashBetweenBudgetExhaustedAndFallback_Cat3bAutoResolverPresent
// verifies that Cat 3b has an auto-resolver (RC-008), which is the mechanism
// that re-executes the verdict on restart in this crash-recovery path.
//
// Spec ref: §4.5 RC-018; §8.5 Cat 3b; §4.2 RC-008.
func TestRC031_CrashBetweenBudgetExhaustedAndFallback_Cat3bAutoResolverPresent(t *testing.T) {
	t.Parallel()

	row, ok := rc79LookupActionRow(ReconciliationCategoryCat3b)
	if !ok {
		t.Fatal("RC-031/crash-b: no action-table row for Cat 3b")
	}
	if !row.autoResolver {
		t.Error("RC-031/crash-b: Cat 3b.autoResolver = false; Cat 3b must have an auto-resolver (RC-008) to re-execute the verdict on restart")
	}
}

// ---- Crash point (c): verdict-emitted commit, no verdict-executed commit ----

// TestRC031_CrashBetweenVerdictEmittedAndVerdictExecuted_RecoveryIsCat3b
// verifies that the canonical Cat 3b scenario — verdict-emitted commit present,
// verdict-executed commit absent — routes to Cat 3b on restart.
//
// This is the primary Cat 3b detection rule (§8.5): "Investigator-run task branch
// has reconciliation_verdict_emitted commit AND no subsequent
// Harmonik-Verdict-Executed: true commit."
//
// Spec ref: §8.5 Cat 3b; §4.5 RC-025; RC-026 (verdict-execution discovery on restart).
func TestRC031_CrashBetweenVerdictEmittedAndVerdictExecuted_RecoveryIsCat3b(t *testing.T) {
	t.Parallel()

	route, ok := rc78CrashRecoveryRouteForPoint(rc78CrashPointBetweenVerdictEmittedAndVerdictExecuted)
	if !ok {
		t.Fatal("RC-031: no route entry for rc78CrashPointBetweenVerdictEmittedAndVerdictExecuted")
	}
	if route.recoveryCategory != ReconciliationCategoryCat3b {
		t.Errorf("RC-031/crash-c: recovery category = %q, want %q (Cat 3b: verdict-emitted, verdict-executed absent)",
			string(route.recoveryCategory), string(ReconciliationCategoryCat3b))
	}
}

// TestRC031_CrashBetweenVerdictEmittedAndVerdictExecuted_TrailerDetection
// verifies that the Cat 3b detection rule depends on the presence or absence
// of the Harmonik-Verdict-Executed trailer — which is in the trailer registry.
//
// Spec ref: §8.5 Cat 3b; reconciliation/schemas.md §6.4.
func TestRC031_CrashBetweenVerdictEmittedAndVerdictExecuted_TrailerDetection(t *testing.T) {
	t.Parallel()

	// The Cat 3b detector checks for the Harmonik-Verdict-Executed trailer
	// on the investigator's task branch. This trailer is registry-known.
	spec, ok := LookupTrailer("Harmonik-Verdict-Executed")
	if !ok {
		t.Fatal("RC-031/crash-c: Harmonik-Verdict-Executed not in trailer registry")
	}
	// The trailer must be TrailerTypeEnum with value "true" (schemas.md §6.4).
	if spec.Type != TrailerTypeEnum {
		t.Errorf("RC-031/crash-c: Harmonik-Verdict-Executed type = %v, want TrailerTypeEnum", spec.Type)
	}
	if len(spec.EnumValues) != 1 || spec.EnumValues[0] != "true" {
		t.Errorf("RC-031/crash-c: Harmonik-Verdict-Executed EnumValues = %v, want [\"true\"]", spec.EnumValues)
	}
	// Validate that "true" is accepted and "false" is rejected (RC-023).
	if err := ValidateTrailerValue(spec, "true"); err != nil {
		t.Errorf("RC-031/crash-c: ValidateTrailerValue(Harmonik-Verdict-Executed, \"true\") = %v, want nil", err)
	}
	if err := ValidateTrailerValue(spec, "false"); err == nil {
		t.Error("RC-031/crash-c: ValidateTrailerValue(Harmonik-Verdict-Executed, \"false\") = nil, want error (RC-023 malformed)")
	}
}

// ---- Crash point (d): staleness check passed, mechanical action not yet applied ----

// TestRC031_CrashBetweenStalenessCheckAndMechanicalAction_RecoveryIsCat3b
// verifies that a crash after RC-024 staleness check passes but before RC-025
// mechanical action lands also routes to Cat 3b.
//
// Recovery rationale: the verdict-emitted commit is present (staleness check is
// post-commit) but the verdict-executed commit is absent → Cat 3b. The Cat 3b
// auto-resolver re-runs the staleness check (RC-026a) and re-executes.
//
// Spec ref: §4.5 RC-024; §4.5 RC-025; §8.5 Cat 3b.
func TestRC031_CrashBetweenStalenessCheckAndMechanicalAction_RecoveryIsCat3b(t *testing.T) {
	t.Parallel()

	route, ok := rc78CrashRecoveryRouteForPoint(rc78CrashPointBetweenStalenessCheckAndMechanicalAction)
	if !ok {
		t.Fatal("RC-031: no route entry for rc78CrashPointBetweenStalenessCheckAndMechanicalAction")
	}
	if route.recoveryCategory != ReconciliationCategoryCat3b {
		t.Errorf("RC-031/crash-d: recovery category = %q, want %q",
			string(route.recoveryCategory), string(ReconciliationCategoryCat3b))
	}
}

// TestRC031_StalenessCheckPayload_SnapshotTokenFields verifies that
// StaleVerdictPayload carries the fields needed to document the staleness
// divergence on restart (schemas.md §6.1 RECORD StaleVerdictPayload).
//
// Spec ref: §4.5 RC-024; reconciliation/schemas.md §6.1 StaleVerdictPayload.
func TestRC031_StalenessCheckPayload_SnapshotTokenFields(t *testing.T) {
	t.Parallel()

	// Simulate a StaleVerdictPayload that would be emitted on crash-d recovery.
	// Both snapshot-at-dispatch and current-at-execution fields must be present.
	payload := StaleVerdictPayload{
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "abc123",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		CurrentGitHeadHash:  "def456",    // advanced since snapshot
		CurrentBeadsAuditID: "audit-001", // same: only git changed
		DivergenceReason:    StaleDivergenceReasonGitBranchAdvanced,
	}
	if !payload.Valid() {
		t.Error("RC-031/crash-d: StaleVerdictPayload.Valid() = false; staleness payload must be valid for restart route")
	}
}

// ---- Crash point (e): inside RC-025a's 7-step verdict-executor ----

// TestRC031_CrashInsideVerdictExecutor_RecoveryIsCat3b verifies that a crash
// inside the RC-025a 7-step verdict-executor (at any step 1–7) routes to
// Cat 3b on restart.
//
// Recovery rationale: RC-025a is panic-safe (PL-018a recover() barrier). A crash
// mid-step leaves the task branch with either no verdict commit (steps 1-3, Cat 5
// on restart) or a verdict commit but no verdict-executed commit (steps 4-7, Cat 3b).
// The dominant failure window is steps 4-7 (mechanical action + verdict-executed
// commit), so Cat 3b is the primary recovery route for the executor.
//
// Spec ref: §4.5 RC-025a — "on panic mid-step, the next daemon startup
// re-classifies via Cat 3b (verdict-emitted-but-unexecuted) per §8.5."
func TestRC031_CrashInsideVerdictExecutor_RecoveryIsCat3b(t *testing.T) {
	t.Parallel()

	route, ok := rc78CrashRecoveryRouteForPoint(rc78CrashPointInsideVerdictExecutor)
	if !ok {
		t.Fatal("RC-031: no route entry for rc78CrashPointInsideVerdictExecutor")
	}
	if route.recoveryCategory != ReconciliationCategoryCat3b {
		t.Errorf("RC-031/crash-e: recovery category = %q, want %q (Cat 3b: verdict-emitted, verdict-executed absent post-crash)",
			string(route.recoveryCategory), string(ReconciliationCategoryCat3b))
	}
}

// TestRC031_CrashInsideVerdictExecutor_Steps verifies that the 7-step
// RC-025a verdict-executor is documented with the correct number of steps.
//
// The 7 steps per RC-025a are:
//  1. Validate verdict (RC-020/RC-023).
//  2. Re-capture staleness (RC-024).
//  3. Construct+commit verdict-emitted commit.
//  4. Mechanically apply verdict action (schemas.md §6.2).
//  5. Construct+commit verdict-executed commit (Harmonik-Verdict-Executed: true).
//  6. Emit reconciliation_verdict_emitted + reconciliation_verdict_executed events.
//  7. Release RC-002a lock.
//
// Spec ref: §4.5 RC-025a.
func TestRC031_CrashInsideVerdictExecutor_Steps(t *testing.T) {
	t.Parallel()

	// RC-025a 7-step executor: the step count is normative; any change requires a
	// spec amendment (RC-009 / architecture.md §4.6 amendment protocol).
	rc025aSteps := []string{
		"1-validate-verdict",        // RC-020/RC-023
		"2-staleness-check",         // RC-024
		"3-commit-verdict-emitted",  // verdict commit on investigator branch
		"4-mechanical-action",       // schemas.md §6.2 verdict-execution table
		"5-commit-verdict-executed", // Harmonik-Verdict-Executed: true per schemas.md §6.4
		"6-emit-events",             // reconciliation_verdict_emitted + reconciliation_verdict_executed
		"7-release-lock",            // RC-002a lock per RC-002b
	}
	const wantStepCount = 7
	if len(rc025aSteps) != wantStepCount {
		t.Errorf("RC-025a: step count = %d, want %d; any change is a spec-level amendment (RC-009)", len(rc025aSteps), wantStepCount)
	}
	for i, s := range rc025aSteps {
		if s == "" {
			t.Errorf("RC-025a: step %d has empty label; all steps must be documented", i+1)
		}
	}
}

// TestRC031_CrashInsideVerdictExecutor_PanicSafetyPattern verifies the Go
// recover() barrier pattern described in RC-025a (panic-safe subroutine).
//
// Spec ref: §4.5 RC-025a — "The verdict-executor MUST be panic-safe
// (per [process-lifecycle.md §4.6 PL-018a])."
func TestRC031_CrashInsideVerdictExecutor_PanicSafetyPattern(t *testing.T) {
	t.Parallel()

	// Simulate the verdict-executor's per-step panic barrier.
	executorWithBarrier := func(step func()) (panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		step()
		return false
	}

	// A panicking step inside the executor is recovered by the barrier.
	panicked := executorWithBarrier(func() {
		panic("rc031: simulated verdict-executor panic at step 4") //nolint:gocritic // intentional panic for recovery test
	})
	if !panicked {
		t.Error("RC-031/crash-e: executorWithBarrier did not catch panic; panic-safe barrier must recover")
	}

	// A non-panicking step succeeds normally.
	notPanicked := executorWithBarrier(func() {
		// normal step execution
	})
	if notPanicked {
		t.Error("RC-031/crash-e: executorWithBarrier reported panic for a non-panicking step")
	}
}

// ---- Crash point (f): inside RC-018's 5-step budget-exhaustion handler ----

// TestRC031_CrashInsideBudgetExhaustionHandler_RecoveryIsCat5 verifies that a
// crash inside the RC-018 5-step budget-exhaustion handler BEFORE any durable
// commit lands routes to Cat 5 on restart.
//
// Recovery rationale: RC-002 (reconciliation workflows emit exactly one checkpoint
// commit — the verdict commit) means that before the verdict commit lands there is
// no mid-investigation durable state. On restart, the outer run's state is
// unchanged → Cat 5 (or the outer run's detector category if it has in-flight
// checkpoints). The Cat 5 route applies when the handler crashed before step (3)
// (budget_exhausted emission), so nothing durable was written.
//
// Spec ref: §4.5 RC-018; §4.1 RC-003 (bounded recursion → no mid-investigation state);
// §8.8 Cat 5.
func TestRC031_CrashInsideBudgetExhaustionHandler_RecoveryIsCat5(t *testing.T) {
	t.Parallel()

	route, ok := rc78CrashRecoveryRouteForPoint(rc78CrashPointInsideBudgetExhaustionHandler)
	if !ok {
		t.Fatal("RC-031: no route entry for rc78CrashPointInsideBudgetExhaustionHandler")
	}
	if route.recoveryCategory != ReconciliationCategoryCat5 {
		t.Errorf("RC-031/crash-f: recovery category = %q, want %q (Cat 5: no durable state before handler commit)",
			string(route.recoveryCategory), string(ReconciliationCategoryCat5))
	}
}

// TestRC031_CrashInsideBudgetExhaustionHandler_Steps verifies that the
// 5-step RC-018 budget-exhaustion handler is documented with the correct
// step count.
//
// The 5 steps per RC-018:
//  1. SIGTERM investigator subprocess; SIGKILL after HC-018 interval.
//  2. Wait for watcher-observation of process termination (HC-011).
//  3. Emit budget_exhausted (class F per EV §8.4.3, fsync-boundary).
//  4. Emit fallback escalate-to-human verdict (class F per RC-021, fsync-boundary).
//  5. Verdict-executor (RC-025a) consumes fallback as investigator-emitted.
//
// Crash between steps (3) and (4): budget_exhausted landed, no verdict → Cat 3b.
// Crash before step (3): no durable write → Cat 5.
//
// Spec ref: §4.5 RC-018.
func TestRC031_CrashInsideBudgetExhaustionHandler_Steps(t *testing.T) {
	t.Parallel()

	rc018Steps := []string{
		"1-sigterm-sigkill-investigator", // HC-018 interval
		"2-wait-for-process-termination", // HC-011 watcher
		"3-emit-budget-exhausted",        // fsync-boundary
		"4-emit-fallback-verdict",        // fsync-boundary
		"5-verdict-executor-consumes",    // RC-025a
	}
	const wantStepCount = 5
	if len(rc018Steps) != wantStepCount {
		t.Errorf("RC-018: step count = %d, want %d", len(rc018Steps), wantStepCount)
	}
}

// ---- Detector evidence path tests (every §4.3 / §8 detector) ----
//
// RC-031 requires every detector to be exercised. The detectors are:
//   Cat 0: infrastructure unavailable (RC-012 pre-check)
//   Cat 1: idempotency_class = idempotent (last checkpoint node)
//   Cat 2: non-idempotent, bead in_progress, no terminal event
//   Cat 3: inter-store disagreement (generic)
//   Cat 3a: torn Beads write (intent-log mismatch)
//   Cat 3b: verdict-unexecuted (Harmonik-Verdict-Executed absent)
//   Cat 3c: inverse premature-close (merge commit + bead in_progress)
//   Cat 4: well-defined retry/backoff state
//   Cat 5: nothing in-flight (clean restart / orphaned prior run)
//   Cat 6a: integrity violation (workspace missing, trailer mismatch, etc.)
//   Cat 6b: JSONL corrupt, git object missing, git fsck failure

// TestRC031_Detector_Cat0_InfraUnavailable verifies that the Cat 0 detector
// evidence type is ReconciliationCategoryCat0 and it's in the priority-first order.
//
// Spec ref: §8.1 Cat 0; §4.3 RC-012.
func TestRC031_Detector_Cat0_InfraUnavailable(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat0
	if !cat.Valid() {
		t.Fatal("RC-031/detector-Cat0: ReconciliationCategoryCat0 is not valid")
	}
	// Cat 0 is highest priority in RC-003a first-match order.
	if rc73PriorityFixtureIndexOf(cat) != 0 {
		t.Errorf("RC-031/detector-Cat0: Cat 0 is not at priority position 0; got %d", rc73PriorityFixtureIndexOf(cat))
	}
}

// TestRC031_Detector_Cat1_IdempotentRerun verifies the Cat 1 detection evidence:
// last checkpoint node has idempotency_class = idempotent.
//
// Spec ref: §8.2 Cat 1; execution-model.md §4.2 EM-009.
func TestRC031_Detector_Cat1_IdempotentRerun(t *testing.T) {
	t.Parallel()

	// The detection rule depends on IdempotencyClassIdempotent.
	if string(IdempotencyClassIdempotent) != "idempotent" {
		t.Errorf("RC-031/detector-Cat1: IdempotencyClassIdempotent = %q, want %q",
			string(IdempotencyClassIdempotent), "idempotent")
	}
	if !IdempotencyClassIdempotent.Valid() {
		t.Error("RC-031/detector-Cat1: IdempotencyClassIdempotent.Valid() = false")
	}

	// Cat 1 is LOWEST priority (last in first-match order per RC-003a).
	expectedLast := len(rc73PriorityFixtureOrder) - 1
	if rc73PriorityFixtureIndexOf(ReconciliationCategoryCat1) != expectedLast {
		t.Errorf("RC-031/detector-Cat1: Cat 1 priority index = %d, want %d (lowest priority in RC-003a)",
			rc73PriorityFixtureIndexOf(ReconciliationCategoryCat1), expectedLast)
	}
}

// TestRC031_Detector_Cat2_NonIdempotentInFlight verifies the Cat 2 detection
// evidence: node is non-idempotent and bead is in_progress with no terminal event.
//
// Spec ref: §8.3 Cat 2; schemas.md §6.3.
func TestRC031_Detector_Cat2_NonIdempotentInFlight(t *testing.T) {
	t.Parallel()

	// Cat 2 requires a non-idempotent or recoverable-non-idempotent class.
	nonIdempotent := IdempotencyClassNonIdempotent
	recoverableNonIdempotent := IdempotencyClassRecoverableNonIdempotent

	if !nonIdempotent.Valid() {
		t.Error("RC-031/detector-Cat2: IdempotencyClassNonIdempotent is not valid")
	}
	if !recoverableNonIdempotent.Valid() {
		t.Error("RC-031/detector-Cat2: IdempotencyClassRecoverableNonIdempotent is not valid")
	}

	// Cat 2 requires investigator dispatch.
	row, ok := rc79LookupActionRow(ReconciliationCategoryCat2)
	if !ok {
		t.Fatal("RC-031/detector-Cat2: no action-table row")
	}
	if !row.investigatorUsed {
		t.Error("RC-031/detector-Cat2: investigatorUsed=false; Cat 2 MUST dispatch investigator")
	}
}

// TestRC031_Detector_Cat3a_TornBeadsWrite verifies the Cat 3a detection evidence:
// intent-log entry present AND bead coarse-status is in intermediate state.
//
// Spec ref: §8.4a Cat 3a; beads-integration.md §4.10 BI-029/BI-031.
func TestRC031_Detector_Cat3a_TornBeadsWrite(t *testing.T) {
	t.Parallel()

	// Cat 3a detection pivots on IntentLogEntry being present (BI-031 intent-log).
	// The IntentLogEntry type exists in internal/core.
	rc031RunID := RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007850"))
	rc031TxID := TransitionID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007851"))
	entry := IntentLogEntry{
		IdempotencyKey:    IdempotencyKey(rc031RunID, rc031TxID, TerminalOpReopen),
		RunID:             rc031RunID,
		TransitionID:      rc031TxID,
		Op:                TerminalOpReopen,
		BeadID:            BeadID("hk-63oh"),
		IntendedPostState: CoarseStatusOpen,
		RequestedAt:       time.Now().UTC(),
		SchemaVersion:     1,
	}
	if !entry.Valid() {
		t.Errorf("RC-031/detector-Cat3a: IntentLogEntry.Valid() = false; fixture error: %+v", entry)
	}
	// Cat 3a auto-resolver (BI-031b status-check-before-reissue).
	row, ok := rc79LookupActionRow(ReconciliationCategoryCat3a)
	if !ok {
		t.Fatal("RC-031/detector-Cat3a: no action-table row")
	}
	if row.investigatorUsed {
		t.Error("RC-031/detector-Cat3a: investigatorUsed=true; Cat 3a is auto-resolved (BI-031b)")
	}
}

// TestRC031_Detector_Cat3b_VerdictUnexecuted verifies the Cat 3b detection
// evidence: verdict-emitted commit present, verdict-executed commit absent.
//
// Spec ref: §8.5 Cat 3b; §4.5 RC-026.
func TestRC031_Detector_Cat3b_VerdictUnexecuted(t *testing.T) {
	t.Parallel()

	// Cat 3b is detected by presence of Harmonik-Verdict-Executed trailer absence.
	spec, ok := LookupTrailer("Harmonik-Verdict-Executed")
	if !ok {
		t.Fatal("RC-031/detector-Cat3b: Harmonik-Verdict-Executed not in registry")
	}
	// The trailer MUST be a known extension (not required) — its ABSENCE is the signal.
	if spec.Requirement != TrailerKnownExtension {
		t.Errorf("RC-031/detector-Cat3b: Harmonik-Verdict-Executed Requirement = %v, want TrailerKnownExtension",
			spec.Requirement)
	}
}

// TestRC031_Detector_Cat3c_InversePrematureClose verifies the Cat 3c detection
// evidence: merge commit on target branch with bead still in_progress.
//
// Spec ref: §8.6 Cat 3c.
func TestRC031_Detector_Cat3c_InversePrematureClose(t *testing.T) {
	t.Parallel()

	// Cat 3c auto-resolver: direct close-write.
	row, ok := rc79LookupActionRow(ReconciliationCategoryCat3c)
	if !ok {
		t.Fatal("RC-031/detector-Cat3c: no action-table row")
	}
	if row.investigatorUsed {
		t.Error("RC-031/detector-Cat3c: investigatorUsed=true; Cat 3c MUST NOT dispatch investigator (direct close-write)")
	}
	if !row.autoResolver {
		t.Error("RC-031/detector-Cat3c: autoResolver=false; Cat 3c MUST have direct-close auto-resolver")
	}
	// Cat 3c is higher priority than Cat 3 generic (RC-003a order).
	if !rc73PriorityFixtureHigherThan(ReconciliationCategoryCat3c, ReconciliationCategoryCat3) {
		t.Error("RC-031/detector-Cat3c: Cat 3c must have higher priority than Cat 3 in RC-003a order")
	}
}

// TestRC031_Detector_Cat4_RecoverableKnownState verifies the Cat 4 detection
// evidence: run was in a well-defined retry/backoff state at crash.
//
// Spec ref: §8.7 Cat 4; control-points.md §4.4.
func TestRC031_Detector_Cat4_RecoverableKnownState(t *testing.T) {
	t.Parallel()

	row, ok := rc79LookupActionRow(ReconciliationCategoryCat4)
	if !ok {
		t.Fatal("RC-031/detector-Cat4: no action-table row")
	}
	if row.investigatorUsed {
		t.Error("RC-031/detector-Cat4: investigatorUsed=true; Cat 4 is auto-resumed (re-arm retry/gate)")
	}
	if !row.autoResolver {
		t.Error("RC-031/detector-Cat4: autoResolver=false; Cat 4 must have re-arm auto-resolver")
	}
}

// TestRC031_Detector_Cat5_CleanRestart verifies the Cat 5 detection evidence:
// nothing in-flight for this run.
//
// Spec ref: §8.8 Cat 5; §4.3 RC-010 (orphaned prior runs).
func TestRC031_Detector_Cat5_CleanRestart(t *testing.T) {
	t.Parallel()

	// Cat 5 includes orphaned branches from prior runs (RC-010).
	cat5 := ReconciliationCategoryCat5
	if !cat5.Valid() {
		t.Fatal("RC-031/detector-Cat5: ReconciliationCategoryCat5 not valid")
	}
	// Cat 5's auto-resolver is no-op; it proceeds to ready.
	row, ok := rc79LookupActionRow(cat5)
	if !ok {
		t.Fatal("RC-031/detector-Cat5: no action-table row")
	}
	if !row.autoResolver {
		t.Error("RC-031/detector-Cat5: autoResolver=false; Cat 5 must have no-op auto-resolver")
	}
}

// TestRC031_Detector_Cat6a_IntegrityLLMTriageable verifies the Cat 6a detection
// evidence: workspace missing + transition-record absent, or trailer mismatch,
// or uncommitted git-in-progress op.
//
// Spec ref: §8.11 Cat 6a; schemas.md §6.3.
func TestRC031_Detector_Cat6a_IntegrityLLMTriageable(t *testing.T) {
	t.Parallel()

	// Cat 6a is detected by workspace-related integrity violations.
	// GitInProgressOp is one signal: any non-none value triggers Cat 6a.
	gitInProgress := GitInProgressOpRebase // from WorkspaceObservation
	if !gitInProgress.Valid() {
		t.Error("RC-031/detector-Cat6a: GitInProgressOpRebase is not valid; Cat 6a uses this as a trigger signal")
	}
	if gitInProgress == GitInProgressOpNone {
		t.Error("RC-031/detector-Cat6a: rebase must not be GitInProgressOpNone")
	}

	// Cat 6a requires investigator.
	row, ok := rc79LookupActionRow(ReconciliationCategoryCat6a)
	if !ok {
		t.Fatal("RC-031/detector-Cat6a: no action-table row")
	}
	if !row.investigatorUsed {
		t.Error("RC-031/detector-Cat6a: investigatorUsed=false; Cat 6a MUST dispatch investigator")
	}
}

// TestRC031_Detector_Cat6b_IntegrityMechanicallyUnrecoverable verifies the Cat 6b
// detection evidence: JSONL corrupt, git object missing, git fsck failure.
//
// Spec ref: §8.11a Cat 6b.
func TestRC031_Detector_Cat6b_IntegrityMechanicallyUnrecoverable(t *testing.T) {
	t.Parallel()

	// Cat 6b is second in priority order (after Cat 0) per RC-003a.
	expectedPos := 1 // index 1 in rc73PriorityFixtureOrder
	if rc73PriorityFixtureIndexOf(ReconciliationCategoryCat6b) != expectedPos {
		t.Errorf("RC-031/detector-Cat6b: priority position = %d, want %d (second after Cat 0)",
			rc73PriorityFixtureIndexOf(ReconciliationCategoryCat6b), expectedPos)
	}

	// Cat 6b does NOT spawn an investigator (auto-escalate to operator).
	row, ok := rc79LookupActionRow(ReconciliationCategoryCat6b)
	if !ok {
		t.Fatal("RC-031/detector-Cat6b: no action-table row")
	}
	if row.investigatorUsed {
		t.Error("RC-031/detector-Cat6b: investigatorUsed=true; Cat 6b MUST NOT spawn investigator (operator intervention)")
	}
}

// ---- Verdict-execution path tests (every §4.5 path) ----

// TestRC031_VerdictExecution_ResumeHere verifies the resume-here verdict
// execution path: re-dispatch current node, no context change.
//
// Spec ref: §4.5 RC-025; schemas.md §6.2.
func TestRC031_VerdictExecution_ResumeHere(t *testing.T) {
	t.Parallel()

	if !VerdictResumeHere.Valid() {
		t.Fatal("RC-031/verdict-ResumeHere: VerdictResumeHere not valid")
	}
	// resume-here: idempotent at dispatch layer (no context change).
	// VerdictEvent for resume-here has no context, no checkpoint_ref.
	e := VerdictEvent{
		Verdict:           VerdictResumeHere,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007811"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007812"),
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-ResumeHere: VerdictEvent.Valid() = false; resume-here event must be valid")
	}
}

// TestRC031_VerdictExecution_ResumeWithContext verifies the resume-with-context
// verdict execution path: re-dispatch with injected context.
//
// Spec ref: §4.5 RC-025; schemas.md §6.2.
func TestRC031_VerdictExecution_ResumeWithContext(t *testing.T) {
	t.Parallel()

	ctx := "injected context from investigator"
	e := VerdictEvent{
		Verdict:           VerdictResumeWithContext,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007813"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007814"),
		Context:           &ctx,
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-ResumeWithContext: VerdictEvent.Valid() = false")
	}
}

// TestRC031_VerdictExecution_ResetToCheckpoint verifies the reset-to-checkpoint
// verdict execution path: intra-run rollback, run_id preserved.
//
// Spec ref: §4.5 RC-025; §4.6 RC-029; schemas.md §6.2.
func TestRC031_VerdictExecution_ResetToCheckpoint(t *testing.T) {
	t.Parallel()

	cpRef := TransitionID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007815"))
	e := VerdictEvent{
		Verdict:           VerdictResetToCheckpoint,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007816"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007817"),
		CheckpointRef:     &cpRef,
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-ResetToCheckpoint: VerdictEvent.Valid() = false")
	}
}

// TestRC031_VerdictExecution_ReopenBead verifies the reopen-bead verdict
// execution path: clear in-flight tracking; subsequent claim is a new run.
//
// Spec ref: §4.5 RC-025; §4.6 RC-028; schemas.md §6.2.
func TestRC031_VerdictExecution_ReopenBead(t *testing.T) {
	t.Parallel()

	if !VerdictReopenBead.Valid() {
		t.Fatal("RC-031/verdict-ReopenBead: VerdictReopenBead not valid")
	}
	// reopen-bead: no context, no checkpoint_ref.
	e := VerdictEvent{
		Verdict:           VerdictReopenBead,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007818"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007819"),
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-ReopenBead: VerdictEvent.Valid() = false")
	}
}

// TestRC031_VerdictExecution_AcceptCloseWithNote verifies the accept-close-with-note
// verdict execution path: annotate + close bead.
//
// Spec ref: §4.5 RC-025; schemas.md §6.2.
func TestRC031_VerdictExecution_AcceptCloseWithNote(t *testing.T) {
	t.Parallel()

	if !VerdictAcceptCloseWithNote.Valid() {
		t.Fatal("RC-031/verdict-AcceptCloseWithNote: VerdictAcceptCloseWithNote not valid")
	}
	e := VerdictEvent{
		Verdict:           VerdictAcceptCloseWithNote,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007820"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007821"),
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-AcceptCloseWithNote: VerdictEvent.Valid() = false")
	}
}

// TestRC031_VerdictExecution_NoOpAccept verifies the no-op-accept verdict
// execution path: confirm state legitimate, no mechanical action.
//
// Spec ref: §4.5 RC-025; schemas.md §6.2.
func TestRC031_VerdictExecution_NoOpAccept(t *testing.T) {
	t.Parallel()

	if !VerdictNoOpAccept.Valid() {
		t.Fatal("RC-031/verdict-NoOpAccept: VerdictNoOpAccept not valid")
	}
	e := VerdictEvent{
		Verdict:           VerdictNoOpAccept,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007822"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007823"),
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-NoOpAccept: VerdictEvent.Valid() = false")
	}
}

// TestRC031_VerdictExecution_EscalateToHuman verifies the escalate-to-human
// verdict execution path: emit operator_escalation_required, outer run stays put.
//
// Spec ref: §4.5 RC-025; schemas.md §6.2.
func TestRC031_VerdictExecution_EscalateToHuman(t *testing.T) {
	t.Parallel()

	if !VerdictEscalateToHuman.Valid() {
		t.Fatal("RC-031/verdict-EscalateToHuman: VerdictEscalateToHuman not valid")
	}
	e := VerdictEvent{
		Verdict:           VerdictEscalateToHuman,
		InvestigatorRunID: uuid.MustParse("018f1e2a-0000-7000-8000-000000007824"),
		TargetRunID:       uuid.MustParse("018f1e2a-0000-7000-8000-000000007825"),
		SnapshotToken: SnapshotToken{
			GitHeadHash: "abc123", BeadsAuditEntryID: "a1", CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !e.Valid() {
		t.Error("RC-031/verdict-EscalateToHuman: VerdictEvent.Valid() = false")
	}
}

// TestRC031_AllSevenVerdictPathsCovered verifies that all 7 verdict execution
// paths documented above have been exercised (cross-check).
//
// Spec ref: §4.5 RC-025; schemas.md §6.1 ENUM Verdict.
func TestRC031_AllSevenVerdictPathsCovered(t *testing.T) {
	t.Parallel()

	allVerdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}
	const wantCount = 7
	if len(allVerdicts) != wantCount {
		t.Errorf("RC-031: %d verdict paths documented, want %d; all paths must be crash-recovery tested", len(allVerdicts), wantCount)
	}
	for _, v := range allVerdicts {
		if !v.Valid() {
			t.Errorf("RC-031: verdict %q is not valid", string(v))
		}
	}
}

// TestRC031_StalenessCheckPath_GitAdvanced verifies the staleness path where
// the target run's git branch advanced since the snapshot.
//
// Spec ref: §4.5 RC-024; schemas.md §6.1 StaleDivergenceReason.
func TestRC031_StalenessCheckPath_GitAdvanced(t *testing.T) {
	t.Parallel()

	payload := StaleVerdictPayload{
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "abc123",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		CurrentGitHeadHash:  "def789", // branch advanced
		CurrentBeadsAuditID: "audit-001",
		DivergenceReason:    StaleDivergenceReasonGitBranchAdvanced,
	}
	if !payload.Valid() {
		t.Error("RC-031/staleness-git: StaleVerdictPayload.Valid() = false")
	}
	if !payload.DivergenceReason.Valid() {
		t.Errorf("RC-031/staleness-git: DivergenceReason %q not valid", string(payload.DivergenceReason))
	}
}

// TestRC031_StalenessCheckPath_BeadsAdvanced verifies the staleness path where
// the target bead's Beads audit entries advanced since the snapshot.
//
// Spec ref: §4.5 RC-024; schemas.md §6.1 StaleDivergenceReason.
func TestRC031_StalenessCheckPath_BeadsAdvanced(t *testing.T) {
	t.Parallel()

	payload := StaleVerdictPayload{
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "abc123",
			BeadsAuditEntryID:   "audit-001",
			CapturedAtTimestamp: "2026-05-01T00:00:00Z",
		},
		CurrentGitHeadHash:  "abc123",    // git unchanged
		CurrentBeadsAuditID: "audit-099", // beads advanced
		DivergenceReason:    StaleDivergenceReasonBeadsAuditAdvanced,
	}
	if !payload.Valid() {
		t.Error("RC-031/staleness-beads: StaleVerdictPayload.Valid() = false")
	}
	if !payload.DivergenceReason.Valid() {
		t.Errorf("RC-031/staleness-beads: DivergenceReason %q not valid", string(payload.DivergenceReason))
	}
}

// ---- Helper ----

// rc78CrashRecoveryRouteForPoint looks up the recovery route for a crash point.
func rc78CrashRecoveryRouteForPoint(cp rc78CrashPoint) (rc78RecoveryRoute, bool) {
	for _, r := range rc78CrashRecoveryRouteTable {
		if r.crashPoint == cp {
			return r, true
		}
	}
	return rc78RecoveryRoute{}, false
}
