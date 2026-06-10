package core

// s02_hka8bg45_test.go — Tests for CP-043: Registry is a single in-process
// table owned by S02 (Policy Engine).
//
// Covers:
//   - S02PolicyEngine holds a *MapRegistry (CP-043 ownership)
//   - Registry() accessor returns the owned registry (CP-043 single source of truth)
//   - RegisterControlPoints populates the registry (§7.1 registration surface)
//   - PolicyEngine interface satisfied by both NoOpPolicyEngine and S02PolicyEngine
//   - NoOpPolicyEngine.Registry() returns a non-nil empty registry
//
// Refs: hk-a8bg.45

import (
	"errors"
	"testing"
)

// --- compile-time interface assertions ---

var (
	_ PolicyEngine = (*S02PolicyEngine)(nil)
	_ PolicyEngine = NoOpPolicyEngine{}
)

// TestS02PolicyEngine_ImplementsInterface verifies that S02PolicyEngine satisfies
// the PolicyEngine interface at compile time.
func TestS02PolicyEngine_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ PolicyEngine = NewS02PolicyEngine()
}

// TestS02PolicyEngine_RegistryNotNil verifies that NewS02PolicyEngine returns an
// engine whose Registry() is non-nil (CP-043: owned in-process table).
func TestS02PolicyEngine_RegistryNotNil(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()
	if eng.Registry() == nil {
		t.Error("S02PolicyEngine.Registry() = nil, want non-nil MapRegistry")
	}
}

// TestS02PolicyEngine_RegistryEmptyOnConstruction verifies that a freshly
// constructed S02PolicyEngine holds an empty registry (no ControlPoints yet).
func TestS02PolicyEngine_RegistryEmptyOnConstruction(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()
	all := eng.Registry().All()
	if len(all) != 0 {
		t.Errorf("fresh S02PolicyEngine.Registry().All() = %v, want empty slice", all)
	}
}

// TestS02PolicyEngine_EvaluatePermitted verifies that S02PolicyEngine.Evaluate
// returns Permitted=true at the current implementation stage (post-MVH stub).
func TestS02PolicyEngine_EvaluatePermitted(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()
	v := eng.Evaluate(PolicyEvalContext{})
	if !v.Permitted {
		t.Error("S02PolicyEngine.Evaluate().Permitted = false, want true")
	}
}

// TestS02PolicyEngine_EvaluateNoConstraints verifies that S02PolicyEngine.Evaluate
// returns nil Constraints at the current implementation stage.
func TestS02PolicyEngine_EvaluateNoConstraints(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()
	v := eng.Evaluate(PolicyEvalContext{})
	if v.Constraints != nil {
		t.Errorf("S02PolicyEngine.Evaluate().Constraints = %v, want nil", v.Constraints)
	}
}

// TestS02PolicyEngine_RegisterControlPoints_SingleCP verifies that
// RegisterControlPoints registers a valid ControlPoint and makes it queryable
// via Registry().LookupByName (CP-043 single source of truth).
func TestS02PolicyEngine_RegisterControlPoints_SingleCP(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()

	cp := s02FixtureGateCP(t, "policy.gate.smoke")
	if err := eng.RegisterControlPoints([]ControlPoint{cp}); err != nil {
		t.Fatalf("RegisterControlPoints(%q): unexpected error: %v", cp.Name, err)
	}

	got, ok := eng.Registry().LookupByName("policy.gate.smoke")
	if !ok {
		t.Fatalf("Registry().LookupByName(%q) = _, false; want found", "policy.gate.smoke")
	}
	if got.Name != "policy.gate.smoke" {
		t.Errorf("LookupByName().Name = %q, want %q", got.Name, "policy.gate.smoke")
	}
}

// TestS02PolicyEngine_RegisterControlPoints_MultipleCP verifies that all
// registered CPs appear in Registry().All() (single source of truth invariant,
// CP-043).
func TestS02PolicyEngine_RegisterControlPoints_MultipleCP(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()

	cps := []ControlPoint{
		s02FixtureGateCP(t, "policy.gate.alpha"),
		s02FixtureHookCP(t, "policy.hook.beta"),
	}
	if err := eng.RegisterControlPoints(cps); err != nil {
		t.Fatalf("RegisterControlPoints: unexpected error: %v", err)
	}

	all := eng.Registry().All()
	if len(all) != 2 {
		t.Errorf("Registry().All() len = %d, want 2", len(all))
	}
}

// TestS02PolicyEngine_RegisterControlPoints_IdempotentIdenticalBody verifies that
// re-registering a ControlPoint with the same body succeeds silently (CP-044).
func TestS02PolicyEngine_RegisterControlPoints_IdempotentIdenticalBody(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()

	cp := s02FixtureGateCP(t, "policy.gate.smoke")
	if err := eng.RegisterControlPoints([]ControlPoint{cp, cp}); err != nil {
		t.Errorf("RegisterControlPoints with duplicate identical CP: unexpected error: %v", err)
	}
}

// TestS02PolicyEngine_RegisterControlPoints_DivergentBodyFails verifies that
// registering a divergent body under an existing name fails (CP-044).
func TestS02PolicyEngine_RegisterControlPoints_DivergentBodyFails(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()

	cp1 := s02FixtureGateCP(t, "policy.gate.smoke")
	cp2 := s02FixtureHookCP(t, "policy.gate.smoke") // same name, different kind = divergent body

	err := eng.RegisterControlPoints([]ControlPoint{cp1, cp2})
	if err == nil {
		t.Fatal("RegisterControlPoints with divergent body: want error, got nil")
	}
	if !errors.Is(err, ErrDivergentBody) {
		t.Errorf("error = %v, want ErrDivergentBody", err)
	}
}

// TestS02PolicyEngine_RegistrySingleSourceOfTruth verifies that registry state
// is visible through both the engine-level accessor and a locally-held Registry
// reference — they are the same object (CP-043 single source of truth).
func TestS02PolicyEngine_RegistrySingleSourceOfTruth(t *testing.T) {
	t.Parallel()
	eng := NewS02PolicyEngine()
	reg := eng.Registry()

	cp := s02FixtureGateCP(t, "policy.gate.truth")
	if err := eng.RegisterControlPoints([]ControlPoint{cp}); err != nil {
		t.Fatalf("RegisterControlPoints: %v", err)
	}

	// The reference obtained before registration must now see the CP.
	_, ok := reg.LookupByName("policy.gate.truth")
	if !ok {
		t.Error("Registry reference obtained before registration does not see newly registered CP; expected single in-process table")
	}
}

// TestNoOpPolicyEngine_RegistryNotNil verifies that NoOpPolicyEngine.Registry()
// returns a non-nil (though always-empty) registry. This prevents nil-pointer
// dereferences in callers that hold a PolicyEngine interface at MVH.
func TestNoOpPolicyEngine_RegistryNotNil(t *testing.T) {
	t.Parallel()
	var eng PolicyEngine = NoOpPolicyEngine{}
	if eng.Registry() == nil {
		t.Error("NoOpPolicyEngine.Registry() = nil, want non-nil empty registry")
	}
}

// TestNoOpPolicyEngine_RegistryAlwaysEmpty verifies that NoOpPolicyEngine.Registry()
// returns an empty registry (no ControlPoints at MVH).
func TestNoOpPolicyEngine_RegistryAlwaysEmpty(t *testing.T) {
	t.Parallel()
	eng := NoOpPolicyEngine{}
	all := eng.Registry().All()
	if len(all) != 0 {
		t.Errorf("NoOpPolicyEngine.Registry().All() = %v, want empty slice", all)
	}
}

// TestPolicyEngine_Interface_RegistryAccessible verifies that the Registry()
// method is accessible through the PolicyEngine interface — confirming the
// composition root can call it without knowing the concrete type.
func TestPolicyEngine_Interface_RegistryAccessible(t *testing.T) {
	t.Parallel()
	var eng PolicyEngine = NewS02PolicyEngine()

	cp := s02FixtureGateCP(t, "policy.gate.interface")
	if err := eng.(*S02PolicyEngine).RegisterControlPoints([]ControlPoint{cp}); err != nil {
		t.Fatalf("RegisterControlPoints: %v", err)
	}

	reg := eng.Registry()
	if reg == nil {
		t.Fatal("PolicyEngine.Registry() = nil through interface, want non-nil")
	}
	_, ok := reg.LookupByName("policy.gate.interface")
	if !ok {
		t.Error("Registry().LookupByName through PolicyEngine interface: not found")
	}
}

// --- test helpers ---

// s02FixtureGateCP returns a minimal valid Gate-kind ControlPoint with the given name.
// Used across CP-043 tests to produce registerable ControlPoints without
// depending on YAML parsing (which is post-MVH).
func s02FixtureGateCP(t *testing.T, name string) ControlPoint {
	t.Helper()
	cp := registryFixtureControlPoint(t, KindGate)
	cp.Name = name
	return cp
}

// s02FixtureHookCP returns a minimal valid Hook-kind ControlPoint with the given name.
func s02FixtureHookCP(t *testing.T, name string) ControlPoint {
	t.Helper()
	cp := registryFixtureControlPoint(t, KindHook)
	cp.Name = name
	return cp
}
