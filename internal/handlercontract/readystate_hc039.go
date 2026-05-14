package handlercontract

// readyState — per-bead helper prefix for test helpers in
// readystate_hc039_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.46).

// AgentReadyMsg is the on-wire NDJSON message emitted (either by the handler
// subprocess for non-tmux substrates, or synthesized by the hook-relay on
// first SessionStart receipt for the tmux substrate) to signal the session is
// ready to accept work (specs/handler-contract.md §4.9.HC-039).
//
// The daemon MUST NOT dispatch work to a session before observing this message
// for that session.  The watcher translates it into the agent_ready bus event
// per §6.4.
//
// Under the interactive (tmux) substrate, agent_ready is synthesized by the
// hook-relay subprocess on receipt of the first SessionStart hook from Claude,
// and carries provenance = "claude_session_start" (CHB-013 / HC-039).
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
//   - provenance    — optional; "claude_session_start" when the message was
//     synthesized by the relay on SessionStart receipt (CHB-013).
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

	// Provenance is the source of the agent_ready signal.  When synthesized
	// by the hook-relay on first SessionStart receipt (CHB-013 / HC-039),
	// this field is "claude_session_start".  Empty for non-tmux substrates
	// that emit agent_ready directly.
	Provenance string `json:"provenance,omitempty"`
}

// LaunchInitiatedMsg is the on-wire NDJSON message the handler-process MUST
// emit as step 4 of the pre-exec sequence per CHB-018, replacing the previous
// agent_ready self-emission under the interactive (tmux) substrate.
//
// launch_initiated signals that the handler is about to exec Claude Code but
// does NOT indicate Claude is ready to accept work.  The ready-state signal
// under the tmux substrate is the relay-synthesized agent_ready on first
// SessionStart hook receipt (CHB-013 / HC-039).
//
// Adapters MUST NOT return true from DetectReady on receipt of this message
// per HC-041.
//
// # Wire fields
//
//   - type             — always "launch_initiated" (ProgressMsgTypeLaunchInitiated).
//   - session_id       — the handler session ID (HARMONIK_HANDLER_SESSION_ID).
//   - claude_session_id — the Claude session ID minted/reused by the handler.
//   - phase            — the launch phase (e.g., "single", "implementer-initial").
//
// JSON tags match the on-wire field names.
type LaunchInitiatedMsg struct {
	// Type is always ProgressMsgTypeLaunchInitiated.
	Type string `json:"type"`

	// SessionID is the handler-assigned session identifier.
	SessionID string `json:"session_id"`

	// ClaudeSessionID is the Claude Code session identifier minted or reused
	// by the handler subprocess for this launch.
	ClaudeSessionID string `json:"claude_session_id"`

	// Phase is the launch phase string (e.g., "single", "implementer-initial",
	// "implementer-resume", "reviewer").  Empty when unknown.
	Phase string `json:"phase,omitempty"`
}
