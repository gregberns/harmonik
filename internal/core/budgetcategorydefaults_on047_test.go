package core

import (
	"testing"
)

// ---------------------------------------------------------------------------
// ON-047: Category defaults for resource budgets — 5-row table.
// specs/operator-nfr.md §4.11.ON-047
// ---------------------------------------------------------------------------

// TestDefaultCategoryBudgets_ReturnsFour verifies that DefaultCategoryBudgets
// returns exactly four entries — one per budget category in the ON-047 table
// (the fifth row, warning-threshold, is the DefaultBudgetWarningThreshold
// constant applied to all four budgets, not a separate PolicyBudget).
func TestDefaultCategoryBudgets_ReturnsFour(t *testing.T) {
	t.Parallel()

	budgets := DefaultCategoryBudgets()
	if len(budgets) != 4 {
		t.Fatalf("DefaultCategoryBudgets() len = %d, want 4", len(budgets))
	}
}

// TestDefaultCategoryBudgets_Names verifies the four returned budgets have
// the expected canonical names in declaration order.
func TestDefaultCategoryBudgets_Names(t *testing.T) {
	t.Parallel()

	budgets := DefaultCategoryBudgets()
	want := []string{
		"default-token-per-run",
		"default-wall-clock-per-run",
		"default-iterations-per-run",
		"default-wall-clock-per-reconciliation",
	}
	for i, b := range budgets {
		if b.Name != want[i] {
			t.Errorf("DefaultCategoryBudgets()[%d].Name = %q, want %q", i, b.Name, want[i])
		}
	}
}

// TestDefaultCategoryBudgets_TokenPerRun_ON047Row1 verifies the token per-run
// budget matches ON-047 row 1: 200,000 tokens.
func TestDefaultCategoryBudgets_TokenPerRun_ON047Row1(t *testing.T) {
	t.Parallel()

	budgets := DefaultCategoryBudgets()
	b := budgets[0]

	if b.Resource != string(BudgetResourceTokens) {
		t.Errorf("token budget Resource = %q, want %q", b.Resource, BudgetResourceTokens)
	}
	if b.Scope != string(BudgetScopePerRun) {
		t.Errorf("token budget Scope = %q, want %q", b.Scope, BudgetScopePerRun)
	}
	if b.Limit != DefaultTokenBudgetPerRunTokens {
		t.Errorf("token budget Limit = %d, want %d (ON-047 row 1)", b.Limit, DefaultTokenBudgetPerRunTokens)
	}
	if DefaultTokenBudgetPerRunTokens != 200_000 {
		t.Errorf("DefaultTokenBudgetPerRunTokens = %d, want 200000 (ON-047 row 1)", DefaultTokenBudgetPerRunTokens)
	}
}

// TestDefaultCategoryBudgets_WallClockPerRun_ON047Row2 verifies the wall-clock
// per-run budget matches ON-047 row 2: 30 minutes (1800 seconds).
func TestDefaultCategoryBudgets_WallClockPerRun_ON047Row2(t *testing.T) {
	t.Parallel()

	budgets := DefaultCategoryBudgets()
	b := budgets[1]

	if b.Resource != string(BudgetResourceWallClockSeconds) {
		t.Errorf("wall-clock per-run Resource = %q, want %q", b.Resource, BudgetResourceWallClockSeconds)
	}
	if b.Scope != string(BudgetScopePerRun) {
		t.Errorf("wall-clock per-run Scope = %q, want %q", b.Scope, BudgetScopePerRun)
	}
	if b.Limit != DefaultWallClockBudgetPerRunSeconds {
		t.Errorf("wall-clock per-run Limit = %d, want %d (ON-047 row 2)", b.Limit, DefaultWallClockBudgetPerRunSeconds)
	}
	const wantSeconds int64 = 1800
	if DefaultWallClockBudgetPerRunSeconds != wantSeconds {
		t.Errorf("DefaultWallClockBudgetPerRunSeconds = %d, want %d (30 min × 60 s/min — ON-047 row 2)", DefaultWallClockBudgetPerRunSeconds, wantSeconds)
	}
}

// TestDefaultCategoryBudgets_IterationsPerRun_ON047Row3 verifies the iterations
// per-run budget matches ON-047 row 3: 50 iterations.
func TestDefaultCategoryBudgets_IterationsPerRun_ON047Row3(t *testing.T) {
	t.Parallel()

	budgets := DefaultCategoryBudgets()
	b := budgets[2]

	if b.Resource != string(BudgetResourceIterations) {
		t.Errorf("iterations per-run Resource = %q, want %q", b.Resource, BudgetResourceIterations)
	}
	if b.Scope != string(BudgetScopePerRun) {
		t.Errorf("iterations per-run Scope = %q, want %q", b.Scope, BudgetScopePerRun)
	}
	if b.Limit != DefaultIterationsBudgetPerRunIterations {
		t.Errorf("iterations per-run Limit = %d, want %d (ON-047 row 3)", b.Limit, DefaultIterationsBudgetPerRunIterations)
	}
	if DefaultIterationsBudgetPerRunIterations != 50 {
		t.Errorf("DefaultIterationsBudgetPerRunIterations = %d, want 50 (ON-047 row 3)", DefaultIterationsBudgetPerRunIterations)
	}
}

// TestDefaultCategoryBudgets_WallClockPerReconciliation_ON047Row4 verifies the
// wall-clock per-reconciliation budget matches ON-047 row 4: 10 minutes (600 seconds).
func TestDefaultCategoryBudgets_WallClockPerReconciliation_ON047Row4(t *testing.T) {
	t.Parallel()

	budgets := DefaultCategoryBudgets()
	b := budgets[3]

	if b.Resource != string(BudgetResourceWallClockSeconds) {
		t.Errorf("wall-clock per-reconciliation Resource = %q, want %q", b.Resource, BudgetResourceWallClockSeconds)
	}
	if b.Scope != string(BudgetScopePerRun) {
		t.Errorf("wall-clock per-reconciliation Scope = %q, want %q", b.Scope, BudgetScopePerRun)
	}
	if b.Limit != DefaultWallClockBudgetPerReconciliationSeconds {
		t.Errorf("wall-clock per-reconciliation Limit = %d, want %d (ON-047 row 4)", b.Limit, DefaultWallClockBudgetPerReconciliationSeconds)
	}
	const wantSeconds int64 = 600
	if DefaultWallClockBudgetPerReconciliationSeconds != wantSeconds {
		t.Errorf("DefaultWallClockBudgetPerReconciliationSeconds = %d, want %d (10 min × 60 s/min — ON-047 row 4)", DefaultWallClockBudgetPerReconciliationSeconds, wantSeconds)
	}
}

// TestDefaultCategoryBudgets_WarningThreshold_ON047Row5 verifies that all four
// default budgets carry the 80% warning threshold (ON-047 row 5 / CP-025).
func TestDefaultCategoryBudgets_WarningThreshold_ON047Row5(t *testing.T) {
	t.Parallel()

	const wantThreshold = 0.8
	if DefaultBudgetWarningThreshold != wantThreshold {
		t.Errorf("DefaultBudgetWarningThreshold = %g, want %g (80%% — ON-047 row 5 / CP-025)", DefaultBudgetWarningThreshold, wantThreshold)
	}

	for i, b := range DefaultCategoryBudgets() {
		if b.WarningThreshold != DefaultBudgetWarningThreshold {
			t.Errorf("DefaultCategoryBudgets()[%d] (%q) WarningThreshold = %g, want %g", i, b.Name, b.WarningThreshold, DefaultBudgetWarningThreshold)
		}
	}
}

// TestDefaultCategoryBudgets_AllLimitsPositive verifies all default budget
// limits are positive — zero-limits would defeat the "safe state" guarantee.
func TestDefaultCategoryBudgets_AllLimitsPositive(t *testing.T) {
	t.Parallel()

	for i, b := range DefaultCategoryBudgets() {
		if b.Limit <= 0 {
			t.Errorf("DefaultCategoryBudgets()[%d] (%q) Limit = %d, want > 0", i, b.Name, b.Limit)
		}
	}
}

// TestDefaultCategoryBudgets_ResourcesAreValid verifies that all default budget
// resource strings are valid BudgetResource values per CP-022.
func TestDefaultCategoryBudgets_ResourcesAreValid(t *testing.T) {
	t.Parallel()

	for i, b := range DefaultCategoryBudgets() {
		r := BudgetResource(b.Resource)
		if !r.Valid() {
			t.Errorf("DefaultCategoryBudgets()[%d] (%q) Resource = %q is not a valid BudgetResource", i, b.Name, b.Resource)
		}
	}
}

// TestDefaultCategoryBudgets_ScopesAreValid verifies that all default budget
// scope strings are valid BudgetScope values per CP-022.
func TestDefaultCategoryBudgets_ScopesAreValid(t *testing.T) {
	t.Parallel()

	for i, b := range DefaultCategoryBudgets() {
		s := BudgetScope(b.Scope)
		if !s.Valid() {
			t.Errorf("DefaultCategoryBudgets()[%d] (%q) Scope = %q is not a valid BudgetScope", i, b.Name, b.Scope)
		}
	}
}

// TestDefaultCategoryBudgets_NamesAreNonEmpty verifies all default budgets have
// non-empty Name fields for debugging and observability.
func TestDefaultCategoryBudgets_NamesAreNonEmpty(t *testing.T) {
	t.Parallel()

	for i, b := range DefaultCategoryBudgets() {
		if b.Name == "" {
			t.Errorf("DefaultCategoryBudgets()[%d] has empty Name", i)
		}
	}
}

// TestDefaultCategoryBudgets_ImmutableSlice verifies that two calls to
// DefaultCategoryBudgets return independent slices — mutating one does not
// affect the other.
func TestDefaultCategoryBudgets_ImmutableSlice(t *testing.T) {
	t.Parallel()

	a := DefaultCategoryBudgets()
	b := DefaultCategoryBudgets()

	// Mutate the first slice.
	a[0].Name = "mutated"

	if b[0].Name == "mutated" {
		t.Error("DefaultCategoryBudgets: mutating the returned slice affected a subsequent call; slices must be independent")
	}
}
