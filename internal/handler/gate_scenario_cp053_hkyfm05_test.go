package handler_test

// gate_scenario_cp053_hkyfm05_test.go — end-to-end scenario test for gate node
// dispatch returning GateDecisionPayload per CP-053/CP-058.
//
// This scenario test exercises the full flow:
//   1. DispatchGateNode produces Outcome with kind=gate_decision and GateDecisionPayload.
//   2. Status is SUCCESS for allow, deny, AND escalate-to-human (CP-058, hk-lt0w7);
//      only an eval-failure is status=FAIL with no payload.
//   3. The cascade (SelectNextEdge / DispatchEdge) routes on the decision, surfaced
//      on outcome.preferred_label, to the correct downstream edge.
//   4. The gate_decision_recorded event is emitted with correct fields.
//
// Unlike the unit tests in gate_dispatch_test.go (which test DispatchGateNode
// in isolation), this file wires the Outcome through the cascade to verify the
// full dispatch → route chain, following the pattern in
// edgecascade_failureclass_wg018_test.go.
//
// Spec refs:
//   - specs/control-points.md §4.12-4.13 (CP-053, CP-054, CP-058)
//   - specs/control-points.md §6.5 (gate_decision_recorded event)
//   - specs/execution-model.md §4.1 EM-005b (gate_decision outcome kind)
//   - specs/execution-model.md §4.10 EM-041 (cascade)
//
// Bead ref: hk-yfm05 (gate scenario test).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
)

// ── scenario fixtures ────────────────────────────────────────────────────────

// scenarioRecordingBus captures events emitted during the scenario.
type scenarioRecordingBus struct {
	events []scenarioRecordedEvent
}

type scenarioRecordedEvent struct {
	EventType core.EventType
	Payload   json.RawMessage
}

func (b *scenarioRecordingBus) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, scenarioRecordedEvent{EventType: eventType, Payload: json.RawMessage(payload)})
	return nil
}

func (b *scenarioRecordingBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, scenarioRecordedEvent{EventType: eventType, Payload: json.RawMessage(payload)})
	return nil
}

func (b *scenarioRecordingBus) Subscribe(_ core.Subscription) (core.Subscription, error) {
	return core.Subscription{}, nil
}
func (b *scenarioRecordingBus) Seal() error                                           { return nil }
func (b *scenarioRecordingBus) ReplayFrom(_ string, _ core.EventID) error             { return nil }
func (b *scenarioRecordingBus) DeadLetterReplay(_ string, _ *core.EventPattern) error { return nil }
func (b *scenarioRecordingBus) Drain(_ context.Context) error                         { return nil }

// scenarioFixtureRun returns a minimal valid Run for scenario tests.
func scenarioFixtureRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0.0"),
		Input:           core.WorkspaceRef("ws-gate-scenario"),
		WorkflowMode:    core.WorkflowModeSingle,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// scenarioConditionEvaluator understands the status- and decision-based
// conditions used in the scenario edges, simulating what the expr-lang
// evaluator produces. Because every evaluated Gate is status=SUCCESS per CP-058,
// the deny/escalate scenarios route on outcome.preferred_label (the decision).
func scenarioConditionEvaluator(expr core.PolicyExpression, _ map[string]any, outcome core.Outcome) bool {
	label := ""
	if outcome.PreferredLabel != nil {
		label = *outcome.PreferredLabel
	}
	switch string(expr) {
	case `outcome.status == 'SUCCESS'`:
		return outcome.Status == core.OutcomeStatusSuccess
	case `outcome.status == 'FAIL'`:
		return outcome.Status == core.OutcomeStatusFail
	case `outcome.preferred_label == 'allow'`:
		return label == string(core.GateActionAllow)
	case `outcome.preferred_label == 'deny'`:
		return label == string(core.GateActionDeny)
	case `outcome.preferred_label == 'escalate-to-human'`:
		return label == string(core.GateActionEscalateToHuman)
	}
	return false
}

// ── (1) Permit: DispatchGateNode → Outcome → cascade routes to SUCCESS path ─

// TestGateScenario_CP053_PermitRoutesToSuccessPath exercises the full flow:
// gate evaluator returns allow → DispatchGateNode produces Outcome(status=SUCCESS,
// kind=gate_decision, payload=GateDecisionPayload) → cascade selects the
// SUCCESS-conditioned edge → DispatchEdge advances to the expected terminal node.
//
// Assertions:
//   - Outcome.Kind == gate_decision with GateDecisionPayload carrying all 5 fields.
//   - Outcome.Status == SUCCESS (allow → SUCCESS per CP-058).
//   - Cascade routes to the SUCCESS edge (not the FAIL edge).
//   - gate_decision_recorded event emitted with correct fields.
func TestGateScenario_CP053_PermitRoutesToSuccessPath(t *testing.T) {
	t.Parallel()

	run := scenarioFixtureRun(t)
	bus := &scenarioRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evidenceRef := "/evidence/review-gate/run-123.json"
	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return &core.GateDecisionPayload{
			PolicyID:            "code-review-policy",
			Decision:            core.GateActionAllow,
			DecisionActor:       "mechanism",
			DecisionEvidenceRef: &evidenceRef,
		}, nil
	}

	// Step 1 — dispatch the gate node.
	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: %v", err)
	}

	// Assert Outcome kind and status.
	if result.Outcome.Kind != core.OutcomeKindGateDecision {
		t.Errorf("Outcome.Kind = %q, want %q", result.Outcome.Kind, core.OutcomeKindGateDecision)
	}
	if result.Outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("Outcome.Status = %q, want %q", result.Outcome.Status, core.OutcomeStatusSuccess)
	}

	// Assert GateDecisionPayload has all 5 fields.
	gdp, ok := result.Outcome.Payload.(*core.GateDecisionPayload)
	if !ok || gdp == nil {
		t.Fatalf("Outcome.Payload is not *GateDecisionPayload")
	}
	if gdp.PolicyID != "code-review-policy" {
		t.Errorf("PolicyID = %q, want %q", gdp.PolicyID, "code-review-policy")
	}
	if gdp.Decision != core.GateActionAllow {
		t.Errorf("Decision = %q, want %q", gdp.Decision, core.GateActionAllow)
	}
	if gdp.DecisionActor != "mechanism" {
		t.Errorf("DecisionActor = %q, want %q", gdp.DecisionActor, "mechanism")
	}
	if gdp.DecisionEvidenceRef == nil || *gdp.DecisionEvidenceRef != evidenceRef {
		t.Errorf("DecisionEvidenceRef = %v, want %q", gdp.DecisionEvidenceRef, evidenceRef)
	}
	// ResolutionSignalID must be nil for allow decisions.
	if gdp.ResolutionSignalID != nil {
		t.Errorf("ResolutionSignalID = %v, want nil (allow decision)", gdp.ResolutionSignalID)
	}

	// Step 2 — feed the Outcome into the cascade and verify routing.
	condSuccess := core.PolicyExpression(`outcome.status == 'SUCCESS'`)
	condFail := core.PolicyExpression(`outcome.status == 'FAIL'`)

	edgeSuccess := core.Edge{
		FromNode:    "gate-review",
		ToNode:      "node-deploy",
		Condition:   &condSuccess,
		Weight:      10,
		OrderingKey: "a",
	}
	edgeFail := core.Edge{
		FromNode:    "gate-review",
		ToNode:      "node-reject",
		Condition:   &condFail,
		Weight:      10,
		OrderingKey: "b",
	}

	cycles := core.NewCycleCounter()
	dispatchResult := core.DispatchEdge(
		run,
		[]core.Edge{edgeFail, edgeSuccess}, // intentionally unordered
		result.Outcome,
		scenarioConditionEvaluator,
		cycles,
		core.IdentityGuard,
		core.PermitGate,
	)

	if !dispatchResult.Advance {
		t.Fatalf("cascade: Advance=false; Stay=%v Escalate=%v Failed=%v FailureClass=%s FailureReason=%s",
			dispatchResult.Stay, dispatchResult.Escalate, dispatchResult.Failed,
			dispatchResult.FailureClass, dispatchResult.FailureReason)
	}
	if dispatchResult.Edge.ToNode != "node-deploy" {
		t.Errorf("cascade routed to %q, want %q (SUCCESS path for allow decision)",
			dispatchResult.Edge.ToNode, "node-deploy")
	}

	// Step 3 — verify gate_decision_recorded event.
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	evt := bus.events[0]
	if evt.EventType != core.EventTypeGateDecisionRecorded {
		t.Errorf("event type = %q, want %q", evt.EventType, core.EventTypeGateDecisionRecorded)
	}
	var payload core.GateDecisionRecordedPayload
	if decErr := json.Unmarshal(evt.Payload, &payload); decErr != nil {
		t.Fatalf("unmarshal GateDecisionRecordedPayload: %v", decErr)
	}
	if payload.RunID != run.RunID {
		t.Errorf("event.RunID = %v, want %v", payload.RunID, run.RunID)
	}
	if payload.NodeID != nodeID {
		t.Errorf("event.NodeID = %q, want %q", payload.NodeID, nodeID)
	}
	if payload.PolicyID != "code-review-policy" {
		t.Errorf("event.PolicyID = %q, want %q", payload.PolicyID, "code-review-policy")
	}
	if payload.Decision != core.GateActionAllow {
		t.Errorf("event.Decision = %q, want %q", payload.Decision, core.GateActionAllow)
	}
	if payload.DecisionActor != "mechanism" {
		t.Errorf("event.DecisionActor = %q, want %q", payload.DecisionActor, "mechanism")
	}
	if payload.OutcomeStatus != core.OutcomeStatusSuccess {
		t.Errorf("event.OutcomeStatus = %q, want %q", payload.OutcomeStatus, core.OutcomeStatusSuccess)
	}
}

// ── (2) Deny: DispatchGateNode → Outcome → cascade routes to FAIL path ──────

// TestGateScenario_CP058_DenyRoutesToDenyEdge exercises the deny flow:
// gate evaluator returns deny → DispatchGateNode produces Outcome(status=SUCCESS,
// kind=gate_decision, preferred_label="deny") → cascade selects the deny-labelled
// edge (routing on the decision, NOT on status — both allow and deny are SUCCESS).
//
// Assertions:
//   - Outcome.Status == SUCCESS (deny is a successfully-evaluated Gate per CP-058).
//   - Outcome.PreferredLabel == "deny".
//   - Cascade routes to the deny edge (not the allow edge), proving status-blind,
//     decision-driven routing.
//   - gate_decision_recorded event has OutcomeStatus=SUCCESS.
func TestGateScenario_CP058_DenyRoutesToDenyEdge(t *testing.T) {
	t.Parallel()

	run := scenarioFixtureRun(t)
	bus := &scenarioRecordingBus{}
	nodeID := core.NodeID("gate-approval")
	gateRef := core.GateRef("approval-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return &core.GateDecisionPayload{
			PolicyID:      "approval-policy",
			Decision:      core.GateActionDeny,
			DecisionActor: "reviewer",
		}, nil
	}

	// Step 1 — dispatch the gate node.
	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: %v", err)
	}

	// Assert Outcome kind and status.
	if result.Outcome.Kind != core.OutcomeKindGateDecision {
		t.Errorf("Outcome.Kind = %q, want %q", result.Outcome.Kind, core.OutcomeKindGateDecision)
	}
	if result.Outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("Outcome.Status = %q, want %q (deny → SUCCESS per CP-058)", result.Outcome.Status, core.OutcomeStatusSuccess)
	}
	if result.Outcome.PreferredLabel == nil || *result.Outcome.PreferredLabel != string(core.GateActionDeny) {
		t.Errorf("Outcome.PreferredLabel = %v, want %q", result.Outcome.PreferredLabel, core.GateActionDeny)
	}

	// Assert GateDecisionPayload.
	gdp, ok := result.Outcome.Payload.(*core.GateDecisionPayload)
	if !ok || gdp == nil {
		t.Fatalf("Outcome.Payload is not *GateDecisionPayload")
	}
	if gdp.Decision != core.GateActionDeny {
		t.Errorf("Decision = %q, want %q", gdp.Decision, core.GateActionDeny)
	}
	if gdp.DecisionActor != "reviewer" {
		t.Errorf("DecisionActor = %q, want %q", gdp.DecisionActor, "reviewer")
	}

	// Step 2 — feed the Outcome into the cascade and verify routing. Both edges
	// would match `outcome.status == 'SUCCESS'`, so the scenario routes on the
	// DECISION via preferred_label — exactly the CP-058 routing channel.
	condAllow := core.PolicyExpression(`outcome.preferred_label == 'allow'`)
	condDeny := core.PolicyExpression(`outcome.preferred_label == 'deny'`)

	edgeAllow := core.Edge{
		FromNode:    "gate-approval",
		ToNode:      "node-proceed",
		Condition:   &condAllow,
		Weight:      10,
		OrderingKey: "a",
	}
	edgeDeny := core.Edge{
		FromNode:    "gate-approval",
		ToNode:      "node-blocked",
		Condition:   &condDeny,
		Weight:      10,
		OrderingKey: "b",
	}

	cycles := core.NewCycleCounter()
	dispatchResult := core.DispatchEdge(
		run,
		[]core.Edge{edgeAllow, edgeDeny}, // intentionally unordered
		result.Outcome,
		scenarioConditionEvaluator,
		cycles,
		core.IdentityGuard,
		core.PermitGate,
	)

	if !dispatchResult.Advance {
		t.Fatalf("cascade: Advance=false; Stay=%v Escalate=%v Failed=%v FailureClass=%s FailureReason=%s",
			dispatchResult.Stay, dispatchResult.Escalate, dispatchResult.Failed,
			dispatchResult.FailureClass, dispatchResult.FailureReason)
	}
	if dispatchResult.Edge.ToNode != "node-blocked" {
		t.Errorf("cascade routed to %q, want %q (deny edge for deny decision)",
			dispatchResult.Edge.ToNode, "node-blocked")
	}

	// Step 3 — verify event has SUCCESS status (deny is a successful evaluation).
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	var payload core.GateDecisionRecordedPayload
	if decErr := json.Unmarshal(bus.events[0].Payload, &payload); decErr != nil {
		t.Fatalf("unmarshal: %v", decErr)
	}
	if payload.Decision != core.GateActionDeny {
		t.Errorf("event.Decision = %q, want %q", payload.Decision, core.GateActionDeny)
	}
	if payload.OutcomeStatus != core.OutcomeStatusSuccess {
		t.Errorf("event.OutcomeStatus = %q, want %q", payload.OutcomeStatus, core.OutcomeStatusSuccess)
	}
}

// TestGateScenario_EvalFailureRoutesToFailEdge exercises the eval-failure flow:
// the evaluator cannot produce a verdict (returns an error) → DispatchGateNode
// produces Outcome(status=FAIL, kind=default, NO payload, failure_class set) →
// cascade routes on `outcome.status == 'FAIL'`. This is the ONLY path that yields
// a FAIL gate outcome per CP-058.
func TestGateScenario_EvalFailureRoutesToFailEdge(t *testing.T) {
	t.Parallel()

	run := scenarioFixtureRun(t)
	bus := &scenarioRecordingBus{}
	nodeID := core.NodeID("gate-eval-fail")
	gateRef := core.GateRef("eval-fail-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return nil, errors.New("policy engine unreachable")
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected Go error: %v", err)
	}

	// Eval failure: FAIL, NO gate_decision payload, failure_class populated.
	if result.Outcome.Status != core.OutcomeStatusFail {
		t.Errorf("Outcome.Status = %q, want %q (eval failure)", result.Outcome.Status, core.OutcomeStatusFail)
	}
	if result.Outcome.Kind == core.OutcomeKindGateDecision {
		t.Errorf("eval-failure Outcome carries kind=gate_decision; CP-058 forbids a payload on FAIL")
	}
	if result.Outcome.Payload != nil {
		t.Errorf("eval-failure Outcome carries a payload; CP-058 forbids it")
	}
	if result.Outcome.FailureClass == nil {
		t.Error("eval-failure Outcome has nil FailureClass; want a populated class")
	}
	if result.Decision != nil {
		t.Errorf("result.Decision = %v on eval failure; want nil", result.Decision)
	}
	// No gate_decision_recorded event on eval failure.
	if len(bus.events) != 0 {
		t.Errorf("expected 0 events on eval failure, got %d", len(bus.events))
	}

	// Cascade routes the FAIL on outcome.status == 'FAIL'.
	condSuccess := core.PolicyExpression(`outcome.status == 'SUCCESS'`)
	condFail := core.PolicyExpression(`outcome.status == 'FAIL'`)
	edgeSuccess := core.Edge{
		FromNode:    "gate-eval-fail",
		ToNode:      "node-proceed",
		Condition:   &condSuccess,
		Weight:      10,
		OrderingKey: "a",
	}
	edgeFail := core.Edge{
		FromNode:    "gate-eval-fail",
		ToNode:      "node-quarantine",
		Condition:   &condFail,
		Weight:      10,
		OrderingKey: "b",
	}
	cycles := core.NewCycleCounter()
	dispatchResult := core.DispatchEdge(
		run,
		[]core.Edge{edgeSuccess, edgeFail},
		result.Outcome,
		scenarioConditionEvaluator,
		cycles,
		core.IdentityGuard,
		core.PermitGate,
	)
	if !dispatchResult.Advance {
		t.Fatalf("cascade: Advance=false; Failed=%v FailureReason=%s",
			dispatchResult.Failed, dispatchResult.FailureReason)
	}
	if dispatchResult.Edge.ToNode != "node-quarantine" {
		t.Errorf("cascade routed to %q, want %q (FAIL path for eval failure)",
			dispatchResult.Edge.ToNode, "node-quarantine")
	}
}

// ── (3) Escalate: deny-like routing + ResolutionSignalID preserved ──────────

// TestGateScenario_CP058_EscalateRoutesToEscalateEdgeWithSignalID exercises
// escalation: gate evaluator returns escalate-to-human → Outcome(status=SUCCESS,
// preferred_label="escalate-to-human") with ResolutionSignalID preserved →
// cascade routes on the decision to the escalate edge.
//
// This verifies that escalate is a SUCCESS verdict (CP-058) and that the
// ResolutionSignalID survives the full flow from evaluator through
// DispatchGateNode through cascade routing.
func TestGateScenario_CP058_EscalateRoutesToEscalateEdgeWithSignalID(t *testing.T) {
	t.Parallel()

	run := scenarioFixtureRun(t)
	bus := &scenarioRecordingBus{}
	nodeID := core.NodeID("gate-manual")
	gateRef := core.GateRef("manual-approval-gate")

	sigID := "sig-needs-human-review"
	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return &core.GateDecisionPayload{
			PolicyID:           "manual-approval-policy",
			Decision:           core.GateActionEscalateToHuman,
			DecisionActor:      "mechanism",
			ResolutionSignalID: &sigID,
		}, nil
	}

	// Step 1 — dispatch the gate node.
	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: %v", err)
	}

	// Status must be SUCCESS for escalate-to-human (CP-058: a successful eval).
	if result.Outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("Outcome.Status = %q, want %q (escalate → SUCCESS per CP-058)",
			result.Outcome.Status, core.OutcomeStatusSuccess)
	}
	if result.Outcome.PreferredLabel == nil ||
		*result.Outcome.PreferredLabel != string(core.GateActionEscalateToHuman) {
		t.Errorf("Outcome.PreferredLabel = %v, want %q",
			result.Outcome.PreferredLabel, core.GateActionEscalateToHuman)
	}

	// ResolutionSignalID must survive in the payload.
	gdp, ok := result.Outcome.Payload.(*core.GateDecisionPayload)
	if !ok || gdp == nil {
		t.Fatalf("Outcome.Payload is not *GateDecisionPayload")
	}
	if gdp.ResolutionSignalID == nil || *gdp.ResolutionSignalID != sigID {
		t.Errorf("ResolutionSignalID = %v, want %q", gdp.ResolutionSignalID, sigID)
	}

	// Step 2 — cascade routes on the decision (preferred_label) to the escalate
	// edge. The allow edge also matches status=SUCCESS, so decision-routing is
	// what distinguishes them.
	condAllow := core.PolicyExpression(`outcome.preferred_label == 'allow'`)
	condEscalate := core.PolicyExpression(`outcome.preferred_label == 'escalate-to-human'`)

	edgeAllow := core.Edge{
		FromNode:    "gate-manual",
		ToNode:      "node-approved",
		Condition:   &condAllow,
		Weight:      10,
		OrderingKey: "a",
	}
	edgeEscalate := core.Edge{
		FromNode:    "gate-manual",
		ToNode:      "node-quarantine",
		Condition:   &condEscalate,
		Weight:      10,
		OrderingKey: "b",
	}

	cycles := core.NewCycleCounter()
	dispatchResult := core.DispatchEdge(
		run,
		[]core.Edge{edgeAllow, edgeEscalate},
		result.Outcome,
		scenarioConditionEvaluator,
		cycles,
		core.IdentityGuard,
		core.PermitGate,
	)

	if !dispatchResult.Advance {
		t.Fatalf("cascade: Advance=false; Failed=%v FailureReason=%s",
			dispatchResult.Failed, dispatchResult.FailureReason)
	}
	if dispatchResult.Edge.ToNode != "node-quarantine" {
		t.Errorf("cascade routed to %q, want %q (escalate edge for escalate decision)",
			dispatchResult.Edge.ToNode, "node-quarantine")
	}
}

// ── (4) Event spy: gate_decision_recorded fields are complete ────────────────

// TestGateScenario_CP053_EventPayloadFieldCompleteness verifies that the
// gate_decision_recorded event emitted during the scenario carries all required
// fields with correct values for both allow and deny decisions.
func TestGateScenario_CP053_EventPayloadFieldCompleteness(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		decision   core.GateAction
		actor      string
		wantStatus core.OutcomeStatus
	}{
		{
			name:       "allow_event",
			decision:   core.GateActionAllow,
			actor:      "mechanism",
			wantStatus: core.OutcomeStatusSuccess,
		},
		{
			name:       "deny_event",
			decision:   core.GateActionDeny,
			actor:      "reviewer",
			wantStatus: core.OutcomeStatusSuccess, // deny is a successful eval per CP-058
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := scenarioFixtureRun(t)
			bus := &scenarioRecordingBus{}

			evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
				return &core.GateDecisionPayload{
					PolicyID:      "completeness-policy",
					Decision:      tc.decision,
					DecisionActor: tc.actor,
				}, nil
			}

			_, err := handler.DispatchGateNode(
				context.Background(), run,
				core.NodeID("gate-completeness"),
				core.GateRef("completeness-gate"),
				evalFn, bus,
			)
			if err != nil {
				t.Fatalf("DispatchGateNode: %v", err)
			}

			if len(bus.events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(bus.events))
			}

			var payload core.GateDecisionRecordedPayload
			if decErr := json.Unmarshal(bus.events[0].Payload, &payload); decErr != nil {
				t.Fatalf("unmarshal: %v", decErr)
			}

			// Validate the event payload is structurally valid.
			if !payload.Valid() {
				t.Error("GateDecisionRecordedPayload.Valid() = false; want true")
			}

			// Verify all required fields.
			if payload.RunID != run.RunID {
				t.Errorf("RunID mismatch")
			}
			if payload.NodeID != "gate-completeness" {
				t.Errorf("NodeID = %q, want %q", payload.NodeID, "gate-completeness")
			}
			if payload.PolicyID != "completeness-policy" {
				t.Errorf("PolicyID = %q, want %q", payload.PolicyID, "completeness-policy")
			}
			if payload.Decision != tc.decision {
				t.Errorf("Decision = %q, want %q", payload.Decision, tc.decision)
			}
			if payload.DecisionActor != tc.actor {
				t.Errorf("DecisionActor = %q, want %q", payload.DecisionActor, tc.actor)
			}
			if payload.OutcomeStatus != tc.wantStatus {
				t.Errorf("OutcomeStatus = %q, want %q", payload.OutcomeStatus, tc.wantStatus)
			}
		})
	}
}
