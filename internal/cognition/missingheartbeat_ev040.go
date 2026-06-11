package cognition

// missingheartbeat_ev040.go — EV-040 missing-heartbeat liveness-failure detection
// and reconnect-with-backoff consumer contract.
//
// EV-040 specifies that when a subscribe consumer does not receive a heartbeat
// for K × heartbeat_interval (recommended K=2 → 120s at 60s), it MUST treat
// the absence as a daemon liveness failure and reconnect with exponential
// backoff (suggested 5s/10s/30s). Reconnection MUST supply
// --since-event-id=<watermark> and MUST NOT start from the live-stream head.
//
// When harmonik subscribe exits 17 (daemon-not-running sentinel), the consumer
// MUST emit a synthetic DaemonDownEvent to its reaction layer.
//
// This file provides the pure, clock-injectable mechanism layer; callers
// drive reconnect with their own loops and supply the last_event_id watermark
// (per EV-037/EV-037a) to the --since-event-id flag on each reconnect.
//
// Spec ref: specs/event-model.md §4.11 EV-040.
// Bead: hk-ek3fl.

import "time"

// DefaultBackoffDelays is the suggested reconnect backoff sequence from EV-040
// (5s / 10s / 30s). BackoffSchedule clamps to the last entry once exhausted.
var DefaultBackoffDelays = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

// BackoffSchedule implements the exponential-backoff sequence for subscribe
// reconnect attempts.
//
// Call Next() to advance through the sequence; the last value is returned for
// all subsequent calls (clamping, not wrapping). Call Reset() on a successful
// reconnect to restart the sequence from the beginning.
//
// Not safe for concurrent use.
type BackoffSchedule struct {
	delays []time.Duration
	index  int
}

// NewBackoffSchedule creates a BackoffSchedule with the given delay sequence.
// If delays is empty or nil, DefaultBackoffDelays is used.
//
// Spec ref: EV-040 — "reconnect with exponential backoff (suggested 5s/10s/30s)."
func NewBackoffSchedule(delays []time.Duration) *BackoffSchedule {
	if len(delays) == 0 {
		delays = DefaultBackoffDelays
	}
	d := make([]time.Duration, len(delays))
	copy(d, delays)
	return &BackoffSchedule{delays: d}
}

// Next returns the next backoff delay. Once all explicit steps are exhausted
// it returns the last step indefinitely (clamping behaviour).
//
// Spec ref: EV-040 — "suggested 5s/10s/30s" implies the ceiling is 30s once
// the schedule is exhausted.
func (b *BackoffSchedule) Next() time.Duration {
	if b.index >= len(b.delays) {
		return b.delays[len(b.delays)-1]
	}
	d := b.delays[b.index]
	b.index++
	return d
}

// Reset restarts the schedule from the first entry. Call after a successful
// reconnect so the next failure starts from the shortest delay again.
func (b *BackoffSchedule) Reset() {
	b.index = 0
}

// HeartbeatLivenessChecker detects daemon liveness failure from a missing
// heartbeat on a harmonik subscribe stream.
//
// Usage:
//
//	checker := cognition.NewHeartbeatLivenessChecker(2, 60*time.Second)
//	checker.Start()                          // once, on subscribe connect
//	for line := range subscribeStream {
//	    if isHeartbeat(line) {
//	        checker.RecordHeartbeat()
//	    }
//	    if checker.IsLivenessFailed() {
//	        // → treat as daemon liveness failure; reconnect per EV-040
//	    }
//	}
//
// Not safe for concurrent use.
//
// Spec ref: specs/event-model.md §4.11 EV-040.
type HeartbeatLivenessChecker struct {
	k                 int
	heartbeatInterval time.Duration
	now               func() time.Time
	lastHeartbeat     time.Time
	started           bool
}

// NewHeartbeatLivenessChecker creates a checker with the given K multiplier
// and heartbeat interval. K ≤ 0 is normalised to 2 (the spec-suggested default).
//
// Spec ref: EV-040 — "recommended K=2 → 120s at 60s."
func NewHeartbeatLivenessChecker(k int, heartbeatInterval time.Duration) *HeartbeatLivenessChecker {
	return newHeartbeatLivenessCheckerWithClock(k, heartbeatInterval, time.Now)
}

// newHeartbeatLivenessCheckerWithClock is the clock-injectable variant used in
// tests. The now function is called each time IsLivenessFailed is evaluated.
func newHeartbeatLivenessCheckerWithClock(k int, heartbeatInterval time.Duration, now func() time.Time) *HeartbeatLivenessChecker {
	if k <= 0 {
		k = 2
	}
	return &HeartbeatLivenessChecker{
		k:                 k,
		heartbeatInterval: heartbeatInterval,
		now:               now,
	}
}

// Start marks the moment the subscribe connection was established. The checker
// begins measuring absence from this point. Must be called exactly once per
// connection attempt.
//
// Spec ref: EV-040 — liveness window begins at subscribe connect time.
func (c *HeartbeatLivenessChecker) Start() {
	c.lastHeartbeat = c.now()
	c.started = true
}

// RecordHeartbeat records that a heartbeat event was received at the current
// clock time. This resets the liveness window.
//
// Spec ref: EV-040 — any received heartbeat resets the absence timer.
func (c *HeartbeatLivenessChecker) RecordHeartbeat() {
	c.lastHeartbeat = c.now()
}

// IsLivenessFailed returns true when no heartbeat has been received for at
// least K × heartbeat_interval since Start() or the last RecordHeartbeat().
//
// Returns false before Start() is called (observation has not begun).
//
// Spec ref: EV-040 — "No heartbeat for K × heartbeat_interval → treat as
// daemon liveness failure."
func (c *HeartbeatLivenessChecker) IsLivenessFailed() bool {
	if !c.started {
		return false
	}
	threshold := time.Duration(c.k) * c.heartbeatInterval
	return c.now().Sub(c.lastHeartbeat) >= threshold
}

// DaemonDownEvent is the synthetic signal consumers MUST emit to their reaction
// layer when harmonik subscribe exits with code 17 (daemon-not-running sentinel).
//
// Consumers detecting exit 17 in their subscribe loop SHOULD surface this event
// to trigger operator alerting or a Tier-2 reconciliation path.
//
// Spec ref: EV-040 — "If harmonik subscribe exits 17 … emit a synthetic
// daemon_down signal to the consumer's reaction layer."
type DaemonDownEvent struct {
	// DetectedAt is the wall-clock time the exit-17 condition was observed.
	DetectedAt time.Time

	// SockPath is the daemon socket path that was unreachable, if known.
	// Empty string when the socket path is not available.
	SockPath string
}
