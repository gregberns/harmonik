package core

// launchdiag_hkfra5l.go — payload types for the two launch-diagnostic event
// types introduced by hk-fra5l:
//
//   - pasteinject_failed  — paste-inject delivery failure (file absent, WriteLastPane error)
//   - launch_stall_detected — run_started seen but no launch_initiated within 30 s
//
// Refs: hk-fra5l.

// PasteInjectFailedPayload is the event-bus payload for the pasteinject_failed
// event type.
//
// Emitted by the daemon when the paste-inject step cannot deliver the kick-off
// message to the tmux pane.  Carries enough context to identify which run and
// which phase failed, and why.
//
// # Payload fields
//
//   - run_id  — the run whose paste-inject failed (required, non-empty)
//   - phase   — the review-loop phase string ("implementer-initial",
//     "implementer-resume", "reviewer", or empty for single-mode)
//   - reason  — short human-readable description of the failure (required,
//     non-empty)
type PasteInjectFailedPayload struct {
	// RunID is the run whose paste-inject failed. Required (non-empty).
	RunID string `json:"run_id"`

	// Phase is the review-loop phase in which the failure occurred.
	// One of "implementer-initial", "implementer-resume", "reviewer",
	// or "" (empty) for the single-mode path.
	Phase string `json:"phase,omitempty"`

	// Reason is a short human-readable description of the failure.
	// Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed PasteInjectFailedPayload.
func (p PasteInjectFailedPayload) Valid() bool {
	return p.RunID != "" && p.Reason != ""
}

// LaunchStallDetectedPayload is the event-bus payload for the
// launch_stall_detected event type.
//
// Emitted by the stale watcher when a run has emitted run_started but no
// launch_initiated has been observed within launchStallThreshold (30 s).
// This indicates the pre-exec sequence stalled — typically a tmux window
// creation failure or a pre-exec emission gap in the daemon.
//
// # Payload fields
//
//   - run_id        — the stalled run (required, non-empty)
//   - bead_id       — the bead being executed (required, non-empty)
//   - stall_seconds — seconds elapsed since run_started without launch_initiated
type LaunchStallDetectedPayload struct {
	// RunID is the stalled run. Required (non-empty).
	RunID string `json:"run_id"`

	// BeadID is the bead being executed. Required (non-empty).
	BeadID string `json:"bead_id"`

	// StallSeconds is the number of seconds elapsed since run_started was
	// observed without a subsequent launch_initiated.  Always positive.
	StallSeconds int64 `json:"stall_seconds"`
}

// Valid reports whether p is a well-formed LaunchStallDetectedPayload.
func (p LaunchStallDetectedPayload) Valid() bool {
	return p.RunID != "" && p.BeadID != "" && p.StallSeconds > 0
}
