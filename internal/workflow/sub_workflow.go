package workflow

// sub_workflow.go — sub-workflow expansion and nested-run dispatch.
//
// Provides three capabilities:
//
//  1. ExpandSubWorkflowGraph: converts a sub-workflow dot.Graph into a
//     core.SubWorkflowExpansion by namespacing all node IDs and edge
//     references to the form <parentNodeID>/<subNodeID> per EM-034a.
//
//  2. ValidateSubWorkflowAcyclicity: enforces WG-029 / EM-034b by
//     checking that adding a parent→child sub-workflow reference does
//     not create a cycle in the transitive reference graph.
//
//  3. DispatchSubWorkflow: runs the edge-selection cascade for all
//     nodes in a sub-workflow expansion, emitting sub_workflow_entered
//     and sub_workflow_exited events, and returning the terminal Outcome
//     for the parent cascade to observe (EM-036a).
//
// Spec refs:
//
//	specs/workflow-graph.md §4 WG-006   — sub-workflow node attributes.
//	specs/workflow-graph.md §9 WG-029   — sub-workflow acyclicity.
//	specs/execution-model.md §4.8.EM-034  — expansion in place; single run_id.
//	specs/execution-model.md §4.8.EM-034a — node-ID namespacing (<parent>/<sub>).
//	specs/execution-model.md §4.8.EM-034b — acyclicity obligation.
//	specs/execution-model.md §4.8.EM-034c — expansion pin durability.
//	specs/execution-model.md §4.8.EM-036  — sub_workflow lifecycle events.
//	specs/execution-model.md §4.8.EM-036a — terminal outcome escapes to parent.
//
// Bead ref: hk-n51yp (T-IMPL-011).
// Tags: mechanism

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// SubWorkflowNodeRunner is the function type the daemon provides to execute
// individual nodes within a sub-workflow expansion.
//
// DispatchSubWorkflow calls it for each node in the expanded graph. For agentic
// and non-agentic nodes the daemon launches the appropriate handler subprocess;
// for gate nodes it calls the gate evaluator; for nested sub-workflow nodes it
// dispatches recursively. The nodeID is already in the namespaced form
// <parentNodeID>/<subNodeID> per EM-034a.
type SubWorkflowNodeRunner func(ctx context.Context, nodeID core.NodeID, nodeType core.NodeType) (core.Outcome, error)

// ErrSubWorkflowExpand is the typed error returned by ExpandSubWorkflowGraph
// when a structural precondition fails.
type ErrSubWorkflowExpand struct {
	// Reason describes the failure.
	Reason string
}

func (e *ErrSubWorkflowExpand) Error() string {
	return fmt.Sprintf("sub_workflow_expand: %s", e.Reason)
}

// ErrSubWorkflowCycle is the typed error returned by ValidateSubWorkflowAcyclicity
// when a cyclic sub-workflow reference is detected per WG-029 / EM-034b.
type ErrSubWorkflowCycle struct {
	Parent string
	Child  string
}

func (e *ErrSubWorkflowCycle) Error() string {
	return fmt.Sprintf("sub_workflow_cycle: %q transitively references itself via %q (WG-029/EM-034b)",
		e.Parent, e.Child)
}

// ExpandSubWorkflowGraph builds a core.SubWorkflowExpansion from subGraph by
// namespacing all node IDs and edge references under parentNodeID per EM-034a.
//
// Every node in the expanded graph gets the node ID form
// "<parentNodeID>/<subNodeID>" (via core.NamespaceNodeID). Every edge's
// FromNode and ToNode fields are rewritten to the same namespaced form.
//
// StartNodeID and TerminalNodeIDs in the returned expansion carry the
// already-namespaced forms. pin must satisfy pin.Valid().
//
// Returns *ErrSubWorkflowExpand when subGraph is structurally invalid (missing
// start node, no terminal nodes, nil graph) or when the resulting expansion
// fails core.SubWorkflowExpansion.Valid().
func ExpandSubWorkflowGraph(
	parentNodeID core.NodeID,
	pin core.SubWorkflowExpansionPin,
	subGraph *dot.Graph,
) (*core.SubWorkflowExpansion, error) {
	if subGraph == nil {
		return nil, &ErrSubWorkflowExpand{Reason: "subGraph is nil"}
	}
	if subGraph.StartNodeID == "" {
		return nil, &ErrSubWorkflowExpand{Reason: "subGraph has no start_node"}
	}
	if len(subGraph.TerminalNodeIDs) == 0 {
		return nil, &ErrSubWorkflowExpand{Reason: "subGraph has no terminal_node_ids"}
	}
	if !pin.Valid() {
		return nil, &ErrSubWorkflowExpand{Reason: "expansion pin is not valid"}
	}

	// Namespace all node IDs per EM-034a. The expansion record carries only
	// NodeID and Type; detailed attributes (HandlerRef etc.) are read from
	// the sub-graph directly at dispatch time.
	expandedNodes := make([]core.Node, 0, len(subGraph.Nodes))
	for _, n := range subGraph.Nodes {
		expandedNodes = append(expandedNodes, core.Node{
			NodeID: core.NamespaceNodeID(parentNodeID, core.NodeID(n.ID)),
			Type:   n.Type,
		})
	}

	// Namespace all edge endpoints per EM-034a.
	expandedEdges := make([]core.Edge, 0, len(subGraph.Edges))
	for _, e := range subGraph.Edges {
		from := core.NamespaceNodeID(parentNodeID, core.NodeID(e.FromNodeID))
		to := core.NamespaceNodeID(parentNodeID, core.NodeID(e.ToNodeID))
		ce := dotEdgeToCoreEdge(e)
		ce.FromNode = from
		ce.ToNode = to
		expandedEdges = append(expandedEdges, ce)
	}

	// Namespace start and terminal node IDs.
	startNodeID := core.NamespaceNodeID(parentNodeID, core.NodeID(subGraph.StartNodeID))
	terminalNodeIDs := make([]core.NodeID, 0, len(subGraph.TerminalNodeIDs))
	for _, tid := range subGraph.TerminalNodeIDs {
		terminalNodeIDs = append(terminalNodeIDs, core.NamespaceNodeID(parentNodeID, core.NodeID(tid)))
	}

	exp := &core.SubWorkflowExpansion{
		ParentNodeID:    parentNodeID,
		ExpandedNodes:   expandedNodes,
		ExpandedEdges:   expandedEdges,
		StartNodeID:     startNodeID,
		TerminalNodeIDs: terminalNodeIDs,
		Pin:             pin,
	}
	if !exp.Valid() {
		return nil, &ErrSubWorkflowExpand{Reason: "expansion failed Valid() after construction"}
	}
	return exp, nil
}

// ValidateSubWorkflowAcyclicity adds the directed edge parentWorkflow→childWorkflow
// to refGraph and reports whether a cycle now exists per WG-029 / EM-034b.
//
// Callers build refGraph by walking all sub-workflow references in the
// parent run's loaded graphs and calling this function for each one. refGraph
// is mutated in place; an *ErrSubWorkflowCycle is returned if the edge
// introduces a cycle.
func ValidateSubWorkflowAcyclicity(
	refGraph *core.SubWorkflowRefGraph,
	parentWorkflowName string,
	childWorkflowName string,
) error {
	refGraph.AddEdge(parentWorkflowName, childWorkflowName)
	if refGraph.HasCycle() {
		return &ErrSubWorkflowCycle{
			Parent: parentWorkflowName,
			Child:  childWorkflowName,
		}
	}
	return nil
}

// DispatchSubWorkflow runs the edge-selection cascade for the nodes in expansion
// within the parent run context.
//
// Steps:
//  1. Emit sub_workflow_entered (EM-036).
//  2. Call nodeRunner for the expansion's start node.
//  3. Run DecideNextNode → nodeRunner in a loop until a terminal node is reached.
//  4. Emit sub_workflow_exited with the terminal Outcome's status (EM-036 / EM-036a).
//  5. Return the terminal Outcome for the parent cascade to observe (EM-036a).
//
// The cascade runs against a synthetic dot.Graph built from the expanded edges
// (namespaced IDs, original conditions). The run's Context map is mutated in
// place per EM-041a.
//
// Non-terminal cascade failures are surfaced as errors rather than as Outcomes.
// The caller (the daemon's dispatch loop) is responsible for mapping them to
// structural failure runs.
func DispatchSubWorkflow(
	ctx context.Context,
	run *core.Run,
	expansion *core.SubWorkflowExpansion,
	subGraph *dot.Graph,
	cycles *core.CycleCounter,
	nodeRunner SubWorkflowNodeRunner,
	bus eventbus.EventBus,
) (core.Outcome, error) {
	// Step 1 — emit sub_workflow_entered (EM-036).
	entered := core.SubWorkflowEnteredPayload{
		RunID:              run.RunID,
		ParentNodeID:       expansion.ParentNodeID,
		SubWorkflowName:    expansion.Pin.SubWorkflowRef,
		SubWorkflowVersion: expansion.Pin.SubWorkflowVersion,
	}
	if err := emitJSON(ctx, bus, run.RunID, core.EventTypeSubWorkflowEntered, entered); err != nil {
		return core.Outcome{}, fmt.Errorf("DispatchSubWorkflow: emit sub_workflow_entered: %w", err)
	}

	// Build a synthetic dot.Graph with namespaced node IDs for DecideNextNode.
	// The graph carries the original edge conditions (unchanged — they evaluate
	// against Outcomes, not node IDs).
	expandedGraph := buildNamespacedDotGraph(expansion.ParentNodeID, expansion, subGraph)

	// Step 2–3 — execute nodes and advance the cascade until terminal.
	currentNodeID := string(expansion.StartNodeID)
	var terminalOutcome core.Outcome

	for {
		nodeType := subWorkflowLookupNodeType(expansion, core.NodeID(currentNodeID))

		// Dispatch the current node via the daemon-provided runner.
		outcome, runErr := nodeRunner(ctx, core.NodeID(currentNodeID), nodeType)
		if runErr != nil {
			return core.Outcome{}, fmt.Errorf("DispatchSubWorkflow: nodeRunner(%q): %w", currentNodeID, runErr)
		}

		// Advance the cascade.
		decision := DecideNextNode(expandedGraph, currentNodeID, outcome, run, cycles)
		if decision.IsTerminal {
			terminalOutcome = outcome
			break
		}
		if decision.Failed {
			return core.Outcome{}, fmt.Errorf("DispatchSubWorkflow: cascade failed at %q: %s (%s)",
				currentNodeID, decision.FailureReason, decision.FailureClass)
		}
		currentNodeID = decision.NextNodeID
	}

	// Step 4 — emit sub_workflow_exited with the terminal Outcome status (EM-036 / EM-036a).
	exited := core.SubWorkflowExitedPayload{
		RunID:                 run.RunID,
		ParentNodeID:          expansion.ParentNodeID,
		SubWorkflowName:       expansion.Pin.SubWorkflowRef,
		SubWorkflowVersion:    expansion.Pin.SubWorkflowVersion,
		TerminalOutcomeStatus: terminalOutcome.Status,
	}
	if err := emitJSON(ctx, bus, run.RunID, core.EventTypeSubWorkflowExited, exited); err != nil {
		return core.Outcome{}, fmt.Errorf("DispatchSubWorkflow: emit sub_workflow_exited: %w", err)
	}

	// Step 5 — return terminal Outcome for parent cascade observation (EM-036a).
	return terminalOutcome, nil
}

// buildNamespacedDotGraph constructs a synthetic *dot.Graph whose edges have
// namespaced node IDs (already computed in expansion.ExpandedEdges) but carry
// the original conditions and routing attributes from subGraph.Edges.
//
// The edges are correlated by position: ExpandSubWorkflowGraph preserves the
// original subGraph.Edges order, so expansion.ExpandedEdges[i] corresponds to
// subGraph.Edges[i].
func buildNamespacedDotGraph(parentNodeID core.NodeID, expansion *core.SubWorkflowExpansion, subGraph *dot.Graph) *dot.Graph {
	dotEdges := make([]*dot.Edge, 0, len(subGraph.Edges))
	for i, orig := range subGraph.Edges {
		fromID := string(core.NamespaceNodeID(parentNodeID, core.NodeID(orig.FromNodeID)))
		toID := string(core.NamespaceNodeID(parentNodeID, core.NodeID(orig.ToNodeID)))
		orderingKey := orig.OrderingKey
		if orderingKey == "" {
			orderingKey = toID
		}
		// Use the expanded edge's namespaced IDs if available (bounds-safe).
		if i < len(expansion.ExpandedEdges) {
			fromID = string(expansion.ExpandedEdges[i].FromNode)
			toID = string(expansion.ExpandedEdges[i].ToNode)
		}
		dotEdges = append(dotEdges, &dot.Edge{
			FromNodeID:     fromID,
			ToNodeID:       toID,
			Condition:      orig.Condition,
			ConditionRaw:   orig.ConditionRaw,
			PreferredLabel: orig.PreferredLabel,
			Weight:         orig.Weight,
			OrderingKey:    orderingKey,
		})
	}

	terminalIDs := make([]string, 0, len(expansion.TerminalNodeIDs))
	for _, tid := range expansion.TerminalNodeIDs {
		terminalIDs = append(terminalIDs, string(tid))
	}

	return &dot.Graph{
		StartNodeID:     string(expansion.StartNodeID),
		TerminalNodeIDs: terminalIDs,
		Edges:           dotEdges,
		ContextKeys:     subGraph.ContextKeys,
	}
}

// subWorkflowLookupNodeType returns the NodeType of nodeID from
// expansion.ExpandedNodes. Returns an empty NodeType if nodeID is not found
// (caller's precondition violation).
func subWorkflowLookupNodeType(expansion *core.SubWorkflowExpansion, nodeID core.NodeID) core.NodeType {
	for _, n := range expansion.ExpandedNodes {
		if n.NodeID == nodeID {
			return n.Type
		}
	}
	return ""
}
