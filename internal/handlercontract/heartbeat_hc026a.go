package handlercontract

// heartbeat — per-bead helper prefix for test helpers in
// heartbeat_hc026a_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.32).

// ─────────────────────────────────────────────────────────────────────────────
// HC-026a — Handler heartbeat obligation (≤ T/2 cadence)
// ─────────────────────────────────────────────────────────────────────────────

// HeartbeatPhase is the extensible phase enum carried in every agent_heartbeat
// progress-stream message per HC-026a.
//
// The enum is additive-only (open): handlers MAY declare additional phase
// values in their per-handler subsystem envelope; the watcher MUST NOT reject
// unknown phase values (additive-evolution rule).
//
// Spec: specs/handler-contract.md §4.6.HC-026a.
type HeartbeatPhase string

// Required heartbeat phase values per HC-026a.
const (
	// HeartbeatPhaseStarting is emitted during handler startup (before agent_ready).
	// Spec: handler-contract.md §4.6.HC-026a.
	HeartbeatPhaseStarting HeartbeatPhase = "starting"

	// HeartbeatPhaseReasoning is emitted while the agent is performing LLM reasoning.
	// Handlers wrapping LLMs with no output channel MUST synthesize heartbeats on an
	// internal timer during extended reasoning per HC-026a.
	// Spec: handler-contract.md §4.6.HC-026a.
	HeartbeatPhaseReasoning HeartbeatPhase = "reasoning"

	// HeartbeatPhaseToolCall is emitted while a tool call is in-flight.
	// Spec: handler-contract.md §4.6.HC-026a.
	HeartbeatPhaseToolCall HeartbeatPhase = "tool_call"

	// HeartbeatPhaseWaitingInput is emitted while waiting for operator input or
	// during rate-limited windows per HC-025.  The rate-limit and silent-hang
	// regimes are independent; heartbeats MUST continue during rate-limited windows.
	// Spec: handler-contract.md §4.6.HC-026a + §4.6.HC-025.
	HeartbeatPhaseWaitingInput HeartbeatPhase = "waiting_input"

	// HeartbeatPhaseRotating is emitted during an account-rotation turn boundary
	// per HC-013a.
	// Spec: handler-contract.md §4.6.HC-026a + §4.3.HC-013a.
	HeartbeatPhaseRotating HeartbeatPhase = "rotating"

	// HeartbeatPhaseShuttingDown is emitted during the post-outcome shutdown window
	// per HC-008a.  Heartbeat emission is not required during the shutdown window
	// (silent-hang is suspended in that regime), but handlers MAY emit it.
	// Spec: handler-contract.md §4.6.HC-026a + §4.2.HC-008a.
	HeartbeatPhaseShuttingDown HeartbeatPhase = "shutting_down"
)

// requiredHeartbeatPhases is the normative set of phase values declared in
// HC-026a.  Used by ValidPhase and tests to enumerate the required set.
var requiredHeartbeatPhases = map[HeartbeatPhase]struct{}{
	HeartbeatPhaseStarting:     {},
	HeartbeatPhaseReasoning:    {},
	HeartbeatPhaseToolCall:     {},
	HeartbeatPhaseWaitingInput: {},
	HeartbeatPhaseRotating:     {},
	HeartbeatPhaseShuttingDown: {},
}

// IsRequiredPhase reports whether phase is one of the 6 required phase values
// declared in HC-026a.  Returns false for any additional (extension) phase.
//
// Spec: handler-contract.md §4.6.HC-026a.
func (p HeartbeatPhase) IsRequiredPhase() bool {
	_, ok := requiredHeartbeatPhases[p]
	return ok
}

// HeartbeatMsg is the JSON-serialisable wire-format struct for an
// agent_heartbeat progress-stream message per HC-026a.
//
// The watcher receives HeartbeatMsg from the handler's progress stream
// (NDJSON-framed per HC-007) and publishes the corresponding bus event per
// HC-011.
//
// Schema:
//   - type:       always ProgressMsgTypeAgentHeartbeat ("agent_heartbeat")
//   - session_id: the stable daemon-assigned session identifier
//   - phase:      one of the HeartbeatPhase constants (extensible, additive-only)
//
// Spec: handler-contract.md §4.6.HC-026a; §6.4 session_id definition.
type HeartbeatMsg struct {
	// Type is the progress-stream discriminator.
	// MUST always be ProgressMsgTypeAgentHeartbeat ("agent_heartbeat").
	Type string `json:"type"`

	// SessionID is the stable daemon-assigned session identifier.
	// Required; must be non-empty.
	SessionID string `json:"session_id"`

	// Phase is the agent's current execution phase.
	// Required; drawn from HeartbeatPhase constants; extensible enum.
	Phase HeartbeatPhase `json:"phase"`
}

// RequiredHeartbeatPhaseCount is the count of required phase values declared
// by HC-026a.  Exposed for sensor tests that enumerate the required set.
//
// Spec: handler-contract.md §4.6.HC-026a.
const RequiredHeartbeatPhaseCount = 6
