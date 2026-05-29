package core

import (
	"errors"
	"fmt"
)

// mVHTierOrder maps model tier names to their capability rank.
// Lower rank = less capable. MVH ordering per specs/control-points.md §4.6.CP-033.
var mVHTierOrder = map[string]int{
	"haiku":  0,
	"sonnet": 1,
	"opus":   2,
}

// ErrIncompatibleFreedomProfiles is returned by IntersectFreedomProfiles when
// two profiles carry different non-nil values for a field without a declared
// ordering (per §4.6.CP-033: enums without declared ordering are not composable
// by tightest-wins; both layers MUST declare compatible values).
var ErrIncompatibleFreedomProfiles = errors.New("freedom profiles have incompatible values for a non-ordered field")

// ErrUnknownModelTier is returned by IntersectFreedomProfiles when a model_tier
// value is not present in the harmonik-level tier table declared in
// specs/_registry.yaml (MVH table: haiku, sonnet, opus).
var ErrUnknownModelTier = errors.New("freedom profile model_tier is not in the declared tier table")

// IntersectFreedomProfiles computes the effective FreedomProfile when multiple
// profiles apply to a single state, implementing the per-field tightest-wins
// semantics of specs/control-points.md §4.6.CP-033.
//
// Field semantics:
//   - ToolWhitelist, WritablePaths (list-valued): set intersection.
//   - MaxIterations (integer-valued): smaller value.
//   - ModelTier (ordered enum): less-capable tier per MVH ordering haiku < sonnet < opus;
//     a nil tier (no constraint) loses to a non-nil tier (the constraint applies).
//   - TokenBudgetRef, WallClockBudgetRef (non-ordered references): if one is nil,
//     the non-nil value is used; if both are non-nil and differ,
//     ErrIncompatibleFreedomProfiles is returned.
//
// An empty slice returns an error. A single-element slice returns that profile
// unchanged. The Name of the result is the deterministic composition of input
// names for auditability.
//
// "Tightest wins" is deterministic and mechanism-tagged per CP-033.
//
// Refs: hk-a8bg.33
func IntersectFreedomProfiles(profiles []FreedomProfile) (FreedomProfile, error) {
	if len(profiles) == 0 {
		return FreedomProfile{}, errors.New("IntersectFreedomProfiles: empty profile list")
	}
	result := profiles[0]
	for i := 1; i < len(profiles); i++ {
		var err error
		result, err = intersectTwoFreedomProfiles(result, profiles[i])
		if err != nil {
			return FreedomProfile{}, fmt.Errorf("IntersectFreedomProfiles: intersecting profiles[%d]: %w", i, err)
		}
	}
	return result, nil
}

// intersectTwoFreedomProfiles computes the per-field tightest-wins intersection
// of exactly two FreedomProfiles per CP-033.
func intersectTwoFreedomProfiles(a, b FreedomProfile) (FreedomProfile, error) {
	modelTier, err := tightestModelTier(a.ModelTier, b.ModelTier)
	if err != nil {
		return FreedomProfile{}, err
	}

	tokenRef, err := intersectBudgetRef(a.TokenBudgetRef, b.TokenBudgetRef)
	if err != nil {
		return FreedomProfile{}, fmt.Errorf("token_budget_ref: %w", err)
	}

	wallRef, err := intersectBudgetRef(a.WallClockBudgetRef, b.WallClockBudgetRef)
	if err != nil {
		return FreedomProfile{}, fmt.Errorf("wall_clock_budget_ref: %w", err)
	}

	maxIter := a.MaxIterations
	if b.MaxIterations < maxIter {
		maxIter = b.MaxIterations
	}

	return FreedomProfile{
		Name:               "intersect:" + a.Name + "+" + b.Name,
		ToolWhitelist:      intersectStringSet(a.ToolWhitelist, b.ToolWhitelist),
		WritablePaths:      intersectStringSet(a.WritablePaths, b.WritablePaths),
		ModelTier:          modelTier,
		TokenBudgetRef:     tokenRef,
		WallClockBudgetRef: wallRef,
		MaxIterations:      maxIter,
	}, nil
}

// intersectStringSet returns the set intersection of a and b, preserving
// the order of a. The result is always a non-nil slice.
func intersectStringSet(a, b []string) []string {
	bSet := make(map[string]struct{}, len(b))
	for _, s := range b {
		bSet[s] = struct{}{}
	}
	result := []string{}
	for _, s := range a {
		if _, ok := bSet[s]; ok {
			result = append(result, s)
		}
	}
	return result
}

// tightestModelTier returns the less-capable of the two model tier pointers
// per the MVH ordering: haiku < sonnet < opus.
//
// nil means "no tier constraint"; a non-nil value is more restrictive than nil.
// Returns ErrUnknownModelTier if a non-nil tier name is not in the tier table.
func tightestModelTier(a, b *string) (*string, error) {
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}
	ra, ok := mVHTierOrder[*a]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownModelTier, *a)
	}
	rb, ok := mVHTierOrder[*b]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownModelTier, *b)
	}
	if ra <= rb {
		return a, nil
	}
	return b, nil
}

// intersectBudgetRef returns the effective BudgetRef for a non-ordered reference
// field. nil beats non-nil in the direction of "constraint wins over absence".
// If both are non-nil and equal, the value is returned. If both are non-nil and
// differ, ErrIncompatibleFreedomProfiles is returned.
func intersectBudgetRef(a, b *BudgetRef) (*BudgetRef, error) {
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}
	if *a != *b {
		return nil, fmt.Errorf("%w: %q and %q", ErrIncompatibleFreedomProfiles, *a, *b)
	}
	return a, nil
}
