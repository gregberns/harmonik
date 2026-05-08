package core

import "fmt"

// BudgetRef is the typed reference for the Node.budget_ref field
// (execution-model.md §6.1 RECORD Node; control-points.md §4.5).
//
// The value names a Budget registered in the policy-layer registry per
// [control-points.md §4.5.CP-022]. Policy YAML documents reference Budgets
// by name; the DOT node attribute budget_ref resolves to a registered name
// per [control-points.md §4.9].
//
// The spec declares budget_ref as String | None at §6.1 of
// execution-model.md. None is represented in Go as *BudgetRef at the call
// site; BudgetRef itself must always be non-empty.
//
// control-points.md §4.5 does not declare a structured record shape for the
// reference value at MVH; MVH realises BudgetRef as a typed non-empty string
// alias following the same pattern as PolicyVersion. A future
// control-points.md revision may promote this to a structured record via the
// amendment protocol per [architecture.md §4.6].
type BudgetRef string

// Valid reports whether r is a non-empty BudgetRef string.
// Empty values are rejected; all non-empty strings are accepted.
func (r BudgetRef) Valid() bool {
	return r != ""
}

// MarshalText implements encoding.TextMarshaler so BudgetRef serialises
// correctly in JSON and YAML.
// It rejects empty values.
func (r BudgetRef) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("budgetref: value must not be empty")
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (r *BudgetRef) UnmarshalText(text []byte) error {
	v := BudgetRef(text)
	if !v.Valid() {
		return fmt.Errorf("budgetref: value must not be empty")
	}
	*r = v
	return nil
}
