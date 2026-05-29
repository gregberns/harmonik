package core

import (
	"testing"

	"github.com/google/uuid"
)

// Tests for CheckBudgetAtDispatch per specs/control-points.md §4.5.CP-023:
// "The agent runner MUST check the Budget's remaining allowance AT DISPATCH
// (pre-exhaustion). If the pending dispatch would exceed the remaining limit,
// the runner MUST emit a budget_exhausted event … and DENY the dispatch."
//
// Exhaustion condition: accrued + attemptedCost > limit → DENY.
// Boundary: accrued + attemptedCost == limit → ADMIT.
//
// Refs: hk-a8bg.22

// budgetDispatchRunID is a stable RunID used across dispatch-check tests.
var budgetDispatchRunID = RunID(uuid.MustParse("01960084-0000-7000-8000-000000000022"))

// budgetDispatchRef is a stable BudgetRef used across dispatch-check tests.
const budgetDispatchRef BudgetRef = "token-budget-default"

// TestCheckBudgetAtDispatch_AdmitWhenCostWithinRemaining verifies that a
// dispatch is admitted when accrued + cost < limit.
func TestCheckBudgetAtDispatch_AdmitWhenCostWithinRemaining(t *testing.T) {
	t.Parallel()

	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 1_000, 500.0)
	if denied {
		t.Error("CheckBudgetAtDispatch: want admitted (cost within remaining), got denied")
	}
}

// TestCheckBudgetAtDispatch_AdmitAtExactBoundary verifies that a dispatch is
// admitted when accrued + cost == limit (boundary is inclusive).
func TestCheckBudgetAtDispatch_AdmitAtExactBoundary(t *testing.T) {
	t.Parallel()

	// accrued=1000, cost=9000.0 → total=10000 == limit=10000 → ADMIT.
	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 1_000, 9_000.0)
	if denied {
		t.Error("CheckBudgetAtDispatch: want admitted at exact boundary, got denied")
	}
}

// TestCheckBudgetAtDispatch_DenyWhenCostExceedsRemaining verifies that a
// dispatch is denied when accrued + cost > limit.
func TestCheckBudgetAtDispatch_DenyWhenCostExceedsRemaining(t *testing.T) {
	t.Parallel()

	// accrued=9500, cost=600.0 → total=10100 > limit=10000 → DENY.
	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 9_500, 600.0)
	if !denied {
		t.Error("CheckBudgetAtDispatch: want denied (cost exceeds remaining), got admitted")
	}
}

// TestCheckBudgetAtDispatch_DenyByOneUnit verifies the boundary: a dispatch
// that exceeds the remaining by the smallest positive amount is denied.
func TestCheckBudgetAtDispatch_DenyByOneUnit(t *testing.T) {
	t.Parallel()

	// accrued=1000, cost=9001.0 → total=10001 > limit=10000 → DENY.
	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 1_000, 9_001.0)
	if !denied {
		t.Error("CheckBudgetAtDispatch: want denied (cost exceeds remaining by 1), got admitted")
	}
}

// TestCheckBudgetAtDispatch_ZeroAccrued verifies that a zero-accrual run
// admits dispatches within limit and denies those that exceed it.
func TestCheckBudgetAtDispatch_ZeroAccrued(t *testing.T) {
	t.Parallel()

	// No accrual yet; full limit available.
	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 0, 5_000.0)
	if denied {
		t.Error("CheckBudgetAtDispatch/zero-accrued: want admitted, got denied")
	}

	// Cost equals full limit: ADMIT.
	_, denied = CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 0, 10_000.0)
	if denied {
		t.Error("CheckBudgetAtDispatch/zero-accrued-full-limit: want admitted, got denied")
	}

	// Cost exceeds full limit by 1: DENY.
	_, denied = CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 0, 10_001.0)
	if !denied {
		t.Error("CheckBudgetAtDispatch/zero-accrued-over-limit: want denied, got admitted")
	}
}

// TestCheckBudgetAtDispatch_DenialPayloadHasCorrectRunID verifies that the
// returned BudgetExhaustedEventPayload carries the caller-supplied RunID.
func TestCheckBudgetAtDispatch_DenialPayloadHasCorrectRunID(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000099"))
	payload, denied := CheckBudgetAtDispatch(runID, budgetDispatchRef,
		10_000, 9_500, 600.0)
	if !denied {
		t.Fatal("CheckBudgetAtDispatch: want denied to test payload, got admitted")
	}
	if payload.RunID != runID {
		t.Errorf("payload.RunID = %v, want %v", payload.RunID, runID)
	}
}

// TestCheckBudgetAtDispatch_DenialPayloadHasCorrectBudgetRef verifies that the
// returned payload carries the caller-supplied BudgetRef.
func TestCheckBudgetAtDispatch_DenialPayloadHasCorrectBudgetRef(t *testing.T) {
	t.Parallel()

	const ref BudgetRef = "reconciliation-wall-clock"
	payload, denied := CheckBudgetAtDispatch(budgetDispatchRunID, ref,
		3_600, 3_500, 200.0)
	if !denied {
		t.Fatal("CheckBudgetAtDispatch: want denied to test payload, got admitted")
	}
	if payload.BudgetRef != ref {
		t.Errorf("payload.BudgetRef = %q, want %q", payload.BudgetRef, ref)
	}
}

// TestCheckBudgetAtDispatch_DenialPayloadHasCorrectAttemptedCost verifies
// that the returned payload carries the caller-supplied attempted cost.
func TestCheckBudgetAtDispatch_DenialPayloadHasCorrectAttemptedCost(t *testing.T) {
	t.Parallel()

	const cost = 750.5
	payload, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 9_500, cost)
	if !denied {
		t.Fatal("CheckBudgetAtDispatch: want denied to test payload, got admitted")
	}
	if payload.AttemptedDispatchCost != cost {
		t.Errorf("payload.AttemptedDispatchCost = %v, want %v", payload.AttemptedDispatchCost, cost)
	}
}

// TestCheckBudgetAtDispatch_AdmittedReturnsZeroPayload verifies that the
// payload returned on admission is the zero value (callers must not read it).
func TestCheckBudgetAtDispatch_AdmittedReturnsZeroPayload(t *testing.T) {
	t.Parallel()

	payload, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 1_000, 500.0)
	if denied {
		t.Fatal("CheckBudgetAtDispatch: want admitted, got denied")
	}
	// On admission the payload must be the zero BudgetExhaustedEventPayload.
	zero := BudgetExhaustedEventPayload{}
	if payload != zero {
		t.Errorf("CheckBudgetAtDispatch: admitted payload = %+v, want zero value", payload)
	}
}

// TestCheckBudgetAtDispatch_DeniedPayloadIsValid verifies that the
// BudgetExhaustedEventPayload returned on denial passes Valid().
func TestCheckBudgetAtDispatch_DeniedPayloadIsValid(t *testing.T) {
	t.Parallel()

	payload, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 9_500, 600.0)
	if !denied {
		t.Fatal("CheckBudgetAtDispatch: want denied, got admitted")
	}
	if !payload.Valid() {
		t.Errorf("denied payload is not valid: %+v", payload)
	}
}

// TestCheckBudgetAtDispatch_WallClockResource verifies the check works
// correctly for wall-clock-second budgets (a common second resource type).
func TestCheckBudgetAtDispatch_WallClockResource(t *testing.T) {
	t.Parallel()

	const wallClockRef BudgetRef = "wall-clock-budget"
	const limit = int64(1800) // 30 minutes

	// 1700 seconds accrued; 100 seconds remaining.

	// 90 seconds attempted: ADMIT.
	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, wallClockRef,
		limit, 1700, 90.0)
	if denied {
		t.Error("CheckBudgetAtDispatch/wall-clock: 90s cost should be admitted with 100s remaining")
	}

	// 100 seconds attempted: ADMIT (exact boundary).
	_, denied = CheckBudgetAtDispatch(budgetDispatchRunID, wallClockRef,
		limit, 1700, 100.0)
	if denied {
		t.Error("CheckBudgetAtDispatch/wall-clock: 100s cost at exact boundary should be admitted")
	}

	// 101 seconds attempted: DENY.
	_, denied = CheckBudgetAtDispatch(budgetDispatchRunID, wallClockRef,
		limit, 1700, 101.0)
	if !denied {
		t.Error("CheckBudgetAtDispatch/wall-clock: 101s cost exceeds 100s remaining, should be denied")
	}
}

// TestCheckBudgetAtDispatch_FullyExhaustedBudget verifies that when accrued
// equals the limit, any positive cost is denied.
func TestCheckBudgetAtDispatch_FullyExhaustedBudget(t *testing.T) {
	t.Parallel()

	// accrued == limit → remaining == 0. Any positive cost is denied.
	_, denied := CheckBudgetAtDispatch(budgetDispatchRunID, budgetDispatchRef,
		10_000, 10_000, 0.001)
	if !denied {
		t.Error("CheckBudgetAtDispatch: want denied when budget fully exhausted, got admitted")
	}
}
