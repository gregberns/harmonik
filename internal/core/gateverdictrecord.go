package core

// GateVerdictRecord is the persisted verdict produced by a Gate evaluator
// (specs/control-points.md §6.1.6 RECORD GateVerdictRecord).
//
// Every cognition-tagged evaluator's verdict MUST be persisted to the run's
// durable trace at invocation time per specs/control-points.md §4.7.CP-037.
// For Gates, the record is written into the Transition record's evidence field
// (keyed by gate_name) BEFORE the transition advances per execution-model.md §4.1.
//
// # ProducedAt type decision
//
// ProducedAt is kept as string rather than time.Time. The record is serialized
// verbatim into Transition evidence and event payloads, so a plain string avoids
// silent timezone normalization and JSON round-trip drift. The caller MUST format
// the value as RFC 3339 per event-model.md §4.3. Promotion to time.Time with
// custom marshal/unmarshal is a future option if a parsing use-case emerges.
// Mirrors the CapturedAtTimestamp rationale in SnapshotToken.
//
// # CognitionMeta deferral
//
// CognitionMeta is not yet implemented. CognitionMeta uses *string as a
// placeholder pending typed-alias implementation.
// TODO hk-a8bg.73: replace *string with *CognitionMeta once defined.
//
// # Reason cross-field invariant
//
// Reason is REQUIRED when Action != GateActionAllow. Valid() enforces this
// invariant: it rejects records where Action is deny or escalate-to-human and
// Reason is nil or empty.
//
// # InputEnvelopeHash
//
// InputEnvelopeHash is the SHA-256 hex digest of the gate's input envelope per
// specs/control-points.md §4.8.CP-040a. Plain string; no typed alias yet.
type GateVerdictRecord struct {
	// GateName is the ControlPoint name that produced this verdict.
	// Required (non-empty). Matches the registered ControlPoint.Name field.
	GateName string `json:"gate_name"`

	// Action is the gate verdict: allow, deny, or escalate-to-human.
	// Required. Unknown values MUST be rejected per §6.1.6.
	Action GateAction `json:"action"`

	// Reason is a human-readable explanation of the verdict. Required when
	// Action != GateActionAllow; nil is permitted only when Action == allow.
	// Valid() rejects records that violate this invariant.
	// Spec: specs/control-points.md §6.1.6 (reason : String | None).
	Reason *string `json:"reason,omitempty"`

	// CognitionMeta carries metadata about the cognition-tagged evaluator that
	// produced this verdict. Nil when the verdict was not produced by a
	// cognition-tagged evaluator.
	// TODO hk-a8bg.73: replace *string with *CognitionMeta once defined.
	// Spec: specs/control-points.md §6.1.6 (cognition_meta : CognitionMeta | None).
	CognitionMeta *string `json:"cognition_meta,omitempty"`

	// InputEnvelopeHash is the SHA-256 hex digest of the input envelope
	// presented to the evaluator, per specs/control-points.md §4.8.CP-040a.
	// Required (non-empty). Must be a 64-character lowercase hex string.
	InputEnvelopeHash string `json:"input_envelope_hash"`

	// ProducedAt is the RFC 3339 timestamp at which the verdict was produced.
	// Caller MUST format as RFC 3339 per event-model.md §4.3.
	// Required (non-empty). Kept as string; see type-decision note above.
	ProducedAt string `json:"produced_at"`
}

// Valid reports whether r is a well-formed GateVerdictRecord.
//
// Rules per specs/control-points.md §6.1.6:
//   - GateName must be non-empty.
//   - Action must be one of the three declared GateAction constants.
//   - Reason must be non-nil and non-empty when Action != GateActionAllow.
//   - InputEnvelopeHash must be non-empty.
//   - ProducedAt must be non-empty.
func (r GateVerdictRecord) Valid() bool {
	if r.GateName == "" {
		return false
	}
	if !r.Action.Valid() {
		return false
	}
	if r.Action != GateActionAllow {
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
