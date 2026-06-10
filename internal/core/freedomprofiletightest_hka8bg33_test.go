package core

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-033: Freedom-profile tightest-wins semantics
// specs/control-points.md §4.6.CP-033
// Refs: hk-a8bg.33
// ---------------------------------------------------------------------------

// helper to make a *string from a literal
func strPtr(s string) *string { return &s }

// helper to make a *BudgetRef from a literal
func budgetRefPtr(s string) *BudgetRef {
	r := BudgetRef(s)
	return &r
}

// TestIntersectFreedomProfiles_EmptySliceErrors verifies that an empty slice
// returns an error (degenerate input).
func TestIntersectFreedomProfiles_EmptySliceErrors(t *testing.T) {
	t.Parallel()

	_, err := IntersectFreedomProfiles([]FreedomProfile{})
	if err == nil {
		t.Error("IntersectFreedomProfiles([]): expected error, got nil")
	}
}

// TestIntersectFreedomProfiles_SinglePassthrough verifies that a single-element
// slice returns the profile unchanged.
func TestIntersectFreedomProfiles_SinglePassthrough(t *testing.T) {
	t.Parallel()

	fp := FreedomProfile{
		Name:          "only",
		ToolWhitelist: []string{"bash", "read"},
		WritablePaths: []string{"output/**"},
		MaxIterations: 10,
	}
	got, err := IntersectFreedomProfiles([]FreedomProfile{fp})
	if err != nil {
		t.Fatalf("IntersectFreedomProfiles single: unexpected error: %v", err)
	}
	if got.Name != "only" {
		t.Errorf("Name: got %q, want %q", got.Name, "only")
	}
	if got.MaxIterations != 10 {
		t.Errorf("MaxIterations: got %d, want 10", got.MaxIterations)
	}
}

// TestIntersectFreedomProfiles_ToolWhitelistIntersection verifies list-valued
// field ToolWhitelist uses set intersection (CP-033).
func TestIntersectFreedomProfiles_ToolWhitelistIntersection(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{"bash", "read", "write"}, WritablePaths: []string{}, MaxIterations: 10}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{"read", "write", "glob"}, WritablePaths: []string{}, MaxIterations: 10}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{"read": true, "write": true}
	if len(got.ToolWhitelist) != len(want) {
		t.Errorf("ToolWhitelist length: got %d, want %d", len(got.ToolWhitelist), len(want))
	}
	for _, tool := range got.ToolWhitelist {
		if !want[tool] {
			t.Errorf("ToolWhitelist contains unexpected tool %q", tool)
		}
	}
}

// TestIntersectFreedomProfiles_ToolWhitelistDisjointIsEmpty verifies that
// disjoint tool whitelists produce an empty intersection.
func TestIntersectFreedomProfiles_ToolWhitelistDisjointIsEmpty(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{"bash"}, WritablePaths: []string{}, MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{"read"}, WritablePaths: []string{}, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.ToolWhitelist) != 0 {
		t.Errorf("ToolWhitelist: got %v, want []", got.ToolWhitelist)
	}
}

// TestIntersectFreedomProfiles_WritablePathsIntersection verifies that
// WritablePaths uses set intersection (CP-033).
func TestIntersectFreedomProfiles_WritablePathsIntersection(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{"output/**", "tmp/**"}, MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{"output/**", "logs/**"}, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.WritablePaths) != 1 || got.WritablePaths[0] != "output/**" {
		t.Errorf("WritablePaths: got %v, want [output/**]", got.WritablePaths)
	}
}

// TestIntersectFreedomProfiles_MaxIterationsTakesSmaller verifies that the
// smaller MaxIterations wins (CP-033: integer-valued fields → smaller value).
func TestIntersectFreedomProfiles_MaxIterationsTakesSmaller(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 50}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 10}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxIterations != 10 {
		t.Errorf("MaxIterations: got %d, want 10 (smaller)", got.MaxIterations)
	}
}

// TestIntersectFreedomProfiles_MaxIterationsBothSame verifies that equal
// MaxIterations values are preserved.
func TestIntersectFreedomProfiles_MaxIterationsBothSame(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 20}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 20}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxIterations != 20 {
		t.Errorf("MaxIterations: got %d, want 20", got.MaxIterations)
	}
}

// TestIntersectFreedomProfiles_ModelTierLessCapableWins verifies that when
// both profiles declare a model_tier, the less-capable tier wins (CP-033).
func TestIntersectFreedomProfiles_ModelTierLessCapableWins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tierA    string
		tierB    string
		wantTier string
	}{
		{"haiku vs sonnet → haiku", "haiku", "sonnet", "haiku"},
		{"sonnet vs opus → sonnet", "sonnet", "opus", "sonnet"},
		{"haiku vs opus → haiku", "haiku", "opus", "haiku"},
		{"opus vs sonnet → sonnet", "opus", "sonnet", "sonnet"},
		{"sonnet vs haiku → haiku", "sonnet", "haiku", "haiku"},
		{"same tier → same", "sonnet", "sonnet", "sonnet"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, ModelTier: strPtr(tt.tierA), MaxIterations: 5}
			b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, ModelTier: strPtr(tt.tierB), MaxIterations: 5}

			got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ModelTier == nil {
				t.Fatalf("ModelTier is nil, want %q", tt.wantTier)
			}
			if *got.ModelTier != tt.wantTier {
				t.Errorf("ModelTier: got %q, want %q", *got.ModelTier, tt.wantTier)
			}
		})
	}
}

// TestIntersectFreedomProfiles_ModelTierNilBeatsAbsence verifies that a
// non-nil model_tier beats nil (nil = no constraint; constraint applies when set).
func TestIntersectFreedomProfiles_ModelTierNilBeatsAbsence(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, ModelTier: strPtr("haiku"), MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, ModelTier: nil, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ModelTier == nil || *got.ModelTier != "haiku" {
		t.Errorf("ModelTier: got %v, want \"haiku\" (non-nil beats nil)", got.ModelTier)
	}
}

// TestIntersectFreedomProfiles_ModelTierBothNilResultsNil verifies that when
// both profiles have no model_tier, the result also has none.
func TestIntersectFreedomProfiles_ModelTierBothNilResultsNil(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ModelTier != nil {
		t.Errorf("ModelTier: got %v, want nil (both nil)", got.ModelTier)
	}
}

// TestIntersectFreedomProfiles_UnknownModelTierErrors verifies that an unknown
// model_tier name (not in the tier table) returns ErrUnknownModelTier.
func TestIntersectFreedomProfiles_UnknownModelTierErrors(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, ModelTier: strPtr("gpt-4"), MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, ModelTier: strPtr("haiku"), MaxIterations: 5}

	_, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err == nil {
		t.Fatal("expected ErrUnknownModelTier, got nil")
	}
	if !errors.Is(err, ErrUnknownModelTier) {
		t.Errorf("error = %v, want errors.Is(ErrUnknownModelTier)", err)
	}
}

// TestIntersectFreedomProfiles_TokenBudgetRefSameIsKept verifies that equal
// TokenBudgetRef values are preserved after intersection.
func TestIntersectFreedomProfiles_TokenBudgetRefSameIsKept(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, TokenBudgetRef: budgetRefPtr("budget-a"), MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, TokenBudgetRef: budgetRefPtr("budget-a"), MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TokenBudgetRef == nil || *got.TokenBudgetRef != "budget-a" {
		t.Errorf("TokenBudgetRef: got %v, want \"budget-a\"", got.TokenBudgetRef)
	}
}

// TestIntersectFreedomProfiles_TokenBudgetRefNilTakesNonNil verifies that a
// non-nil TokenBudgetRef beats nil (constraint applies when set).
func TestIntersectFreedomProfiles_TokenBudgetRefNilTakesNonNil(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, TokenBudgetRef: budgetRefPtr("budget-x"), MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, TokenBudgetRef: nil, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TokenBudgetRef == nil || *got.TokenBudgetRef != "budget-x" {
		t.Errorf("TokenBudgetRef: got %v, want \"budget-x\"", got.TokenBudgetRef)
	}
}

// TestIntersectFreedomProfiles_TokenBudgetRefBothNilResultsNil verifies that
// when both profiles have no token budget ref, the result has none.
func TestIntersectFreedomProfiles_TokenBudgetRefBothNilResultsNil(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TokenBudgetRef != nil {
		t.Errorf("TokenBudgetRef: got %v, want nil", got.TokenBudgetRef)
	}
}

// TestIntersectFreedomProfiles_TokenBudgetRefDifferentErrors verifies that
// two different non-nil TokenBudgetRef values return ErrIncompatibleFreedomProfiles
// (non-ordered field; both layers must declare compatible values per CP-033).
func TestIntersectFreedomProfiles_TokenBudgetRefDifferentErrors(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, TokenBudgetRef: budgetRefPtr("budget-a"), MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, TokenBudgetRef: budgetRefPtr("budget-b"), MaxIterations: 5}

	_, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err == nil {
		t.Fatal("expected ErrIncompatibleFreedomProfiles, got nil")
	}
	if !errors.Is(err, ErrIncompatibleFreedomProfiles) {
		t.Errorf("error = %v, want errors.Is(ErrIncompatibleFreedomProfiles)", err)
	}
}

// TestIntersectFreedomProfiles_WallClockBudgetRefDifferentErrors verifies that
// two different non-nil WallClockBudgetRef values return ErrIncompatibleFreedomProfiles.
func TestIntersectFreedomProfiles_WallClockBudgetRefDifferentErrors(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{}, WritablePaths: []string{}, WallClockBudgetRef: budgetRefPtr("wall-a"), MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{}, WritablePaths: []string{}, WallClockBudgetRef: budgetRefPtr("wall-b"), MaxIterations: 5}

	_, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err == nil {
		t.Fatal("expected ErrIncompatibleFreedomProfiles, got nil")
	}
	if !errors.Is(err, ErrIncompatibleFreedomProfiles) {
		t.Errorf("error = %v, want errors.Is(ErrIncompatibleFreedomProfiles)", err)
	}
}

// TestIntersectFreedomProfiles_ThreeProfilesChained verifies that three profiles
// are composed correctly (associative reduction).
func TestIntersectFreedomProfiles_ThreeProfilesChained(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{"bash", "read", "write"}, WritablePaths: []string{}, ModelTier: strPtr("opus"), MaxIterations: 100}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{"read", "write", "glob"}, WritablePaths: []string{}, ModelTier: strPtr("sonnet"), MaxIterations: 50}
	c := FreedomProfile{Name: "c", ToolWhitelist: []string{"read", "ls"}, WritablePaths: []string{}, ModelTier: strPtr("haiku"), MaxIterations: 20}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b, c})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ToolWhitelist: {"bash","read","write"} ∩ {"read","write","glob"} ∩ {"read","ls"} = {"read"}
	if len(got.ToolWhitelist) != 1 || got.ToolWhitelist[0] != "read" {
		t.Errorf("ToolWhitelist: got %v, want [read]", got.ToolWhitelist)
	}
	// MaxIterations: min(100, 50, 20) = 20
	if got.MaxIterations != 20 {
		t.Errorf("MaxIterations: got %d, want 20", got.MaxIterations)
	}
	// ModelTier: min(opus, sonnet, haiku) = haiku
	if got.ModelTier == nil || *got.ModelTier != "haiku" {
		t.Errorf("ModelTier: got %v, want \"haiku\"", got.ModelTier)
	}
}

// TestIntersectFreedomProfiles_NameIsComposedDeterministically verifies that
// the result name encodes the input names deterministically for auditability.
func TestIntersectFreedomProfiles_NameIsComposedDeterministically(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "role-default", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 10}
	b := FreedomProfile{Name: "node-override", ToolWhitelist: []string{}, WritablePaths: []string{}, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantName := "intersect:role-default+node-override"
	if got.Name != wantName {
		t.Errorf("Name: got %q, want %q", got.Name, wantName)
	}
}

// TestIntersectFreedomProfiles_ResultToolWhitelistIsNonNilWhenEmpty verifies
// that an empty intersection ToolWhitelist is non-nil (appendable).
func TestIntersectFreedomProfiles_ResultToolWhitelistIsNonNilWhenEmpty(t *testing.T) {
	t.Parallel()

	a := FreedomProfile{Name: "a", ToolWhitelist: []string{"bash"}, WritablePaths: []string{}, MaxIterations: 5}
	b := FreedomProfile{Name: "b", ToolWhitelist: []string{"read"}, WritablePaths: []string{}, MaxIterations: 5}

	got, err := IntersectFreedomProfiles([]FreedomProfile{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ToolWhitelist == nil {
		t.Error("ToolWhitelist is nil after intersection, want non-nil empty slice")
	}
}
