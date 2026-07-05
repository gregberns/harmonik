package core

// stalldetected_hkl087e.go — payload type for the stall_detected event type.
//
// Emitted by the Layer A detector (hk-l087e) when one of three per-run stall
// signatures fires:
//   - heartbeat_gap  (class-2 silent hang): no agent_heartbeat/agent_message
//     for > run_silence_stall.
//   - review_stall   (class-3 review wedge): reviewer_verdict fired but no
//     run_completed/run_failed within review_finalize_stall.
//   - run_age        (backstop): run dispatched > run_max_age with no terminal
//     event.
//
// Spec: .kerf/works/stall-sentinel/SPEC.md §2 (Layer A), 02-analysis.md §Layer A.
// Bead: hk-l087e.

// StallSignature is the class of stall detected by the Layer A detector.
// Each value corresponds directly to one of the three detection conditions in
// the spec (02-analysis.md §Layer A).
type StallSignature string

const (
	// StallSignatureHeartbeatGap fires when a non-terminal run has produced no
	// agent_heartbeat or agent_message for longer than run_silence_stall.
	// This is the class-2 (silent hang) signature.
	StallSignatureHeartbeatGap StallSignature = "heartbeat_gap"

	// StallSignatureReviewStall fires when reviewer_verdict has been emitted for
	// a run but run_completed / run_failed has not arrived within
	// review_finalize_stall. This is the class-3 (review-loop wedge) signature.
	StallSignatureReviewStall StallSignature = "review_stall"

	// StallSignatureRunAge fires when a non-terminal run has been active for
	// longer than run_max_age. This is the backstop signature for novel hangs not
	// caught by the two more-specific detectors above.
	StallSignatureRunAge StallSignature = "run_age"
)

// StallDetectedPayload is the event-bus payload for the stall_detected event type.
//
// Emitted at most once per (run_id, signature) per detector pass so the event
// log is not flooded on repeated scans. De-duplication is the caller's
// responsibility; the payload itself records enough context for triage.
//
// # Payload fields
//
//   - run_id     — the stalled run (required, non-empty)
//   - bead_id    — the bead being executed (required, non-empty)
//   - signature  — which stall condition fired (heartbeat_gap | review_stall | run_age)
//   - elapsed_ms — milliseconds elapsed since the stall condition began:
//     heartbeat_gap: ms since LastEventAt; review_stall: ms since VerdictAt;
//     run_age: ms since StartedAt.
type StallDetectedPayload struct {
	// RunID is the stalled run. Required (non-empty).
	RunID string `json:"run_id"`

	// BeadID is the bead being executed. Required (non-empty).
	BeadID string `json:"bead_id"`

	// Signature is the stall class that fired.
	Signature StallSignature `json:"signature"`

	// ElapsedMs is the number of milliseconds elapsed since the stall condition
	// began. Always positive.
	ElapsedMs int64 `json:"elapsed_ms"`
}

// Valid reports whether p is a well-formed StallDetectedPayload.
func (p StallDetectedPayload) Valid() bool {
	return p.RunID != "" &&
		p.BeadID != "" &&
		p.Signature != "" &&
		p.ElapsedMs > 0
}
