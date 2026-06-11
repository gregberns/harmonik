package core

import "github.com/google/uuid"

// agentlifecyclepayloads_gjyks.go — event-bus payload types for the 8
// EventType constants that were declared in eventtype.go but lacked
// constructor registrations in eventreg_hqwn59.go (bead hk-gjyks):
//
//   - agent_completed            (§8.3.4)
//   - agent_hard_terminating     (§8.3.13)
//   - agent_heartbeat            (handler-contract.md §4.6 HC-026a)
//   - agent_resumed_after_warning (§8.3.11)
//   - agent_soft_terminating     (§8.3.12)
//   - agent_warning_silent_hang  (§8.3.10)
//   - bead_closed                (execution-model.md §4.12 EM-052)
//   - working_tree_refresh_failed (execution-model.md §4.12 EM-054)
//
// Spec refs: specs/event-model.md §8.3, specs/execution-model.md §4.12,
//            specs/handler-contract.md §4.6 HC-026a.
// Bead ref: hk-gjyks.

// AgentCompletedPayload is the typed event payload for the agent_completed
// event (event-model.md §8.3.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — handler lifecycle observability; the
// orchestrator and audit subsystem use this event to record that an agent
// subprocess exited cleanly per handler-contract.md §4.1).
//
// Emitted by the daemon watcher when the handler subprocess exits with a
// zero exit code and an outcome has been observed. exit_code is observational
// only per §8.3.4; it MUST NOT be used for branching — use outcome_emitted.
//
// # Payload fields (event-model.md §8.3.4)
//
//   - run_id      — the run in whose context the agent completed
//   - session_id  — handler-assigned session identifier
//   - ended_at    — RFC 3339 wall-clock timestamp at completion
//   - exit_code   — optional process exit code (observational only)
//   - outcome_ref — reference to the outcome_emitted event (opaque string)
type AgentCompletedPayload struct {
	// RunID is the run in whose context the agent completed.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// EndedAt is the RFC 3339 wall-clock timestamp at which the agent completed.
	// Required (non-empty).
	EndedAt string `json:"ended_at"`

	// ExitCode is the optional process exit code for the handler subprocess.
	// Observational only per §8.3.4; MUST NOT be used for branching decisions.
	// Nil when the watcher cannot determine the exit code.
	ExitCode *int `json:"exit_code,omitempty"`

	// OutcomeRef is a reference to the outcome_emitted event for this session.
	// Required (non-empty); opaque string identifying the emitted outcome.
	OutcomeRef string `json:"outcome_ref"`
}

// Valid reports whether p is a well-formed AgentCompletedPayload.
//
// Rules per event-model.md §8.3.4:
//   - RunID must not be uuid.Nil.
//   - SessionID must be non-empty.
//   - EndedAt must be non-empty.
//   - OutcomeRef must be non-empty.
func (p AgentCompletedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.EndedAt == "" {
		return false
	}
	if p.OutcomeRef == "" {
		return false
	}
	return true
}

// AgentHeartbeatPayload is the typed event payload for the agent_heartbeat
// event (handler-contract.md §4.6 HC-026a).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — silent-hang detection; the daemon watcher
// uses heartbeat events to reset the silent-hang timer per §7.1).
//
// Emitted by the handler subprocess at least every T/2 seconds for as long
// as the subprocess is alive and has not emitted outcome_emitted. The phase
// field MUST be drawn from the extensible enum defined in HC-026a; additional
// values may be declared by subsystem extensions.
//
// # Payload fields (handler-contract.md §4.6 HC-026a)
//
//   - session_id — handler-assigned session identifier
//   - phase      — current execution phase of the handler subprocess
type AgentHeartbeatPayload struct {
	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// Phase is the current execution phase of the handler subprocess.
	// Required (non-empty). Drawn from the extensible enum declared in
	// handler-contract.md §4.6 HC-026a: {starting, reasoning, tool_call,
	// waiting_input, rotating, shutting_down}. Additional values may be
	// declared by subsystem envelopes; the enum is additive-only.
	Phase string `json:"phase"`
}

// Valid reports whether p is a well-formed AgentHeartbeatPayload.
//
// Rules per handler-contract.md §4.6 HC-026a:
//   - SessionID must be non-empty.
//   - Phase must be non-empty.
func (p AgentHeartbeatPayload) Valid() bool {
	if p.SessionID == "" {
		return false
	}
	if p.Phase == "" {
		return false
	}
	return true
}

// AgentWarningSilentHangPayload is the typed event payload for the
// agent_warning_silent_hang event (event-model.md §8.3.10).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — orchestrator-core and observability; the
// orchestrator uses this event to initiate the soft-then-hard termination
// sequence per handler-contract.md §7.1).
//
// Emitted by the daemon watcher when the handler subprocess has not emitted
// any progress-stream message (including heartbeats) for the configured
// threshold interval T. Precedes agent_soft_terminating per §7.1.
//
// # Payload fields (event-model.md §8.3.10)
//
//   - run_id                — the run in whose context the hang was detected
//   - session_id            — handler-assigned session identifier
//   - threshold_seconds     — the silent-hang threshold T in seconds
//   - last_progress_event_at — RFC 3339 timestamp of the last progress event seen
//   - fsm_state             — watcher FSM state string at detection time
type AgentWarningSilentHangPayload struct {
	// RunID is the run in whose context the silent hang was detected.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// ThresholdSeconds is the configured silent-hang threshold T in seconds.
	// Required (must be > 0).
	ThresholdSeconds int `json:"threshold_seconds"`

	// LastProgressEventAt is the RFC 3339 wall-clock timestamp of the last
	// progress-stream message observed before the threshold elapsed.
	// Required (non-empty).
	LastProgressEventAt string `json:"last_progress_event_at"`

	// FSMState is the watcher FSM state string at the time of detection.
	// Required (non-empty).
	FSMState string `json:"fsm_state"`
}

// Valid reports whether p is a well-formed AgentWarningSilentHangPayload.
//
// Rules per event-model.md §8.3.10:
//   - RunID must not be uuid.Nil.
//   - SessionID must be non-empty.
//   - ThresholdSeconds must be > 0.
//   - LastProgressEventAt must be non-empty.
//   - FSMState must be non-empty.
func (p AgentWarningSilentHangPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ThresholdSeconds <= 0 {
		return false
	}
	if p.LastProgressEventAt == "" {
		return false
	}
	if p.FSMState == "" {
		return false
	}
	return true
}

// AgentResumedAfterWarningPayload is the typed event payload for the
// agent_resumed_after_warning event (event-model.md §8.3.11).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — orchestrator-core and observability; emitted
// when the agent produces output after an agent_warning_silent_hang, cancelling
// the pending soft-termination sequence per handler-contract.md §7.1).
//
// # Payload fields (event-model.md §8.3.11)
//
//   - run_id                  — the run in whose context the agent resumed
//   - session_id              — handler-assigned session identifier
//   - resumed_at              — RFC 3339 wall-clock timestamp at which output resumed
//   - warning_duration_seconds — elapsed seconds between the hang warning and this resumption
type AgentResumedAfterWarningPayload struct {
	// RunID is the run in whose context the agent resumed after warning.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// ResumedAt is the RFC 3339 wall-clock timestamp at which the agent resumed
	// producing output. Required (non-empty).
	ResumedAt string `json:"resumed_at"`

	// WarningDurationSeconds is the elapsed time in seconds between the
	// agent_warning_silent_hang emission and this resumption.
	// Required (must be >= 0).
	WarningDurationSeconds int `json:"warning_duration_seconds"`
}

// Valid reports whether p is a well-formed AgentResumedAfterWarningPayload.
//
// Rules per event-model.md §8.3.11:
//   - RunID must not be uuid.Nil.
//   - SessionID must be non-empty.
//   - ResumedAt must be non-empty.
//   - WarningDurationSeconds must be >= 0.
func (p AgentResumedAfterWarningPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ResumedAt == "" {
		return false
	}
	if p.WarningDurationSeconds < 0 {
		return false
	}
	return true
}

// AgentSoftTerminatingPayload is the typed event payload for the
// agent_soft_terminating event (event-model.md §8.3.12).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — orchestrator-core and audit; records that
// SIGTERM was sent to the handler subprocess after silent-hang threshold T
// was exceeded per handler-contract.md §7.1).
//
// # Payload fields (event-model.md §8.3.12)
//
//   - run_id            — the run in whose context the soft termination began
//   - session_id        — handler-assigned session identifier
//   - threshold_seconds — the silent-hang threshold T in seconds
//   - started_at        — RFC 3339 wall-clock timestamp at which SIGTERM was sent
type AgentSoftTerminatingPayload struct {
	// RunID is the run in whose context the soft termination was initiated.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// ThresholdSeconds is the silent-hang threshold T in seconds that was exceeded.
	// Required (must be > 0).
	ThresholdSeconds int `json:"threshold_seconds"`

	// StartedAt is the RFC 3339 wall-clock timestamp at which SIGTERM was sent.
	// Required (non-empty).
	StartedAt string `json:"started_at"`
}

// Valid reports whether p is a well-formed AgentSoftTerminatingPayload.
//
// Rules per event-model.md §8.3.12:
//   - RunID must not be uuid.Nil.
//   - SessionID must be non-empty.
//   - ThresholdSeconds must be > 0.
//   - StartedAt must be non-empty.
func (p AgentSoftTerminatingPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ThresholdSeconds <= 0 {
		return false
	}
	if p.StartedAt == "" {
		return false
	}
	return true
}

// AgentHardTerminatingPayload is the typed event payload for the
// agent_hard_terminating event (event-model.md §8.3.13).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — orchestrator-core and audit; records that
// SIGKILL was sent to the handler subprocess after the soft-termination grace
// period elapsed without a clean exit per handler-contract.md §7.1).
//
// # Payload fields (event-model.md §8.3.13)
//
//   - run_id            — the run in whose context the hard termination began
//   - session_id        — handler-assigned session identifier
//   - threshold_seconds — the soft-termination grace period T in seconds
//   - started_at        — RFC 3339 wall-clock timestamp at which SIGKILL was sent
type AgentHardTerminatingPayload struct {
	// RunID is the run in whose context the hard termination was initiated.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// SessionID is the handler-assigned session identifier. Required (non-empty).
	// UUIDv7 per handler-contract.md §4.1; opaque to non-handler consumers.
	SessionID SessionID `json:"session_id"`

	// ThresholdSeconds is the soft-termination grace period T in seconds that
	// elapsed before SIGKILL was sent. Required (must be > 0).
	ThresholdSeconds int `json:"threshold_seconds"`

	// StartedAt is the RFC 3339 wall-clock timestamp at which SIGKILL was sent.
	// Required (non-empty).
	StartedAt string `json:"started_at"`
}

// Valid reports whether p is a well-formed AgentHardTerminatingPayload.
//
// Rules per event-model.md §8.3.13:
//   - RunID must not be uuid.Nil.
//   - SessionID must be non-empty.
//   - ThresholdSeconds must be > 0.
//   - StartedAt must be non-empty.
func (p AgentHardTerminatingPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.SessionID == "" {
		return false
	}
	if p.ThresholdSeconds <= 0 {
		return false
	}
	if p.StartedAt == "" {
		return false
	}
	return true
}

// EpicCompletedPayload is the typed event payload for the epic_completed event
// (specs/event-model.md §8.13).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — observational; emitted at most once per epic per daemon session).
//
// Emitted by the daemon after the last child of an epic closes, guarded by
// an in-process at-most-once lock (emittedEpics). Cross-process sibling-race
// (AC-2) and boot-survival (AC-5) are exercised by the T4 scenario bead.
//
// # Payload fields (specs/event-model.md §8.13)
//
//   - epic_id            — the parent epic bead that just completed
//   - last_child_bead_id — the child bead whose closure triggered the check
//   - closed_at          — RFC3339 timestamp of the triggering close
type EpicCompletedPayload struct {
	// EpicID is the identifier of the parent epic bead.
	// Required (non-empty).
	EpicID BeadID `json:"epic_id"`

	// LastChildBeadID is the bead that closed last, triggering emission.
	// Required (non-empty).
	LastChildBeadID BeadID `json:"last_child_bead_id"`

	// ClosedAt is the RFC3339 timestamp of the triggering close.
	// Required (non-empty).
	ClosedAt string `json:"closed_at"`
}

// Valid reports whether p is a well-formed EpicCompletedPayload.
//
// Rules per specs/event-model.md §8.13:
//   - EpicID must be non-empty.
//   - LastChildBeadID must be non-empty.
//   - ClosedAt must be non-empty.
func (p EpicCompletedPayload) Valid() bool {
	return p.EpicID != "" && p.LastChildBeadID != "" && p.ClosedAt != ""
}

// BeadClosedPayload is the typed event payload for the bead_closed event
// (execution-model.md §4.12 EM-052).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — bead closure is a terminal-state
// landmark; loss would silently leave the bead open in the ledger per EM-052).
//
// Emitted by the daemon after CloseBead succeeds on the success branch of the
// merge-to-main step (EM-052 step 6). Emitted before run_completed.
//
// # Payload fields (execution-model.md §4.12 EM-052)
//
//   - run_id  — the run that caused the bead to be closed
//   - bead_id — the bead that was closed
type BeadClosedPayload struct {
	// RunID is the run that caused the bead to be closed.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// BeadID is the identifier of the bead that was closed.
	// Required (non-empty).
	BeadID BeadID `json:"bead_id"`
}

// Valid reports whether p is a well-formed BeadClosedPayload.
//
// Rules per execution-model.md §4.12 EM-052:
//   - RunID must not be uuid.Nil.
//   - BeadID must be non-empty.
func (p BeadClosedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	return true
}

// WorkingTreeRefreshFailedPayload is the typed event payload for the
// working_tree_refresh_failed event (execution-model.md §4.12 EM-054).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — informational; the merge itself succeeded
// and is durable before this event is emitted per EM-054).
//
// Emitted when git reset --hard HEAD fails after a successful merge-to-main.
// The daemon continues to CloseBead normally per EM-054 (refresh failure MUST
// NOT cause ReopenBead or prevent bead closure).
//
// # Payload fields (execution-model.md §4.12 EM-054)
//
//   - run_id  — the run whose merge-to-main triggered the refresh attempt
//   - bead_id — the bead associated with the run
//   - error   — the error message from the failed git reset --hard HEAD
type WorkingTreeRefreshFailedPayload struct {
	// RunID is the run whose merge-to-main triggered the working-tree refresh.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// BeadID is the identifier of the bead associated with the run.
	// Required (non-empty).
	BeadID BeadID `json:"bead_id"`

	// Error is the error message from the failed git reset --hard HEAD command.
	// Required (non-empty).
	Error string `json:"error"`
}

// Valid reports whether p is a well-formed WorkingTreeRefreshFailedPayload.
//
// Rules per execution-model.md §4.12 EM-054:
//   - RunID must not be uuid.Nil.
//   - BeadID must be non-empty.
//   - Error must be non-empty.
func (p WorkingTreeRefreshFailedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.Error == "" {
		return false
	}
	return true
}

// MergeBuildFailedPayload is the typed event payload for the
// merge_build_failed event (hk-o68j3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (terminal-state landmark — the bead is about to be
// reopened; loss would silently leave the bead in a bad state).
//
// Emitted inside lockedMergeRunBranchToMain when go build or go vet fails
// on the freshly fast-forwarded merged tree. The update-ref is rolled back
// and the push is skipped before this event fires.
//
// # Payload fields
//
//   - run_id  — the run whose merged tree failed the build gate
//   - bead_id — the bead associated with the run
//   - error   — combined output from the failed go build/vet invocation
type MergeBuildFailedPayload struct {
	// RunID is the run whose merged tree failed the build gate.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// BeadID is the identifier of the bead associated with the run.
	// Required (non-empty).
	BeadID BeadID `json:"bead_id"`

	// Error is the combined output from the failed go build or go vet command.
	// Required (non-empty).
	Error string `json:"error"`
}

// Valid reports whether p is a well-formed MergeBuildFailedPayload.
func (p MergeBuildFailedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.Error == "" {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// beads-integration.md §4.5a BI-013c — bead_claim_skipped
// ---------------------------------------------------------------------------

// BeadClaimSkippedPayload is the typed event payload for the bead_claim_skipped
// event (beads-integration.md §4.5a BI-013c).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — the queue item is transitioned to
// deferred-for-ledger-dep; the skip is observational evidence, not a
// routing-gating decision).
//
// Emitted by the daemon's pre-claim status re-read guard (BI-013c) when the
// bead's status is not open between the dispatcher's selection of a queue item
// and the claim write to Beads. The queue item is returned to the group with
// status deferred-for-ledger-dep per queue-model.md §6 QM-022.
//
// # Payload fields (beads-integration.md §4.5a BI-013c)
//
//   - bead_id         — the bead whose status was observed as non-open
//   - observed_status — the CoarseStatus value returned by br show at re-read time
//   - reason          — always "status_changed_between_select_and_claim"
//   - detected_at     — RFC 3339 wall-clock timestamp at detection
type BeadClaimSkippedPayload struct {
	// BeadID is the opaque bead identifier per beads-integration.md §4.3 BI-008.
	// Required (non-empty).
	BeadID string `json:"bead_id"`

	// ObservedStatus is the CoarseStatus value returned by br show at re-read
	// time. Required (non-empty).
	ObservedStatus string `json:"observed_status"`

	// Reason is the reason the claim was skipped. Required (non-empty).
	// The value is always "status_changed_between_select_and_claim" per BI-013c.
	Reason string `json:"reason"`

	// DetectedAt is the RFC 3339 wall-clock timestamp at detection.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed BeadClaimSkippedPayload.
//
// Rules per beads-integration.md §4.5a BI-013c:
//   - BeadID must be non-empty.
//   - ObservedStatus must be non-empty.
//   - Reason must be non-empty.
//   - DetectedAt must be non-empty.
func (p BeadClaimSkippedPayload) Valid() bool {
	if p.BeadID == "" {
		return false
	}
	if p.ObservedStatus == "" {
		return false
	}
	if p.Reason == "" {
		return false
	}
	if p.DetectedAt == "" {
		return false
	}
	return true
}
