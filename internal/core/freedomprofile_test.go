package core

import (
	"encoding/json"
	"testing"
)

// freedomProfileFixture returns a fully-populated FreedomProfile with all
// fields set to valid non-zero values, suitable for structural tests (hk-a8bg.80).
func freedomProfileFixture(t *testing.T) FreedomProfile {
	t.Helper()

	tier := "sonnet"
	tokenRef := BudgetRef("token-budget")
	wallRef := BudgetRef("wall-clock-budget")
	return FreedomProfile{
		Name:               "standard-agent",
		ToolWhitelist:      []string{"bash", "read"},
		WritablePaths:      []string{"output/**"},
		ModelTier:          &tier,
		TokenBudgetRef:     &tokenRef,
		WallClockBudgetRef: &wallRef,
		MaxIterations:      100,
	}
}

// TestNewFreedomProfile_ListsNonNil verifies that all list fields returned by
// NewFreedomProfile are non-nil (empty but appendable).
func TestNewFreedomProfile_ListsNonNil(t *testing.T) {
	t.Parallel()

	fp := NewFreedomProfile()
	if fp.ToolWhitelist == nil {
		t.Error("NewFreedomProfile().ToolWhitelist is nil, want non-nil empty slice")
	}
	if fp.WritablePaths == nil {
		t.Error("NewFreedomProfile().WritablePaths is nil, want non-nil empty slice")
	}
}

// TestNewFreedomProfile_OptionalFieldsNil verifies that NewFreedomProfile
// leaves optional pointer fields nil.
func TestNewFreedomProfile_OptionalFieldsNil(t *testing.T) {
	t.Parallel()

	fp := NewFreedomProfile()
	if fp.ModelTier != nil {
		t.Errorf("NewFreedomProfile().ModelTier = %v, want nil", fp.ModelTier)
	}
	if fp.TokenBudgetRef != nil {
		t.Errorf("NewFreedomProfile().TokenBudgetRef = %v, want nil", fp.TokenBudgetRef)
	}
	if fp.WallClockBudgetRef != nil {
		t.Errorf("NewFreedomProfile().WallClockBudgetRef = %v, want nil", fp.WallClockBudgetRef)
	}
}

// TestNewFreedomProfile_ZeroValueNameAndIterations verifies that NewFreedomProfile
// leaves Name empty and MaxIterations at zero, documenting that callers must set
// them before the profile is valid.
func TestNewFreedomProfile_ZeroValueNameAndIterations(t *testing.T) {
	t.Parallel()

	fp := NewFreedomProfile()
	if fp.Name != "" {
		t.Errorf("NewFreedomProfile().Name = %q, want empty", fp.Name)
	}
	if fp.MaxIterations != 0 {
		t.Errorf("NewFreedomProfile().MaxIterations = %d, want 0", fp.MaxIterations)
	}
}

// TestFreedomProfile_ValidFullyPopulated verifies that a fully-populated fixture
// is valid.
func TestFreedomProfile_ValidFullyPopulated(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	if !fp.Valid() {
		t.Error("freedomProfileFixture should be valid, but Valid() returned false")
	}
}

// TestFreedomProfile_ValidNameEmpty verifies that an empty Name makes the
// profile invalid.
func TestFreedomProfile_ValidNameEmpty(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.Name = ""
	if fp.Valid() {
		t.Error("FreedomProfile with empty Name should be invalid")
	}
}

// TestFreedomProfile_ValidMaxIterationsZero verifies that MaxIterations=0 makes
// the profile invalid (must be positive per §6.2).
func TestFreedomProfile_ValidMaxIterationsZero(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.MaxIterations = 0
	if fp.Valid() {
		t.Error("FreedomProfile with MaxIterations=0 should be invalid")
	}
}

// TestFreedomProfile_ValidMaxIterationsNegative verifies that a negative
// MaxIterations makes the profile invalid.
func TestFreedomProfile_ValidMaxIterationsNegative(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.MaxIterations = -1
	if fp.Valid() {
		t.Error("FreedomProfile with MaxIterations=-1 should be invalid")
	}
}

// TestFreedomProfile_ValidMaxIterationsOne verifies that MaxIterations=1 is
// valid (boundary: minimum positive value).
func TestFreedomProfile_ValidMaxIterationsOne(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.MaxIterations = 1
	if !fp.Valid() {
		t.Error("FreedomProfile with MaxIterations=1 should be valid (minimum positive)")
	}
}

// TestFreedomProfile_ValidNilTokenBudgetRef verifies that a nil TokenBudgetRef
// is acceptable (field is optional).
func TestFreedomProfile_ValidNilTokenBudgetRef(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.TokenBudgetRef = nil
	if !fp.Valid() {
		t.Error("FreedomProfile with nil TokenBudgetRef should be valid (field is optional)")
	}
}

// TestFreedomProfile_ValidNilWallClockBudgetRef verifies that a nil
// WallClockBudgetRef is acceptable (field is optional).
func TestFreedomProfile_ValidNilWallClockBudgetRef(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.WallClockBudgetRef = nil
	if !fp.Valid() {
		t.Error("FreedomProfile with nil WallClockBudgetRef should be valid (field is optional)")
	}
}

// TestFreedomProfile_ValidNilModelTier verifies that a nil ModelTier is
// acceptable (field is optional).
func TestFreedomProfile_ValidNilModelTier(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	fp.ModelTier = nil
	if !fp.Valid() {
		t.Error("FreedomProfile with nil ModelTier should be valid (field is optional)")
	}
}

// TestFreedomProfile_ValidMinimal verifies that a profile with only the required
// fields set (Name + MaxIterations) is valid.
func TestFreedomProfile_ValidMinimal(t *testing.T) {
	t.Parallel()

	fp := FreedomProfile{
		Name:          "minimal",
		ToolWhitelist: []string{},
		WritablePaths: []string{},
		MaxIterations: 1,
	}
	if !fp.Valid() {
		t.Error("minimal FreedomProfile with Name and MaxIterations=1 should be valid")
	}
}

// TestFreedomProfile_JSONRoundTrip verifies that a fully-populated FreedomProfile
// survives a JSON marshal/unmarshal round-trip with all fields intact
// (specs/control-points.md §6.3 wire shape).
func TestFreedomProfile_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := freedomProfileFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got FreedomProfile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Name != orig.Name {
		t.Errorf("Name: got %q, want %q", got.Name, orig.Name)
	}
	if got.MaxIterations != orig.MaxIterations {
		t.Errorf("MaxIterations: got %d, want %d", got.MaxIterations, orig.MaxIterations)
	}
	if got.ModelTier == nil || *got.ModelTier != *orig.ModelTier {
		t.Errorf("ModelTier round-trip: got %v, want %v", got.ModelTier, orig.ModelTier)
	}
	if got.TokenBudgetRef == nil || *got.TokenBudgetRef != *orig.TokenBudgetRef {
		t.Errorf("TokenBudgetRef round-trip: got %v, want %v", got.TokenBudgetRef, orig.TokenBudgetRef)
	}
	if got.WallClockBudgetRef == nil || *got.WallClockBudgetRef != *orig.WallClockBudgetRef {
		t.Errorf("WallClockBudgetRef round-trip: got %v, want %v", got.WallClockBudgetRef, orig.WallClockBudgetRef)
	}
	if len(got.ToolWhitelist) != len(orig.ToolWhitelist) {
		t.Errorf("ToolWhitelist length: got %d, want %d", len(got.ToolWhitelist), len(orig.ToolWhitelist))
	}
	if len(got.WritablePaths) != len(orig.WritablePaths) {
		t.Errorf("WritablePaths length: got %d, want %d", len(got.WritablePaths), len(orig.WritablePaths))
	}
	if !got.Valid() {
		t.Error("round-tripped FreedomProfile is not valid")
	}
}

// TestFreedomProfile_JSONOmitsOptionalFieldsWhenNil verifies that when optional
// pointer fields are nil they are omitted from JSON output (omitempty).
func TestFreedomProfile_JSONOmitsOptionalFieldsWhenNil(t *testing.T) {
	t.Parallel()

	fp := FreedomProfile{
		Name:          "minimal",
		ToolWhitelist: []string{},
		WritablePaths: []string{},
		MaxIterations: 10,
	}
	data, err := json.Marshal(fp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	for _, key := range []string{"model_tier", "token_budget_ref", "wall_clock_budget_ref"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q present in JSON when field is nil, want omitted", key)
		}
	}
}

// TestFreedomProfile_JSONIncludesOptionalFieldsWhenSet verifies that when
// optional pointer fields are non-nil they appear in JSON output.
func TestFreedomProfile_JSONIncludesOptionalFieldsWhenSet(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	data, err := json.Marshal(fp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	for _, key := range []string{"model_tier", "token_budget_ref", "wall_clock_budget_ref"} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q absent from JSON when field is set, want present", key)
		}
	}
}

// TestFreedomProfile_JSONFieldNames verifies that JSON keys match the
// snake_case field names declared in the spec (§6.2).
func TestFreedomProfile_JSONFieldNames(t *testing.T) {
	t.Parallel()

	fp := freedomProfileFixture(t)
	data, err := json.Marshal(fp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{
		"name",
		"tool_whitelist",
		"writable_paths",
		"model_tier",
		"token_budget_ref",
		"wall_clock_budget_ref",
		"max_iterations",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON key %q absent from marshalled FreedomProfile", key)
		}
	}
}

// TestFreedomProfile_ZeroValueIsInvalid confirms that a zero-value
// FreedomProfile (struct literal without any fields set) is invalid, because
// Name is empty and MaxIterations is 0.
func TestFreedomProfile_ZeroValueIsInvalid(t *testing.T) {
	t.Parallel()

	var fp FreedomProfile
	if fp.Valid() {
		t.Error("zero-value FreedomProfile should be invalid (empty Name, MaxIterations=0)")
	}
}
