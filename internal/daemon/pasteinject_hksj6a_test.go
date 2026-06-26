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

// TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget verifies that Kill fires
// at the heartbeat-extension ceiling (2×budget, hk-4u1mb), NOT at the flat hard
// ceiling, even when heartbeats keep arriving continuously.
//
// Prior to hk-4u1mb, continuous heartbeats could drive the deadline all the way
// to reviewFileHardCeiling (300ms here), making the per-KLine diff budget
// irrelevant.  After the fix the effective extension ceiling is 2×budget=40ms, so
// Kill fires around 40ms regardless of whether heartbeats continue beyond that.
func TestQuitOnReviewFile_ContinuousHeartbeatsExtendBudget(t *testing.T) {
	const (
		budget      = 20 * time.Millisecond
		grace       = 60 * time.Millisecond
		hardCeiling = 300 * time.Millisecond
		// heartbeatExtensionCeiling = 2×budget = 40ms (computed inside the function)
	)
	restore := hksj6aSetBudget(
		budget,             // base budget (no diff → changedLines=-1 → base applies)
		grace,              // heartbeatActiveGrace
		hardCeiling,        // hard ceiling — absolute backstop
		5*time.Millisecond, // poll
		5*time.Millisecond, // noChangeKillDelay
	)
	defer restore()

	wtPath := t.TempDir()
	eventCh := make(chan core.EventEnvelope, 8)

	qs := &hksj6aQuitSender{}
	kl := &hksj6aKiller{}

	// Goroutine: pump heartbeats every 5ms indefinitely (well past any budget).
	// Prior to hk-4u1mb this would have kept the reviewer alive until hardCeiling
	// (300ms).  After the fix it is killed at heartbeatExtensionCeiling (~40ms).
	stopHB := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
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
	defer close(stopHB)

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, 0)

	elapsed := time.Since(startedAt)
	// Kill must fire.
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1, got %d", got)
	}
	// Must fire after the initial budget period.
	if elapsed < budget-5*time.Millisecond {
		t.Errorf("Kill fired too early (%v): expected >= budget (%v)", elapsed, budget)
	}
	// Must fire well before the hard ceiling — the heartbeat extension ceiling
	// (2×budget=40ms) prevents riding to the flat 300ms hardCeiling (hk-4u1mb).
	if elapsed > hardCeiling/2 {
		t.Errorf("Kill fired too late (%v): heartbeat ceiling (2×budget=%v) should have applied, not hard ceiling (%v)",
			elapsed, 2*budget, hardCeiling)
	}
}
