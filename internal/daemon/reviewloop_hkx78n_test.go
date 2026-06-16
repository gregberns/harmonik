package daemon_test

// reviewloop_hkx78n_test.go — unit tests for the heartbeat-aware implementer-
// wait budget in the review-loop (hk-x78n).
//
// Problem addressed: runReviewLoop passed nil for the eventCh argument of
// pasteInjectQuitOnCommit, disabling heartbeat-extension of the per-progress
// commit budget (commitPollTimeout).  An implementer that legitimately ran for
// >30 min while steadily emitting agent_heartbeat events was killed at the flat
// 30-min wall-clock deadline — silently, with its work discarded.
//
// Fix: runReviewLoop now calls implTap.Subscribe() and passes the resulting
// channel as eventCh to pasteInjectQuitOnCommit.  The fan-out tap delivers a
// copy of every heartbeat to the watchdog independently of any other subscriber
// (implTapCh for waitAgentReady, postReadyCh for the hang-detector).
//
// Test matrix:
//   - HeartbeatExtendsBudget: events emitted via a perRunEventTap fan-out to a
//     Subscribe() channel; those heartbeats reach pasteInjectQuitOnCommit and
//     extend the commit budget so no kill fires within the observation window.
//   - StaleHeartbeatKills: when heartbeats stop, the staleness kill fires,
//     proving the bounded-hang property (no infinite wait).
//
// Both tests route heartbeats through ExportedPerRunEventTap.ExportedEmit →
// fan-out → ExportedSubscribe channel, matching the reviewloop wiring exactly.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkx78nSetTimings overrides timing package vars for hk-x78n tests and returns
// a restore function.  postQuitKillGrace is pinned to 1 h so the post-commit
// watchdog never fires during the tests.
func hkx78nSetTimings(pollInterval, budget, hardCeiling, staleness, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origBudget := *daemon.ExportedCommitPollTimeout
	origHard := *daemon.ExportedCommitHardCeiling
	origStale := *daemon.ExportedHeartbeatStalenessThreshold
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = pollInterval
	*daemon.ExportedCommitPollTimeout = budget
	*daemon.ExportedCommitHardCeiling = hardCeiling
	*daemon.ExportedHeartbeatStalenessThreshold = staleness
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedCommitPollTimeout = origBudget
		*daemon.ExportedCommitHardCeiling = origHard
		*daemon.ExportedHeartbeatStalenessThreshold = origStale
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// TestReviewLoopImplementerWait_HeartbeatExtendsBudget verifies that heartbeats
// routed through a perRunEventTap subscription — exactly as runReviewLoop wires
// them after hk-x78n — prevent pasteInjectQuitOnCommit from killing a
// progressing implementer within a window that is several times the per-
// progress budget (hk-x78n).
//
// BEFORE the fix: eventCh was nil; heartbeats from the tap never reached the
// watchdog; totalDeadline fired at ~budget regardless of progress.
// AFTER the fix: tap.Subscribe() feeds eventCh; each heartbeat extends
// totalDeadline by another budget window so no kill fires.
func TestReviewLoopImplementerWait_HeartbeatExtendsBudget(t *testing.T) {
	restore := hkx78nSetTimings(
		5*time.Millisecond,  // poll interval
		40*time.Millisecond, // per-progress budget (short; extended by heartbeats)
		10*time.Second,      // hard ceiling (far away — must not fire in window)
		40*time.Millisecond, // staleness (kept in step with budget)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)

	// Create a perRunEventTap mirroring reviewloop.go.
	// newPerRunEventTap returns (tap, firstSubscriberCh); we discard the first
	// channel (it simulates waitAgentReady's consumer) and call Subscribe() for
	// the pasteInjectQuitOnCommit watchdog — identical to the hk-x78n fix.
	tap, _ := daemon.ExportedNewPerRunEventTap(core.RunID{})
	implHBCh := tap.ExportedSubscribe()

	qs := &hk9vp51LivenessQuitSender{}
	qs.alive.Store(true)
	kl := &hk9vp51Killer{}
	noChangeCh := make(chan struct{}, 1)

	// Observation window = 300 ms ≈ 7.5× the per-progress budget.  Under the old
	// nil-eventCh behaviour the kill would fire by ~40 ms.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Emit heartbeats every 15 ms (well inside the 40 ms budget) via the tap,
	// simulating agent_heartbeat events flowing through the daemon bus.
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = tap.ExportedEmit(context.Background(), core.EventTypeAgentHeartbeat)
			}
		}
	}()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, implHBCh)

	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (heartbeating implementer must NOT be killed at flat budget), got %d", got)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh was closed — implementer was killed despite active heartbeats (hk-x78n not wired)")
	default:
	}
}

// TestReviewLoopImplementerWait_StaleHeartbeatKills verifies that when
// heartbeats stop, pasteInjectQuitOnCommit kills the implementer session via
// the staleness path (hk-x78n bounded-hang property).
//
// An implementer that goes dark (no heartbeats, pane inactive) must still be
// reaped within heartbeatStalenessThreshold so the slot is freed.
func TestReviewLoopImplementerWait_StaleHeartbeatKills(t *testing.T) {
	restore := hkx78nSetTimings(
		5*time.Millisecond,  // poll interval
		2*time.Second,       // per-progress budget (must NOT fire before staleness)
		10*time.Second,      // hard ceiling (must not fire during test)
		50*time.Millisecond, // staleness threshold (kill fires after 50 ms of silence)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)

	tap, _ := daemon.ExportedNewPerRunEventTap(core.RunID{})
	implHBCh := tap.ExportedSubscribe()

	qs := &hk9vp51LivenessQuitSender{}
	qs.alive.Store(false) // pane not active → staleness kill path fires promptly
	kl := &hk9vp51Killer{}
	noChangeCh := make(chan struct{}, 1)

	// No heartbeats emitted — staleness kill must fire within staleness + kill delay.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, implHBCh)

	if got := kl.calls.Load(); got == 0 {
		t.Error("Kill calls: want ≥1 (stale implementer must be killed), got 0")
	}
}
