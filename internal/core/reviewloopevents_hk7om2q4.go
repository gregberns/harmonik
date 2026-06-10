package core

import "github.com/google/uuid"

// reviewloopevents_hk7om2q4.go — event-bus payload types for §8.1a review-loop
// cycle events and §8.8.6 bead_label_conflict:
//
//   - implementer_resumed       (§8.1a.1)
//   - reviewer_launched         (§8.1a.2)
//   - reviewer_verdict          (§8.1a.3)
//   - iteration_cap_hit         (§8.1a.4)
//   - no_progress_detected      (§8.1a.5)
//   - review_loop_cycle_complete (§8.1a.6)
//   - bead_label_conflict       (§8.8.6)
//
// Spec ref: specs/event-model.md §8.1a, §8.8.6, §6.3.
// Bead ref: hk-7om2q.4.

// ---------------------------------------------------------------------------
// Enum types for §8.1a payload discriminators
// ---------------------------------------------------------------------------

// ReviewerVerdict is the verdict value from the agent-reviewer JSON schema v1.
// Used in reviewer_verdict (§8.1a.3) and iteration_cap_hit (§8.1a.4) payloads.
//
// Values MUST match the agent-reviewer skill's JSON verdict schema v1 verbatim
// per event-model.md §8.1a.3 reviewer-verdict schema-reuse rule.
type ReviewerVerdict string

const (
	// ReviewerVerdictApprove indicates the reviewer approved the implementer output.
	ReviewerVerdictApprove ReviewerVerdict = "APPROVE"

	// ReviewerVerdictRequestChanges indicates the reviewer requires changes
	// before approval.
	ReviewerVerdictRequestChanges ReviewerVerdict = "REQUEST_CHANGES"

	// ReviewerVerdictBlock indicates the reviewer has blocked the output entirely.
	ReviewerVerdictBlock ReviewerVerdict = "BLOCK"
)

// Valid reports whether v is one of the declared ReviewerVerdict constants.
func (v ReviewerVerdict) Valid() bool {
	switch v {
	case ReviewerVerdictApprove, ReviewerVerdictRequestChanges, ReviewerVerdictBlock:
		return true
	default:
		return false
	}
}

// ReviewLoopCompletionReason is the completion_reason discriminator for
// review_loop_cycle_complete (§8.1a.6).
//
// Values per event-model.md §8.1a.6 and execution-model.md §4.3.EM-015e.
type ReviewLoopCompletionReason string

const (
	// ReviewLoopCompletionReasonApproved indicates the cycle ended with a
	// reviewer APPROVE verdict.
	ReviewLoopCompletionReasonApproved ReviewLoopCompletionReason = "approved"

	// ReviewLoopCompletionReasonCapHit indicates the iteration cap was
	// exhausted (cap = 3 at MVH per execution-model.md §4.3.EM-015e).
	ReviewLoopCompletionReasonCapHit ReviewLoopCompletionReason = "cap_hit"

	// ReviewLoopCompletionReasonBlocked indicates the cycle ended with a
	// reviewer BLOCK verdict.
	ReviewLoopCompletionReasonBlocked ReviewLoopCompletionReason = "blocked"

	// ReviewLoopCompletionReasonNoProgress indicates no-progress was detected
	// (diff_hash_current == diff_hash_prior) per §8.1a emission-ordering rule.
	ReviewLoopCompletionReasonNoProgress ReviewLoopCompletionReason = "no_progress"

	// ReviewLoopCompletionReasonError indicates the cycle terminated due to
	// an error (e.g., malformed verdict file per §8.1a.3 reviewer-verdict
	// schema-reuse rule).
	ReviewLoopCompletionReasonError ReviewLoopCompletionReason = "error"

	// ReviewLoopCompletionReasonFixupStalled indicates the cycle terminated
	// because a REQUEST_CHANGES fix-up run advanced HEAD by zero commits — the
	// implementer was given reviewer feedback but produced no new commit in
	// response. Distinct from no_progress (the generic no-commit failure class)
	// because it carries the reviewer flags from the prior REQUEST_CHANGES verdict.
	// Bead ref: hk-m1wqp.
	ReviewLoopCompletionReasonFixupStalled ReviewLoopCompletionReason = "fixup_stalled"
)

// Valid reports whether r is one of the declared ReviewLoopCompletionReason constants.
func (r ReviewLoopCompletionReason) Valid() bool {
	switch r {
	case ReviewLoopCompletionReasonApproved,
		ReviewLoopCompletionReasonCapHit,
		ReviewLoopCompletionReasonBlocked,
		ReviewLoopCompletionReasonNoProgress,
		ReviewLoopCompletionReasonError,
		ReviewLoopCompletionReasonFixupStalled:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// §8.1a.1 — implementer_resumed
// ---------------------------------------------------------------------------

// ImplementerResumedPayload is the typed event payload for the
// implementer_resumed event (event-model.md §8.1a.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability; emitted by orchestrator-core
// on every implementer launch after iteration 1 within a review-loop run).
//
// Emitted by orchestrator-core immediately before dispatching the implementer
// for iteration ≥ 2. Iteration 1 is covered by run_started (§8.1); this event
// fires only from iteration 2 onward. The prior_verdict_summary is front-
// truncated to 256 UTF-8 bytes from the prior reviewer_verdict.notes per §6.3.
//
// # Payload fields (event-model.md §8.1a.1)
//
//   - run_id               — the umbrella run_id for this review-loop cycle
//   - workflow_mode        — always "review-loop" for this event
//   - session_id           — harmonik handler-minted UUIDv7 for this implementer session
//   - claude_session_id    — Claude Code session identifier per §3 glossary
//   - iteration_count      — 1-based iteration number (≥ 2 for this event)
//   - prior_verdict_summary — ≤ 256 bytes; front-truncation of prior reviewer notes
type ImplementerResumedPayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is always WorkflowModeReviewLoop for this event.
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// SessionID is the harmonik handler-minted UUIDv7 for this implementer session.
	// Required (non-empty). Distinct from ClaudeSessionID; correlates with
	// agent_started / agent_completed per handler-contract.md §4.1.
	SessionID SessionID `json:"session_id"`

	// ClaudeSessionID is the Claude Code session identifier per §3 glossary.
	// Required (non-empty). Used for `claude --resume <id>` continuity.
	ClaudeSessionID string `json:"claude_session_id"`

	// IterationCount is the 1-based iteration number. Required (must be ≥ 2
	// because iteration 1 is covered by run_started, not this event).
	IterationCount int `json:"iteration_count"`

	// PriorVerdictSummary is the first 256 UTF-8 bytes of the prior iteration's
	// reviewer_verdict.notes, front-truncated with any incomplete trailing
	// UTF-8 sequence discarded per §6.3 derivation rule. Required (non-empty).
	PriorVerdictSummary string `json:"prior_verdict_summary"`
}

// Valid reports whether p is a well-formed ImplementerResumedPayload.
//
// Rules per event-model.md §8.1a.1:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - SessionID must be non-empty.
//   - ClaudeSessionID must be non-empty.
//   - IterationCount must be ≥ 2 (iteration 1 has no implementer_resumed).
//   - PriorVerdictSummary must be non-empty.
func (p ImplementerResumedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ClaudeSessionID == "" {
		return false
	}
	if p.IterationCount < 2 {
		return false
	}
	if p.PriorVerdictSummary == "" {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.1a.2 — reviewer_launched
// ---------------------------------------------------------------------------

// ReviewerLaunchedPayload is the typed event payload for the reviewer_launched
// event (event-model.md §8.1a.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observability; emitted by orchestrator-core
// immediately before dispatching the reviewer subprocess for each iteration).
//
// # Payload fields (event-model.md §8.1a.2)
//
//   - run_id            — the umbrella run_id for this review-loop cycle
//   - workflow_mode     — always "review-loop" for this event
//   - session_id        — harmonik handler-minted UUIDv7 for this reviewer session
//   - claude_session_id — Claude Code session identifier; reviewer launches a
//     fresh session (not resumed) per execution-model.md §4.3.EM-015d
//   - iteration_count   — 1-based iteration number
type ReviewerLaunchedPayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is always WorkflowModeReviewLoop for this event.
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// SessionID is the harmonik handler-minted UUIDv7 for this reviewer session.
	// Required (non-empty). Correlates with agent_started / agent_completed.
	SessionID SessionID `json:"session_id"`

	// ClaudeSessionID is the Claude Code session identifier for this reviewer
	// session. Required (non-empty). The reviewer launches a fresh (not resumed)
	// session per execution-model.md §4.3.EM-015d.
	ClaudeSessionID string `json:"claude_session_id"`

	// IterationCount is the 1-based iteration number. Required (must be ≥ 1).
	IterationCount int `json:"iteration_count"`
}

// Valid reports whether p is a well-formed ReviewerLaunchedPayload.
//
// Rules per event-model.md §8.1a.2:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - SessionID must be non-empty.
//   - ClaudeSessionID must be non-empty.
//   - IterationCount must be ≥ 1.
func (p ReviewerLaunchedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ClaudeSessionID == "" {
		return false
	}
	if p.IterationCount < 1 {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.1a.3 — reviewer_verdict
// ---------------------------------------------------------------------------

// ReviewerVerdictPayload is the typed event payload for the reviewer_verdict
// event (event-model.md §8.1a.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss would orphan the cycle's
// terminal-routing decision per EV-016; the verdict gates run_completed /
// run_failed emission ordering per §8.1a emission-ordering rule).
//
// Emitted by orchestrator-core after successfully reading and validating
// .harmonik/review.json. The schema_version, verdict, flags, and notes fields
// are passed through verbatim from the verdict file per §8.1a.3 reviewer-verdict
// schema-reuse rule. The daemon MUST NOT emit this event with a malformed
// verdict; instead it MUST emit review_loop_cycle_complete{completion_reason=error}.
//
// # Payload fields (event-model.md §8.1a.3)
//
//   - run_id            — the umbrella run_id for this review-loop cycle
//   - workflow_mode     — always "review-loop" for this event
//   - session_id        — harmonik handler-minted UUIDv7 for this reviewer session
//   - claude_session_id — Claude Code session identifier
//   - iteration_count   — 1-based iteration number
//   - schema_version    — MUST equal 1 (agent-reviewer JSON schema v1)
//   - verdict           — APPROVE | REQUEST_CHANGES | BLOCK (verbatim from file)
//   - flags             — issue tags from schema v1; MAY be empty
//   - notes             — free text from schema v1; 1–3 sentences per skill contract
type ReviewerVerdictPayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is always WorkflowModeReviewLoop for this event.
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// SessionID is the harmonik handler-minted UUIDv7 for the reviewer session.
	// Required (non-empty). Correlates with agent_started / agent_completed.
	SessionID SessionID `json:"session_id"`

	// ClaudeSessionID is the Claude Code session identifier for the reviewer session.
	// Required (non-empty).
	ClaudeSessionID string `json:"claude_session_id"`

	// IterationCount is the 1-based iteration number. Required (must be ≥ 1).
	IterationCount int `json:"iteration_count"`

	// SchemaVersion is the agent-reviewer JSON schema version. Required; MUST
	// equal 1 per §8.1a.3 reviewer-verdict schema-reuse rule.
	SchemaVersion int `json:"schema_version"`

	// Verdict is the reviewer's verdict. Required; must be a valid ReviewerVerdict
	// constant (APPROVE | REQUEST_CHANGES | BLOCK).
	Verdict ReviewerVerdict `json:"verdict"`

	// Flags is the list of issue tags from the agent-reviewer schema v1. Required
	// (non-nil; MAY be an empty slice when no flags are present).
	Flags []string `json:"flags"`

	// Notes is the free-text verdict rationale from the agent-reviewer schema v1.
	// Required (non-empty; 1–3 sentences per the agent-reviewer skill contract).
	Notes string `json:"notes"`
}

// Valid reports whether p is a well-formed ReviewerVerdictPayload.
//
// Rules per event-model.md §8.1a.3:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - SessionID must be non-empty.
//   - ClaudeSessionID must be non-empty.
//   - IterationCount must be ≥ 1.
//   - SchemaVersion must equal 1.
//   - Verdict must be a valid ReviewerVerdict constant.
//   - Flags must be non-nil.
//   - Notes must be non-empty.
func (p ReviewerVerdictPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ClaudeSessionID == "" {
		return false
	}
	if p.IterationCount < 1 {
		return false
	}
	if p.SchemaVersion != 1 {
		return false
	}
	if !p.Verdict.Valid() {
		return false
	}
	if p.Flags == nil {
		return false
	}
	if p.Notes == "" {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.1a.4 — iteration_cap_hit
// ---------------------------------------------------------------------------

// IterationCapHitPayload is the typed event payload for the iteration_cap_hit
// event (event-model.md §8.1a.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — deliberately downgraded from class F;
// terminal routing weight rests on review_loop_cycle_complete which is class F
// per the §8.1a emission-ordering rule and §8.1a Note).
//
// Emitted by orchestrator-core when the iteration cap is reached (cap = 3 at
// MVH per execution-model.md §4.3.EM-015e). Emitted BEFORE
// review_loop_cycle_complete{completion_reason=cap_hit} per §8.1a ordering rule.
//
// # Payload fields (event-model.md §8.1a.4)
//
//   - run_id         — the umbrella run_id for this review-loop cycle
//   - workflow_mode  — always "review-loop" for this event
//   - iteration_count — = cap_value at MVH (the iteration at which cap was hit)
//   - cap_value      — the configured cap (= 3 at MVH)
//   - final_verdict  — the verdict at the cap-hit boundary: REQUEST_CHANGES | BLOCK
type IterationCapHitPayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is always WorkflowModeReviewLoop for this event.
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// IterationCount is the 1-based iteration at which the cap was hit.
	// Required (must be ≥ 1). Equals CapValue at MVH.
	IterationCount int `json:"iteration_count"`

	// CapValue is the configured iteration cap. Required (must be ≥ 1).
	// = 3 at MVH per execution-model.md §4.3.EM-015e.
	CapValue int `json:"cap_value"`

	// FinalVerdict is the verdict at the cap-hit boundary. Required; must be
	// ReviewerVerdictRequestChanges or ReviewerVerdictBlock (APPROVE cannot
	// produce a cap-hit since the cycle terminates on APPROVE before cap).
	FinalVerdict ReviewerVerdict `json:"final_verdict"`
}

// Valid reports whether p is a well-formed IterationCapHitPayload.
//
// Rules per event-model.md §8.1a.4:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - IterationCount must be ≥ 1.
//   - CapValue must be ≥ 1.
//   - FinalVerdict must be REQUEST_CHANGES or BLOCK (not APPROVE).
func (p IterationCapHitPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.IterationCount < 1 {
		return false
	}
	if p.CapValue < 1 {
		return false
	}
	// APPROVE cannot co-occur with cap-hit; only REQUEST_CHANGES and BLOCK are valid.
	switch p.FinalVerdict {
	case ReviewerVerdictRequestChanges, ReviewerVerdictBlock:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// §8.1a.5 — no_progress_detected
// ---------------------------------------------------------------------------

// NoProgressDetectedPayload is the typed event payload for the
// no_progress_detected event (event-model.md §8.1a.5).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — improvement-loop signal; emitted before
// review_loop_cycle_complete{completion_reason=no_progress} per §8.1a ordering
// rule for review-loop mode; no-progress early-exit skips reviewer_launched per
// §8.1a(c). In DOT mode review_loop_cycle_complete is NOT emitted — the cascade
// terminates directly per the §8.1a ordering-rule DOT exemption).
//
// Emitted by orchestrator-core when the diff hash of the current iteration's
// worktree state matches the prior iteration's diff hash (indicating the
// implementer made no meaningful code changes). For workflow_mode=review-loop,
// emitted BEFORE review_loop_cycle_complete per §8.1a ordering rule (c). For
// workflow_mode=dot, emitted at cascade termination with no subsequent
// review_loop_cycle_complete.
//
// # Payload fields (event-model.md §8.1a.5)
//
//   - run_id             — the umbrella run_id for this review-loop or DOT cycle
//   - workflow_mode      — "review-loop" or "dot" (see §8.1a.5 normative note)
//   - iteration_count    — the iteration at which no-progress was detected (≥ 2)
//   - diff_hash_current  — SHA-256 hex of `git diff <parent>..<head>` at current iteration
//   - diff_hash_prior    — SHA-256 hex of the prior iteration's diff; equal to diff_hash_current
type NoProgressDetectedPayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is WorkflowModeReviewLoop or WorkflowModeDot for this event
	// (event-model.md §8.1a.5 normative: no_progress_detected is permitted from
	// both review-loop and DOT workflow modes).
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// IterationCount is the 1-based iteration at which no-progress was detected.
	// Required (must be ≥ 2; no-progress cannot be detected on iteration 1 since
	// there is no prior iteration to compare).
	IterationCount int `json:"iteration_count"`

	// DiffHashCurrent is the SHA-256 hex digest of `git diff <parent>..<head>` at
	// the current iteration's worktree state. Required (non-empty; 64-char hex).
	DiffHashCurrent string `json:"diff_hash_current"`

	// DiffHashPrior is the SHA-256 hex digest of the prior iteration's diff.
	// Required (non-empty; 64-char hex). Equal to DiffHashCurrent at emission time.
	DiffHashPrior string `json:"diff_hash_prior"`
}

// Valid reports whether p is a well-formed NoProgressDetectedPayload.
//
// Rules per event-model.md §8.1a.5:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - IterationCount must be ≥ 2.
//   - DiffHashCurrent must be non-empty.
//   - DiffHashPrior must be non-empty.
func (p NoProgressDetectedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.IterationCount < 2 {
		return false
	}
	if p.DiffHashCurrent == "" {
		return false
	}
	if p.DiffHashPrior == "" {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.1a.7 — review_fixup_stalled
// ---------------------------------------------------------------------------

// ReviewFixupStalledPayload is the typed event payload for the
// review_fixup_stalled event (event-model.md §8.1a.7).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — improvement-loop signal; emitted before
// review_loop_cycle_complete{completion_reason=fixup_stalled} in review-loop
// mode; terminates the DOT cascade directly per §8.1a ordering-rule DOT
// exemption; distinct from no_progress_detected because it carries the reviewer
// flags from the prior REQUEST_CHANGES verdict).
//
// Emitted when a REQUEST_CHANGES fix-up run advances HEAD by zero commits —
// the implementer was given reviewer feedback but produced no new commit.
// Carrying the reviewer flags exposes the specific flag the implementer failed
// to address, so triage can act without reading the full verdict text.
//
// # Payload fields
//
//   - run_id            — the umbrella run_id for this review-loop or DOT cycle
//   - workflow_mode     — "review-loop" or "dot"
//   - iteration_count   — the iteration at which the stall was detected (≥ 2)
//   - reviewer_flags    — flags from the prior REQUEST_CHANGES verdict (MAY be empty)
//   - diff_hash_current — SHA-256 hex of `git diff <parent>..<head>` at current iteration
//   - diff_hash_prior   — SHA-256 hex of the prior iteration's diff
//
// Bead ref: hk-m1wqp.
type ReviewFixupStalledPayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is WorkflowModeReviewLoop or WorkflowModeDot.
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// IterationCount is the 1-based iteration at which the stall was detected.
	// Required (must be ≥ 2; a fix-up stall requires at least one prior
	// REQUEST_CHANGES verdict, which cannot occur before iteration 2).
	IterationCount int `json:"iteration_count"`

	// ReviewerFlags is the list of issue tags from the prior REQUEST_CHANGES
	// reviewer verdict. Required (non-nil; MAY be an empty slice when the
	// reviewer emitted no flags in the REQUEST_CHANGES verdict).
	ReviewerFlags []string `json:"reviewer_flags"`

	// DiffHashCurrent is the SHA-256 hex digest of `git diff <parent>..<head>` at
	// the current iteration's worktree state. Required (non-empty; 64-char hex).
	DiffHashCurrent string `json:"diff_hash_current"`

	// DiffHashPrior is the SHA-256 hex digest of the prior iteration's diff.
	// Required (non-empty; 64-char hex).
	DiffHashPrior string `json:"diff_hash_prior"`
}

// Valid reports whether p is a well-formed ReviewFixupStalledPayload.
//
// Rules:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - IterationCount must be ≥ 2.
//   - ReviewerFlags must be non-nil.
//   - DiffHashCurrent must be non-empty.
//   - DiffHashPrior must be non-empty.
func (p ReviewFixupStalledPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.IterationCount < 2 {
		return false
	}
	if p.ReviewerFlags == nil {
		return false
	}
	if p.DiffHashCurrent == "" {
		return false
	}
	if p.DiffHashPrior == "" {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.1a.6 — review_loop_cycle_complete
// ---------------------------------------------------------------------------

// ReviewLoopCycleCompletePayload is the typed event payload for the
// review_loop_cycle_complete event (event-model.md §8.1a.6).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss would orphan the cycle's
// terminal-routing decision; the terminal run_completed / run_failed MUST
// follow this event, never precede it per §8.1a emission-ordering rule).
//
// This is the terminal event for every review-loop cycle regardless of
// completion reason. The daemon MUST emit this event before emitting
// run_completed or run_failed (§8.1).
//
// # Payload fields (event-model.md §8.1a.6)
//
//   - run_id                — the umbrella run_id for this review-loop cycle
//   - workflow_mode         — always "review-loop" for this event
//   - final_iteration_count — the iteration count at termination (1..3 at MVH)
//   - completion_reason     — approved | cap_hit | blocked | no_progress | error
type ReviewLoopCycleCompletePayload struct {
	// RunID is the umbrella run identifier for this review-loop cycle.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// WorkflowMode is always WorkflowModeReviewLoop for this event.
	// Required; must be a valid WorkflowMode constant.
	WorkflowMode WorkflowMode `json:"workflow_mode"`

	// FinalIterationCount is the iteration count at termination (1-based).
	// Required (must be ≥ 1). At MVH this is 1..3 per EM-015e.
	FinalIterationCount int `json:"final_iteration_count"`

	// CompletionReason describes why the review loop terminated.
	// Required; must be a valid ReviewLoopCompletionReason constant.
	CompletionReason ReviewLoopCompletionReason `json:"completion_reason"`
}

// Valid reports whether p is a well-formed ReviewLoopCycleCompletePayload.
//
// Rules per event-model.md §8.1a.6:
//   - RunID must not be uuid.Nil.
//   - WorkflowMode must be a valid WorkflowMode constant.
//   - FinalIterationCount must be ≥ 1.
//   - CompletionReason must be a valid ReviewLoopCompletionReason constant.
func (p ReviewLoopCycleCompletePayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.WorkflowMode.Valid() {
		return false
	}
	if p.FinalIterationCount < 1 {
		return false
	}
	if !p.CompletionReason.Valid() {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.8.6 — bead_label_conflict
// ---------------------------------------------------------------------------

// BeadLabelConflictPayload is the typed event payload for the bead_label_conflict
// event (event-model.md §8.8.6).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — the resolution path falls through to a defined
// tier-2/3/4 result; the conflict is observational evidence rather than a
// routing-gating decision per §8.8.6 Note).
//
// Emitted by the daemon's claim path during workflow-mode resolution per
// execution-model.md §4.3.EM-012a when (a) a bead carries more than one
// workflow:<mode> label or (b) a bead carries a workflow:<mode> label whose
// <mode> value is not in {single, review-loop, dot}. In either case the daemon
// treats tier-1 input as absent and continues the precedence walk.
//
// # Payload fields (event-model.md §8.8.6)
//
//   - bead_id            — opaque bead identifier per beads-integration.md §4.3 BI-008
//   - conflicting_labels — the offending workflow:<mode> labels (length ≥ 1)
//   - fallback_action    — describes the daemon's fallback behavior
//   - detected_at        — RFC 3339 wall-clock timestamp at conflict detection
type BeadLabelConflictPayload struct {
	// BeadID is the opaque bead identifier per beads-integration.md §4.3 BI-008.
	// Required (non-empty).
	BeadID string `json:"bead_id"`

	// ConflictingLabels is the list of offending workflow:<mode> labels observed
	// on the bead. Required (non-nil; length ≥ 1).
	ConflictingLabels []string `json:"conflicting_labels"`

	// FallbackAction describes the daemon's fallback behavior after the conflict
	// is detected (e.g., "tier-1 input treated as absent; precedence walk
	// continues to tier 2"). Required (non-empty).
	FallbackAction string `json:"fallback_action"`

	// DetectedAt is the RFC 3339 wall-clock timestamp at conflict detection.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed BeadLabelConflictPayload.
//
// Rules per event-model.md §8.8.6:
//   - BeadID must be non-empty.
//   - ConflictingLabels must be non-nil and non-empty (length ≥ 1).
//   - FallbackAction must be non-empty.
//   - DetectedAt must be non-empty.
func (p BeadLabelConflictPayload) Valid() bool {
	if p.BeadID == "" {
		return false
	}
	if len(p.ConflictingLabels) == 0 {
		return false
	}
	if p.FallbackAction == "" {
		return false
	}
	if p.DetectedAt == "" {
		return false
	}
	return true
}

// ReviewBypassedPayload is the typed event payload for the review_bypassed event.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — informational audit event; the resolved mode
// is authoritative in the run record, not in this event).
//
// Emitted during workflow-mode resolution (EM-012a §4.3) when a bead carries an
// explicit workflow:single label that resolves at tier-1. The event gates the
// single mode behind an observable audit trail so that review bypass is never
// silent (hk-81n9r).
//
// # Payload fields
//
//   - bead_id    — opaque bead identifier per beads-integration.md §4.3 BI-008
//   - label      — the workflow:<mode> label that triggered the bypass (e.g. "workflow:single")
//   - bypassed_at — RFC 3339 wall-clock timestamp at resolution time
type ReviewBypassedPayload struct {
	// BeadID is the opaque bead identifier per beads-integration.md §4.3 BI-008.
	// Required (non-empty).
	BeadID string `json:"bead_id"`

	// Label is the workflow:<mode> label that resolved at tier-1 to single mode,
	// causing review to be bypassed. Required (non-empty).
	Label string `json:"label"`

	// BypassedAt is the RFC 3339 wall-clock timestamp at resolution time.
	// Required (non-empty).
	BypassedAt string `json:"bypassed_at"`
}

// Valid reports whether p is a well-formed ReviewBypassedPayload.
func (p ReviewBypassedPayload) Valid() bool {
	return p.BeadID != "" && p.Label != "" && p.BypassedAt != ""
}
