package core

import "fmt"

// BudgetResource identifies the consumable resource tracked by a Budget
// (specs/control-points.md §6.1.4 ENUM BudgetResource).
//
// The three values determine which counter the Budget accrual check
// increments and compares against Budget.limit:
//   - BudgetResourceTokens: LLM tokens consumed by the agent run
//   - BudgetResourceWallClockSeconds: elapsed wall-clock time in seconds
//   - BudgetResourceIterations: discrete agent iteration steps
//
// When multiple Budgets apply to a single agent run for the same resource,
// the tightest applicable Budget wins (tightest = smaller integer limit for
// the same resource) per specs/control-points.md §4.5.CP-022.
//
// A reader observing an unknown BudgetResource MUST reject the enclosing
// BudgetPayload; no silent fallback is permitted.
type BudgetResource string

// BudgetResource values per specs/control-points.md §6.1.4 ENUM BudgetResource.
const (
	// BudgetResourceTokens indicates the Budget tracks LLM token consumption.
	BudgetResourceTokens BudgetResource = "tokens"

	// BudgetResourceWallClockSeconds indicates the Budget tracks elapsed
	// wall-clock time in seconds. Reconciliation workflows carry a mandatory
	// wall-clock Budget per specs/control-points.md §4.5.
	BudgetResourceWallClockSeconds BudgetResource = "wall_clock_seconds"

	// BudgetResourceIterations indicates the Budget tracks discrete agent
	// iteration steps. A FreedomProfile's max_iterations field expresses the
	// same resource as an inline limit per specs/control-points.md §6.1.6.
	BudgetResourceIterations BudgetResource = "iterations"
)

// Valid reports whether r is one of the three declared BudgetResource constants.
// Unknown values are NOT tolerated — a reader observing an unknown BudgetResource
// MUST reject the enclosing record per specs/control-points.md §6.1.4.
func (r BudgetResource) Valid() bool {
	switch r {
	case BudgetResourceTokens, BudgetResourceWallClockSeconds, BudgetResourceIterations:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so BudgetResource serialises
// correctly in JSON and YAML policy documents (specs/control-points.md §6.3).
// It rejects any value that is not one of the three declared constants.
func (r BudgetResource) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("budgetresource: unknown value %q", string(r))
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
// Per specs/control-points.md §6.1.4, unknown BudgetResource values must be
// rejected; callers MUST NOT silently degrade to a default resource.
func (r *BudgetResource) UnmarshalText(text []byte) error {
	v := BudgetResource(text)
	if !v.Valid() {
		return fmt.Errorf(
			"budgetresource: unknown value %q; must be one of tokens, wall_clock_seconds, iterations",
			string(text),
		)
	}
	*r = v
	return nil
}
