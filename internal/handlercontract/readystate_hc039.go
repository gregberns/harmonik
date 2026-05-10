package handlercontract

// readyState — per-bead helper prefix for test helpers in
// readystate_hc039_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.46).

// AgentReadyMsg is the on-wire NDJSON message the handler subprocess MUST emit
// exactly once on process startup to signal it can accept work
// (specs/handler-contract.md §4.9.HC-039).
//
// The daemon MUST NOT dispatch work to a session before observing this message
// for that session.  The watcher translates it into the agent_ready bus event
// per §6.4.
//
// # Wire fields
//
// Normative field list per specs/handler-contract.md §4.9.HC-039:
//
//   - type          — always "agent_ready" (ProgressMsgTypeAgentReady);
//     not stored after dispatch.
//   - session_id    — the session that just became ready.
//   - capabilities  — ordered list of capability strings the handler claims
//     for this session.  MUST be non-nil; may be empty when the handler
//     declares no optional capabilities.
//
// Full payload schema is owned by event-model.md §6.3; this struct carries
// the minimum required fields declared in handler-contract.md §4.9.HC-039.
//
// JSON tags match the on-wire field names.
type AgentReadyMsg struct {
	// Type is always ProgressMsgTypeAgentReady; retained for round-trip
	// fidelity.  The watcher dispatches on this field before decoding.
	Type string `json:"type"`

	// SessionID is the session that just became ready
	// (specs/handler-contract.md §4.9.HC-039).
	SessionID string `json:"session_id"`

	// Capabilities is the list of capability strings the handler claims for
	// this session (specs/handler-contract.md §4.9.HC-039).
	//
	// The vocabulary is extensible and handler-defined.  The watcher passes
	// the slice through to the agent_ready bus event without validation.
	Capabilities []string `json:"capabilities"`
}
