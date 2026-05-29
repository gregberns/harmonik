package core

// cp018_guard_reorders_test.go — Conformance tests for CP-018
//
// specs/control-points.md §4.4.CP-018:
//
//	A Guard MUST fire during edge evaluation of the deterministic cascade
//	defined in [execution-model.md §4.10]. The evaluator receives the candidate
//	edge set, current state, and outcome; the evaluator returns a reordered edge
//	list that is a subset or permutation of the input. A Guard MUST NOT add
//	edges not present in the input, remove edges, or block a transition. Gate
//	semantics (deny / escalate) are NOT available to Guards.
//
// Tests:
//  1. Guard fires during edge evaluation (is called by DispatchEdge).
//  2. Guard receives the candidate edge set, *Run (current state), and Outcome.
//  3. Guard MUST NOT add edges: DispatchEdge panics when returned length > input.
//  4. Guard MUST NOT remove edges: DispatchEdge panics when returned length < input.
//  5. Gate semantics unavailable: GuardEvaluator return type is []Edge, not GateAction.
//  6. Guard observes post-context-update run state (EM-041a ordering: context
//     updates precede guard invocation per execution-model.md §7.3 pseudocode).

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

func cp018FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cp018"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func cp018FixtureEdge(t *testing.T, toNode NodeID) Edge {
	t.Helper()
	return Edge{FromNode: "node-src", ToNode: toNode, Weight: 1, OrderingKey: "a"}
}

// ── CP-018 §1: guard fires during edge evaluation ────────────────────────────

// TestCP018_GuardFiresDuringEdgeEvaluation verifies that the guard evaluator is
// called by DispatchEdge, confirming that a Guard fires during edge evaluation
// per specs/control-points.md §4.4.CP-018.
func TestCP018_GuardFiresDuringEdgeEvaluation(t *testing.T) {
	t.Parallel()

	run := cp018FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	e := cp018FixtureEdge(t, "node-dst")

	guardCalled := false
	guard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		guardCalled = true
		return edges
	}

	cycles := NewCycleCounter()
	DispatchEdge(run, []Edge{e}, outcome, func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }, cycles, guard, PermitGate)

	if !guardCalled {
		t.Error("CP-018: guard was not called during edge evaluation")
	}
}

// ── CP-018 §2: guard receives correct inputs ─────────────────────────────────

// TestCP018_GuardReceivesCandidateEdgesRunAndOutcome verifies that the guard
// evaluator receives the candidate edge set, current *Run (current state), and
// the Outcome, per specs/control-points.md §4.4.CP-018.
func TestCP018_GuardReceivesCandidateEdgesRunAndOutcome(t *testing.T) {
	t.Parallel()

	run := cp018FixtureRun(t)
	outcome := Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
		Notes:  "cp018-marker",
	}
	eA := cp018FixtureEdge(t, "node-a")
	eB := cp018FixtureEdge(t, "node-b")
	eB.OrderingKey = "b"
	candidates := []Edge{eA, eB}

	var gotRun *Run
	var gotEdges []Edge
	var gotOutcome Outcome

	guard := func(r *Run, edges []Edge, o Outcome) []Edge {
		gotRun = r
		gotEdges = make([]Edge, len(edges))
		copy(gotEdges, edges)
		gotOutcome = o
		return edges
	}

	cycles := NewCycleCounter()
	DispatchEdge(run, candidates, outcome, func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }, cycles, guard, PermitGate)

	if gotRun != run {
		t.Error("CP-018: guard did not receive the expected *Run")
	}
	if len(gotEdges) != len(candidates) {
		t.Errorf("CP-018: guard received %d edges, want %d", len(gotEdges), len(candidates))
	}
	if gotOutcome.Notes != "cp018-marker" {
		t.Errorf("CP-018: guard received Outcome.Notes=%q, want %q", gotOutcome.Notes, "cp018-marker")
	}
}

// ── CP-018 §3: guard MUST NOT add edges ──────────────────────────────────────

// TestCP018_GuardMustNotAddEdges verifies that a guard returning a longer slice
// than its input causes DispatchEdge to panic per the "MUST NOT add edges not
// present in the input" constraint of CP-018.
func TestCP018_GuardMustNotAddEdges(t *testing.T) {
	t.Parallel()

	run := cp018FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	e := cp018FixtureEdge(t, "node-dst")

	// Guard violates CP-018: adds an extra edge.
	addGuard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		extra := edges[0]
		extra.ToNode = "node-extra"
		return append(edges, extra)
	}

	cycles := NewCycleCounter()

	defer func() {
		if r := recover(); r == nil {
			t.Error("CP-018: expected panic when guard adds an edge, got none")
		}
	}()

	DispatchEdge(run, []Edge{e}, outcome, func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }, cycles, addGuard, PermitGate)
}

// ── CP-018 §4: guard MUST NOT remove edges ───────────────────────────────────

// TestCP018_GuardMustNotRemoveEdges verifies that a guard returning a shorter
// slice than its input causes DispatchEdge to panic per the "MUST NOT remove
// edges" constraint of CP-018.
func TestCP018_GuardMustNotRemoveEdges(t *testing.T) {
	t.Parallel()

	run := cp018FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	eA := cp018FixtureEdge(t, "node-a")
	eB := cp018FixtureEdge(t, "node-b")
	eB.OrderingKey = "b"

	// Guard violates CP-018: removes one edge.
	removeGuard := func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return edges[:1]
	}

	cycles := NewCycleCounter()

	defer func() {
		if r := recover(); r == nil {
			t.Error("CP-018: expected panic when guard removes an edge, got none")
		}
	}()

	DispatchEdge(run, []Edge{eA, eB}, outcome, func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }, cycles, removeGuard, PermitGate)
}

// ── CP-018 §5: gate semantics not available to guards ────────────────────────

// TestCP018_GuardReturnTypeIsEdgeListNotGateAction verifies at the type level
// that GuardEvaluator returns []Edge (not GateAction), confirming that gate
// semantics (deny / escalate) are structurally unavailable to guards per CP-018.
// This test is a compile-time assertion encoded as a runtime check.
func TestCP018_GuardReturnTypeIsEdgeListNotGateAction(t *testing.T) {
	t.Parallel()

	// The GuardEvaluator type is func(*Run, []Edge, Outcome) []Edge.
	// If this compiles, the return type is []Edge — not GateAction.
	// A guard cannot return GateActionDeny or GateActionEscalateToHuman.
	var _ GuardEvaluator = func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return edges // can only reorder — not deny/escalate
	}

	// Confirm IdentityGuard is a valid GuardEvaluator (no-op path).
	var _ GuardEvaluator = IdentityGuard
}

// ── CP-018 §6: guard observes post-context-update run state ──────────────────

// TestCP018_GuardObservesPostContextUpdateState verifies that the guard sees
// run.Context state AFTER outcome.ContextUpdates have been applied, per the
// execution-model.md §7.3 pseudocode ordering:
//
//	apply_context_updates(run, outcome.context_updates)  -- §4.10.EM-041a
//	candidate_edges = apply_guards(run, ...)              -- per [control-points.md §6.4]
//
// Before the fix for CP-018, DispatchEdge called the guard before applying
// context updates, so the guard saw stale context. This test pins the correct
// post-update ordering.
func TestCP018_GuardObservesPostContextUpdateState(t *testing.T) {
	t.Parallel()

	run := cp018FixtureRun(t)

	// Outcome carries a context update that sets "route" = "priority".
	outcome := Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
		ContextUpdates: map[string]any{
			"route": "priority",
		},
	}
	e := cp018FixtureEdge(t, "node-dst")

	var contextSeenByGuard map[string]any
	guard := func(r *Run, edges []Edge, _ Outcome) []Edge {
		// Capture a snapshot of run.Context at the moment guard fires.
		contextSeenByGuard = make(map[string]any, len(r.Context))
		for k, v := range r.Context {
			contextSeenByGuard[k] = v
		}
		return edges
	}

	cycles := NewCycleCounter()
	DispatchEdge(run, []Edge{e}, outcome, func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }, cycles, guard, PermitGate)

	// Guard MUST have seen "route" = "priority" — the post-context-update value.
	val, ok := contextSeenByGuard["route"]
	if !ok {
		t.Fatal("CP-018: guard did not observe run.Context[\"route\"] — context updates must precede guard invocation per EM-041a")
	}
	if val != "priority" {
		t.Errorf("CP-018: guard observed run.Context[\"route\"] = %v, want %q", val, "priority")
	}
}
