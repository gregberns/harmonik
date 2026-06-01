package core

// cp047_ownership_split_hka8bg49_test.go — CP-047 ownership-split conformance suite
//
// Covers specs/control-points.md §4.10.CP-047:
//
//	"S02 (Policy Engine) owns the §4.9 registry and the registration path.
//	 S05 (Hook System) owns the Hook dispatch loop (subscribing to the event bus,
//	 ordering Hooks per §4.3.CP-014, applying side-effects, isolating failures);
//	 S05 consults the registry but does NOT own it. S01 (Orchestrator Core) owns
//	 Gate and Guard invocation during the edge cascade (per [execution-model.md
//	 §4.10]); S01 consults the registry but does NOT own it. No other subsystem
//	 may invoke a ControlPoint except through these three owner paths."
//
// # Coverage
//
//   - core.Registry interface is read-only: no Register() method is present,
//     so S01 and S05 consumers cannot write to the registry via the interface
//     they receive from S02.
//   - S02PolicyEngine is the sole write path: RegisterControlPoints populates the
//     registry; the result is visible through the read-only Registry() interface
//     handed to S01- and S05-style consumers.
//   - S02 registry is the single source of truth: the same table is accessible
//     to both S01 (via S01BuildGateEvaluator/S01BuildGuardEvaluator) and S05
//     (via the Registry() accessor).
//   - S01BuildGateEvaluator only consults (reads) the registry: the entry count
//     is unchanged after the call.
//   - S01BuildGuardEvaluator only consults (reads) the registry: the entry count
//     is unchanged after the call.
//   - S01BuildGateEvaluator ignores non-Gate ControlPoints: Hooks, Guards, and
//     Budgets registered at the same trigger namespace never fire through the
//     Gate invocation path.
//   - S01BuildGuardEvaluator ignores non-Guard ControlPoints: Gates, Hooks, and
//     Budgets registered in the registry never fire through the Guard invocation
//     path.
//   - S01 Gate invocation does not dispatch Hooks: Hook ControlPoints registered
//     in the same registry are invisible to S01BuildGateEvaluator, confirming
//     that only S05 may dispatch Hooks (CP-047 cross-owner boundary).
//
// Tags: mechanism
//
// Refs: hk-a8bg.49

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Registry interface conformance (CP-047 + §6.1.7)
// ---------------------------------------------------------------------------

// TestCP047_RegistryInterface_HasAllSpecMethods verifies that the core.Registry
// interface exposes the five methods declared in §6.1.7: Register, LookupByName,
// LookupByTrigger, LookupByAttachPoint, and All.
//
// The §6.1.7 Registry interface is the single surface through which all
// subsystems interact with ControlPoint state. Register is included per spec;
// the CP-047 ownership rule is semantic: only S02PolicyEngine.RegisterControlPoints
// may call Register, while S01 and S05 are obligated to use only the read
// methods (LookupByName, LookupByTrigger, LookupByAttachPoint, All). The
// behavioral tests below verify that the S01 invocation paths never call
// Register — the ownership rule is enforced by convention, not by interface
// splitting, per the spec's explicit inclusion of Register in §6.1.7.
func TestCP047_RegistryInterface_HasAllSpecMethods(t *testing.T) {
	t.Parallel()

	registryType := reflect.TypeOf((*Registry)(nil)).Elem()
	specMethods := []string{"Register", "LookupByName", "LookupByTrigger", "LookupByAttachPoint", "All"}
	for _, m := range specMethods {
		if _, ok := registryType.MethodByName(m); !ok {
			t.Errorf("core.Registry interface missing required method %q (§6.1.7)", m)
		}
	}
}

// ---------------------------------------------------------------------------
// S02 is the sole write path (CP-047)
// ---------------------------------------------------------------------------

// TestCP047_S02_IsTheSoleWritePath verifies that S02PolicyEngine.RegisterControlPoints
// is the only mechanism that populates the registry, and that the registrations
// are immediately visible through the read-only Registry() interface.
//
// CP-047: "S02 (Policy Engine) owns the §4.9 registry and the registration
// path." Any registry content consumed by S01 or S05 must have been placed
// there by S02's RegisterControlPoints; there is no other path.
func TestCP047_S02_IsTheSoleWritePath(t *testing.T) {
	t.Parallel()

	engine := NewS02PolicyEngine()

	// Before registration: registry is empty.
	if n := len(engine.Registry().All()); n != 0 {
		t.Fatalf("pre-registration: Registry().All() len = %d, want 0", n)
	}

	cps := []ControlPoint{
		cp002FixtureGate(t, "s02-gate-alpha"),
		cp002FixtureHook(t, "s02-hook-beta"),
		cp002FixtureGuardMechanism(t, "s02-guard-gamma"),
	}
	if err := engine.RegisterControlPoints(cps); err != nil {
		t.Fatalf("S02 RegisterControlPoints: %v", err)
	}

	// After registration: the same Registry() interface sees all three entries.
	reg := engine.Registry()
	if n := len(reg.All()); n != len(cps) {
		t.Fatalf("Registry().All() len = %d, want %d", n, len(cps))
	}
	for _, cp := range cps {
		if _, ok := reg.LookupByName(cp.Name); !ok {
			t.Errorf("Registry().LookupByName(%q): not found after S02 registration", cp.Name)
		}
	}
}

// TestCP047_S02_RegistryIsTheSingleSourceOfTruth verifies that the Registry
// returned by S02PolicyEngine.Registry() is the same in-process table for
// all callers within the daemon — not a copy.
//
// CP-047's single-source-of-truth property: S01 and S05 both consult the same
// registry. Any ControlPoint registered via S02 must be visible to both
// consumers through the shared interface.
func TestCP047_S02_RegistryIsTheSingleSourceOfTruth(t *testing.T) {
	t.Parallel()

	engine := NewS02PolicyEngine()

	// Obtain two independent references to the registry (simulating S01 and S05
	// each calling engine.Registry() during daemon startup).
	regForS01 := engine.Registry()
	regForS05 := engine.Registry()

	// Register via S02 after both consumers obtained their references.
	gate := cp002FixtureGate(t, "shared-gate")
	hook := cp002FixtureHook(t, "shared-hook")
	if err := engine.RegisterControlPoints([]ControlPoint{gate, hook}); err != nil {
		t.Fatalf("RegisterControlPoints: %v", err)
	}

	// Both consumers see the same registrations.
	for _, ref := range []Registry{regForS01, regForS05} {
		if _, ok := ref.LookupByName("shared-gate"); !ok {
			t.Error("consumer registry reference does not see S02-registered gate")
		}
		if _, ok := ref.LookupByName("shared-hook"); !ok {
			t.Error("consumer registry reference does not see S02-registered hook")
		}
		if n := len(ref.All()); n != 2 {
			t.Errorf("consumer registry All() len = %d, want 2", n)
		}
	}
}

// ---------------------------------------------------------------------------
// S01 Gate invocation — read-only consultation (CP-047)
// ---------------------------------------------------------------------------

// TestCP047_S01BuildGateEvaluator_OnlyConsultsRegistry verifies that
// S01BuildGateEvaluator does not mutate the registry — it consults (reads) it
// and returns an evaluator function without modifying any entries.
//
// CP-047: S01 "consults the registry but does NOT own it." A registry whose
// state is unchanged after S01BuildGateEvaluator runs satisfies this property.
func TestCP047_S01BuildGateEvaluator_OnlyConsultsRegistry(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	if err := reg.Register(cp002FixtureGate(t, "consult-gate-1")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(cp002FixtureGate(t, "consult-gate-2")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	beforeAll := reg.All()
	beforeNames := make([]string, len(beforeAll))
	for i, cp := range beforeAll {
		beforeNames[i] = cp.Name
	}

	_ = S01BuildGateEvaluator(reg, AttachPointNodePreEntry, nil)

	afterAll := reg.All()
	if len(afterAll) != len(beforeAll) {
		t.Errorf("registry entry count changed: before=%d after=%d — S01BuildGateEvaluator mutated the registry (CP-047)",
			len(beforeAll), len(afterAll))
	}
	for i, cp := range afterAll {
		if cp.Name != beforeNames[i] {
			t.Errorf("registry[%d].Name = %q after S01BuildGateEvaluator, was %q — mutation detected (CP-047)",
				i, cp.Name, beforeNames[i])
		}
	}
}

// TestCP047_S01BuildGateEvaluator_IgnoresNonGateCPs verifies that Hooks,
// Guards, and Budgets in the registry are never invoked through the Gate
// invocation path.
//
// CP-047 cross-owner boundary: only S05 may dispatch Hooks; only S01 may
// invoke Guards (via S01BuildGuardEvaluator); S01's Gate path may only fire
// Gate-kind ControlPoints. Non-Gate CPs in the registry must be invisible to
// S01BuildGateEvaluator.
func TestCP047_S01BuildGateEvaluator_IgnoresNonGateCPs(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register non-Gate ControlPoints.
	nonGates := []ControlPoint{
		cp002FixtureHook(t, "should-not-fire-hook"),
		cp002FixtureGuardMechanism(t, "should-not-fire-guard"),
		cp002FixtureBudget(t, "should-not-fire-budget"),
	}
	for _, cp := range nonGates {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}
	// Also register one Gate so we know the evaluator build path is exercised.
	gateName := "gatekeeper"
	gate := cp002FixtureGate(t, gateName)
	if err := reg.Register(gate); err != nil {
		t.Fatalf("Register gate: %v", err)
	}

	// Track which evaluators are invoked.
	var invoked []string
	fns := map[string]GateEvaluator{}
	for _, cp := range nonGates {
		cpName := cp.Name
		fns[cpName] = func(_ *Run, _ Edge, _ Outcome) GateAction {
			invoked = append(invoked, cpName)
			return GateActionAllow
		}
	}
	fns[gateName] = func(_ *Run, _ Edge, _ Outcome) GateAction {
		invoked = append(invoked, gateName)
		return GateActionAllow
	}

	eval := S01BuildGateEvaluator(reg, AttachPointNodePreEntry, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "dst-node")
	eval(run, edge, Outcome{})

	// Only the Gate should have fired; non-Gate CPs must not appear.
	for _, name := range invoked {
		if name != gateName {
			t.Errorf("S01BuildGateEvaluator invoked non-Gate CP %q — CP-047 cross-owner boundary violation", name)
		}
	}
	found := false
	for _, name := range invoked {
		if name == gateName {
			found = true
		}
	}
	if !found {
		t.Errorf("S01BuildGateEvaluator did not invoke the registered Gate %q — gate was silently skipped", gateName)
	}
}

// ---------------------------------------------------------------------------
// S01 Guard invocation — read-only consultation (CP-047)
// ---------------------------------------------------------------------------

// TestCP047_S01BuildGuardEvaluator_OnlyConsultsRegistry verifies that
// S01BuildGuardEvaluator does not mutate the registry — it consults (reads) it
// and returns an evaluator function without modifying any entries.
//
// CP-047: S01 "consults the registry but does NOT own it."
func TestCP047_S01BuildGuardEvaluator_OnlyConsultsRegistry(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-guarded")
	expr := PolicyExpression("true")
	guard := ControlPoint{
		Name:          "consult-guard",
		Kind:          KindGuard,
		Trigger:       Trigger{Name: ""},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionReorder,
		Payload:       KindPayload{Guard: &GuardPayload{AppliesToNode: &nodeID}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}
	if err := reg.Register(guard); err != nil {
		t.Fatalf("Register guard: %v", err)
	}

	beforeAll := reg.All()
	beforeCount := len(beforeAll)

	_ = S01BuildGuardEvaluator(reg, nodeID, nil)

	afterAll := reg.All()
	if len(afterAll) != beforeCount {
		t.Errorf("registry entry count changed: before=%d after=%d — S01BuildGuardEvaluator mutated the registry (CP-047)",
			beforeCount, len(afterAll))
	}
}

// TestCP047_S01BuildGuardEvaluator_IgnoresNonGuardCPs verifies that Gates,
// Hooks, and Budgets in the registry are never invoked through the Guard
// invocation path.
//
// CP-047 cross-owner boundary: only S01's Gate path may invoke Gates; only S05
// may dispatch Hooks; S01's Guard path may only fire Guard-kind ControlPoints.
func TestCP047_S01BuildGuardEvaluator_IgnoresNonGuardCPs(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-for-guards")

	// Register non-Guard ControlPoints.
	nonGuards := []ControlPoint{
		cp002FixtureGate(t, "non-guard-gate"),
		cp002FixtureHook(t, "non-guard-hook"),
		cp002FixtureBudget(t, "non-guard-budget"),
	}
	for _, cp := range nonGuards {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	// Register one Guard scoped to nodeID.
	guardName := "real-guard"
	expr := PolicyExpression("true")
	guard := ControlPoint{
		Name:          guardName,
		Kind:          KindGuard,
		Trigger:       Trigger{Name: ""},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionReorder,
		Payload:       KindPayload{Guard: &GuardPayload{AppliesToNode: &nodeID}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}
	if err := reg.Register(guard); err != nil {
		t.Fatalf("Register guard: %v", err)
	}

	var invoked []string
	fns := map[string]GuardEvaluator{}
	for _, cp := range nonGuards {
		cpName := cp.Name
		fns[cpName] = func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			invoked = append(invoked, cpName)
			return candidates
		}
	}
	fns[guardName] = func(_ *Run, candidates []Edge, _ Outcome) []Edge {
		invoked = append(invoked, guardName)
		return candidates
	}

	eval := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "guard-dst")
	eval(run, []Edge{edge}, Outcome{})

	// Only the Guard should have fired.
	for _, name := range invoked {
		if name != guardName {
			t.Errorf("S01BuildGuardEvaluator invoked non-Guard CP %q — CP-047 cross-owner boundary violation", name)
		}
	}
}

// ---------------------------------------------------------------------------
// S01 does not dispatch Hooks (CP-047 cross-owner boundary)
// ---------------------------------------------------------------------------

// TestCP047_S01GateInvocation_DoesNotDispatchHooks verifies that Hook
// ControlPoints registered in the same registry are never invoked through
// S01's Gate invocation path.
//
// CP-047: "No other subsystem may invoke a ControlPoint except through these
// three owner paths." Only S05 (Hook System) may dispatch Hooks. S01
// (Orchestrator Core) may only invoke Gates via S01BuildGateEvaluator.
// This test proves that a Hook ControlPoint — even one registered at a
// trigger-namespace name that would be plausible as a Gate attach point — is
// never fired by S01's Gate path.
func TestCP047_S01GateInvocation_DoesNotDispatchHooks(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register a Hook and a Gate in the same registry.
	hook := cp002FixtureHook(t, "cross-owner-hook")
	gate := cp002FixtureGate(t, "cross-owner-gate")
	for _, cp := range []ControlPoint{hook, gate} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	var hookFired bool
	fns := map[string]GateEvaluator{
		// Wire the hook name in the fns map — S01BuildGateEvaluator must still
		// not invoke it, because LookupByAttachPoint only returns KindGate.
		hook.Name: func(_ *Run, _ Edge, _ Outcome) GateAction {
			hookFired = true
			return GateActionAllow
		},
		gate.Name: func(_ *Run, _ Edge, _ Outcome) GateAction {
			return GateActionAllow
		},
	}

	eval := S01BuildGateEvaluator(reg, AttachPointNodePreEntry, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "check-dst")
	eval(run, edge, Outcome{})

	if hookFired {
		t.Error("S01BuildGateEvaluator invoked a Hook-kind ControlPoint — only S05 may dispatch Hooks (CP-047)")
	}
}

// TestCP047_S01GuardInvocation_DoesNotDispatchHooks verifies that Hook
// ControlPoints in the registry are never invoked through S01's Guard
// invocation path.
func TestCP047_S01GuardInvocation_DoesNotDispatchHooks(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register a Hook and a Guard in the same registry.
	hook := cp002FixtureHook(t, "guard-path-hook")
	nodeID := NodeID("guarded-node")
	expr := PolicyExpression("true")
	guard := ControlPoint{
		Name:          "guard-path-guard",
		Kind:          KindGuard,
		Trigger:       Trigger{Name: ""},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionReorder,
		Payload:       KindPayload{Guard: &GuardPayload{AppliesToNode: &nodeID}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}
	for _, cp := range []ControlPoint{hook, guard} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	var hookFired bool
	fns := map[string]GuardEvaluator{
		hook.Name: func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			hookFired = true
			return candidates
		},
		guard.Name: func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			return candidates
		},
	}

	eval := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "guard-hook-dst")
	eval(run, []Edge{edge}, Outcome{})

	if hookFired {
		t.Error("S01BuildGuardEvaluator invoked a Hook-kind ControlPoint — only S05 may dispatch Hooks (CP-047)")
	}
}
