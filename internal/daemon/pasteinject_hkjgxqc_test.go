package daemon_test

// pasteinject_hkjgxqc_test.go — unit test for the launch-suppression hard
// ceiling in pasteInjectQuitOnCommit (hk-jgxqc).
//
// Problem addressed (concurrent-bead wedge): the launch-verification branch
// (hk-3gq0b) resets launchDeadline on every tick where the first agent_heartbeat
// has not yet arrived but the pane reports an active child process (hk-fbydv).
// Under concurrency the per-run heartbeat tap (tapCh) is drained by a competing
// consumer (chanAgentEventSource feeding waitAgentReady), so
// pasteInjectQuitOnCommit NEVER observes a heartbeat, firstHeartbeatSeen stays
// false forever, and the suppression resets the launch deadline UNBOUNDEDLY.
// The goroutine spins in a sleep/reset loop emitting "launch-heartbeat-timeout
// suppressed" every launchWindow and never proceeds to /quit → grace →
// force-kill, so sess.Wait never unblocks and the workflow never advances from
// implement → merge.  Observed live: the suppress line fired 217 times for two
// concurrent wedged runs that HAD already committed.
//
// The fix adds launchSuppressionCeiling: an ABSOLUTE bound (never extended) on
// the active-pane suppression window.  Past the ceiling the suppression is no
// longer permitted; the kill fires even when the pane still reports an active
// child process.
//
// Test matrix:
//   - LaunchSuppressionTerminates_ActivePaneForever (the key regression): pane
//     reports active child process FOREVER, NO heartbeat ever arrives → the kill
//     MUST fire once launchSuppressionCeiling elapses (it does NOT loop forever).
//   - LaunchSuppressionCeiling_PreservesNormalSuppression: a heartbeat arriving
//     within the ceiling clears firstHeartbeatSeen and the launch branch never
//     guillotines (regression guard for the legitimate launch-phase suppression).

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkjgxqcShortCeiling overrides timing vars for the ceiling tests and also sets
// launchSuppressionCeiling.  Returns a restore func; call defer restore().
func hkjgxqcShortCeiling(pollInterval, launchWindow, staleness, totalTimeout, killDelay, ceiling time.Duration) func() {
	restoreBase := hkfbydvShortTimeouts(pollInterval, launchWindow, staleness, totalTimeout, killDelay)
	origCeil := *daemon.ExportedLaunchSuppressionCeiling
	*daemon.ExportedLaunchSuppressionCeiling = ceiling
	return func() {
		*daemon.ExportedLaunchSuppressionCeiling = origCeil
		restoreBase()
	}
}

// TestPasteInjectLaunchSuppressionTerminates_ActivePaneForever is the core
// hk-jgxqc regression: a pane that reports an active child process FOREVER, with
// NO heartbeat ever delivered, must NOT suppress the launch-verification kill
// indefinitely.  Once launchSuppressionCeiling elapses the kill fires so
// sess.Wait unblocks.  Without the ceiling this test would hang until ctx
// timeout and Kill would never be called.
func TestPasteInjectLaunchSuppressionTerminates_ActivePaneForever(t *testing.T) {
	restore := hkjgxqcShortCeiling(
		5*time.Millisecond,   // poll interval
		20*time.Millisecond,  // launch window (fires repeatedly; would reset forever)
		5*time.Second,        // staleness (irrelevant — no heartbeat, launch branch owns it)
		10*time.Second,       // total timeout (must NOT be what frees us — ceiling is shorter)
		5*time.Millisecond,   // kill delay
		150*time.Millisecond, // launch-suppression ceiling (the bound under test)
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(true) // pane ALWAYS reports an active child process — never flips
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope) // non-nil so heartbeatProvided=true; never written

	// Generous ctx: if the ceiling works the call returns in ~150ms; the 5s
	// budget only guards against a true hang (the bug under test).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	// Runs synchronously; MUST return (does not loop forever) thanks to the ceiling.
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)
	elapsed := time.Since(start)

	if ctx.Err() != nil {
		t.Fatalf("call did not return before ctx deadline — launch suppression looped forever (the hk-jgxqc wedge)")
	}
	// Kill must have fired (the noChange kill path ran past the ceiling).
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (ceiling elapsed → kill fires despite active pane), got %d", got)
	}
	// /quit must have been sent as part of the noChange path.
	if got := qs.calls.Load(); got < 1 {
		t.Errorf("SendQuitToLastPane calls: want >=1, got %d", got)
	}
	// noChangeTimeoutCh must be closed.
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after the ceiling-bounded launch kill")
	}
	// Sanity: it returned roughly at the ceiling, not at the 10s total timeout.
	if elapsed > 2*time.Second {
		t.Errorf("kill fired after %v — far past the 150ms ceiling; suppression was not bounded by the ceiling", elapsed)
	}
}

// TestPasteInjectLaunchSuppressionCeiling_PreservesNormalSuppression is the
// regression guard for the legitimate launch-phase suppression: a heartbeat that
// arrives within the ceiling clears firstHeartbeatSeen so the launch-verification
// branch is permanently bypassed and never guillotines the session — even when
// the launch-suppression ceiling is set SHORTER than the launch window.  An
// active pane that keeps progressing (heartbeat seen) must be kept alive by the
// commit-budget extension path, NOT killed by the launch ceiling.
func TestPasteInjectLaunchSuppressionCeiling_PreservesNormalSuppression(t *testing.T) {
	restore := hkjgxqcShortCeiling(
		5*time.Millisecond,  // poll interval
		20*time.Millisecond, // launch window
		5*time.Second,       // staleness (must not fire — pane active, heartbeat seen)
		10*time.Second,      // total timeout (safety backstop; we return early on commit)
		5*time.Millisecond,  // kill delay
		30*time.Millisecond, // ceiling DELIBERATELY short — must NOT bite once a heartbeat is seen
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hkfbydvLivenessQuitSender{}
	qs.alive.Store(true) // pane active throughout
	kl := &hkfbydvKiller{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Deliver a heartbeat immediately so firstHeartbeatSeen becomes true and the
	// launch-verification branch (and thus the launch-suppression ceiling) is
	// permanently bypassed.
	eventCh <- hk7srrdHeartbeatEnv()

	// After ~120ms — well past the 30ms ceiling AND past the 20ms launch window —
	// land a commit so the watchdog exits via the success path.  If the launch
	// ceiling had (incorrectly) fired, the kill would have happened ~30ms in,
	// before this commit, and Kill.calls would be 1.
	go func() {
		time.Sleep(120 * time.Millisecond)
		hk7srrdGitBg(wtPath, "commit", "--allow-empty", "-m", "work landed")
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	// Success path (commit detected) does NOT call killer.Kill synchronously — it
	// schedules a post-quit watchdog gated on postQuitKillGrace (set to 1h by the
	// helper), so no Kill fires within the test.  A launch-ceiling guillotine
	// WOULD have called Kill.  Assert no kill happened: the heartbeat correctly
	// bypassed the launch path despite the short ceiling.
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (heartbeat seen → launch ceiling must NOT fire; commit ended the run), got %d", got)
	}
	// noChangeTimeoutCh must NOT be closed (no noChange kill happened).
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh was closed — launch ceiling fired despite a heartbeat (regression)")
	default:
		// expected: clean commit-detected exit
	}
}
