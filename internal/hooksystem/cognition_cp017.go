package hooksystem

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

type hookInputEnvelope struct {
	ControlPointName string              `json:"control_point_name"`
	DelegationPath   core.DelegationPath `json:"delegation_path"`
	EventPayload     json.RawMessage     `json:"event_payload"`
	SchemaVersion    int                 `json:"schema_version"`
}

func computeHookEnvelopeHash(cp core.ControlPoint, ev core.Event) (string, error) {
	if cp.Evaluator.DelegationPath == nil {
		return "", fmt.Errorf("cognition hook %q has nil evaluator delegation path", cp.Name)
	}
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
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	if ev.RunID == nil {
		msg := "cognition-tagged hook requires a run-scoped event (ev.RunID is nil)"
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
	runID := *ev.RunID

	currentHash, err := computeHookEnvelopeHash(cp, ev)
	if err != nil {
		msg := fmt.Sprintf("envelope hash computation failed: %v", err)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	existing, found, readErr := d.verdictReader.LookupVerdict(ctx, runID, cp.Name, triggeringID)
	if readErr != nil {
		msg := fmt.Sprintf("verdict lookup failed: %v", readErr)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
	if found {
		if existing.InputEnvelopeHash == currentHash {
			return d.applyHookVerdictResult(ctx, ev, cp, hookName, triggeringID, hookPL, haltOnFailure, existing)
		}
		if err := d.emitVerdictEnvelopeMismatch(ctx, runID, cp.Name, triggeringID,
			existing.InputEnvelopeHash, currentHash); err != nil {
			return haltOnFailure, fmt.Errorf("hooksystem: %s: emit verdict_envelope_mismatch: %w", cp.Name, err)
		}
		msg := "verdict_envelope_mismatch: envelope hash drifted since persisted verdict — escalate to Cat 6"
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	verdict, dispatchErr := d.cognitionEval.EvaluateCognitionHook(ctx, cp, ev)
	if dispatchErr != nil {
		msg := fmt.Sprintf("cognition dispatch to role failed: %v", dispatchErr)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	invocationID, uuidErr := uuid.NewV7()
	if uuidErr != nil {
		msg := fmt.Sprintf("invocation UUID generation failed: %v", uuidErr)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
	verdict.HookName = cp.Name
	verdict.InvocationID = invocationID
	verdict.InputEnvelopeHash = currentHash
	verdict.ProducedAt = time.Now().UTC().Format(time.RFC3339)

	ic := hookPL.IdempotencyClass
	if !ic.Valid() {
		ic = core.IdempotencyClassNonIdempotent
	}
	verdict.SideEffect = core.SideEffect{
		Kind:             hookPL.SideEffectKind,
		Target:           cp.Name,
		IdempotencyClass: ic,
	}

	if err := PersistHookVerdict(ctx, runID, verdict, d.verdictWriter, d.bus); err != nil {
		msg := fmt.Sprintf("verdict persistence failed: %v", err)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	return d.applyHookVerdictResult(ctx, ev, cp, hookName, triggeringID, hookPL, haltOnFailure, verdict)
}

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
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic,
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
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient,
			fmt.Sprintf("hook_fired emit failed: %v", err))
		return haltOnFailure, err
	}
	return false, nil
}

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
