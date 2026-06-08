package core

// keeperevents.go — event-bus payload types for §8.13 session-keeper events
// (codename:session-keeper, hk-ekap1):
//   - session_keeper_warn     (§8.13.1) — upward pct-threshold crossing
//   - session_keeper_no_gauge (§8.13.2) — gauge file absent or stale
//
// Spec ref: codename:session-keeper (hk-ekap1).
// Bead ref: hk-8vzek.

// SessionKeeperWarnPayload is the typed event payload for session_keeper_warn
// (event-model.md §8.13.1).
//
// Emitted by the keeper watcher on the first upward crossing of the
// warn_pct threshold (default 80 %).  Not re-emitted until the percentage
// drops below the threshold and rises again (one-shot per crossing).
//
// Durability class: O (ordinary — observability; crossing is recoverable).
type SessionKeeperWarnPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// Pct is the context-window percentage at the moment of the crossing.
	Pct float64 `json:"pct"`

	// WarnPct is the configured warn threshold.
	WarnPct float64 `json:"warn_pct"`

	// SessionID is the Claude Code session_id from the gauge file at the
	// time of the crossing (may be empty if not present in the gauge).
	SessionID string `json:"session_id,omitempty"`
}

// SessionKeeperNoGaugePayload is the typed event payload for
// session_keeper_no_gauge (event-model.md §8.13.2).
//
// Emitted at keeper startup when the gauge file is absent or stale, and
// re-emitted every staleness interval thereafter until a fresh gauge appears.
//
// Durability class: O (ordinary — configuration-gap signal).
type SessionKeeperNoGaugePayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// Reason describes why the gauge is considered unavailable.
	// Values: "absent" | "stale".
	Reason string `json:"reason"`
}

// SessionKeeperHandoffStartedPayload is the payload for
// session_keeper_handoff_started (event-model.md §8.13.3).
//
// Emitted by the cycle core before the /session-handoff injection so the
// cycle is auditable even if it subsequently aborts.
//
// Durability class: O (ordinary — observability).
// Refs: hk-22i70.
type SessionKeeperHandoffStartedPayload struct {
	// AgentName is the keeper agent name.
	AgentName string `json:"agent_name"`
	// CycleID is the monotonic cycle identifier for this run.
	CycleID string `json:"cycle_id"`
	// SessionID is the gauge session_id at the time the cycle fires.
	SessionID string `json:"session_id,omitempty"`
}

// SessionKeeperCycleCompletePayload is the payload for
// session_keeper_cycle_complete (event-model.md §8.13.4).
//
// Emitted on successful completion of the full 7-step cycle.
//
// Durability class: O (ordinary — observability).
// Refs: hk-22i70.
type SessionKeeperCycleCompletePayload struct {
	// AgentName is the keeper agent name.
	AgentName string `json:"agent_name"`
	// CycleID is the cycle identifier.
	CycleID string `json:"cycle_id"`
	// PrevSessionID is the session_id before the /clear.
	PrevSessionID string `json:"prev_session_id,omitempty"`
	// NewSessionID is the session_id observed after the /clear (may be empty
	// if the settle wait elapsed without detecting a new session_id).
	NewSessionID string `json:"new_session_id,omitempty"`
}

// SessionKeeperCycleAbortedPayload is the payload for
// session_keeper_cycle_aborted (event-model.md §8.13.5).
//
// Emitted when the cycle aborts without issuing /clear because the handoff
// nonce confirmation timed out. The session is left untouched.
//
// Durability class: O (ordinary — operator attention required).
// Refs: hk-22i70.
type SessionKeeperCycleAbortedPayload struct {
	// AgentName is the keeper agent name.
	AgentName string `json:"agent_name"`
	// CycleID is the cycle identifier.
	CycleID string `json:"cycle_id"`
	// SessionID is the gauge session_id at the time the cycle was attempted.
	SessionID string `json:"session_id,omitempty"`
	// Reason describes why the cycle aborted. Values: "handoff_timeout".
	Reason string `json:"reason"`
}

// SessionKeeperClearUnconfirmedPayload is the payload for
// session_keeper_clear_unconfirmed (event-model.md §8.13.6).
//
// Emitted (best-effort) when the post-/clear settle wait elapses without
// observing a new session_id in the gauge. The cycle continues regardless.
//
// Durability class: O (ordinary — observability).
// Refs: hk-22i70.
type SessionKeeperClearUnconfirmedPayload struct {
	// AgentName is the keeper agent name.
	AgentName string `json:"agent_name"`
	// CycleID is the cycle identifier.
	CycleID string `json:"cycle_id"`
	// SessionID is the session_id before /clear was issued.
	SessionID string `json:"session_id,omitempty"`
}

// SessionKeeperPrecompactBlockedPayload is the payload for
// session_keeper_precompact_blocked (event-model.md §8.13.8).
//
// Emitted by the keeper watcher when it detects the .precompact trigger marker
// (written by the PreCompact hook) and makes a cycle decision. Always emitted
// once per detected marker, immediately before clearing it.
//
// Durability class: O (ordinary — observability).
// Refs: hk-aalsm.
type SessionKeeperPrecompactBlockedPayload struct {
	// AgentName is the keeper agent name.
	AgentName string `json:"agent_name"`

	// SessionID is the gauge session_id at the time the marker was detected.
	// May be empty if the gauge file was unavailable.
	SessionID string `json:"session_id,omitempty"`

	// Action describes what the keeper did upon detecting the marker.
	// Values:
	//   "cycle_triggered"       — all gates passed; cycle was started.
	//   "hold_dispatch_skip"    — HoldingDispatch was true; cycle skipped.
	//   "anti_loop_suppressed"  — anti-loop gate suppressed re-fire on same session.
	//   "not_managed"           — .managed marker was absent (defensive; shell script
	//                             should have caught this before writing the marker).
	Action string `json:"action"`
}

// SessionKeeperCycleRecoveredPayload is the payload for
// session_keeper_cycle_recovered (event-model.md §8.13.7).
//
// Emitted on keeper boot when the journal shows the keeper crashed in the
// "cleared" phase (after /clear was issued but before /session-resume).
// The recovery path injects /session-resume to complete the interrupted cycle.
//
// Durability class: O (ordinary — observability; recovery is automatic).
// Refs: hk-kct9t.
type SessionKeeperCycleRecoveredPayload struct {
	// AgentName is the keeper agent name.
	AgentName string `json:"agent_name"`
	// CycleID is the cycle identifier from the recovered journal.
	CycleID string `json:"cycle_id"`
	// PhaseAtCrash is the journal phase at the time of the crash.
	PhaseAtCrash string `json:"phase_at_crash"`
}
