package hooksystem

// cognition_cp017.go — cognition-tagged Hook evaluator dispatch per CP-017.
//
// CP-017: A Hook's evaluator MAY be cognition-tagged (e.g., an
// on_review_required Hook that delegates to a reviewer agent).
// Cognition-tagged Hook evaluators MUST satisfy the §4.8 replay-safety
// contract. The delegation path — role, model class, input shape, response
// schema — MUST be named on the Hook record.
//
// Spec ref: specs/control-points.md §4.3.CP-017, §4.8 CP-039–CP-042, §7.2.
// Bead ref: hk-a8bg.16

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// CognitionHookEvaluator dispatches a cognition-tagged Hook to the declared
// role and returns a partially-populated HookVerdictRecord per CP-017 and
// CP-039.
//
// The returned record MUST have CognitionMeta populated (delegation path
// snapshot, model response digest). Failed and Reason MUST be set when the
// evaluator returns a typed failure. The caller (Dispatcher) stamps
// HookName, InvocationID, SideEffect, InputEnvelopeHash, and ProducedAt;
// values set by the evaluator for those fields are overwritten.
//
// Tags: cognition (the dispatch to the role model is the cognition boundary
// per specs/control-points.md §4.8.CP-042).
type CognitionHookEvaluator interface {
	EvaluateCognitionHook(ctx context.Context, cp core.ControlPoint, ev core.Event) (core.HookVerdictRecord, error)
}

// VerdictReader looks up a previously persisted HookVerdictRecord for the
// given (runID, hookName, eventID) key per specs/control-points.md
// §4.8.CP-041.
//
// LookupVerdict returns:
//   - (record, true, nil)   when a matching verdict is found.
//   - (zero, false, nil)    when no verdict exists for the key (first fire).
//   - (zero, false, err)    on I/O error.
//
// Tags: mechanism (the file-read is a deterministic I/O operation).
type VerdictReader interface {
	LookupVerdict(ctx context.Context, runID core.RunID, hookName string, eventID core.EventID) (core.HookVerdictRecord, bool, error)
}

// hookInputEnvelope is the canonical set of inputs hashed to produce the
// InputEnvelopeHash per specs/control-points.md §4.8.CP-040a.
//
// Conservative fallback per CP-040a: the full event payload is used as the
// context_subset, since this implementation does not AST-walk prompt
// templates to determine the reachable subset. Any change to the event
// payload busts the hash and escalates to Cat 6 on replay per CP-041.
// This behaviour is declared explicitly on every cognition-tagged Hook
// ControlPoint per the single-mode rule in CP-040a.
type hookInputEnvelope struct {
	ControlPointName string              `json:"control_point_name"`
	DelegationPath   core.DelegationPath `json:"delegation_path"`
	EventPayload     json.RawMessage     `json:"event_payload"`
	SchemaVersion    int                 `json:"schema_version"`
}

// computeHookEnvelopeHash returns the SHA-256 hex digest of the canonical
// JSON of the hook's input envelope per specs/control-points.md §4.8.CP-040a.
func computeHookEnvelopeHash(cp core.ControlPoint, ev core.Event) (string, error) {
	envelope := hookInputEnvelope{
		ControlPointName: cp.Name,
		DelegationPath:   *cp.Evaluator.DelegationPath,
		EventPayload:     ev.Payload,
		SchemaVersion:    cp.SchemaVersion,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal hook input envelope: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

// fireCognitionHook implements the §7.2 cognition-tagged evaluator invocation
// path for Hook ControlPoints per CP-017 / CP-039–CP-042.
//
// Three-path logic per §7.2:
//  1. Existing verdict with matching hash → return persisted verdict (replay).
//  2. Existing verdict with mismatching hash → emit verdict_envelope_mismatch
//     and fail (Cat 6 escalation; re-invocation is not permitted here).
//  3. No existing verdict → dispatch to role, stamp hash, persist, apply.
func (d *Dispatcher) fireCognitionHook(
	ctx context.Context,
	ev core.Event,
	cp core.ControlPoint,
	hookName core.HookName,
	triggeringID core.EventID,
	hookPL *core.HookPayload,
	haltOnFailure bool,
) (halt bool, _ error) {
	if d.cognitionEval == nil || d.verdictWriter == nil || d.verdictReader == nil {
		msg := "cognition-tagged hook evaluator not wired (call WithCognition to enable)"
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	// Cognition-tagged hooks require a run-scoped event (unscoped hooks are
	// forbidden in this implementation per the OQ-CP-004 default deferral in
	// specs/control-points.md §10).
	if ev.RunID == nil {
		msg := "cognition-tagged hook requires a run-scoped event (ev.RunID is nil)"
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
	runID := *ev.RunID

	// CP-040a: compute the input envelope hash for replay-safety checks.
	currentHash, err := computeHookEnvelopeHash(cp, ev)
	if err != nil {
		msg := fmt.Sprintf("envelope hash computation failed: %v", err)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	// CP-041: check for an existing persisted verdict before dispatching.
	existing, found, readErr := d.verdictReader.LookupVerdict(ctx, runID, cp.Name, triggeringID)
	if readErr != nil {
		msg := fmt.Sprintf("verdict lookup failed: %v", readErr)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
	if found {
		if existing.InputEnvelopeHash == currentHash {
			// Hash match: replay path — consume the persisted verdict without
			// re-invoking the model per CP-INV-003.
			return d.applyHookVerdictResult(ctx, ev, cp, hookName, triggeringID, hookPL, haltOnFailure, existing)
		}
		// Hash mismatch: the envelope has drifted since the verdict was persisted.
		// Emit verdict_envelope_mismatch and fail; only a Cat 6 reconciliation
		// verdict can authorize re-invocation per CP-041.
		_ = d.emitVerdictEnvelopeMismatch(ctx, runID, cp.Name, triggeringID,
			existing.InputEnvelopeHash, currentHash)
		msg := "verdict_envelope_mismatch: envelope hash drifted since persisted verdict — escalate to Cat 6"
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	// No prior verdict: first invocation. Dispatch to the declared role per
	// CP-039 / §7.2 dispatch_to_role.
	verdict, dispatchErr := d.cognitionEval.EvaluateCognitionHook(ctx, cp, ev)
	if dispatchErr != nil {
		msg := fmt.Sprintf("cognition dispatch to role failed: %v", dispatchErr)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	// Dispatcher owns the mechanical fields per CP-042.
	invocationID, uuidErr := uuid.NewV7()
	if uuidErr != nil {
		msg := fmt.Sprintf("invocation UUID generation failed: %v", uuidErr)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
	verdict.HookName = cp.Name
	verdict.InvocationID = invocationID
	verdict.InputEnvelopeHash = currentHash
	verdict.ProducedAt = time.Now().UTC().Format(time.RFC3339)

	// Resolve the side-effect from the hook payload (not from the model).
	ic := hookPL.IdempotencyClass
	if !ic.Valid() {
		ic = core.IdempotencyClassNonIdempotent
	}
	verdict.SideEffect = core.SideEffect{
		Kind:             hookPL.SideEffectKind,
		Target:           cp.Name,
		IdempotencyClass: ic,
	}

	// CP-040: persist the verdict to the run's task branch (mechanism-tagged).
	if err := PersistHookVerdict(ctx, runID, verdict, d.verdictWriter, d.bus); err != nil {
		msg := fmt.Sprintf("verdict persistence failed: %v", err)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	return d.applyHookVerdictResult(ctx, ev, cp, hookName, triggeringID, hookPL, haltOnFailure, verdict)
}

// applyHookVerdictResult applies a (fresh or replayed) verdict by emitting
// hook_fired on success or hook_failed on evaluator failure.
func (d *Dispatcher) applyHookVerdictResult(
	ctx context.Context,
	ev core.Event,
	cp core.ControlPoint,
	hookName core.HookName,
	triggeringID core.EventID,
	hookPL *core.HookPayload,
	haltOnFailure bool,
	verdict core.HookVerdictRecord,
) (halt bool, _ error) {
	if verdict.Failed {
		reason := ""
		if verdict.Reason != nil {
			reason = *verdict.Reason
		}
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic,
			fmt.Sprintf("cognition evaluator returned failure: %s", reason))
		return haltOnFailure, fmt.Errorf("hooksystem: %s: cognition evaluator failed: %s", cp.Name, reason)
	}

	ic := hookPL.IdempotencyClass
	if !ic.Valid() {
		ic = core.IdempotencyClassNonIdempotent
	}
	se := core.SideEffect{
		Kind:             hookPL.SideEffectKind,
		Target:           cp.Name,
		IdempotencyClass: ic,
	}
	if err := d.emitHookFired(ctx, ev, hookName, triggeringID, se); err != nil {
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient,
			fmt.Sprintf("hook_fired emit failed: %v", err))
		return haltOnFailure, err
	}
	return false, nil
}

// emitVerdictEnvelopeMismatch emits a verdict_envelope_mismatch event per
// specs/control-points.md §4.8.CP-041 and event-model.md §8.2.11.
func (d *Dispatcher) emitVerdictEnvelopeMismatch(
	ctx context.Context,
	runID core.RunID,
	controlPointName string,
	eventIDRef core.EventID,
	storedHash string,
	currentHash string,
) error {
	pl := core.VerdictEnvelopeMismatchPayload{
		RunID:               runID,
		ControlPointName:    controlPointName,
		EventIDRef:          &eventIDRef,
		StoredEnvelopeHash:  storedHash,
		CurrentEnvelopeHash: currentHash,
		DetectedAt:          time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("hooksystem: marshal verdict_envelope_mismatch: %w", err)
	}
	return d.bus.EmitWithRunID(ctx, runID, core.EventTypeVerdictEnvelopeMismatch, raw)
}
