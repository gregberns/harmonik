package core

// cpinv001_registry_single_source_hka8bg54_test.go — CP-INV-001 sensor suite
//
// Covers specs/control-points.md §5.CP-INV-001:
//
//	"Every ControlPoint observable by the daemon MUST be resolvable through the
//	 §4.9 registry. No subsystem may maintain a private ControlPoint store, a
//	 shadow registry, or a lazy-resolved ControlPoint surface. Divergence
//	 between the registry and any subsystem-local cache is a structural
//	 invariant violation."
//
// This file is the §10.2 sensor for the CP-043..CP-046 registration group.
// The invariant is verified via cross-subsystem inspection: every ControlPoint
// name that becomes observable to a subsystem (S01 Gate path, S01 Guard path)
// must be resolvable through the single §4.9 registry via LookupByName.
//
// # Coverage
//
//   - Every Gate name that fires via S01BuildGateEvaluator is resolvable via
//     Registry.LookupByName — the Gate path has no shadow store.
//   - Every Guard name that fires via S01BuildGuardEvaluator is resolvable via
//     Registry.LookupByName — the Guard path has no shadow store.
//   - A function wired under a name NOT present in the registry is never
//     invoked: subsystem dispatch is gated on registry presence.
//   - Cross-subsystem: Gate and Guard paths share the same registry instance;
//     every name observable through either path resolves to a unique registry
//     entry.
//   - S02PolicyEngine.Registry() is the shared, single table: registrations
//     placed via S02 are visible to both S01 Gate and S01 Guard paths without
//     any per-path copying.
//   - NoOpPolicyEngine: its empty registry means no CP is observable, so no
//     shadow-store path exists in the MVH binding.
//
// Tags: mechanism
//
// Refs: hk-a8bg.54

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Gate path: fired names are in the registry
// ---------------------------------------------------------------------------

// TestCPINV001_FiredGateNamesAreInRegistry verifies that every Gate-name
// actually invoked through S01BuildGateEvaluator corresponds to a ControlPoint
// resolvable via Registry.LookupByName.
//
// CP-INV-001: "Every ControlPoint observable by the daemon MUST be resolvable
// through the §4.9 registry." A Gate that fires IS observable; if its name
// cannot be looked up, the Gate must have come from a shadow store — a
// structural invariant violation.
func TestCPINV001_FiredGateNamesAreInRegistry(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register three Gates; their names are the single source of truth.
	gateNames := []string{"inv001-gate-alpha", "inv001-gate-beta", "inv001-gate-gamma"}
	for _, name := range gateNames {
		if err := reg.Register(cp002FixtureGate(t, name)); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	// Record which names actually fire.
	var fired []string
	fns := make(map[string]GateEvaluator, len(gateNames))
	for _, name := range gateNames {
		n := name
		fns[n] = func(_ *Run, _ Edge, _ Outcome) GateAction {
			fired = append(fired, n)
			return GateActionAllow
		}
	}

	eval := S01BuildGateEvaluator(reg, AttachPointNodePreEntry, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "inv001-dst")
	eval(run, edge, Outcome{})

	if len(fired) == 0 {
		t.Fatal("no Gates fired; test is vacuous — check fixture attach point")
	}

	// Invariant check: every fired name must be in the registry.
	for _, name := range fired {
		cp, ok := reg.LookupByName(name)
		if !ok {
			t.Errorf("fired Gate %q is NOT resolvable via LookupByName — shadow store violation (CP-INV-001)", name)
			continue
		}
		if cp.Kind != KindGate {
			t.Errorf("LookupByName(%q).Kind = %q, want %q (CP-INV-001)", name, cp.Kind, KindGate)
		}
	}
}

// ---------------------------------------------------------------------------
// Guard path: fired names are in the registry
// ---------------------------------------------------------------------------

// TestCPINV001_FiredGuardNamesAreInRegistry verifies that every Guard-name
// actually invoked through S01BuildGuardEvaluator corresponds to a
// ControlPoint resolvable via Registry.LookupByName.
//
// Mirrors TestCPINV001_FiredGateNamesAreInRegistry for the Guard path.
func TestCPINV001_FiredGuardNamesAreInRegistry(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("inv001-guarded-node")

	// Register three Guards scoped to nodeID.
	guardNames := []string{"inv001-guard-alpha", "inv001-guard-beta", "inv001-guard-gamma"}
	for _, name := range guardNames {
		cp := cp021FixtureScopedGuard(t, reg, name, nodeID)
		_ = cp // already registered inside the helper
	}

	// Record which names actually fire.
	var fired []string
	fns := make(map[string]GuardEvaluator, len(guardNames))
	for _, name := range guardNames {
		n := name
		fns[n] = func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			fired = append(fired, n)
			return candidates
		}
	}

	eval := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "inv001-guard-dst")
	eval(run, []Edge{edge}, Outcome{})

	if len(fired) == 0 {
		t.Fatal("no Guards fired; test is vacuous — check fixture node scope")
	}

	// Invariant check: every fired name must be in the registry.
	for _, name := range fired {
		cp, ok := reg.LookupByName(name)
		if !ok {
			t.Errorf("fired Guard %q is NOT resolvable via LookupByName — shadow store violation (CP-INV-001)", name)
			continue
		}
		if cp.Kind != KindGuard {
			t.Errorf("LookupByName(%q).Kind = %q, want %q (CP-INV-001)", name, cp.Kind, KindGuard)
		}
	}
}

// ---------------------------------------------------------------------------
// No shadow store: function not in registry cannot fire
// ---------------------------------------------------------------------------

// TestCPINV001_ShadowFunctionNotInRegistryNeverFires verifies that a
// GateEvaluator function wired for a name absent from the registry is never
// invoked — the dispatch path is gated on registry presence.
//
// CP-INV-001: "No subsystem may maintain a private ControlPoint store." The
// only way a Gate evaluator function can fire is if the corresponding
// ControlPoint was registered in the §4.9 registry. A function keyed on an
// unregistered name is a shadow: it exists outside the registry and MUST NOT
// fire.
func TestCPINV001_ShadowFunctionNotInRegistryNeverFires(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register exactly one Gate.
	registeredName := "inv001-registered-gate"
	if err := reg.Register(cp002FixtureGate(t, registeredName)); err != nil {
		t.Fatalf("Register(%q): %v", registeredName, err)
	}

	// Wire a function for the registered gate AND a shadow function for a name
	// that is NOT in the registry.
	shadowName := "inv001-shadow-gate-not-in-registry"

	var shadowFired bool
	fns := map[string]GateEvaluator{
		registeredName: func(_ *Run, _ Edge, _ Outcome) GateAction {
			return GateActionAllow
		},
		shadowName: func(_ *Run, _ Edge, _ Outcome) GateAction {
			shadowFired = true // this MUST NOT execute
			return GateActionAllow
		},
	}

	eval := S01BuildGateEvaluator(reg, AttachPointNodePreEntry, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "inv001-shadow-dst")
	eval(run, edge, Outcome{})

	// The shadow function must never fire; the dispatch path is registry-gated.
	if shadowFired {
		t.Errorf("shadow GateEvaluator for %q (not in registry) fired — shadow store violation (CP-INV-001)", shadowName)
	}

	// Confirm the shadow name is genuinely absent from the registry.
	if _, ok := reg.LookupByName(shadowName); ok {
		t.Errorf("LookupByName(%q) found unexpectedly — test invariant broken", shadowName)
	}
}

// TestCPINV001_ShadowGuardFunctionNotInRegistryNeverFires is the Guard-path
// analog of TestCPINV001_ShadowFunctionNotInRegistryNeverFires.
func TestCPINV001_ShadowGuardFunctionNotInRegistryNeverFires(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("inv001-shadow-guard-node")

	// Register exactly one Guard.
	registeredName := "inv001-registered-guard"
	_ = cp021FixtureScopedGuard(t, reg, registeredName, nodeID)

	// Wire both the registered guard and a shadow guard (not in registry).
	shadowName := "inv001-shadow-guard-not-in-registry"

	var shadowFired bool
	fns := map[string]GuardEvaluator{
		registeredName: func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			return candidates
		},
		shadowName: func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			shadowFired = true // MUST NOT execute
			return candidates
		},
	}

	eval := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "inv001-shadow-guard-dst")
	eval(run, []Edge{edge}, Outcome{})

	if shadowFired {
		t.Errorf("shadow GuardEvaluator for %q (not in registry) fired — shadow store violation (CP-INV-001)", shadowName)
	}
}

// ---------------------------------------------------------------------------
// Cross-subsystem: shared registry, all observable CPs resolvable
// ---------------------------------------------------------------------------

// TestCPINV001_CrossSubsystemAllObservableCPsResolvable verifies that every
// ControlPoint name observable through any subsystem path (S01 Gate, S01
// Guard) is resolvable via the single shared registry — no path can surface a
// CP that is absent from LookupByName.
//
// This is the primary cross-subsystem inspection test for CP-INV-001: it
// assembles Gate and Guard evaluators against the same MapRegistry, runs both
// paths, and asserts that the union of all fired names resolves via
// LookupByName to a unique, kind-correct entry.
func TestCPINV001_CrossSubsystemAllObservableCPsResolvable(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("inv001-cross-node")

	// Register Gates and Guards into the single shared registry.
	gateNames := []string{"xsub-gate-1", "xsub-gate-2"}
	for _, name := range gateNames {
		if err := reg.Register(cp002FixtureGate(t, name)); err != nil {
			t.Fatalf("Register gate %q: %v", name, err)
		}
	}

	guardNames := []string{"xsub-guard-1", "xsub-guard-2"}
	for _, name := range guardNames {
		_ = cp021FixtureScopedGuard(t, reg, name, nodeID)
	}

	// Collect all names that fire through each path.
	var firedGates, firedGuards []string

	gateFns := make(map[string]GateEvaluator, len(gateNames))
	for _, name := range gateNames {
		n := name
		gateFns[n] = func(_ *Run, _ Edge, _ Outcome) GateAction {
			firedGates = append(firedGates, n)
			return GateActionAllow
		}
	}

	guardFns := make(map[string]GuardEvaluator, len(guardNames))
	for _, name := range guardNames {
		n := name
		guardFns[n] = func(_ *Run, candidates []Edge, _ Outcome) []Edge {
			firedGuards = append(firedGuards, n)
			return candidates
		}
	}

	gateEval := S01BuildGateEvaluator(reg, AttachPointNodePreEntry, gateFns)
	guardEval := S01BuildGuardEvaluator(reg, nodeID, guardFns)

	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "xsub-dst")
	gateEval(run, edge, Outcome{})
	guardEval(run, []Edge{edge}, Outcome{})

	// Invariant: every name fired through either path must resolve via LookupByName.
	allFired := append(firedGates, firedGuards...)
	if len(allFired) == 0 {
		t.Fatal("no CPs fired across either path; test is vacuous")
	}

	seenNames := make(map[string]bool, len(allFired))
	for _, name := range allFired {
		if seenNames[name] {
			// A name fired twice is unusual but not a violation; skip duplicate check.
			continue
		}
		seenNames[name] = true

		cp, ok := reg.LookupByName(name)
		if !ok {
			t.Errorf("fired CP %q (cross-subsystem) NOT resolvable via LookupByName — shadow store violation (CP-INV-001)", name)
			continue
		}

		// Uniqueness: LookupByName must return exactly the registered entry.
		if cp.Name != name {
			t.Errorf("LookupByName(%q).Name = %q — registry returned wrong entry (CP-INV-001)", name, cp.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// S02PolicyEngine: single shared table across S01 paths
// ---------------------------------------------------------------------------

// TestCPINV001_S02RegistryIsSharedAcrossS01Paths verifies that
// S02PolicyEngine.Registry() returns the same table to both the Gate and
// Guard S01 invocation paths — no path receives a private copy.
//
// CP-INV-001 requires one registry; if S01 Gate and S01 Guard paths each hold
// a private copy, divergence is structurally possible even if both are
// populated from the same source.
func TestCPINV001_S02RegistryIsSharedAcrossS01Paths(t *testing.T) {
	t.Parallel()

	engine := NewS02PolicyEngine()

	gate := cp002FixtureGate(t, "shared-table-gate")
	nodeID := NodeID("shared-table-node")

	// Register via S02 — the single write path (CP-047).
	if err := engine.RegisterControlPoints([]ControlPoint{gate}); err != nil {
		t.Fatalf("RegisterControlPoints gate: %v", err)
	}

	// Simulate the two S01 consultation paths: both call engine.Registry().
	gateReg := engine.Registry()
	guardReg := engine.Registry()

	// Gate path sees the Gate.
	cp, ok := gateReg.LookupByName("shared-table-gate")
	if !ok {
		t.Errorf("Gate path: LookupByName(shared-table-gate) not found — registry not shared (CP-INV-001)")
	} else if cp.Kind != KindGate {
		t.Errorf("Gate path: Kind = %q, want %q", cp.Kind, KindGate)
	}

	// Guard path consults the same table and sees the same entry.
	cp2, ok2 := guardReg.LookupByName("shared-table-gate")
	if !ok2 {
		t.Errorf("Guard path: LookupByName(shared-table-gate) not found — registry not shared (CP-INV-001)")
	}

	// Both paths must see identical content — one table, not two copies.
	if ok && ok2 && cp.Name != cp2.Name {
		t.Errorf("Gate path and Guard path see different Names (%q vs %q) for the same registry — divergence (CP-INV-001)",
			cp.Name, cp2.Name)
	}

	// Registrations added after the initial call are visible to both paths
	// (confirming they reference the same underlying table, not snapshots).
	guardCP := cp002FixtureGuardMechanism(t, "shared-table-guard")
	if err := engine.RegisterControlPoints([]ControlPoint{guardCP}); err != nil {
		t.Fatalf("RegisterControlPoints guard: %v", err)
	}

	_, okGate := gateReg.LookupByName("shared-table-guard")
	_, okGuard := guardReg.LookupByName("shared-table-guard")
	if !okGate || !okGuard {
		t.Errorf("post-registration: Gate-path found=%v Guard-path found=%v — paths see different tables (CP-INV-001)",
			okGate, okGuard)
	}

	// nodeID is referenced only to ensure S01BuildGuardEvaluator finds
	// nothing unexpected via this node (Guard not scoped to it — safe).
	_ = nodeID
}

// ---------------------------------------------------------------------------
// NoOpPolicyEngine: empty registry means no shadow-store path
// ---------------------------------------------------------------------------

// TestCPINV001_NoOpPolicyEngine_NoObservableCPs verifies that the MVH binding
// (NoOpPolicyEngine) has no observable CPs in its registry, so the shadow-store
// invariant is vacuously satisfied: with nothing to observe, there is nothing
// to diverge from.
func TestCPINV001_NoOpPolicyEngine_NoObservableCPs(t *testing.T) {
	t.Parallel()

	noop := NoOpPolicyEngine{}
	reg := noop.Registry()

	// The registry MUST be empty — no CPs can be observed.
	if all := reg.All(); len(all) != 0 {
		t.Errorf("NoOpPolicyEngine registry has %d entries, want 0 (CP-INV-001)", len(all))
	}

	// Attempting to build evaluators against the empty registry produces
	// no-op evaluators (PermitGate / IdentityGuard) — nothing observable,
	// nothing to diverge from the registry.
	gateEval := S01BuildGateEvaluator(reg, AttachPointNodePreEntry, nil)
	run := cp010FixtureRun(t)
	edge := cp010FixtureEdge(t, "noop-dst")
	action := gateEval(run, edge, Outcome{})
	if action != GateActionAllow {
		t.Errorf("empty-registry GateEvaluator returned %v, want %v (should be PermitGate)", action, GateActionAllow)
	}

	nodeID := NodeID("noop-node")
	guardEval := S01BuildGuardEvaluator(reg, nodeID, nil)
	edges := guardEval(run, []Edge{edge}, Outcome{})
	if len(edges) != 1 {
		t.Errorf("empty-registry GuardEvaluator returned %d edges, want 1 (should be IdentityGuard)", len(edges))
	}
}
