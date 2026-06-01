package core

// cp017_hook_cognition_s05.go — cognition-tagged Hook evaluator invocation per CP-017.
//
// Implements specs/control-points.md §4.3.CP-017:
//
//	CP-017 — Hook evaluator MAY be cognition-tagged
//	A Hook's evaluator MAY be cognition-tagged (e.g., an on_review_required Hook
//	that delegates to a reviewer agent). Cognition-tagged Hook evaluators MUST
//	satisfy the replay-safety contract of §4.8. The delegation path — role,
//	model class, input shape, response schema — MUST be named on the Hook record.
//
// InvokeCognitionHook is the §7.2 invocation path for cognition-tagged Hook
// ControlPoints. It enforces the three-path logic per §7.2:
//  1. Existing verdict with matching envelope hash → return persisted verdict (replay).
//  2. Existing verdict with mismatching hash → return ErrHookVerdictEnvelopeMismatch
//     (caller MUST emit verdict_envelope_mismatch and escalate to Cat 6 per CP-041).
//  3. No existing verdict → dispatch to role via eval, stamp mechanical fields,
//     return the verdict. The caller MUST persist the returned HookVerdictRecord to
//     .harmonik/hooks/<run_id>/<invocation_id>.json and emit hook_verdict_persisted
//     per CP-040 BEFORE the hook chain continues.
//
// CP-041 refinement for CP-012: a cognition-tagged Hook's evaluator is invoked
// directly ONLY on the original launch. Every subsequent invocation (replay,
// reconciliation resume, daemon restart) MUST route through §7.2.
//
// Tags: mechanism (replay check, hash computation, field-stamping);
//
//	cognition (role dispatch via CognitionHookEvaluator — boundary per CP-042).
//
// Refs: hk-a8bg.43

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CognitionHookEvaluator dispatches a cognition-tagged Hook ControlPoint to the
// declared role and returns a partially-populated HookVerdictRecord per CP-017
// and CP-039.
//
// The returned record MUST have SideEffect set to a valid SideEffect value. The
// caller (InvokeCognitionHook) stamps HookName, InvocationID,
// InputEnvelopeHash, and ProducedAt; values set by the evaluator for those
// fields are overwritten.
//
// CognitionMeta SHOULD be populated by the evaluator with the delegation path
// snapshot, model response digest, and token usage when available.
//
// Tags: cognition (the dispatch to the role model is the cognition boundary per
// specs/control-points.md §4.8.CP-042).
type CognitionHookEvaluator interface {
	EvaluateCognitionHook(ctx context.Context, cp ControlPoint, run *Run, triggeringEventID EventID) (HookVerdictRecord, error)
}

// HookVerdictReader looks up a previously persisted HookVerdictRecord for the
// given (runID, invocationID) key per specs/control-points.md §4.8.CP-041.
//
// LookupHookVerdict returns:
//   - (record, true, nil)   when a matching verdict is found.
//   - (zero, false, nil)    when no verdict exists for the key (first invocation).
//   - (zero, false, err)    on I/O error.
//
// Tags: mechanism (the durable-trace read is a deterministic I/O operation).
type HookVerdictReader interface {
	LookupHookVerdict(ctx context.Context, runID RunID, invocationID uuid.UUID) (HookVerdictRecord, bool, error)
}

// ErrHookVerdictEnvelopeMismatch is returned by InvokeCognitionHook when an
// existing persisted verdict has an input_envelope_hash that does not match
// the current envelope per specs/control-points.md §4.8.CP-041.
//
// The caller MUST emit a verdict_envelope_mismatch event carrying the stored
// and current hashes, then escalate to Cat 6 reconciliation. Re-invocation
// under the new envelope is only permitted under an explicit Cat 6 verdict.
type ErrHookVerdictEnvelopeMismatch struct {
	HookName     string
	InvocationID uuid.UUID
	StoredHash   string
	CurrentHash  string
}

func (e *ErrHookVerdictEnvelopeMismatch) Error() string {
	return fmt.Sprintf("hook %q (invocation %s): verdict_envelope_mismatch: stored=%s current=%s — escalate to Cat 6",
		e.HookName, e.InvocationID, e.StoredHash, e.CurrentHash)
}

// hookInputEnvelope is the canonical set of inputs hashed to produce the
// InputEnvelopeHash for a cognition-tagged Hook evaluator per
// specs/control-points.md §4.8.CP-040a.
//
// CP-040a declares five envelope inputs. This implementation covers a subset;
// the covered and deferred items are listed below so readers know the exact
// replay-safety surface:
//
// Item 1 — expression_text: nil for Hook ControlPoints (Hooks have no
//
//	mechanism-tagged expression body; the delegation-path ref set serves as
//	the structural identity of the evaluator).
//
// Item 2 — resolved prompt-template body: NOT YET IMPLEMENTED. This envelope
//
//	carries PromptTemplateRef (a registry name) rather than the resolved
//	template body. A template content change that leaves the ref name unchanged
//	will NOT bust the hash and will NOT trigger Cat 6 escalation. Tracked as a
//	known narrowing; full coverage requires a template-resolver pass at
//	invocation time. File a follow-up bead to add resolved body inclusion.
//
// Item 3 — skill_packages snapshot: NOT YET IMPLEMENTED. No skill-package
//
//	snapshot is included in the envelope. A skill-package update will NOT bust
//	the hash. Tracked as a known narrowing; coverage requires the skill-package
//	registry snapshot surface. File a follow-up bead to add snapshotting.
//
// Item 4 — context_subset (conservative fallback): covered. The full run.Context
//
//	map is used since this implementation does not AST-walk delegation-path
//	prompt templates to determine the reachable subset. Any change to run.Context
//	busts the hash and triggers Cat 6 escalation on replay per CP-041. This
//	behaviour is declared explicitly on cognition-tagged Hook ControlPoints per
//	the single-mode rule in CP-040a.
//
// Item 5 — policy_meta block: PARTIALLY COVERED. Only the integer SchemaVersion
//
//	field is included rather than the full policy_meta block declared in CP-040a.
//	Changes to other policy metadata fields (e.g., document hash, policy name)
//	will NOT bust the hash. Tracked as a known narrowing; full coverage requires
//	the PolicyMeta record to be carried on the ControlPoint. File a follow-up
//	bead to promote SchemaVersion to a full PolicyMeta struct.
type hookInputEnvelope struct {
	ControlPointName  string         `json:"control_point_name"`
	DelegationPath    DelegationPath `json:"delegation_path"`
	TriggeringEventID string         `json:"triggering_event_id"`
	RunContext        map[string]any `json:"run_context"`
	SchemaVersion     int            `json:"schema_version"`
}

// computeHookEnvelopeHash returns the SHA-256 hex digest of the canonical JSON
// of the hook's input envelope per specs/control-points.md §4.8.CP-040a.
func computeHookEnvelopeHash(cp ControlPoint, run *Run, triggeringEventID EventID) (string, error) {
	envelope := hookInputEnvelope{
		ControlPointName:  cp.Name,
		DelegationPath:    *cp.Evaluator.DelegationPath,
		TriggeringEventID: uuid.UUID(triggeringEventID).String(),
		RunContext:        run.Context,
		SchemaVersion:     cp.SchemaVersion,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal hook input envelope: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

// InvokeCognitionHook implements the §7.2 cognition-tagged evaluator invocation
// path for Hook ControlPoints per CP-017 / CP-039–CP-042.
//
// # Invocation paths (§7.2)
//
//  1. Existing verdict with matching envelope hash → return the persisted verdict
//     unchanged. No model call is made (CP-INV-003: idempotency=idempotent).
//
//  2. Existing verdict with mismatching hash → return ErrHookVerdictEnvelopeMismatch.
//     The caller MUST emit a verdict_envelope_mismatch event and escalate to Cat 6.
//     Re-invocation under the new envelope is only authorised by a Cat 6 verdict
//     per CP-041.
//
//  3. No existing verdict (first invocation) → dispatch to the role via
//     eval.EvaluateCognitionHook (cognition boundary per CP-042), stamp the
//     mechanical fields (HookName, InvocationID, InputEnvelopeHash, ProducedAt),
//     and return the verdict. The caller MUST persist the returned HookVerdictRecord
//     to .harmonik/hooks/<run_id>/<invocation_id>.json on the task branch and emit
//     a hook_verdict_persisted event per CP-040 BEFORE the hook chain continues.
//
// # Precondition
//
// cp MUST be a cognition-tagged Hook ControlPoint:
//   - cp.Evaluator.Mode == ModeTagCognition
//   - cp.Evaluator.DelegationPath != nil
//
// A non-cognition-tagged ControlPoint returns an error immediately.
//
// # invocationID parameter
//
// invocationID is the per-firing UUID assigned by the caller (S05 dispatcher)
// when the hook is initially dispatched. On replay, the caller supplies the
// same UUID to look up the persisted verdict. The UUID MUST be stable across
// restarts for the same hook firing; callers MUST derive it from the triggering
// event identifier or a durable checkpoint, not from time.Now() or rand.
//
// # Responsibility split (CP-042)
//
// InvokeCognitionHook straddles the mechanism/cognition boundary:
//   - Verdict production (eval.EvaluateCognitionHook) — Tags: cognition
//   - Replay check, hash computation, field-stamping — Tags: mechanism
//
// Verdict persistence (write to .harmonik/hooks/ + hook_verdict_persisted event)
// is the caller's responsibility and MUST happen before the hook chain continues.
//
// Spec ref: specs/control-points.md §4.3.CP-017, §4.8 CP-039–CP-042, §7.2.
// Refs: hk-a8bg.43
func InvokeCognitionHook(
	ctx context.Context,
	cp ControlPoint,
	run *Run,
	triggeringEventID EventID,
	invocationID uuid.UUID,
	eval CognitionHookEvaluator,
	reader HookVerdictReader,
) (HookVerdictRecord, error) {
	// Structural invariant: cp must be cognition-tagged with a valid delegation path.
	if cp.Evaluator.Mode != ModeTagCognition || cp.Evaluator.DelegationPath == nil {
		return HookVerdictRecord{}, fmt.Errorf("hook %q: InvokeCognitionHook requires a cognition-tagged ControlPoint (mode=%s)", cp.Name, cp.Evaluator.Mode)
	}

	// CP-040a: compute the input envelope hash for replay-safety checks.
	currentHash, err := computeHookEnvelopeHash(cp, run, triggeringEventID)
	if err != nil {
		return HookVerdictRecord{}, fmt.Errorf("hook %q: envelope hash computation failed: %w", cp.Name, err)
	}

	// CP-041: check for an existing persisted verdict before dispatching.
	existing, found, readErr := reader.LookupHookVerdict(ctx, run.RunID, invocationID)
	if readErr != nil {
		return HookVerdictRecord{}, fmt.Errorf("hook %q: verdict lookup failed: %w", cp.Name, readErr)
	}
	if found {
		if existing.InputEnvelopeHash == currentHash {
			// Hash match: replay path — consume the persisted verdict without
			// re-invoking the model per CP-INV-003 (idempotency=idempotent).
			return existing, nil
		}
		// Hash mismatch: the envelope has drifted since the verdict was persisted.
		// Only a Cat 6 reconciliation verdict can authorise re-invocation per CP-041.
		return HookVerdictRecord{}, &ErrHookVerdictEnvelopeMismatch{
			HookName:     cp.Name,
			InvocationID: invocationID,
			StoredHash:   existing.InputEnvelopeHash,
			CurrentHash:  currentHash,
		}
	}

	// No prior verdict: first invocation. Dispatch to the declared role per
	// CP-039 / §7.2 dispatch_to_role (cognition boundary per CP-042).
	verdict, dispatchErr := eval.EvaluateCognitionHook(ctx, cp, run, triggeringEventID)
	if dispatchErr != nil {
		return HookVerdictRecord{}, fmt.Errorf("hook %q: cognition dispatch failed: %w", cp.Name, dispatchErr)
	}

	// Stamp the mechanical fields (InvokeCognitionHook owns these per CP-042).
	verdict.HookName = cp.Name
	verdict.InvocationID = invocationID
	verdict.InputEnvelopeHash = currentHash
	verdict.ProducedAt = time.Now().UTC().Format(time.RFC3339)

	return verdict, nil
}
