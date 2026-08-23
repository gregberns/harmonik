package core

// AgentReadyStallDetectedPayload is the event-bus payload for the
// agent_ready_stall_detected event type.
//
// Emitted by the stale watcher when a run has emitted launch_initiated but no
// agent_ready has been observed within agentReadyStallThreshold (a few minutes).
// This indicates the agent process spawned but never reported ready — e.g. the
// claude process never started, the -default session was orphaned, or the
// relay never synthesized agent_ready.
//
// # Payload fields
//
//   - run_id        — the stalled run (required, non-empty)
//   - bead_id       — the bead being executed (required, non-empty)
//   - stall_seconds — seconds elapsed since launch_initiated without agent_ready
type AgentReadyStallDetectedPayload struct {
	// RunID is the stalled run. Required (non-empty).
	RunID string `json:"run_id"`

	// BeadID is the bead being executed. Required (non-empty).
	BeadID string `json:"bead_id"`

	// StallSeconds is the number of seconds elapsed since launch_initiated was
	// observed without a subsequent agent_ready.  Always positive.
	StallSeconds int64 `json:"stall_seconds"`
}

// Valid reports whether p is a well-formed AgentReadyStallDetectedPayload.
func (p AgentReadyStallDetectedPayload) Valid() bool {
	return p.RunID != "" && p.BeadID != "" && p.StallSeconds > 0
}
