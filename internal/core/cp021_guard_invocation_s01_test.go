package core

// cp021_guard_invocation_s01_test.go — Conformance tests for CP-021
//
// specs/control-points.md §4.4.CP-021:
//
//	Guard invocation during the cascade MUST be performed by S01 (Orchestrator
//	Core) consulting the §4.9 registry. This spec defines the Guard record and
//	its outcome semantics; invocation is S01's obligation under
//	[execution-model.md §4.10].
//
// Tests:
//  1. Empty registry → S01BuildGuardEvaluator returns IdentityGuard (no-op).
//  2. Guard scoped to the current node is applied.
//  3. Guard scoped to a different node is NOT applied.
//  4. Guard with nil scope (applies to all nodes) is applied to every node.
//  5. Multiple Guards applied in declaration order.
//  6. Non-Guard ControlPoints (Gates, Hooks, Budgets) are ignored.
//  7. Guard with no wired evaluator fn is skipped; does not break the chain.
//  8. Edge threading: each Guard receives the output of the previous Guard.
//  9. S01BuildGuardEvaluator result integrates with DispatchEdge (end-to-end).
//
// Refs: hk-a8bg.20

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

func cp021FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cp021"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func cp021FixtureEdge(t *testing.T, toNode NodeID) Edge {
	t.Helper()
	return Edge{FromNode: "node-src", ToNode: toNode, Weight: 1, OrderingKey: "a"}
}

// cp021FixtureGuard registers a Guard ControlPoint scoped to appliesToNode
// (nil = all nodes) in reg and returns the guard's name.
func cp021FixtureGuard(t *testing.T, reg *MapRegistry, name string, appliesToNode *NodeID) string {
	t.Helper()
	expr := PolicyExpression("true")
	gp := &GuardPayload{AppliesToNode: appliesToNode}
	cp := ControlPoint{
		Name:          name,
		Kind:          KindGuard,
		Trigger:       Trigger{Name: ""},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionReorder,
		Payload:       KindPayload{Guard: gp},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}
	if err := reg.Register(cp); err != nil {
		t.Fatalf("cp021FixtureGuard: Register(%q): %v", name, err)
	}
	return name
}

// cp021FixtureScopedGuard returns a node scoped to a specific NodeID.
func cp021FixtureScopedGuard(t *testing.T, reg *MapRegistry, name string, nodeID NodeID) string {
	t.Helper()
	return cp021FixtureGuard(t, reg, name, &nodeID)
}

// cp021FixtureGlobalGuard returns a guard scoped to all nodes (nil AppliesToNode).
func cp021FixtureGlobalGuard(t *testing.T, reg *MapRegistry, name string) string {
	t.Helper()
	return cp021FixtureGuard(t, reg, name, nil)
}

// cp021RecordingFn returns a GuardEvaluator that appends name to called and
// returns the edge slice unchanged (identity behaviour for tracking purposes).
func cp021RecordingFn(name string, called *[]string) GuardEvaluator {
	return func(_ *Run, edges []Edge, _ Outcome) []Edge {
		*called = append(*called, name)
		return edges
	}
}

// ── CP-021 §1: empty registry → IdentityGuard ────────────────────────────────

// TestCP021_EmptyRegistry_ReturnsIdentityGuard verifies that when no Guards are
// registered, S01BuildGuardEvaluator returns IdentityGuard: the composite
// evaluator passes candidates through unchanged.
//
// CP-021: S01 consults the §4.9 registry; an empty registry means no guards fire.
func TestCP021_EmptyRegistry_ReturnsIdentityGuard(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	fns := map[string]GuardEvaluator{}
	evaluator := S01BuildGuardEvaluator(reg, "any-node", fns)

	run := cp021FixtureRun(t)
	eA := cp021FixtureEdge(t, "node-a")
	eB := cp021FixtureEdge(t, "node-b")
	eB.OrderingKey = "b"
	candidates := []Edge{eA, eB}

	result := evaluator(run, candidates, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})
	if len(result) != len(candidates) {
		t.Fatalf("CP-021: empty registry evaluator returned %d edges, want %d", len(result), len(candidates))
	}
	for i, e := range result {
		if e.ToNode != candidates[i].ToNode {
			t.Errorf("CP-021: edge[%d].ToNode = %q, want %q — IdentityGuard must not reorder", i, e.ToNode, candidates[i].ToNode)
		}
	}
}

// ── CP-021 §2: guard scoped to current node is applied ───────────────────────

// TestCP021_ScopedGuard_AppliedToMatchingNode verifies that a Guard scoped to
// the current node is applied (its evaluator is called).
//
// CP-021: S01 consults the registry; scoped Guards matching the node must fire.
func TestCP021_ScopedGuard_AppliedToMatchingNode(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-target")
	name := cp021FixtureScopedGuard(t, reg, "scoped-guard", nodeID)

	called := false
	fns := map[string]GuardEvaluator{
		name: func(_ *Run, edges []Edge, _ Outcome) []Edge {
			called = true
			return edges
		},
	}

	evaluator := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp021FixtureRun(t)
	e := cp021FixtureEdge(t, "node-dst")

	evaluator(run, []Edge{e}, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

	if !called {
		t.Error("CP-021: scoped Guard was not called for matching node — S01 must apply it")
	}
}

// ── CP-021 §3: guard scoped to different node is NOT applied ─────────────────

// TestCP021_ScopedGuard_NotAppliedToDifferentNode verifies that a Guard scoped
// to a different node is NOT applied when S01BuildGuardEvaluator is called for
// a different nodeID.
//
// CP-021: S01 filters by node scope; Guards for other nodes must not fire.
func TestCP021_ScopedGuard_NotAppliedToDifferentNode(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	otherNode := NodeID("node-other")
	currentNode := NodeID("node-current")
	name := cp021FixtureScopedGuard(t, reg, "scoped-other-guard", otherNode)

	called := false
	fns := map[string]GuardEvaluator{
		name: func(_ *Run, edges []Edge, _ Outcome) []Edge {
			called = true
			return edges
		},
	}

	// Build evaluator for currentNode — the guard is scoped to otherNode.
	evaluator := S01BuildGuardEvaluator(reg, currentNode, fns)
	run := cp021FixtureRun(t)
	e := cp021FixtureEdge(t, "node-dst")

	evaluator(run, []Edge{e}, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

	if called {
		t.Error("CP-021: scoped Guard fired for a different node — S01 must filter by AppliesToNode scope")
	}
}

// ── CP-021 §4: nil-scope guard applies to all nodes ──────────────────────────

// TestCP021_GlobalGuard_AppliesToAllNodes verifies that a Guard with
// AppliesToNode=nil applies to any nodeID passed to S01BuildGuardEvaluator.
//
// CP-021: a nil-scope Guard fires for every node in the cascade.
func TestCP021_GlobalGuard_AppliesToAllNodes(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	name := cp021FixtureGlobalGuard(t, reg, "global-guard")

	nodeIDs := []NodeID{"node-a", "node-b", "node-c", "some-other-node"}
	for _, nid := range nodeIDs {
		nid := nid
		t.Run(string(nid), func(t *testing.T) {
			t.Parallel()

			called := false
			fns := map[string]GuardEvaluator{
				name: func(_ *Run, edges []Edge, _ Outcome) []Edge {
					called = true
					return edges
				},
			}

			evaluator := S01BuildGuardEvaluator(reg, nid, fns)
			run := cp021FixtureRun(t)
			e := cp021FixtureEdge(t, "node-dst")

			evaluator(run, []Edge{e}, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

			if !called {
				t.Errorf("CP-021: global Guard not called for node %q — nil AppliesToNode must apply to all nodes", nid)
			}
		})
	}
}

// ── CP-021 §5: multiple guards applied in declaration order ──────────────────

// TestCP021_MultipleGuards_AppliedInDeclarationOrder verifies that when
// multiple Guards are registered, S01BuildGuardEvaluator applies them in
// declaration order (ascending DeclarationIndex, which reflects registration
// order in MapRegistry).
//
// CP-021 + §4.9.CP-046: Guards MUST be applied in declaration order.
func TestCP021_MultipleGuards_AppliedInDeclarationOrder(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-multi")

	// Register three guards in order: first, second, third.
	// DeclarationIndex is assigned by MapRegistry in registration order.
	names := []string{"guard-first", "guard-second", "guard-third"}
	for _, n := range names {
		cp021FixtureScopedGuard(t, reg, n, nodeID)
	}

	var callOrder []string
	fns := map[string]GuardEvaluator{}
	for _, n := range names {
		n := n
		fns[n] = cp021RecordingFn(n, &callOrder)
	}

	evaluator := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp021FixtureRun(t)
	e := cp021FixtureEdge(t, "node-dst")

	evaluator(run, []Edge{e}, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

	if len(callOrder) != len(names) {
		t.Fatalf("CP-021: %d Guards called, want %d", len(callOrder), len(names))
	}
	for i, want := range names {
		if callOrder[i] != want {
			t.Errorf("CP-021: call[%d] = %q, want %q — Guards must fire in declaration order", i, callOrder[i], want)
		}
	}
}

// ── CP-021 §6: non-Guard ControlPoints are ignored ───────────────────────────

// TestCP021_NonGuardControlPoints_Ignored verifies that Gate, Hook, and Budget
// ControlPoints in the registry are not applied as Guards.
//
// CP-021: S01 consults the registry for Guard ControlPoints only.
func TestCP021_NonGuardControlPoints_Ignored(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-mix")

	// Register one Guard (scoped to nodeID) plus a Gate, Hook, and Budget.
	guardName := cp021FixtureScopedGuard(t, reg, "the-guard", nodeID)
	gate := cp002FixtureGate(t, "the-gate")
	hook := cp002FixtureHook(t, "the-hook")
	budget := cp002FixtureBudget(t, "the-budget")
	for _, cp := range []ControlPoint{gate, hook, budget} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register %q: %v", cp.Name, err)
		}
	}

	var called []string
	// Provide evaluators for ALL names; only guardName should be called.
	fns := map[string]GuardEvaluator{
		guardName:   cp021RecordingFn(guardName, &called),
		"the-gate":  cp021RecordingFn("the-gate", &called),
		"the-hook":  cp021RecordingFn("the-hook", &called),
		"the-budget": cp021RecordingFn("the-budget", &called),
	}

	evaluator := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp021FixtureRun(t)
	e := cp021FixtureEdge(t, "node-dst")

	evaluator(run, []Edge{e}, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

	if len(called) != 1 || called[0] != guardName {
		t.Errorf("CP-021: called = %v, want [%q] — only Guard ControlPoints may be applied", called, guardName)
	}
}

// ── CP-021 §7: guard with no wired fn is skipped ─────────────────────────────

// TestCP021_UnwiredGuard_Skipped verifies that a Guard ControlPoint registered
// in the registry but absent from the fns map is skipped without error.
//
// This prevents a missing evaluator wiring from breaking the cascade.
func TestCP021_UnwiredGuard_Skipped(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-unwired")

	// Register two Guards; only provide a fn for the second.
	cp021FixtureScopedGuard(t, reg, "guard-unwired", nodeID)
	wiredName := cp021FixtureScopedGuard(t, reg, "guard-wired", nodeID)

	wiredCalled := false
	fns := map[string]GuardEvaluator{
		wiredName: func(_ *Run, edges []Edge, _ Outcome) []Edge {
			wiredCalled = true
			return edges
		},
		// "guard-unwired" intentionally absent.
	}

	evaluator := S01BuildGuardEvaluator(reg, nodeID, fns)
	run := cp021FixtureRun(t)
	e := cp021FixtureEdge(t, "node-dst")

	// Must not panic.
	result := evaluator(run, []Edge{e}, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

	if !wiredCalled {
		t.Error("CP-021: wired Guard not called when an unwired Guard precedes it")
	}
	if len(result) != 1 {
		t.Errorf("CP-021: edge count = %d, want 1", len(result))
	}
}

// ── CP-021 §8: edge threading through the chain ──────────────────────────────

// TestCP021_EdgeThreadingThroughChain verifies that each Guard in the chain
// receives the edge slice returned by the previous Guard, not the original
// candidates slice.
//
// CP-021 composite semantics: Guards are chained; each sees the prior output.
func TestCP021_EdgeThreadingThroughChain(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-chain")

	first := cp021FixtureScopedGuard(t, reg, "guard-first-chain", nodeID)
	second := cp021FixtureScopedGuard(t, reg, "guard-second-chain", nodeID)

	// firstGuard reverses the edge list.
	// secondGuard records what it received (should be the reversed list).
	var secondInput []Edge

	fns := map[string]GuardEvaluator{
		first: func(_ *Run, edges []Edge, _ Outcome) []Edge {
			reversed := make([]Edge, len(edges))
			for i, e := range edges {
				reversed[len(edges)-1-i] = e
			}
			return reversed
		},
		second: func(_ *Run, edges []Edge, _ Outcome) []Edge {
			secondInput = make([]Edge, len(edges))
			copy(secondInput, edges)
			return edges
		},
	}

	run := cp021FixtureRun(t)
	eA := cp021FixtureEdge(t, "node-a")
	eB := cp021FixtureEdge(t, "node-b")
	eB.OrderingKey = "b"
	eC := cp021FixtureEdge(t, "node-c")
	eC.OrderingKey = "c"
	candidates := []Edge{eA, eB, eC}

	evaluator := S01BuildGuardEvaluator(reg, nodeID, fns)
	evaluator(run, candidates, Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault})

	// secondGuard must see [C, B, A] (the reversed output of firstGuard).
	wantOrder := []NodeID{"node-c", "node-b", "node-a"}
	if len(secondInput) != len(wantOrder) {
		t.Fatalf("CP-021: secondGuard received %d edges, want %d", len(secondInput), len(wantOrder))
	}
	for i, want := range wantOrder {
		if secondInput[i].ToNode != want {
			t.Errorf("CP-021: secondGuard edge[%d].ToNode = %q, want %q — edge threading must pass first guard's output to second", i, secondInput[i].ToNode, want)
		}
	}
}

// ── CP-021 §9: end-to-end integration with DispatchEdge ─────────────────────

// TestCP021_DispatchEdge_UsesRegistryGuard verifies the end-to-end path:
// S01BuildGuardEvaluator produces a GuardEvaluator that DispatchEdge applies
// correctly, selecting the edge favoured by the Guard's reordering.
//
// CP-021: S01 must invoke Guards through the registry path, not directly.
func TestCP021_DispatchEdge_UsesRegistryGuard(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	nodeID := NodeID("node-e2e")
	name := cp021FixtureScopedGuard(t, reg, "e2e-guard", nodeID)

	// Guard reverses the candidates so node-b comes first.
	fns := map[string]GuardEvaluator{
		name: func(_ *Run, edges []Edge, _ Outcome) []Edge {
			reversed := make([]Edge, len(edges))
			for i, e := range edges {
				reversed[len(edges)-1-i] = e
			}
			return reversed
		},
	}

	guard := S01BuildGuardEvaluator(reg, nodeID, fns)

	run := cp021FixtureRun(t)
	// Two equal-weight edges; guard reversal means node-b should be selected first.
	eA := Edge{FromNode: "node-src", ToNode: "node-a", Weight: 5, OrderingKey: "a"}
	eB := Edge{FromNode: "node-src", ToNode: "node-b", Weight: 5, OrderingKey: "a"}
	candidates := []Edge{eA, eB}

	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	eval := func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, eval, cycles, guard, PermitGate)

	if !result.Advance {
		t.Fatalf("CP-021: DispatchEdge did not advance: %s / %s", result.FailureClass, result.FailureReason)
	}
	// Guard reversed [A, B] to [B, A]; equal weight+key → stable → B wins.
	if result.Edge.ToNode != "node-b" {
		t.Errorf("CP-021: selected %q, want %q — registry Guard must determine edge ordering", result.Edge.ToNode, "node-b")
	}
}
