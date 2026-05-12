package core

import "github.com/google/uuid"

// RunCompletedPayload is the event-bus payload for the run_completed event
// (event-model.md §8.1.2; emission rules at execution-model.md §4.3.EM-015b).
//
// run_completed MUST be emitted when the run enters a node in terminal_node_ids
// with outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS} per EM-015b. Exactly one of
// {run_completed, run_failed} is emitted per run terminal transition; the two
// events are mutually exclusive at emission time.
//
// The terminal-transition bead write per beads-integration.md §4.4 BI-010 MUST
// follow terminal event emission; it MUST NOT precede it.
//
// # Payload fields (event-model.md §8.1.2)
//
//   - run_id            — the run that completed (required)
//   - terminal_state_id — StateID of the terminal node reached (required)
//   - ended_at          — RFC 3339 wall-clock timestamp (required)
//   - summary           — optional human-readable completion note
//   - workflow_mode     — optional resolved dispatch shape (backward-compat per §8.1)
type RunCompletedPayload struct {
	// RunID identifies the run that completed. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// TerminalStateID is the StateID of the terminal node reached by this run.
	// Required (must not be uuid.Nil).
	TerminalStateID StateID `json:"terminal_state_id"`

	// EndedAt is the RFC 3339 wall-clock timestamp at which the run completed.
	// Caller MUST format as RFC 3339 per event-model.md §4.3.
	// Required (non-empty). Kept as string to avoid silent timezone normalization
	// and JSON round-trip drift; mirrors the ProducedAt rationale in HookVerdictRecord.
	EndedAt string `json:"ended_at"`

	// Summary is an optional human-readable completion note.
	// Nil when omitted; non-nil must be non-empty (Valid() enforces this).
	Summary *string `json:"summary,omitempty"`

	// WorkflowMode surfaces the resolved dispatch shape for this run
	// (event-model.md §8.1 workflow_mode payload-field rule;
	// execution-model.md §4.3.EM-012a). Optional for backward compatibility
	// with v0.3.x consumers. When non-nil must be a valid WorkflowMode constant.
	WorkflowMode *WorkflowMode `json:"workflow_mode,omitempty"`
}

// Valid reports whether p is a well-formed RunCompletedPayload.
//
// Rules per event-model.md §8.1.2:
//   - RunID must not be uuid.Nil.
//   - TerminalStateID must not be uuid.Nil.
//   - EndedAt must be non-empty.
//   - Summary, when non-nil, must be non-empty.
//   - WorkflowMode, when non-nil, must be a declared WorkflowMode constant.
func (p RunCompletedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.TerminalStateID) == uuid.Nil {
		return false
	}
	if p.EndedAt == "" {
		return false
	}
	if p.Summary != nil && *p.Summary == "" {
		return false
	}
	if p.WorkflowMode != nil && !p.WorkflowMode.Valid() {
		return false
	}
	return true
}

// RunFailedPayload is the event-bus payload for the run_failed event
// (event-model.md §8.1.3; emission rules at execution-model.md §4.3.EM-015b).
//
// run_failed MUST be emitted when the classifier (execution-model.md §8)
// produces a terminal verdict, when the cascade returns FAIL (per
// §4.10.EM-046a or §4.10.EM-043 compilation_loop), or when an operator cancel
// is observed per §7.1. Exactly one of {run_completed, run_failed} is emitted
// per run terminal transition; the two events are mutually exclusive at
// emission time.
//
// The payload MUST carry the failure class and the last successful checkpoint's
// commit SHA (last_checkpoint) per execution-model.md §4.5.EM-025. The
// last_checkpoint SHA is the git trail anchor for reconciliation detectors.
//
// The terminal-transition bead write per beads-integration.md §4.4 BI-010 MUST
// follow terminal event emission; it MUST NOT precede it.
//
// # Payload fields (event-model.md §8.1.3 + execution-model.md §4.5.EM-025)
//
//   - run_id              — the run that failed (required)
//   - terminal_state_id   — StateID of the node at failure; nil when failure
//     occurs before any node is entered (e.g., budget_exhausted at dispatch)
//   - failure_class       — coarse failure bucket per execution-model.md §8 (required)
//   - error_category      — narrow sentinel from handler-contract.md §4.5; absent
//     for orchestrator-originated failures (e.g., compilation_loop)
//   - ended_at            — RFC 3339 wall-clock timestamp (required)
//   - reason              — human-readable failure description (required)
//   - last_checkpoint     — SHA of the last successful checkpoint commit per
//     EM-025; empty string when no checkpoint exists (first-node failure)
type RunFailedPayload struct {
	// RunID identifies the run that failed. Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// TerminalStateID is the StateID of the node at which the failure occurred.
	// Nil when the failure occurs before any node is entered (e.g.,
	// budget_exhausted at dispatch time). Corresponds to terminal_state_id? in
	// event-model.md §8.1.3.
	TerminalStateID *StateID `json:"terminal_state_id,omitempty"`

	// FailureClass is the coarse failure-class bucket per execution-model.md §8.
	// Required (must be a valid FailureClass constant).
	FailureClass FailureClass `json:"failure_class"`

	// ErrorCategory is the narrow handler-origin sentinel per
	// handler-contract.md §4.5 (event-model.md §3 / §6.3). Absent (nil) for
	// orchestrator-originated failures (e.g., compilation_loop,
	// no_outgoing_edge_matches) that have no handler-origin sentinel.
	ErrorCategory *ErrorCategory `json:"error_category,omitempty"`

	// EndedAt is the RFC 3339 wall-clock timestamp at which the run failed.
	// Caller MUST format as RFC 3339 per event-model.md §4.3.
	// Required (non-empty). Kept as string; mirrors RunCompletedPayload.EndedAt rationale.
	EndedAt string `json:"ended_at"`

	// Reason is a human-readable description of the failure. Required (non-empty).
	Reason string `json:"reason"`

	// LastCheckpoint is the SHA of the last successful checkpoint commit on the
	// run's task branch per execution-model.md §4.5.EM-025. It provides an
	// anchor to the git trail for reconciliation detectors. Empty string when
	// no checkpoint exists (failure before the first durable transition, e.g.,
	// budget_exhausted at dispatch).
	LastCheckpoint string `json:"last_checkpoint"`

	// WorkflowMode surfaces the resolved dispatch shape for this run
	// (event-model.md §8.1 workflow_mode payload-field rule;
	// execution-model.md §4.3.EM-012a). Optional for backward compatibility
	// with v0.3.x consumers. When non-nil must be a valid WorkflowMode constant.
	WorkflowMode *WorkflowMode `json:"workflow_mode,omitempty"`
}

// Valid reports whether p is a well-formed RunFailedPayload.
//
// Rules per event-model.md §8.1.3 and execution-model.md §4.5.EM-025:
//   - RunID must not be uuid.Nil.
//   - FailureClass must be a valid FailureClass constant.
//   - EndedAt must be non-empty.
//   - Reason must be non-empty.
//   - TerminalStateID, when non-nil, must not be uuid.Nil.
//   - ErrorCategory, when non-nil, must be a declared ErrorCategory constant.
//   - LastCheckpoint is permitted to be empty (no prior checkpoint).
//   - WorkflowMode, when non-nil, must be a declared WorkflowMode constant.
func (p RunFailedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.FailureClass.Valid() {
		return false
	}
	if p.EndedAt == "" {
		return false
	}
	if p.Reason == "" {
		return false
	}
	if p.TerminalStateID != nil && uuid.UUID(*p.TerminalStateID) == uuid.Nil {
		return false
	}
	if p.ErrorCategory != nil && !p.ErrorCategory.Valid() {
		return false
	}
	if p.WorkflowMode != nil && !p.WorkflowMode.Valid() {
		return false
	}
	return true
}
