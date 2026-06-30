package core

// agentreadystall_hk1s1or.go — payload type for the agent_ready_stall_detected
// event type.
//
// The stale watcher already covers two adjacent gaps in the launch sequence:
//
//   - run_started → launch_initiated  (launch_stall_detected, hk-fra5l, ~30 s)
//   - launch_initiated → agent_ready  (the never-spawned reaper CANCEL, hk-0z5x,
//     ~30 min)
//
// Between those two there was a detection blind spot: once launch_initiated
// arrives the launch-stall check is suppressed, and nothing emits an observable
// event for the launch_initiated → agent_ready interval until the 30-min reaper
// cancels the run. agent_ready_stall_detected fills that gap — it fires once,
// in a bounded few-minute window, so a hung launch→ready transition is visible
// and recoverable long before the 30-min deadline.
//
// Ref: hk-1s1or.

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
