package core

// budgetdispatchcheck_hka8bg22.go — CP-023: Budget is enforced at dispatch.
//
// Implements the pre-dispatch budget gate defined in
// specs/control-points.md §4.5.CP-023:
//
//   The agent runner MUST check the Budget's remaining allowance AT DISPATCH
//   (pre-exhaustion). If the pending dispatch would exceed the remaining limit,
//   the runner MUST emit a budget_exhausted event per [event-model.md §8.4]
//   and DENY the dispatch (the handler is NOT launched). The run's failure
//   class MUST be budget_exhausted per [execution-model.md §8.5].
//
// Refs: hk-a8bg.22

// CheckBudgetAtDispatch evaluates whether a pending dispatch is admissible
// under the declared budget ceiling per control-points.md §4.5.CP-023.
//
// Parameters:
//   - runID:          the RunID of the run requesting dispatch
//   - budgetRef:      the name of the budget being checked
//   - limit:          the declared budget ceiling (from BudgetPayload.Limit)
//   - accrued:        the total accrued units since run_started (reconstructed
//                     from budget_accrual replay per CP-026a)
//   - attemptedCost:  the estimated cost of the pending dispatch
//
// Returns (BudgetExhaustedEventPayload, true) when the dispatch is DENIED —
// the caller MUST emit the payload as a budget_exhausted event per
// event-model.md §8.4.3 and MUST NOT launch the handler.
//
// Returns (zero BudgetExhaustedEventPayload, false) when the dispatch is
// ADMITTED — accrued + attemptedCost does not exceed the limit.
//
// The exhaustion condition is strict: accrued + attemptedCost > limit.
// A dispatch that exactly reaches the limit (accrued + attemptedCost == limit)
// is admitted; the next dispatch that would exceed it is denied.
func CheckBudgetAtDispatch(
	runID RunID,
	budgetRef BudgetRef,
	limit int64,
	accrued int64,
	attemptedCost float64,
) (BudgetExhaustedEventPayload, bool) {
	remaining := float64(limit) - float64(accrued)
	if attemptedCost <= remaining {
		return BudgetExhaustedEventPayload{}, false
	}
	return BudgetExhaustedEventPayload{
		RunID:                 runID,
		BudgetRef:             budgetRef,
		AttemptedDispatchCost: attemptedCost,
	}, true
}
