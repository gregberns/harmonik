package daemon_test

// pasteinject_hk1too_test.go — unit tests for the stale-spawn dead-pane
// fast-fail in pasteInjectQuitOnCommit (hk-1too).
//
// Problem addressed: when a daemon restarts while beads are mid-spawn, the
// prior daemon's KillAllWindows kills the window the new daemon spawned for the
// same bead (same deterministic window name: session:bead-X/i1).  The paste
// then fails with "can't find pane: %N", briefDelivered closes immediately, and
// — without the fix — the watchdog waits 180 s for launchHeartbeatTimeout before
// firing noChangePath.
//
// The fix detects the dead pane immediately after brief delivery (when all of:
// briefDelivered fired, eventCh is non-nil, PaneHasActiveProcess is false) and
// fires noChangePath right away so the bead is reopened in ~killDelay seconds
// rather than ~180 s.
//
// Test matrix:
//   - StalePane_DeadPaneAtBriefDelivery_FiresNoChangeImmediately: briefDelivered
//     fires with pane dead → noChangeTimeoutCh closed quickly (< launchWindow).
//   - StalePane_LivePaneAtBriefDelivery_NoImmediateKill: briefDelivered fires
//     but pane is alive → immediate kill NOT triggered; normal flow continues.
//   - StalePane_NilEventCh_SkipsCheck: eventCh nil → dead-pane check skipped;
//     function waits for wall-clock timeout, not immediate kill.
//   - StalePane_BriefDeliveredTimeout_SkipsCheck: briefDelivered never fires
//     (times out) → check skipped even when pane is dead.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hk1tooQuitKillSender implements quitSender, sessionKiller, and
// daemon.PaneLivenessCheckerExported.  alive controls PaneHasActiveProcess.
type hk1tooQuitKillSender struct {
	quitCalls int64
	killCalls int64
	alive     bool
}

func (s *hk1tooQuitKillSender) SendQuitToLastPane(_ context.Context) error {
	s.quitCalls++
	return nil
}

func (s *hk1tooQuitKillSender) Kill(_ context.Context) error {
	s.killCalls++
	return nil
}

func (s *hk1tooQuitKillSender) PaneHasActiveProcess(_ context.Context) bool {
	return s.alive
}

var _ daemon.PaneLivenessCheckerExported = (*hk1tooQuitKillSender)(nil)

// hk1tooShortTimeouts sets timing vars so launchHeartbeatTimeout is long enough
// to distinguish "fired immediately (stale-pane path)" from "fired after launch
// window (normal path)".  Returns a restore function.
func hk1tooShortTimeouts(briefTimeout, launchWindow, killDelay, totalTimeout time.Duration) func() {
	origBrief := *daemon.ExportedBriefDeliveredTimeout
	origLaunch := *daemon.ExportedLaunchHeartbeatTimeout
	origKill := *daemon.ExportedNoChangeKillDelay
	origTotal := *daemon.ExportedCommitPollTimeout
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedBriefDeliveredTimeout = briefTimeout
	*daemon.ExportedLaunchHeartbeatTimeout = launchWindow
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedCommitPollTimeout = totalTimeout
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedBriefDeliveredTimeout = origBrief
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunch
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedCommitPollTimeout = origTotal
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestStalePane_DeadPaneAtBriefDelivery_FiresNoChangeImmediately verifies that
// when briefDelivered fires with the pane dead and eventCh non-nil, noChange is
// fired immediately (well before launchHeartbeatTimeout).
func TestStalePane_DeadPaneAtBriefDelivery_FiresNoChangeImmediately(t *testing.T) {
	// launchWindow = 500ms; if the stale-pane path fires, it should complete in
	// killDelay (10ms) + small overhead — well under 200ms. If the normal path
	// fires (bug not fixed), it would take ~500ms.
	restore := hk1tooShortTimeouts(
		500*time.Millisecond, // brief timeout (long; brief will fire before this)
		500*time.Millisecond, // launch window (long; stale path must fire first)
		10*time.Millisecond,  // kill delay
		10*time.Second,       // total timeout (safety backstop)
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk1tooQuitKillSender{alive: false} // pane is DEAD
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4) // non-nil: enables check
	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Close briefDelivered immediately (simulates paste-inject goroutine exiting
	// after "can't find pane" failure).
	close(briefDelivered)

	start := time.Now()
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, qs, wtPath, headSHA, noChangeCh, briefDelivered, eventCh)
	elapsed := time.Since(start)

	// noChangeTimeoutCh must be closed (stale-pane path fired).
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed: stale-pane path did not fire")
	}
	if qs.killCalls != 1 {
		t.Errorf("Kill calls: want 1, got %d", qs.killCalls)
	}
	// Must complete well under the launch window (500ms); stale path fires in
	// ~killDelay (10ms). Allow 300ms for scheduling jitter.
	if elapsed > 300*time.Millisecond {
		t.Errorf("elapsed %v; stale-pane path should fire in ~killDelay (10ms), not after launch window (500ms)", elapsed)
	}
}

// TestStalePane_LivePaneAtBriefDelivery_NoImmediateKill verifies that when
// briefDelivered fires but the pane is ALIVE, the immediate kill is NOT
// triggered.  The test cancels ctx after a brief observation window to avoid
// waiting through suppression loops.
func TestStalePane_LivePaneAtBriefDelivery_NoImmediateKill(t *testing.T) {
	restore := hk1tooShortTimeouts(
		500*time.Millisecond, // brief timeout
		10*time.Second,       // launch window (large; stale path fires in <50ms if broken)
		10*time.Millisecond,  // kill delay
		10*time.Second,       // total timeout
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk1tooQuitKillSender{alive: true} // pane is ALIVE
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4) // non-nil: enables check
	briefDelivered := make(chan struct{})

	// Cancel ctx after 150ms — enough time for the stale path to fire (10ms)
	// if it incorrectly triggers, but not long enough for the launch window (10s).
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	close(briefDelivered)

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, qs, wtPath, headSHA, noChangeCh, briefDelivered, eventCh)
	}()
	<-done

	// Immediate kill must NOT have fired — pane was alive.
	if qs.killCalls != 0 {
		t.Errorf("Kill calls: want 0 (live pane, no immediate kill), got %d", qs.killCalls)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh closed: immediate kill fired on a LIVE pane (false positive)")
	default:
	}
}

// TestStalePane_NilEventCh_SkipsCheck verifies that when eventCh is nil, the
// stale-pane check is skipped even if the pane is dead.  The function falls back
// to the wall-clock commitPollTimeout.
func TestStalePane_NilEventCh_SkipsCheck(t *testing.T) {
	// launchWindow irrelevant (nil eventCh → launch check inactive).
	// totalTimeout = 50ms governs; stale path fires in ~10ms if check runs.
	restore := hk1tooShortTimeouts(
		500*time.Millisecond, // brief timeout
		10*time.Millisecond,  // launch window (irrelevant — nil eventCh)
		10*time.Millisecond,  // kill delay
		50*time.Millisecond,  // total timeout (wall-clock governs)
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk1tooQuitKillSender{alive: false} // pane dead — but check must be skipped
	noChangeCh := make(chan struct{})
	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	close(briefDelivered)

	start := time.Now()
	// nil eventCh → stale-pane check must NOT fire.
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, qs, wtPath, headSHA, noChangeCh, briefDelivered, nil)
	elapsed := time.Since(start)

	// Must have waited at least the total timeout (50ms) — confirms check was skipped.
	if elapsed < 40*time.Millisecond {
		t.Errorf("elapsed %v < 40ms; stale-pane check fired despite nil eventCh", elapsed)
	}
	select {
	case <-noChangeCh:
		// expected (wall-clock fired)
	default:
		t.Fatal("noChangeTimeoutCh not closed after wall-clock timeout")
	}
}

// TestStalePane_BriefDeliveredTimeout_SkipsCheck verifies that when
// briefDelivered times out (never fires), the stale-pane check is skipped even
// if the pane is dead.
func TestStalePane_BriefDeliveredTimeout_SkipsCheck(t *testing.T) {
	// briefTimeout = 20ms (fires before launchWindow).
	// stale path fires in ~10ms if check runs — would be < 25ms total.
	// Normal path (brief timeout + launch window) = 20 + 80 = 100ms.
	restore := hk1tooShortTimeouts(
		20*time.Millisecond,  // brief timeout (fires; briefDelivered never closes)
		80*time.Millisecond,  // launch window (fires after brief timeout elapses)
		10*time.Millisecond,  // kill delay
		10*time.Second,       // total timeout
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk1tooQuitKillSender{alive: false} // pane dead — but check must be skipped
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)
	briefDelivered := make(chan struct{}) // NEVER closed

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, qs, wtPath, headSHA, noChangeCh, briefDelivered, eventCh)
	elapsed := time.Since(start)

	// Must have waited at least briefTimeout + launchWindow (100ms) — confirms
	// check was skipped (not fired at ~killDelay = 10ms).
	if elapsed < 80*time.Millisecond {
		t.Errorf("elapsed %v < 80ms; stale-pane check fired despite briefDelivered timeout (not fired)", elapsed)
	}
}
