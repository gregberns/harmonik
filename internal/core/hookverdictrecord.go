package core

import "github.com/google/uuid"

// HookVerdictRecord is the persisted verdict produced by a Hook evaluator
// (specs/control-points.md §6.1.6 RECORD HookVerdictRecord).
//
// Every cognition-tagged Hook evaluator's verdict MUST be persisted to the
// run's durable trace at invocation time per specs/control-points.md
// §4.7.CP-037. For Hooks, the record is written to the path
// ".harmonik/hooks/<run_id>/<hook_invocation_id>.json" on the run's task
// branch per workspace-model.md §4.2 and emitted as a hook_verdict_persisted
// event per event-model.md §8.2.
//
// HookVerdictRecord is the typed output of the hook-dispatch segment in the
// outcome spine (execution-model.md §4.6.EM-027). Each segment of the spine
// MUST consume the prior segment's typed output and produce the next segment's
// typed input; no segment may bypass another. In the spine ordering:
//
//	Outcome → [hook dispatch] → HookVerdictRecord → [gate evaluation] → GateVerdictRecord → …
//
// # ProducedAt type decision
//
// ProducedAt is kept as string rather than time.Time. The record is serialized
// verbatim into hook-verdict files and event payloads, so a plain string avoids
// silent timezone normalization and JSON round-trip drift. The caller MUST
// format the value as RFC 3339 per event-model.md §4.3. Mirrors the
// CapturedAtTimestamp rationale in SnapshotToken and the same rationale in
// GateVerdictRecord.
//
// # Reason cross-field invariant
//
// Reason is REQUIRED when Failed == true. Valid() enforces this invariant:
// it rejects records where Failed is true and Reason is nil or empty.
//
// # InputEnvelopeHash
//
// InputEnvelopeHash is the SHA-256 hex digest of the hook's input envelope per
// specs/control-points.md §4.8.CP-040a. Plain string; no typed alias yet.
type HookVerdictRecord struct {
	// HookName is the ControlPoint name that produced this verdict.
	// Required (non-empty). Matches the registered ControlPoint.Name field.
	HookName string `json:"hook_name"`

	// InvocationID is a UUID unique per Hook firing.
	// Required (must not be uuid.Nil).
	InvocationID uuid.UUID `json:"invocation_id"`

	// SideEffect is the side-effect descriptor produced by the evaluator.
	// Required; must satisfy SideEffect.Valid().
	SideEffect SideEffect `json:"side_effect"`

	// Failed is true when the evaluator returned a typed failure.
	// When true, Reason MUST be non-nil and non-empty.
	Failed bool `json:"failed"`

	// Reason is a human-readable explanation of the verdict. Required when
	// Failed is true; nil is permitted only when Failed is false.
	// Valid() rejects records that violate this invariant.
	// Spec: specs/control-points.md §6.1.6 (reason : String | None).
	Reason *string `json:"reason,omitempty"`

	// CognitionMeta carries metadata about the cognition-tagged evaluator that
	// produced this verdict. Nil when the verdict was not produced by a
	// cognition-tagged evaluator.
	// Spec: specs/control-points.md §6.1.6 (cognition_meta : CognitionMeta | None).
	CognitionMeta *CognitionMeta `json:"cognition_meta,omitempty"`

	// InputEnvelopeHash is the SHA-256 hex digest of the input envelope
	// presented to the evaluator, per specs/control-points.md §4.8.CP-040a.
	// Required (non-empty). Must be a 64-character lowercase hex string.
	InputEnvelopeHash string `json:"input_envelope_hash"`

	// ProducedAt is the RFC 3339 timestamp at which the verdict was produced.
	// Caller MUST format as RFC 3339 per event-model.md §4.3.
	// Required (non-empty). Kept as string; see type-decision note above.
	ProducedAt string `json:"produced_at"`
}

// Valid reports whether r is a well-formed HookVerdictRecord.
//
// Rules per specs/control-points.md §6.1.6:
//   - HookName must be non-empty.
//   - InvocationID must not be uuid.Nil.
//   - SideEffect must satisfy SideEffect.Valid().
//   - Reason must be non-nil and non-empty when Failed is true.
//   - InputEnvelopeHash must be non-empty.
//   - ProducedAt must be non-empty.
func (r HookVerdictRecord) Valid() bool {
	if r.HookName == "" {
		return false
	}
	if r.InvocationID == uuid.Nil {
		return false
	}
	if !r.SideEffect.Valid() {
		return false
	}
	if r.Failed {
		if r.Reason == nil || *r.Reason == "" {
			return false
		}
	}
	if r.InputEnvelopeHash == "" {
		return false
	}
	if r.ProducedAt == "" {
		return false
	}
	return true
}
