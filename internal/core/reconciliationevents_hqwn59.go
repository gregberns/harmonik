package core

import "github.com/google/uuid"

// reconciliationevents_hqwn59.go — event-bus payload types for §8.6 reconciliation
// lifecycle events covered by this implementer wave (hqwn59b):
//   - reconciliation_started                  (§8.6.1)
//   - reconciliation_category_assigned        (§8.6.2)
//   - reconciliation_verdict_emitted          (§8.6.3)
//   - reconciliation_verdict_executed         (§8.6.4)  — uses existing VerdictExecutedPayload
//   - reconciliation_verdict_malformed        (§8.6.5)  — uses existing MalformedVerdictPayload
//   - reconciliation_budget_exhausted         (§8.6.6)  — uses existing BudgetExhaustedPayload
//   - reconciliation_verdict_stale            (§8.6.7)  — uses existing StaleVerdictPayload
//   - store_divergence_detected               (§8.6.8)
//   - operator_escalation_required            (§8.6.9)
//   - divergence_inconclusive                 (§8.6.10)
//   - reconciliation_dispatch_deduplicated    (§8.6.11)
//   - reconciliation_detector_panic           (§8.6.12)
//   - reconciliation_verdict_execution_retry  (§8.6.13)
//   - bead_terminal_transition_recovered      (§8.6.14) — post-MVH reserved per OQ-BI-008
//
// §8.6.4, §8.6.5, §8.6.6, §8.6.7 already have dedicated payload types in
// this package (VerdictExecutedPayload, MalformedVerdictPayload,
// BudgetExhaustedPayload, StaleVerdictPayload respectively) and are registered
// in registerReconciliationEvents() by forwarding to those existing types.
//
// Spec ref: specs/event-model.md §8.6.
// Bead refs: hk-hqwn.59.43 through hk-hqwn.59.56.

// ReconciliationTrigger is the typed discriminator for the trigger field of a
// reconciliation_started event (event-model.md §8.6.1).
type ReconciliationTrigger string

const (
	// ReconciliationTriggerStartup indicates the reconciliation run was triggered
	// at daemon startup (RC-020a dispatch point (a)).
	ReconciliationTriggerStartup ReconciliationTrigger = "startup"

	// ReconciliationTriggerOnDemand indicates the reconciliation run was triggered
	// by an operator on-demand request (RC-020a dispatch point (b)).
	ReconciliationTriggerOnDemand ReconciliationTrigger = "on-demand"

	// ReconciliationTriggerScheduled indicates the reconciliation run was triggered
	// by the background scheduled cadence (RC-020a dispatch point (c)).
	// MVH default interval is hourly (3600 s); configurable via operator YAML
	// per operator-nfr.md §4.3 (knob: reconciliation_scan_cadence).
	ReconciliationTriggerScheduled ReconciliationTrigger = "scheduled-hourly"

	// ReconciliationTriggerDivergenceDetected indicates the reconciliation run
	// was triggered by a detected store divergence.
	ReconciliationTriggerDivergenceDetected ReconciliationTrigger = "divergence-detected"
)

// Valid reports whether t is one of the four declared ReconciliationTrigger constants.
func (t ReconciliationTrigger) Valid() bool {
	switch t {
	case ReconciliationTriggerStartup, ReconciliationTriggerOnDemand,
		ReconciliationTriggerScheduled, ReconciliationTriggerDivergenceDetected:
		return true
	default:
		return false
	}
}

// ReconciliationStartedPayload is the typed event payload for the
// reconciliation_started event (event-model.md §8.6.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation lifecycle observability per
// reconciliation/spec.md §4.1).
//
// Emitted by the daemon-core when a reconciliation workflow is dispatched.
//
// # Payload fields (event-model.md §8.6.1)
//
//   - reconciliation_run_id — the run_id of the reconciliation workflow
//   - trigger               — what triggered this reconciliation run
type ReconciliationStartedPayload struct {
	// ReconciliationRunID is the run_id of the reconciliation workflow.
	// Required (must not be uuid.Nil).
	ReconciliationRunID RunID `json:"reconciliation_run_id"`

	// Trigger identifies what triggered this reconciliation run.
	// Required; must be a valid ReconciliationTrigger constant.
	Trigger ReconciliationTrigger `json:"trigger"`
}

// Valid reports whether p is a well-formed ReconciliationStartedPayload.
//
// Rules per event-model.md §8.6.1:
//   - ReconciliationRunID must not be uuid.Nil.
//   - Trigger must be a valid ReconciliationTrigger constant.
func (p ReconciliationStartedPayload) Valid() bool {
	if uuid.UUID(p.ReconciliationRunID) == uuid.Nil {
		return false
	}
	if !p.Trigger.Valid() {
		return false
	}
	return true
}

// ReconciliationCategoryAssignedPayload is the typed event payload for the
// reconciliation_category_assigned event (event-model.md §8.6.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation classification observability
// and improvement-loop input per reconciliation/spec.md §4.1).
//
// Emitted by the daemon-core when the reconciliation workflow assigns a
// category (Cat 0..6) to a target run.
//
// # Payload fields (event-model.md §8.6.2)
//
//   - reconciliation_run_id — the run_id of the reconciliation workflow
//   - target_run_id         — optional run_id of the run being classified
//   - category              — assigned reconciliation category (Cat 0..6)
//   - evidence_ref          — opaque reference to the evidence driving the classification
//   - post_crash_window     — optional flag: true if the classification was made within the post-crash lossy-tail window
type ReconciliationCategoryAssignedPayload struct {
	// ReconciliationRunID is the run_id of the reconciliation workflow.
	// Required (must not be uuid.Nil).
	ReconciliationRunID RunID `json:"reconciliation_run_id"`

	// TargetRunID is the optional run_id of the run being classified.
	// Corresponds to target_run_id? in §8.6.2. Nil when the classification
	// is not scoped to a specific run (e.g., Cat 0 daemon-level categories).
	// Non-nil must not be uuid.Nil.
	TargetRunID *RunID `json:"target_run_id,omitempty"`

	// Category is the assigned reconciliation category (Cat 0..6).
	// Required; must be a valid ReconciliationCategory constant.
	Category ReconciliationCategory `json:"category"`

	// EvidenceRef is an opaque reference to the evidence driving the
	// classification (e.g., a JSONL event_id or reconciliation artifact path).
	// Required (non-empty).
	EvidenceRef string `json:"evidence_ref"`

	// PostCrashWindow is an optional flag indicating whether the classification
	// was made within the post-crash lossy-tail window per EV-023.
	// Corresponds to post_crash_window? in §8.6.2. Nil when not applicable.
	PostCrashWindow *bool `json:"post_crash_window,omitempty"`
}

// Valid reports whether p is a well-formed ReconciliationCategoryAssignedPayload.
//
// Rules per event-model.md §8.6.2:
//   - ReconciliationRunID must not be uuid.Nil.
//   - TargetRunID, when non-nil, must not be uuid.Nil.
//   - Category must be a valid ReconciliationCategory constant.
//   - EvidenceRef must be non-empty.
func (p ReconciliationCategoryAssignedPayload) Valid() bool {
	if uuid.UUID(p.ReconciliationRunID) == uuid.Nil {
		return false
	}
	if p.TargetRunID != nil && uuid.UUID(*p.TargetRunID) == uuid.Nil {
		return false
	}
	if !p.Category.Valid() {
		return false
	}
	if p.EvidenceRef == "" {
		return false
	}
	return true
}

// ReconciliationVerdictEmittedPayload is the typed event payload for the
// reconciliation_verdict_emitted event (event-model.md §8.6.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation verdict observability per
// reconciliation/spec.md §4.5).
//
// Emitted by the daemon-core when the investigator agent produces a verdict.
//
// # Payload fields (event-model.md §8.6.3)
//
//   - investigator_run_id — the run_id of the reconciliation investigator
//   - target_run_id       — the run_id of the run being reconciled
//   - verdict             — the reconciliation verdict per reconciliation/spec.md §4.5
//   - rationale           — optional human-readable rationale from the investigator
type ReconciliationVerdictEmittedPayload struct {
	// InvestigatorRunID is the run_id of the reconciliation investigator workflow.
	// Required (must not be uuid.Nil).
	InvestigatorRunID RunID `json:"investigator_run_id"`

	// TargetRunID is the run_id of the run being reconciled.
	// Required (must not be uuid.Nil).
	TargetRunID RunID `json:"target_run_id"`

	// Verdict is the reconciliation verdict. Required; must be a valid
	// Verdict constant per reconciliation/spec.md §4.5.
	Verdict Verdict `json:"verdict"`

	// Rationale is an optional human-readable rationale from the investigator.
	// Corresponds to rationale? in §8.6.3. Nil when no rationale is provided.
	// Non-nil must be non-empty.
	Rationale *string `json:"rationale,omitempty"`
}

// Valid reports whether p is a well-formed ReconciliationVerdictEmittedPayload.
//
// Rules per event-model.md §8.6.3:
//   - InvestigatorRunID must not be uuid.Nil.
//   - TargetRunID must not be uuid.Nil.
//   - Verdict must be a valid Verdict constant.
//   - Rationale, when non-nil, must be non-empty.
func (p ReconciliationVerdictEmittedPayload) Valid() bool {
	if uuid.UUID(p.InvestigatorRunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.TargetRunID) == uuid.Nil {
		return false
	}
	if !p.Verdict.Valid() {
		return false
	}
	if p.Rationale != nil && *p.Rationale == "" {
		return false
	}
	return true
}

// DivergenceKind is the typed discriminator for the divergence_kind field of a
// store_divergence_detected event (event-model.md §8.6.8 §6.3).
//
// The enum is closed at MVH per the §6.3 note. Adapter-specific values are
// reserved for a future revision per OQ-BI-008; until then, adapters emit
// divergence_inconclusive (§8.6.10) per EV-023a's single-authority semantics.
type DivergenceKind string

const (
	// DivergenceKindCheckpointMissing indicates a checkpoint present in JSONL
	// is missing from git.
	DivergenceKindCheckpointMissing DivergenceKind = "checkpoint_missing"

	// DivergenceKindBeadsClosedNoCommit indicates a bead is closed in Beads
	// but the corresponding git commit is absent.
	DivergenceKindBeadsClosedNoCommit DivergenceKind = "beads_closed_no_commit"

	// DivergenceKindJSONLReferencesMissingCommit indicates the JSONL log
	// references a git commit that cannot be found in the DAG.
	DivergenceKindJSONLReferencesMissingCommit DivergenceKind = "jsonl_references_missing_commit"

	// DivergenceKindParseFailure indicates a JSONL line or git object failed
	// to parse.
	DivergenceKindParseFailure DivergenceKind = "parse_failure"

	// DivergenceKindSchemaMismatch indicates a payload schema version mismatch
	// between the JSONL record and the current registry.
	DivergenceKindSchemaMismatch DivergenceKind = "schema_mismatch"

	// DivergenceKindLogMissing indicates the JSONL log file is missing or
	// unreadable.
	DivergenceKindLogMissing DivergenceKind = "log_missing"
)

// Valid reports whether k is one of the six declared DivergenceKind constants.
// Unknown values are not tolerated per the closed-enum note at §6.3.
func (k DivergenceKind) Valid() bool {
	switch k {
	case DivergenceKindCheckpointMissing,
		DivergenceKindBeadsClosedNoCommit,
		DivergenceKindJSONLReferencesMissingCommit,
		DivergenceKindParseFailure,
		DivergenceKindSchemaMismatch,
		DivergenceKindLogMissing:
		return true
	default:
		return false
	}
}

// Note: DivergenceCorroboration type and constants are defined in
// divergencecorroboration.go (DivergenceCorroborationGitCorroborated,
// DivergenceCorroborationBeadsCorroborated) per EV-023a.

// StoreDivergenceDetectedPayload is the typed event payload for the
// store_divergence_detected event (event-model.md §8.6.8 §6.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation and audit input; the detector
// MUST emit only corroborated evidence per EV-023a).
//
// Emitted by the daemon-core reconciliation detector when a store divergence is
// corroborated. Inconclusive evidence produces divergence_inconclusive (§8.6.10)
// instead per EV-023a's single-authority semantics.
//
// # Payload fields (event-model.md §8.6.8 §6.3)
//
//   - run_id          — optional run_id associated with the divergence
//   - bead_id         — optional Beads bead ID associated with the divergence
//   - divergence_kind — classified type of divergence (closed enum per §6.3)
//   - evidence_ref    — opaque reference to the divergence evidence
//   - post_crash_window — true when evidence falls in the post-crash lossy-tail window per EV-023
//   - corroboration   — how the evidence was corroborated (git-corroborated / beads-corroborated)
type StoreDivergenceDetectedPayload struct {
	// RunID is the optional run_id associated with the divergence.
	// Corresponds to run_id? in §8.6.8 §6.3. Nil when not run-scoped.
	// Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// BeadID is the optional Beads bead ID associated with the divergence.
	// Corresponds to bead_id? in §8.6.8 §6.3. Nil when not bead-scoped.
	// Non-nil must be non-empty.
	BeadID *BeadID `json:"bead_id,omitempty"`

	// DivergenceKind classifies the type of divergence.
	// Required; must be a valid DivergenceKind constant per §6.3 closed enum.
	DivergenceKind DivergenceKind `json:"divergence_kind"`

	// EvidenceRef is an opaque reference to the divergence evidence (e.g.,
	// a JSONL event_id, git ref, or path). Required (non-empty).
	EvidenceRef string `json:"evidence_ref"`

	// PostCrashWindow is true when the evidence falls within the post-crash
	// lossy-tail window per EV-023. Required (always present).
	PostCrashWindow bool `json:"post_crash_window"`

	// Corroboration indicates how the evidence was corroborated.
	// Required; must be a valid DivergenceCorroboration constant.
	// Only corroborated evidence reaches this event type per EV-023a.
	Corroboration DivergenceCorroboration `json:"corroboration"`
}

// Valid reports whether p is a well-formed StoreDivergenceDetectedPayload.
//
// Rules per event-model.md §8.6.8 §6.3 and EV-023a:
//   - RunID, when non-nil, must not be uuid.Nil.
//   - BeadID, when non-nil, must be non-empty.
//   - DivergenceKind must be a valid DivergenceKind constant.
//   - EvidenceRef must be non-empty.
//   - Corroboration must be a valid DivergenceCorroboration constant.
func (p StoreDivergenceDetectedPayload) Valid() bool {
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID != nil && *p.BeadID == "" {
		return false
	}
	if !p.DivergenceKind.Valid() {
		return false
	}
	if p.EvidenceRef == "" {
		return false
	}
	if !p.Corroboration.Valid() {
		return false
	}
	return true
}

// OperatorEscalationReason is the typed discriminator for the reason field of
// an operator_escalation_required event (event-model.md §8.6.9 §6.3).
//
// The enum is widened per the §6.3 note referencing the escalation reason set.
type OperatorEscalationReason string

const (
	// OperatorEscalationReasonCat6aInvestigatorEscalated is a Cat 6a escalation
	// where the investigator agent explicitly escalated.
	OperatorEscalationReasonCat6aInvestigatorEscalated OperatorEscalationReason = "cat_6a_investigator_escalated"

	// OperatorEscalationReasonCat6bAutoEscalated is a Cat 6b escalation where
	// the daemon automatically escalated without investigator input.
	OperatorEscalationReasonCat6bAutoEscalated OperatorEscalationReason = "cat_6b_auto_escalated"

	// OperatorEscalationReasonCat3StaleWrite is a Cat 3 escalation due to a
	// detected stale write.
	OperatorEscalationReasonCat3StaleWrite OperatorEscalationReason = "cat_3_stale_write"

	// OperatorEscalationReasonBudgetExhausted is an escalation due to budget
	// exhaustion on a critical path.
	OperatorEscalationReasonBudgetExhausted OperatorEscalationReason = "budget_exhausted"

	// OperatorEscalationReasonMergeConflict is an escalation due to an
	// unresolvable merge conflict.
	OperatorEscalationReasonMergeConflict OperatorEscalationReason = "merge_conflict"

	// OperatorEscalationReasonGateEscalated is an escalation triggered by a
	// gate_escalated control-point verdict.
	OperatorEscalationReasonGateEscalated OperatorEscalationReason = "gate_escalated"

	// OperatorEscalationReasonOtherVerdictDriven is a verdict-driven escalation
	// not covered by the other reason constants.
	OperatorEscalationReasonOtherVerdictDriven OperatorEscalationReason = "other_verdict_driven"
)

// Valid reports whether r is one of the seven declared OperatorEscalationReason constants.
func (r OperatorEscalationReason) Valid() bool {
	switch r {
	case OperatorEscalationReasonCat6aInvestigatorEscalated,
		OperatorEscalationReasonCat6bAutoEscalated,
		OperatorEscalationReasonCat3StaleWrite,
		OperatorEscalationReasonBudgetExhausted,
		OperatorEscalationReasonMergeConflict,
		OperatorEscalationReasonGateEscalated,
		OperatorEscalationReasonOtherVerdictDriven:
		return true
	default:
		return false
	}
}

// OperatorEscalationRequiredPayload is the typed event payload for the
// operator_escalation_required event (event-model.md §8.6.9 §6.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — operator-observability and audit).
//
// Emitted by the daemon-core when a reconciliation condition requires
// operator intervention. Paired with operator_escalation_cleared (§8.7.17).
//
// # Payload fields (event-model.md §8.6.9 §6.3)
//
//   - target_run_id     — optional run_id of the run requiring escalation
//   - reason            — classified escalation reason (enum per §6.3)
//   - reference_commits — optional list of git commit SHAs providing context
type OperatorEscalationRequiredPayload struct {
	// TargetRunID is the optional run_id of the run requiring escalation.
	// Corresponds to target_run_id? in §8.6.9 §6.3. Nil when not run-scoped.
	// Non-nil must not be uuid.Nil.
	TargetRunID *RunID `json:"target_run_id,omitempty"`

	// Reason classifies why operator escalation is required.
	// Required; must be a valid OperatorEscalationReason constant.
	Reason OperatorEscalationReason `json:"reason"`

	// ReferenceCommits is an optional list of git commit SHAs providing
	// context for the escalation. Corresponds to reference_commits[]? in §8.6.9.
	// Nil or empty when no commits are referenced.
	ReferenceCommits []string `json:"reference_commits,omitempty"`
}

// Valid reports whether p is a well-formed OperatorEscalationRequiredPayload.
//
// Rules per event-model.md §8.6.9 §6.3:
//   - TargetRunID, when non-nil, must not be uuid.Nil.
//   - Reason must be a valid OperatorEscalationReason constant.
func (p OperatorEscalationRequiredPayload) Valid() bool {
	if p.TargetRunID != nil && uuid.UUID(*p.TargetRunID) == uuid.Nil {
		return false
	}
	if !p.Reason.Valid() {
		return false
	}
	return true
}

// DivergenceInconclusiveReason is the typed discriminator for the reason field
// of a divergence_inconclusive event (event-model.md §8.6.10 §6.3).
//
// Per EV-023a, inconclusive evidence cannot be attributed to git or Beads and
// is classified by this reason enum.
type DivergenceInconclusiveReason string

const (
	// DivergenceInconclusiveReasonNoAuthorityReference indicates the evidence
	// does not carry any git commit hash or bead_id that could be checked.
	DivergenceInconclusiveReasonNoAuthorityReference DivergenceInconclusiveReason = "no_authority_reference"

	// DivergenceInconclusiveReasonAuthorityUnavailable indicates the evidence
	// carries an authority reference but the authority (git or Beads) was
	// unavailable at detection time.
	DivergenceInconclusiveReasonAuthorityUnavailable DivergenceInconclusiveReason = "authority_unavailable"
)

// Valid reports whether r is one of the two declared DivergenceInconclusiveReason constants.
func (r DivergenceInconclusiveReason) Valid() bool {
	switch r {
	case DivergenceInconclusiveReasonNoAuthorityReference, DivergenceInconclusiveReasonAuthorityUnavailable:
		return true
	default:
		return false
	}
}

// DivergenceInconclusivePayload is the typed event payload for the
// divergence_inconclusive event (event-model.md §8.6.10 §6.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation audit and operator context;
// inconclusive evidence cannot itself drive a reconciliation action per EV-023a).
//
// Emitted by the daemon-core reconciliation detector when evidence cannot be
// corroborated against git or Beads. store_divergence_detected (§8.6.8) is
// emitted instead when evidence is corroborated.
//
// # Payload fields (event-model.md §8.6.10 §6.3)
//
//   - run_id          — optional run_id associated with the inconclusive evidence
//   - bead_id         — optional Beads bead ID associated with the inconclusive evidence
//   - evidence_ref    — opaque reference to the evidence that could not be corroborated
//   - post_crash_window — true when evidence falls in the post-crash lossy-tail window per EV-023
//   - reason          — why the evidence was inconclusive
type DivergenceInconclusivePayload struct {
	// RunID is the optional run_id associated with the inconclusive evidence.
	// Corresponds to run_id? in §8.6.10 §6.3. Nil when not run-scoped.
	// Non-nil must not be uuid.Nil.
	RunID *RunID `json:"run_id,omitempty"`

	// BeadID is the optional Beads bead ID associated with the inconclusive evidence.
	// Corresponds to bead_id? in §8.6.10 §6.3. Nil when not bead-scoped.
	// Non-nil must be non-empty.
	BeadID *BeadID `json:"bead_id,omitempty"`

	// EvidenceRef is an opaque reference to the evidence that could not be
	// corroborated. Required (non-empty).
	EvidenceRef string `json:"evidence_ref"`

	// PostCrashWindow is true when the evidence falls within the post-crash
	// lossy-tail window per EV-023. Required (always present).
	PostCrashWindow bool `json:"post_crash_window"`

	// Reason classifies why the evidence was inconclusive.
	// Required; must be a valid DivergenceInconclusiveReason constant.
	Reason DivergenceInconclusiveReason `json:"reason"`
}

// Valid reports whether p is a well-formed DivergenceInconclusivePayload.
//
// Rules per event-model.md §8.6.10 §6.3 and EV-023a:
//   - RunID, when non-nil, must not be uuid.Nil.
//   - BeadID, when non-nil, must be non-empty.
//   - EvidenceRef must be non-empty.
//   - Reason must be a valid DivergenceInconclusiveReason constant.
func (p DivergenceInconclusivePayload) Valid() bool {
	if p.RunID != nil && uuid.UUID(*p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID != nil && *p.BeadID == "" {
		return false
	}
	if p.EvidenceRef == "" {
		return false
	}
	if !p.Reason.Valid() {
		return false
	}
	return true
}

// ReconciliationDispatchDeduplicatedPayload is the typed event payload for the
// reconciliation_dispatch_deduplicated event (event-model.md §8.6.11).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation-monitoring and audit; the
// dedup gate uses flock(LOCK_EX|LOCK_NB) per RC-002a).
//
// Emitted by the daemon-core when a second dispatch for the same target run is
// deduplicated because an investigator is already running for that run.
//
// # Payload fields (event-model.md §8.6.11)
//
//   - target_run_id                 — the run_id of the run a second dispatch was attempted for
//   - existing_investigator_run_id  — optional run_id of the already-running investigator
//   - dedup_at                      — RFC 3339 wall-clock timestamp at deduplication
type ReconciliationDispatchDeduplicatedPayload struct {
	// TargetRunID is the run_id of the run for which a second dispatch was
	// attempted. Required (must not be uuid.Nil).
	TargetRunID RunID `json:"target_run_id"`

	// ExistingInvestigatorRunID is the run_id of the already-running investigator,
	// when known. Corresponds to existing_investigator_run_id? in §8.6.11.
	// Nil when the existing investigator run_id cannot be determined.
	// Non-nil must not be uuid.Nil.
	ExistingInvestigatorRunID *RunID `json:"existing_investigator_run_id,omitempty"`

	// DedupAt is the RFC 3339 wall-clock timestamp at deduplication.
	// Required (non-empty).
	DedupAt string `json:"dedup_at"`
}

// Valid reports whether p is a well-formed ReconciliationDispatchDeduplicatedPayload.
//
// Rules per event-model.md §8.6.11 and RC-002a:
//   - TargetRunID must not be uuid.Nil.
//   - ExistingInvestigatorRunID, when non-nil, must not be uuid.Nil.
//   - DedupAt must be non-empty.
func (p ReconciliationDispatchDeduplicatedPayload) Valid() bool {
	if uuid.UUID(p.TargetRunID) == uuid.Nil {
		return false
	}
	if p.ExistingInvestigatorRunID != nil && uuid.UUID(*p.ExistingInvestigatorRunID) == uuid.Nil {
		return false
	}
	if p.DedupAt == "" {
		return false
	}
	return true
}

// ReconciliationDetectorPanicPayload is the typed event payload for the
// reconciliation_detector_panic event (event-model.md §8.6.12).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation-monitoring, audit, and
// operator-observability; the per-detector recover() barrier emits this per
// RC-020b before restarting the detector).
//
// Emitted by the daemon-core when a reconciliation detector's per-detector
// panic-recovery barrier catches a panic, per RC-020b.
//
// # Payload fields (event-model.md §8.6.12)
//
//   - detector_class — identifies which detector class panicked
//   - error_class    — typed error class of the panic
//   - panicked_at    — RFC 3339 wall-clock timestamp at panic
type ReconciliationDetectorPanicPayload struct {
	// DetectorClass identifies which reconciliation detector class panicked.
	// Required (non-empty). See detectorclass.go (hk-hqwn.75).
	DetectorClass DetectorClass `json:"detector_class"`

	// ErrorClass is the typed error class of the panic.
	// Required; must be a valid ErrorCategory constant per §8.6.12 RC-020b.
	ErrorClass ErrorCategory `json:"error_class"`

	// PanickedAt is the RFC 3339 wall-clock timestamp at panic.
	// Required (non-empty).
	PanickedAt string `json:"panicked_at"`
}

// Valid reports whether p is a well-formed ReconciliationDetectorPanicPayload.
//
// Rules per event-model.md §8.6.12 and RC-020b:
//   - DetectorClass must be non-empty.
//   - ErrorClass must be a valid ErrorCategory constant.
//   - PanickedAt must be non-empty.
func (p ReconciliationDetectorPanicPayload) Valid() bool {
	if p.DetectorClass == DetectorClass("") {
		return false
	}
	if !p.ErrorClass.Valid() {
		return false
	}
	if p.PanickedAt == "" {
		return false
	}
	return true
}

// ReconciliationVerdictExecutionRetryPayload is the typed event payload for the
// reconciliation_verdict_execution_retry event (event-model.md §8.6.13).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation-monitoring and audit; the retry
// cap is N=5 per RC-026a).
//
// Emitted by the daemon-core when verdict execution is retried (Cat 3b retry
// per RC-026a), before the retry attempt is made.
//
// # Payload fields (event-model.md §8.6.13)
//
//   - target_run_id — the run_id of the run whose verdict execution is being retried
//   - attempt       — the one-based retry attempt number (1 = first retry after initial failure)
//   - retried_at    — RFC 3339 wall-clock timestamp at retry
type ReconciliationVerdictExecutionRetryPayload struct {
	// TargetRunID is the run_id of the run whose verdict execution is being retried.
	// Required (must not be uuid.Nil).
	TargetRunID RunID `json:"target_run_id"`

	// Attempt is the one-based retry attempt number. The first retry after the
	// initial failure is attempt=1; the cap is N=5 per RC-026a.
	// Required (must be >= 1).
	Attempt int `json:"attempt"`

	// RetriedAt is the RFC 3339 wall-clock timestamp at retry.
	// Required (non-empty).
	RetriedAt string `json:"retried_at"`
}

// Valid reports whether p is a well-formed ReconciliationVerdictExecutionRetryPayload.
//
// Rules per event-model.md §8.6.13 and RC-026a:
//   - TargetRunID must not be uuid.Nil.
//   - Attempt must be >= 1.
//   - RetriedAt must be non-empty.
func (p ReconciliationVerdictExecutionRetryPayload) Valid() bool {
	if uuid.UUID(p.TargetRunID) == uuid.Nil {
		return false
	}
	if p.Attempt < 1 {
		return false
	}
	if p.RetriedAt == "" {
		return false
	}
	return true
}

// ReconciliationCompletedPayload is the typed event payload for the
// reconciliation_completed event.
//
// Emitted after each reconciliation scan (startup or scheduled cadence) finishes,
// paired with reconciliation_started so that a hung reconciliation is detectable
// (reconciliation_started with no matching reconciliation_completed).
//
// # Payload fields
//
//   - reconciliation_run_id — the run_id of the reconciliation workflow (matches reconciliation_started)
//   - trigger               — what triggered this reconciliation run
//   - beads_examined        — number of in_progress beads examined during the scan
//   - beads_closed          — number of beads auto-closed (Cat 3c)
//   - beads_reset           — number of beads reset to open (Class B orphan repair)
//   - completed_at          — RFC 3339 wall-clock timestamp at completion
type ReconciliationCompletedPayload struct {
	// ReconciliationRunID is the run_id of the reconciliation workflow.
	// Required (must not be uuid.Nil). Matches the reconciliation_started event.
	ReconciliationRunID RunID `json:"reconciliation_run_id"`

	// Trigger mirrors the trigger from the paired reconciliation_started event.
	// Required; must be a valid ReconciliationTrigger constant.
	Trigger ReconciliationTrigger `json:"trigger"`

	// BeadsExamined is the number of in_progress beads examined during the scan.
	// Required (must be >= 0).
	BeadsExamined int `json:"beads_examined"`

	// BeadsClosed is the number of beads auto-closed via Cat 3c resolution.
	// Required (must be >= 0).
	BeadsClosed int `json:"beads_closed"`

	// BeadsReset is the number of beads reset to open by the Class B orphan repair.
	// Required (must be >= 0).
	BeadsReset int `json:"beads_reset"`

	// CompletedAt is the RFC 3339 wall-clock timestamp at scan completion.
	// Required (non-empty).
	CompletedAt string `json:"completed_at"`
}

// Valid reports whether p is a well-formed ReconciliationCompletedPayload.
func (p ReconciliationCompletedPayload) Valid() bool {
	if uuid.UUID(p.ReconciliationRunID) == uuid.Nil {
		return false
	}
	if !p.Trigger.Valid() {
		return false
	}
	if p.BeadsExamined < 0 || p.BeadsClosed < 0 || p.BeadsReset < 0 {
		return false
	}
	if p.CompletedAt == "" {
		return false
	}
	return true
}

// BeadTerminalTransitionOp is the typed discriminator for the op field of a
// bead_terminal_transition_recovered event (event-model.md §8.6.14).
//
// This type is declared at MVH even though the event itself is post-MVH, so
// the type identifier is burned per OQ-BI-008 for future BI-adapter use and
// not reused for any other purpose.
type BeadTerminalTransitionOp string

const (
	// BeadTerminalTransitionOpClaim is the claim terminal transition.
	BeadTerminalTransitionOpClaim BeadTerminalTransitionOp = "claim"

	// BeadTerminalTransitionOpClose is the close terminal transition.
	BeadTerminalTransitionOpClose BeadTerminalTransitionOp = "close"

	// BeadTerminalTransitionOpReopen is the reopen terminal transition.
	BeadTerminalTransitionOpReopen BeadTerminalTransitionOp = "reopen"
)

// Valid reports whether o is one of the three declared BeadTerminalTransitionOp constants.
func (o BeadTerminalTransitionOp) Valid() bool {
	switch o {
	case BeadTerminalTransitionOpClaim, BeadTerminalTransitionOpClose, BeadTerminalTransitionOpReopen:
		return true
	default:
		return false
	}
}

// BeadTerminalTransitionRecoveredPayload is the typed event payload for the
// bead_terminal_transition_recovered event (event-model.md §8.6.14).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reserved for post-MVH BI adapter emission per OQ-BI-008).
//
// **(post-MVH)** This event type is reserved for a future revision per OQ-BI-008.
// At MVH, the BI adapter emits a structured-log record per operator-nfr.md §4.9
// ON-035 for adapter-recovery observability rather than this event. The type is
// declared here so the identifier is burned and not reused for any other purpose.
// No MVH conformance obligation attaches to §8.6.14.
//
// # Payload fields (event-model.md §8.6.14)
//
//   - bead_id         — the Beads bead ID that was recovered
//   - op              — the terminal transition operation that was recovered
//   - idempotency_key — the idempotency key used for the recovered write
//   - recovered_at    — RFC 3339 wall-clock timestamp at recovery
type BeadTerminalTransitionRecoveredPayload struct {
	// BeadID is the Beads bead ID that was recovered.
	// Required (non-empty).
	BeadID BeadID `json:"bead_id"`

	// Op is the terminal transition operation that was recovered.
	// Required; must be a valid BeadTerminalTransitionOp constant.
	Op BeadTerminalTransitionOp `json:"op"`

	// IdempotencyKey is the idempotency key used for the recovered write.
	// Required (non-empty).
	IdempotencyKey string `json:"idempotency_key"`

	// RecoveredAt is the RFC 3339 wall-clock timestamp at recovery.
	// Required (non-empty).
	RecoveredAt string `json:"recovered_at"`
}

// Valid reports whether p is a well-formed BeadTerminalTransitionRecoveredPayload.
//
// Rules per event-model.md §8.6.14 (post-MVH reserved; declared for type-burning per OQ-BI-008):
//   - BeadID must be non-empty.
//   - Op must be a valid BeadTerminalTransitionOp constant.
//   - IdempotencyKey must be non-empty.
//   - RecoveredAt must be non-empty.
func (p BeadTerminalTransitionRecoveredPayload) Valid() bool {
	if p.BeadID == "" {
		return false
	}
	if !p.Op.Valid() {
		return false
	}
	if p.IdempotencyKey == "" {
		return false
	}
	if p.RecoveredAt == "" {
		return false
	}
	return true
}
