package core

// launchdiag_hkfra5l.go — payload types for the launch-diagnostic event types:
//
//   - pasteinject_failed  — paste-inject delivery failure (file absent, WriteLastPane error)
//   - launch_stall_detected — run_started seen but no launch_initiated within 30 s
//   - spawn_cap_blocked — SpawnWindow could not acquire a spawn slot within the
//     bounded acquire timeout (slot-leak signature; hk-4l7zs)
//
// Refs: hk-fra5l (first two), hk-4l7zs (spawn_cap_blocked).

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

// SpawnCapBlockedPayload is the event-bus payload for the spawn_cap_blocked
// event type (hk-4l7zs).
//
// Emitted by the daemon when tmuxSubstrate.SpawnWindow cannot acquire a
// spawn-semaphore slot within the bounded acquire timeout. This is the
// observable signature of a slot leak: every slot is held by a session that
// acquired it and never released it, so a new launch cannot proceed. Carrying
// the in-use / cap counts lets operators confirm the pool is saturated.
//
// # Payload fields
//
//   - run_id       — the run whose launch was blocked (required, non-empty)
//   - waited_ms    — milliseconds spent blocked before timing out (> 0)
//   - slots_in_use — spawn-semaphore slots held at the moment of the timeout
//   - cap_size     — configured spawn-cap ceiling (> 0)
type SpawnCapBlockedPayload struct {
	// RunID is the run whose launch was blocked. Required (non-empty).
	RunID string `json:"run_id"`

	// WaitedMS is the number of milliseconds SpawnWindow blocked before the
	// acquire timeout fired. Always positive.
	WaitedMS int64 `json:"waited_ms"`

	// SlotsInUse is the number of spawn-semaphore slots held when the timeout
	// fired (expected == CapSize, i.e. the pool was saturated).
	SlotsInUse int `json:"slots_in_use"`

	// CapSize is the configured spawn-cap ceiling. Always positive.
	CapSize int `json:"cap_size"`
}

// Valid reports whether p is a well-formed SpawnCapBlockedPayload.
func (p SpawnCapBlockedPayload) Valid() bool {
	return p.RunID != "" && p.WaitedMS > 0 && p.CapSize > 0
}

// TmuxNewWindowTimeoutPayload is the event-bus payload for the
// tmux_new_window_timeout event type (hk-r1rup).
//
// Emitted by the daemon when tmuxSubstrate.SpawnWindow's underlying
// `tmux new-window` shell call (adapter.NewWindowIn) does not return within the
// bounded new-window timeout. This is the observable signature of a hung tmux
// invocation: the call neither succeeds nor errors, so handler.Launch never
// returns, launch_initiated never fires, and the run wedges at
// launch_stall_detected → run_stale forever, holding a daemon slot. Bounding the
// call converts that indefinite hang into a prompt, observable launch failure.
//
// This is DISTINCT from spawn_cap_blocked, which fires when SpawnWindow cannot
// acquire a spawn-semaphore slot (a slot leak), not when the new-window call
// itself hangs.
//
// # Payload fields
//
//   - run_id    — the run whose new-window call hung (required, non-empty)
//   - waited_ms — milliseconds spent blocked before the timeout fired (> 0)
type TmuxNewWindowTimeoutPayload struct {
	// RunID is the run whose new-window call hung. Required (non-empty).
	RunID string `json:"run_id"`

	// WaitedMS is the number of milliseconds the new-window call blocked before
	// the bounded timeout fired. Always positive.
	WaitedMS int64 `json:"waited_ms"`
}

// Valid reports whether p is a well-formed TmuxNewWindowTimeoutPayload.
func (p TmuxNewWindowTimeoutPayload) Valid() bool {
	return p.RunID != "" && p.WaitedMS > 0
}

// ImplementerBudgetExceededPayload is the event-bus payload for the
// implementer_budget_exceeded event type (hk-9vp51).
//
// Emitted by pasteInjectQuitOnCommit when it force-kills a hosted implementer
// session that ran past its commit budget without committing. Two shapes of
// kill produce this event:
//
//  1. Hard ceiling — the absolute backstop (commitHardCeiling) elapsed even
//     though the pane was making progress (real agent_heartbeat events kept
//     extending the per-progress budget). A genuinely long task that never
//     finishes hits this.
//  2. Stalled — the pane went dark (no progress signal) for longer than the
//     per-progress budget window and the liveness checker confirmed no active
//     process, so the heartbeat-staleness / total-budget path fired.
//
// The payload makes a previously-silent no_commit self-explaining: operators
// see how long the run actually ran (elapsed) and when it last made progress
// (since_last_progress).
//
// # Payload fields
//
//   - run_id                  — the killed run (required, non-empty)
//   - elapsed_ms              — milliseconds the commit-poll loop ran before the
//     kill (> 0)
//   - since_last_progress_ms  — milliseconds since the last progress signal
//     (agent_heartbeat) at the moment of the kill (>= 0)
//   - reason                  — short human-readable kill reason (required,
//     non-empty)
type ImplementerBudgetExceededPayload struct {
	// RunID is the killed run. Required (non-empty).
	RunID string `json:"run_id"`

	// ElapsedMS is the number of milliseconds the commit-poll loop ran before
	// the kill fired. Always positive.
	ElapsedMS int64 `json:"elapsed_ms"`

	// SinceLastProgressMS is the number of milliseconds since the last progress
	// signal (agent_heartbeat) was observed, measured at the kill. Non-negative.
	SinceLastProgressMS int64 `json:"since_last_progress_ms"`

	// Reason is a short human-readable description of why the budget was
	// exceeded (e.g. "hard-ceiling", "heartbeat-stale"). Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed ImplementerBudgetExceededPayload.
func (p ImplementerBudgetExceededPayload) Valid() bool {
	return p.RunID != "" && p.ElapsedMS > 0 && p.SinceLastProgressMS >= 0 && p.Reason != ""
}

// ReviewerBudgetExceededPayload is the event-bus payload for the
// reviewer_budget_exceeded event type (hk-da3rr).
//
// Emitted when pasteInjectQuitOnReviewFile force-kills a hosted reviewer
// session that exhausted its diff-scaled verdict budget without writing a
// verdict file. Both the builtin review-loop path (reviewloop.go) and the DOT
// reviewer-node path (dot_cascade.go) read the marker file written by
// writeReviewerBudgetSentinel and emit this event in place of the generic
// "verdict absent" error.
//
// The payload makes a previously-silent no-verdict self-explaining: operators
// see how large the diff was (changed_lines), what the diff-scaled budget was
// (budget_ms), how long the reviewer actually ran (elapsed_ms), and a
// human-readable kill reason.
//
// # Payload fields
//
//   - run_id        — the killed run (required, non-empty)
//   - budget_ms     — diff-scaled verdict budget in milliseconds (> 0)
//   - elapsed_ms    — milliseconds the reviewer ran before the kill (> 0)
//   - changed_lines — number of changed lines in the diff used to scale the
//     budget (>= 0)
//   - reason        — short human-readable kill reason (required, non-empty)
type ReviewerBudgetExceededPayload struct {
	// RunID is the killed run. Required (non-empty).
	RunID string `json:"run_id"`

	// BudgetMS is the diff-scaled verdict budget in milliseconds. Always positive.
	BudgetMS int64 `json:"budget_ms"`

	// ElapsedMS is the number of milliseconds the reviewer ran before the kill.
	// Always positive.
	ElapsedMS int64 `json:"elapsed_ms"`

	// ChangedLines is the number of changed lines in the diff used to scale the
	// budget. Non-negative.
	ChangedLines int `json:"changed_lines"`

	// Reason is a short human-readable description of why the budget was
	// exceeded (e.g. "reviewer-budget-hard-ceiling"). Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed ReviewerBudgetExceededPayload.
func (p ReviewerBudgetExceededPayload) Valid() bool {
	return p.RunID != "" && p.BudgetMS > 0 && p.ElapsedMS > 0 && p.ChangedLines >= 0 && p.Reason != ""
}
