package core

// cp017_hook_cognition_s05_test.go — conformance tests for CP-017.
//
// specs/control-points.md §4.3.CP-017:
//
//	Hook evaluator MAY be cognition-tagged
//	A Hook's evaluator MAY be cognition-tagged (e.g., an on_review_required Hook
//	that delegates to a reviewer agent). Cognition-tagged Hook evaluators MUST
//	satisfy the replay-safety contract of §4.8. The delegation path — role,
//	model class, input shape, response schema — MUST be named on the Hook record.
//
// Coverage (§7.2 three-path logic + structural invariants):
//  1. First invocation: evaluator called once; mechanical fields are stamped.
//  2. Replay — hash match: evaluator NOT called; persisted verdict returned.
//  3. Replay — hash mismatch: ErrHookVerdictEnvelopeMismatch returned; evaluator NOT called.
//  4. Evaluator error: error propagated; no verdict returned.
//  5. Reader error: error propagated; evaluator NOT called.
//  6. Mechanism-tagged ControlPoint: error returned immediately (invariant guard).
//  7. Cognition-tagged ControlPoint with nil DelegationPath: error returned (invariant guard).
//  8. Mechanical fields (HookName, InvocationID) are stamped from the caller's parameters.
//
// Refs: hk-a8bg.43

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── test-local stubs ──────────────────────────────────────────────────────────

// cp017StubEval is a stub CognitionHookEvaluator that records call count.
type cp017StubEval struct {
	returnVerdict HookVerdictRecord
	returnErr     error
	callCount     int
}

func (e *cp017StubEval) EvaluateCognitionHook(_ context.Context, _ ControlPoint, _ *Run, _ EventID) (HookVerdictRecord, error) {
	e.callCount++
	return e.returnVerdict, e.returnErr
}

// cp017StubReader is a stub HookVerdictReader.
type cp017StubReader struct {
	found   bool
	verdict HookVerdictRecord
	readErr error
}

func (r *cp017StubReader) LookupHookVerdict(_ context.Context, _ RunID, _ uuid.UUID) (HookVerdictRecord, bool, error) {
	return r.verdict, r.found, r.readErr
}

// Compile-time interface satisfaction checks.
var _ CognitionHookEvaluator = (*cp017StubEval)(nil)
var _ HookVerdictReader = (*cp017StubReader)(nil)

// ── fixtures ──────────────────────────────────────────────────────────────────

func cp017FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cp017"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{"env": "test"},
		StartTime:       time.Now(),
	}
}

func cp017FixtureTriggeringEventID(t *testing.T) EventID {
	t.Helper()
	return EventID(uuid.Must(uuid.NewV7()))
}

func cp017FixtureInvocationID(t *testing.T) uuid.UUID {
	t.Helper()
	return uuid.Must(uuid.NewV7())
}

// cp017FixtureCognitionHook returns a cognition-tagged Hook ControlPoint with
// a fully-populated DelegationPath.
func cp017FixtureCognitionHook(t *testing.T, name string) ControlPoint {
	t.Helper()
	dp := DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "hook.review.input.v1",
		ResponseSchemaRef: "hook.review.response.v1",
		PromptTemplateRef: "hook.review.prompt.v1",
	}
	pl := NewHookPayload()
	pl.TriggerEvent = string(HookTriggerOnReviewRequired)
	pl.SideEffectKind = SideEffectKindEmitEvent
	return ControlPoint{
		Name:          name,
		Kind:          KindHook,
		Trigger:       Trigger{Name: string(HookTriggerOnReviewRequired)},
		Evaluator:     Evaluator{Mode: ModeTagCognition, DelegationPath: &dp},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Hook: &pl},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

// cp017FixtureSideEffect returns a valid SideEffect for use in test HookVerdictRecord construction.
func cp017FixtureSideEffect(t *testing.T) SideEffect {
	t.Helper()
	return SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "hook.review.verdict",
		Payload:          map[string]any{"approved": true},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
}

// cp017ComputeExpectedHash computes the hash that InvokeCognitionHook should
// produce for the given cp, run, and triggeringEventID. Used in tests that
// pre-seed the reader with a correctly-hashed persisted verdict.
func cp017ComputeExpectedHash(t *testing.T, cp ControlPoint, run *Run, triggeringEventID EventID) string {
	t.Helper()
	hash, err := computeHookEnvelopeHash(cp, run, triggeringEventID)
	if err != nil {
		t.Fatalf("cp017ComputeExpectedHash: %v", err)
	}
	return hash
}

// ── CP-017 §1: first invocation ───────────────────────────────────────────────

// TestCP017_FirstInvocation_EvaluatorCalledOnce verifies that on first invocation
// (no prior verdict), the cognition evaluator is called exactly once and the
// returned verdict has its mechanical fields stamped.
//
// CP-042: production (cognition) is separate from persistence (mechanism).
// InvokeCognitionHook stamps HookName, InvocationID, InputEnvelopeHash, and
// ProducedAt; the caller owns persistence via the hook verdict file write.
func TestCP017_FirstInvocation_EvaluatorCalledOnce(t *testing.T) {
	t.Parallel()

	cp := cp017FixtureCognitionHook(t, "review-hook")
	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	reason := "policy review required"
	eval := &cp017StubEval{returnVerdict: HookVerdictRecord{
		HookName:          "will-be-overwritten",
		InvocationID:      uuid.Nil, // will be overwritten
		SideEffect:        cp017FixtureSideEffect(t),
		Failed:            false,
		Reason:            &reason,
		InputEnvelopeHash: "will-be-overwritten",
		ProducedAt:        "will-be-overwritten",
	}}
	reader := &cp017StubReader{found: false}

	verdict, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err != nil {
		t.Fatalf("CP-017: unexpected error: %v", err)
	}

	// Evaluator must have been called exactly once (first invocation).
	if eval.callCount != 1 {
		t.Errorf("CP-017: evaluator called %d times, want 1 (first invocation)", eval.callCount)
	}

	// HookName must be stamped from the ControlPoint, not the evaluator's value.
	if verdict.HookName != cp.Name {
		t.Errorf("CP-017: verdict.HookName = %q, want %q (stamped from ControlPoint)", verdict.HookName, cp.Name)
	}

	// InvocationID must be stamped from the caller's invocationID parameter.
	if verdict.InvocationID != invID {
		t.Errorf("CP-017: verdict.InvocationID = %v, want %v (stamped from parameter)", verdict.InvocationID, invID)
	}

	// InputEnvelopeHash must be non-empty and 64-char lowercase hex.
	if len(verdict.InputEnvelopeHash) != 64 {
		t.Errorf("CP-017: InputEnvelopeHash len = %d, want 64 (SHA-256 hex)", len(verdict.InputEnvelopeHash))
	}

	// ProducedAt must be non-empty.
	if verdict.ProducedAt == "" {
		t.Error("CP-017: ProducedAt is empty; InvokeCognitionHook must stamp ProducedAt")
	}
}

// ── CP-017 §2: replay — hash match ───────────────────────────────────────────

// TestCP017_Replay_HashMatch_EvaluatorNotCalled verifies that when a persisted
// verdict exists with a matching envelope hash, the evaluator is NOT called and
// the persisted verdict is returned unchanged.
//
// CP-041: replay MUST consume the persisted verdict when the hash matches.
// CP-INV-003 (idempotency=idempotent): no second model call.
func TestCP017_Replay_HashMatch_EvaluatorNotCalled(t *testing.T) {
	t.Parallel()

	cp := cp017FixtureCognitionHook(t, "approval-hook")
	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	correctHash := cp017ComputeExpectedHash(t, cp, run, trigEvt)

	persistedVerdict := HookVerdictRecord{
		HookName:          "approval-hook",
		InvocationID:      invID,
		SideEffect:        cp017FixtureSideEffect(t),
		Failed:            false,
		InputEnvelopeHash: correctHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp017StubEval{}
	reader := &cp017StubReader{found: true, verdict: persistedVerdict}

	verdict, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err != nil {
		t.Fatalf("CP-017: unexpected error on replay: %v", err)
	}

	// Evaluator MUST NOT be called on replay (idempotency=idempotent).
	if eval.callCount != 0 {
		t.Errorf("CP-017: evaluator called %d times on replay, want 0 "+
			"(CP-INV-003: persisted verdict must be reused without re-invoking the model)", eval.callCount)
	}

	// Returned verdict must be the persisted one.
	if verdict.HookName != persistedVerdict.HookName {
		t.Errorf("CP-017: replay returned verdict.HookName = %q, want %q", verdict.HookName, persistedVerdict.HookName)
	}
	if verdict.InputEnvelopeHash != correctHash {
		t.Errorf("CP-017: replay returned verdict.InputEnvelopeHash = %q, want %q", verdict.InputEnvelopeHash, correctHash)
	}
}

// ── CP-017 §3: replay — hash mismatch ────────────────────────────────────────

// TestCP017_Replay_HashMismatch_ReturnsEnvelopeMismatchError verifies that when
// a persisted verdict exists but its envelope hash does not match the current
// envelope, InvokeCognitionHook returns ErrHookVerdictEnvelopeMismatch.
//
// CP-041: on envelope hash mismatch, the caller MUST escalate to Cat 6.
// Re-invocation is not permitted without an explicit Cat 6 reconciliation verdict.
func TestCP017_Replay_HashMismatch_ReturnsEnvelopeMismatchError(t *testing.T) {
	t.Parallel()

	cp := cp017FixtureCognitionHook(t, "goal-hook")
	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	staleHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	persistedVerdict := HookVerdictRecord{
		HookName:          "goal-hook",
		InvocationID:      invID,
		SideEffect:        cp017FixtureSideEffect(t),
		Failed:            false,
		InputEnvelopeHash: staleHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp017StubEval{}
	reader := &cp017StubReader{found: true, verdict: persistedVerdict}

	_, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err == nil {
		t.Fatal("CP-017: expected ErrHookVerdictEnvelopeMismatch, got nil error")
	}

	var mismatchErr *ErrHookVerdictEnvelopeMismatch
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("CP-017: expected *ErrHookVerdictEnvelopeMismatch, got %T: %v", err, err)
	}

	if mismatchErr.HookName != cp.Name {
		t.Errorf("CP-017: mismatch error HookName = %q, want %q", mismatchErr.HookName, cp.Name)
	}
	if mismatchErr.InvocationID != invID {
		t.Errorf("CP-017: mismatch error InvocationID = %v, want %v", mismatchErr.InvocationID, invID)
	}
	if mismatchErr.StoredHash != staleHash {
		t.Errorf("CP-017: mismatch error StoredHash = %q, want %q", mismatchErr.StoredHash, staleHash)
	}
	if mismatchErr.CurrentHash == "" {
		t.Error("CP-017: mismatch error CurrentHash is empty")
	}
	if mismatchErr.CurrentHash == staleHash {
		t.Error("CP-017: mismatch error StoredHash == CurrentHash; hashes should differ")
	}

	// Evaluator MUST NOT be called on hash mismatch.
	if eval.callCount != 0 {
		t.Errorf("CP-017: evaluator called %d times on hash mismatch, want 0", eval.callCount)
	}
}

// ── CP-017 §4: evaluator error ────────────────────────────────────────────────

// TestCP017_EvaluatorError_PropagatesError verifies that when the cognition
// evaluator returns an error, InvokeCognitionHook propagates it and no verdict
// is returned.
func TestCP017_EvaluatorError_PropagatesError(t *testing.T) {
	t.Parallel()

	cp := cp017FixtureCognitionHook(t, "review-hook")
	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	dispatchErr := fmt.Errorf("cognition dispatch failed: model timeout")
	eval := &cp017StubEval{returnErr: dispatchErr}
	reader := &cp017StubReader{found: false}

	_, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err == nil {
		t.Fatal("CP-017: expected error from evaluator, got nil")
	}
	if !errors.Is(err, dispatchErr) {
		t.Errorf("CP-017: error chain does not include original dispatch error: %v", err)
	}

	// Evaluator was called (dispatch was attempted).
	if eval.callCount != 1 {
		t.Errorf("CP-017: evaluator callCount = %d, want 1 (dispatch was attempted)", eval.callCount)
	}
}

// ── CP-017 §5: reader error ───────────────────────────────────────────────────

// TestCP017_ReaderError_PropagatesError verifies that when the HookVerdictReader
// returns an error, InvokeCognitionHook propagates it and does NOT call the
// evaluator.
func TestCP017_ReaderError_PropagatesError(t *testing.T) {
	t.Parallel()

	cp := cp017FixtureCognitionHook(t, "approval-hook")
	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	readErr := fmt.Errorf("verdict store unavailable: I/O error")
	eval := &cp017StubEval{}
	reader := &cp017StubReader{readErr: readErr}

	_, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err == nil {
		t.Fatal("CP-017: expected error from reader, got nil")
	}
	if !errors.Is(err, readErr) {
		t.Errorf("CP-017: error chain does not include original read error: %v", err)
	}

	// Evaluator MUST NOT be called when the reader fails.
	if eval.callCount != 0 {
		t.Errorf("CP-017: evaluator called %d times despite reader error, want 0", eval.callCount)
	}
}

// ── CP-017 §6: mechanism-tagged ControlPoint rejected ────────────────────────

// TestCP017_MechanismTaggedCP_ReturnsError verifies that InvokeCognitionHook
// returns an error when called with a mechanism-tagged ControlPoint.
func TestCP017_MechanismTaggedCP_ReturnsError(t *testing.T) {
	t.Parallel()

	expr := PolicyExpression("true")
	pl := NewHookPayload()
	pl.TriggerEvent = string(HookTriggerOnAgentCompleted)
	pl.SideEffectKind = SideEffectKindEmitEvent
	cp := ControlPoint{
		Name:          "mechanism-hook",
		Kind:          KindHook,
		Trigger:       Trigger{Name: string(HookTriggerOnAgentCompleted)},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionAllow,
		Payload:       KindPayload{Hook: &pl},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: 1,
	}

	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	eval := &cp017StubEval{}
	reader := &cp017StubReader{}

	_, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err == nil {
		t.Fatal("CP-017: expected error for mechanism-tagged ControlPoint, got nil")
	}

	// Evaluator and reader MUST NOT be called.
	if eval.callCount != 0 {
		t.Errorf("CP-017: evaluator called on mechanism-tagged CP, want 0 calls")
	}
}

// ── CP-017 §7: nil DelegationPath rejected ────────────────────────────────────

// TestCP017_NilDelegationPath_ReturnsError verifies that InvokeCognitionHook
// returns an error when the ControlPoint claims ModeTagCognition but has a nil
// DelegationPath.
func TestCP017_NilDelegationPath_ReturnsError(t *testing.T) {
	t.Parallel()

	pl := NewHookPayload()
	pl.TriggerEvent = string(HookTriggerOnReviewRequired)
	pl.SideEffectKind = SideEffectKindEmitEvent
	cp := ControlPoint{
		Name:          "malformed-cognition-hook",
		Kind:          KindHook,
		Trigger:       Trigger{Name: string(HookTriggerOnReviewRequired)},
		Evaluator:     Evaluator{Mode: ModeTagCognition, DelegationPath: nil},
		OutcomeAction: OutcomeActionAllow,
		Payload:       KindPayload{Hook: &pl},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}

	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	eval := &cp017StubEval{}
	reader := &cp017StubReader{}

	_, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err == nil {
		t.Fatal("CP-017: expected error for nil DelegationPath, got nil")
	}
}

// ── CP-017 §8: mechanical fields are stamped from caller parameters ───────────

// TestCP017_MechanicalFields_StampedFromParameters verifies that HookName and
// InvocationID in the returned verdict are taken from cp.Name and the
// invocationID parameter respectively (not from the evaluator's return value).
//
// CP-042: InvokeCognitionHook (dispatcher) owns the mechanical fields;
// the evaluator's HookName and InvocationID values are overwritten.
func TestCP017_MechanicalFields_StampedFromParameters(t *testing.T) {
	t.Parallel()

	cp := cp017FixtureCognitionHook(t, "canonical-hook-name")
	run := cp017FixtureRun(t)
	trigEvt := cp017FixtureTriggeringEventID(t)
	invID := cp017FixtureInvocationID(t)

	wrongID := uuid.Must(uuid.NewV7())
	eval := &cp017StubEval{returnVerdict: HookVerdictRecord{
		HookName:          "wrong-name-set-by-evaluator",
		InvocationID:      wrongID,
		SideEffect:        cp017FixtureSideEffect(t),
		Failed:            false,
		InputEnvelopeHash: "will-be-overwritten",
		ProducedAt:        "will-be-overwritten",
	}}
	reader := &cp017StubReader{found: false}

	verdict, err := InvokeCognitionHook(context.Background(), cp, run, trigEvt, invID, eval, reader)
	if err != nil {
		t.Fatalf("CP-017: unexpected error: %v", err)
	}

	if verdict.HookName != "canonical-hook-name" {
		t.Errorf("CP-017: verdict.HookName = %q, want %q "+
			"(InvokeCognitionHook must stamp HookName from ControlPoint.Name, not evaluator's value)",
			verdict.HookName, "canonical-hook-name")
	}
	if verdict.InvocationID != invID {
		t.Errorf("CP-017: verdict.InvocationID = %v, want %v "+
			"(InvokeCognitionHook must stamp InvocationID from parameter, not evaluator's value)",
			verdict.InvocationID, invID)
	}
}
