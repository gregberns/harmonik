package core

// BudgetPayload is the typed payload for a Budget control-point
// (specs/control-points.md §6.1.4 RECORD BudgetPayload).
//
// A BudgetPayload declares a single typed, scoped allowance. The evaluator
// enforces the allowance at runtime by tracking accrual against Limit; when
// cumulative accrual crosses WarningThreshold × Limit, it emits a
// budget_warning event per event-model.md §8.4 and continues. The tightest
// applicable Budget (smallest Limit for the same Resource) wins per CP-022
// §4.5 when multiple Budgets apply to a single agent run.
//
// # Default: WarningThreshold
//
// WarningThreshold defaults to 0.8 per CP-022. Use [NewBudgetPayload] to
// obtain a zero-value BudgetPayload with this default applied. Callers
// constructing a BudgetPayload via struct literal MUST set WarningThreshold
// explicitly; the zero value (0.0) is not equivalent to 0.8.
//
// # Validity
//
// Call [BudgetPayload.Valid] before persisting or evaluating a BudgetPayload.
// Valid requires:
//   - Resource must be a recognised BudgetResource constant.
//   - Scope must be a recognised BudgetScope constant.
//   - Limit must be positive (≥ 1).
//   - WarningThreshold must be in [0, 1].
//   - ScopeTarget must be structurally valid per [ScopeTarget.Valid].
type BudgetPayload struct {
	// Resource is the consumable resource tracked by this budget.
	// Must be one of BudgetResourceTokens, BudgetResourceWallClockSeconds,
	// or BudgetResourceIterations.
	Resource BudgetResource `json:"resource"`

	// Scope is the scoping axis that determines how ScopeTarget is interpreted.
	// Must be one of BudgetScopePerRole, BudgetScopePerRun, or BudgetScopePerState.
	Scope BudgetScope `json:"scope"`

	// Limit is the allowance ceiling (positive integer).
	// The evaluator denies or gates the enclosing action when accrual reaches Limit.
	Limit int64 `json:"limit"`

	// WarningThreshold is the ratio in [0, 1] at which a budget_warning event
	// is emitted (event-model.md §8.4). Defaults to 0.8 per CP-022; use
	// [NewBudgetPayload] to get the correct default. The zero value (0.0) is
	// valid but causes a warning to be emitted immediately upon any accrual.
	WarningThreshold float64 `json:"warning_threshold"`

	// ScopeTarget narrows the scope to a specific wildcard, predicate, list,
	// or singleton per specs/control-points.md §6.1.4 RECORD ScopeTarget.
	ScopeTarget ScopeTarget `json:"scope_target"`
}

// defaultWarningThreshold is the spec-mandated default for WarningThreshold
// per specs/control-points.md §4.5.CP-022.
const defaultWarningThreshold = 0.8

// NewBudgetPayload returns a BudgetPayload with the spec-mandated default
// applied: WarningThreshold is initialised to 0.8 per CP-022.
//
// All other fields are left at their zero values; callers MUST set Resource,
// Scope, Limit, and ScopeTarget before the payload can be considered valid.
func NewBudgetPayload() BudgetPayload {
	return BudgetPayload{
		WarningThreshold: defaultWarningThreshold,
	}
}

// Valid reports whether the BudgetPayload is structurally well-formed per
// specs/control-points.md §6.1.4:
//   - Resource must be a recognised BudgetResource constant.
//   - Scope must be a recognised BudgetScope constant.
//   - Limit must be positive (≥ 1).
//   - WarningThreshold must be in [0, 1].
//   - ScopeTarget must be structurally valid per [ScopeTarget.Valid].
func (b BudgetPayload) Valid() bool {
	if !b.Resource.Valid() {
		return false
	}
	if !b.Scope.Valid() {
		return false
	}
	if b.Limit < 1 {
		return false
	}
	if b.WarningThreshold < 0 || b.WarningThreshold > 1 {
		return false
	}
	if !b.ScopeTarget.Valid() {
		return false
	}
	return true
}
