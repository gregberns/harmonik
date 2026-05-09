package core

// FreedomProfile is the per-state constraint bundle attached to a node or role
// in a policy document (specs/control-points.md §6.2 RECORD FreedomProfile).
//
// A FreedomProfile declares the maximum freedom an agent may exercise while
// executing within a state. When multiple profiles apply to a state (e.g., a
// role-default profile and a node-level freedom_profile_ref), the effective
// profile is the per-field intersection per §4.6.CP-033: for list-valued
// fields, set intersection; for integer-valued fields, the smaller value; for
// model_tier, the less-capable tier per the harmonik-level ordering declared
// in the _registry.yaml tier table.
//
// # Validity
//
// Call [FreedomProfile.Valid] before persisting or evaluating a FreedomProfile.
// Valid requires:
//   - Name must be non-empty.
//   - MaxIterations must be positive (≥ 1).
//   - TokenBudgetRef, if non-nil, must be a valid BudgetRef.
//   - WallClockBudgetRef, if non-nil, must be a valid BudgetRef.
//
// # Typed-alias deferral
//
// The following fields use string as a placeholder pending typed-alias
// implementation:
//   - ToolWhitelist []string  — TODO hk-a8bg.86 (ToolName)
//   - WritablePaths []string  — TODO hk-a8bg.87 (PathGlob)
type FreedomProfile struct {
	// Name is the unique identifier for this profile within a policy document.
	// Referenced by DOT node attribute freedom_profile_ref per §4.9.
	// Must be non-empty.
	Name string `json:"name"`

	// ToolWhitelist is the closed list of tool names the agent may invoke under
	// this profile. Intersects with the role's allowed_tools at composition time.
	// An empty slice means no tools are permitted.
	// TODO hk-a8bg.86: replace string with ToolName typed alias.
	ToolWhitelist []string `json:"tool_whitelist"`

	// WritablePaths is the list of workspace-relative globs the agent may write.
	// Intersects with the role's writable_paths at composition time.
	// An empty slice means no paths are writable.
	// TODO hk-a8bg.87: replace string with PathGlob typed alias.
	WritablePaths []string `json:"writable_paths"`

	// ModelTier is the optional model tier name allocated under this profile.
	// When multiple profiles compose, the less-capable tier wins per CP-033.
	// When nil, no tier constraint applies.
	// Spec: specs/control-points.md §6.2 (model_tier : String | None).
	ModelTier *string `json:"model_tier,omitempty"`

	// TokenBudgetRef names the Budget registered in the policy document that
	// caps token consumption per §4.5.CP-022. When nil, no token budget is
	// imposed by this profile.
	// Spec: specs/control-points.md §6.2 (token_budget_ref : String | None).
	TokenBudgetRef *BudgetRef `json:"token_budget_ref,omitempty"`

	// WallClockBudgetRef names the Budget registered in the policy document that
	// caps wall-clock time per §4.5.CP-022. When nil, no wall-clock budget is
	// imposed by this profile.
	// Spec: specs/control-points.md §6.2 (wall_clock_budget_ref : String | None).
	WallClockBudgetRef *BudgetRef `json:"wall_clock_budget_ref,omitempty"`

	// MaxIterations is the upper bound on agentic loop iterations permitted under
	// this profile. When multiple profiles compose, the smaller value wins.
	// Must be positive (≥ 1).
	// Spec: specs/control-points.md §6.2 (max_iterations : Integer).
	MaxIterations int `json:"max_iterations"`
}

// NewFreedomProfile returns a FreedomProfile with all list fields initialised to
// empty (non-nil) slices so callers can append without a nil-slice check.
//
// Name and MaxIterations are left at zero values; callers MUST set them before
// the profile can be considered valid.
func NewFreedomProfile() FreedomProfile {
	return FreedomProfile{
		ToolWhitelist: []string{},
		WritablePaths: []string{},
	}
}

// Valid reports whether the FreedomProfile is structurally well-formed per
// specs/control-points.md §6.2:
//   - Name must be non-empty.
//   - MaxIterations must be positive (≥ 1).
//   - TokenBudgetRef, if non-nil, must be a valid BudgetRef.
//   - WallClockBudgetRef, if non-nil, must be a valid BudgetRef.
func (f FreedomProfile) Valid() bool {
	if f.Name == "" {
		return false
	}
	if f.MaxIterations < 1 {
		return false
	}
	if f.TokenBudgetRef != nil && !f.TokenBudgetRef.Valid() {
		return false
	}
	if f.WallClockBudgetRef != nil && !f.WallClockBudgetRef.Valid() {
		return false
	}
	return true
}
