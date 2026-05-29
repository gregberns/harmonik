package core

// budgetouterbound_hka8bg27.go — CP-027: Reconciliation wall-clock budget is an outer bound.
//
// Implements the composition rule from specs/control-points.md §4.5.CP-027:
//
//	When a reconciliation wall-clock Budget is active for a run, inner
//	per-role or per-state Budgets MUST NOT extend the effective wall-clock
//	beyond the outer bound. On any conflict between an inner Budget limit
//	and the outer wall-clock Budget remaining, the outer Budget wins
//	(a dispatch admissible under the inner Budget but not the outer's
//	remaining allowance is DENIED with failure class budget_exhausted per
//	[execution-model.md §8.5]).
//
// Refs: hk-a8bg.27

import (
	"fmt"

	"github.com/google/uuid"
)

// ReconciliationWallClockBudgetRef is the canonical BudgetRef name for the
// per-run wall-clock Budget registered by every reconciliation workflow per
// CP-027 and reconciliation/spec.md §4.4 RC-017.
const ReconciliationWallClockBudgetRef BudgetRef = "reconciliation-wall-clock"

// NewReconciliationWallClockBudget constructs a BudgetPayload for the
// mandatory reconciliation wall-clock outer bound declared by CP-027.
//
// The returned payload registers with:
//   - Resource      = BudgetResourceWallClockSeconds
//   - Scope         = BudgetScopePerRun
//   - ScopeTarget   = singleton(<reconciliationRunID>)
//   - Limit         = wallClockSeconds
//   - WarningThreshold = 0.8 (CP-022 default)
//
// Returns an error when reconciliationRunID is the zero RunID or
// wallClockSeconds is < 1.
func NewReconciliationWallClockBudget(reconciliationRunID RunID, wallClockSeconds int64) (BudgetPayload, error) {
	if reconciliationRunID == RunID(uuid.Nil) {
		return BudgetPayload{}, fmt.Errorf("budgetouterbound: reconciliationRunID must not be the zero RunID")
	}
	if wallClockSeconds < 1 {
		return BudgetPayload{}, fmt.Errorf("budgetouterbound: wallClockSeconds must be >= 1, got %d", wallClockSeconds)
	}
	scopeTarget, err := ScopeTargetSingleton(reconciliationRunID.String())
	if err != nil {
		return BudgetPayload{}, fmt.Errorf("budgetouterbound: ScopeTargetSingleton: %w", err)
	}
	bp := NewBudgetPayload()
	bp.Resource = BudgetResourceWallClockSeconds
	bp.Scope = BudgetScopePerRun
	bp.ScopeTarget = scopeTarget
	bp.Limit = wallClockSeconds
	return bp, nil
}

// CheckWallClockOuterBound evaluates the CP-027 composition rule: a dispatch
// that would cause the outer reconciliation wall-clock budget to be exceeded
// is DENIED regardless of whether it was admissible under an inner budget.
//
// Parameters:
//   - runID:                 the RunID of the reconciliation run
//   - outerBudgetRef:        the BudgetRef for the outer wall-clock budget
//   - outerLimit:            the declared wall-clock ceiling (seconds)
//   - outerAccrued:          the total accrued wall-clock seconds since run_started
//   - proposedWallClockCost: the estimated wall-clock cost of the pending dispatch
//
// Returns (BudgetExhaustedEventPayload, true) when the dispatch is DENIED —
// the caller MUST emit the payload as a budget_exhausted event per
// event-model.md §8.4.3 and MUST NOT launch the handler.
//
// Returns (zero BudgetExhaustedEventPayload, false) when the dispatch is
// ADMITTED — outerAccrued + proposedWallClockCost does not exceed outerLimit.
//
// The exhaustion predicate is strict: outerAccrued + proposedWallClockCost > outerLimit.
// A dispatch that exactly reaches the outer limit is admitted; the next dispatch
// that would exceed it is denied.
func CheckWallClockOuterBound(
	runID RunID,
	outerBudgetRef BudgetRef,
	outerLimit int64,
	outerAccrued int64,
	proposedWallClockCost float64,
) (BudgetExhaustedEventPayload, bool) {
	remaining := float64(outerLimit) - float64(outerAccrued)
	if proposedWallClockCost <= remaining {
		return BudgetExhaustedEventPayload{}, false
	}
	return BudgetExhaustedEventPayload{
		RunID:                 runID,
		BudgetRef:             outerBudgetRef,
		AttemptedDispatchCost: proposedWallClockCost,
	}, true
}
