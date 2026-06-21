package daemon_test

// pasteinject_hksj6a_test.go — tests for the DOT reviewer watchdog heartbeat
// routing fix (hk-sj6a).
//
// Root cause: RunHeartbeatLoop in dispatchDotAgenticNode was emitting daemon
// heartbeats through the perRunEventTap (tap), which fans out to all
// tap.Subscribe() consumers — including reviewerHBCh used by
// pasteInjectQuitOnReviewFile.  These synthetic daemon heartbeats kept
// lastHeartbeatAt always fresh so recentHB remained true indefinitely after the
// reviewer claude process died, preventing Kill from firing until the hard
// ceiling (60 min) even though the reviewer's session was dead.
//
// Fix: RunHeartbeatLoop now emits to deps.bus directly (not through tap), so
// daemon heartbeats reach the stale watcher but do NOT enter reviewerHBCh.
//
// Tests in this file verify pasteInjectQuitOnReviewFile's heartbeat extension
// logic in isolation: Kill fires once heartbeats stop arriving on eventCh,
// regardless of whether a separate daemon goroutine is emitting to the bus.
//
// Bead: hk-sj6a.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hksj6aSetBudget overrides reviewer-budget timing vars and returns a restore
// function.  postQuitKillGrace is kept long to avoid interfering with the kill
// path under test.
func hksj6aSetBudget(base, grace, ceiling, poll, killDelay time.Duration) func() {
	origBase := *daemon.ExportedReviewFileTimeout
	origGrace := *daemon.ExportedReviewerHeartbeatActiveGrace
	origCeil := *daemon.ExportedReviewFileHardCeiling
	origPoll := *daemon.ExportedReviewFilePollInterval
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	origPerK := *daemon.ExportedReviewFilePerKLineBudget
	*daemon.ExportedReviewFileTimeout = base
	*daemon.ExportedReviewerHeartbeatActiveGrace = grace
	*daemon.ExportedReviewFileHardCeiling = ceiling
	*daemon.ExportedReviewFilePollInterval = poll
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	*daemon.ExportedReviewFilePerKLineBudget = 5 * time.Minute // unused (diff unknown → base)
	return func() {
		*daemon.ExportedReviewFileTimeout = origBase
		*daemon.ExportedReviewerHeartbeatActiveGrace = origGrace
		*daemon.ExportedReviewFileHardCeiling = origCeil
		*daemon.ExportedReviewFilePollInterval = origPoll
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
		*daemon.ExportedReviewFilePerKLineBudget = origPerK
	}
}

// hksj6aQuitSender is a minimal quitSender stub.
type hksj6aQuitSender struct{ calls atomic.Int64 }

func (q *hksj6aQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hksj6aKiller records Kill calls.
type hksj6aKiller struct{ calls atomic.Int64 }

func (k *hksj6aKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// hksj6aHeartbeatEnv builds a minimal agent_heartbeat EventEnvelope.
func hksj6aHeartbeatEnv() core.EventEnvelope {
	return core.EventEnvelope{
		EventID: core.EventID(uuid.Must(uuid.NewV7())),
		Type:    string(core.EventTypeAgentHeartbeat),
	}
}

// TestQuitOnReviewFile_KillsAfterHeartbeatGraceExpires verifies that Kill fires
// once heartbeats stop arriving on eventCh (base budget + grace elapses).
//
// This is the positive case for hk-sj6a: after the fix, daemon heartbeats do
// NOT arrive on reviewerHBCh, so the grace window correctly expires when the
// reviewer claude process dies.
func TestQuitOnReviewFile_KillsAfterHeartbeatGraceExpires(t *testing.T) {
	restore := hksj6aSetBudget(
		20*time.Millisecond,  // base budget (no diff → base applies)
		60*time.Millisecond,  // heartbeatActiveGrace (short for test)
		500*time.Millisecond, // hard ceiling (won't be hit)
		5*time.Millisecond,   // poll interval
		5*time.Millisecond,   // noChangeKillDelay
	)
	defer restore()

	wtPath := t.TempDir()

	// One heartbeat in the channel — simulates the last Claude heartbeat before
	// the reviewer process died (daemon heartbeats, which no longer route through
	// tap after the hk-sj6a fix, are NOT present in this channel).
	eventCh := make(chan core.EventEnvelope, 1)
	eventCh <- hksj6aHeartbeatEnv()

	qs := &hksj6aQuitSender{}
	kl := &hksj6aKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, 0)

	// Kill must fire: base budget elapsed, heartbeat grace expired, no verdict.
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (heartbeat grace expired), got %d", got)
	}
}

// TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget verifies that Kill does
// NOT fire while heartbeats keep arriving on eventCh (reviewer is active).
//
// This models the CORRECT case: when the reviewer claude is genuinely running
// and sending heartbeats, the budget is extended.  After heartbeats stop,
// Kill fires within the grace window.
func TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget(t *testing.T) {
	const (
		grace       = 60 * time.Millisecond
		hardCeiling = 300 * time.Millisecond
	)
	restore := hksj6aSetBudget(
		20*time.Millisecond, // base budget
		grace,               // heartbeatActiveGrace
		hardCeiling,         // hard ceiling — ultimate backstop
		5*time.Millisecond,  // poll
		5*time.Millisecond,  // noChangeKillDelay
	)
	defer restore()

	wtPath := t.TempDir()
	eventCh := make(chan core.EventEnvelope, 8)

	qs := &hksj6aQuitSender{}
	kl := &hksj6aKiller{}

	// Goroutine: pump heartbeats every 15ms for 120ms, then stop.
	// Kill should NOT fire while heartbeats arrive; after they stop it fires
	// once the grace window (60ms) elapses.
	stopHB := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopHB:
				return
			case <-ticker.C:
				select {
				case eventCh <- hksj6aHeartbeatEnv():
				default:
				}
			}
		}
	}()

	// Let heartbeats pump for a short window, then stop them.
	time.AfterFunc(120*time.Millisecond, func() { close(stopHB) })

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, 0)

	elapsed := time.Since(startedAt)
	// Kill must fire — but NOT before the heartbeat pump stopped (~120ms).
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1, got %d", got)
	}
	// Must have run at least until heartbeats stopped (120ms).
	if elapsed < 100*time.Millisecond {
		t.Errorf("Kill fired too early (%v): should have waited for heartbeats to stop", elapsed)
	}
}
