package handler_test

// gate_dispatch_test.go — requirement-traceable sensors for T-IMPL-010
// (gate node dispatch + GateDecisionPayload routing).
//
// Acceptance criteria coverage (hk-jtxnr):
//  1. Allow routing: allow decision → Outcome status=SUCCESS, kind=gate_decision,
//     preferred_label="allow".
//  2. Deny routing: deny decision → Outcome status=SUCCESS, kind=gate_decision,
//     preferred_label="deny" (CP-058: a deny is a successful evaluation, hk-lt0w7).
//  3. Eval failure: evaluator returning an error / nil / invalid payload yields a
//     FAIL Outcome with NO gate_decision payload (and a failure_class), not a
//     gate_decision Outcome — distinct from the empty-gate_ref structural Go error.
//  4. Event emission: gate_decision_recorded event is emitted with correct fields.
//  5. Escalate routing: escalate-to-human → Outcome status=SUCCESS with
//     ResolutionSignalID in payload and preferred_label="escalate-to-human".
//
// Spec refs:
//   - specs/control-points.md §4.12-4.13 (CP-053, CP-054, CP-058)
//   - specs/control-points.md §6.5 (gate_decision_recorded event)
//   - specs/execution-model.md §4.1 EM-005b (gate_decision outcome kind)
//
// Bead ref: hk-jtxnr (T-IMPL-010).

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

// ── test-local fixtures ──────────────────────────────────────────────────────

// gateTestRecordedEvent captures a single event emitted by the recording bus.
type gateTestRecordedEvent struct {
	EventType core.EventType
	Payload   json.RawMessage
}

// gateTestRecordingBus is a minimal in-memory event bus for test assertions.
type gateTestRecordingBus struct {
	events []gateTestRecordedEvent
}

func (b *gateTestRecordingBus) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, gateTestRecordedEvent{EventType: eventType, Payload: json.RawMessage(payload)})
	return nil
}

func (b *gateTestRecordingBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, gateTestRecordedEvent{EventType: eventType, Payload: json.RawMessage(payload)})
	return nil
}

func (b *gateTestRecordingBus) Subscribe(_ core.Subscription) (core.Subscription, error) {
	return core.Subscription{}, nil
}
func (b *gateTestRecordingBus) Seal() error                                           { return nil }
func (b *gateTestRecordingBus) ReplayFrom(_ string, _ core.EventID) error             { return nil }
func (b *gateTestRecordingBus) DeadLetterReplay(_ string, _ *core.EventPattern) error { return nil }
func (b *gateTestRecordingBus) Drain(_ context.Context) error                         { return nil }

// gateTestFixtureRun returns a minimal valid Run for gate dispatch tests.
func gateTestFixtureRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0.0"),
		Input:           core.WorkspaceRef("ws-gate-test"),
		WorkflowMode:    core.WorkflowModeSingle,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// gateTestFixtureAllowDecision returns a valid GateDecisionPayload with allow.
func gateTestFixtureAllowDecision() *core.GateDecisionPayload {
	return &core.GateDecisionPayload{
		PolicyID:      "review-gate-policy",
		Decision:      core.GateActionAllow,
		DecisionActor: "mechanism",
	}
}

// gateTestFixtureDenyDecision returns a valid GateDecisionPayload with deny.
func gateTestFixtureDenyDecision() *core.GateDecisionPayload {
	return &core.GateDecisionPayload{
		PolicyID:      "review-gate-policy",
		Decision:      core.GateActionDeny,
		DecisionActor: "reviewer",
	}
}

// gateTestFixtureEscalateDecision returns a valid GateDecisionPayload with
// escalate-to-human and a ResolutionSignalID.
func gateTestFixtureEscalateDecision() *core.GateDecisionPayload {
	sigID := "sig-manual-review-required"
	return &core.GateDecisionPayload{
		PolicyID:           "approval-gate-policy",
		Decision:           core.GateActionEscalateToHuman,
		DecisionActor:      "mechanism",
		ResolutionSignalID: &sigID,
	}
}

// ── (1) Permit routing ──────────────────────────────────────────────────────

// TestDispatchGateNode_AllowRouting verifies that a gate evaluator returning
// GateActionAllow produces an Outcome with status=SUCCESS and kind=gate_decision.
func TestDispatchGateNode_AllowRouting(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return gateTestFixtureAllowDecision(), nil
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected error: %v", err)
	}

	// Outcome status must be SUCCESS for allow.
	if result.Outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("Outcome.Status = %q, want %q", result.Outcome.Status, core.OutcomeStatusSuccess)
	}

	// Outcome kind must be gate_decision.
	if result.Outcome.Kind != core.OutcomeKindGateDecision {
		t.Errorf("Outcome.Kind = %q, want %q", result.Outcome.Kind, core.OutcomeKindGateDecision)
	}

	// Payload must be a *GateDecisionPayload with allow.
	gdp, ok := result.Outcome.Payload.(*core.GateDecisionPayload)
	if !ok || gdp == nil {
		t.Fatalf("Outcome.Payload is not *GateDecisionPayload")
	}
	if gdp.Decision != core.GateActionAllow {
		t.Errorf("GateDecisionPayload.Decision = %q, want %q", gdp.Decision, core.GateActionAllow)
	}

	// PreferredLabel must carry the decision string so the cascade can route.
	if result.Outcome.PreferredLabel == nil || *result.Outcome.PreferredLabel != string(core.GateActionAllow) {
		t.Errorf("Outcome.PreferredLabel = %v, want %q", result.Outcome.PreferredLabel, core.GateActionAllow)
	}

	// Outcome must pass Valid().
	if !result.Outcome.Valid() {
		t.Error("Outcome.Valid() = false; want true")
	}
}

// ── (2) Deny routing ────────────────────────────────────────────────────────

// TestDispatchGateNode_DenyRouting verifies that a gate evaluator returning
// GateActionDeny produces an Outcome with status=SUCCESS and kind=gate_decision
// (CP-058: a deny is a successfully-evaluated Gate, hk-lt0w7). The cascade routes
// on preferred_label=="deny", NOT on status.
func TestDispatchGateNode_DenyRouting(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return gateTestFixtureDenyDecision(), nil
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected error: %v", err)
	}

	// Outcome status must be SUCCESS for deny (CP-058: deny is a successful eval).
	if result.Outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("Outcome.Status = %q, want %q (deny → SUCCESS per CP-058)",
			result.Outcome.Status, core.OutcomeStatusSuccess)
	}

	// Outcome kind must be gate_decision.
	if result.Outcome.Kind != core.OutcomeKindGateDecision {
		t.Errorf("Outcome.Kind = %q, want %q", result.Outcome.Kind, core.OutcomeKindGateDecision)
	}

	// Payload must carry deny.
	gdp, ok := result.Outcome.Payload.(*core.GateDecisionPayload)
	if !ok || gdp == nil {
		t.Fatalf("Outcome.Payload is not *GateDecisionPayload")
	}
	if gdp.Decision != core.GateActionDeny {
		t.Errorf("GateDecisionPayload.Decision = %q, want %q", gdp.Decision, core.GateActionDeny)
	}

	// PreferredLabel must carry "deny" — this is how the cascade distinguishes a
	// deny from an allow (both are status=SUCCESS).
	if result.Outcome.PreferredLabel == nil || *result.Outcome.PreferredLabel != string(core.GateActionDeny) {
		t.Errorf("Outcome.PreferredLabel = %v, want %q", result.Outcome.PreferredLabel, core.GateActionDeny)
	}

	// Outcome must pass Valid().
	if !result.Outcome.Valid() {
		t.Error("Outcome.Valid() = false; want true")
	}
}

// ── (3) Eval-failure path ───────────────────────────────────────────────────

// gateAssertEvalFailureOutcome asserts that a GateDispatchResult is the
// eval-failure shape required by CP-058: status=FAIL, NO gate_decision payload,
// a populated failure_class, and no gate_decision_recorded event.
func gateAssertEvalFailureOutcome(t *testing.T, result *handler.GateDispatchResult, bus *gateTestRecordingBus) {
	t.Helper()
	if result == nil {
		t.Fatal("DispatchGateNode: expected a FAIL GateDispatchResult, got nil")
	}
	if result.Outcome.Status != core.OutcomeStatusFail {
		t.Errorf("Outcome.Status = %q, want %q (eval failure)", result.Outcome.Status, core.OutcomeStatusFail)
	}
	// CP-058: a FAIL gate Outcome MUST NOT carry a gate_decision payload.
	if result.Outcome.Kind == core.OutcomeKindGateDecision {
		t.Errorf("Outcome.Kind = %q on eval failure; want NOT gate_decision (CP-058: no payload)", result.Outcome.Kind)
	}
	if result.Outcome.Payload != nil {
		t.Errorf("Outcome.Payload = %v on eval failure; want nil (CP-058: no payload)", result.Outcome.Payload)
	}
	if result.Decision != nil {
		t.Errorf("result.Decision = %v on eval failure; want nil", result.Decision)
	}
	// A FAIL outcome MUST carry a failure_class so the cascade can route.
	if result.Outcome.FailureClass == nil {
		t.Error("Outcome.FailureClass = nil on eval failure; want a populated class")
	}
	// The Outcome must still be structurally valid (FAIL + no payload is legal).
	if !result.Outcome.Valid() {
		t.Error("Outcome.Valid() = false on eval failure; want true")
	}
	// No gate_decision_recorded event — there was no decision to record.
	if len(bus.events) != 0 {
		t.Errorf("expected 0 events on eval failure, got %d", len(bus.events))
	}
}

// TestDispatchGateNode_InvalidPayloadIsEvalFailure verifies that an evaluator
// returning an invalid GateDecisionPayload (e.g., empty PolicyID) is treated as
// a Gate that could not be evaluated: a FAIL Outcome with no payload, not a
// gate_decision Outcome.
func TestDispatchGateNode_InvalidPayloadIsEvalFailure(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	// Return a GateDecisionPayload with empty PolicyID (invalid per CP-058).
	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return &core.GateDecisionPayload{
			PolicyID:      "", // invalid: required non-empty
			Decision:      core.GateActionAllow,
			DecisionActor: "mechanism",
		}, nil
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected Go error: %v", err)
	}
	gateAssertEvalFailureOutcome(t, result, bus)
}

// TestDispatchGateNode_NilDecisionIsEvalFailure verifies that a nil return from
// the evaluator yields a FAIL Outcome (Gate could not be evaluated).
func TestDispatchGateNode_NilDecisionIsEvalFailure(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return nil, nil // nil decision: evaluator produced no verdict
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected Go error: %v", err)
	}
	gateAssertEvalFailureOutcome(t, result, bus)
}

// TestDispatchGateNode_EvaluatorErrorIsEvalFailure verifies that an evaluator
// returning an error yields a FAIL Outcome (Gate could not be evaluated) rather
// than propagating a Go error — so the cascade routes it like any FAIL outcome.
func TestDispatchGateNode_EvaluatorErrorIsEvalFailure(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalErr := errors.New("policy engine unavailable")
	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return nil, evalErr
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected Go error: %v", err)
	}
	gateAssertEvalFailureOutcome(t, result, bus)
}

// TestDispatchGateNode_EmptyGateRefRejected verifies that an empty gate_ref
// produces an error (structural guard).
func TestDispatchGateNode_EmptyGateRefRejected(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("") // empty — invalid

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		t.Fatal("evaluator should not be called with empty gate_ref")
		return nil, nil
	}

	_, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err == nil {
		t.Fatal("DispatchGateNode: expected error for empty gate_ref, got nil")
	}
}

// ── (4) Event emission ──────────────────────────────────────────────────────

// TestDispatchGateNode_EmitsGateDecisionRecordedEvent verifies that
// gate_decision_recorded is emitted with the correct fields.
func TestDispatchGateNode_EmitsGateDecisionRecordedEvent(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return gateTestFixtureAllowDecision(), nil
	}

	_, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: %v", err)
	}

	// Exactly one event should be emitted.
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}

	evt := bus.events[0]
	if evt.EventType != core.EventTypeGateDecisionRecorded {
		t.Errorf("event type = %q, want %q", evt.EventType, core.EventTypeGateDecisionRecorded)
	}

	// Decode and verify payload fields.
	var payload core.GateDecisionRecordedPayload
	if decErr := json.Unmarshal(evt.Payload, &payload); decErr != nil {
		t.Fatalf("unmarshal GateDecisionRecordedPayload: %v", decErr)
	}
	if payload.RunID != run.RunID {
		t.Errorf("payload.RunID = %v, want %v", payload.RunID, run.RunID)
	}
	if payload.NodeID != nodeID {
		t.Errorf("payload.NodeID = %q, want %q", payload.NodeID, nodeID)
	}
	if payload.PolicyID != "review-gate-policy" {
		t.Errorf("payload.PolicyID = %q, want %q", payload.PolicyID, "review-gate-policy")
	}
	if payload.Decision != core.GateActionAllow {
		t.Errorf("payload.Decision = %q, want %q", payload.Decision, core.GateActionAllow)
	}
	if payload.DecisionActor != "mechanism" {
		t.Errorf("payload.DecisionActor = %q, want %q", payload.DecisionActor, "mechanism")
	}
	if payload.OutcomeStatus != core.OutcomeStatusSuccess {
		t.Errorf("payload.OutcomeStatus = %q, want %q", payload.OutcomeStatus, core.OutcomeStatusSuccess)
	}
}

// ── (5) Escalate routing ────────────────────────────────────────────────────

// TestDispatchGateNode_EscalateRouting verifies that a gate evaluator returning
// GateActionEscalateToHuman produces an Outcome with status=SUCCESS (CP-058: a
// successful escalation verdict), preferred_label="escalate-to-human", and the
// ResolutionSignalID preserved in the payload per CP-058 field 5.
func TestDispatchGateNode_EscalateRouting(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-approval")
	gateRef := core.GateRef("approval-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return gateTestFixtureEscalateDecision(), nil
	}

	result, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err != nil {
		t.Fatalf("DispatchGateNode: unexpected error: %v", err)
	}

	// Status must be SUCCESS for escalate-to-human (CP-058: a successful eval).
	if result.Outcome.Status != core.OutcomeStatusSuccess {
		t.Errorf("Outcome.Status = %q, want %q (escalate → SUCCESS per CP-058)",
			result.Outcome.Status, core.OutcomeStatusSuccess)
	}

	// PreferredLabel must carry "escalate-to-human" for cascade routing.
	if result.Outcome.PreferredLabel == nil ||
		*result.Outcome.PreferredLabel != string(core.GateActionEscalateToHuman) {
		t.Errorf("Outcome.PreferredLabel = %v, want %q",
			result.Outcome.PreferredLabel, core.GateActionEscalateToHuman)
	}

	// Payload must preserve the ResolutionSignalID.
	gdp, ok := result.Outcome.Payload.(*core.GateDecisionPayload)
	if !ok || gdp == nil {
		t.Fatalf("Outcome.Payload is not *GateDecisionPayload")
	}
	if gdp.Decision != core.GateActionEscalateToHuman {
		t.Errorf("Decision = %q, want %q", gdp.Decision, core.GateActionEscalateToHuman)
	}
	if gdp.ResolutionSignalID == nil || *gdp.ResolutionSignalID != "sig-manual-review-required" {
		t.Errorf("ResolutionSignalID = %v, want %q", gdp.ResolutionSignalID, "sig-manual-review-required")
	}

	// Outcome must pass Valid().
	if !result.Outcome.Valid() {
		t.Error("Outcome.Valid() = false; want true")
	}
}
