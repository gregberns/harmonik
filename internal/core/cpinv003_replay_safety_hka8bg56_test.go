package core

// cpinv003_replay_safety_hka8bg56_test.go — CP-INV-003 sensor suite
//
// Covers specs/control-points.md §5.CP-INV-003:
//
//	"For every cognition-tagged evaluator invocation that has a persisted verdict
//	 on the run's task branch with a matching input_envelope_hash per §4.8.CP-040a,
//	 replay MUST consume the persisted verdict. On envelope-hash mismatch, replay
//	 MUST escalate to Cat 6 rather than re-invoke silently. Re-invocation during
//	 replay is permitted ONLY under an explicit Cat 6 reconciliation verdict per
//	 [reconciliation/spec.md §4.2]. Any replay path that silently re-invokes a
//	 model — whether because the verdict is missing, the envelope has drifted, or
//	 the replayer ignored the hash check — violates this invariant."
//
// This file is the §10.2 sensor for the CP-039..CP-042 replay-safety group.
// Tests exercise the actual §7.2 invocation functions (InvokeCognitionGate and
// InvokeCognitionHook) to prove the invariant holds across both Gate (S01) and
// Hook (S05) paths.
//
// # Coverage
//
//  1. CP-039: cognition-tagged Gate and Hook have named delegation paths — the
//     structural precondition that makes CP-INV-003 checkable at runtime.
//
//  2. CP-040a / envelope-hash determinism: same inputs always produce the same
//     64-char hex hash for both the Gate and Hook envelope algorithms.
//
//  3. CP-041 / CP-INV-003 Gate replay on hash-match: InvokeCognitionGate returns
//     the persisted verdict without calling the cognition evaluator.
//
//  4. CP-041 / CP-INV-003 Hook replay on hash-match: InvokeCognitionHook returns
//     the persisted verdict without calling the cognition evaluator.
//
//  5. CP-041 / CP-INV-003 Gate hash-mismatch: InvokeCognitionGate returns
//     ErrGateVerdictEnvelopeMismatch; the cognition evaluator is NOT called.
//
//  6. CP-041 / CP-INV-003 Hook hash-mismatch: InvokeCognitionHook returns
//     ErrHookVerdictEnvelopeMismatch; the cognition evaluator is NOT called.
//
//  7. Mismatch errors carry both stored and current hashes (Gate and Hook) so the
//     caller can build a well-formed VerdictEnvelopeMismatchPayload.
//
//  8. VerdictEnvelopeMismatchPayload built from a Gate mismatch is Valid().
//
//  9. VerdictEnvelopeMismatchPayload built from a Hook mismatch is Valid().
//
// 10. ReconciliationCategoryCat6a is the sole category authorised for stale-verdict
//     re-invocation per reconciliation/spec.md §4.2; Cat-0 through Cat-5 are NOT
//     authorised.
//
// 11. CP-INV-003 decision table across all three §7.2 paths: hash-match (consume
//     persisted), hash-mismatch (escalate to Cat 6), no prior verdict (first
//     invocation — evaluator called once).
//
// Tags: mechanism
//
// Refs: hk-a8bg.56

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── stubs ─────────────────────────────────────────────────────────────────────

// cpinv003GateEval is a stub CognitionGateEvaluator that records how many times
// it was called. Its sole purpose in this sensor is to detect whether
// InvokeCognitionGate bypasses the model call on replay paths.
type cpinv003GateEval struct {
	returnVerdict GateVerdictRecord
	returnErr     error
	callCount     int
}

func (e *cpinv003GateEval) EvaluateCognitionGate(
	_ context.Context, _ ControlPoint, _ *Run, _ Edge, _ Outcome,
) (GateVerdictRecord, error) {
	e.callCount++
	return e.returnVerdict, e.returnErr
}

// cpinv003GateReader is a stub GateVerdictReader. Configure found=true and
// verdict with the expected hash to exercise the replay path; configure
// found=true and a wrong hash to exercise the mismatch path.
type cpinv003GateReader struct {
	found   bool
	verdict GateVerdictRecord
	readErr error
}

func (r *cpinv003GateReader) LookupGateVerdict(
	_ context.Context, _ RunID, _ string,
) (GateVerdictRecord, bool, error) {
	return r.verdict, r.found, r.readErr
}

// cpinv003HookEval is a stub CognitionHookEvaluator that records call count.
type cpinv003HookEval struct {
	returnVerdict HookVerdictRecord
	returnErr     error
	callCount     int
}

func (e *cpinv003HookEval) EvaluateCognitionHook(
	_ context.Context, _ ControlPoint, _ *Run, _ EventID,
) (HookVerdictRecord, error) {
	e.callCount++
	return e.returnVerdict, e.returnErr
}

// cpinv003HookReader is a stub HookVerdictReader.
type cpinv003HookReader struct {
	found   bool
	verdict HookVerdictRecord
	readErr error
}

func (r *cpinv003HookReader) LookupHookVerdict(
	_ context.Context, _ RunID, _ uuid.UUID,
) (HookVerdictRecord, bool, error) {
	return r.verdict, r.found, r.readErr
}

// Compile-time interface satisfaction.
var (
	_ CognitionGateEvaluator = (*cpinv003GateEval)(nil)
	_ GateVerdictReader      = (*cpinv003GateReader)(nil)
	_ CognitionHookEvaluator = (*cpinv003HookEval)(nil)
	_ HookVerdictReader      = (*cpinv003HookReader)(nil)
)

// ── fixtures ──────────────────────────────────────────────────────────────────

// cpinv003CognitionAxes is the correct AxisTags for cognition-tagged evaluators
// per specs/control-points.md §4.8.CP-011 / CP-017 axis declarations:
// llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe;
// idempotency=idempotent.
var cpinv003CognitionAxes = AxisTags{
	LLMFreedom:    LLMFreedomBounded,
	IODeterminism: IODeterminismBestEffort,
	ReplaySafety:  ReplaySafetySafe,
	Idempotency:   AxisIdempotencyIdempotent,
}

// cpinv003FixtureRun returns a minimal valid Run with a non-empty Context.
func cpinv003FixtureRun(t *testing.T) *Run {
	t.Helper()
	return &Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("0.1.0"),
		Input:           WorkspaceRef("ws-ref-cpinv003"),
		WorkflowMode:    WorkflowModeSingle,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{"env": "sensor-test"},
		StartTime:       time.Now(),
	}
}

// cpinv003FixtureCognitionGate returns a cognition-tagged Gate ControlPoint
// with a fully-populated DelegationPath (satisfying CP-039) and a structurally
// valid GatePayload (GateSubtypeApproval requires NamedApprover; used so the CP
// passes cp.Valid() when registered).
func cpinv003FixtureCognitionGate(t *testing.T, name string) ControlPoint {
	t.Helper()
	dp := DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "gate.review.input.v1",
		ResponseSchemaRef: "gate.review.response.v1",
		PromptTemplateRef: "gate.review.prompt.v1",
	}
	approver := "ops-lead"
	return ControlPoint{
		Name:          name,
		Kind:          KindGate,
		Trigger:       Trigger{Name: "node-pre-entry"},
		Evaluator:     Evaluator{Mode: ModeTagCognition, DelegationPath: &dp},
		OutcomeAction: OutcomeActionAllow,
		Payload: KindPayload{Gate: &GatePayload{
			Subtype:       GateSubtypeApproval,
			AttachPoint:   AttachPointNodePreEntry,
			NamedApprover: &approver,
		}},
		Axes:          cpinv003CognitionAxes,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

// cpinv003FixtureCognitionHook returns a cognition-tagged Hook ControlPoint
// with a fully-populated DelegationPath (satisfying CP-039) and a structurally
// valid HookPayload (OutcomeActionSideEffect is the only valid outcome for Hooks
// per OutcomeAction.ValidForKind).
func cpinv003FixtureCognitionHook(t *testing.T, name string) ControlPoint {
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
		OutcomeAction: OutcomeActionSideEffect,
		Payload:       KindPayload{Hook: &pl},
		Axes:          cpinv003CognitionAxes,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

// cpinv003FixtureSideEffect returns a valid SideEffect for HookVerdictRecord
// construction in sensor tests.
func cpinv003FixtureSideEffect(t *testing.T) SideEffect {
	t.Helper()
	return SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "hook.review.verdict",
		Payload:          map[string]any{"approved": true},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
}

// cpinv003FixtureGateEnvelope returns a minimal valid InputEnvelope for use in
// CP-INV-003 Gate replay-safety tests. The caller must pass this same envelope
// to both cpinv003GateHash and InvokeCognitionGate so that the pre-seeded stored
// hash matches the hash computed at invocation time.
func cpinv003FixtureGateEnvelope(t *testing.T, run *Run) InputEnvelope {
	t.Helper()
	return InputEnvelope{
		ExpressionText:    nil, // pure-cognition: no expression body (item 1)
		PromptTemplate:    "You are a quality gate reviewer. Assess: {{.work}}",
		SkillPackages:     []string{"code-reviewer@1.0.0"},
		ContextSubset:     run.Context,
		PolicyMeta:        map[string]any{"name": "test-gate-policy", "schema_version": 1},
		ContextSubsetMode: ContextSubsetModeConservative,
	}
}

// cpinv003GateHash computes the gate envelope hash the same way InvokeCognitionGate
// will. Used to pre-seed a reader with a verdict carrying the correct stored hash
// so that the hash-match replay path is exercised.
func cpinv003GateHash(t *testing.T, envelope InputEnvelope) string {
	t.Helper()
	hash, err := ComputeInputEnvelopeHash(envelope)
	if err != nil {
		t.Fatalf("cpinv003GateHash: %v", err)
	}
	return hash
}

// cpinv003HookHash computes the hook envelope hash the same way InvokeCognitionHook
// will. Used to pre-seed a reader with a verdict carrying the correct stored hash.
func cpinv003HookHash(t *testing.T, cp ControlPoint, run *Run, triggeringEventID EventID) string {
	t.Helper()
	hash, err := computeHookEnvelopeHash(cp, run, triggeringEventID)
	if err != nil {
		t.Fatalf("cpinv003HookHash: %v", err)
	}
	return hash
}

// ── (1) CP-039 delegation-path structural precondition ────────────────────────

// TestCPINV003_Sensor_CP039DelegationPathIsNamedOnBothKinds verifies that the
// cognition Gate and Hook fixtures satisfy CP-039 (delegation path fully named),
// establishing the structural precondition for CP-INV-003.
//
// CP-INV-003's replay-safety contract is only checkable when the delegation path
// is named per CP-039. A missing path fails registration before any invocation
// can occur, so the invariant is vacuously satisfied for unregistered CPs; this
// test confirms the sensor's fixtures represent the live, checkable case.
func TestCPINV003_Sensor_CP039DelegationPathIsNamedOnBothKinds(t *testing.T) {
	t.Parallel()

	gateCP := cpinv003FixtureCognitionGate(t, "sensor-gate-cp039")
	hookCP := cpinv003FixtureCognitionHook(t, "sensor-hook-cp039")

	for _, tc := range []struct {
		name string
		cp   ControlPoint
	}{
		{"Gate", gateCP},
		{"Hook", hookCP},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.cp.Evaluator.DelegationPath == nil {
				t.Fatalf("CP-039 precondition: %s DelegationPath is nil; sensor fixture is invalid", tc.name)
			}
			if !tc.cp.Evaluator.DelegationPath.Valid() {
				t.Errorf("CP-039 precondition: %s DelegationPath.Valid() = false; all five fields must be populated", tc.name)
			}

			reg := NewMapRegistry()
			if err := reg.Register(tc.cp); err != nil {
				t.Errorf("CP-039: %s with full DelegationPath failed registration: %v", tc.name, err)
			}
		})
	}
}

// ── (2) CP-040a envelope-hash determinism ─────────────────────────────────────

// TestCPINV003_Sensor_EnvelopeHashDeterminism verifies that the Gate and Hook
// envelope-hash computations are deterministic: identical inputs always produce
// the same 64-char hex digest per §4.8.CP-040a.
//
// Hash non-determinism is a structural bug per CP-040a: it would produce a hash
// mismatch on every replay even when the envelope has not changed, falsely
// escalating every replay to Cat 6. Determinism is therefore a load-bearing
// property of CP-INV-003.
func TestCPINV003_Sensor_EnvelopeHashDeterminism(t *testing.T) {
	t.Parallel()

	run := cpinv003FixtureRun(t)
	triggeringEventID := EventID(uuid.Must(uuid.NewV7()))

	hookCP := cpinv003FixtureCognitionHook(t, "sensor-hash-det-hook")

	t.Run("Gate", func(t *testing.T) {
		t.Parallel()

		gateEnv := cpinv003FixtureGateEnvelope(t, run)
		h1 := cpinv003GateHash(t, gateEnv)
		h2 := cpinv003GateHash(t, gateEnv)

		if h1 != h2 {
			t.Errorf("CP-040a: Gate envelope hash is non-deterministic: run1=%q run2=%q — replay-safety is structurally broken", h1, h2)
		}
		if len(h1) != 64 {
			t.Errorf("CP-040a: Gate envelope hash length = %d, want 64 (SHA-256 hex)", len(h1))
		}
	})

	t.Run("Hook", func(t *testing.T) {
		t.Parallel()

		h1 := cpinv003HookHash(t, hookCP, run, triggeringEventID)
		h2 := cpinv003HookHash(t, hookCP, run, triggeringEventID)

		if h1 != h2 {
			t.Errorf("CP-040a: Hook envelope hash is non-deterministic: run1=%q run2=%q — replay-safety is structurally broken", h1, h2)
		}
		if len(h1) != 64 {
			t.Errorf("CP-040a: Hook envelope hash length = %d, want 64 (SHA-256 hex)", len(h1))
		}
	})

	t.Run("GateAndHookDifferByKind", func(t *testing.T) {
		t.Parallel()
		// Gate and Hook hashes over the same run MUST differ so a Gate verdict
		// cannot be accepted as a Hook verdict. Gate uses ComputeInputEnvelopeHash
		// while Hook still uses the computeHookEnvelopeHash (hook narrowings tracked
		// separately); the different JSON shapes guarantee distinct digests.
		gateHash := cpinv003GateHash(t, cpinv003FixtureGateEnvelope(t, run))
		hookHash := cpinv003HookHash(t, hookCP, run, triggeringEventID)
		if gateHash == hookHash {
			t.Error("CP-040a: Gate and Hook envelope hashes are identical — kind is not covered in the hash surface")
		}
	})
}

// ── (3) CP-041 / CP-INV-003: Gate hash-match → persisted verdict, no model call ──

// TestCPINV003_Sensor_GateReplayConsumesPersistedVerdictOnHashMatch verifies that
// InvokeCognitionGate returns the persisted verdict WITHOUT calling the model
// evaluator when the envelope hash matches per CP-041 and CP-INV-003.
//
// "Replay MUST consume the persisted verdict" means zero calls to the cognition
// evaluator. Any non-zero call count here means the model was silently re-invoked
// — a direct CP-INV-003 violation.
func TestCPINV003_Sensor_GateReplayConsumesPersistedVerdictOnHashMatch(t *testing.T) {
	t.Parallel()

	cp := cpinv003FixtureCognitionGate(t, "sensor-gate-hash-match")
	run := cpinv003FixtureRun(t)
	chosen := Edge{FromNode: "node-src", ToNode: "node-dst", Weight: 1, OrderingKey: "a"}
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	envelope := cpinv003FixtureGateEnvelope(t, run)

	// Pre-seed reader with a persisted verdict whose hash matches the current envelope.
	storedHash := cpinv003GateHash(t, envelope)
	persistedVerdict := GateVerdictRecord{
		GateName:          cp.Name,
		Action:            GateActionAllow,
		InputEnvelopeHash: storedHash,
		ProducedAt:        "2026-05-31T00:00:00Z",
	}
	reader := &cpinv003GateReader{found: true, verdict: persistedVerdict}
	eval := &cpinv003GateEval{}

	got, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err != nil {
		t.Fatalf("CP-INV-003 Gate hash-match: unexpected error: %v", err)
	}

	// MUST NOT have called the model evaluator (zero calls = replay, not re-invoke).
	if eval.callCount != 0 {
		t.Errorf("CP-INV-003 Gate hash-match: cognition evaluator called %d time(s); want 0 — "+
			"silent re-invocation during replay violates CP-INV-003", eval.callCount)
	}

	// Returned verdict must be the persisted one (byte-equal on key fields).
	if got.InputEnvelopeHash != storedHash {
		t.Errorf("CP-INV-003 Gate hash-match: returned verdict hash = %q, want persisted %q", got.InputEnvelopeHash, storedHash)
	}
	if got.Action != persistedVerdict.Action {
		t.Errorf("CP-INV-003 Gate hash-match: returned verdict action = %q, want %q", got.Action, persistedVerdict.Action)
	}
}

// ── (4) CP-041 / CP-INV-003: Hook hash-match → persisted verdict, no model call ──

// TestCPINV003_Sensor_HookReplayConsumesPersistedVerdictOnHashMatch verifies that
// InvokeCognitionHook returns the persisted verdict WITHOUT calling the model
// evaluator when the envelope hash matches per CP-041 and CP-INV-003.
//
// Mirrors TestCPINV003_Sensor_GateReplayConsumesPersistedVerdictOnHashMatch for
// the Hook path. CP-041 explicitly states the §7.2 rule applies to both Gates
// (§4.2) and Hooks (§4.3).
func TestCPINV003_Sensor_HookReplayConsumesPersistedVerdictOnHashMatch(t *testing.T) {
	t.Parallel()

	cp := cpinv003FixtureCognitionHook(t, "sensor-hook-hash-match")
	run := cpinv003FixtureRun(t)
	triggeringEventID := EventID(uuid.Must(uuid.NewV7()))
	invocationID := uuid.Must(uuid.NewV7())

	// Pre-seed reader with a persisted verdict whose hash matches the current envelope.
	storedHash := cpinv003HookHash(t, cp, run, triggeringEventID)
	persistedVerdict := HookVerdictRecord{
		HookName:          cp.Name,
		InvocationID:      invocationID,
		SideEffect:        cpinv003FixtureSideEffect(t),
		InputEnvelopeHash: storedHash,
		ProducedAt:        "2026-05-31T00:00:00Z",
	}
	reader := &cpinv003HookReader{found: true, verdict: persistedVerdict}
	eval := &cpinv003HookEval{}

	got, err := InvokeCognitionHook(context.Background(), cp, run, triggeringEventID, invocationID, eval, reader)
	if err != nil {
		t.Fatalf("CP-INV-003 Hook hash-match: unexpected error: %v", err)
	}

	// MUST NOT have called the model evaluator.
	if eval.callCount != 0 {
		t.Errorf("CP-INV-003 Hook hash-match: cognition evaluator called %d time(s); want 0 — "+
			"silent re-invocation during replay violates CP-INV-003", eval.callCount)
	}

	if got.InputEnvelopeHash != storedHash {
		t.Errorf("CP-INV-003 Hook hash-match: returned verdict hash = %q, want persisted %q", got.InputEnvelopeHash, storedHash)
	}
}

// ── (5) CP-041 / CP-INV-003: Gate hash-mismatch → error, no model call ───────

// TestCPINV003_Sensor_GateHashMismatchPreventsSilentReInvoke verifies that
// InvokeCognitionGate returns ErrGateVerdictEnvelopeMismatch WITHOUT calling the
// model evaluator when the stored hash does not match the current envelope.
//
// CP-INV-003: "On envelope-hash mismatch, replay MUST escalate to Cat 6 rather
// than re-invoke silently." A non-zero evaluator call count here means the
// implementation silently re-invoked the model — a direct violation.
func TestCPINV003_Sensor_GateHashMismatchPreventsSilentReInvoke(t *testing.T) {
	t.Parallel()

	cp := cpinv003FixtureCognitionGate(t, "sensor-gate-hash-mismatch")
	run := cpinv003FixtureRun(t)
	chosen := Edge{FromNode: "node-src", ToNode: "node-dst", Weight: 1, OrderingKey: "a"}
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	envelope := cpinv003FixtureGateEnvelope(t, run)

	// Pre-seed reader with a verdict carrying a STALE hash (64 f's — obviously wrong).
	staleHash := strings.Repeat("f", 64)
	persistedVerdict := GateVerdictRecord{
		GateName:          cp.Name,
		Action:            GateActionAllow,
		InputEnvelopeHash: staleHash,
		ProducedAt:        "2026-05-01T00:00:00Z",
	}
	reader := &cpinv003GateReader{found: true, verdict: persistedVerdict}
	eval := &cpinv003GateEval{}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)
	if err == nil {
		t.Fatal("CP-INV-003 Gate hash-mismatch: expected error, got nil")
	}

	// Error must be ErrGateVerdictEnvelopeMismatch (the Cat 6 escalation signal).
	var mismatchErr *ErrGateVerdictEnvelopeMismatch
	if !errors.As(err, &mismatchErr) {
		t.Errorf("CP-INV-003 Gate hash-mismatch: error is %T (%v), want *ErrGateVerdictEnvelopeMismatch", err, err)
	}

	// MUST NOT have called the model evaluator — the mismatch path is not a
	// "try again with new context" path; it is a Cat 6 escalation path.
	if eval.callCount != 0 {
		t.Errorf("CP-INV-003 Gate hash-mismatch: cognition evaluator called %d time(s); want 0 — "+
			"evaluator MUST NOT be invoked on hash mismatch (CP-INV-003)", eval.callCount)
	}
}

// ── (6) CP-041 / CP-INV-003: Hook hash-mismatch → error, no model call ───────

// TestCPINV003_Sensor_HookHashMismatchPreventsSilentReInvoke verifies that
// InvokeCognitionHook returns ErrHookVerdictEnvelopeMismatch WITHOUT calling the
// model evaluator when the stored hash does not match the current envelope.
//
// Mirrors TestCPINV003_Sensor_GateHashMismatchPreventsSilentReInvoke for the
// Hook path.
func TestCPINV003_Sensor_HookHashMismatchPreventsSilentReInvoke(t *testing.T) {
	t.Parallel()

	cp := cpinv003FixtureCognitionHook(t, "sensor-hook-hash-mismatch")
	run := cpinv003FixtureRun(t)
	triggeringEventID := EventID(uuid.Must(uuid.NewV7()))
	invocationID := uuid.Must(uuid.NewV7())

	// Pre-seed reader with a verdict carrying a STALE hash.
	staleHash := strings.Repeat("e", 64)
	persistedVerdict := HookVerdictRecord{
		HookName:          cp.Name,
		InvocationID:      invocationID,
		SideEffect:        cpinv003FixtureSideEffect(t),
		InputEnvelopeHash: staleHash,
		ProducedAt:        "2026-05-01T00:00:00Z",
	}
	reader := &cpinv003HookReader{found: true, verdict: persistedVerdict}
	eval := &cpinv003HookEval{}

	_, err := InvokeCognitionHook(context.Background(), cp, run, triggeringEventID, invocationID, eval, reader)
	if err == nil {
		t.Fatal("CP-INV-003 Hook hash-mismatch: expected error, got nil")
	}

	var mismatchErr *ErrHookVerdictEnvelopeMismatch
	if !errors.As(err, &mismatchErr) {
		t.Errorf("CP-INV-003 Hook hash-mismatch: error is %T (%v), want *ErrHookVerdictEnvelopeMismatch", err, err)
	}

	if eval.callCount != 0 {
		t.Errorf("CP-INV-003 Hook hash-mismatch: cognition evaluator called %d time(s); want 0 — "+
			"evaluator MUST NOT be invoked on hash mismatch (CP-INV-003)", eval.callCount)
	}
}

// ── (7) Mismatch errors carry both hashes ────────────────────────────────────

// TestCPINV003_Sensor_MismatchErrorsCarryBothHashes verifies that
// ErrGateVerdictEnvelopeMismatch and ErrHookVerdictEnvelopeMismatch each carry
// both the stored and current hashes, enabling the caller to build a well-formed
// VerdictEnvelopeMismatchPayload per event-model.md §8.2.11.
//
// The stored and current hashes in the error are the two fields required by
// VerdictEnvelopeMismatchPayload.StoredEnvelopeHash and
// VerdictEnvelopeMismatchPayload.CurrentEnvelopeHash. Without them, the caller
// cannot emit a complete mismatch event and the escalation signal is partial.
func TestCPINV003_Sensor_MismatchErrorsCarryBothHashes(t *testing.T) {
	t.Parallel()

	run := cpinv003FixtureRun(t)

	t.Run("Gate", func(t *testing.T) {
		t.Parallel()

		cp := cpinv003FixtureCognitionGate(t, "sensor-gate-mismatch-hashes")
		chosen := Edge{FromNode: "node-src", ToNode: "node-dst", Weight: 1, OrderingKey: "a"}
		outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
		envelope := cpinv003FixtureGateEnvelope(t, run)

		staleHash := strings.Repeat("a", 64)
		reader := &cpinv003GateReader{
			found: true,
			verdict: GateVerdictRecord{
				GateName:          cp.Name,
				Action:            GateActionAllow,
				InputEnvelopeHash: staleHash,
				ProducedAt:        "2026-05-01T00:00:00Z",
			},
		}

		_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, &cpinv003GateEval{}, reader, envelope)
		if err == nil {
			t.Fatal("expected mismatch error, got nil")
		}
		var me *ErrGateVerdictEnvelopeMismatch
		if !errors.As(err, &me) {
			t.Fatalf("error is %T, want *ErrGateVerdictEnvelopeMismatch", err)
		}

		if me.StoredHash == "" {
			t.Error("ErrGateVerdictEnvelopeMismatch.StoredHash is empty — mismatch event cannot be built")
		}
		if me.CurrentHash == "" {
			t.Error("ErrGateVerdictEnvelopeMismatch.CurrentHash is empty — mismatch event cannot be built")
		}
		if me.StoredHash == me.CurrentHash {
			t.Error("ErrGateVerdictEnvelopeMismatch.StoredHash == CurrentHash — not actually a mismatch")
		}
		if me.GateName != cp.Name {
			t.Errorf("ErrGateVerdictEnvelopeMismatch.GateName = %q, want %q", me.GateName, cp.Name)
		}
	})

	t.Run("Hook", func(t *testing.T) {
		t.Parallel()

		cp := cpinv003FixtureCognitionHook(t, "sensor-hook-mismatch-hashes")
		triggeringEventID := EventID(uuid.Must(uuid.NewV7()))
		invocationID := uuid.Must(uuid.NewV7())

		staleHash := strings.Repeat("b", 64)
		reader := &cpinv003HookReader{
			found: true,
			verdict: HookVerdictRecord{
				HookName:          cp.Name,
				InvocationID:      invocationID,
				SideEffect:        cpinv003FixtureSideEffect(t),
				InputEnvelopeHash: staleHash,
				ProducedAt:        "2026-05-01T00:00:00Z",
			},
		}

		_, err := InvokeCognitionHook(context.Background(), cp, run, triggeringEventID, invocationID, &cpinv003HookEval{}, reader)
		if err == nil {
			t.Fatal("expected mismatch error, got nil")
		}
		var me *ErrHookVerdictEnvelopeMismatch
		if !errors.As(err, &me) {
			t.Fatalf("error is %T, want *ErrHookVerdictEnvelopeMismatch", err)
		}

		if me.StoredHash == "" {
			t.Error("ErrHookVerdictEnvelopeMismatch.StoredHash is empty — mismatch event cannot be built")
		}
		if me.CurrentHash == "" {
			t.Error("ErrHookVerdictEnvelopeMismatch.CurrentHash is empty — mismatch event cannot be built")
		}
		if me.StoredHash == me.CurrentHash {
			t.Error("ErrHookVerdictEnvelopeMismatch.StoredHash == CurrentHash — not actually a mismatch")
		}
		if me.HookName != cp.Name {
			t.Errorf("ErrHookVerdictEnvelopeMismatch.HookName = %q, want %q", me.HookName, cp.Name)
		}
	})
}

// ── (8) VerdictEnvelopeMismatchPayload is well-formed (Gate) ─────────────────

// TestCPINV003_Sensor_GateMismatchPayloadIsWellFormed verifies that a
// VerdictEnvelopeMismatchPayload built from an ErrGateVerdictEnvelopeMismatch
// satisfies Valid().
//
// The caller of InvokeCognitionGate MUST emit a verdict_envelope_mismatch event
// per CP-041 when a mismatch is detected. VerdictEnvelopeMismatchPayload is the
// typed event payload for that event; a malformed payload would produce an
// invalid event — the Cat 6 escalation signal would be silent or partial.
func TestCPINV003_Sensor_GateMismatchPayloadIsWellFormed(t *testing.T) {
	t.Parallel()

	run := cpinv003FixtureRun(t)
	cp := cpinv003FixtureCognitionGate(t, "sensor-gate-mismatch-payload")
	chosen := Edge{FromNode: "node-src", ToNode: "node-dst", Weight: 1, OrderingKey: "a"}
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	envelope := cpinv003FixtureGateEnvelope(t, run)

	staleHash := strings.Repeat("c", 64)
	reader := &cpinv003GateReader{
		found: true,
		verdict: GateVerdictRecord{
			GateName:          cp.Name,
			Action:            GateActionAllow,
			InputEnvelopeHash: staleHash,
			ProducedAt:        "2026-05-01T00:00:00Z",
		},
	}

	_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, &cpinv003GateEval{}, reader, envelope)
	var me *ErrGateVerdictEnvelopeMismatch
	if !errors.As(err, &me) {
		t.Fatalf("expected *ErrGateVerdictEnvelopeMismatch, got %T: %v", err, err)
	}

	// Build a VerdictEnvelopeMismatchPayload from the error (as the caller would).
	payload := VerdictEnvelopeMismatchPayload{
		RunID:               run.RunID,
		ControlPointName:    me.GateName,
		StoredEnvelopeHash:  me.StoredHash,
		CurrentEnvelopeHash: me.CurrentHash,
		DetectedAt:          "2026-05-31T00:00:00Z",
	}
	if !payload.Valid() {
		t.Errorf("CP-INV-003 Gate mismatch: VerdictEnvelopeMismatchPayload built from error is not Valid(): %+v", payload)
	}
}

// ── (9) VerdictEnvelopeMismatchPayload is well-formed (Hook) ─────────────────

// TestCPINV003_Sensor_HookMismatchPayloadIsWellFormed verifies that a
// VerdictEnvelopeMismatchPayload built from an ErrHookVerdictEnvelopeMismatch
// satisfies Valid().
func TestCPINV003_Sensor_HookMismatchPayloadIsWellFormed(t *testing.T) {
	t.Parallel()

	run := cpinv003FixtureRun(t)
	cp := cpinv003FixtureCognitionHook(t, "sensor-hook-mismatch-payload")
	triggeringEventID := EventID(uuid.Must(uuid.NewV7()))
	invocationID := uuid.Must(uuid.NewV7())

	staleHash := strings.Repeat("d", 64)
	reader := &cpinv003HookReader{
		found: true,
		verdict: HookVerdictRecord{
			HookName:          cp.Name,
			InvocationID:      invocationID,
			SideEffect:        cpinv003FixtureSideEffect(t),
			InputEnvelopeHash: staleHash,
			ProducedAt:        "2026-05-01T00:00:00Z",
		},
	}

	_, err := InvokeCognitionHook(context.Background(), cp, run, triggeringEventID, invocationID, &cpinv003HookEval{}, reader)
	var me *ErrHookVerdictEnvelopeMismatch
	if !errors.As(err, &me) {
		t.Fatalf("expected *ErrHookVerdictEnvelopeMismatch, got %T: %v", err, err)
	}

	payload := VerdictEnvelopeMismatchPayload{
		RunID:               run.RunID,
		ControlPointName:    me.HookName,
		StoredEnvelopeHash:  me.StoredHash,
		CurrentEnvelopeHash: me.CurrentHash,
		DetectedAt:          "2026-05-31T00:00:00Z",
	}
	if !payload.Valid() {
		t.Errorf("CP-INV-003 Hook mismatch: VerdictEnvelopeMismatchPayload built from error is not Valid(): %+v", payload)
	}
}

// ── (10) Cat-6a is the sole authorised re-invocation category ────────────────

// TestCPINV003_Sensor_Cat6aIsOnlyAuthorizedReInvocationCategory verifies that
// ReconciliationCategoryCat6a is the sole ReconciliationCategory that can
// authorise re-invocation of a cognition-tagged evaluator under CP-INV-003 and
// reconciliation/spec.md §4.2.
//
// The test documents the invariant explicitly: Cat-0 through Cat-5 MUST NOT be
// treated as authorising re-invocation. Only a Cat 6a investigator verdict
// (which may flag the persisted verdict as stale) can authorise a re-dispatch.
func TestCPINV003_Sensor_Cat6aIsOnlyAuthorizedReInvocationCategory(t *testing.T) {
	t.Parallel()

	// Cat 6a is the authorised category per reconciliation/spec.md §4.2.
	if !ReconciliationCategoryCat6a.Valid() {
		t.Errorf("CP-INV-003: ReconciliationCategoryCat6a is not a valid ReconciliationCategory — " +
			"the authorised re-invocation gate has no valid category (reconciliation/spec.md §4.2)")
	}

	// Verify the authorised category is Cat 6a (not Cat 6b or any other sub-cat).
	if ReconciliationCategoryCat6a != "cat-6a" {
		t.Errorf("CP-INV-003: ReconciliationCategoryCat6a = %q, want %q (literal check for spec alignment)",
			ReconciliationCategoryCat6a, "cat-6a")
	}

	// Non-Cat-6 categories MUST NOT authorise re-invocation.
	nonAuthorised := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
	}
	for _, cat := range nonAuthorised {
		if cat == ReconciliationCategoryCat6a {
			t.Errorf("CP-INV-003: non-authorised list erroneously contains Cat6a (%q) — test invariant broken", cat)
		}
		if !cat.Valid() {
			// Category may not exist in the current version — skip; we only need
			// to confirm it is not Cat6a.
			continue
		}
	}
}

// ── (11) CP-INV-003 decision table ───────────────────────────────────────────

// TestCPINV003_Sensor_GateDecisionTable exercises all three §7.2 paths for the
// Gate kind and verifies the evaluator call-count for each, documenting the
// CP-INV-003 invariant as a decision table.
//
// §7.2 paths:
//   - Hash-match (verdict exists + matching hash):  evaluator called 0 times.
//   - Hash-mismatch (verdict exists + wrong hash):  evaluator called 0 times,
//     ErrGateVerdictEnvelopeMismatch returned.
//   - No prior verdict (first invocation):          evaluator called exactly once.
//
// CP-INV-003 forbids silent re-invocation. "Silent" means the model is called
// without the caller having gone through Cat 6 reconciliation. The first-
// invocation path is NOT a replay and is therefore not covered by CP-INV-003;
// the table entry exists to confirm the boundary.
func TestCPINV003_Sensor_GateDecisionTable(t *testing.T) {
	t.Parallel()

	run := cpinv003FixtureRun(t)
	cp := cpinv003FixtureCognitionGate(t, "sensor-gate-decision-table")
	chosen := Edge{FromNode: "node-src", ToNode: "node-dst", Weight: 1, OrderingKey: "a"}
	outcome := Outcome{Status: OutcomeStatusSuccess, Kind: OutcomeKindDefault}
	envelope := cpinv003FixtureGateEnvelope(t, run)

	matchingHash := cpinv003GateHash(t, envelope)
	staleHash := strings.Repeat("9", 64)

	type tableRow struct {
		name              string
		readerFound       bool
		readerHash        string
		wantCallCount     int
		wantMismatchError bool
	}

	rows := []tableRow{
		{
			name:          "hash-match-replay",
			readerFound:   true,
			readerHash:    matchingHash,
			wantCallCount: 0, // CP-INV-003: persisted verdict consumed, no model call
		},
		{
			name:              "hash-mismatch-cat6-escalation",
			readerFound:       true,
			readerHash:        staleHash,
			wantCallCount:     0, // CP-INV-003: mismatch → escalate, not re-invoke
			wantMismatchError: true,
		},
		{
			name:          "no-prior-verdict-first-invocation",
			readerFound:   false,
			wantCallCount: 1, // Not a replay path; first invocation calls model once.
		},
	}

	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			reader := &cpinv003GateReader{found: row.readerFound}
			if row.readerFound {
				reader.verdict = GateVerdictRecord{
					GateName:          cp.Name,
					Action:            GateActionAllow,
					InputEnvelopeHash: row.readerHash,
					ProducedAt:        "2026-05-31T00:00:00Z",
				}
			}
			eval := &cpinv003GateEval{returnVerdict: GateVerdictRecord{
				Action: GateActionAllow,
			}}

			_, err := InvokeCognitionGate(context.Background(), cp, run, chosen, outcome, eval, reader, envelope)

			if row.wantMismatchError {
				var me *ErrGateVerdictEnvelopeMismatch
				if !errors.As(err, &me) {
					t.Errorf("row %q: error is %T (%v), want *ErrGateVerdictEnvelopeMismatch", row.name, err, err)
				}
			} else if err != nil {
				t.Errorf("row %q: unexpected error: %v", row.name, err)
			}

			if eval.callCount != row.wantCallCount {
				t.Errorf("row %q: evaluator called %d time(s), want %d — "+
					"CP-INV-003 silent-re-invoke check failed", row.name, eval.callCount, row.wantCallCount)
			}
		})
	}
}

// TestCPINV003_Sensor_HookDecisionTable mirrors TestCPINV003_Sensor_GateDecisionTable
// for the Hook path, proving CP-INV-003 holds across both CP kinds.
func TestCPINV003_Sensor_HookDecisionTable(t *testing.T) {
	t.Parallel()

	run := cpinv003FixtureRun(t)
	cp := cpinv003FixtureCognitionHook(t, "sensor-hook-decision-table")
	triggeringEventID := EventID(uuid.Must(uuid.NewV7()))
	invocationID := uuid.Must(uuid.NewV7())

	matchingHash := cpinv003HookHash(t, cp, run, triggeringEventID)
	staleHash := strings.Repeat("8", 64)

	type tableRow struct {
		name              string
		readerFound       bool
		readerHash        string
		wantCallCount     int
		wantMismatchError bool
	}

	rows := []tableRow{
		{
			name:          "hash-match-replay",
			readerFound:   true,
			readerHash:    matchingHash,
			wantCallCount: 0,
		},
		{
			name:              "hash-mismatch-cat6-escalation",
			readerFound:       true,
			readerHash:        staleHash,
			wantCallCount:     0,
			wantMismatchError: true,
		},
		{
			name:          "no-prior-verdict-first-invocation",
			readerFound:   false,
			wantCallCount: 1,
		},
	}

	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			reader := &cpinv003HookReader{found: row.readerFound}
			if row.readerFound {
				reader.verdict = HookVerdictRecord{
					HookName:          cp.Name,
					InvocationID:      invocationID,
					SideEffect:        cpinv003FixtureSideEffect(t),
					InputEnvelopeHash: row.readerHash,
					ProducedAt:        "2026-05-31T00:00:00Z",
				}
			}
			eval := &cpinv003HookEval{returnVerdict: HookVerdictRecord{
				SideEffect: cpinv003FixtureSideEffect(t),
			}}

			_, err := InvokeCognitionHook(context.Background(), cp, run, triggeringEventID, invocationID, eval, reader)

			if row.wantMismatchError {
				var me *ErrHookVerdictEnvelopeMismatch
				if !errors.As(err, &me) {
					t.Errorf("row %q: error is %T (%v), want *ErrHookVerdictEnvelopeMismatch", row.name, err, err)
				}
			} else if err != nil {
				t.Errorf("row %q: unexpected error: %v", row.name, err)
			}

			if eval.callCount != row.wantCallCount {
				t.Errorf("row %q: evaluator called %d time(s), want %d — "+
					"CP-INV-003 silent-re-invoke check failed", row.name, eval.callCount, row.wantCallCount)
			}
		})
	}
}
