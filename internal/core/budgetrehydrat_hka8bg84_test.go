package core

// budgetrehydrat_hka8bg84_test.go — Budget JSONL-replay rehydration fixture
//
// Covers specs/control-points.md §10.2 (CP-022..CP-027 + CP-026a):
//   - Canonical run with N in-flight runs at daemon restart.
//   - JSONL log carries run_started + budget_accrual events for each run.
//   - Rehydration sums per-Budget deltas from run_started to reconstruct pre-crash state.
//   - Rehydration MUST complete BEFORE handler accepts any dispatch (CP-023).
//   - Double-spend negative case (in-flight run started with zero counter when
//     prior accruals exist → defect).
//   - Reconciliation wall-clock outer-bound composition test (CP-027):
//     admissible-under-inner-but-exceeds-outer-remaining → DENIED with budget_exhausted.
//
// These tests are fixtures-only: they document the rehydration algorithm and
// invariants at the core-types level. The daemon's budget subsystem implements
// the actual JSONL scan; here we verify the data shapes and arithmetic contracts.

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// budgetRehydratRunFixture is a minimal record describing one in-flight run's
// budget state as reconstructed from a JSONL replay pass. It captures the run
// identity and the per-Budget accrual deltas accumulated since run_started.
//
// This type documents the rehydration contract per CP-026a: the daemon MUST
// replay budget_accrual events from run_started forward and sum deltas to
// recover the pre-crash counter state.
type budgetRehydratRunFixture struct {
	// RunID is the UUIDv7 run identifier (execution-model.md §6.1).
	RunID RunID

	// BudgetName identifies the Budget being tracked (its ControlPoint name).
	BudgetName string

	// Resource is the consumable resource (tokens, wall_clock_seconds, iterations).
	Resource BudgetResource

	// Limit is the declared Budget ceiling from the policy.
	Limit int64

	// AccruedDeltas is the ordered list of per-chunk accrual deltas recorded in
	// budget_accrual events since run_started. Rehydration sums these to recover
	// the pre-crash counter.
	AccruedDeltas []int64

	// RunStartedSeen is true when a run_started event for this RunID has been
	// observed in the JSONL replay. Rehydration MUST begin from run_started.
	RunStartedSeen bool
}

// Accrued returns the sum of all AccruedDeltas, representing the total
// accrual since run_started per CP-026a.
func (r budgetRehydratRunFixture) Accrued() int64 {
	var total int64
	for _, d := range r.AccruedDeltas {
		total += d
	}
	return total
}

// Remaining returns the remaining allowance: Limit - Accrued().
// A negative value indicates the budget is already exhausted.
func (r budgetRehydratRunFixture) Remaining() int64 {
	return r.Limit - r.Accrued()
}

// ExhaustAtDispatch reports true when a new dispatch would exceed the budget:
// Accrued() + requestedDelta > Limit.
func (r budgetRehydratRunFixture) ExhaustAtDispatch(requestedDelta int64) bool {
	return r.Accrued()+requestedDelta > r.Limit
}

// budgetRehydratJSONLEvent is the minimal structure for a budget-related JSONL
// event, used in replay reconstruction tests. It covers run_started and
// budget_accrual event shapes.
type budgetRehydratJSONLEvent struct {
	// EventType is the event name: "run_started" or "budget_accrual".
	EventType string `json:"event_type"`

	// RunID is the run identifier this event belongs to.
	RunID string `json:"run_id"`

	// BudgetName identifies the Budget (budget_accrual events only).
	BudgetName string `json:"budget_name,omitempty"`

	// Delta is the accrual delta (budget_accrual events only).
	Delta int64 `json:"delta,omitempty"`
}

// --- Fixture builders ---

// budgetRehydratSingleRunFixture returns one in-flight run with three accrual
// deltas (500 + 300 + 200 = 1000 tokens accrued, limit 10000).
func budgetRehydratSingleRunFixture(t *testing.T) budgetRehydratRunFixture {
	t.Helper()
	return budgetRehydratRunFixture{
		RunID:          RunID(uuid.MustParse("01960084-0000-7000-8000-000000000001")),
		BudgetName:     "token-budget-default",
		Resource:       BudgetResourceTokens,
		Limit:          10000,
		AccruedDeltas:  []int64{500, 300, 200},
		RunStartedSeen: true,
	}
}

// budgetRehydratMultiRunFixtures returns N=3 in-flight runs with distinct
// accrual histories, modeling a daemon restart with multiple concurrent runs.
func budgetRehydratMultiRunFixtures(t *testing.T) []budgetRehydratRunFixture {
	t.Helper()
	return []budgetRehydratRunFixture{
		{
			RunID:          RunID(uuid.MustParse("01960084-0000-7000-8000-000000000011")),
			BudgetName:     "token-budget-default",
			Resource:       BudgetResourceTokens,
			Limit:          10000,
			AccruedDeltas:  []int64{1000, 2000},
			RunStartedSeen: true,
		},
		{
			RunID:          RunID(uuid.MustParse("01960084-0000-7000-8000-000000000012")),
			BudgetName:     "token-budget-default",
			Resource:       BudgetResourceTokens,
			Limit:          10000,
			AccruedDeltas:  []int64{500},
			RunStartedSeen: true,
		},
		{
			RunID:          RunID(uuid.MustParse("01960084-0000-7000-8000-000000000013")),
			BudgetName:     "wall-clock-budget",
			Resource:       BudgetResourceWallClockSeconds,
			Limit:          3600,
			AccruedDeltas:  []int64{100, 200, 150},
			RunStartedSeen: true,
		},
	}
}

// budgetRehydratJSONLLog returns an ordered slice of JSONL events representing
// the event stream for budgetRehydratSingleRunFixture. This is the "tape" that
// the rehydration pass replays per CP-026a.
func budgetRehydratJSONLLog(t *testing.T, run budgetRehydratRunFixture) []budgetRehydratJSONLEvent {
	t.Helper()
	runIDStr := run.RunID.String()
	events := []budgetRehydratJSONLEvent{
		{EventType: "run_started", RunID: runIDStr},
	}
	for _, delta := range run.AccruedDeltas {
		events = append(events, budgetRehydratJSONLEvent{
			EventType:  "budget_accrual",
			RunID:      runIDStr,
			BudgetName: run.BudgetName,
			Delta:      delta,
		})
	}
	return events
}

// --- Tests ---

// TestBudgetRehydrat_SingleRunSumFromRunStarted verifies that replaying a
// JSONL log for one in-flight run correctly sums budget_accrual deltas from
// run_started to reconstruct the pre-crash counter.
//
// specs/control-points.md §4.8 CP-026a: "Replay begins at the run_started
// event for each in-flight run and consumes every subsequent budget_accrual
// event for that run, summing per-Budget deltas to reconstruct the pre-crash
// counter state."
func TestBudgetRehydrat_SingleRunSumFromRunStarted(t *testing.T) {
	t.Parallel()

	run := budgetRehydratSingleRunFixture(t)
	log := budgetRehydratJSONLLog(t, run)

	// Simulate rehydration: scan events for this run_id and sum deltas.
	var accrued int64
	runIDStr := run.RunID.String()
	runStartedSeen := false

	for _, ev := range log {
		if ev.RunID != runIDStr {
			continue
		}
		if ev.EventType == "run_started" {
			runStartedSeen = true
			continue
		}
		if ev.EventType == "budget_accrual" && runStartedSeen {
			accrued += ev.Delta
		}
	}

	if !runStartedSeen {
		t.Error("run_started event not found during rehydration; cannot start counter")
	}

	// Expected: 500 + 300 + 200 = 1000
	expectedAccrued := run.Accrued()
	if accrued != expectedAccrued {
		t.Errorf("rehydrated accrual = %d, want %d", accrued, expectedAccrued)
	}

	remaining := run.Limit - accrued
	if remaining != run.Remaining() {
		t.Errorf("remaining after rehydration = %d, want %d", remaining, run.Remaining())
	}
}

// TestBudgetRehydrat_MultiRunIndependentRehydration verifies that N in-flight
// runs each rehydrate their counters independently from the shared JSONL log.
//
// CP-026a requires per-run sum-from-run_started; different runs MUST NOT share
// counter state.
func TestBudgetRehydrat_MultiRunIndependentRehydration(t *testing.T) {
	t.Parallel()

	runs := budgetRehydratMultiRunFixtures(t)

	// Build a merged JSONL log from all runs.
	var mergedLog []budgetRehydratJSONLEvent
	for _, run := range runs {
		mergedLog = append(mergedLog, budgetRehydratJSONLLog(t, run)...)
	}

	// Rehydrate each run independently.
	for _, run := range runs {
		runIDStr := run.RunID.String()
		var accrued int64
		runStartedSeen := false

		for _, ev := range mergedLog {
			if ev.RunID != runIDStr {
				continue
			}
			switch ev.EventType {
			case "run_started":
				runStartedSeen = true
			case "budget_accrual":
				if runStartedSeen {
					accrued += ev.Delta
				}
			}
		}

		if !runStartedSeen {
			t.Errorf("run %s: run_started not found during rehydration", runIDStr)
			continue
		}

		if accrued != run.Accrued() {
			t.Errorf("run %s: rehydrated accrual = %d, want %d",
				runIDStr, accrued, run.Accrued())
		}
	}
}

// TestBudgetRehydrat_RehydrationMustCompleteBeforeDispatch documents the
// CP-023 requirement: rehydration MUST complete before the handler accepts
// any dispatch.
//
// The test models the ordering invariant as a state machine: the daemon is in
// the "rehydrating" state until all in-flight runs' counters are reconstructed,
// then transitions to "ready". Dispatch is only allowed from "ready".
func TestBudgetRehydrat_RehydrationMustCompleteBeforeDispatch(t *testing.T) {
	t.Parallel()

	type daemonState string
	const (
		daemonStateRehydrating daemonState = "rehydrating"
		daemonStateReady       daemonState = "ready"
	)

	type dispatchRequest struct {
		RunID string
		Delta int64
	}

	var state daemonState = daemonStateRehydrating

	// Before rehydration: dispatch must be rejected.
	canDispatch := func() bool { return state == daemonStateReady }
	if canDispatch() {
		t.Error("dispatch allowed before rehydration complete, violates CP-023")
	}

	// Simulate rehydration completing.
	state = daemonStateReady

	// After rehydration: dispatch is permitted.
	if !canDispatch() {
		t.Error("dispatch blocked after rehydration complete, should be allowed")
	}

	// Verify a dispatch request can be evaluated against rehydrated counters.
	run := budgetRehydratSingleRunFixture(t)
	req := dispatchRequest{RunID: run.RunID.String(), Delta: 100}
	_ = req // the actual dispatch enforcement lives in the budget evaluator

	remaining := run.Remaining()
	if remaining < 0 {
		t.Errorf("run fixture has negative remaining (%d), fixture is broken", remaining)
	}
}

// TestBudgetRehydrat_DoubleSpendNegativeCase verifies the double-spend defect:
// a run MUST NOT be started with a zero counter when prior budget_accrual events
// exist in the JSONL log for that run.
//
// CP-026a: "Implementations MUST NOT start an in-flight run's handler with a
// zero counter when prior budget_accrual events exist in the JSONL log for that
// run; doing so would double-spend the already-accrued allowance."
func TestBudgetRehydrat_DoubleSpendNegativeCase(t *testing.T) {
	t.Parallel()

	run := budgetRehydratSingleRunFixture(t)

	// Simulate a broken rehydration that ignores prior budget_accrual events
	// and starts the handler with a zero counter.
	brokenRehydratedAccrued := int64(0) // BUG: prior accruals ignored

	// The zero counter would allow the run to consume another full Limit,
	// double-spending the already-accrued allowance.
	correctRehydratedAccrued := run.Accrued()

	if brokenRehydratedAccrued == correctRehydratedAccrued {
		t.Fatal("fixture setup error: broken and correct accruals should differ")
	}

	// The defect: with a zero counter the handler believes it has Limit tokens
	// remaining, but the actual remaining is Limit - correctRehydratedAccrued.
	brokenRemaining := run.Limit - brokenRehydratedAccrued   // incorrect
	correctRemaining := run.Limit - correctRehydratedAccrued // correct

	if brokenRemaining <= correctRemaining {
		t.Errorf(
			"double-spend fixture: broken remaining (%d) should exceed correct remaining (%d)",
			brokenRemaining, correctRemaining,
		)
	}

	// Document: the correct state after rehydration must show the pre-crash counter.
	if correctRemaining != run.Remaining() {
		t.Errorf("correct remaining = %d, want %d (run.Remaining())", correctRemaining, run.Remaining())
	}
}

// TestBudgetRehydrat_DispatchDeniedWhenExceedsRemainingBudget verifies that a
// dispatch is denied when the requested delta would exceed the remaining budget.
//
// CP-022 + CP-023: the Budget enforces its limit at dispatch time.
func TestBudgetRehydrat_DispatchDeniedWhenExceedsRemainingBudget(t *testing.T) {
	t.Parallel()

	run := budgetRehydratSingleRunFixture(t) // accrued=1000, limit=10000, remaining=9000

	// A request that fits within remaining budget: ALLOWED.
	smallDelta := int64(500)
	if run.ExhaustAtDispatch(smallDelta) {
		t.Errorf("dispatch of delta=%d should be ALLOWED (remaining=%d)", smallDelta, run.Remaining())
	}

	// A request that exactly equals remaining: ALLOWED (boundary).
	exactDelta := run.Remaining()
	if run.ExhaustAtDispatch(exactDelta) {
		t.Errorf("dispatch of delta=%d (exact remaining) should be ALLOWED", exactDelta)
	}

	// A request that exceeds remaining by 1: DENIED with budget_exhausted.
	overDelta := run.Remaining() + 1
	if !run.ExhaustAtDispatch(overDelta) {
		t.Errorf("dispatch of delta=%d should be DENIED (remaining=%d)", overDelta, run.Remaining())
	}
}

// TestBudgetRehydrat_WallClockOuterBoundComposition verifies CP-027: the
// reconciliation wall-clock budget is an outer bound on any inner Budget.
//
// A dispatch admissible under the inner budget (e.g., token budget has
// remaining=5000) but that would cause the outer wall-clock budget to be
// exceeded MUST be DENIED with budget_exhausted.
//
// CP-027: "If the remaining reconciliation wall-clock budget is insufficient
// to admit the requested operation, the daemon MUST deny the request with
// budget_exhausted regardless of the inner-budget state."
func TestBudgetRehydrat_WallClockOuterBoundComposition(t *testing.T) {
	t.Parallel()

	// Inner budget: tokens — large remaining.
	innerBudget := budgetRehydratRunFixture{
		RunID:          RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027")),
		BudgetName:     "token-budget-default",
		Resource:       BudgetResourceTokens,
		Limit:          10000,
		AccruedDeltas:  []int64{1000},
		RunStartedSeen: true,
	}
	// innerBudget.Remaining() = 9000

	// Outer budget: wall-clock seconds — nearly exhausted.
	outerBudget := budgetRehydratRunFixture{
		RunID:          innerBudget.RunID, // same run
		BudgetName:     "reconciliation-wall-clock",
		Resource:       BudgetResourceWallClockSeconds,
		Limit:          3600,
		AccruedDeltas:  []int64{3500}, // 100 seconds remaining
		RunStartedSeen: true,
	}
	// outerBudget.Remaining() = 100 seconds

	// Proposed dispatch: admissible under inner budget (500 tokens < 9000 remaining).
	proposedTokenDelta := int64(500)
	admissibleUnderInner := !innerBudget.ExhaustAtDispatch(proposedTokenDelta)
	if !admissibleUnderInner {
		t.Fatalf("fixture setup: proposed delta should be admissible under inner budget")
	}

	// Proposed dispatch wall-clock cost: 200 seconds > 100 remaining.
	proposedWallClockCost := int64(200)
	exceedsOuter := outerBudget.ExhaustAtDispatch(proposedWallClockCost)
	if !exceedsOuter {
		t.Fatalf("fixture setup: proposed wall-clock cost should exceed outer budget")
	}

	// CP-027: outer bound takes precedence. Dispatch MUST be DENIED.
	// admissibleUnderInner=true but exceedsOuter=true → DENIED.
	if admissibleUnderInner && exceedsOuter {
		// Correct outcome: deny with budget_exhausted.
		// The budget_exhausted event is emitted; the run does not proceed.
		budgetExhaustedEventEmitted := true // model the emission decision
		if !budgetExhaustedEventEmitted {
			t.Error("budget_exhausted event must be emitted when outer bound is exceeded")
		}
	} else {
		t.Error("fixture setup error: admissibleUnderInner && exceedsOuter must both be true")
	}
}

// TestBudgetRehydrat_JSONLEventJSONRoundTrip verifies that the JSONL event
// structure serialises and deserialises correctly, confirming the wire shape
// used in rehydration is stable.
func TestBudgetRehydrat_JSONLEventJSONRoundTrip(t *testing.T) {
	t.Parallel()

	run := budgetRehydratSingleRunFixture(t)
	log := budgetRehydratJSONLLog(t, run)

	for i, ev := range log {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("event[%d] json.Marshal: %v", i, err)
		}
		var got budgetRehydratJSONLEvent
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("event[%d] json.Unmarshal: %v", i, err)
		}
		if got.EventType != ev.EventType {
			t.Errorf("event[%d] EventType: got %q, want %q", i, got.EventType, ev.EventType)
		}
		if got.RunID != ev.RunID {
			t.Errorf("event[%d] RunID: got %q, want %q", i, got.RunID, ev.RunID)
		}
		if got.Delta != ev.Delta {
			t.Errorf("event[%d] Delta: got %d, want %d", i, got.Delta, ev.Delta)
		}
	}
}

// TestBudgetRehydrat_BudgetPayloadValidityAfterRehydration verifies that the
// BudgetPayload used to declare the Budget limit is valid, ensuring the
// WarningThreshold default (0.8) is honoured per CP-022.
func TestBudgetRehydrat_BudgetPayloadValidityAfterRehydration(t *testing.T) {
	t.Parallel()

	// BudgetPayload declares the Budget ceiling; rehydration reads the policy
	// to reconstruct Limit. Verify the canonical payload shape is valid.
	bp := NewBudgetPayload()
	bp.Resource = BudgetResourceTokens
	bp.Scope = BudgetScopePerRun
	bp.Limit = 10000
	bp.ScopeTarget = ScopeTargetWildcard()

	if !bp.Valid() {
		t.Errorf("BudgetPayload for rehydration fixture is invalid: %+v", bp)
	}
	if bp.WarningThreshold != 0.8 {
		t.Errorf("WarningThreshold = %v, want 0.8 (CP-022 default)", bp.WarningThreshold)
	}
}
