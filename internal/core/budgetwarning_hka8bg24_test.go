package core

import (
	"testing"

	"github.com/google/uuid"
)

// Tests for CheckBudgetWarningThreshold per specs/control-points.md §4.5.CP-025:
// "When cumulative accrual crosses the warning_threshold fraction of limit,
// the runner MUST emit a budget_warning event … and continue."
//
// Warning condition: accrued >= warningThreshold * limit → WARN.
// Below threshold: accrued < warningThreshold * limit → no warning.
//
// Refs: hk-a8bg.24

// budgetWarnRunID is a stable RunID used across warning-threshold tests.
var budgetWarnRunID = RunID(uuid.MustParse("01960084-0000-7000-8000-000000000024"))

// budgetWarnRef is a stable BudgetRef used across warning-threshold tests.
const budgetWarnRef BudgetRef = "token-budget-default"

// TestCheckBudgetWarningThreshold_NoWarnBelowThreshold verifies that no warning
// fires when accrual is strictly below the threshold.
func TestCheckBudgetWarningThreshold_NoWarnBelowThreshold(t *testing.T) {
	t.Parallel()

	// limit=10000, threshold=0.8 → fires at 8000. accrued=7999 → no warn.
	_, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 7_999.0)
	if warned {
		t.Error("CheckBudgetWarningThreshold: want no warning below threshold, got warning")
	}
}

// TestCheckBudgetWarningThreshold_WarnAtExactThreshold verifies that the warning
// fires when accrual exactly equals warningThreshold * limit.
func TestCheckBudgetWarningThreshold_WarnAtExactThreshold(t *testing.T) {
	t.Parallel()

	// limit=10000, threshold=0.8 → fires at 8000. accrued=8000 → WARN.
	_, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 8_000.0)
	if !warned {
		t.Error("CheckBudgetWarningThreshold: want warning at exact threshold boundary, got none")
	}
}

// TestCheckBudgetWarningThreshold_WarnAboveThreshold verifies that the warning
// fires when accrual exceeds the threshold.
func TestCheckBudgetWarningThreshold_WarnAboveThreshold(t *testing.T) {
	t.Parallel()

	// limit=10000, threshold=0.8 → fires at 8000. accrued=9000 → WARN.
	_, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 9_000.0)
	if !warned {
		t.Error("CheckBudgetWarningThreshold: want warning above threshold, got none")
	}
}

// TestCheckBudgetWarningThreshold_WarnPayloadThresholdFraction verifies that the
// returned payload carries the caller-supplied warningThreshold.
func TestCheckBudgetWarningThreshold_WarnPayloadThresholdFraction(t *testing.T) {
	t.Parallel()

	const threshold = 0.8
	payload, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, threshold, 9_000.0)
	if !warned {
		t.Fatal("CheckBudgetWarningThreshold: want warning to test payload, got none")
	}
	if payload.ThresholdFraction != threshold {
		t.Errorf("payload.ThresholdFraction = %v, want %v", payload.ThresholdFraction, threshold)
	}
}

// TestCheckBudgetWarningThreshold_WarnPayloadRemaining verifies that the returned
// payload Remaining is limit minus accrued.
func TestCheckBudgetWarningThreshold_WarnPayloadRemaining(t *testing.T) {
	t.Parallel()

	// limit=10000, accrued=9000 → remaining=1000.
	payload, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 9_000.0)
	if !warned {
		t.Fatal("CheckBudgetWarningThreshold: want warning to test payload, got none")
	}
	const wantRemaining = 1_000.0
	if payload.Remaining != wantRemaining {
		t.Errorf("payload.Remaining = %v, want %v", payload.Remaining, wantRemaining)
	}
}

// TestCheckBudgetWarningThreshold_WarnPayloadRunID verifies that the returned
// payload carries the caller-supplied RunID.
func TestCheckBudgetWarningThreshold_WarnPayloadRunID(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("01960084-0000-7000-8000-000000000099"))
	payload, warned := CheckBudgetWarningThreshold(runID, budgetWarnRef,
		10_000, 0.8, 9_000.0)
	if !warned {
		t.Fatal("CheckBudgetWarningThreshold: want warning to test payload, got none")
	}
	if payload.RunID != runID {
		t.Errorf("payload.RunID = %v, want %v", payload.RunID, runID)
	}
}

// TestCheckBudgetWarningThreshold_WarnPayloadBudgetRef verifies that the returned
// payload carries the caller-supplied BudgetRef.
func TestCheckBudgetWarningThreshold_WarnPayloadBudgetRef(t *testing.T) {
	t.Parallel()

	const ref BudgetRef = "custom-token-budget"
	payload, warned := CheckBudgetWarningThreshold(budgetWarnRunID, ref,
		10_000, 0.8, 9_000.0)
	if !warned {
		t.Fatal("CheckBudgetWarningThreshold: want warning to test payload, got none")
	}
	if payload.BudgetRef != ref {
		t.Errorf("payload.BudgetRef = %q, want %q", payload.BudgetRef, ref)
	}
}

// TestCheckBudgetWarningThreshold_WarnPayloadIsValid verifies that the payload
// returned when the warning fires passes BudgetWarningPayload.Valid().
func TestCheckBudgetWarningThreshold_WarnPayloadIsValid(t *testing.T) {
	t.Parallel()

	payload, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 9_000.0)
	if !warned {
		t.Fatal("CheckBudgetWarningThreshold: want warning to test payload, got none")
	}
	if !payload.Valid() {
		t.Errorf("warning payload is not valid: %+v", payload)
	}
}

// TestCheckBudgetWarningThreshold_NoWarnReturnsZeroPayload verifies that the
// payload returned when no warning fires is the zero value.
func TestCheckBudgetWarningThreshold_NoWarnReturnsZeroPayload(t *testing.T) {
	t.Parallel()

	payload, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 7_999.0)
	if warned {
		t.Fatal("CheckBudgetWarningThreshold: want no warning, got warning")
	}
	zero := BudgetWarningPayload{}
	if payload != zero {
		t.Errorf("no-warn payload = %+v, want zero value", payload)
	}
}

// TestCheckBudgetWarningThreshold_RemainingClampedAtZero verifies that Remaining
// is clamped to 0 when accrued exceeds limit (over-accrual edge case).
func TestCheckBudgetWarningThreshold_RemainingClampedAtZero(t *testing.T) {
	t.Parallel()

	// accrued=12000 > limit=10000 → Remaining must be 0, not negative.
	payload, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		10_000, 0.8, 12_000.0)
	if !warned {
		t.Fatal("CheckBudgetWarningThreshold: want warning when accrued > limit, got none")
	}
	if payload.Remaining < 0 {
		t.Errorf("payload.Remaining = %v, want >= 0 (clamped)", payload.Remaining)
	}
	if payload.Remaining != 0 {
		t.Errorf("payload.Remaining = %v, want 0 (clamped) when accrued > limit", payload.Remaining)
	}
}

// TestCheckBudgetWarningThreshold_DefaultThreshold80Percent verifies the CP-022
// default: warning_threshold=0.8 fires at 80% of limit.
func TestCheckBudgetWarningThreshold_DefaultThreshold80Percent(t *testing.T) {
	t.Parallel()

	const limit = int64(10_000)
	const threshold = defaultWarningThreshold // 0.8 per CP-022

	// 79.9% accrued → below threshold → no warn.
	_, warned := CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		limit, threshold, 7_990.0)
	if warned {
		t.Error("CheckBudgetWarningThreshold: want no warning at 79.9%, got warning")
	}

	// 80.0% accrued → at threshold → WARN.
	_, warned = CheckBudgetWarningThreshold(budgetWarnRunID, budgetWarnRef,
		limit, threshold, 8_000.0)
	if !warned {
		t.Error("CheckBudgetWarningThreshold: want warning at exactly 80%, got none")
	}
}

// TestCheckBudgetWarningThreshold_WallClockResource verifies the check works
// for wall-clock-second budgets (resource-agnostic; units are caller-supplied).
func TestCheckBudgetWarningThreshold_WallClockResource(t *testing.T) {
	t.Parallel()

	const wallClockRef BudgetRef = "reconciliation-wall-clock"
	const limit = int64(3600) // 60 minutes

	// threshold=0.8 → fires at 2880s. accrued=2879s → no warn.
	_, warned := CheckBudgetWarningThreshold(budgetWarnRunID, wallClockRef,
		limit, 0.8, 2_879.0)
	if warned {
		t.Error("CheckBudgetWarningThreshold/wall-clock: want no warning at 2879s, got warning")
	}

	// accrued=2880s → at threshold → WARN.
	_, warned = CheckBudgetWarningThreshold(budgetWarnRunID, wallClockRef,
		limit, 0.8, 2_880.0)
	if !warned {
		t.Error("CheckBudgetWarningThreshold/wall-clock: want warning at 2880s (80%), got none")
	}
}
