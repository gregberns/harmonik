package workflow

// dispatcher.go — DOT-mode cascade engine (T-IMPL-008).
//
// DecideNextNode implements the five-step outcome → next-node cascade defined
// in specs/workflow-graph.md §5 WG-010 (citing execution-model.md §4.10
// EM-041) for workflow_mode=dot runs.
//
// The function bridges the dot-AST type layer (internal/workflow/dot) with the
// core cascade primitives (internal/core.SelectNextEdge), producing a
// DispatchDecision that the daemon consumes to advance the run's state machine.
// It also returns a NodeDispatchDecidedPayload ready for the caller to emit as
// a node_dispatch_decided event (O-class per event-model.md).
//
// Spec refs:
//   - specs/workflow-graph.md §5 WG-010 — five-step cascade.
//   - specs/workflow-graph.md §5 WG-011 — unconditional-edge fallback invariant.
//   - specs/workflow-graph.md §5 WG-012 — no-match-set fallback (structural failure).
//   - specs/execution-model.md §4.10 EM-041  — deterministic cascade.
//   - specs/execution-model.md §4.10 EM-043  — traversal cap / compilation_loop.
//   - specs/execution-model.md §4.3 EM-015e  — cap_hit completion_reason vocabulary.
//   - specs/execution-model.md §7.5.2 EM-056 — DOT dispatch equivalence.
//
// Bead ref: hk-bf85t (T-IMPL-008).
// Tags: mechanism

import (
	"strconv"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// DispatchDecision is the result of DecideNextNode.
//
// Exactly one of Advance, IsTerminal, or Failed is true after a successful
// call. The Payload field is always non-nil.
type DispatchDecision struct {
	// Advance is true when the cascade selected a next node.
	Advance bool

	// NextNodeID is the selected next node ID. Non-empty when Advance is true.
	NextNodeID string

	// IsTerminal is true when fromNodeID is in the graph's terminal_node_ids.
	IsTerminal bool

	// Failed is true when the cascade produced no satisfiable match (WG-012 /
	// EM-046a) or the traversal cap was reached (EM-043).
	Failed bool

	// FailureClass carries the cascade failure class when Failed is true.
	FailureClass core.FailureClass

	// FailureReason is the human-readable failure reason when Failed is true.
	FailureReason string

	// CompletionReason is "cap_hit" when the traversal cap was reached per the
	// EM-015d-RFD cap-hit vocabulary (execution-model.md §4.3.EM-015e).
	CompletionReason string

	// Payload is the node_dispatch_decided event payload. Always non-nil.
	Payload *core.NodeDispatchDecidedPayload
}

// DecideNextNode resolves the next node for a dot-mode run by running the
// EM-041 edge-selection cascade (WG-010) against the outgoing edges of
// fromNodeID in graph.
//
// Steps:
//  1. If fromNodeID is in graph.TerminalNodeIDs, return IsTerminal=true.
//  2. Collect outgoing edges from fromNodeID.
//  3. Apply context updates from outcome.ContextUpdates (EM-041a) via
//     core.SelectNextEdge's built-in application.
//  4. Run the EM-041 cascade via core.SelectNextEdge with a DOT-condition
//     evaluator bridging dot.EvalCondition to core.ConditionEvaluator.
//  5. On cap-hit failure set CompletionReason="cap_hit" (EM-015d-RFD vocabulary).
//  6. Populate and return the NodeDispatchDecidedPayload event.
//
// The run's Context map is mutated in-place by the EM-041a context-update step
// (same contract as core.SelectNextEdge).
//
// Preconditions (caller-enforced):
//   - graph, run, and cycles must not be nil.
//   - outcome must satisfy outcome.Valid().
//   - fromNodeID must be a node declared in graph.Nodes.
func DecideNextNode(
	graph *dot.Graph,
	fromNodeID string,
	outcome core.Outcome,
	run *core.Run,
	cycles *core.CycleCounter,
) DispatchDecision {
	for _, tid := range graph.TerminalNodeIDs {
		if tid == fromNodeID {
			return DispatchDecision{
				IsTerminal: true,
				Payload: &core.NodeDispatchDecidedPayload{
					RunID:      run.RunID,
					FromNodeID: fromNodeID,
					IsTerminal: true,
				},
			}
		}
	}

	var outgoing []*dot.Edge
	for _, e := range graph.Edges {
		if e.FromNodeID == fromNodeID {
			outgoing = append(outgoing, e)
		}
	}

	condByRaw := make(map[string]*dot.Condition, len(outgoing))
	for _, e := range outgoing {
		if e.Condition != nil {
			condByRaw[e.ConditionRaw] = e.Condition
		}
	}

	eval := func(expr core.PolicyExpression, ctx map[string]any, o core.Outcome) bool {
		cond, ok := condByRaw[string(expr)]
		if !ok {
			return false
		}
		strCtx := anyToStringMap(ctx)
		matched, err := dot.EvalCondition(cond, o, strCtx)
		if err != nil {
			return false
		}
		return matched
	}

	candidates := make([]core.Edge, 0, len(outgoing))
	for _, e := range outgoing {
		ce := dotEdgeToCoreEdge(e)
		candidates = append(candidates, ce)
	}

	result := core.SelectNextEdge(run, candidates, outcome, eval, cycles)

	if result.Failed {
		completionReason := ""
		if result.FailureClass == core.FailureClassCompilationLoop {
			completionReason = "cap_hit"
		}
		return DispatchDecision{
			Failed:           true,
			FailureClass:     result.FailureClass,
			FailureReason:    result.FailureReason,
			CompletionReason: completionReason,
			Payload: &core.NodeDispatchDecidedPayload{
				RunID:            run.RunID,
				FromNodeID:       fromNodeID,
				Failed:           true,
				FailureClass:     string(result.FailureClass),
				FailureReason:    result.FailureReason,
				CompletionReason: completionReason,
			},
		}
	}

	nextNodeID := string(result.Edge.ToNode)
	return DispatchDecision{
		Advance:    true,
		NextNodeID: nextNodeID,
		Payload: &core.NodeDispatchDecidedPayload{
			RunID:      run.RunID,
			FromNodeID: fromNodeID,
			NextNodeID: nextNodeID,
		},
	}
}

func dotEdgeToCoreEdge(e *dot.Edge) core.Edge {
	ce := core.Edge{
		FromNode:    core.NodeID(e.FromNodeID),
		ToNode:      core.NodeID(e.ToNodeID),
		OrderingKey: e.OrderingKey,
	}

	if ce.OrderingKey == "" {
		ce.OrderingKey = e.ToNodeID
	}

	if e.Condition != nil {
		pe := core.PolicyExpression(e.ConditionRaw)
		ce.Condition = &pe
	}

	if e.PreferredLabel != "" {
		ce.Label = &e.PreferredLabel
	}

	if e.Weight != "" {
		if w, err := strconv.Atoi(e.Weight); err == nil {
			ce.Weight = w
		}
	}

	if raw, ok := e.UnknownAttrs["traversal_cap"]; ok {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			ce.TraversalCap = &n
		}
	}

	return ce
}

func anyToStringMap(ctx map[string]any) map[string]string {
	if len(ctx) == 0 {
		return nil
	}
	out := make(map[string]string, len(ctx))
	for k, v := range ctx {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
