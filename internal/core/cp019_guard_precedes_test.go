package core

// cp019_guard_precedes_test.go — Conformance tests for CP-019
//
// specs/control-points.md §4.4.CP-019:
//
//	In the edge-selection cascade of [execution-model.md §4.10 EM-042], Guards
//	MUST run BEFORE the condition cascade. The cascade then operates on the
//	Guard-reordered edge list as its input; subsequent precedence rules
//	(condition match, preferred_label, suggested_next_ids, weight, ordering_key)
//	are applied in the order defined by the execution-model spec.
//
// Tests:
//  1. Guard fires before the condition evaluator is called.
//  2. Condition evaluator receives edges in the guard-reordered order.
//  3. Guard-reordered order is preserved through the condition filter step for
//     equal-weight, equal-ordering-key edges (stable sort).
//  4. preferred_label hint is applied downstream of guard reordering.
//  5. suggested_next_ids hint is applied downstream of guard reordering.
//
// Refs: hk-a8bg.18

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

func cp019FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cp019"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func cp019FixtureEdge(t *testing.T, toNode NodeID, weight int, key string) Edge {
	t.Helper()
	return Edge{FromNode: "node-src", ToNode: toNode, Weight: weight, OrderingKey: key}
}

func cp019EvalAlwaysTrue(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }

// ── CP-019 §1: guard fires before condition evaluator ─────────────────────────

// TestCP019_GuardFiresBeforeConditionEvaluator verifies that the guard evaluator
// is invoked before any condition expression is evaluated against the run context.
//
// CP-019: "Guards MUST run BEFORE the condition cascade."
func TestCP019_GuardFiresBeforeConditionEvaluator(t *testing.T) {
	t.Parallel()

	run := cp019FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	cond := PolicyExpression("always")
	e := Edge{FromNode: "node-src", ToNode: "node-dst", Condition: &cond, Weight: 1, OrderingKey: "a"}

	var callOrder []string

	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		callOrder = append(callOrder, "guard")
		return edges
	}
	eval := func(_ PolicyExpression, _ map[string]any, _ Outcome) bool {
		callOrder = append(callOrder, "condition")
		return true
	}

	cycles := NewCycleCounter()
	DispatchEdge(run, []Edge{e}, outcome, eval, cycles, guard, PermitGate)

	if len(callOrder) < 2 {
		t.Fatalf("CP-019: expected guard and condition both called; got callOrder=%v", callOrder)
	}
	if callOrder[0] != "guard" {
		t.Errorf("CP-019: first call was %q, want %q — guard must precede condition cascade", callOrder[0], "guard")
	}
	if callOrder[1] != "condition" {
		t.Errorf("CP-019: second call was %q, want %q", callOrder[1], "condition")
	}
}

// ── CP-019 §2: condition evaluator receives guard-reordered list ──────────────

// TestCP019_ConditionEvaluatorReceivesGuardReorderedList verifies that the
// condition evaluator is called for edges in the order returned by the guard,
// not the original candidate order.
//
// CP-019: "The cascade then operates on the Guard-reordered edge list as its input."
func TestCP019_ConditionEvaluatorReceivesGuardReorderedList(t *testing.T) {
	t.Parallel()

	run := cp019FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	condA := PolicyExpression("cond-a")
	condB := PolicyExpression("cond-b")
	condC := PolicyExpression("cond-c")
	eA := Edge{FromNode: "node-src", ToNode: "node-a", Condition: &condA, Weight: 1, OrderingKey: "a"}
	eB := Edge{FromNode: "node-src", ToNode: "node-b", Condition: &condB, Weight: 1, OrderingKey: "b"}
	eC := Edge{FromNode: "node-src", ToNode: "node-c", Condition: &condC, Weight: 1, OrderingKey: "c"}
	candidates := []Edge{eA, eB, eC}

	// Guard reverses the candidate list: output order is [C, B, A].
	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		reversed := make([]Edge, len(edges))
		for i, e := range edges {
			reversed[len(edges)-1-i] = e
		}
		return reversed
	}

	// Record which edge each condition evaluation corresponds to, in call order.
	var evalOrder []NodeID
	eval := func(expr PolicyExpression, _ map[string]any, _ Outcome) bool {
		switch string(expr) {
		case "cond-a":
			evalOrder = append(evalOrder, "node-a")
		case "cond-b":
			evalOrder = append(evalOrder, "node-b")
		case "cond-c":
			evalOrder = append(evalOrder, "node-c")
		}
		return true
	}

	cycles := NewCycleCounter()
	DispatchEdge(run, candidates, outcome, eval, cycles, guard, PermitGate)

	// Condition evaluator must be called in guard-reordered order: C, B, A.
	want := []NodeID{"node-c", "node-b", "node-a"}
	if len(evalOrder) != len(want) {
		t.Fatalf("CP-019: condition evaluator called %d times, want %d; got %v", len(evalOrder), len(want), evalOrder)
	}
	for i, got := range evalOrder {
		if got != want[i] {
			t.Errorf("CP-019: condition eval call[%d] = %q, want %q — cascade must iterate guard-reordered list", i, got, want[i])
		}
	}
}

// ── CP-019 §3: guard order preserved through condition filter ─────────────────

// TestCP019_GuardOrderPreservedThroughConditionFilter verifies that when the
// guard reorders edges and the condition filter eliminates some, the remaining
// matched set preserves the guard's relative ordering for equal-weight,
// equal-key edges (stable sort invariant).
//
// CP-019: "The cascade then operates on the Guard-reordered edge list as its input."
func TestCP019_GuardOrderPreservedThroughConditionFilter(t *testing.T) {
	t.Parallel()

	run := cp019FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}

	condFail := PolicyExpression("fail")
	// Three edges with identical weight and ordering key; sort tie-break is stable.
	// Guard places B first.
	eA := Edge{FromNode: "node-src", ToNode: "node-a", Weight: 5, OrderingKey: "x"}
	eB := Edge{FromNode: "node-src", ToNode: "node-b", Weight: 5, OrderingKey: "x"}
	eC := Edge{FromNode: "node-src", ToNode: "node-c", Weight: 5, OrderingKey: "x", Condition: &condFail}
	candidates := []Edge{eA, eC, eB}

	// Guard reorders to [B, A, C].
	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		var b, a, c Edge
		for _, e := range edges {
			switch e.ToNode {
			case "node-b":
				b = e
			case "node-a":
				a = e
			case "node-c":
				c = e
			}
		}
		return []Edge{b, a, c}
	}

	// Condition evaluator: fail for condFail, pass for everything else.
	eval := func(expr PolicyExpression, _ map[string]any, _ Outcome) bool {
		return string(expr) != "fail"
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, eval, cycles, guard, PermitGate)

	if !result.Advance {
		t.Fatalf("CP-019: expected Advance=true; got failure: %s / %s", result.FailureClass, result.FailureReason)
	}
	// After guard reorder [B, A, C] and condition filter (C removed): matched = [B, A].
	// Equal weight + equal key → stable sort preserves guard's order → B wins.
	if result.Edge.ToNode != "node-b" {
		t.Errorf("CP-019: selected %q, want %q — guard's reordering must be preserved through condition filter (guard placed B first)", result.Edge.ToNode, "node-b")
	}
}

// ── CP-019 §4: preferred_label step is downstream of guard ───────────────────

// TestCP019_PreferredLabelStepIsDownstreamOfGuard verifies that the
// preferred_label hint (EM-041 step b) narrows the guard-reordered,
// condition-filtered matched set — not the original candidate list.
//
// CP-019: "subsequent precedence rules (condition match, preferred_label, …)
// are applied in the order defined by the execution-model spec."
func TestCP019_PreferredLabelStepIsDownstreamOfGuard(t *testing.T) {
	t.Parallel()

	run := cp019FixtureRun(t)
	labelStr := "pick-me"
	outcome := Outcome{
		Status:         OutcomeStatusSuccess,
		Kind:           OutcomeKindDefault,
		PreferredLabel: &labelStr,
	}

	// eOther has higher weight; without preferred_label it would win.
	// ePick carries the matching label.
	ePick := cp019FixtureEdge(t, "node-pick", 5, "b")
	ePick.Label = &labelStr
	eOther := cp019FixtureEdge(t, "node-other", 10, "a")
	candidates := []Edge{eOther, ePick}

	// Guard reverses to [ePick, eOther] — guard order should not affect preferred_label result.
	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return []Edge{edges[1], edges[0]}
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, cp019EvalAlwaysTrue, cycles, guard, PermitGate)

	if !result.Advance {
		t.Fatalf("CP-019: expected Advance=true; failure: %s / %s", result.FailureClass, result.FailureReason)
	}
	// preferred_label "pick-me" must select ePick even though eOther has higher weight.
	if result.Edge.ToNode != "node-pick" {
		t.Errorf("CP-019: preferred_label selected %q, want %q — preferred_label step must operate on guard-reordered list", result.Edge.ToNode, "node-pick")
	}
}

// ── CP-019 §5: suggested_next_ids step is downstream of guard ────────────────

// TestCP019_SuggestedNextIDsStepIsDownstreamOfGuard verifies that the
// suggested_next_ids hint (EM-041 step c) narrows the guard-reordered,
// condition-filtered matched set.
//
// CP-019: "subsequent precedence rules (…, suggested_next_ids, …) are applied
// in the order defined by the execution-model spec."
func TestCP019_SuggestedNextIDsStepIsDownstreamOfGuard(t *testing.T) {
	t.Parallel()

	run := cp019FixtureRun(t)
	outcome := Outcome{
		Status:           OutcomeStatusSuccess,
		Kind:             OutcomeKindDefault,
		SuggestedNextIDs: []NodeID{"node-hint"},
	}

	// eOther has higher weight; without the hint it would win.
	eHint := cp019FixtureEdge(t, "node-hint", 1, "b")
	eOther := cp019FixtureEdge(t, "node-other", 10, "a")
	candidates := []Edge{eOther, eHint}

	// Guard reverses to [eHint, eOther].
	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return []Edge{edges[1], edges[0]}
	}

	cycles := NewCycleCounter()
	result := DispatchEdge(run, candidates, outcome, cp019EvalAlwaysTrue, cycles, guard, PermitGate)

	if !result.Advance {
		t.Fatalf("CP-019: expected Advance=true; failure: %s / %s", result.FailureClass, result.FailureReason)
	}
	// suggested_next_ids narrows to eHint even though eOther has higher weight.
	if result.Edge.ToNode != "node-hint" {
		t.Errorf("CP-019: suggested_next_ids selected %q, want %q — hint step must operate on guard-reordered list", result.Edge.ToNode, "node-hint")
	}
}
