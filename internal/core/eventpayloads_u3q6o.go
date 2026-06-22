package core

// eventpayloads_u3q6o.go — event-bus payload types for the six §8 event types
// added in hk-u3q6o (event-model G4 conformance):
//
//   §8.2.13  gate_definition_drift           (F)  — Gate envelope drift at replay
//   §8.2.14  gate_redefined_under_cat_6      (F)  — Cat 6 authorized Gate re-evaluation
//   §8.12.1  decision_required               (F)  — daemon dispatch-blocking escalation
//   §8.12.2  decision_acknowledged           (F)  — ACK for a decision_required
//   §8.15.1  bead_sync_failed                (F)  — `br sync --import-only` failure
//   §8.15.2  bead_ledger_conflict_audit      (O)  — Cat-BL3 conflict-log audit batch
//
// Spec ref: specs/event-model.md §8.2.13–14, §8.12.1–2, §8.15.1–2.
// Bead ref: hk-u3q6o.

// ---------------------------------------------------------------------------
// §8.2.13 gate_definition_drift
// ---------------------------------------------------------------------------

// GateDefinitionDriftPayload is the typed event payload for the
// gate_definition_drift event (event-model.md §8.2.13, v0.3.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — replay is blocked on this event
// before Cat 6 escalation fires).
//
// # Payload fields (§8.2.13)
//
//   - run_id               — UUID of the run being replayed (REQUIRED)
//   - gate_name            — name of the Gate-kind ControlPoint (CP-002) (REQUIRED)
//   - prior_envelope_hash  — SHA-256 hex of the envelope at original evaluation (REQUIRED)
//   - current_envelope_hash — SHA-256 hex recomputed at replay time (REQUIRED)
//   - changed_inputs       — subset of {expression_text, context_subset, policy_meta} (REQUIRED)
type GateDefinitionDriftPayload struct {
	// RunID is the UUID of the run being replayed.
	RunID string `json:"run_id"`

	// GateName is the name of the Gate-kind ControlPoint (CP-002).
	GateName string `json:"gate_name"`

	// PriorEnvelopeHash is the SHA-256 hex of the Gate envelope at original evaluation per CP-038a.
	PriorEnvelopeHash string `json:"prior_envelope_hash"`

	// CurrentEnvelopeHash is the SHA-256 hex of the Gate envelope recomputed at replay time.
	CurrentEnvelopeHash string `json:"current_envelope_hash"`

	// ChangedInputs lists which inputs changed: subset of
	// {expression_text, context_subset, policy_meta}.
	ChangedInputs []string `json:"changed_inputs"`
}

// Valid reports whether p is a well-formed GateDefinitionDriftPayload.
func (p GateDefinitionDriftPayload) Valid() bool {
	return p.RunID != "" && p.GateName != "" &&
		p.PriorEnvelopeHash != "" && p.CurrentEnvelopeHash != ""
}

// ---------------------------------------------------------------------------
// §8.2.14 gate_redefined_under_cat_6
// ---------------------------------------------------------------------------

// GateDecision is the typed discriminator for the gate decision field in
// gate_redefined_under_cat_6 (§8.2.14). Matches the allow/deny/escalate-to-human
// enum from the control-points spec.
type GateDecision string

const (
	// GateDecisionAllow indicates the gate evaluation produced an allow outcome.
	GateDecisionAllow GateDecision = "allow"

	// GateDecisionDeny indicates the gate evaluation produced a deny outcome.
	GateDecisionDeny GateDecision = "deny"

	// GateDecisionEscalateToHuman indicates the gate evaluation produced an
	// escalate-to-human outcome.
	GateDecisionEscalateToHuman GateDecision = "escalate-to-human"
)

// Valid reports whether d is a declared GateDecision constant.
func (d GateDecision) Valid() bool {
	switch d {
	case GateDecisionAllow, GateDecisionDeny, GateDecisionEscalateToHuman:
		return true
	default:
		return false
	}
}

// GateRedefinedUnderCat6Payload is the typed event payload for the
// gate_redefined_under_cat_6 event (event-model.md §8.2.14, v0.3.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — the re-evaluation outcome is a lifecycle boundary).
//
// # Payload fields (§8.2.14)
//
//   - run_id           — UUID of the run (REQUIRED)
//   - gate_name        — name of the Gate-kind ControlPoint (CP-002) (REQUIRED)
//   - prior_decision   — decision from original evaluation (REQUIRED)
//   - new_decision     — decision from Cat 6 re-evaluation (REQUIRED)
//   - cat_6_verdict_id — identifier of the Cat 6 reconciliation verdict (REQUIRED)
type GateRedefinedUnderCat6Payload struct {
	// RunID is the UUID of the run.
	RunID string `json:"run_id"`

	// GateName is the name of the Gate-kind ControlPoint (CP-002).
	GateName string `json:"gate_name"`

	// PriorDecision is the Gate decision from the original evaluation.
	PriorDecision GateDecision `json:"prior_decision"`

	// NewDecision is the Gate decision from the Cat 6 re-evaluation.
	NewDecision GateDecision `json:"new_decision"`

	// Cat6VerdictID is the identifier of the Cat 6 reconciliation verdict that
	// authorized re-evaluation.
	Cat6VerdictID string `json:"cat_6_verdict_id"`
}

// Valid reports whether p is a well-formed GateRedefinedUnderCat6Payload.
func (p GateRedefinedUnderCat6Payload) Valid() bool {
	return p.RunID != "" && p.GateName != "" &&
		p.PriorDecision.Valid() && p.NewDecision.Valid() && p.Cat6VerdictID != ""
}

// ---------------------------------------------------------------------------
// §8.12.1 decision_required
// ---------------------------------------------------------------------------

// DecisionRequiredReason is the typed discriminator for the reason field of a
// decision_required event (§8.12.1). Exhaustive at v1; new variants require
// an EV-027 amendment.
type DecisionRequiredReason string

const (
	// DecisionRequiredReasonBeadDoubleFailure indicates the bead failed twice
	// in a daemon session without an intervening success.
	DecisionRequiredReasonBeadDoubleFailure DecisionRequiredReason = "bead_double_failure"

	// DecisionRequiredReasonIterationCapHit indicates iteration_cap_hit fired
	// with a final verdict of REQUEST_CHANGES or BLOCK.
	DecisionRequiredReasonIterationCapHit DecisionRequiredReason = "iteration_cap_hit"

	// DecisionRequiredReasonMergeConflictEscalation indicates
	// merge_conflict_escalation (§8.5.6) was emitted.
	DecisionRequiredReasonMergeConflictEscalation DecisionRequiredReason = "merge_conflict_escalation"

	// DecisionRequiredReasonQueueGroupFailure indicates
	// queue_paused{reason: group_failure} (§8.10.4) was emitted.
	DecisionRequiredReasonQueueGroupFailure DecisionRequiredReason = "queue_group_failure"
)

// Valid reports whether r is a declared DecisionRequiredReason constant.
func (r DecisionRequiredReason) Valid() bool {
	switch r {
	case DecisionRequiredReasonBeadDoubleFailure,
		DecisionRequiredReasonIterationCapHit,
		DecisionRequiredReasonMergeConflictEscalation,
		DecisionRequiredReasonQueueGroupFailure:
		return true
	default:
		return false
	}
}

// DecisionSubjectKind is the typed discriminator for the subject.kind field
// shared by decision_required (§8.12.1) and decision_acknowledged (§8.12.2).
type DecisionSubjectKind string

const (
	// DecisionSubjectKindBead indicates the subject is a bead.
	DecisionSubjectKindBead DecisionSubjectKind = "bead"

	// DecisionSubjectKindQueue indicates the subject is a queue.
	DecisionSubjectKindQueue DecisionSubjectKind = "queue"
)

// Valid reports whether k is a declared DecisionSubjectKind constant.
func (k DecisionSubjectKind) Valid() bool {
	switch k {
	case DecisionSubjectKindBead, DecisionSubjectKindQueue:
		return true
	default:
		return false
	}
}

// DecisionSubject is the nested subject struct shared by decision_required
// and decision_acknowledged (§8.12.1–2).
type DecisionSubject struct {
	// Kind identifies whether the subject is a bead or a queue.
	Kind DecisionSubjectKind `json:"kind"`

	// ID is the bead_id or queue_id of the subject.
	ID string `json:"id"`
}

// Valid reports whether s is a well-formed DecisionSubject.
func (s DecisionSubject) Valid() bool {
	return s.Kind.Valid() && s.ID != ""
}

// DecisionRequiredPayload is the typed event payload for the decision_required
// event (event-model.md §8.12.1, v0.6.0).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — loss silently leaves a double-failed
// bead eligible for re-dispatch; see eventbus.fsyncBoundaryEventTypes).
//
// Idempotency-keyed on triggering_event_id. Dispatch-blocking while
// unacknowledged (EV-043). ACK surface: `harmonik decision ack <token>` or
// cognition-loop note(). Re-emitted with fresh ack_token after TTL (24h default)
// per §8.12.1 TTL rule.
//
// DISTINCT from the §8.14 hitl-decisions decision_needed/resolved/withdrawn
// family — different emitter, purpose, and payload shape.
//
// # Payload fields (§8.12.1)
//
//   - subject            — bead or queue being escalated (REQUIRED)
//   - reason             — exhaustive enum of triggering conditions (REQUIRED)
//   - suggested_action   — free text; SHOULD be ≤ 256 bytes (REQUIRED)
//   - ack_required       — always true at v1; reserved for advisory signals (REQUIRED)
//   - ack_token          — opaque UUIDv4; key for .harmonik/decision_acks/ (REQUIRED)
//   - triggering_event_id — event_id of the condition event; dedup key (REQUIRED)
type DecisionRequiredPayload struct {
	// Subject identifies the bead or queue being escalated.
	Subject DecisionSubject `json:"subject"`

	// Reason is the exhaustive enum of triggering conditions.
	Reason DecisionRequiredReason `json:"reason"`

	// SuggestedAction is free text describing the recommended operator action.
	// SHOULD be ≤ 256 bytes.
	SuggestedAction string `json:"suggested_action"`

	// AckRequired indicates whether an ACK is required to unblock dispatch.
	// Always true at v1; reserved for future advisory-only signals.
	AckRequired bool `json:"ack_required"`

	// AckRef is an opaque UUIDv4 that keys the .harmonik/decision_acks/ file
	// per EV-043a. Unique per emission. JSON: ack_token (spec §8.12.1).
	// Named AckRef (not AckToken) per EV-036 — "token" matches the secret-prefix rule.
	AckRef string `json:"ack_token"`

	// TriggeringEventID is the event_id of the condition event that caused this
	// emission. Used as the idempotency dedup key: re-processing after restart
	// MUST NOT produce a second event for an already-pending AckRef.
	TriggeringEventID string `json:"triggering_event_id"`
}

// Valid reports whether p is a well-formed DecisionRequiredPayload.
func (p DecisionRequiredPayload) Valid() bool {
	return p.Subject.Valid() && p.Reason.Valid() &&
		p.AckRef != "" && p.TriggeringEventID != ""
}

// ---------------------------------------------------------------------------
// §8.12.2 decision_acknowledged
// ---------------------------------------------------------------------------

// DecisionAckMethod is the typed discriminator for the ack_method field of a
// decision_acknowledged event (§8.12.2).
type DecisionAckMethod string

const (
	// DecisionAckMethodOperator indicates ACK via `harmonik decision ack <token>` CLI.
	DecisionAckMethodOperator DecisionAckMethod = "operator"

	// DecisionAckMethodNote indicates implicit ACK via cognition-loop note().
	DecisionAckMethodNote DecisionAckMethod = "note"
)

// Valid reports whether m is a declared DecisionAckMethod constant.
func (m DecisionAckMethod) Valid() bool {
	switch m {
	case DecisionAckMethodOperator, DecisionAckMethodNote:
		return true
	default:
		return false
	}
}

// DecisionAcknowledgedPayload is the typed event payload for the
// decision_acknowledged event (event-model.md §8.12.2, v0.6.0).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — loss breaks JSONL observability for
// the ACK; ack-state file remains authoritative per EV-043a).
//
// Emitted after ACK via operator CLI or cognition-loop note(). MUST be
// emitted+fsynced BEFORE the workloop is permitted to dispatch for the subject.
//
// # Payload fields (§8.12.2)
//
//   - ack_token  — MUST match the ack_token of the matching decision_required (REQUIRED)
//   - subject    — bead or queue being unblocked (REQUIRED)
//   - ack_method — how the ACK was delivered (REQUIRED)
//   - acked_at   — RFC 3339 wall-clock timestamp of the ACK (REQUIRED)
type DecisionAcknowledgedPayload struct {
	// AckRef MUST match the AckRef (JSON: ack_token) of the matching
	// decision_required. Named AckRef (not AckToken) per EV-036 — "token"
	// matches the secret-prefix rule.
	AckRef string `json:"ack_token"`

	// Subject identifies the bead or queue being unblocked.
	Subject DecisionSubject `json:"subject"`

	// AckMethod indicates how the ACK was delivered.
	AckMethod DecisionAckMethod `json:"ack_method"`

	// AckedAt is the RFC 3339 wall-clock timestamp of the acknowledgement.
	AckedAt string `json:"acked_at"`
}

// Valid reports whether p is a well-formed DecisionAcknowledgedPayload.
func (p DecisionAcknowledgedPayload) Valid() bool {
	return p.AckRef != "" && p.Subject.Valid() &&
		p.AckMethod.Valid() && p.AckedAt != ""
}

// ---------------------------------------------------------------------------
// §8.15.1 bead_sync_failed
// ---------------------------------------------------------------------------

// BeadSyncFailedPayload is the typed event payload for the bead_sync_failed
// event (event-model.md §8.15.1, v0.6.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss silences the Cat-BL2 routing
// obligation per BL-MRG-004).
//
// Emitted by the daemon (beads-adapter, post-merge) when `br sync
// --import-only` fails following any rebase or merge that touches
// .beads/issues.jsonl per BL-MRG-004. MUST be emitted+fsynced before the
// daemon routes to Cat-BL2.
//
// # Payload fields (§8.15.1)
//
//   - run_id    — UUID of the run whose merge triggered the sync (REQUIRED)
//   - error     — free-form error from `br sync` stderr or exit code (REQUIRED)
//   - timestamp — RFC 3339 wall-clock time at the failure site (REQUIRED)
type BeadSyncFailedPayload struct {
	// RunID is the UUID of the run whose merge triggered the sync.
	RunID string `json:"run_id"`

	// Error is the free-form error string from `br sync` subprocess stderr or exit code.
	Error string `json:"error"`

	// Timestamp is the RFC 3339 wall-clock time at the failure site.
	Timestamp string `json:"timestamp"`
}

// Valid reports whether p is a well-formed BeadSyncFailedPayload.
func (p BeadSyncFailedPayload) Valid() bool {
	return p.RunID != "" && p.Error != "" && p.Timestamp != ""
}

// ---------------------------------------------------------------------------
// §8.15.2 bead_ledger_conflict_audit
// ---------------------------------------------------------------------------

// BeadLedgerConflict represents one conflict line from .beads/merge-conflicts.log
// as read during a Cat-BL3 audit per BL-MRG-003.
type BeadLedgerConflict struct {
	// BeadID is the bead involved in the conflict.
	BeadID string `json:"bead_id"`

	// Field is the conflicting field name.
	Field string `json:"field"`

	// AValue is the value on side A of the merge.
	AValue string `json:"a_value"`

	// BValue is the value on side B of the merge.
	BValue string `json:"b_value"`

	// Resolution describes how the conflict was resolved.
	Resolution string `json:"resolution"`
}

// BeadLedgerConflictAuditPayload is the typed event payload for the
// bead_ledger_conflict_audit event (event-model.md §8.15.2, v0.6.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — .beads/merge-conflicts.log is the
// authoritative source; the investigator can re-emit on recovery).
//
// Emitted by the reconciliation-investigator for each .beads/merge-conflicts.log
// batch read during a Cat-BL3 audit per BL-MRG-003.
//
// # Payload fields (§8.15.2)
//
//   - run_id    — UUID of the run context (REQUIRED)
//   - bead_ids  — IDs of the beads involved in logged conflicts (REQUIRED)
//   - conflicts — structured representation of logged conflict lines (REQUIRED)
//   - timestamp — RFC 3339 wall-clock time of the log read (REQUIRED)
type BeadLedgerConflictAuditPayload struct {
	// RunID is the UUID of the run context.
	RunID string `json:"run_id"`

	// BeadIDs lists the bead IDs involved in the semantic conflicts.
	BeadIDs []string `json:"bead_ids"`

	// Conflicts is a structured representation of the conflict log lines.
	Conflicts []BeadLedgerConflict `json:"conflicts"`

	// Timestamp is the RFC 3339 wall-clock time of the conflict log read.
	Timestamp string `json:"timestamp"`
}

// Valid reports whether p is a well-formed BeadLedgerConflictAuditPayload.
func (p BeadLedgerConflictAuditPayload) Valid() bool {
	return p.RunID != "" && p.Timestamp != ""
}
