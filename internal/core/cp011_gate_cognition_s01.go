package core

// cp011_gate_cognition_s01.go — cognition-tagged Gate evaluator invocation per CP-011.
//
// Implements specs/control-points.md §4.2.CP-011:
//
//	CP-011 — Gate evaluator MAY be cognition-tagged
//	A Gate's evaluator MAY be cognition-tagged (delegating to a model) when the
//	policy requires judgment that a mechanism-tagged expression cannot express.
//	Cognition-tagged Gate evaluators MUST satisfy the replay-safety contract of
//	§4.8 (persisted-verdict).
//
// InvokeCognitionGate is the §7.2 invocation path for cognition-tagged Gate
// ControlPoints. It enforces the three-path logic per §7.2:
//  1. Existing verdict with matching envelope hash → return persisted verdict (replay).
//  2. Existing verdict with mismatching hash → return ErrGateVerdictEnvelopeMismatch
//     (caller MUST emit verdict_envelope_mismatch and escalate to Cat 6 per CP-041).
//  3. No existing verdict → dispatch to role via eval, stamp mechanical fields,
//     return the verdict. The caller MUST persist the returned GateVerdictRecord to
//     Transition.Evidence via Evidence.SetGateVerdict BEFORE the transition
//     advances (CP-040).
//
// Tags: mechanism (replay check, hash computation, field-stamping);
//
//	cognition (role dispatch via CognitionGateEvaluator — boundary per CP-042).
//
// Refs: hk-a8bg.10, hk-021nw

import (
	"context"
	"fmt"
	"time"
)

// CognitionGateEvaluator dispatches a cognition-tagged Gate ControlPoint to the
// declared role and returns a partially-populated GateVerdictRecord per CP-011
// and CP-039.
//
// The returned record MUST have Action set to a valid GateAction. The caller
// (InvokeCognitionGate) stamps GateName, InputEnvelopeHash, and ProducedAt;
// values set by the evaluator for those fields are overwritten.
//
// CognitionMeta SHOULD be populated by the evaluator with the delegation path
// snapshot, model response digest, and token usage when available.
//
// Tags: cognition (the dispatch to the role model is the cognition boundary per
// specs/control-points.md §4.8.CP-042).
type CognitionGateEvaluator interface {
	EvaluateCognitionGate(ctx context.Context, cp ControlPoint, run *Run, chosen Edge, outcome Outcome) (GateVerdictRecord, error)
}

// GateVerdictReader looks up a previously persisted GateVerdictRecord for the
// given (runID, gateName) key per specs/control-points.md §4.8.CP-041.
//
// LookupGateVerdict returns:
//   - (record, true, nil)   when a matching verdict is found.
//   - (zero, false, nil)    when no verdict exists for the key (first invocation).
//   - (zero, false, err)    on I/O error.
//
// Tags: mechanism (the durable-trace read is a deterministic I/O operation).
type GateVerdictReader interface {
	LookupGateVerdict(ctx context.Context, runID RunID, gateName string) (GateVerdictRecord, bool, error)
}

// ErrGateVerdictEnvelopeMismatch is returned by InvokeCognitionGate when an
// existing persisted verdict has an input_envelope_hash that does not match
// the current envelope per specs/control-points.md §4.8.CP-041.
//
// The caller MUST emit a verdict_envelope_mismatch event carrying the stored
// and current hashes, then escalate to Cat 6 reconciliation. Re-invocation
// under the new envelope is only permitted under an explicit Cat 6 verdict.
type ErrGateVerdictEnvelopeMismatch struct {
	GateName    string
	StoredHash  string
	CurrentHash string
}

func (e *ErrGateVerdictEnvelopeMismatch) Error() string {
	return fmt.Sprintf("gate %q: verdict_envelope_mismatch: stored=%s current=%s — escalate to Cat 6",
		e.GateName, e.StoredHash, e.CurrentHash)
}

// InvokeCognitionGate implements the §7.2 cognition-tagged evaluator invocation
// path for Gate ControlPoints per CP-011 / CP-039–CP-042.
//
// # Invocation paths (§7.2)
//
//  1. Existing verdict with matching envelope hash → return the persisted verdict
//     unchanged. No model call is made (CP-INV-003: idempotency=idempotent).
//
//  2. Existing verdict with mismatching hash → return ErrGateVerdictEnvelopeMismatch.
//     The caller MUST emit a verdict_envelope_mismatch event and escalate to Cat 6.
//     Re-invocation under the new envelope is only authorised by a Cat 6 verdict
//     per CP-041.
//
//  3. No existing verdict (first invocation) → dispatch to the role via
//     eval.EvaluateCognitionGate (cognition boundary per CP-042), stamp the
//     mechanical fields (GateName, InputEnvelopeHash, ProducedAt), and return
//     the verdict. The caller MUST call Evidence.SetGateVerdict with the returned
//     record and include it in the Transition.Evidence map BEFORE the checkpoint
//     commit advances the transition per CP-040.
//
// # Precondition
//
// cp MUST be a cognition-tagged Gate ControlPoint:
//   - cp.Evaluator.Mode == ModeTagCognition
//   - cp.Evaluator.DelegationPath != nil
//
// A non-cognition-tagged ControlPoint returns an error immediately.
//
// # Responsibility split (CP-042)
//
// InvokeCognitionGate straddles the mechanism/cognition boundary:
//   - Verdict production (eval.EvaluateCognitionGate) — Tags: cognition
//   - Replay check, hash computation, field-stamping — Tags: mechanism
//
// Verdict persistence (Evidence.SetGateVerdict + Transition commit) is the
// caller's responsibility and MUST happen before the transition advances.
//
// # envelope parameter (CP-040a, hk-021nw)
//
// envelope is the fully-populated five-input CP-040a envelope. The caller MUST
// supply the resolved prompt template body (item 2 — NOT the ref name from
// DelegationPath.PromptTemplateRef, but the body resolved from the template
// registry at invocation time), the skill_packages snapshot from the originating
// handler launch's skills_provisioned event (item 3), the run context subset
// (item 4; ContextSubsetModeConservative = full run.Context when no AST walk is
// performed), and the full policy_meta block from the declaring policy document
// (item 5). ExpressionText (item 1) MUST be nil for cognition-tagged Gate
// evaluators. ComputeInputEnvelopeHash is called internally; the caller must
// not pre-hash the envelope. See [InputEnvelope] for full field semantics.
//
// Spec ref: specs/control-points.md §4.2.CP-011, §4.8 CP-039–CP-042, §7.2.
// Refs: hk-a8bg.10, hk-021nw
func InvokeCognitionGate(
	ctx context.Context,
	cp ControlPoint,
	run *Run,
	chosen Edge,
	outcome Outcome,
	eval CognitionGateEvaluator,
	reader GateVerdictReader,
	envelope InputEnvelope,
) (GateVerdictRecord, error) {
	// Structural invariant: cp must be cognition-tagged with a valid delegation path.
	if cp.Evaluator.Mode != ModeTagCognition || cp.Evaluator.DelegationPath == nil {
		return GateVerdictRecord{}, fmt.Errorf("gate %q: InvokeCognitionGate requires a cognition-tagged ControlPoint (mode=%s)", cp.Name, cp.Evaluator.Mode)
	}

	// CP-040a: compute the input envelope hash for replay-safety checks.
	currentHash, err := ComputeInputEnvelopeHash(envelope)
	if err != nil {
		return GateVerdictRecord{}, fmt.Errorf("gate %q: envelope hash computation failed: %w", cp.Name, err)
	}

	// CP-041: check for an existing persisted verdict before dispatching.
	existing, found, readErr := reader.LookupGateVerdict(ctx, run.RunID, cp.Name)
	if readErr != nil {
		return GateVerdictRecord{}, fmt.Errorf("gate %q: verdict lookup failed: %w", cp.Name, readErr)
	}
	if found {
		if existing.InputEnvelopeHash == currentHash {
			// Hash match: replay path — consume the persisted verdict without
			// re-invoking the model per CP-INV-003 (idempotency=idempotent).
			return existing, nil
		}
		// Hash mismatch: the envelope has drifted since the verdict was persisted.
		// Only a Cat 6 reconciliation verdict can authorise re-invocation per CP-041.
		return GateVerdictRecord{}, &ErrGateVerdictEnvelopeMismatch{
			GateName:    cp.Name,
			StoredHash:  existing.InputEnvelopeHash,
			CurrentHash: currentHash,
		}
	}

	// No prior verdict: first invocation. Dispatch to the declared role per
	// CP-039 / §7.2 dispatch_to_role (cognition boundary per CP-042).
	verdict, dispatchErr := eval.EvaluateCognitionGate(ctx, cp, run, chosen, outcome)
	if dispatchErr != nil {
		return GateVerdictRecord{}, fmt.Errorf("gate %q: cognition dispatch failed: %w", cp.Name, dispatchErr)
	}

	// Stamp the mechanical fields (InvokeCognitionGate owns these per CP-042).
	verdict.GateName = cp.Name
	verdict.InputEnvelopeHash = currentHash
	verdict.ProducedAt = time.Now().UTC().Format(time.RFC3339)

	return verdict, nil
}
