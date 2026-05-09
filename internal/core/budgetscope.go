package core

import "fmt"

// BudgetScope is the scoping axis for a Budget; it drives how ScopeTarget is
// interpreted (specs/control-points.md §6.1.4 ENUM BudgetScope).
//
// The three values correspond to the three allocation windows the budget
// evaluator recognises:
//   - BudgetScopePerRole: allowance is shared across all runs for a given role;
//     ScopeTarget.singleton is a role name.
//   - BudgetScopePerRun: allowance is isolated to a single run;
//     ScopeTarget.singleton is a run_id.
//   - BudgetScopePerState: allowance is tied to a single state node;
//     ScopeTarget.singleton is a state_id.
//
// A reader observing an unknown BudgetScope MUST reject the enclosing
// BudgetPayload; no silent fallback is permitted.
type BudgetScope string

// BudgetScope values per specs/control-points.md §6.1.4 ENUM BudgetScope.
const (
	// BudgetScopePerRole scopes the budget to a role;
	// ScopeTarget.singleton is a role name.
	BudgetScopePerRole BudgetScope = "per_role"

	// BudgetScopePerRun scopes the budget to a single run;
	// ScopeTarget.singleton is a run_id.
	BudgetScopePerRun BudgetScope = "per_run"

	// BudgetScopePerState scopes the budget to a single state node;
	// ScopeTarget.singleton is a state_id.
	BudgetScopePerState BudgetScope = "per_state"
)

// Valid reports whether s is one of the three declared BudgetScope constants.
// Unknown values are NOT tolerated — a reader observing an unknown BudgetScope
// MUST reject the enclosing BudgetPayload per specs/control-points.md §6.1.4.
func (s BudgetScope) Valid() bool {
	switch s {
	case BudgetScopePerRole, BudgetScopePerRun, BudgetScopePerState:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so BudgetScope serialises
// correctly in JSON and YAML policy documents (specs/control-points.md §6.3).
// It rejects any value that is not one of the three declared constants.
func (s BudgetScope) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("budgetscope: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants.
// Per specs/control-points.md §6.1.4, unknown BudgetScope values must be
// rejected; callers MUST NOT silently degrade to a default scope.
func (s *BudgetScope) UnmarshalText(text []byte) error {
	v := BudgetScope(text)
	if !v.Valid() {
		return fmt.Errorf(
			"budgetscope: unknown value %q; must be one of per_role, per_run, per_state",
			string(text),
		)
	}
	*s = v
	return nil
}
