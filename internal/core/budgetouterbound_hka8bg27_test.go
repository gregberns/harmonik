package core

// budgetouterbound_hka8bg27_test.go — tests for CP-027 composition rule.
//
// Covers specs/control-points.md §4.5.CP-027 and §10.2:
//   - NewReconciliationWallClockBudget constructs a valid BudgetPayload with
//     resource=wall_clock_seconds, scope=per_run, scope_target=singleton(runID).
//   - CheckWallClockOuterBound admits dispatches within the outer limit.
//   - CheckWallClockOuterBound denies dispatches that exceed the outer limit
//     even when admissible under an inner budget.
//   - Exhaustion predicate is strict: exactly reaching the limit is admitted.
//
// Refs: hk-a8bg.27

import (
	"testing"

	"github.com/google/uuid"
)

// --- NewReconciliationWallClockBudget ---

func TestNewReconciliationWallClockBudget_Valid(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))
	bp, err := NewReconciliationWallClockBudget(runID, 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bp.Valid() {
		t.Errorf("BudgetPayload.Valid() = false, want true: %+v", bp)
	}
	if bp.Resource != BudgetResourceWallClockSeconds {
		t.Errorf("Resource = %q, want %q", bp.Resource, BudgetResourceWallClockSeconds)
	}
	if bp.Scope != BudgetScopePerRun {
		t.Errorf("Scope = %q, want %q", bp.Scope, BudgetScopePerRun)
	}
	if bp.Limit != 600 {
		t.Errorf("Limit = %d, want 600", bp.Limit)
	}
	if bp.WarningThreshold != 0.8 {
		t.Errorf("WarningThreshold = %v, want 0.8 (CP-022 default)", bp.WarningThreshold)
	}
	if bp.ScopeTarget.Kind != ScopeTargetKindSingleton {
		t.Errorf("ScopeTarget.Kind = %q, want %q", bp.ScopeTarget.Kind, ScopeTargetKindSingleton)
	}
	if len(bp.ScopeTarget.IDs) != 1 || bp.ScopeTarget.IDs[0] != runID.String() {
		t.Errorf("ScopeTarget.IDs = %v, want [%s]", bp.ScopeTarget.IDs, runID.String())
	}
}

func TestNewReconciliationWallClockBudget_ZeroRunID(t *testing.T) {
	t.Parallel()

	_, err := NewReconciliationWallClockBudget(RunID(uuid.Nil), 600)
	if err == nil {
		t.Error("expected error for zero RunID, got nil")
	}
}

func TestNewReconciliationWallClockBudget_ZeroWallClockSeconds(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))
	_, err := NewReconciliationWallClockBudget(runID, 0)
	if err == nil {
		t.Error("expected error for wallClockSeconds=0, got nil")
	}
}

func TestNewReconciliationWallClockBudget_NegativeWallClockSeconds(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))
	_, err := NewReconciliationWallClockBudget(runID, -1)
	if err == nil {
		t.Error("expected error for wallClockSeconds=-1, got nil")
	}
}

// --- CheckWallClockOuterBound ---

func TestCheckWallClockOuterBound_AdmittedWithinLimit(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))
	// outerLimit=3600s, accrued=3500s, remaining=100s; proposed cost=50s → ADMITTED.
	payload, denied := CheckWallClockOuterBound(runID, ReconciliationWallClockBudgetRef, 3600, 3500, 50)
	if denied {
		t.Errorf("dispatch should be ADMITTED (50s cost < 100s remaining), got DENIED: %+v", payload)
	}
}

func TestCheckWallClockOuterBound_DeniedExceedsLimit(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))
	// outerLimit=3600s, accrued=3500s, remaining=100s; proposed cost=200s → DENIED.
	payload, denied := CheckWallClockOuterBound(runID, ReconciliationWallClockBudgetRef, 3600, 3500, 200)
	if !denied {
		t.Error("dispatch should be DENIED (200s cost > 100s remaining), got ADMITTED")
	}
	if payload.RunID != runID {
		t.Errorf("payload.RunID = %v, want %v", payload.RunID, runID)
	}
	if payload.BudgetRef != ReconciliationWallClockBudgetRef {
		t.Errorf("payload.BudgetRef = %q, want %q", payload.BudgetRef, ReconciliationWallClockBudgetRef)
	}
	if payload.AttemptedDispatchCost != 200 {
		t.Errorf("payload.AttemptedDispatchCost = %v, want 200", payload.AttemptedDispatchCost)
	}
}

// TestCheckWallClockOuterBound_ExactlyReachesLimit verifies that a dispatch
// that exactly exhausts the outer budget is ADMITTED per the strict predicate
// (accrued + cost > limit is the denial condition; == limit is allowed).
func TestCheckWallClockOuterBound_ExactlyReachesLimit(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))
	// outerLimit=3600s, accrued=3500s, remaining=100s; proposed cost=100s → ADMITTED (boundary).
	_, denied := CheckWallClockOuterBound(runID, ReconciliationWallClockBudgetRef, 3600, 3500, 100)
	if denied {
		t.Error("dispatch should be ADMITTED when cost == remaining (boundary case), got DENIED")
	}
}

// TestCheckWallClockOuterBound_OuterWinsOverInner verifies the CP-027
// composition rule: a dispatch admissible under an inner budget (tokens) but
// that would exceed the outer wall-clock budget is DENIED.
func TestCheckWallClockOuterBound_OuterWinsOverInner(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000027"))

	// Inner budget (tokens): large remaining — dispatch would be admitted.
	innerLimit := int64(10000)
	innerAccrued := int64(1000)
	proposedTokenCost := float64(500)
	admissibleUnderInner := innerAccrued+int64(proposedTokenCost) <= innerLimit
	if !admissibleUnderInner {
		t.Fatal("fixture: dispatch must be admissible under inner token budget")
	}

	// Outer budget (wall-clock): nearly exhausted — 100s remaining.
	outerLimit := int64(3600)
	outerAccrued := int64(3500)
	proposedWallClockCost := float64(200) // exceeds the 100s remaining

	// CP-027: outer bound wins. Dispatch MUST be DENIED.
	_, denied := CheckWallClockOuterBound(runID, ReconciliationWallClockBudgetRef, outerLimit, outerAccrued, proposedWallClockCost)
	if !denied {
		t.Error("CP-027 violation: dispatch admissible under inner budget but exceeding outer wall-clock budget must be DENIED")
	}
}
