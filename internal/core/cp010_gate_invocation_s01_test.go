package core

// cp010_gate_invocation_s01_test.go — Conformance tests for CP-010
//
// specs/control-points.md §4.2.CP-010:
//
//	Gate invocation during the transition cascade MUST be performed by S01
//	(Orchestrator Core) consulting the §4.9 registry. This spec defines the
//	Gate record and its outcome semantics; it does NOT define the invocation
//	mechanics — those belong to the S01 subsystem spec and are constrained by
//	[execution-model.md §4.10].
//
// Also covers CP-007 (declaration order + short-circuit on first non-allow).
//
// Tests:
//  1. Empty registry → S01BuildGateEvaluator returns PermitGate (always allow).
//  2. Gate at matching attach point is applied.
//  3. Gate at a different attach point is NOT applied.
//  4. Multiple gates applied in declaration order.
//  5. Short-circuit on first deny: subsequent gates are not called.
//  6. Short-circuit on escalate-to-human: subsequent gates are not called.
//  7. Non-Gate ControlPoints (Guards, Hooks, Budgets) are ignored.
//  8. Gate with no wired evaluator fn is skipped; does not break the chain.
//  9. S01BuildGateEvaluator result integrates with DispatchEdge (end-to-end).
//
// Refs: hk-a8bg.9

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

func cp010FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cp010"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func cp010FixtureEdge(t *testing.T, toNode NodeID) Edge {
	t.Helper()
	return Edge{FromNode: "node-src", ToNode: toNode, Weight: 1, OrderingKey: "a"}
}

// cp010FixtureGate registers a Gate ControlPoint at the given attachPoint in
// reg and returns the gate's name.
func cp010FixtureGate(t *testing.T, reg *MapRegistry, name string, attachPoint AttachPoint) string {
	t.Helper()
	expr := PolicyExpression("true")
	cp := ControlPoint{
		Name:          name,
		Kind:          KindGate,
		Trigger:       Trigger{Name: string(attachPoint)},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Gate: &GatePayload{
			Subtype:     GateSubtypeGoal,
			AttachPoint: attachPoint,
		}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}
	if err := reg.Register(cp); err != nil {
		t.Fatalf("cp010FixtureGate: Register(%q): %v", name, err)
	}
	return name
}

// cp010RecordingFn returns a GateEvaluator that appends name to called and
// returns GateActionAllow.
func cp010RecordingFn(name string, called *[]string) GateEvaluator {
	return func(_ *Run, _ Edge, _ Outcome) GateAction {
		*called = append(*called, name)
		return GateActionAllow
	}
}

// ── CP-010 §1: empty registry → PermitGate ───────────────────────────────────

// TestCP010_EmptyRegistry_ReturnsPermitGate verifies that when no Gates are
// registered at the given attach point, S01BuildGateEvaluator returns PermitGate:
// the composite evaluator always returns GateActionAllow.
//
// CP-010: S01 consults the §4.9 registry; an empty registry means no gates fire.
func TestCP010_EmptyRegistry_ReturnsPermitGate(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	fns := map[string]GateEvaluator{}
	evaluator := S01BuildGateEvaluator(reg, AttachPointEdgeAfterSelection, fns)

	run := cp010FixtureRun(t)
	chosen := cp010FixtureEdge(t, "node-a")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	action := evaluator(run, chosen, outcome)
	if action != GateActionAllow {
		t.Errorf("CP-010: empty registry evaluator returned %q, want %q", action, GateActionAllow)
	}
}

// ── CP-010 §2: gate at matching attach point is applied ──────────────────────

// TestCP010_GateAtMatchingAttachPoint_Applied verifies that a Gate registered at
// the given attach point is applied (its evaluator is called).
//
// CP-010: S01 consults the registry; Gates matching the attach point must fire.
func TestCP010_GateAtMatchingAttachPoint_Applied(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	name := cp010FixtureGate(t, reg, "gate-post-exit", AttachPointNodePostExit)

	called := false
	fns := map[string]GateEvaluator{
		name: func(_ *Run, _ Edge, _ Outcome) GateAction {
			called = true
			return GateActionAllow
		},
	}

	evaluator := S01BuildGateEvaluator(reg, AttachPointNodePostExit, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	evaluator(run, e, outcome)

	if !called {
		t.Error("CP-010: Gate at matching attach point was not called — S01 must apply it")
	}
}

// ── CP-010 §3: gate at different attach point is NOT applied ─────────────────

// TestCP010_GateAtDifferentAttachPoint_NotApplied verifies that a Gate registered
// at a different attach point is NOT applied.
//
// CP-010: S01 filters by attach point; Gates for other attach points must not fire.
func TestCP010_GateAtDifferentAttachPoint_NotApplied(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	// Register gate at node-pre-entry; build evaluator for edge-after-selection.
	name := cp010FixtureGate(t, reg, "gate-pre-entry", AttachPointNodePreEntry)

	called := false
	fns := map[string]GateEvaluator{
		name: func(_ *Run, _ Edge, _ Outcome) GateAction {
			called = true
			return GateActionAllow
		},
	}

	evaluator := S01BuildGateEvaluator(reg, AttachPointEdgeAfterSelection, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	evaluator(run, e, outcome)

	if called {
		t.Error("CP-010: Gate at different attach point fired — S01 must filter by AttachPoint")
	}
}

// ── CP-010 §4: multiple gates applied in declaration order ───────────────────

// TestCP010_MultipleGates_AppliedInDeclarationOrder verifies that when multiple
// Gates are registered at the same attach point, S01BuildGateEvaluator applies
// them in declaration order (ascending DeclarationIndex per CP-046).
//
// CP-010 + §4.2.CP-007: Gates MUST be applied in declaration order.
func TestCP010_MultipleGates_AppliedInDeclarationOrder(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	ap := AttachPointEdgeAfterSelection

	// Register three gates in order: first, second, third.
	names := []string{"gate-first", "gate-second", "gate-third"}
	for _, n := range names {
		cp010FixtureGate(t, reg, n, ap)
	}

	var callOrder []string
	fns := map[string]GateEvaluator{}
	for _, n := range names {
		n := n
		fns[n] = cp010RecordingFn(n, &callOrder)
	}

	evaluator := S01BuildGateEvaluator(reg, ap, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	evaluator(run, e, outcome)

	if len(callOrder) != len(names) {
		t.Fatalf("CP-010: %d Gates called, want %d", len(callOrder), len(names))
	}
	for i, want := range names {
		if callOrder[i] != want {
			t.Errorf("CP-010: call[%d] = %q, want %q — Gates must fire in declaration order", i, callOrder[i], want)
		}
	}
}

// ── CP-010 §5: short-circuit on deny ─────────────────────────────────────────

// TestCP010_ShortCircuit_OnDeny verifies that the composite evaluator stops at
// the first Gate that returns GateActionDeny and does NOT call subsequent Gates.
//
// CP-007: S01 MUST short-circuit on the first non-allow verdict.
func TestCP010_ShortCircuit_OnDeny(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	ap := AttachPointEdgeAfterSelection

	first := cp010FixtureGate(t, reg, "gate-deny-first", ap)
	second := cp010FixtureGate(t, reg, "gate-second-after-deny", ap)

	secondCalled := false
	fns := map[string]GateEvaluator{
		first: func(_ *Run, _ Edge, _ Outcome) GateAction {
			return GateActionDeny
		},
		second: func(_ *Run, _ Edge, _ Outcome) GateAction {
			secondCalled = true
			return GateActionAllow
		},
	}

	evaluator := S01BuildGateEvaluator(reg, ap, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	action := evaluator(run, e, outcome)

	if action != GateActionDeny {
		t.Errorf("CP-010: composite returned %q, want %q", action, GateActionDeny)
	}
	if secondCalled {
		t.Error("CP-010: second Gate was called after first Gate denied — S01 must short-circuit on deny")
	}
}

// ── CP-010 §6: short-circuit on escalate-to-human ────────────────────────────

// TestCP010_ShortCircuit_OnEscalate verifies that the composite evaluator stops
// at the first Gate returning GateActionEscalateToHuman.
//
// CP-007: S01 MUST short-circuit on the first non-allow verdict.
func TestCP010_ShortCircuit_OnEscalate(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	ap := AttachPointNodePostExit

	first := cp010FixtureGate(t, reg, "gate-escalate-first", ap)
	second := cp010FixtureGate(t, reg, "gate-second-after-escalate", ap)

	secondCalled := false
	fns := map[string]GateEvaluator{
		first: func(_ *Run, _ Edge, _ Outcome) GateAction {
			return GateActionEscalateToHuman
		},
		second: func(_ *Run, _ Edge, _ Outcome) GateAction {
			secondCalled = true
			return GateActionAllow
		},
	}

	evaluator := S01BuildGateEvaluator(reg, ap, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	action := evaluator(run, e, outcome)

	if action != GateActionEscalateToHuman {
		t.Errorf("CP-010: composite returned %q, want %q", action, GateActionEscalateToHuman)
	}
	if secondCalled {
		t.Error("CP-010: second Gate was called after escalation — S01 must short-circuit on escalate-to-human")
	}
}

// ── CP-010 §7: non-Gate ControlPoints are ignored ────────────────────────────

// TestCP010_NonGateControlPoints_Ignored verifies that Guard, Hook, and Budget
// ControlPoints in the registry are not applied as Gates at the given attach point.
//
// CP-010: S01 consults the registry for Gate ControlPoints only.
func TestCP010_NonGateControlPoints_Ignored(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	ap := AttachPointEdgeAfterSelection

	// Register one Gate plus a Guard, Hook, and Budget.
	gateName := cp010FixtureGate(t, reg, "the-gate", ap)

	guard := cp002FixtureGuardMechanism(t, "the-guard")
	hook := cp002FixtureHook(t, "the-hook")
	budget := cp002FixtureBudget(t, "the-budget")
	for _, cp := range []ControlPoint{guard, hook, budget} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register %q: %v", cp.Name, err)
		}
	}

	var called []string
	fns := map[string]GateEvaluator{
		gateName:     cp010RecordingFn(gateName, &called),
		"the-guard":  cp010RecordingFn("the-guard", &called),
		"the-hook":   cp010RecordingFn("the-hook", &called),
		"the-budget": cp010RecordingFn("the-budget", &called),
	}

	evaluator := S01BuildGateEvaluator(reg, ap, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	evaluator(run, e, outcome)

	if len(called) != 1 || called[0] != gateName {
		t.Errorf("CP-010: called = %v, want [%q] — only Gate ControlPoints at the attach point may be applied", called, gateName)
	}
}

// ── CP-010 §8: gate with no wired fn is skipped ──────────────────────────────

// TestCP010_UnwiredGate_Skipped verifies that a Gate registered in the registry
// but absent from the fns map is skipped without error.
//
// This prevents a missing evaluator wiring from breaking the cascade.
func TestCP010_UnwiredGate_Skipped(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	ap := AttachPointNodePreEntry

	// Register two Gates; only provide a fn for the second.
	cp010FixtureGate(t, reg, "gate-unwired", ap)
	wiredName := cp010FixtureGate(t, reg, "gate-wired", ap)

	wiredCalled := false
	fns := map[string]GateEvaluator{
		wiredName: func(_ *Run, _ Edge, _ Outcome) GateAction {
			wiredCalled = true
			return GateActionAllow
		},
		// "gate-unwired" intentionally absent.
	}

	evaluator := S01BuildGateEvaluator(reg, ap, fns)
	run := cp010FixtureRun(t)
	e := cp010FixtureEdge(t, "node-dst")
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	// Must not panic.
	action := evaluator(run, e, outcome)

	if !wiredCalled {
		t.Error("CP-010: wired Gate not called when an unwired Gate precedes it")
	}
	if action != GateActionAllow {
		t.Errorf("CP-010: expected GateActionAllow, got %q", action)
	}
}

// ── CP-010 §9: end-to-end integration with DispatchEdge ─────────────────────

// TestCP010_DispatchEdge_UsesRegistryGate verifies the end-to-end path:
// S01BuildGateEvaluator produces a GateEvaluator that DispatchEdge applies
// correctly, honoring the Gate's deny verdict.
//
// CP-010: S01 must invoke Gates through the registry path, not directly.
func TestCP010_DispatchEdge_UsesRegistryGate(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	ap := AttachPointEdgeAfterSelection
	name := cp010FixtureGate(t, reg, "e2e-gate", ap)

	// Gate denies the transition.
	fns := map[string]GateEvaluator{
		name: func(_ *Run, _ Edge, _ Outcome) GateAction {
			return GateActionDeny
		},
	}

	gate := S01BuildGateEvaluator(reg, ap, fns)

	run := cp010FixtureRun(t)
	eA := Edge{FromNode: "node-src", ToNode: "node-a", Weight: 5, OrderingKey: "a"}
	eB := Edge{FromNode: "node-src", ToNode: "node-b", Weight: 3, OrderingKey: "b"}
	candidates := []Edge{eA, eB}

	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	eval := func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, eval, cycles, IdentityGuard, gate)

	if result.Advance {
		t.Error("CP-010: DispatchEdge advanced despite gate deny — registry Gate must block the transition")
	}
	if !result.Stay {
		t.Errorf("CP-010: DispatchEdge.Stay = false, want true on gate deny (got Advance=%v Escalate=%v Failed=%v)",
			result.Advance, result.Escalate, result.Failed)
	}
}
