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
