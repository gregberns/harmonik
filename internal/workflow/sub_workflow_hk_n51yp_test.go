package workflow

// sub_workflow_hk_n51yp_test.go — requirement-traceable sensors for T-IMPL-011.
//
// Acceptance criteria coverage (hk-n51yp):
//  1. ExpandSubWorkflowGraph: namespace node IDs and edges under parentNodeID.
//  2. ValidateSubWorkflowAcyclicity: detect direct and transitive cycles.
//  3. DispatchSubWorkflow: cascades through expanded nodes, emits
//     sub_workflow_entered and sub_workflow_exited, returns terminal Outcome.
//  4. Terminal Outcome surfaces to parent cascade unchanged (EM-036a).
//
// Spec refs:
//
//	specs/workflow-graph.md §4 WG-006   — sub-workflow node attributes.
//	specs/workflow-graph.md §9 WG-029   — sub-workflow acyclicity.
//	specs/execution-model.md §4.8.EM-034  — expansion in place.
//	specs/execution-model.md §4.8.EM-034a — node-ID namespacing.
//	specs/execution-model.md §4.8.EM-034b — acyclicity obligation.
//	specs/execution-model.md §4.8.EM-036  — lifecycle events.
//	specs/execution-model.md §4.8.EM-036a — terminal outcome escapes.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

// subwfFixturePin builds a valid SubWorkflowExpansionPin.
func subwfFixturePin(ref string, ver string) core.SubWorkflowExpansionPin {
	return core.SubWorkflowExpansionPin{
		SubWorkflowRef:     core.SubWorkflowRef(ref),
		SubWorkflowVersion: core.WorkflowVersion(ver),
		ResolvedWorkflowID: core.WorkflowID(uuid.MustParse("01960000-0000-7000-8000-000000001001")),
	}
}

// subwfFixtureSubGraph builds a minimal two-node sub-workflow dot.Graph:
//   start → end (unconditional, no condition).
func subwfFixtureSubGraph() *dot.Graph {
	return &dot.Graph{
		StartNodeID:     "start",
		TerminalNodeIDs: []string{"end"},
		Nodes: []*dot.Node{
			{ID: "start", Type: core.NodeTypeAgentic},
			{ID: "end", Type: core.NodeTypeAgentic},
		},
		Edges: []*dot.Edge{
			{
				FromNodeID:  "start",
				ToNodeID:    "end",
				OrderingKey: "a",
			},
		},
	}
}

// subwfFixtureRun returns a minimal *core.Run suitable for cascade testing.
func subwfFixtureRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-ref"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// recordingBus is a minimal in-memory event bus for test assertions.
type recordingBus struct {
	events []recordedEvent
}

type recordedEvent struct {
	EventType core.EventType
	Payload   json.RawMessage
}

func (b *recordingBus) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, recordedEvent{EventType: eventType, Payload: payload})
	return nil
}

func (b *recordingBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, recordedEvent{EventType: eventType, Payload: payload})
	return nil
}

func (b *recordingBus) Subscribe(_ core.Subscription) (core.Subscription, error) { return core.Subscription{}, nil }
func (b *recordingBus) Seal() error                                               { return nil }
func (b *recordingBus) ReplayFrom(_ string, _ core.EventID) error                { return nil }
func (b *recordingBus) DeadLetterReplay(_ string, _ *core.EventPattern) error    { return nil }
func (b *recordingBus) Drain(_ context.Context) error                            { return nil }

// ── ExpandSubWorkflowGraph ────────────────────────────────────────────────────

// TestExpandSubWorkflowGraph_NodeIDsNamespaced verifies that all expanded
// node IDs carry the <parentNodeID>/<subNodeID> form per EM-034a.
func TestExpandSubWorkflowGraph_NodeIDsNamespaced(t *testing.T) {
	t.Parallel()

	parentID := core.NodeID("review")
	pin := subwfFixturePin("sub-wf", "1.0")
	subGraph := subwfFixtureSubGraph()

	exp, err := ExpandSubWorkflowGraph(parentID, pin, subGraph)
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph returned error: %v", err)
	}

	wantStart := core.NodeID("review/start")
	if exp.StartNodeID != wantStart {
		t.Errorf("StartNodeID = %q, want %q (EM-034a: namespaced start)", exp.StartNodeID, wantStart)
	}

	if len(exp.TerminalNodeIDs) != 1 || exp.TerminalNodeIDs[0] != "review/end" {
		t.Errorf("TerminalNodeIDs = %v, want [review/end] (EM-034a)", exp.TerminalNodeIDs)
	}

	for _, n := range exp.ExpandedNodes {
		if string(n.NodeID)[:len("review/")] != "review/" {
			t.Errorf("node %q is not namespaced under %q (EM-034a)", n.NodeID, parentID)
		}
	}
}

// TestExpandSubWorkflowGraph_EdgesNamespaced verifies that edge endpoints are
// rewritten to the namespaced form per EM-034a.
func TestExpandSubWorkflowGraph_EdgesNamespaced(t *testing.T) {
	t.Parallel()

	parentID := core.NodeID("review")
	pin := subwfFixturePin("sub-wf", "1.0")
	subGraph := subwfFixtureSubGraph()

	exp, err := ExpandSubWorkflowGraph(parentID, pin, subGraph)
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph returned error: %v", err)
	}
	if len(exp.ExpandedEdges) != 1 {
		t.Fatalf("ExpandedEdges len = %d, want 1", len(exp.ExpandedEdges))
	}
	e := exp.ExpandedEdges[0]
	if e.FromNode != "review/start" {
		t.Errorf("edge FromNode = %q, want %q (EM-034a)", e.FromNode, "review/start")
	}
	if e.ToNode != "review/end" {
		t.Errorf("edge ToNode = %q, want %q (EM-034a)", e.ToNode, "review/end")
	}
}

// TestExpandSubWorkflowGraph_ExpansionValid verifies that the returned
// SubWorkflowExpansion satisfies core.SubWorkflowExpansion.Valid().
func TestExpandSubWorkflowGraph_ExpansionValid(t *testing.T) {
	t.Parallel()

	exp, err := ExpandSubWorkflowGraph("review", subwfFixturePin("sub-wf", "1.0"), subwfFixtureSubGraph())
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph returned error: %v", err)
	}
	if !exp.Valid() {
		t.Error("expansion must be Valid() after ExpandSubWorkflowGraph (EM-034)")
	}
}

// TestExpandSubWorkflowGraph_NilSubGraph rejects nil input.
func TestExpandSubWorkflowGraph_NilSubGraph(t *testing.T) {
	t.Parallel()

	_, err := ExpandSubWorkflowGraph("review", subwfFixturePin("sub-wf", "1.0"), nil)
	if err == nil {
		t.Error("want error on nil subGraph, got nil")
	}
	var expErr *ErrSubWorkflowExpand
	if !errors.As(err, &expErr) {
		t.Errorf("want *ErrSubWorkflowExpand, got %T: %v", err, err)
	}
}

// TestExpandSubWorkflowGraph_InvalidPin rejects an invalid expansion pin.
func TestExpandSubWorkflowGraph_InvalidPin(t *testing.T) {
	t.Parallel()

	badPin := core.SubWorkflowExpansionPin{} // zero value: not valid
	_, err := ExpandSubWorkflowGraph("review", badPin, subwfFixtureSubGraph())
	if err == nil {
		t.Error("want error on invalid pin, got nil")
	}
}

// ── ValidateSubWorkflowAcyclicity ────────────────────────────────────────────

// TestValidateSubWorkflowAcyclicity_DirectCycle detects a direct self-reference
// (A → A) per WG-029 / EM-034b.
func TestValidateSubWorkflowAcyclicity_DirectCycle(t *testing.T) {
	t.Parallel()

	refGraph := core.NewSubWorkflowRefGraph()
	err := ValidateSubWorkflowAcyclicity(refGraph, "wf-A", "wf-A")
	if err == nil {
		t.Error("want cycle error for A→A, got nil")
	}
	var cycleErr *ErrSubWorkflowCycle
	if !errors.As(err, &cycleErr) {
		t.Errorf("want *ErrSubWorkflowCycle, got %T: %v", err, err)
	}
}

// TestValidateSubWorkflowAcyclicity_TransitiveCycle detects A→B→A per WG-029.
func TestValidateSubWorkflowAcyclicity_TransitiveCycle(t *testing.T) {
	t.Parallel()

	refGraph := core.NewSubWorkflowRefGraph()
	// A→B is acyclic.
	if err := ValidateSubWorkflowAcyclicity(refGraph, "wf-A", "wf-B"); err != nil {
		t.Fatalf("A→B should not be cyclic: %v", err)
	}
	// B→A closes the cycle.
	err := ValidateSubWorkflowAcyclicity(refGraph, "wf-B", "wf-A")
	if err == nil {
		t.Error("want cycle error for A→B→A, got nil")
	}
}

// TestValidateSubWorkflowAcyclicity_Acyclic accepts a valid DAG per WG-029.
func TestValidateSubWorkflowAcyclicity_Acyclic(t *testing.T) {
	t.Parallel()

	refGraph := core.NewSubWorkflowRefGraph()
	if err := ValidateSubWorkflowAcyclicity(refGraph, "wf-A", "wf-B"); err != nil {
		t.Errorf("A→B: unexpected cycle error: %v", err)
	}
	if err := ValidateSubWorkflowAcyclicity(refGraph, "wf-A", "wf-C"); err != nil {
		t.Errorf("A→C: unexpected cycle error: %v", err)
	}
	if err := ValidateSubWorkflowAcyclicity(refGraph, "wf-B", "wf-C"); err != nil {
		t.Errorf("B→C: unexpected cycle error: %v", err)
	}
}

// ── DispatchSubWorkflow ───────────────────────────────────────────────────────

// TestDispatchSubWorkflow_EmitsEnteredAndExited verifies that sub_workflow_entered
// and sub_workflow_exited events are emitted in order per EM-036.
func TestDispatchSubWorkflow_EmitsEnteredAndExited(t *testing.T) {
	t.Parallel()

	run := subwfFixtureRun(t)
	pin := subwfFixturePin("sub-wf", "1.0")
	subGraph := subwfFixtureSubGraph()
	exp, err := ExpandSubWorkflowGraph("review", pin, subGraph)
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph: %v", err)
	}

	bus := &recordingBus{}
	cycles := core.NewCycleCounter()

	nodeRunner := func(_ context.Context, _ core.NodeID, _ core.NodeType) (core.Outcome, error) {
		return core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}, nil
	}

	if _, err := DispatchSubWorkflow(context.Background(), run, exp, subGraph, cycles, nodeRunner, bus); err != nil {
		t.Fatalf("DispatchSubWorkflow: %v", err)
	}

	if len(bus.events) < 2 {
		t.Fatalf("want at least 2 events (entered+exited), got %d", len(bus.events))
	}
	if bus.events[0].EventType != core.EventTypeSubWorkflowEntered {
		t.Errorf("events[0] type = %q, want %q (EM-036)", bus.events[0].EventType, core.EventTypeSubWorkflowEntered)
	}
	// sub_workflow_exited is the last event emitted.
	last := bus.events[len(bus.events)-1]
	if last.EventType != core.EventTypeSubWorkflowExited {
		t.Errorf("last event type = %q, want %q (EM-036)", last.EventType, core.EventTypeSubWorkflowExited)
	}
}

// TestDispatchSubWorkflow_TerminalOutcomeEscapes verifies that the terminal
// Outcome returned by DispatchSubWorkflow equals the Outcome produced by the
// last expanded node, per EM-036a.
func TestDispatchSubWorkflow_TerminalOutcomeEscapes(t *testing.T) {
	t.Parallel()

	run := subwfFixtureRun(t)
	pin := subwfFixturePin("sub-wf", "1.0")
	subGraph := subwfFixtureSubGraph()
	exp, err := ExpandSubWorkflowGraph("review", pin, subGraph)
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph: %v", err)
	}

	bus := &recordingBus{}
	cycles := core.NewCycleCounter()

	label := "approved"
	// The "end" node is terminal; its outcome escapes.
	nodeRunner := func(_ context.Context, nodeID core.NodeID, _ core.NodeType) (core.Outcome, error) {
		if string(nodeID) == "review/end" {
			return core.Outcome{
				Status:         core.OutcomeStatusSuccess,
				Kind:           core.OutcomeKindDefault,
				PreferredLabel: &label,
			}, nil
		}
		return core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}, nil
	}

	outcome, err := DispatchSubWorkflow(context.Background(), run, exp, subGraph, cycles, nodeRunner, bus)
	if err != nil {
		t.Fatalf("DispatchSubWorkflow: %v", err)
	}

	if outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("terminal Outcome.Status = %q, want %q (EM-036a)", outcome.Status, core.OutcomeStatusSuccess)
	}
	if outcome.PreferredLabel == nil || *outcome.PreferredLabel != "approved" {
		t.Errorf("terminal Outcome.PreferredLabel = %v, want \"approved\" (EM-036a)", outcome.PreferredLabel)
	}
}

// TestDispatchSubWorkflow_NodeRunnerErrorPropagates verifies that a nodeRunner
// error aborts the dispatch and is returned as an error (not swallowed).
func TestDispatchSubWorkflow_NodeRunnerErrorPropagates(t *testing.T) {
	t.Parallel()

	run := subwfFixtureRun(t)
	pin := subwfFixturePin("sub-wf", "1.0")
	subGraph := subwfFixtureSubGraph()
	exp, err := ExpandSubWorkflowGraph("review", pin, subGraph)
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph: %v", err)
	}

	bus := &recordingBus{}
	cycles := core.NewCycleCounter()
	runnerErr := errors.New("infrastructure failure")

	nodeRunner := func(_ context.Context, _ core.NodeID, _ core.NodeType) (core.Outcome, error) {
		return core.Outcome{}, runnerErr
	}

	_, err = DispatchSubWorkflow(context.Background(), run, exp, subGraph, cycles, nodeRunner, bus)
	if err == nil {
		t.Error("want error from nodeRunner failure, got nil")
	}
	if !errors.Is(err, runnerErr) {
		t.Errorf("want error chain to include runnerErr; got: %v", err)
	}
}

// TestDispatchSubWorkflow_ExitedPayloadCarriesTerminalStatus verifies that the
// sub_workflow_exited payload's TerminalOutcomeStatus matches the actual
// terminal Outcome status per EM-036 / EM-036a.
func TestDispatchSubWorkflow_ExitedPayloadCarriesTerminalStatus(t *testing.T) {
	t.Parallel()

	run := subwfFixtureRun(t)
	pin := subwfFixturePin("sub-wf", "1.0")
	subGraph := subwfFixtureSubGraph()
	exp, err := ExpandSubWorkflowGraph("review", pin, subGraph)
	if err != nil {
		t.Fatalf("ExpandSubWorkflowGraph: %v", err)
	}

	bus := &recordingBus{}
	cycles := core.NewCycleCounter()

	nodeRunner := func(_ context.Context, nodeID core.NodeID, _ core.NodeType) (core.Outcome, error) {
		if string(nodeID) == "review/end" {
			return core.Outcome{Status: core.OutcomeStatusFail, Kind: core.OutcomeKindDefault}, nil
		}
		return core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}, nil
	}

	_, err = DispatchSubWorkflow(context.Background(), run, exp, subGraph, cycles, nodeRunner, bus)
	if err != nil {
		t.Fatalf("DispatchSubWorkflow: %v", err)
	}

	// Find the exited event and unmarshal its payload.
	var exitedPayload core.SubWorkflowExitedPayload
	for _, ev := range bus.events {
		if ev.EventType == core.EventTypeSubWorkflowExited {
			if jErr := json.Unmarshal(ev.Payload, &exitedPayload); jErr != nil {
				t.Fatalf("unmarshal exited payload: %v", jErr)
			}
			break
		}
	}
	if exitedPayload.TerminalOutcomeStatus != core.OutcomeStatusFail {
		t.Errorf("exited payload TerminalOutcomeStatus = %q, want %q (EM-036a)",
			exitedPayload.TerminalOutcomeStatus, core.OutcomeStatusFail)
	}
}
