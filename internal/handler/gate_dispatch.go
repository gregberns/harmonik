package handler

// gate_dispatch.go — Gate node dispatch + GateDecisionPayload routing (T-IMPL-010).
//
// DispatchGateNode is the daemon-side entry point for evaluating a gate node.
// It receives the bound Gate ControlPoint via gate_ref, invokes the gate
// evaluator, and returns an Outcome with kind=gate_decision carrying a
// GateDecisionPayload. The status/decision/payload mapping follows CP-058
// (gate-semantics owner; the EM-005b vs CP-058 contradiction was resolved in
// favour of CP-058 per hk-lt0w7):
//
//   - allow             → Outcome status=SUCCESS, kind=gate_decision, payload set
//   - deny              → Outcome status=SUCCESS, kind=gate_decision, payload set
//   - escalate-to-human → Outcome status=SUCCESS, kind=gate_decision, payload set
//                         (ResolutionSignalID populated per CP-058 field 5)
//
// A *successfully-evaluated* Gate is ALWAYS status=SUCCESS regardless of the
// decision — allow/deny/escalate are all valid verdicts, not failures. The
// cascade distinguishes them by routing on the decision, surfaced on
// Outcome.PreferredLabel (the decision string, e.g. "deny") so edge conditions
// of the form `outcome.preferred_label == 'deny'` route correctly per
// workflow-graph.md WG-014 / WG-019. status=FAIL is reserved STRICTLY for a Gate
// that could NOT be evaluated (evaluator returns an error or a nil decision); a
// FAIL gate Outcome carries NO gate_decision payload (it has a failure_class).
//
// The function emits a gate_decision_recorded event per CP §6.5 ONLY on a
// successful evaluation (when there is a decision to record).
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
	"github.com/gregberns/harmonik/internal/handlercontract"
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
	// Outcome is the Outcome record suitable for feeding into the cascade.
	// On a successful evaluation it carries kind=gate_decision with status=SUCCESS
	// (regardless of the decision) and PreferredLabel set to the decision string.
	// On an evaluation FAILURE (evalFn error or nil decision) it carries
	// status=FAIL, kind=default, a FailureClass, and NO payload (CP-058).
	Outcome core.Outcome

	// Decision is the raw GateDecisionPayload for caller inspection. It is nil
	// when the Gate could not be evaluated (the Outcome is a FAIL with no payload).
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
//  1. Validate that gateRef is non-empty (structural guard — returns a Go error).
//  2. Call evalFn. If it returns an error or a nil/invalid decision, the Gate
//     could NOT be evaluated: return a FAIL Outcome (kind=default, no payload,
//     failure_class=structural) and a nil Go error so the cascade can route it.
//  3. On a valid decision, build a SUCCESS Outcome with kind=gate_decision,
//     the payload, and PreferredLabel = the decision string (CP-058: every
//     evaluated Gate is SUCCESS; routing distinguishes the verdicts).
//  4. Emit gate_decision_recorded event via the event bus.
//  5. Return the GateDispatchResult.
//
// Routing per CP-058 / workflow-graph.md WG-019: status=SUCCESS for allow,
// deny, AND escalate-to-human. The cascade distinguishes them by matching
// `outcome.preferred_label == '<decision>'` (or `outcome.kind == gate_decision`).
// status=FAIL is reserved for a Gate that could not be evaluated.
func DispatchGateNode(
	ctx context.Context,
	run *core.Run,
	nodeID core.NodeID,
	gateRef core.GateRef,
	evalFn GateEvalFunc,
	bus handlercontract.EventEmitter,
) (*GateDispatchResult, error) {
	// Step 1 — validate gate_ref. A missing gate_ref is a graph-authoring /
	// configuration error, not an evaluation outcome, so it remains a Go error.
	if !gateRef.Valid() {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "gate_ref is empty or invalid",
		}
	}

	// Step 2 — invoke the gate evaluator. Per CP-058, a Gate that cannot be
	// evaluated returns an Outcome with status=FAILURE, a failure_class, and NO
	// gate_decision payload. We surface that as a FAIL Outcome (not a Go error)
	// so the cascade routes it like any other FAIL outcome.
	decision, err := evalFn(ctx, run, nodeID, gateRef)
	if err != nil {
		return gateEvalFailureResult("evaluator failed: " + err.Error()), nil
	}
	if decision == nil {
		return gateEvalFailureResult("evaluator returned nil decision"), nil
	}
	if !decision.Valid() {
		return gateEvalFailureResult("evaluator returned invalid GateDecisionPayload"), nil
	}

	// Step 3 — build the SUCCESS Outcome. Status is SUCCESS regardless of the
	// decision (allow/deny/escalate are all successful evaluations per CP-058).
	// PreferredLabel carries the decision so the cascade can route on it.
	label := string(decision.Decision)
	outcome := core.Outcome{
		Status:         core.OutcomeStatusSuccess,
		PreferredLabel: &label,
		Kind:           core.OutcomeKindGateDecision,
		Payload:        decision,
	}

	// Validate the complete Outcome (defense-in-depth).
	if !outcome.Valid() {
		return nil, &ErrGateDispatch{
			NodeID: nodeID,
			Reason: "constructed Outcome failed Valid()",
		}
	}

	// Step 4 — emit gate_decision_recorded event (CP §6.5). Only emitted when a
	// decision was actually produced (the eval-failure path has no decision).
	eventPayload := core.GateDecisionRecordedPayload{
		RunID:         run.RunID,
		NodeID:        nodeID,
		PolicyID:      decision.PolicyID,
		Decision:      decision.Decision,
		DecisionActor: decision.DecisionActor,
		OutcomeStatus: core.OutcomeStatusSuccess,
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

// gateEvalFailureResult builds the GateDispatchResult for a Gate that could not
// be evaluated: a FAIL Outcome with NO gate_decision payload and a
// failure_class, per CP-058 ("A handler that cannot evaluate the Gate ... MUST
// return an Outcome with status = FAILURE and a failure_class ...; that Outcome
// MUST NOT carry a gate_decision payload"). The class is `structural` — a Gate
// whose evaluator could not run is an environment/graph-shape failure per the
// workflow-graph.md §7 taxonomy. The Notes field records the reason for
// observability. No gate_decision_recorded event is emitted (there is no
// decision to record).
func gateEvalFailureResult(reason string) *GateDispatchResult {
	fc := core.FailureClassStructural
	return &GateDispatchResult{
		Outcome: core.Outcome{
			Status:       core.OutcomeStatusFail,
			Kind:         core.OutcomeKindDefault,
			FailureClass: &fc,
			Notes:        "gate eval failure: " + reason,
		},
		Decision: nil,
	}
}

// emitGateEvent marshals the payload and emits it on the event bus.
func emitGateEvent(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, payload core.GateDecisionRecordedPayload) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal gate_decision_recorded: %w", err)
	}
	return bus.EmitWithRunID(ctx, runID, core.EventTypeGateDecisionRecorded, b)
}
