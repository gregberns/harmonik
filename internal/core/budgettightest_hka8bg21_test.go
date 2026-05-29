package core

import "testing"

// Tests for TightestBudget per specs/control-points.md §4.5.CP-022:
// "Multiple Budgets MAY apply to a single agent run; the tightest applicable
// Budget wins on any accrual check (tightest = smaller integer `limit` for the
// same `resource`)."
//
// Refs: hk-a8bg.21

// tightestBudgetFixture returns a BudgetPayload with the given resource and
// limit; WarningThreshold and Scope are set to valid values.
func tightestBudgetFixture(resource BudgetResource, limit int64) BudgetPayload {
	bp := NewBudgetPayload()
	bp.Resource = resource
	bp.Scope = BudgetScopePerRun
	bp.Limit = limit
	bp.ScopeTarget = ScopeTargetWildcard()
	return bp
}

// TestTightestBudget_SingleEntry returns the single matching entry.
func TestTightestBudget_SingleEntry(t *testing.T) {
	t.Parallel()

	budgets := []BudgetPayload{
		tightestBudgetFixture(BudgetResourceTokens, 5000),
	}
	got, ok := TightestBudget(budgets, BudgetResourceTokens)
	if !ok {
		t.Fatal("TightestBudget: expected ok=true, got false")
	}
	if got.Limit != 5000 {
		t.Errorf("TightestBudget: Limit = %d, want 5000", got.Limit)
	}
}

// TestTightestBudget_TwoEntriesReturnsSmaller verifies the smaller limit wins.
func TestTightestBudget_TwoEntriesReturnsSmaller(t *testing.T) {
	t.Parallel()

	budgets := []BudgetPayload{
		tightestBudgetFixture(BudgetResourceTokens, 10000),
		tightestBudgetFixture(BudgetResourceTokens, 3000),
	}
	got, ok := TightestBudget(budgets, BudgetResourceTokens)
	if !ok {
		t.Fatal("TightestBudget: expected ok=true, got false")
	}
	if got.Limit != 3000 {
		t.Errorf("TightestBudget: Limit = %d, want 3000 (tightest)", got.Limit)
	}
}

// TestTightestBudget_ThreeEntriesReturnsSmallest verifies across three entries.
func TestTightestBudget_ThreeEntriesReturnsSmallest(t *testing.T) {
	t.Parallel()

	budgets := []BudgetPayload{
		tightestBudgetFixture(BudgetResourceTokens, 8000),
		tightestBudgetFixture(BudgetResourceTokens, 2000),
		tightestBudgetFixture(BudgetResourceTokens, 5000),
	}
	got, ok := TightestBudget(budgets, BudgetResourceTokens)
	if !ok {
		t.Fatal("TightestBudget: expected ok=true, got false")
	}
	if got.Limit != 2000 {
		t.Errorf("TightestBudget: Limit = %d, want 2000 (tightest)", got.Limit)
	}
}

// TestTightestBudget_NoMatchingResource returns false when no entry matches.
func TestTightestBudget_NoMatchingResource(t *testing.T) {
	t.Parallel()

	budgets := []BudgetPayload{
		tightestBudgetFixture(BudgetResourceWallClockSeconds, 1800),
	}
	_, ok := TightestBudget(budgets, BudgetResourceTokens)
	if ok {
		t.Error("TightestBudget: expected ok=false for no matching resource, got true")
	}
}

// TestTightestBudget_EmptySlice returns false.
func TestTightestBudget_EmptySlice(t *testing.T) {
	t.Parallel()

	_, ok := TightestBudget(nil, BudgetResourceTokens)
	if ok {
		t.Error("TightestBudget: expected ok=false for empty slice, got true")
	}
}

// TestTightestBudget_SkipsOtherResources verifies that entries for other
// resources are ignored when selecting the tightest for a given resource.
func TestTightestBudget_SkipsOtherResources(t *testing.T) {
	t.Parallel()

	// wall_clock_seconds has a very small limit (1) but should not affect the
	// tokens selection.
	budgets := []BudgetPayload{
		tightestBudgetFixture(BudgetResourceWallClockSeconds, 1),
		tightestBudgetFixture(BudgetResourceTokens, 7000),
		tightestBudgetFixture(BudgetResourceIterations, 10),
	}
	got, ok := TightestBudget(budgets, BudgetResourceTokens)
	if !ok {
		t.Fatal("TightestBudget: expected ok=true, got false")
	}
	if got.Limit != 7000 {
		t.Errorf("TightestBudget: Limit = %d, want 7000 (only matching entry)", got.Limit)
	}
	if got.Resource != BudgetResourceTokens {
		t.Errorf("TightestBudget: Resource = %q, want %q", got.Resource, BudgetResourceTokens)
	}
}

// TestTightestBudget_EqualLimitsReturnsFirst verifies that when two entries
// share the minimum limit, the first in slice order is returned (stable).
func TestTightestBudget_EqualLimitsReturnsFirst(t *testing.T) {
	t.Parallel()

	first := NewBudgetPayload()
	first.Resource = BudgetResourceTokens
	first.Scope = BudgetScopePerRole
	first.Limit = 5000
	first.ScopeTarget = ScopeTargetWildcard()

	second := NewBudgetPayload()
	second.Resource = BudgetResourceTokens
	second.Scope = BudgetScopePerRun
	second.Limit = 5000
	second.ScopeTarget = ScopeTargetWildcard()

	got, ok := TightestBudget([]BudgetPayload{first, second}, BudgetResourceTokens)
	if !ok {
		t.Fatal("TightestBudget: expected ok=true, got false")
	}
	if got.Limit != 5000 {
		t.Errorf("TightestBudget: Limit = %d, want 5000", got.Limit)
	}
	// First entry wins on tie.
	if got.Scope != BudgetScopePerRole {
		t.Errorf("TightestBudget: Scope = %q, want %q (first entry on tie)", got.Scope, BudgetScopePerRole)
	}
}

// TestTightestBudget_AllThreeResources verifies each resource can be selected
// independently from a mixed slice.
func TestTightestBudget_AllThreeResources(t *testing.T) {
	t.Parallel()

	budgets := []BudgetPayload{
		tightestBudgetFixture(BudgetResourceTokens, 200_000),
		tightestBudgetFixture(BudgetResourceTokens, 50_000),
		tightestBudgetFixture(BudgetResourceWallClockSeconds, 1800),
		tightestBudgetFixture(BudgetResourceWallClockSeconds, 600),
		tightestBudgetFixture(BudgetResourceIterations, 50),
		tightestBudgetFixture(BudgetResourceIterations, 25),
	}

	cases := []struct {
		resource  BudgetResource
		wantLimit int64
	}{
		{BudgetResourceTokens, 50_000},
		{BudgetResourceWallClockSeconds, 600},
		{BudgetResourceIterations, 25},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.resource), func(t *testing.T) {
			t.Parallel()

			got, ok := TightestBudget(budgets, tc.resource)
			if !ok {
				t.Fatalf("TightestBudget(%s): expected ok=true, got false", tc.resource)
			}
			if got.Limit != tc.wantLimit {
				t.Errorf("TightestBudget(%s): Limit = %d, want %d", tc.resource, got.Limit, tc.wantLimit)
			}
		})
	}
}
