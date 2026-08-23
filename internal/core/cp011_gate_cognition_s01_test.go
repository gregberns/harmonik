package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

type cp011StubEval struct {
	returnVerdict GateVerdictRecord
	returnErr     error
	callCount     int
}

func (e *cp011StubEval) EvaluateCognitionGate(_ context.Context, _ ControlPoint, _ *Run, _ Edge, _ Outcome) (GateVerdictRecord, error) {
	e.callCount++
	return e.returnVerdict, e.returnErr
}

type cp011StubReader struct {
	found   bool
	verdict GateVerdictRecord
	readErr error
}

func (r *cp011StubReader) LookupGateVerdict(_ context.Context, _ RunID, _ string) (GateVerdictRecord, bool, error) {
	return r.verdict, r.found, r.readErr
}

var (
	_ CognitionGateEvaluator = (*cp011StubEval)(nil)
	_ GateVerdictReader      = (*cp011StubReader)(nil)
)

func cp011FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      mustParseWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cp011"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{"env": "test"},
		StartTime:       time.Now(),
	}
}

func cp011FixtureEdge(t *testing.T) Edge {
	t.Helper()
	return Edge{FromNode: "node-src", ToNode: "node-dst", Weight: 1, OrderingKey: "a"}
}

func cp011FixtureOutcome(t *testing.T) Outcome {
	t.Helper()
	return Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
}

func cp011FixtureCognitionGate(t *testing.T, name string) ControlPoint {
	t.Helper()
	dp := DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "gate.review.input.v1",
		ResponseSchemaRef: "gate.review.response.v1",
		PromptTemplateRef: "gate.review.prompt.v1",
	}
	return ControlPoint{
		Name:          name,
		Kind:          KindGate,
		Trigger:       Trigger{Name: "edge-after-selection"},
		Evaluator:     Evaluator{Mode: ModeTagCognition, DelegationPath: &dp},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Gate: &GatePayload{
			Subtype:     GateSubtypeQuality,
			AttachPoint: AttachPointEdgeAfterSelection,
		}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

func cp011FixtureEnvelope(t *testing.T, run *Run) InputEnvelope {
	t.Helper()
	return InputEnvelope{
		ExpressionText:    nil, // pure-cognition: no expression body (item 1)
		PromptTemplate:    "You are a quality reviewer. Assess the work item for merge readiness.",
		SkillPackages:     []string{"code-reviewer@1.0.0"},
		ContextSubset:     run.Context,
		PolicyMeta:        map[string]any{"name": "test-quality-policy", "schema_version": 1},
		ContextSubsetMode: ContextSubsetModeConservative,
	}
}

func cp011ComputeExpectedHash(t *testing.T, envelope InputEnvelope) string {
	t.Helper()
	hash, err := ComputeInputEnvelopeHash(envelope)
	if err != nil {
		t.Fatalf("cp011ComputeExpectedHash: %v", err)
	}
	return hash
}

// TestCP011_FirstInvocation_EvaluatorCalledOnce verifies that on first invocation
// (no prior verdict), the cognition evaluator is called exactly once and the
// returned verdict has its mechanical fields stamped.
//
// CP-042: production (cognition) is separate from persistence (mechanism).
// InvokeCognitionGate stamps GateName, InputEnvelopeHash, and ProducedAt;
// the caller owns persistence via Evidence.SetGateVerdict.
func TestCP011_FirstInvocation_EvaluatorCalledOnce(t *testing.T) {
	t.Parallel()

	cp := cp011FixtureCognitionGate(t, "quality-gate")
	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	reason := "quality bar not met"
	eval := &cp011StubEval{returnVerdict: GateVerdictRecord{
		GateName:          "will-be-overwritten",
		Action:            GateActionDeny,
		Reason:            &reason,
		InputEnvelopeHash: "will-be-overwritten",
		ProducedAt:        "will-be-overwritten",
	}}
	reader := &cp011StubReader{found: false}

	verdict, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err != nil {
		t.Fatalf("CP-011: unexpected error: %v", err)
	}

	if eval.callCount != 1 {
		t.Errorf("CP-011: evaluator called %d times, want 1 (first invocation)", eval.callCount)
	}

	if verdict.GateName != cp.Name {
		t.Errorf("CP-011: verdict.GateName = %q, want %q (stamped from ControlPoint)", verdict.GateName, cp.Name)
	}

	if len(verdict.InputEnvelopeHash) != 64 {
		t.Errorf("CP-011: InputEnvelopeHash len = %d, want 64 (SHA-256 hex)", len(verdict.InputEnvelopeHash))
	}

	if verdict.ProducedAt == "" {
		t.Error("CP-011: ProducedAt is empty; InvokeCognitionGate must stamp ProducedAt")
	}

	if verdict.Action != GateActionDeny {
		t.Errorf("CP-011: verdict.Action = %q, want %q", verdict.Action, GateActionDeny)
	}
}

// TestCP011_Replay_HashMatch_EvaluatorNotCalled verifies that when a persisted
// verdict exists with a matching envelope hash, the evaluator is NOT called and
// the persisted verdict is returned unchanged.
//
// CP-041: replay MUST consume the persisted verdict when the hash matches.
// CP-INV-003 (idempotency=idempotent): no second model call.
func TestCP011_Replay_HashMatch_EvaluatorNotCalled(t *testing.T) {
	t.Parallel()

	cp := cp011FixtureCognitionGate(t, "approval-gate")
	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	correctHash := cp011ComputeExpectedHash(t, envelope)

	persistedVerdict := GateVerdictRecord{
		GateName:          "approval-gate",
		Action:            GateActionAllow,
		InputEnvelopeHash: correctHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp011StubEval{}
	reader := &cp011StubReader{found: true, verdict: persistedVerdict}

	verdict, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err != nil {
		t.Fatalf("CP-011: unexpected error on replay: %v", err)
	}

	if eval.callCount != 0 {
		t.Errorf("CP-011: evaluator called %d times on replay, want 0 "+
			"(CP-INV-003: persisted verdict must be reused without re-invoking the model)", eval.callCount)
	}

	if verdict.GateName != persistedVerdict.GateName {
		t.Errorf("CP-011: replay returned verdict.GateName = %q, want %q", verdict.GateName, persistedVerdict.GateName)
	}
	if verdict.InputEnvelopeHash != correctHash {
		t.Errorf("CP-011: replay returned verdict.InputEnvelopeHash = %q, want %q", verdict.InputEnvelopeHash, correctHash)
	}
	if verdict.Action != GateActionAllow {
		t.Errorf("CP-011: replay returned verdict.Action = %q, want %q", verdict.Action, GateActionAllow)
	}
}

// TestCP011_Replay_HashMismatch_ReturnsEnvelopeMismatchError verifies that when
// a persisted verdict exists but its envelope hash does not match the current
// envelope, InvokeCognitionGate returns ErrGateVerdictEnvelopeMismatch.
//
// CP-041: on envelope hash mismatch, the caller MUST escalate to Cat 6.
// Re-invocation is not permitted without an explicit Cat 6 reconciliation verdict.
func TestCP011_Replay_HashMismatch_ReturnsEnvelopeMismatchError(t *testing.T) {
	t.Parallel()

	cp := cp011FixtureCognitionGate(t, "goal-gate")
	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	staleHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	persistedVerdict := GateVerdictRecord{
		GateName:          "goal-gate",
		Action:            GateActionAllow,
		InputEnvelopeHash: staleHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp011StubEval{}
	reader := &cp011StubReader{found: true, verdict: persistedVerdict}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err == nil {
		t.Fatal("CP-011: expected ErrGateVerdictEnvelopeMismatch, got nil error")
	}

	var mismatchErr *ErrGateVerdictEnvelopeMismatch
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("CP-011: expected *ErrGateVerdictEnvelopeMismatch, got %T: %v", err, err)
	}

	if mismatchErr.GateName != cp.Name {
		t.Errorf("CP-011: mismatch error GateName = %q, want %q", mismatchErr.GateName, cp.Name)
	}
	if mismatchErr.StoredHash != staleHash {
		t.Errorf("CP-011: mismatch error StoredHash = %q, want %q", mismatchErr.StoredHash, staleHash)
	}
	if mismatchErr.CurrentHash == "" {
		t.Error("CP-011: mismatch error CurrentHash is empty")
	}
	if mismatchErr.CurrentHash == staleHash {
		t.Error("CP-011: mismatch error StoredHash == CurrentHash; hashes should differ")
	}

	if eval.callCount != 0 {
		t.Errorf("CP-011: evaluator called %d times on hash mismatch, want 0", eval.callCount)
	}
}

// TestCP011_EvaluatorError_PropagatesError verifies that when the cognition
// evaluator returns an error, InvokeCognitionGate propagates it and no verdict
// is returned.
func TestCP011_EvaluatorError_PropagatesError(t *testing.T) {
	t.Parallel()

	cp := cp011FixtureCognitionGate(t, "quality-gate")
	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	dispatchErr := fmt.Errorf("cognition dispatch failed: model timeout")
	eval := &cp011StubEval{returnErr: dispatchErr}
	reader := &cp011StubReader{found: false}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err == nil {
		t.Fatal("CP-011: expected error from evaluator, got nil")
	}
	if !errors.Is(err, dispatchErr) {
		t.Errorf("CP-011: error chain does not include original dispatch error: %v", err)
	}

	if eval.callCount != 1 {
		t.Errorf("CP-011: evaluator callCount = %d, want 1 (dispatch was attempted)", eval.callCount)
	}
}

// TestCP011_ReaderError_PropagatesError verifies that when the GateVerdictReader
// returns an error, InvokeCognitionGate propagates it and does NOT call the
// evaluator.
func TestCP011_ReaderError_PropagatesError(t *testing.T) {
	t.Parallel()

	cp := cp011FixtureCognitionGate(t, "approval-gate")
	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	readErr := fmt.Errorf("verdict store unavailable: I/O error")
	eval := &cp011StubEval{}
	reader := &cp011StubReader{readErr: readErr}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err == nil {
		t.Fatal("CP-011: expected error from reader, got nil")
	}
	if !errors.Is(err, readErr) {
		t.Errorf("CP-011: error chain does not include original read error: %v", err)
	}

	if eval.callCount != 0 {
		t.Errorf("CP-011: evaluator called %d times despite reader error, want 0", eval.callCount)
	}
}

// TestCP011_MechanismTaggedCP_ReturnsError verifies that InvokeCognitionGate
// returns an error when called with a mechanism-tagged ControlPoint.
//
// This enforces the precondition: InvokeCognitionGate is only valid for
// cognition-tagged Gate ControlPoints.
func TestCP011_MechanismTaggedCP_ReturnsError(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("true")
	cp := ControlPoint{
		Name:          "mechanism-gate",
		Kind:          KindGate,
		Trigger:       Trigger{Name: "edge-after-selection"},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Gate: &GatePayload{
			Subtype:     GateSubtypeGoal,
			AttachPoint: AttachPointEdgeAfterSelection,
		}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}

	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	eval := &cp011StubEval{}
	reader := &cp011StubReader{}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err == nil {
		t.Fatal("CP-011: expected error for mechanism-tagged ControlPoint, got nil")
	}

	if eval.callCount != 0 {
		t.Errorf("CP-011: evaluator called on mechanism-tagged CP, want 0 calls")
	}
}

// TestCP011_NilDelegationPath_ReturnsError verifies that InvokeCognitionGate
// returns an error when the ControlPoint claims ModeTagCognition but has a nil
// DelegationPath.
//
// This would be a malformed ControlPoint (CP-039 requires naming the delegation
// path); InvokeCognitionGate must not proceed with nil DelegationPath.
func TestCP011_NilDelegationPath_ReturnsError(t *testing.T) {
	t.Parallel()

	cp := ControlPoint{
		Name:          "malformed-cognition-gate",
		Kind:          KindGate,
		Trigger:       Trigger{Name: "edge-after-selection"},
		Evaluator:     Evaluator{Mode: ModeTagCognition, DelegationPath: nil},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Gate: &GatePayload{
			Subtype:     GateSubtypeGoal,
			AttachPoint: AttachPointEdgeAfterSelection,
		}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}

	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	eval := &cp011StubEval{}
	reader := &cp011StubReader{}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err == nil {
		t.Fatal("CP-011: expected error for nil DelegationPath, got nil")
	}
}

// TestCP011_GateName_StampedFromControlPoint verifies that the GateName in the
// returned verdict is taken from cp.Name (not from the evaluator's return value).
//
// CP-042: InvokeCognitionGate (dispatcher) owns the mechanical fields;
// the evaluator's GateName value is overwritten.
func TestCP011_GateName_StampedFromControlPoint(t *testing.T) {
	t.Parallel()

	cp := cp011FixtureCognitionGate(t, "canonical-gate-name")
	run := cp011FixtureRun(t)
	chosen := cp011FixtureEdge(t)
	outcome := cp011FixtureOutcome(t)
	envelope := cp011FixtureEnvelope(t, run)

	eval := &cp011StubEval{returnVerdict: GateVerdictRecord{
		GateName:          "wrong-name-set-by-evaluator",
		Action:            GateActionAllow,
		InputEnvelopeHash: "will-be-overwritten",
		ProducedAt:        "will-be-overwritten",
	}}
	reader := &cp011StubReader{found: false}

	verdict, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err != nil {
		t.Fatalf("CP-011: unexpected error: %v", err)
	}

	if verdict.GateName != "canonical-gate-name" {
		t.Errorf("CP-011: verdict.GateName = %q, want %q "+
			"(InvokeCognitionGate must stamp GateName from ControlPoint.Name, not evaluator's value)",
			verdict.GateName, "canonical-gate-name")
	}
}
