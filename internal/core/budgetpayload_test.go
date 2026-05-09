package core

import (
	"encoding/json"
	"testing"
)

// budgetPayloadFixture returns a fully-populated BudgetPayload with all fields
// set to valid non-zero values, suitable for structural tests (hk-a8bg.66).
func budgetPayloadFixture(t *testing.T) BudgetPayload {
	t.Helper()

	return BudgetPayload{
		Resource:         BudgetResourceTokens,
		Scope:            BudgetScopePerRun,
		Limit:            10000,
		WarningThreshold: 0.8,
		ScopeTarget:      ScopeTargetWildcard(),
	}
}

// TestNewBudgetPayload_WarningThreshold verifies that NewBudgetPayload sets
// WarningThreshold to 0.8 per specs/control-points.md §4.5.CP-022.
func TestNewBudgetPayload_WarningThreshold(t *testing.T) {
	t.Parallel()

	bp := NewBudgetPayload()
	if bp.WarningThreshold != 0.8 {
		t.Errorf("NewBudgetPayload().WarningThreshold = %v, want 0.8", bp.WarningThreshold)
	}
}

// TestNewBudgetPayload_ZeroValueOtherFields verifies that NewBudgetPayload
// leaves Resource, Scope, Limit, and ScopeTarget at their zero values so
// callers know they must set them explicitly.
func TestNewBudgetPayload_ZeroValueOtherFields(t *testing.T) {
	t.Parallel()

	bp := NewBudgetPayload()
	if bp.Resource != "" {
		t.Errorf("NewBudgetPayload().Resource = %q, want empty", bp.Resource)
	}
	if bp.Scope != "" {
		t.Errorf("NewBudgetPayload().Scope = %q, want empty", bp.Scope)
	}
	if bp.Limit != 0 {
		t.Errorf("NewBudgetPayload().Limit = %d, want 0", bp.Limit)
	}
}

// TestBudgetPayload_ZeroValueWarningThreshold confirms that a zero-value
// BudgetPayload (struct literal without NewBudgetPayload) has WarningThreshold
// 0.0, documenting the need to use NewBudgetPayload for the default.
func TestBudgetPayload_ZeroValueWarningThreshold(t *testing.T) {
	t.Parallel()

	var bp BudgetPayload
	if bp.WarningThreshold != 0.0 {
		t.Errorf("zero-value BudgetPayload.WarningThreshold = %v, want 0.0 (use NewBudgetPayload for 0.8 default)", bp.WarningThreshold)
	}
}

// TestBudgetPayload_ValidFullyPopulated verifies that a fully-populated fixture
// is valid.
func TestBudgetPayload_ValidFullyPopulated(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	if !bp.Valid() {
		t.Error("budgetPayloadFixture should be valid, but Valid() returned false")
	}
}

// TestBudgetPayload_ValidAllResources verifies that each BudgetResource value
// produces a valid payload.
func TestBudgetPayload_ValidAllResources(t *testing.T) {
	t.Parallel()

	resources := []BudgetResource{
		BudgetResourceTokens,
		BudgetResourceWallClockSeconds,
		BudgetResourceIterations,
	}
	for _, r := range resources {
		bp := budgetPayloadFixture(t)
		bp.Resource = r
		if !bp.Valid() {
			t.Errorf("BudgetPayload with Resource=%q should be valid", r)
		}
	}
}

// TestBudgetPayload_ValidAllScopes verifies that each BudgetScope value
// produces a valid payload.
func TestBudgetPayload_ValidAllScopes(t *testing.T) {
	t.Parallel()

	scopes := []BudgetScope{
		BudgetScopePerRole,
		BudgetScopePerRun,
		BudgetScopePerState,
	}
	for _, s := range scopes {
		bp := budgetPayloadFixture(t)
		bp.Scope = s
		if !bp.Valid() {
			t.Errorf("BudgetPayload with Scope=%q should be valid", s)
		}
	}
}

// TestBudgetPayload_InvalidResource verifies that an unknown Resource makes
// the payload invalid.
func TestBudgetPayload_InvalidResource(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.Resource = "cpu_seconds"
	if bp.Valid() {
		t.Error("BudgetPayload with unknown Resource should be invalid")
	}
}

// TestBudgetPayload_InvalidResourceEmpty verifies that an empty Resource makes
// the payload invalid.
func TestBudgetPayload_InvalidResourceEmpty(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.Resource = ""
	if bp.Valid() {
		t.Error("BudgetPayload with empty Resource should be invalid")
	}
}

// TestBudgetPayload_InvalidScope verifies that an unknown Scope makes the
// payload invalid.
func TestBudgetPayload_InvalidScope(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.Scope = "per_session"
	if bp.Valid() {
		t.Error("BudgetPayload with unknown Scope should be invalid")
	}
}

// TestBudgetPayload_InvalidLimitZero verifies that a Limit of 0 makes the
// payload invalid (Limit must be positive per §6.1.4).
func TestBudgetPayload_InvalidLimitZero(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.Limit = 0
	if bp.Valid() {
		t.Error("BudgetPayload with Limit=0 should be invalid")
	}
}

// TestBudgetPayload_InvalidLimitNegative verifies that a negative Limit makes
// the payload invalid.
func TestBudgetPayload_InvalidLimitNegative(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.Limit = -1
	if bp.Valid() {
		t.Error("BudgetPayload with Limit=-1 should be invalid")
	}
}

// TestBudgetPayload_ValidLimitOne verifies that a Limit of 1 is valid (boundary).
func TestBudgetPayload_ValidLimitOne(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.Limit = 1
	if !bp.Valid() {
		t.Error("BudgetPayload with Limit=1 should be valid (minimum positive)")
	}
}

// TestBudgetPayload_ValidWarningThresholdBoundaries verifies that
// WarningThreshold at 0.0 and 1.0 (both inclusive) is accepted.
func TestBudgetPayload_ValidWarningThresholdBoundaries(t *testing.T) {
	t.Parallel()

	for _, wt := range []float64{0.0, 0.5, 0.8, 1.0} {
		bp := budgetPayloadFixture(t)
		bp.WarningThreshold = wt
		if !bp.Valid() {
			t.Errorf("BudgetPayload with WarningThreshold=%v should be valid", wt)
		}
	}
}

// TestBudgetPayload_InvalidWarningThresholdAboveOne verifies that
// WarningThreshold > 1.0 makes the payload invalid.
func TestBudgetPayload_InvalidWarningThresholdAboveOne(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.WarningThreshold = 1.1
	if bp.Valid() {
		t.Error("BudgetPayload with WarningThreshold=1.1 should be invalid")
	}
}

// TestBudgetPayload_InvalidWarningThresholdBelowZero verifies that
// WarningThreshold < 0.0 makes the payload invalid.
func TestBudgetPayload_InvalidWarningThresholdBelowZero(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.WarningThreshold = -0.1
	if bp.Valid() {
		t.Error("BudgetPayload with WarningThreshold=-0.1 should be invalid")
	}
}

// TestBudgetPayload_InvalidScopeTarget verifies that an invalid ScopeTarget
// (zero value) makes the payload invalid.
func TestBudgetPayload_InvalidScopeTarget(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	bp.ScopeTarget = ScopeTarget{} // zero value — Kind is empty string, not a valid constant
	if bp.Valid() {
		t.Error("BudgetPayload with zero-value ScopeTarget should be invalid")
	}
}

// TestBudgetPayload_ValidScopeTargetShapes verifies that all four ScopeTarget
// shapes yield a valid payload.
func TestBudgetPayload_ValidScopeTargetShapes(t *testing.T) {
	t.Parallel()

	predicate, err := ScopeTargetPredicate("orchestrator")
	if err != nil {
		t.Fatalf("ScopeTargetPredicate: %v", err)
	}
	list, err := ScopeTargetList([]string{"role-a", "role-b"})
	if err != nil {
		t.Fatalf("ScopeTargetList: %v", err)
	}
	singleton, err := ScopeTargetSingleton("role-orchestrator")
	if err != nil {
		t.Fatalf("ScopeTargetSingleton: %v", err)
	}

	shapes := []ScopeTarget{
		ScopeTargetWildcard(),
		predicate,
		list,
		singleton,
	}
	for _, st := range shapes {
		bp := budgetPayloadFixture(t)
		bp.ScopeTarget = st
		if !bp.Valid() {
			t.Errorf("BudgetPayload with ScopeTarget kind=%q should be valid", st.Kind)
		}
	}
}

// TestBudgetPayload_JSONRoundTrip verifies that a fully-populated BudgetPayload
// survives a JSON marshal/unmarshal round-trip with all fields intact
// (specs/control-points.md §6.3 wire shape).
func TestBudgetPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := budgetPayloadFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got BudgetPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Resource != orig.Resource {
		t.Errorf("Resource: got %q, want %q", got.Resource, orig.Resource)
	}
	if got.Scope != orig.Scope {
		t.Errorf("Scope: got %q, want %q", got.Scope, orig.Scope)
	}
	if got.Limit != orig.Limit {
		t.Errorf("Limit: got %d, want %d", got.Limit, orig.Limit)
	}
	if got.WarningThreshold != orig.WarningThreshold {
		t.Errorf("WarningThreshold: got %v, want %v", got.WarningThreshold, orig.WarningThreshold)
	}
	if !got.Valid() {
		t.Error("round-tripped BudgetPayload is not valid")
	}
}

// TestBudgetPayload_JSONFieldNames verifies that JSON keys match the
// snake_case field names declared in the spec (§6.1.4).
func TestBudgetPayload_JSONFieldNames(t *testing.T) {
	t.Parallel()

	bp := budgetPayloadFixture(t)
	data, err := json.Marshal(bp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	for _, key := range []string{"resource", "scope", "limit", "warning_threshold", "scope_target"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON key %q absent from marshalled BudgetPayload", key)
		}
	}
}
