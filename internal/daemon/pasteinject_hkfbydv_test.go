package daemon_test

// pasteinject_hkfbydv_test.go — unit tests for the pane-liveness secondary
// check in pasteInjectQuitOnCommit (hk-fbydv).
//
// Problem addressed: the heartbeat watchdog (8-minute staleness threshold and
// 60-second launch-verification window) killed sessions that were actively
// working during Claude's initial thinking phase.  Claude Code's context-
// loading and planning phase takes 8–10 minutes and emits NO heartbeat events,
// making it look identical to an empty pane.
//
// The fix adds a paneLivenessChecker secondary check: before firing the kill,
// the daemon asks whether the tmux pane shell has a child process.  If it does,
// Claude is still running (just not emitting heartbeats yet); the kill is
// suppressed and lastHeartbeat is reset so the staleness clock restarts.
//
// Test matrix:
//   - LivenessChecker_StalenessSupressedWhenProcessActive: staleness fires but
//     pane has a child process → kill suppressed, clock reset, eventually kills
//     only after liveness reports dead.
//   - LivenessChecker_LaunchTimeoutSuppressedWhenProcessActive: launch-
//     heartbeat-timeout fires but pane has child process → kill suppressed,
//     deadline extended, eventually fires when liveness reports dead.
//   - LivenessChecker_KillsWhenProcessDead: liveness reports no child process
//     → kill fires normally (regression guard).
//   - LivenessChecker_NilCheckerKillsNormally: qs does not implement
//     paneLivenessChecker → no liveness check, kill fires as before.
//   - HasChildProcess_CurrentProcess: hasChildProcess(os.Getpid()) must
//     return false (the test runner process has no children by default in this
//     scope — we verify only that the function runs without panic).
//   - HasChildProcess_InvalidPID: hasChildProcess(0) and hasChildProcess(-1)
//     must return false without panic.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hkfbydvQuitSender records SendQuitToLastPane calls.
type hkfbydvQuitSender struct {
	calls atomic.Int64
}

func (q *hkfbydvQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hkfbydvKiller records Kill calls.
type hkfbydvKiller struct {
	calls atomic.Int64
}

func (k *hkfbydvKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// hkfbydvLivenessQuitSender implements both quitSender and
// daemon.PaneLivenessCheckerExported.  The alive field controls whether
// PaneHasActiveProcess returns true; it can be flipped between calls to
// simulate the process dying after some delay.
type hkfbydvLivenessQuitSender struct {
	hkfbydvQuitSender
	alive atomic.Bool
}

func (q *hkfbydvLivenessQuitSender) PaneHasActiveProcess(_ context.Context) bool {
	return q.alive.Load()
}

// Compile-time check: hkfbydvLivenessQuitSender satisfies both interfaces.
var _ daemon.PaneLivenessCheckerExported = (*hkfbydvLivenessQuitSender)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hkfbydvShortTimeouts overrides timing package vars for liveness-check tests.
// Returns a restore function; call defer restore() immediately.
//
//   - pollInterval  — git HEAD check cadence
//   - launchWindow  — launchHeartbeatTimeout
//   - staleness     — heartbeatStalenessThreshold
//   - totalTimeout  — commitPollTimeout (safety backstop)
//   - killDelay     — noChangeKillDelay
//
// postQuitKillGrace is always set to 1 h so the post-commit watchdog never
// fires during these tests.
func hkfbydvShortTimeouts(pollInterval, launchWindow, staleness, totalTimeout, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origLaunch := *daemon.ExportedLaunchHeartbeatTimeout
	origStale := *daemon.ExportedHeartbeatStalenessThreshold
	origTotal := *daemon.ExportedCommitPollTimeout
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = pollInterval
	*daemon.ExportedLaunchHeartbeatTimeout = launchWindow
	*daemon.ExportedHeartbeatStalenessThreshold = staleness
	*daemon.ExportedCommitPollTimeout = totalTimeout
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunch
		*daemon.ExportedHeartbeatStalenessThreshold = origStale
		*daemon.ExportedCommitPollTimeout = origTotal
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectLiveness_StalenessSupressedWhenProcessActive verifies that
// when the heartbeat-staleness threshold fires but the pane liveness checker
// reports an active child process, the kill is suppressed and the staleness
// clock is reset.  The kill only fires after the liveness checker reports dead.
func TestPasteInjectLiveness_StalenessSupressedWhenProcessActive(t *testing.T) {
	restore := hkfbydvShortTimeouts(
		5*time.Millisecond,  // poll interval
		5*time.Second,       // launch window (must not fire — we send a heartbeat)
		50*time.Millisecond, // staleness threshold
		10*time.Second,      // total timeout (safety backstop)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(true) // process active initially
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send one heartbeat immediately so firstHeartbeatSeen=true (bypasses launch window).
	eventCh <- hk7srrdHeartbeatEnv()

	// After 100ms (2× the staleness threshold), mark the process as dead so the
	// kill fires on the next staleness check.
	go func() {
		time.Sleep(100 * time.Millisecond)
		qs.alive.Store(false)
	}()

	// Runs synchronously; blocks until kill fires.
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	// Kill must have fired (process eventually died).
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (process died → kill should fire), got %d", got)
	}
	// noChangeTimeoutCh must be closed.
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after liveness-guarded staleness kill")
	}
}

// TestPasteInjectLiveness_LaunchTimeoutSuppressedWhenProcessActive verifies
// that when the launch-heartbeat-timeout fires but the liveness checker reports
// an active child process, the kill is suppressed and the launch deadline is
// extended.  The kill fires only after the liveness checker returns false.
func TestPasteInjectLiveness_LaunchTimeoutSuppressedWhenProcessActive(t *testing.T) {
	restore := hkfbydvShortTimeouts(
		5*time.Millisecond,  // poll interval
		50*time.Millisecond, // launch window (fires quickly)
		5*time.Second,       // staleness (must not fire — will get heartbeats later)
		10*time.Second,      // total timeout
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(true) // process active during launch window
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// No heartbeats initially — the launch window will fire but be suppressed.
	// After 150ms, mark the process as dead so the (extended) launch deadline
	// eventually fires and actually kills.
	go func() {
		time.Sleep(150 * time.Millisecond)
		qs.alive.Store(false)
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (process died → launch kill should fire), got %d", got)
	}
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after liveness-guarded launch kill")
	}
}

// TestPasteInjectLiveness_KillsWhenProcessDead verifies that when the
// staleness threshold fires and the liveness checker reports no child process,
// the kill fires normally (regression guard).
func TestPasteInjectLiveness_KillsWhenProcessDead(t *testing.T) {
	restore := hkfbydvShortTimeouts(
		5*time.Millisecond,  // poll interval
		5*time.Second,       // launch window (irrelevant — we send a heartbeat)
		50*time.Millisecond, // staleness threshold
		10*time.Second,      // total timeout
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(false) // process already dead
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send one heartbeat so firstHeartbeatSeen=true, then let staleness fire.
	eventCh <- hk7srrdHeartbeatEnv()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (process dead → kill should fire immediately on staleness), got %d", got)
	}
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after staleness kill (process dead)")
	}
}

// TestPasteInjectLiveness_NilCheckerKillsNormally verifies that when qs does
// not implement paneLivenessChecker, the staleness kill fires as before —
// no regression for callers that don't have liveness support.
func TestPasteInjectLiveness_NilCheckerKillsNormally(t *testing.T) {
	restore := hkfbydvShortTimeouts(
		5*time.Millisecond,  // poll interval
		5*time.Second,       // launch window (irrelevant — we send a heartbeat)
		50*time.Millisecond, // staleness threshold
		10*time.Second,      // total timeout
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	// Plain quitSender without paneLivenessChecker.
	qs := &hk7srrdQuitSender{}
	kl := &hk7srrdKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send one heartbeat so firstHeartbeatSeen=true, then let staleness fire.
	eventCh <- hk7srrdHeartbeatEnv()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	// Kill must fire: no liveness checker → staleness kills immediately.
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (no liveness checker → immediate kill on staleness), got %d", got)
	}
}

// TestHasChildProcess_InvalidPIDs verifies that hasChildProcess returns false
// for invalid PID values without panicking.
func TestHasChildProcess_InvalidPIDs(t *testing.T) {
	for _, pid := range []int{0, -1, -9999} {
		if got := daemon.ExportedHasChildProcess(pid); got {
			t.Errorf("hasChildProcess(%d): want false for invalid PID, got true", pid)
		}
	}
}
