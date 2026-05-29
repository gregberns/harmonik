package core

// TightestBudget selects the applicable Budget with the smallest Limit for the
// given resource, implementing the "tightest applicable Budget wins on any
// accrual check" rule of specs/control-points.md §4.5.CP-022.
//
// When multiple Budgets in the slice declare the same Resource, the one with
// the smaller integer Limit is returned. When two entries share the same
// (minimum) Limit, the first such entry in the slice is returned — iteration
// order is stable.
//
// Returns the tightest BudgetPayload and true when at least one Budget for
// resource is present. Returns a zero BudgetPayload and false when no Budget
// in the slice matches the given resource.
//
// Refs: hk-a8bg.21
func TightestBudget(budgets []BudgetPayload, resource BudgetResource) (BudgetPayload, bool) {
	var tightest BudgetPayload
	found := false
	for _, b := range budgets {
		if b.Resource != resource {
			continue
		}
		if !found || b.Limit < tightest.Limit {
			tightest = b
			found = true
		}
	}
	return tightest, found
}
