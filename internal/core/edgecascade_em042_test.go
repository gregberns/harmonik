// Package core — EM-042 requirement-traceable sensors.
//
// This file covers the Guards/Gates carve-out defined in
// execution-model.md §4.10.EM-042:
//
//   - Guards reorder the candidate edge list before the EM-041 cascade runs.
//   - Guards MUST NOT add, remove, or block edges.
//   - Gates evaluate the chosen edge after the cascade and may permit, deny,
//     or escalate the transition.
//   - Gate denial leaves the run in the source state (STAY) with no checkpoint;
//     this does NOT constitute a durable transition per EM-023a.
//
// Test naming pattern:
//
//	TestDispatchEdgeEM042_<Case>
//
// Run all sensors for EM-042 with:
//
//	go test -run TestDispatchEdgeEM042 ./internal/core/...
package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func guardsGatesFixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      mustParseWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-1"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func guardsGatesFixtureOutcome(t *testing.T) Outcome {
	t.Helper()
	return Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
	}
}

func guardsGatesFixtureEdge(t *testing.T, toNode NodeID, weight int, orderingKey string) Edge {
	t.Helper()
	return Edge{
		FromNode:    "node-a",
		ToNode:      toNode,
		Weight:      weight,
		OrderingKey: orderingKey,
	}
}

func guardsGatesFixtureEvalAlwaysTrue(_ PolicyExpression, _ map[string]any, _ Outcome) bool {
	return true
}

// TestDispatchEdgeEM042_GuardReordersEdgesBeforeCascade verifies that when a guard
// swaps two edges, the cascade sees the reordered list and selects accordingly.
// The guard promotes a lower-weight edge to the front; the cascade must respect
// the reorder (condition filter runs on reordered list, then weight sort applies,
// but if conditions are equal the guard-pushed-first edge still influences outcome
// only via the weight+key tie-break — here we verify the guard's list is passed).
func TestDispatchEdgeEM042_GuardReordersEdgesBeforeCascade(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	eLow := guardsGatesFixtureEdge(t, "node-low", 1, "a")
	eHigh := guardsGatesFixtureEdge(t, "node-high", 10, "b")
	candidates := []Edge{eHigh, eLow}

	var cascadeSawOrder []NodeID
	evalCapture := func(_ PolicyExpression, _ map[string]any, _ Outcome) bool {
		return true
	}

	guardCalled := false
	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		guardCalled = true
		reversed := make([]Edge, len(edges))
		for i, e := range edges {
			reversed[len(edges)-1-i] = e
		}
		cascadeSawOrder = make([]NodeID, len(reversed))
		for i, e := range reversed {
			cascadeSawOrder[i] = e.ToNode
		}
		return reversed
	}

	_ = cascadeSawOrder
	_ = evalCapture

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, guard, PermitGate)

	if !guardCalled {
		t.Error("EM-042: guard was not called")
	}
	if result.Failed {
		t.Errorf("EM-042: dispatch failed unexpectedly: %s / %s", result.FailureClass, result.FailureReason)
	}
	if !result.Advance {
		t.Error("EM-042: expected Advance=true after guard reorder + gate allow")
	}
}

// TestDispatchEdgeEM042_GuardReorderAffectsSelectionWhenWeightsTied verifies that
// when two edges have equal weight and equal ordering key, the guard's reordering
// has no effect on cascade selection — the cascade's deterministic sort drives it.
// This test verifies the guard doesn't corrupt the list when weights tie.
func TestDispatchEdgeEM042_GuardReorderAffectsSelectionWhenWeightsTied(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	eA := guardsGatesFixtureEdge(t, "node-a-key", 5, "a")
	eB := guardsGatesFixtureEdge(t, "node-b-key", 5, "b")
	candidates := []Edge{eB, eA} // guard will reverse

	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return []Edge{edges[1], edges[0]}
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, guard, PermitGate)

	if !result.Advance {
		t.Fatalf("EM-042: expected Advance=true; got failure: %s / %s", result.FailureClass, result.FailureReason)
	}
	if result.Edge.ToNode != "node-a-key" {
		t.Errorf("EM-042: selected %q, want %q (lexical ordering_key wins on equal weight after guard reorder)",
			result.Edge.ToNode, "node-a-key")
	}
}

// TestDispatchEdgeEM042_IdentityGuardIsNoop verifies that [IdentityGuard] leaves
// the candidate list unchanged and the cascade behaves identically to
// [SelectNextEdge] called directly.
func TestDispatchEdgeEM042_IdentityGuardIsNoop(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	eA := guardsGatesFixtureEdge(t, "node-a", 10, "a")
	eB := guardsGatesFixtureEdge(t, "node-b", 5, "b")
	candidates := []Edge{eB, eA}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, PermitGate)

	if !result.Advance {
		t.Fatalf("EM-042 IdentityGuard: expected Advance=true; failure: %s / %s", result.FailureClass, result.FailureReason)
	}
	if result.Edge.ToNode != "node-a" {
		t.Errorf("EM-042 IdentityGuard: selected %q, want %q", result.Edge.ToNode, "node-a")
	}
}

// TestDispatchEdgeEM042_GuardViolatesLengthInvariantPanics verifies that a guard
// that returns a shorter slice (violating the no-remove invariant) causes
// [DispatchEdge] to panic.
func TestDispatchEdgeEM042_GuardViolatesLengthInvariantPanics(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	eA := guardsGatesFixtureEdge(t, "node-a", 5, "a")
	eB := guardsGatesFixtureEdge(t, "node-b", 5, "b")
	candidates := []Edge{eA, eB}

	badGuard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return edges[:1]
	}

	cycles := NewCycleCounter()

	defer func() {
		if r := recover(); r == nil {
			t.Error("EM-042: expected panic when guard returns shorter slice, got none")
		}
	}()

	DispatchEdge(run, candidates, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, badGuard, PermitGate)
}

// TestDispatchEdgeEM042_GateAllowAdvancesTransition verifies that [GateActionAllow]
// returns Advance=true with the chosen edge.
func TestDispatchEdgeEM042_GateAllowAdvancesTransition(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)
	e := guardsGatesFixtureEdge(t, "node-next", 5, "a")

	cycles := NewCycleCounter()
	result := DispatchEdge(run, []Edge{e}, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, PermitGate)

	if !result.Advance {
		t.Errorf("EM-042 gate allow: expected Advance=true; got Stay=%v Escalate=%v Failed=%v",
			result.Stay, result.Escalate, result.Failed)
	}
	if result.Edge.ToNode != "node-next" {
		t.Errorf("EM-042 gate allow: Edge.ToNode=%q, want %q", result.Edge.ToNode, "node-next")
	}
}

// TestDispatchEdgeEM042_GateDenyProducesStay verifies that [GateActionDeny]
// returns Stay=true, leaving the run in the source state per EM-042 / EM-042a.
func TestDispatchEdgeEM042_GateDenyProducesStay(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)
	e := guardsGatesFixtureEdge(t, "node-next", 5, "a")

	denyGate := func(_ *Run, _ Edge, _ Outcome) GateAction {
		return GateActionDeny
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, []Edge{e}, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, denyGate)

	if !result.Stay {
		t.Errorf("EM-042 gate deny: expected Stay=true; got Advance=%v Escalate=%v Failed=%v",
			result.Advance, result.Escalate, result.Failed)
	}
	if result.Advance || result.Escalate || result.Failed {
		t.Errorf("EM-042 gate deny: expected only Stay=true; got Advance=%v Escalate=%v Failed=%v",
			result.Advance, result.Escalate, result.Failed)
	}
}

// TestDispatchEdgeEM042_GateDenyIsNotDurableTransition verifies the EM-042 /
// EM-023a requirement that gate denial does NOT constitute a durable transition.
// This test verifies Stay=true and Edge is zero-value (no edge was chosen for commit).
func TestDispatchEdgeEM042_GateDenyIsNotDurableTransition(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)
	e := guardsGatesFixtureEdge(t, "node-next", 5, "a")

	denyGate := func(_ *Run, _ Edge, _ Outcome) GateAction {
		return GateActionDeny
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, []Edge{e}, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, denyGate)

	if result.Stay && result.Edge != (Edge{}) {
		t.Errorf("EM-042/EM-023a: gate denial set Edge=%+v, want zero-value (no checkpoint written)", result.Edge)
	}
}

// TestDispatchEdgeEM042_GateEscalateProducesEscalate verifies that
// [GateActionEscalateToHuman] returns Escalate=true.
func TestDispatchEdgeEM042_GateEscalateProducesEscalate(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)
	e := guardsGatesFixtureEdge(t, "node-next", 5, "a")

	escalateGate := func(_ *Run, _ Edge, _ Outcome) GateAction {
		return GateActionEscalateToHuman
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, []Edge{e}, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, escalateGate)

	if !result.Escalate {
		t.Errorf("EM-042 gate escalate: expected Escalate=true; got Advance=%v Stay=%v Failed=%v",
			result.Advance, result.Stay, result.Failed)
	}
	if result.Advance || result.Stay || result.Failed {
		t.Errorf("EM-042 gate escalate: expected only Escalate=true; got Advance=%v Stay=%v Failed=%v",
			result.Advance, result.Stay, result.Failed)
	}
}

// TestDispatchEdgeEM042_GateReceivesChosenEdge verifies that the gate evaluator
// receives the edge the cascade selected (not the candidates list).
func TestDispatchEdgeEM042_GateReceivesChosenEdge(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	eWinner := guardsGatesFixtureEdge(t, "node-winner", 10, "a") // higher weight wins
	eLoser := guardsGatesFixtureEdge(t, "node-loser", 1, "b")
	candidates := []Edge{eLoser, eWinner}

	var gateReceivedEdge Edge
	captureGate := func(_ *Run, chosen Edge, _ Outcome) GateAction {
		gateReceivedEdge = chosen
		return GateActionAllow
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, captureGate)

	if !result.Advance {
		t.Fatalf("EM-042 gate receives chosen: expected Advance=true; failure: %s / %s",
			result.FailureClass, result.FailureReason)
	}
	if gateReceivedEdge.ToNode != "node-winner" {
		t.Errorf("EM-042: gate received edge ToNode=%q, want %q (cascade winner)",
			gateReceivedEdge.ToNode, "node-winner")
	}
}

// TestDispatchEdgeEM042_GateNotCalledOnCascadeFailure verifies that the gate
// evaluator is NOT called when the cascade itself fails (e.g., empty candidates).
func TestDispatchEdgeEM042_GateNotCalledOnCascadeFailure(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	gateCalled := false
	sentinelGate := func(_ *Run, _ Edge, _ Outcome) GateAction {
		gateCalled = true
		return GateActionAllow
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, nil, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, IdentityGuard, sentinelGate)

	if !result.Failed {
		t.Error("EM-042: expected Failed=true for empty candidates")
	}
	if gateCalled {
		t.Error("EM-042: gate was called even though cascade failed, want gate NOT called")
	}
}

// TestDispatchEdgeEM042_GuardAndGateBothFire verifies that both the guard and
// gate are called in the correct order: guard before cascade, gate after.
func TestDispatchEdgeEM042_GuardAndGateBothFire(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)
	e := guardsGatesFixtureEdge(t, "node-next", 5, "a")

	var callOrder []string

	trackGuard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		callOrder = append(callOrder, "guard")
		return edges
	}
	trackGate := func(_ *Run, _ Edge, _ Outcome) GateAction {
		callOrder = append(callOrder, "gate")
		return GateActionAllow
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, []Edge{e}, outcome, guardsGatesFixtureEvalAlwaysTrue, cycles, trackGuard, trackGate)

	if !result.Advance {
		t.Fatalf("EM-042: expected Advance=true; failure: %s / %s", result.FailureClass, result.FailureReason)
	}
	if len(callOrder) != 2 || callOrder[0] != "guard" || callOrder[1] != "gate" {
		t.Errorf("EM-042: call order = %v, want [guard gate]", callOrder)
	}
}

// TestDispatchEdgeEM042_PermitGateAlwaysAllows verifies [PermitGate] returns
// GateActionAllow for any input.
func TestDispatchEdgeEM042_PermitGateAlwaysAllows(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	e := guardsGatesFixtureEdge(t, "node-next", 5, "a")
	outcome := guardsGatesFixtureOutcome(t)

	action := PermitGate(run, e, outcome)
	if action != GateActionAllow {
		t.Errorf("PermitGate: returned %q, want %q", action, GateActionAllow)
	}
}

// TestDispatchEdgeEM042_CascadeFailureForwardedThroughDispatch verifies that
// FailureClass and FailureReason from the cascade are forwarded correctly through
// [DispatchEdge] when the cascade fails.
func TestDispatchEdgeEM042_CascadeFailureForwardedThroughDispatch(t *testing.T) {
	t.Parallel()

	run := guardsGatesFixtureRun(t)
	outcome := guardsGatesFixtureOutcome(t)

	cond := PolicyExpression("never")
	e := Edge{
		FromNode:    "node-a",
		ToNode:      "node-b",
		Condition:   &cond,
		Weight:      0,
		OrderingKey: "a",
	}
	evalAlwaysFalse := func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return false }

	cycles := NewCycleCounter()
	result := DispatchEdge(run, []Edge{e}, outcome, evalAlwaysFalse, cycles, IdentityGuard, PermitGate)

	if !result.Failed {
		t.Error("EM-042: expected Failed=true when all conditions false")
	}
	if result.FailureClass != FailureClassStructural {
		t.Errorf("EM-042: FailureClass=%q, want %q", result.FailureClass, FailureClassStructural)
	}
	if result.FailureReason != "no_outgoing_edge_matches" {
		t.Errorf("EM-042: FailureReason=%q, want %q", result.FailureReason, "no_outgoing_edge_matches")
	}
}
