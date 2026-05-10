package handlercontract

import "time"

// sessionLogLoc — per-bead helper prefix for test helpers in
// sessionlogloc_hc010_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.11).

// SessionLogLocationMsg is the on-wire NDJSON message the handler subprocess
// MUST emit early in the session — after handler_capabilities and before
// skills_provisioned / agent_ready — to announce the session-log path.
//
// The daemon-side watcher translates this message into the session_log_location
// bus event per workspace-model.md §4.7.  The session-log directory and sidecar
// already exist when this message is emitted; this message announces the path,
// it does NOT create it.
//
// # Wire fields
//
// Normative field list per specs/handler-contract.md §4.2.HC-010:
//
//   - type        — always "session_log_location"; not stored (used for dispatch).
//   - session_id  — the session this log belongs to.
//   - run_id      — the run this session belongs to.
//   - node_id     — the workflow node that started this session.
//   - agent_type  — the handler's registered agent type.
//   - log_path    — absolute path to the session-log file on the daemon's
//     filesystem.
//   - log_format  — the format of the log file (e.g. "jsonl", "text").
//   - bead_id     — optional; the bead assigned to this session, when present.
//
// Full payload schema is owned by event-model.md §6.3; this struct carries the
// required fields declared in handler-contract.md §4.2.HC-010.
//
// JSON tags match the on-wire field names.
type SessionLogLocationMsg struct {
	// Type is always ProgressMsgTypeSessionLogLocation; retained for round-trip
	// fidelity.  The watcher dispatches on this field before decoding.
	Type string `json:"type"`

	// SessionID identifies the session this log belongs to
	// (specs/handler-contract.md §4.2.HC-010).
	SessionID string `json:"session_id"`

	// RunID identifies the run this session belongs to
	// (specs/handler-contract.md §4.2.HC-010).
	RunID string `json:"run_id"`

	// NodeID identifies the workflow node that started this session
	// (specs/handler-contract.md §4.2.HC-010).
	NodeID string `json:"node_id"`

	// AgentType is the handler's registered agent type
	// (specs/handler-contract.md §4.2.HC-010).
	AgentType string `json:"agent_type"`

	// LogPath is the absolute path to the session-log file on the daemon's
	// filesystem (specs/handler-contract.md §4.2.HC-010).
	//
	// The directory and sidecar already exist per workspace-model.md §4.7;
	// this field announces the path, it does not provision it.
	LogPath string `json:"log_path"`

	// LogFormat is the format of the log file
	// (specs/handler-contract.md §4.2.HC-010).
	//
	// Example values: "jsonl", "text".  The format vocabulary is
	// extensible; the watcher passes the value through to the bus event
	// without validation.
	LogFormat string `json:"log_format"`

	// BeadID is the optional bead assigned to this session.  Nil when
	// absent on the wire (specs/handler-contract.md §4.2.HC-010 "bead_id?").
	BeadID *string `json:"bead_id,omitempty"`
}

// SessionLogLocationTimeout is the maximum duration the watcher waits for the
// session_log_location message after successfully completing version negotiation
// (handler_capabilities + version_selected exchange).
//
// If no session_log_location message is received within this window, the watcher
// MUST abort the session with ErrStructural per §7.2 pseudocode.
//
// Normative value: 10 seconds (specs/handler-contract.md §7.2 pseudocode).
const SessionLogLocationTimeout = 10 * time.Second
