package cognition_test

// ev040_hkek3fl_test.go — scenario tests for specs/event-model.md §4.11 EV-040
// (missing heartbeat = daemon liveness failure; reconnect with backoff).
//
// EV-040 specifies that when a harmonik subscribe consumer does not receive a
// heartbeat for K × heartbeat_interval (recommended K=2 → 120s at 60s), it
// MUST treat the absence as a daemon liveness failure and reconnect with
// exponential backoff (suggested 5s/10s/30s). Reconnection MUST supply
// --since-event-id=<watermark> and MUST NOT start from live-stream head.
//
// Scenarios exercised:
//
//	EV040-S1: liveness failure detected after K × heartbeat_interval elapses
//	EV040-S2: heartbeat received within interval resets the window → no failure
//	EV040-S3: checker not started → IsLivenessFailed always false (no false
//	          positives before observation begins)
//	EV040-S4: K ≤ 0 normalised to 2 (spec-suggested default)
//	EV040-S5: BackoffSchedule progression: 5s → 10s → 30s → 30s (clamped)
//	EV040-S6: BackoffSchedule Reset() restarts sequence at first entry
//	EV040-S7: RecordHeartbeat resets the liveness window (timer restarts)
//	EV040-S8: DaemonDownEvent carries detected-at time and sock path
//
// Spec ref: specs/event-model.md §4.11 EV-040.
// Bead: hk-ek3fl.

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/cognition"
)

// ─────────────────────────────────────────────────────────────────────────────
// HeartbeatLivenessChecker tests — EV040-S1 through EV040-S4, EV040-S7
// ─────────────────────────────────────────────────────────────────────────────

// TestEV040_S1_LivenessFailureAfterKInterval verifies that IsLivenessFailed
// returns true once K × heartbeat_interval has elapsed without a heartbeat.
//
// Spec ref: EV-040 — "No heartbeat for K × heartbeat_interval → treat as
// daemon liveness failure."
// Bead: hk-ek3fl.
func TestEV040_S1_LivenessFailureAfterKInterval(t *testing.T) {
	t.Parallel()

	const K = 2
	interval := 60 * time.Second
	threshold := time.Duration(K) * interval // 120s

	var now time.Time
	checker := cognition.ExportNewHeartbeatLivenessCheckerWithClock(K, interval, func() time.Time {
		return now
	})

	now = time.Unix(1000, 0)
	checker.Start()

	// Just before the threshold: not yet failed.
	now = time.Unix(1000, 0).Add(threshold - time.Millisecond)
	if checker.IsLivenessFailed() {
		t.Errorf("EV040-S1: IsLivenessFailed()=true at %v before threshold %v; "+
			"must NOT fail before K×interval elapses (EV-040)", threshold-time.Millisecond, threshold)
	}

	// Exactly at the threshold: liveness failure.
	now = time.Unix(1000, 0).Add(threshold)
	if !checker.IsLivenessFailed() {
		t.Errorf("EV040-S1 FAIL: IsLivenessFailed()=false at threshold %v; "+
			"MUST return true after K=%d × interval=%v elapses without heartbeat (EV-040)", threshold, K, interval)
	}

	// Well past the threshold: still failed.
	now = time.Unix(1000, 0).Add(threshold + time.Minute)
	if !checker.IsLivenessFailed() {
		t.Errorf("EV040-S1 FAIL: IsLivenessFailed()=false well past threshold; " +
			"must remain failed until reconnect (EV-040)")
	}

	t.Logf("EV040-S1 PASS: liveness failure detected at K=%d × interval=%v = %v", K, interval, threshold)
}

// TestEV040_S2_HeartbeatWithinIntervalNoFailure verifies that receiving a
// heartbeat before K × heartbeat_interval prevents liveness failure.
//
// Spec ref: EV-040 — heartbeat resets the absence window.
// Bead: hk-ek3fl.
func TestEV040_S2_HeartbeatWithinIntervalNoFailure(t *testing.T) {
	t.Parallel()

	const K = 2
	interval := 60 * time.Second

	var now time.Time
	checker := cognition.ExportNewHeartbeatLivenessCheckerWithClock(K, interval, func() time.Time {
		return now
	})

	now = time.Unix(2000, 0)
	checker.Start()

	// Advance to 50s (within the first 60s window) and deliver a heartbeat.
	now = time.Unix(2000, 0).Add(50 * time.Second)
	checker.RecordHeartbeat()

	// Advance an additional 110s (50s + 110s = 160s from start, but only
	// 110s from the last heartbeat — still below 120s threshold).
	now = time.Unix(2000, 0).Add(50*time.Second + 110*time.Second)
	if checker.IsLivenessFailed() {
		t.Errorf("EV040-S2 FAIL: IsLivenessFailed()=true 110s after heartbeat; "+
			"threshold is K=%d × interval=%v = %v; heartbeat must reset the window (EV-040)",
			K, interval, time.Duration(K)*interval)
	}

	t.Logf("EV040-S2 PASS: heartbeat within interval prevented liveness failure")
}

// TestEV040_S3_NotStartedNeverFails verifies that IsLivenessFailed returns
// false when Start() has not been called, regardless of how much clock time
// passes.
//
// Spec ref: EV-040 — liveness observation must not produce false positives
// before the subscribe connection is established.
// Bead: hk-ek3fl.
func TestEV040_S3_NotStartedNeverFails(t *testing.T) {
	t.Parallel()

	var now time.Time
	checker := cognition.ExportNewHeartbeatLivenessCheckerWithClock(2, 60*time.Second, func() time.Time {
		return now
	})

	// Advance time far into the future without calling Start().
	now = time.Unix(0, 0).Add(24 * time.Hour)

	if checker.IsLivenessFailed() {
		t.Errorf("EV040-S3 FAIL: IsLivenessFailed()=true before Start() was called; " +
			"MUST NOT fire before observation begins (EV-040)")
	}

	t.Logf("EV040-S3 PASS: no false liveness failure before Start()")
}

// TestEV040_S4_DefaultKIsTwo verifies that K ≤ 0 is normalised to 2, matching
// the spec-suggested default.
//
// Spec ref: EV-040 — "recommended K=2."
// Bead: hk-ek3fl.
func TestEV040_S4_DefaultKIsTwo(t *testing.T) {
	t.Parallel()

	interval := 60 * time.Second

	var now time.Time
	checker := cognition.ExportNewHeartbeatLivenessCheckerWithClock(0, interval, func() time.Time {
		return now
	})

	now = time.Unix(4000, 0)
	checker.Start()

	// 1 × interval (60s): should NOT fail (K=2 required, not K=1).
	now = time.Unix(4000, 0).Add(interval)
	if checker.IsLivenessFailed() {
		t.Errorf("EV040-S4: IsLivenessFailed()=true at 1×interval; K=0 must normalise to K=2; " +
			"must NOT fail before 2×interval (EV-040)")
	}

	// 2 × interval (120s): MUST fail.
	now = time.Unix(4000, 0).Add(2 * interval)
	if !checker.IsLivenessFailed() {
		t.Errorf("EV040-S4 FAIL: IsLivenessFailed()=false at 2×interval with K=0→2; " +
			"MUST fail at K=2 × interval (EV-040)")
	}

	t.Logf("EV040-S4 PASS: K=0 normalised to 2; liveness check at 2×interval=120s")
}

// TestEV040_S7_RecordHeartbeatResetsWindow verifies that RecordHeartbeat resets
// the absence timer so the full K × interval is required again.
//
// Spec ref: EV-040 — receiving a heartbeat resets the absence window.
// Bead: hk-ek3fl.
func TestEV040_S7_RecordHeartbeatResetsWindow(t *testing.T) {
	t.Parallel()

	const K = 2
	interval := 60 * time.Second
	threshold := time.Duration(K) * interval

	var now time.Time
	checker := cognition.ExportNewHeartbeatLivenessCheckerWithClock(K, interval, func() time.Time {
		return now
	})

	now = time.Unix(7000, 0)
	checker.Start()

	// Advance past the threshold — liveness should have failed.
	now = time.Unix(7000, 0).Add(threshold + time.Second)
	if !checker.IsLivenessFailed() {
		t.Fatalf("EV040-S7 precondition: IsLivenessFailed()=false at threshold; "+
			"liveness must fail after K=%d × interval=%v", K, interval)
	}

	// Deliver a heartbeat: timer resets.
	checker.RecordHeartbeat()
	if checker.IsLivenessFailed() {
		t.Errorf("EV040-S7 FAIL: IsLivenessFailed()=true immediately after RecordHeartbeat; " +
			"heartbeat MUST reset the liveness window (EV-040)")
	}

	// Advance almost to the threshold again: not yet failed.
	now = now.Add(threshold - time.Millisecond)
	if checker.IsLivenessFailed() {
		t.Errorf("EV040-S7: IsLivenessFailed()=true %v before threshold after reset; "+
			"window must not expire early", time.Millisecond)
	}

	// Cross the threshold: failed again.
	now = now.Add(time.Millisecond)
	if !checker.IsLivenessFailed() {
		t.Errorf("EV040-S7 FAIL: IsLivenessFailed()=false at threshold after heartbeat reset; " +
			"MUST fail again after K×interval without a subsequent heartbeat (EV-040)")
	}

	t.Logf("EV040-S7 PASS: RecordHeartbeat reset the liveness window")
}

// ─────────────────────────────────────────────────────────────────────────────
// BackoffSchedule tests — EV040-S5, EV040-S6
// ─────────────────────────────────────────────────────────────────────────────

// TestEV040_S5_BackoffScheduleProgression verifies the 5s/10s/30s sequence
// and that the schedule clamps to the last entry once exhausted.
//
// Spec ref: EV-040 — "Reconnect with exponential backoff (suggested 5s/10s/30s)."
// Bead: hk-ek3fl.
func TestEV040_S5_BackoffScheduleProgression(t *testing.T) {
	t.Parallel()

	sched := cognition.NewBackoffSchedule(nil) // nil → DefaultBackoffDelays

	steps := []struct {
		want time.Duration
		desc string
	}{
		{5 * time.Second, "first attempt"},
		{10 * time.Second, "second attempt"},
		{30 * time.Second, "third attempt"},
		{30 * time.Second, "fourth attempt (clamped at last entry)"},
		{30 * time.Second, "fifth attempt (still clamped)"},
	}

	for i, step := range steps {
		got := sched.Next()
		if got != step.want {
			t.Errorf("EV040-S5 FAIL: step %d (%s): Next()=%v, want %v; "+
				"backoff must follow the 5s/10s/30s(+) sequence (EV-040)",
				i+1, step.desc, got, step.want)
		}
	}

	t.Logf("EV040-S5 PASS: backoff sequence 5s→10s→30s→30s→30s (clamped)")
}

// TestEV040_S6_BackoffScheduleResetRestartsSequence verifies that Reset()
// restarts the schedule from the first entry after a successful reconnect.
//
// Spec ref: EV-040 — after a successful reconnect the next failure should start
// from the shortest delay again.
// Bead: hk-ek3fl.
func TestEV040_S6_BackoffScheduleResetRestartsSequence(t *testing.T) {
	t.Parallel()

	sched := cognition.NewBackoffSchedule(nil)

	// Exhaust the schedule.
	for i := 0; i < 5; i++ {
		sched.Next()
	}

	// Reset simulates a successful reconnect.
	sched.Reset()

	// Sequence must restart from the beginning.
	got := sched.Next()
	if got != 5*time.Second {
		t.Errorf("EV040-S6 FAIL: after Reset(), Next()=%v, want 5s; "+
			"Reset() must restart the backoff sequence from the first entry (EV-040)", got)
	}

	got = sched.Next()
	if got != 10*time.Second {
		t.Errorf("EV040-S6: after Reset() step 2: Next()=%v, want 10s", got)
	}

	t.Logf("EV040-S6 PASS: Reset() restarts the backoff schedule from 5s")
}

// ─────────────────────────────────────────────────────────────────────────────
// DaemonDownEvent tests — EV040-S8
// ─────────────────────────────────────────────────────────────────────────────

// TestEV040_S8_DaemonDownEvent verifies that DaemonDownEvent carries the
// detected-at time and socket path for consumer reaction.
//
// Spec ref: EV-040 — "If harmonik subscribe exits 17 (daemon-not-running
// sentinel), emit a synthetic daemon_down signal to the consumer's reaction layer."
// Bead: hk-ek3fl.
func TestEV040_S8_DaemonDownEvent(t *testing.T) {
	t.Parallel()

	at := time.Unix(8000, 0)
	sock := "/tmp/test-project/.harmonik/daemon.sock"

	ev := cognition.DaemonDownEvent{
		DetectedAt: at,
		SockPath:   sock,
	}

	if ev.DetectedAt != at {
		t.Errorf("EV040-S8: DetectedAt=%v, want %v", ev.DetectedAt, at)
	}
	if ev.SockPath != sock {
		t.Errorf("EV040-S8: SockPath=%q, want %q", ev.SockPath, sock)
	}

	// Zero-value SockPath is valid (socket path may be unavailable).
	evNoSock := cognition.DaemonDownEvent{DetectedAt: at}
	if evNoSock.SockPath != "" {
		t.Errorf("EV040-S8: empty SockPath: got %q, want empty string", evNoSock.SockPath)
	}

	t.Logf("EV040-S8 PASS: DaemonDownEvent carries DetectedAt and SockPath")
}
