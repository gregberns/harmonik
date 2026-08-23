package core

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func cp018FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      mustParseWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
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

// TestCP018_GuardMustNotAddEdges verifies that a guard returning a longer slice
// than its input causes DispatchEdge to panic per the "MUST NOT add edges not
// present in the input" constraint of CP-018.
func TestCP018_GuardMustNotAddEdges(t *testing.T) {
	t.Parallel()

	run := cp018FixtureRun(t)
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	e := cp018FixtureEdge(t, "node-dst")

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

// TestCP018_GuardReturnTypeIsEdgeListNotGateAction verifies at the type level
// that GuardEvaluator returns []Edge (not GateAction), confirming that gate
// semantics (deny / escalate) are structurally unavailable to guards per CP-018.
// This test is a compile-time assertion encoded as a runtime check.
func TestCP018_GuardReturnTypeIsEdgeListNotGateAction(t *testing.T) {
	t.Parallel()

	var _ GuardEvaluator = func(_ *Run, edges []Edge, _ Outcome) []Edge {
		return edges // can only reorder — not deny/escalate
	}

	var _ GuardEvaluator = IdentityGuard
}

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
		contextSeenByGuard = make(map[string]any, len(r.Context))
		for k, v := range r.Context {
			contextSeenByGuard[k] = v
		}
		return edges
	}

	cycles := NewCycleCounter()
	DispatchEdge(run, []Edge{e}, outcome, func(_ PolicyExpression, _ map[string]any, _ Outcome) bool { return true }, cycles, guard, PermitGate)

	val, ok := contextSeenByGuard["route"]
	if !ok {
		t.Fatal("CP-018: guard did not observe run.Context[\"route\"] — context updates must precede guard invocation per EM-041a")
	}
	if val != "priority" {
		t.Errorf("CP-018: guard observed run.Context[\"route\"] = %v, want %q", val, "priority")
	}
}
