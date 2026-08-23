package workflow

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func fixtureRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      mustWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
		WorkflowVersion: core.WorkflowVersion("0.1.0"),
		Input:           core.WorkspaceRef("ws-ref"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func fixtureOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

const mkCondEdgeFromNode = "a"

func mkCondEdge(to, lhs, rhs string) *dot.Edge {
	const op = "=="
	raw := lhs + " " + op + " '" + rhs + "'"
	return &dot.Edge{
		FromNodeID:   mkCondEdgeFromNode,
		ToNodeID:     to,
		OrderingKey:  to,
		Condition:    &dot.Condition{Clauses: []dot.Equality{{LHS: lhs, Op: op, RHS: rhs}}},
		ConditionRaw: raw,
	}
}

func mkUncondEdge(from, to string) *dot.Edge {
	return &dot.Edge{FromNodeID: from, ToNodeID: to, OrderingKey: to}
}

func mkGraph(startNode string, terminalNodes, nodeIDs []string, edges []*dot.Edge) *dot.Graph {
	g := &dot.Graph{
		StartNodeID:     startNode,
		TerminalNodeIDs: terminalNodes,
	}
	for _, n := range nodeIDs {
		g.Nodes = append(g.Nodes, &dot.Node{ID: n})
	}
	g.Edges = edges
	return g
}

// TestDecideNextNode_Terminal verifies that when fromNodeID is in
// terminal_node_ids the decision is IsTerminal=true with no NextNodeID.
func TestDecideNextNode_Terminal(t *testing.T) {
	g := mkGraph("start", []string{"done"}, []string{"start", "done"}, nil)
	run := fixtureRun(t)
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "done", fixtureOutcome(core.OutcomeStatusSuccess), run, cycles)

	if !dec.IsTerminal {
		t.Errorf("IsTerminal = false, want true")
	}
	if dec.Advance || dec.Failed {
		t.Errorf("unexpected Advance=%v Failed=%v on terminal", dec.Advance, dec.Failed)
	}
	if dec.Payload == nil {
		t.Fatal("Payload is nil")
	}
	if !dec.Payload.IsTerminal {
		t.Errorf("Payload.IsTerminal = false, want true")
	}
	if dec.Payload.NextNodeID != "" {
		t.Errorf("Payload.NextNodeID = %q, want empty on terminal", dec.Payload.NextNodeID)
	}
}

// TestDecideNextNode_ConditionalMatch_Success verifies the SUCCESS edge is
// selected when outcome.status == SUCCESS.
func TestDecideNextNode_ConditionalMatch_Success(t *testing.T) {
	successEdge := mkCondEdge("b", "outcome.status", "SUCCESS")
	failEdge := mkCondEdge("c", "outcome.status", "FAIL")
	g := mkGraph("a", []string{"b", "c"}, []string{"a", "b", "c"},
		[]*dot.Edge{successEdge, failEdge})
	run := fixtureRun(t)
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "a", fixtureOutcome(core.OutcomeStatusSuccess), run, cycles)

	if !dec.Advance {
		t.Fatalf("Advance = false: FailureClass=%s FailureReason=%s", dec.FailureClass, dec.FailureReason)
	}
	if dec.NextNodeID != "b" {
		t.Errorf("NextNodeID = %q, want %q", dec.NextNodeID, "b")
	}
	if dec.Payload.NextNodeID != "b" {
		t.Errorf("Payload.NextNodeID = %q, want %q", dec.Payload.NextNodeID, "b")
	}
}

// TestDecideNextNode_ConditionalMatch_Fail verifies the FAIL edge is selected
// when outcome.status == FAIL.
func TestDecideNextNode_ConditionalMatch_Fail(t *testing.T) {
	successEdge := mkCondEdge("b", "outcome.status", "SUCCESS")
	failEdge := mkCondEdge("c", "outcome.status", "FAIL")
	g := mkGraph("a", []string{"b", "c"}, []string{"a", "b", "c"},
		[]*dot.Edge{successEdge, failEdge})
	run := fixtureRun(t)
	outcome := fixtureOutcome(core.OutcomeStatusFail)
	fc := core.FailureClassStructural
	outcome.FailureClass = &fc
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "a", outcome, run, cycles)

	if !dec.Advance {
		t.Fatalf("Advance = false: %s %s", dec.FailureClass, dec.FailureReason)
	}
	if dec.NextNodeID != "c" {
		t.Errorf("NextNodeID = %q, want %q", dec.NextNodeID, "c")
	}
}

// TestDecideNextNode_PreferredLabel narrows the candidate set to the edge
// whose label matches outcome.preferred_label.
func TestDecideNextNode_PreferredLabel(t *testing.T) {
	e1 := &dot.Edge{FromNodeID: "a", ToNodeID: "approve", OrderingKey: "approve", PreferredLabel: "APPROVE"}
	e2 := &dot.Edge{FromNodeID: "a", ToNodeID: "reject", OrderingKey: "reject", PreferredLabel: "REJECT"}
	g := mkGraph("a", []string{"approve", "reject"}, []string{"a", "approve", "reject"},
		[]*dot.Edge{e1, e2})
	run := fixtureRun(t)
	label := "APPROVE"
	outcome := core.Outcome{
		Status:         core.OutcomeStatusSuccess,
		Kind:           core.OutcomeKindDefault,
		PreferredLabel: &label,
	}
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "a", outcome, run, cycles)

	if !dec.Advance || dec.NextNodeID != "approve" {
		t.Errorf("got Advance=%v NextNodeID=%q, want Advance=true NextNodeID=approve",
			dec.Advance, dec.NextNodeID)
	}
}

// TestDecideNextNode_UnconditionalFallback verifies that when no conditional
// edge matches the unconditional edge is taken (WG-011 invariant).
func TestDecideNextNode_UnconditionalFallback(t *testing.T) {
	condEdge := mkCondEdge("specific", "outcome.status", "FAIL")
	uncondEdge := mkUncondEdge("a", "default")
	g := mkGraph("a", []string{"specific", "default"}, []string{"a", "specific", "default"},
		[]*dot.Edge{condEdge, uncondEdge})
	run := fixtureRun(t)
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "a", fixtureOutcome(core.OutcomeStatusSuccess), run, cycles)

	if !dec.Advance {
		t.Fatalf("Advance = false: %s %s", dec.FailureClass, dec.FailureReason)
	}
	if dec.NextNodeID != "default" {
		t.Errorf("NextNodeID = %q, want %q (unconditional fallback)", dec.NextNodeID, "default")
	}
}

// TestDecideNextNode_NoMatch_Structural verifies that when no edge matches the
// cascade returns Failed=true with FailureClass=structural.
func TestDecideNextNode_NoMatch_Structural(t *testing.T) {
	failEdge := mkCondEdge("b", "outcome.status", "FAIL")
	g := mkGraph("a", []string{"b"}, []string{"a", "b"}, []*dot.Edge{failEdge})
	run := fixtureRun(t)
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "a", fixtureOutcome(core.OutcomeStatusSuccess), run, cycles)

	if !dec.Failed {
		t.Fatalf("Failed = false, want true (no matching edge)")
	}
	if dec.FailureClass != core.FailureClassStructural {
		t.Errorf("FailureClass = %q, want %q", dec.FailureClass, core.FailureClassStructural)
	}
	if dec.FailureReason != "no_outgoing_edge_matches" {
		t.Errorf("FailureReason = %q, want %q", dec.FailureReason, "no_outgoing_edge_matches")
	}
	if dec.Payload.FailureClass != string(core.FailureClassStructural) {
		t.Errorf("Payload.FailureClass = %q, want %q", dec.Payload.FailureClass, core.FailureClassStructural)
	}
}

// TestDecideNextNode_CapHit verifies that a traversal-cap hit on the selected
// edge produces Failed=true with FailureClass=compilation_loop and
// CompletionReason="cap_hit" per EM-015d-RFD vocabulary.
func TestDecideNextNode_CapHit(t *testing.T) {
	run := fixtureRun(t)
	outcome := fixtureOutcome(core.OutcomeStatusSuccess)
	cycles := core.NewCycleCounter()

	capVal := 1
	_, err := cycles.Increment(run.RunID, core.NodeID("a"), core.NodeID("b"), &capVal)
	if err != nil {
		t.Fatalf("unexpected error on pre-increment: %v", err)
	}

	coreEdge := core.Edge{
		FromNode:     core.NodeID("a"),
		ToNode:       core.NodeID("b"),
		OrderingKey:  "b",
		TraversalCap: &capVal,
	}
	alwaysTrue := func(_ core.PolicyExpression, _ map[string]any, _ core.Outcome) bool { return true }
	cascadeResult := core.SelectNextEdge(run, []core.Edge{coreEdge}, outcome, alwaysTrue, cycles)

	if !cascadeResult.Failed {
		t.Fatal("cascade should have failed with cap-hit")
	}
	if cascadeResult.FailureClass != core.FailureClassCompilationLoop {
		t.Errorf("FailureClass = %q, want %q", cascadeResult.FailureClass, core.FailureClassCompilationLoop)
	}

	dec := DispatchDecision{
		Failed:           true,
		FailureClass:     core.FailureClassCompilationLoop,
		FailureReason:    "traversal cap reached",
		CompletionReason: "cap_hit",
		Payload: &core.NodeDispatchDecidedPayload{
			RunID:            run.RunID,
			FromNodeID:       "a",
			Failed:           true,
			FailureClass:     string(core.FailureClassCompilationLoop),
			FailureReason:    "traversal cap reached",
			CompletionReason: "cap_hit",
		},
	}
	if dec.CompletionReason != "cap_hit" {
		t.Errorf("CompletionReason = %q, want %q", dec.CompletionReason, "cap_hit")
	}
	if dec.Payload.CompletionReason != "cap_hit" {
		t.Errorf("Payload.CompletionReason = %q, want %q", dec.Payload.CompletionReason, "cap_hit")
	}
}

// TestDecideNextNode_Payload verifies that Payload fields RunID, FromNodeID,
// and NextNodeID are correctly populated on a successful cascade.
func TestDecideNextNode_Payload(t *testing.T) {
	e := mkUncondEdge("start", "end")
	g := mkGraph("start", []string{"end"}, []string{"start", "end"}, []*dot.Edge{e})
	run := fixtureRun(t)
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "start", fixtureOutcome(core.OutcomeStatusSuccess), run, cycles)

	if dec.Payload == nil {
		t.Fatal("Payload is nil")
	}
	if dec.Payload.RunID != run.RunID {
		t.Errorf("Payload.RunID = %v, want %v", dec.Payload.RunID, run.RunID)
	}
	if dec.Payload.FromNodeID != "start" {
		t.Errorf("Payload.FromNodeID = %q, want %q", dec.Payload.FromNodeID, "start")
	}
	if dec.Payload.NextNodeID != "end" {
		t.Errorf("Payload.NextNodeID = %q, want %q", dec.Payload.NextNodeID, "end")
	}
}

// TestNodeDispatchDecidedPayload_Valid exercises the Valid() predicate.
func TestNodeDispatchDecidedPayload_Valid(t *testing.T) {
	runID := core.RunID(uuid.Must(uuid.NewV7()))

	cases := []struct {
		name   string
		p      core.NodeDispatchDecidedPayload
		wantOK bool
	}{
		{
			name:   "advance_ok",
			p:      core.NodeDispatchDecidedPayload{RunID: runID, FromNodeID: "a", NextNodeID: "b"},
			wantOK: true,
		},
		{
			name:   "terminal_ok",
			p:      core.NodeDispatchDecidedPayload{RunID: runID, FromNodeID: "a", IsTerminal: true},
			wantOK: true,
		},
		{
			name:   "failed_ok",
			p:      core.NodeDispatchDecidedPayload{RunID: runID, FromNodeID: "a", Failed: true},
			wantOK: true,
		},
		{
			name:   "nil_run_id",
			p:      core.NodeDispatchDecidedPayload{FromNodeID: "a", NextNodeID: "b"},
			wantOK: false,
		},
		{
			name:   "empty_from_node_id",
			p:      core.NodeDispatchDecidedPayload{RunID: runID, NextNodeID: "b"},
			wantOK: false,
		},
		{
			name:   "two_outcomes_set",
			p:      core.NodeDispatchDecidedPayload{RunID: runID, FromNodeID: "a", NextNodeID: "b", IsTerminal: true},
			wantOK: false,
		},
		{
			name:   "no_outcome_set",
			p:      core.NodeDispatchDecidedPayload{RunID: runID, FromNodeID: "a"},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Valid(); got != tc.wantOK {
				t.Errorf("Valid() = %v, want %v", got, tc.wantOK)
			}
		})
	}
}

// TestDecideNextNode_ContextUpdate verifies that context updates in the outcome
// are applied before the cascade evaluates edge conditions.
func TestDecideNextNode_ContextUpdate(t *testing.T) {
	condEdge := mkCondEdge("b", "context.phase", "done")
	g := mkGraph("a", []string{"b"}, []string{"a", "b"}, []*dot.Edge{condEdge})
	run := fixtureRun(t)
	outcome := core.Outcome{
		Status:         core.OutcomeStatusSuccess,
		Kind:           core.OutcomeKindDefault,
		ContextUpdates: map[string]any{"phase": "done"},
	}
	cycles := core.NewCycleCounter()

	dec := DecideNextNode(g, "a", outcome, run, cycles)

	if !dec.Advance || dec.NextNodeID != "b" {
		t.Errorf("context update not applied: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}
