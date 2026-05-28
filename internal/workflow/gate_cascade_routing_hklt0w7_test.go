package workflow_test

// gate_cascade_routing_hklt0w7_test.go — gate-node-outcome → cascade routing.
//
// This is the testable seam for "gate-node dispatch in the cascade" (hk-lt0w7 /
// hk-karlz). A real daemon-side gate EVALUATOR is not wired yet (hk-karlz: no
// GateEvalFunc provider, no ControlPoint-registry loading from the daemon), so
// driveDotWorkflow still refuses gate nodes. But the two halves that the daemon
// would glue together ARE both implemented and can be exercised end-to-end here:
//
//   1. handler.DispatchGateNode produces the Outcome from a gate evaluator's
//      decision (allow/deny/escalate → status=SUCCESS, kind=gate_decision,
//      preferred_label=<decision>; eval-failure → status=FAIL, no payload).
//   2. workflow.DecideNextNode routes that Outcome along the graph edges.
//
// This proves the CP-058 routing contract that the daemon wiring (hk-karlz) will
// rely on: because every evaluated gate is status=SUCCESS, allow vs deny vs
// escalate are distinguished ONLY by routing on outcome.preferred_label. A
// status-only graph could not tell them apart — the test below would route every
// decision to the same edge if the contract were status-based.
//
// Spec refs:
//   - specs/control-points.md §6.1.8 CP-058 (gate is SUCCESS regardless of decision).
//   - specs/execution-model.md §4.1 EM-005b (gate_decision Outcome; routing on decision).
//   - specs/workflow-graph.md §6 WG-014 / §7 WG-019 (preferred_label routing).
// Bead ref: hk-lt0w7 (gate-deny semantics); hk-karlz (daemon evaluator wiring, deferred).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// gateRouteRun returns a minimal valid Run for the routing test.
func gateRouteRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("0.1.0"),
		Input:           core.WorkspaceRef("ws-gate-route"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// gateRouteNoopBus is a no-op event bus for DispatchGateNode (we assert routing,
// not events; events are covered by the handler-package gate tests).
type gateRouteNoopBus struct{}

func (gateRouteNoopBus) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}
func (gateRouteNoopBus) Emit(context.Context, core.EventType, []byte) error { return nil }
func (gateRouteNoopBus) Subscribe(core.Subscription) (core.Subscription, error) {
	return core.Subscription{}, nil
}
func (gateRouteNoopBus) Seal() error                                       { return nil }
func (gateRouteNoopBus) ReplayFrom(string, core.EventID) error             { return nil }
func (gateRouteNoopBus) DeadLetterReplay(string, *core.EventPattern) error { return nil }
func (gateRouteNoopBus) Drain(context.Context) error                       { return nil }

// gateRouteDecisionEdge builds a *dot.Edge keyed on
// `outcome.preferred_label == '<decision>'`.
func gateRouteDecisionEdge(from, to, decision string) *dot.Edge {
	lhs := "outcome.preferred_label"
	op := "=="
	raw := lhs + " " + op + " '" + decision + "'"
	return &dot.Edge{
		FromNodeID:   from,
		ToNodeID:     to,
		OrderingKey:  to,
		Condition:    &dot.Condition{Clauses: []dot.Equality{{LHS: lhs, Op: op, RHS: decision}}},
		ConditionRaw: raw,
	}
}

// gateRouteStatusEdge builds a *dot.Edge keyed on `outcome.status == '<status>'`.
func gateRouteStatusEdge(from, to, status string) *dot.Edge {
	lhs := "outcome.status"
	op := "=="
	raw := lhs + " " + op + " '" + status + "'"
	return &dot.Edge{
		FromNodeID:   from,
		ToNodeID:     to,
		OrderingKey:  to,
		Condition:    &dot.Condition{Clauses: []dot.Equality{{LHS: lhs, Op: op, RHS: status}}},
		ConditionRaw: raw,
	}
}

// gateRouteGraph builds a graph: gate node "g" with the supplied outbound edges
// plus the named terminal nodes.
func gateRouteGraph(edges []*dot.Edge, nodeIDs, terminals []string) *dot.Graph {
	g := &dot.Graph{StartNodeID: "g", TerminalNodeIDs: terminals}
	for _, id := range nodeIDs {
		g.Nodes = append(g.Nodes, &dot.Node{ID: id})
	}
	g.Edges = edges
	return g
}

// TestGateDecisionRoutesOnPreferredLabel proves that a gate node's evaluated
// decision (allow/deny/escalate — all status=SUCCESS) routes to the matching
// decision-labelled edge through the real DispatchGateNode → DecideNextNode path.
func TestGateDecisionRoutesOnPreferredLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		decision core.GateAction
		sigID    *string
		wantNext string
	}{
		{name: "allow", decision: core.GateActionAllow, wantNext: "node-allow"},
		{name: "deny", decision: core.GateActionDeny, wantNext: "node-deny"},
		{
			name:     "escalate",
			decision: core.GateActionEscalateToHuman,
			sigID:    strPtr("sig-x"),
			wantNext: "node-escalate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := gateRouteRun(t)
			evalFn := func(context.Context, *core.Run, core.NodeID, core.GateRef) (*core.GateDecisionPayload, error) {
				return &core.GateDecisionPayload{
					PolicyID:           "p",
					Decision:           tc.decision,
					DecisionActor:      "mechanism",
					ResolutionSignalID: tc.sigID,
				}, nil
			}

			// 1) Dispatch the gate node → Outcome.
			res, err := handler.DispatchGateNode(
				context.Background(), run, core.NodeID("g"), core.GateRef("gate-policy"),
				evalFn, gateRouteNoopBus{},
			)
			if err != nil {
				t.Fatalf("DispatchGateNode: %v", err)
			}
			if res.Outcome.Status != core.OutcomeStatusSuccess {
				t.Fatalf("Outcome.Status = %q, want SUCCESS (CP-058)", res.Outcome.Status)
			}

			// 2) Route the Outcome via the cascade. All three decision edges share
			// status=SUCCESS, so only preferred_label-based routing can pick the
			// right one — exactly the CP-058 contract.
			edges := []*dot.Edge{
				gateRouteDecisionEdge("g", "node-allow", string(core.GateActionAllow)),
				gateRouteDecisionEdge("g", "node-deny", string(core.GateActionDeny)),
				gateRouteDecisionEdge("g", "node-escalate", string(core.GateActionEscalateToHuman)),
			}
			graph := gateRouteGraph(edges,
				[]string{"g", "node-allow", "node-deny", "node-escalate"},
				[]string{"node-allow", "node-deny", "node-escalate"})

			dec := workflow.DecideNextNode(graph, "g", res.Outcome, run, core.NewCycleCounter())
			if !dec.Advance {
				t.Fatalf("DecideNextNode: Advance=false (Failed=%v reason=%s)",
					dec.Failed, dec.FailureReason)
			}
			if dec.NextNodeID != tc.wantNext {
				t.Errorf("routed to %q, want %q (decision=%s)", dec.NextNodeID, tc.wantNext, tc.decision)
			}
		})
	}
}

// TestGateEvalFailureRoutesToFailEdge proves that a gate whose evaluator cannot
// produce a verdict (returns an error) yields a FAIL Outcome (no payload) and
// routes on outcome.status == 'FAIL' — the ONLY path that produces a FAIL gate
// outcome per CP-058.
func TestGateEvalFailureRoutesToFailEdge(t *testing.T) {
	t.Parallel()

	run := gateRouteRun(t)
	evalFn := func(context.Context, *core.Run, core.NodeID, core.GateRef) (*core.GateDecisionPayload, error) {
		return nil, errors.New("evaluator down")
	}

	res, err := handler.DispatchGateNode(
		context.Background(), run, core.NodeID("g"), core.GateRef("gate-policy"),
		evalFn, gateRouteNoopBus{},
	)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected Go error: %v", err)
	}
	if res.Outcome.Status != core.OutcomeStatusFail {
		t.Fatalf("Outcome.Status = %q, want FAIL", res.Outcome.Status)
	}
	if res.Outcome.Kind == core.OutcomeKindGateDecision || res.Outcome.Payload != nil {
		t.Fatalf("eval-failure Outcome must not carry a gate_decision payload (CP-058)")
	}

	edges := []*dot.Edge{
		gateRouteStatusEdge("g", "node-ok", "SUCCESS"),
		gateRouteStatusEdge("g", "node-fail", "FAIL"),
	}
	graph := gateRouteGraph(edges,
		[]string{"g", "node-ok", "node-fail"},
		[]string{"node-ok", "node-fail"})

	dec := workflow.DecideNextNode(graph, "g", res.Outcome, run, core.NewCycleCounter())
	if !dec.Advance {
		t.Fatalf("DecideNextNode: Advance=false (Failed=%v reason=%s)", dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "node-fail" {
		t.Errorf("routed to %q, want %q (eval failure)", dec.NextNodeID, "node-fail")
	}
}

func strPtr(s string) *string { return &s }
