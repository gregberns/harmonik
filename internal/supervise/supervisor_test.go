package supervise_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/supervise"
)

// exitScript returns a shell one-liner that exits with the given code.
func exitScript(code int) []string {
	return []string{"sh", "-c", "exit " + itoa(code)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [10]byte{}
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestRestartOnFailure_ThreeRestarts verifies that a child exiting code=1
// is restarted up to MaxRestarts with monotonically non-decreasing delays.
func TestRestartOnFailure_ThreeRestarts(t *testing.T) {
	spec := supervise.Spec{
		Command: exitScript(1),
		Policy:  supervise.PolicyOnFailure,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         200 * time.Millisecond,
			Jitter:      0,
			MaxRestarts: 3,
		},
		StartTimeout:    50 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
	}

	sv := supervise.New(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sv.Run(ctx)
	if err == nil {
		t.Fatal("expected crash-loop error, got nil")
	}

	snap := sv.Snapshot()
	if snap.Status != supervise.StatusCrashLoop {
		t.Errorf("expected status crashloop, got %s", snap.Status)
	}
	if snap.RestartCount < 3 {
		t.Errorf("expected ≥3 restarts, got %d", snap.RestartCount)
	}
}

// TestNoRestartOnCleanExit verifies that policy=on-failure does NOT restart a
// child that exits with code 0.
func TestNoRestartOnCleanExit(t *testing.T) {
	spec := supervise.Spec{
		Command: exitScript(0),
		Policy:  supervise.PolicyOnFailure,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         1 * time.Second,
			Jitter:      0,
			MaxRestarts: 5,
		},
		StartTimeout:    50 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
	}

	sv := supervise.New(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := sv.Run(ctx)
	if err != nil {
		t.Fatalf("expected nil error on clean exit, got %v", err)
	}

	snap := sv.Snapshot()
	if snap.RestartCount != 0 {
		t.Errorf("expected 0 restarts, got %d", snap.RestartCount)
	}
	if snap.Status == supervise.StatusCrashLoop {
		t.Errorf("unexpected crashloop status on clean exit")
	}
}

// TestMaxRestartsCap_CrashLoop verifies MaxRestarts=2 → exactly 2 restarts
// then crashloop + Run() returns.
func TestMaxRestartsCap_CrashLoop(t *testing.T) {
	spec := supervise.Spec{
		Command: exitScript(1),
		Policy:  supervise.PolicyOnFailure,
		Backoff: supervise.BackoffConfig{
			Base:        5 * time.Millisecond,
			Cap:         50 * time.Millisecond,
			Jitter:      0,
			MaxRestarts: 2,
		},
		StartTimeout:    20 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
	}

	sv := supervise.New(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sv.Run(ctx)
	if err == nil {
		t.Fatal("expected error from crash-loop, got nil")
	}

	snap := sv.Snapshot()
	if snap.Status != supervise.StatusCrashLoop {
		t.Errorf("expected crashloop status, got %s", snap.Status)
	}
}

// waitForStatus polls the supervisor snapshot until it reaches want or the
// deadline elapses. It returns the last snapshot observed and whether want was
// reached. The poll interval is short so timing-sensitive transitions are
// caught promptly.
func waitForStatus(sv *supervise.Supervisor, want supervise.Status, within time.Duration) (supervise.State, bool) {
	deadline := time.Now().Add(within)
	var last supervise.State
	for time.Now().Before(deadline) {
		last = sv.Snapshot()
		if last.Status == want {
			return last, true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return last, false
}

// TestHeartbeatStaleness verifies that a stale heartbeat file transitions
// state.Status to unhealthy WITHOUT triggering a restart. The probe interval is
// driven to ≤25ms via HealthProbeInterval so the transition is actually
// observed within the test window (the prior version conceded it could not
// assert this because the interval was hardcoded at 15s).
func TestHeartbeatStaleness(t *testing.T) {
	dir := t.TempDir()
	hbPath := filepath.Join(dir, "heartbeat.json")

	// Write a heartbeat file with a very old mtime (2 minutes ago) → stale.
	f, err := os.Create(hbPath)
	if err != nil {
		t.Fatalf("create heartbeat: %v", err)
	}
	f.Close()
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(hbPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Long-running child (sleep) so it doesn't exit before the probe fires.
	spec := supervise.Spec{
		Command:             []string{"sh", "-c", "sleep 30"},
		Policy:              supervise.PolicyOnFailure,
		HeartbeatPath:       hbPath,
		HeartbeatTTL:        30 * time.Second, // file is 2 min old → stale
		HealthProbeInterval: 25 * time.Millisecond,
		StartTimeout:        10 * time.Millisecond, // → running before first probe tick
		CrashLoopWindow:     10 * time.Second,
		StopTimeout:         500 * time.Millisecond,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         1 * time.Second,
			Jitter:      0,
			MaxRestarts: 0,
		},
	}

	sv := supervise.New(spec, silentLogger())
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- sv.Run(ctx) }()

	// With a 25ms probe interval and a 2-min-stale file, the supervisor must
	// transition to unhealthy well within 2s.
	snap, ok := waitForStatus(sv, supervise.StatusUnhealthy, 2*time.Second)
	if !ok {
		_ = sv.Stop(0)
		<-done
		t.Fatalf("expected status unhealthy from stale heartbeat, last seen %s", snap.Status)
	}
	// Health failure must NOT restart.
	if snap.RestartCount != 0 {
		t.Errorf("heartbeat failure should not restart; got %d restarts", snap.RestartCount)
	}
	if snap.Status == supervise.StatusCrashLoop {
		t.Errorf("heartbeat staleness must not trigger crashloop; got %s", snap.Status)
	}

	if err := sv.Stop(0); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-done
}

// TestHeartbeatFreshRemainsRunning verifies the control case: a fresh heartbeat
// file (mtime continuously bumped) keeps the supervisor in the running state —
// the probe must NOT spuriously mark a healthy process unhealthy.
func TestHeartbeatFreshRemainsRunning(t *testing.T) {
	dir := t.TempDir()
	hbPath := filepath.Join(dir, "heartbeat.json")
	if f, err := os.Create(hbPath); err != nil {
		t.Fatalf("create heartbeat: %v", err)
	} else {
		f.Close()
	}

	spec := supervise.Spec{
		Command:             []string{"sh", "-c", "sleep 30"},
		Policy:              supervise.PolicyOnFailure,
		HeartbeatPath:       hbPath,
		HeartbeatTTL:        500 * time.Millisecond,
		HealthProbeInterval: 25 * time.Millisecond,
		StartTimeout:        10 * time.Millisecond,
		CrashLoopWindow:     10 * time.Second,
		StopTimeout:         500 * time.Millisecond,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         1 * time.Second,
			Jitter:      0,
			MaxRestarts: 0,
		},
	}

	sv := supervise.New(spec, silentLogger())
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- sv.Run(ctx) }()

	// Keep the heartbeat fresh for ~400ms (many probe ticks), bumping mtime.
	bumpStop := make(chan struct{})
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-bumpStop:
				return
			case <-t.C:
				now := time.Now()
				_ = os.Chtimes(hbPath, now, now)
			}
		}
	}()

	// Confirm it reaches running and stays healthy across several probe ticks.
	if _, ok := waitForStatus(sv, supervise.StatusRunning, 1*time.Second); !ok {
		close(bumpStop)
		_ = sv.Stop(0)
		<-done
		t.Fatal("expected status running with a fresh heartbeat")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if s := sv.Snapshot(); s.Status == supervise.StatusUnhealthy {
			close(bumpStop)
			_ = sv.Stop(0)
			<-done
			t.Fatalf("fresh heartbeat must not be marked unhealthy; got %s", s.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(bumpStop)
	if err := sv.Stop(0); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-done
}

// TestStopTimeoutHonored verifies that Stop(timeout) threads its timeout into
// the SIGTERM→SIGKILL window. The child traps SIGTERM and refuses to die, so
// only SIGKILL (fired after the timeout) can terminate it. With a short stop
// timeout, Run must return quickly — proving the configured timeout, not the
// old hardcoded 10s window, governs escalation.
func TestStopTimeoutHonored(t *testing.T) {
	spec := supervise.Spec{
		// trap+ignore SIGTERM, then sleep — only SIGKILL ends it.
		Command:         []string{"sh", "-c", "trap '' TERM; sleep 30"},
		Policy:          supervise.PolicyOnFailure,
		StartTimeout:    20 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         1 * time.Second,
			Jitter:      0,
			MaxRestarts: 3,
		},
	}

	sv := supervise.New(spec, silentLogger())
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- sv.Run(ctx) }()

	time.Sleep(100 * time.Millisecond) // let it start

	const stopTimeout = 200 * time.Millisecond
	start := time.Now()
	if err := sv.Stop(stopTimeout); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop — SIGKILL escalation may not have fired")
	}

	elapsed := time.Since(start)
	// Must escalate near stopTimeout, not the old hardcoded 10s. Allow slack
	// for scheduling/reap latency but require it to be well under 10s.
	if elapsed < stopTimeout {
		t.Errorf("Stop returned before the SIGTERM window elapsed (%s < %s)", elapsed, stopTimeout)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Stop took %s — timeout not honored (expected ~%s)", elapsed, stopTimeout)
	}

	snap := sv.Snapshot()
	if snap.Status != supervise.StatusStopped {
		t.Errorf("expected stopped status, got %s", snap.Status)
	}
}

// TestStopTerminatesChild verifies that Stop() causes Run() to return.
func TestStopTerminatesChild(t *testing.T) {
	spec := supervise.Spec{
		Command:         []string{"sh", "-c", "sleep 30"},
		Policy:          supervise.PolicyOnFailure,
		StartTimeout:    20 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         1 * time.Second,
			Jitter:      0,
			MaxRestarts: 3,
		},
	}

	sv := supervise.New(spec, silentLogger())
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- sv.Run(ctx) }()

	time.Sleep(100 * time.Millisecond) // let it start
	if err := sv.Stop(2 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after Stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after Stop within timeout")
	}

	snap := sv.Snapshot()
	if snap.Status != supervise.StatusStopped {
		t.Errorf("expected stopped status, got %s", snap.Status)
	}
}

// TestPolicyNever verifies that PolicyNever does not restart on failure.
func TestPolicyNever(t *testing.T) {
	spec := supervise.Spec{
		Command: exitScript(1),
		Policy:  supervise.PolicyNever,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         1 * time.Second,
			Jitter:      0,
			MaxRestarts: 5,
		},
		StartTimeout:    20 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
	}

	sv := supervise.New(spec, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := sv.Run(ctx)
	if err != nil {
		t.Fatalf("PolicyNever should return nil on first exit, got %v", err)
	}

	snap := sv.Snapshot()
	if snap.RestartCount != 0 {
		t.Errorf("PolicyNever: expected 0 restarts, got %d", snap.RestartCount)
	}
}
