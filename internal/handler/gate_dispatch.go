package handler

// gate_dispatch.go — Gate node dispatch + GateDecisionPayload routing (T-IMPL-010).
//
// DispatchGateNode is the daemon-side entry point for evaluating a gate node.
// It receives the bound Gate ControlPoint via gate_ref, invokes the gate
// evaluator, and returns an Outcome with kind=gate_decision carrying a
// GateDecisionPayload. The cascade routes the outcome as follows:
//
//   - allow  → Outcome status=SUCCESS (SUCCESS path)
//   - deny   → Outcome status=FAIL    (FAIL path)
//   - escalate-to-human → Outcome status=FAIL + ResolutionSignalID set
//
// The function also emits a gate_decision_recorded event per CP §6.5.
//
// Status coupling follows CP-058 (gate-semantics owner): a deny is a
// successfully-evaluated Gate conceptually, but the cascade needs status=FAIL
// to route to the FAIL path. See OQ bead hk-lt0w7 for the tracking item.
//
// Spec refs:
//   - specs/control-points.md §4.12-4.13 (CP-053, CP-054, CP-058)
//   - specs/control-points.md §6.5 (gate_decision_recorded event)
//   - specs/workflow-graph.md WG-005/WG-006 (gate node attributes)
//   - specs/execution-model.md §4.1 EM-005b (gate_decision outcome kind)
//
// Bead ref: hk-jtxnr (T-IMPL-010).
// Tags: mechanism

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// GateEvalFunc is the function type the daemon provides to evaluate a gate
// node's policy. It receives the run, node, and gate_ref, and returns the
// gate evaluator's structured decision as a GateDecisionPayload.
//
// A nil GateDecisionPayload return is a programming error — the evaluator
// MUST always produce a decision. Errors indicate evaluation infrastructure
// failure (not a deny verdict).
type GateEvalFunc func(ctx context.Context, run *core.Run, nodeID core.NodeID, gateRef core.GateRef) (*core.GateDecisionPayload, error)

// GateDispatchResult is the result of DispatchGateNode.
type GateDispatchResult struct {
	// Outcome is the Outcome record with kind=gate_decision suitable for
	// feeding into the cascade. Status is SUCCESS for allow, FAIL for
	// deny/escalate.
	Outcome core.Outcome

	// Decision is the raw GateDecisionPayload for caller inspection.
	Decision *core.GateDecisionPayload
}

// ErrGateDispatch wraps gate dispatch errors with structured context.
type ErrGateDispatch struct {
	NodeID core.NodeID
	Reason string
	Cause  error
}

func (e *ErrGateDispatch) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("gate_dispatch(%s): %s: %v", e.NodeID, e.Reason, e.Cause)
	}
	return fmt.Sprintf("gate_dispatch(%s): %s", e.NodeID, e.Reason)
}

func (e *ErrGateDispatch) Unwrap() error { return e.Cause }

// DispatchGateNode evaluates a gate node and returns the cascade-ready Outcome.
//
// Steps:
//  1. Validate that gateRef is non-empty (structural guard).
//  2. Call evalFn to obtain the GateDecisionPayload.
//  3. Validate the returned payload (GateDecisionPayload.Valid).
//  4. Build the Outcome with kind=gate_decision and appropriate status.
//  5. Emit gate_decision_recorded event via the event bus.
//  6. Return the GateDispatchResult.
//
// The cascade reads outcome.Status to route:
//   - allow → SUCCESS path
//   - deny → FAIL path
//   - escalate-to-human → FAIL path (with ResolutionSignalID in payload)
//
// A handler that cannot evaluate the Gate returns an error (not a deny).
func DispatchGateNode(
	ctx context.Context,
	run *core.Run,
	nodeID core.NodeID,
	gateRef core.GateRef,
	evalFn GateEvalFunc,
	bus eventbus.EventBus,
) (*GateDispatchResult, error) {
	// Step 1 — validate gate_ref.
	if !gateRef.Valid() {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "gate_ref is empty or invalid",
		}
	}

	// Step 2 — invoke the gate evaluator.
	decision, err := evalFn(ctx, run, nodeID, gateRef)
	if err != nil {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "evaluator failed",
			Cause:  err,
		}
	}
	if decision == nil {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "evaluator returned nil decision",
		}
	}

	// Step 3 — validate the payload.
	if !decision.Valid() {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "evaluator returned invalid GateDecisionPayload",
		}
	}

	// Step 4 — build the Outcome.
	//
	// Status mapping per CP-058 (gate-semantics owner):
	//   allow → SUCCESS (gate permitted the transition)
	//   deny  → FAIL    (gate blocked the transition; cascade routes to FAIL path)
	//   escalate-to-human → FAIL (run needs external resolution)
	var status core.OutcomeStatus
	switch decision.Decision {
	case core.GateActionAllow:
		status = core.OutcomeStatusSuccess
	case core.GateActionDeny, core.GateActionEscalateToHuman:
		status = core.OutcomeStatusFail
	default:
		// Unknown GateAction: structural error (GateDecisionPayload.Valid should
		// have caught this, but defense-in-depth).
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "unknown gate action: " + string(decision.Decision),
		}
	}

	outcome := core.Outcome{
		Status:  status,
		Kind:    core.OutcomeKindGateDecision,
		Payload: decision,
	}

	// Validate the complete Outcome (defense-in-depth).
	if !outcome.Valid() {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "constructed Outcome failed Valid()",
		}
	}

	// Step 5 — emit gate_decision_recorded event (CP §6.5).
	eventPayload := core.GateDecisionRecordedPayload{
		RunID:         run.RunID,
		NodeID:        nodeID,
		PolicyID:      decision.PolicyID,
		Decision:      decision.Decision,
		DecisionActor: decision.DecisionActor,
		OutcomeStatus: status,
	}
	if emitErr := emitGateEvent(ctx, bus, run.RunID, eventPayload); emitErr != nil {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "emit gate_decision_recorded failed",
			Cause:  emitErr,
		}
	}

	return &GateDispatchResult{
		Outcome:  outcome,
		Decision: decision,
	}, nil
}

// emitGateEvent marshals the payload and emits it on the event bus.
func emitGateEvent(ctx context.Context, bus eventbus.EventBus, runID core.RunID, payload core.GateDecisionRecordedPayload) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal gate_decision_recorded: %w", err)
	}
	return bus.EmitWithRunID(ctx, runID, core.EventTypeGateDecisionRecorded, b)
}
