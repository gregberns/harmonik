package handler_test

// gate_dispatch_test.go — requirement-traceable sensors for T-IMPL-010
// (gate node dispatch + GateDecisionPayload routing).
//
// Acceptance criteria coverage (hk-jtxnr):
//  1. Permit routing: allow decision → Outcome status=SUCCESS, kind=gate_decision.
//  2. Deny routing: deny decision → Outcome status=FAIL, kind=gate_decision.
//  3. Invalid payload rejection: evaluator returning invalid GateDecisionPayload
//     produces an error, not an Outcome.
//  4. Event emission: gate_decision_recorded event is emitted with correct fields.
//  5. Escalate routing: escalate-to-human → Outcome status=FAIL with
//     ResolutionSignalID in payload.
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
func (b *gateTestRecordingBus) Seal() error                                            { return nil }
func (b *gateTestRecordingBus) ReplayFrom(_ string, _ core.EventID) error              { return nil }
func (b *gateTestRecordingBus) DeadLetterReplay(_ string, _ *core.EventPattern) error  { return nil }
func (b *gateTestRecordingBus) Drain(_ context.Context) error                          { return nil }

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

	// Outcome must pass Valid().
	if !result.Outcome.Valid() {
		t.Error("Outcome.Valid() = false; want true")
	}
}

// ── (2) Deny routing ────────────────────────────────────────────────────────

// TestDispatchGateNode_DenyRouting verifies that a gate evaluator returning
// GateActionDeny produces an Outcome with status=FAIL and kind=gate_decision.
// The cascade will route this to the FAIL path.
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

	// Outcome status must be FAIL for deny.
	if result.Outcome.Status != core.OutcomeStatusFail {
		t.Errorf("Outcome.Status = %q, want %q", result.Outcome.Status, core.OutcomeStatusFail)
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

	// Outcome must pass Valid().
	if !result.Outcome.Valid() {
		t.Error("Outcome.Valid() = false; want true")
	}
}

// ── (3) Invalid payload rejection ───────────────────────────────────────────

// TestDispatchGateNode_InvalidPayloadRejected verifies that an evaluator
// returning an invalid GateDecisionPayload (e.g., empty PolicyID) produces
// an error, not an Outcome.
func TestDispatchGateNode_InvalidPayloadRejected(t *testing.T) {
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

	_, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err == nil {
		t.Fatal("DispatchGateNode: expected error for invalid payload, got nil")
	}

	// No events should have been emitted.
	if len(bus.events) != 0 {
		t.Errorf("expected 0 events on invalid payload, got %d", len(bus.events))
	}
}

// TestDispatchGateNode_NilDecisionRejected verifies that a nil return from
// the evaluator produces an error.
func TestDispatchGateNode_NilDecisionRejected(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return nil, nil // nil decision is a programming error
	}

	_, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err == nil {
		t.Fatal("DispatchGateNode: expected error for nil decision, got nil")
	}
}

// TestDispatchGateNode_EvaluatorErrorPropagated verifies that an evaluator
// returning an error propagates that error to the caller.
func TestDispatchGateNode_EvaluatorErrorPropagated(t *testing.T) {
	t.Parallel()

	run := gateTestFixtureRun(t)
	bus := &gateTestRecordingBus{}
	nodeID := core.NodeID("gate-review")
	gateRef := core.GateRef("review-gate")

	evalErr := errors.New("policy engine unavailable")
	evalFn := func(_ context.Context, _ *core.Run, _ core.NodeID, _ core.GateRef) (*core.GateDecisionPayload, error) {
		return nil, evalErr
	}

	_, err := handler.DispatchGateNode(context.Background(), run, nodeID, gateRef, evalFn, bus)
	if err == nil {
		t.Fatal("DispatchGateNode: expected error, got nil")
	}
	if !errors.Is(err, evalErr) {
		t.Errorf("expected wrapped evalErr, got: %v", err)
	}
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
// GateActionEscalateToHuman produces an Outcome with status=FAIL and the
// ResolutionSignalID preserved in the payload.
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

	// Status must be FAIL for escalate-to-human.
	if result.Outcome.Status != core.OutcomeStatusFail {
		t.Errorf("Outcome.Status = %q, want %q", result.Outcome.Status, core.OutcomeStatusFail)
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
