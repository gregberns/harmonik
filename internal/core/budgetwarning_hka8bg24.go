package core

// budgetwarning_hka8bg24.go — CP-025: Budget warning threshold fires at 80% by default.
//
// Implements the warning-threshold check from specs/control-points.md §4.5.CP-025:
//
//	When cumulative accrual crosses the warning_threshold fraction of limit,
//	the runner MUST emit a budget_warning event per [event-model.md §8.4]
//	and continue. The threshold check uses live in-handler counters (the
//	handler tracks accrual against remaining budget in real time per its
//	own tick cadence). The threshold value is governed by §4.5.CP-022
//	(default 0.8, operator-overridable per §4.7).
//
// Refs: hk-a8bg.24

// CheckBudgetWarningThreshold evaluates whether cumulative accrual has crossed
// the warning_threshold fraction of the declared limit per control-points.md
// §4.5.CP-025.
//
// Parameters:
//   - runID:             the RunID of the run in whose context accrual is tracked
//   - budgetRef:         the name of the budget being checked
//   - limit:             the declared budget ceiling (from BudgetPayload.Limit)
//   - warningThreshold:  the fraction in [0, 1] at which the warning fires
//                        (BudgetPayload.WarningThreshold; default 0.8 per CP-022)
//   - accrued:           the total accrued units since run_started (sum of all
//                        budget_accrual CostUnits for this budget)
//
// Returns (BudgetWarningPayload, true) when the warning threshold has been
// crossed — the caller MUST emit the payload as a budget_warning event per
// event-model.md §8.4.1 and MUST continue (warning does not deny the dispatch).
//
// Returns (zero BudgetWarningPayload, false) when accrual has not yet crossed
// the threshold.
//
// The crossing condition is: accrued >= warningThreshold * float64(limit).
// Equality is included so the warning fires at the exact threshold boundary.
//
// The caller is responsible for warn-once semantics: this function returns true
// on every call while accrued >= threshold. The handler MUST suppress duplicate
// emissions by tracking whether the warning has already been emitted for this
// run and budget_ref.
func CheckBudgetWarningThreshold(
	runID RunID,
	budgetRef BudgetRef,
	limit int64,
	warningThreshold float64,
	accrued float64,
) (BudgetWarningPayload, bool) {
	threshold := warningThreshold * float64(limit)
	if accrued < threshold {
		return BudgetWarningPayload{}, false
	}
	remaining := float64(limit) - accrued
	if remaining < 0 {
		remaining = 0
	}
	return BudgetWarningPayload{
		RunID:             runID,
		BudgetRef:         budgetRef,
		ThresholdFraction: warningThreshold,
		Remaining:         remaining,
	}, true
}
