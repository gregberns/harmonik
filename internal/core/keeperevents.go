package core

// keeperevents.go — event-bus payload types for §8.16 session-keeper events
// (codename:session-keeper, hk-ekap1):
//   - session_keeper_warn     (§8.16.1) — upward pct-threshold crossing
//   - session_keeper_no_gauge (§8.16.2) — gauge file absent or stale
//
// Spec ref: codename:session-keeper (hk-ekap1).
// Bead ref: hk-8vzek.

// SessionKeeperWarnPayload is the typed event payload for session_keeper_warn
// (event-model.md §8.16.1).
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
// session_keeper_no_gauge (event-model.md §8.16.2).
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
// session_keeper_handoff_started (event-model.md §8.16.3).
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
// session_keeper_cycle_complete (event-model.md §8.16.4).
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
// session_keeper_cycle_aborted (event-model.md §8.16.5).
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
// session_keeper_clear_unconfirmed (event-model.md §8.16.6).
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
// session_keeper_precompact_blocked (event-model.md §8.16.8).
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
	//   "operator_attached"     — a human operator is attached to the tmux session;
	//                             injection suppressed (warn-only). Refs: hk-6qf.
	//   "not_managed"           — .managed marker was absent (defensive; shell script
	//                             should have caught this before writing the marker).
	Action string `json:"action"`
}

// SessionKeeperCycleRecoveredPayload is the payload for
// session_keeper_cycle_recovered (event-model.md §8.16.7).
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

// SessionKeeperRespawnAttemptedPayload is the payload for
// session_keeper_respawn_attempted (event-model.md §8.16.9).
//
// Emitted by the keeper watcher when it detects that the managed pane has
// gone idle (agent exited after a /quit injection) and fires --respawn-cmd
// to re-launch the agent.
//
// Durability class: O (ordinary — observability).
// Refs: hk-3w2.
type SessionKeeperRespawnAttemptedPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// Outcome is "ok" when the respawn command exited 0, or "error" otherwise.
	Outcome string `json:"outcome"`

	// Error is the error message when Outcome is "error". Omitted on success.
	Error string `json:"error,omitempty"`
}

// SessionKeeperRestartNowBlockedPayload is the payload for
// session_keeper_restart_now_blocked (ON-059, hk-wjzf).
//
// Emitted by RunOnDemand whenever the on-demand restart-now cycle is suppressed
// by a gate or freshness check. The marker is always consumed-once first; this
// event is the only observability signal for the suppression.
//
// Durability class: O (ordinary — observability; non-destructive).
// Refs: hk-wjzf, hk-xjlq.
type SessionKeeperRestartNowBlockedPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// SessionID is the gauge session_id at the time the marker was consumed.
	// May be empty if the gauge file was unavailable.
	SessionID string `json:"session_id,omitempty"`

	// Reason describes why the cycle was suppressed. Values: "not_managed",
	// "empty_session_id", "hold_dispatch", "not_crisp_idle",
	// "anti_loop_suppressed", "operator_attached", "session_id_mismatch",
	// "nonce_mismatch", "handoff_stale", "handoff_modified_during_settle",
	// "handoff_read_error", "handoff_stat_error", "marker_read_error".
	Reason string `json:"reason"`
}

// SessionKeeperRestartNowPayload is the payload for session_keeper_restart_now
// (SK-030, hk-keeper-delivery-restartnow-nonce-kz4w6).
//
// Emitted by RestartNow on a SUCCESSFUL agent-run restart (ack + /clear + brief
// all injected). The Nonce carries the value supplied to `restart-now --nonce`
// (carry-for-audit — NEVER validated), so a query of events.jsonl by that nonce
// joins the self-restart to its originating keeper cycle.
//
// Durability class: O (ordinary — observability; non-destructive).
type SessionKeeperRestartNowPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// SessionID is the verified gauge session_id the restart was driven against.
	SessionID string `json:"session_id,omitempty"`

	// Nonce is the verifiability/provenance token supplied to restart-now
	// (--nonce, or the derived rn-<ms> default when the flag is omitted). Carried
	// for audit; never validated.
	Nonce string `json:"nonce"`
}

// SessionKeeperLivePaneRecoverPayload is the payload for
// session_keeper_live_pane_recover (hk-75mr).
//
// Emitted by the keeper watcher when its gauge-INDEPENDENT last-resort recovery
// fires a gated ForceRestart: the gauge has been stale past LiveRecoverGrace,
// the tmux pane is still alive (the agent is hung mid-turn, not exited — so the
// respawn-on-idle path does NOT apply and a /clear inject cannot reach it), no
// human operator is actively attached, the agent is not blocked on an open
// decision, the cooldown has elapsed, and the bound .sid identity is a valid
// UUIDv4. This is the complement of session_keeper_respawn_attempted (which
// fires when the pane has gone IDLE).
//
// Durability class: O (ordinary — observability of a destructive last-resort).
// Refs: hk-75mr; codename:keeper-redesign.
type SessionKeeperLivePaneRecoverPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// SessionID is the bound identity (from the .sid channel) that the recovery
	// was gated on — always a valid UUIDv4 (recovery fails closed otherwise).
	SessionID string `json:"session_id,omitempty"`

	// StaleSeconds is the gauge staleness (seconds) at the moment recovery fired.
	StaleSeconds int64 `json:"stale_seconds"`

	// Outcome is "ok" when the ForceRestart action returned nil, or "error".
	Outcome string `json:"outcome"`

	// Error is the error message when Outcome is "error". Omitted on success.
	Error string `json:"error,omitempty"`
}

// SessionKeeperOperatorAttachedPayload is the payload for
// session_keeper_operator_attached (codename:session-keeper).
//
// Emitted by the keeper cycle core when it suppresses a reset-cycle injection
// because a human operator is attached to the target tmux session. The keeper
// falls back to warn-only (the watcher keeps emitting the warning/gauge) and
// resumes the normal inject cycle once the operator detaches.
//
// Durability class: O (ordinary — observability).
// Refs: hk-6qf.
type SessionKeeperOperatorAttachedPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// SessionID is the gauge session_id at the time the cycle was suppressed.
	SessionID string `json:"session_id,omitempty"`

	// Phase identifies which act-path suppressed the injection. Values:
	// "cycle" (Cycler.MaybeRun) | "precompact" (Cycler.RunForPrecompact).
	Phase string `json:"phase"`
}

// SessionKeeperBlindPayload is the payload for session_keeper_blind (hk-34ac).
//
// Emitted when the keeper watcher has continuously rejected its gauge as
// foreign_session for more than 5 minutes. The keeper is bound to the wrong
// session and cannot monitor the live agent's context usage. Latched once per
// blind episode; cleared on the next successful (non-foreign, non-stale) tick.
//
// Durability class: O (ordinary — safety alarm; operator attention required).
// Refs: hk-34ac.
type SessionKeeperBlindPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// ManagedSID is the session_id the keeper is bound to (.managed).
	ManagedSID string `json:"managed_sid,omitempty"`

	// LiveSID is the session_id the live gauge is reporting.
	LiveSID string `json:"live_sid,omitempty"`

	// BlindSeconds is how long the keeper has been blind (seconds).
	BlindSeconds int64 `json:"blind_seconds"`
}

// SessionKeeperHardCeilingPayload is the payload for session_keeper_hard_ceiling
// (hk-34ac).
//
// Emitted by the keeper watcher when a pane's token count exceeds
// HardCeilingAbsTokens (280 000) regardless of SID binding. This is a
// SID-independent failsafe: the action fires even when the keeper is mis-bound,
// keyed only on the observed gauge token count.
//
// Durability class: O (ordinary — safety action; observability).
// Refs: hk-34ac.
type SessionKeeperHardCeilingPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// ContextLen is the token count observed in the gauge at the time of the
	// action. JSON key is "tokens" for wire compatibility with existing events.
	// Field is NOT named "Tokens" to satisfy EV-036 secret-prefix rule (hk-6x7dw).
	ContextLen int64 `json:"tokens"`

	// HardCeiling is the configured HardCeilingAbsTokens threshold.
	HardCeiling int64 `json:"hard_ceiling"`
}

// SessionKeeperIdleCrewPayload is the payload for session_keeper_idle_crew
// (hk-ee81).
//
// Emitted when a crew session is idle (CrispIdle + not HoldingDispatch) with
// a token count BELOW the idle-restart floor (IdleRestartAbsTokens, default
// 150 000). The keeper does not restart on this path — the captain may choose
// to reap the crew instead. Restart is triggered only when tokens are at or
// above the idle-restart floor (see RunForIdle in cycle.go).
//
// Durability class: O (ordinary — advisory signal to captain).
// Refs: hk-ee81.
type SessionKeeperIdleCrewPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent"`

	// ContextLen is the token count observed in the gauge at the time of
	// emission. JSON key is "tokens" for wire consistency with related events.
	// Field is NOT named "Tokens" to satisfy EV-036 secret-prefix rule (hk-6x7dw).
	ContextLen int64 `json:"tokens"`

	// Reason is always "below_idle_threshold" on this path.
	Reason string `json:"reason"`
}

// SessionKeeperAckTimeoutPayload is the payload for session_keeper_ack_timeout
// (hk-uldg — the agent-side half of the restart-now/ping ACK handshake).
//
// Emitted by `harmonik keeper await-ack` when the timeout elapses without
// observing the exact `[KEEPER ACK <nonce>]` line in the watched agent's tmux
// pane. This makes the "keeper did not deliver the ACK" failure DURABLE — an
// orchestrator's `harmonik subscribe` or a postmortem can find it. The binary
// then exits non-zero (3); per design §3 the comms escalation is the CALLER's
// responsibility (the binary stays identity-free).
//
// Durability class: O (ordinary — escalation signal; operator attention).
// Refs: hk-uldg; codename:keeper-redesign.
type SessionKeeperAckTimeoutPayload struct {
	// AgentName is the watched agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// Nonce is the exact verifiability token await-ack was waiting for.
	Nonce string `json:"nonce"`

	// Kind is the handshake kind being confirmed: "restart" or "ping".
	Kind string `json:"kind"`

	// TimeoutSeconds is the configured timeout (seconds) that elapsed.
	TimeoutSeconds float64 `json:"timeout_seconds"`

	// TmuxTarget is the resolved pane address await-ack polled. May be empty
	// if no pane could be resolved.
	TmuxTarget string `json:"tmux_target,omitempty"`

	// Reason describes why the ACK was not confirmed. Values:
	//   "ack_not_observed"  — polled to timeout, the exact ACK line never appeared.
	//   "no_tmux_target"    — no pane could be resolved for the agent.
	Reason string `json:"reason"`
}

// SessionKeeperWatcherDeadPayload is the payload for session_keeper_watcher_dead
// (hk-qgfme).
//
// Emitted by the daemon when an async post-spawn liveness probe finds the crew
// keeper watcher NOT holding its exclusive flock lock after the configured
// keeper.timings.flock_acquire_grace window. The crew agent is still live; this
// event signals the captain/operator that the crew is monitor-less.
//
// Durability class: O (ordinary — operator attention).
// Refs: hk-qgfme.
type SessionKeeperWatcherDeadPayload struct {
	// AgentName is the crew name (--agent flag value).
	AgentName string `json:"agent_name"`

	// GracePeriodSeconds is the configured flock_acquire_grace in seconds.
	GracePeriodSeconds float64 `json:"grace_period_seconds"`

	// Reason is a human-readable description of why the check failed.
	Reason string `json:"reason"`
}

// SessionKeeperWatcherRevivedPayload is the payload for
// session_keeper_watcher_revived (hk-220lv).
//
// Emitted by the daemon's periodic keeper-revive watcher immediately after it
// re-arms a crew's keeper window. The preceding session_keeper_watcher_dead for
// the same agent states the diagnosis (flock unheld past the grace window); this
// event states the remediation that was attempted, and how many attempts remain
// before the watcher stops trying and escalates to the operator.
//
// Durability class: O (ordinary — operator attention; self-heal audit trail).
// Refs: hk-220lv.
type SessionKeeperWatcherRevivedPayload struct {
	// AgentName is the crew name whose keeper window was re-armed.
	AgentName string `json:"agent_name"`

	// Session is the tmux session the keeper window was re-armed into, derived
	// from the crew registry Handle ("<session>:agent" → "<session>").
	Session string `json:"session,omitempty"`

	// DeadForSeconds is how long the watcher flock had been continuously unheld
	// when the re-arm fired.
	DeadForSeconds float64 `json:"dead_for_seconds"`

	// Attempt is the 1-based revive attempt number for this dead episode. The
	// counter resets to zero as soon as a live watcher flock is observed again.
	Attempt int `json:"attempt"`

	// MaxAttempts is the configured per-agent revive cap (keeper.budgets.revive_max_attempts).
	MaxAttempts int `json:"max_attempts"`
}

// SessionKeeperConfigRejectedPayload is the typed payload for
// session_keeper_config_rejected (hk-4pnv).
//
// Emitted by the standalone `harmonik keeper` start path when the threshold
// precedence resolver (cmd/harmonik.ResolveKeeperConfig) rejects the keeper
// threshold config or CLI flags — a bad value (pct out of range), an invalid
// enum, a malformed duration, or a cross-field band inversion
// (warn<act<force_act<hard_ceiling, warn_pct<act_pct). The keeper then REFUSES to
// start rather than running with silently-defaulted thresholds, so the operator
// learns of the misconfiguration. Durability class: O (ordinary — operator
// attention; the refusal is also logged to stderr).
type SessionKeeperConfigRejectedPayload struct {
	// AgentName is the keeper agent name (--agent flag value).
	AgentName string `json:"agent_name"`

	// Field names the offending knob (e.g. "act_pct_ceil", "--warn-pct",
	// "warn<act<force_act<hard_ceiling").
	Field string `json:"field"`

	// Reason is the human-readable rejection explanation.
	Reason string `json:"reason"`
}

// ---------------------------------------------------------------------------
// §8.20 Session-keeper interior cycle events (codename:session-restart-substrate)
// ---------------------------------------------------------------------------
//
// Fine-grained restart-cycle milestones, durable on the bus and joinable by the
// composite (agent_name, cycle_id) key (EV-046). Emitted by internal/keeper;
// consumed by the internal/replay invariant harness (EV-048). All class O.
//
// Each payload carries AgentName + a REQUIRED CycleID (json "cycle_id", no
// omitempty) with a Valid() method asserting non-empty cycle scope, following
// the ReconciliationStartedPayload.Valid() precedent. Valid() is NOT wired into
// DecodePayload (EventPayload is an empty marker interface); it is exercised by
// the roundtrip/prop tests and the replay harness explicitly.
//
// Spec ref: event-model.md §8.20; EV-046..EV-050.
// Bead ref: codename:session-restart-substrate.

// SessionKeeperHandoffWrittenPayload is the payload for
// session_keeper_handoff_written (event-model.md §8.20.1).
//
// Emitted when the handoff nonce is confirmed written by the model
// (cycle.go confirmed-phase). Precondition of clear_sent (SR3).
//
// Durability class: O (ordinary — observability).
// Refs: codename:session-restart-substrate.
type SessionKeeperHandoffWrittenPayload struct {
	AgentName    string `json:"agent_name"`
	CycleID      string `json:"cycle_id"` // REQUIRED
	SessionID    string `json:"session_id,omitempty"`
	Nonce        string `json:"nonce,omitempty"`         // confirmed nonce marker (audit)
	Recovered    bool   `json:"recovered,omitempty"`     // true iff accepted via freshness recovery (not nonce)
	HandoffMtime string `json:"handoff_mtime,omitempty"` // RFC3339; carried on the recovery edge
}

// SessionKeeperModelDonePayload is the payload for session_keeper_model_done
// (event-model.md §8.20.2).
//
// Emitted when the model reaches an await-input boundary after the handoff turn
// (D12); Degraded=true if the liveness bound fired. Precondition of clear_sent
// (SR4).
//
// Durability class: O (ordinary — observability).
// Refs: codename:session-restart-substrate.
type SessionKeeperModelDonePayload struct {
	AgentName string `json:"agent_name"`
	CycleID   string `json:"cycle_id"` // REQUIRED
	SessionID string `json:"session_id,omitempty"`
	Source    string `json:"source"`             // REQUIRED: "idle_marker" | "transcript_turn" | "timeout"
	Degraded  bool   `json:"degraded,omitempty"` // true iff reached via model_done_timeout
}

// SessionKeeperClearSentPayload is the payload for session_keeper_clear_sent
// (event-model.md §8.20.3).
//
// Emitted when /clear is injected into the pane (cycle.go cleared-phase).
//
// Durability class: O (ordinary — observability).
// Refs: codename:session-restart-substrate.
type SessionKeeperClearSentPayload struct {
	AgentName string `json:"agent_name"`
	CycleID   string `json:"cycle_id"`             // REQUIRED
	SessionID string `json:"session_id,omitempty"` // session_id before /clear
	Attempt   int    `json:"attempt"`              // 1-based; increments on defensive re-injects
}

// SessionKeeperNewSessionUpPayload is the payload for
// session_keeper_new_session_up (event-model.md §8.20.4).
//
// Emitted when a new session_id is observed after /clear; precondition of
// briefing (SR6).
//
// Durability class: O (ordinary — observability).
// Refs: codename:session-restart-substrate.
type SessionKeeperNewSessionUpPayload struct {
	AgentName     string `json:"agent_name"`
	CycleID       string `json:"cycle_id"`        // REQUIRED
	PrevSessionID string `json:"prev_session_id"` // REQUIRED (needed for the != check)
	NewSessionID  string `json:"new_session_id"`  // REQUIRED; Valid(): non-empty AND != PrevSessionID
}

// Valid reports whether p is a well-formed SessionKeeperHandoffWrittenPayload:
// non-empty agent_name and cycle_id.
func (p SessionKeeperHandoffWrittenPayload) Valid() bool {
	return validCycleScope(p.AgentName, p.CycleID)
}

// Valid reports whether p is a well-formed SessionKeeperModelDonePayload:
// non-empty agent_name and cycle_id.
func (p SessionKeeperModelDonePayload) Valid() bool {
	return validCycleScope(p.AgentName, p.CycleID)
}

// Valid reports whether p is a well-formed SessionKeeperClearSentPayload:
// non-empty agent_name and cycle_id, and Attempt >= 1 (1-based).
func (p SessionKeeperClearSentPayload) Valid() bool {
	return validCycleScope(p.AgentName, p.CycleID) && p.Attempt >= 1
}

// Valid reports whether p is a well-formed SessionKeeperNewSessionUpPayload:
// non-empty agent_name and cycle_id, plus a non-empty NewSessionID that differs
// from PrevSessionID.
func (p SessionKeeperNewSessionUpPayload) Valid() bool {
	return validCycleScope(p.AgentName, p.CycleID) &&
		p.NewSessionID != "" && p.NewSessionID != p.PrevSessionID
}

// validCycleScope is the shared §8.20 keeper-interior precondition: a well-formed
// cycle-scoped payload carries a non-empty agent_name and cycle_id. The ^cyc-
// prefix is a SOFT check kept OUT of Valid() (a future CycleIDGen change must not
// retro-invalidate historical corpora); the replay harness reports a
// non-conforming id as a low-severity finding rather than dropping the event.
func validCycleScope(agent, cycleID string) bool {
	return agent != "" && cycleID != ""
}
