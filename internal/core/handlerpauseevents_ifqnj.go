package core

// handlerpauseevents_ifqnj.go — event-bus payload types for §8.11 handler-pause
// lifecycle events introduced by the handler-pause MVH (hk-ifqnj):
//
//   - handler_paused                      (§8.11.1)
//   - handler_resumed                     (§8.11.2)
//   - queue_item_held_for_handler_pause   (§8.11.3)
//
// Spec ref: specs/event-model.md §8.11.
// Spec ref: specs/handler-pause.md.
// Bead ref: hk-ifqnj.

// ---------------------------------------------------------------------------
// Shared sub-types
// ---------------------------------------------------------------------------

// HandlerPauseCause carries the structured reason a handler type was paused.
// Used by both HandlerPausedPayload.Cause and HandlerResumedPayload.PriorCause.
//
// Fields match the cause sub-object in .harmonik/handler-state.json §5.1.
type HandlerPauseCause struct {
	// FailureClass is the coarse failure-class bucket that triggered the pause.
	// Required; must satisfy FailureClass.Valid() per execution-model.md §8.
	FailureClass FailureClass `json:"failure_class"`

	// SubReason is the fine-grained sub-reason within the failure class.
	// Required (non-empty). Known values at MVH: "rate_limit", "budget_exhausted_handler_account".
	// The vocabulary is open per specs/handler-pause.md §5.
	SubReason string `json:"sub_reason"`

	// SourceRunID is the run ID of the run whose outcome tripped the pause.
	// Required (non-empty; UUIDv7 as string).
	SourceRunID string `json:"source_run_id"`

	// SourceBeadID is the bead ID of the bead dispatched in SourceRunID.
	// Required (non-empty).
	SourceBeadID string `json:"source_bead_id"`

	// TrippedAt is the RFC 3339 wall-clock timestamp at which the pause was triggered.
	// Required (non-empty).
	TrippedAt string `json:"tripped_at"`

	// DiagnosticMessage is the human-readable summary from Adapter.Diagnose
	// (specs/handler-contract.md §4.3a HC-014a).  Optional (omitempty); absent
	// when the adapter does not support diagnostics or the probe failed.
	// At MVH the field is informational only; no policy decision is gated on it.
	DiagnosticMessage string `json:"diagnostic_message,omitempty"`
}

// Valid reports whether c is a well-formed HandlerPauseCause.
//
// Rules:
//   - FailureClass must be a valid FailureClass constant.
//   - SubReason must be non-empty.
//   - SourceRunID must be non-empty.
//   - SourceBeadID must be non-empty.
//   - TrippedAt must be non-empty.
func (c HandlerPauseCause) Valid() bool {
	if !c.FailureClass.Valid() {
		return false
	}
	if c.SubReason == "" {
		return false
	}
	if c.SourceRunID == "" {
		return false
	}
	if c.SourceBeadID == "" {
		return false
	}
	if c.TrippedAt == "" {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.11.1 handler_paused
// ---------------------------------------------------------------------------

// HandlerPausedPayload is the typed event payload for the handler_paused event
// (event-model.md §8.11.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss orphans the pause-state landmark;
// reconciliation depends on this event to detect a paused handler across
// daemon restart per specs/handler-pause.md §8.3 HP-008).
//
// Emitted by daemon-core (HandlerPauseController.Pause) when a handler type
// is determined to be handler-fatal per the trigger taxonomy in
// specs/handler-pause.md §5. MUST be fsync-backed before control returns
// from Pause() per §8.11 emission ordering.
//
// # Payload fields (event-model.md §8.11.1)
//
//   - agent_type      — the handler type being paused (e.g. "claude-code")
//   - cause           — structured pause cause (failure_class, sub_reason, source_run_id, source_bead_id, tripped_at)
//   - in_flight_count — number of runs of this handler type that were in-flight at pause time
//   - paused_epoch    — monotonic counter for this pause→resume cycle; starts at 1 and increments each pause
type HandlerPausedPayload struct {
	// AgentType is the handler-type identifier being paused.
	// Required; must satisfy AgentType.Valid() per AR-025 / AR-027.
	AgentType AgentType `json:"agent_type"`

	// Cause is the structured reason the handler was paused.
	// Required; must satisfy HandlerPauseCause.Valid().
	Cause HandlerPauseCause `json:"cause"`

	// InFlightCount is the number of runs of this handler type that were
	// in-flight (dispatched but not yet terminal) at the moment of pause.
	// Required (>= 0). These runs are NOT interrupted; they continue to their
	// natural terminal per specs/handler-pause.md §9 HP-050.
	InFlightCount int `json:"in_flight_count"`

	// PausedEpoch is the monotonic counter identifying this pause→resume cycle.
	// Required (>= 1). Incremented atomically by HandlerPauseController on each
	// pause; used by the dispatcher to dedup queue_item_held_for_handler_pause
	// events per §8.11.3 dedup contract.
	PausedEpoch int `json:"paused_epoch"`
}

// Valid reports whether p is a well-formed HandlerPausedPayload.
//
// Rules per event-model.md §8.11.1:
//   - AgentType must satisfy AgentType.Valid().
//   - Cause must satisfy HandlerPauseCause.Valid().
//   - InFlightCount must be >= 0.
//   - PausedEpoch must be >= 1.
func (p HandlerPausedPayload) Valid() bool {
	if !p.AgentType.Valid() {
		return false
	}
	if !p.Cause.Valid() {
		return false
	}
	if p.InFlightCount < 0 {
		return false
	}
	if p.PausedEpoch < 1 {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.11.2 handler_resumed
// ---------------------------------------------------------------------------

// HandlerResumedBy is the typed discriminator for the by field of a
// handler_resumed event (event-model.md §8.11.2).
//
// At MVH the only value is "operator" (manual resume via `harmonik handler resume`).
// Post-MVH values (e.g. "auto-backoff", "webhook") will be added via EV-027 amendment.
type HandlerResumedBy string

const (
	// HandlerResumedByOperator indicates the resume was triggered by an operator
	// via `harmonik handler resume <agent-type>`.
	HandlerResumedByOperator HandlerResumedBy = "operator"

	// HandlerResumedByAutoBackoff indicates the resume was triggered automatically
	// after a timed backoff derived from the rate-limit retry_after window.
	// Post-MVH: see specs/handler-pause.md §1.2 and bead hk-0otqs.
	HandlerResumedByAutoBackoff HandlerResumedBy = "auto-backoff"

	// HandlerResumedBySignal indicates the resume was triggered by an OS signal
	// (SIGUSR1) sent to the daemon process.
	//
	// Auth model: SIGUSR1 can only be sent by processes with the same UID as the
	// daemon or by the superuser (root).  The kernel enforces this via kill(2)
	// permission checks; no additional application-level authentication is needed.
	//
	// Post-MVH: see specs/handler-pause.md §1.2 (external-trigger resume) and
	// bead hk-bdvae.
	HandlerResumedBySignal HandlerResumedBy = "signal"
)

// Valid reports whether b is one of the declared HandlerResumedBy constants.
func (b HandlerResumedBy) Valid() bool {
	switch b {
	case HandlerResumedByOperator, HandlerResumedByAutoBackoff, HandlerResumedBySignal:
		return true
	default:
		return false
	}
}

// HandlerResumedPayload is the typed event payload for the handler_resumed event
// (event-model.md §8.11.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss would leave the operator's resume
// action unrecorded; the dispatcher depends on this event's visibility to
// resume dispatch for the handler type per specs/handler-pause.md §7.3 HP-040).
//
// Emitted by daemon-core (HandlerPauseController.Resume) when an operator
// clears the pause for a handler type. MUST be fsync-backed before control
// returns from Resume() per §8.11 emission ordering.
//
// # Payload fields (event-model.md §8.11.2)
//
//   - agent_type  — the handler type being resumed
//   - by          — who initiated the resume (enum: operator)
//   - prior_cause — the cause record from the preceding handler_paused event
//   - paused_epoch — the epoch of the pause being cleared (matches the paired handler_paused)
type HandlerResumedPayload struct {
	// AgentType is the handler-type identifier being resumed.
	// Required; must satisfy AgentType.Valid() per AR-025 / AR-027.
	AgentType AgentType `json:"agent_type"`

	// By is the resume initiator. Required; must be a valid HandlerResumedBy constant.
	// At MVH: always HandlerResumedByOperator.
	By HandlerResumedBy `json:"by"`

	// PriorCause is the cause record from the preceding handler_paused event
	// for this epoch. Required; must satisfy HandlerPauseCause.Valid().
	PriorCause HandlerPauseCause `json:"prior_cause"`

	// PausedEpoch is the epoch counter of the pause being cleared.
	// Required (>= 1). MUST match the PausedEpoch on the paired handler_paused event.
	PausedEpoch int `json:"paused_epoch"`
}

// Valid reports whether p is a well-formed HandlerResumedPayload.
//
// Rules per event-model.md §8.11.2:
//   - AgentType must satisfy AgentType.Valid().
//   - By must be a valid HandlerResumedBy constant.
//   - PriorCause must satisfy HandlerPauseCause.Valid().
//   - PausedEpoch must be >= 1.
func (p HandlerResumedPayload) Valid() bool {
	if !p.AgentType.Valid() {
		return false
	}
	if !p.By.Valid() {
		return false
	}
	if !p.PriorCause.Valid() {
		return false
	}
	if p.PausedEpoch < 1 {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// §8.11.3 queue_item_held_for_handler_pause
// ---------------------------------------------------------------------------

// QueueItemHeldForHandlerPausePayload is the typed event payload for the
// queue_item_held_for_handler_pause event (event-model.md §8.11.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — the held state is reconstructible from
// handler-state.json plus queue.json at startup; the dedup contract ensures
// this is low-frequency per §8.11.3 dedup note).
//
// Emitted by daemon-core (dispatcher) when a pending queue item is skipped
// because its resolved agent_type maps to a currently-paused handler.
// MUST be emitted at-most-once per (bead_id, paused_epoch) pair per the
// §8.11.3 dedup contract.
//
// # Payload fields (event-model.md §8.11.3)
//
//   - bead_id      — the bead item that was held
//   - agent_type   — the handler type that is paused, causing the hold
//   - paused_epoch — the epoch of the handler pause during which the hold occurred
type QueueItemHeldForHandlerPausePayload struct {
	// BeadID is the bead item that was held (skipped) by the dispatcher.
	// Required (non-empty).
	BeadID string `json:"bead_id"`

	// AgentType is the handler type that is currently paused, causing the hold.
	// Required; must satisfy AgentType.Valid() per AR-025 / AR-027.
	AgentType AgentType `json:"agent_type"`

	// PausedEpoch is the pause epoch during which this hold occurred.
	// Required (>= 1). Used with BeadID to enforce the at-most-once dedup
	// contract per §8.11.3.
	PausedEpoch int `json:"paused_epoch"`
}

// Valid reports whether p is a well-formed QueueItemHeldForHandlerPausePayload.
//
// Rules per event-model.md §8.11.3:
//   - BeadID must be non-empty.
//   - AgentType must satisfy AgentType.Valid().
//   - PausedEpoch must be >= 1.
func (p QueueItemHeldForHandlerPausePayload) Valid() bool {
	if p.BeadID == "" {
		return false
	}
	if !p.AgentType.Valid() {
		return false
	}
	if p.PausedEpoch < 1 {
		return false
	}
	return true
}
