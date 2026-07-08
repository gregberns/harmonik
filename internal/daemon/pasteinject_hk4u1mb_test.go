package daemon_test

// pasteinject_hk4u1mb_test.go — regression test for hk-4u1mb: reviewer
// diff-scaled budget defeated by heartbeats.
//
// Root cause: a reviewer that heartbeats continuously but never writes
// review.json rode the recentHB-extension path all the way to the flat
// reviewFileHardCeiling (60 min), rendering the per-KLine diff budget
// irrelevant.  For a small diff (budget ≈ 11 min) the reviewer could run for
// the full 60 min ceiling purely because it was alive (heartbeating), not
// because it was progressing toward a verdict.
//
// Fix (hk-4u1mb): introduce a heartbeatExtensionCeiling = min(2×budget,
// hardDeadline).  Heartbeat-based extensions are now bounded by twice the
// diff-scaled budget, not the flat hard ceiling.  A reviewer that heartbeats
// but never writes review.json is killed at heartbeatExtensionCeiling, not at
// the hard ceiling.
//
// Test scenario:
//   - Small diff → low budget (base only, changedLines=-1 → budget = base).
//   - Hard ceiling set to 10× budget (mimics the real 60-min / 6-min-budget ratio).
//   - Heartbeats pump continuously through the whole test.
//   - Kill must fire around 2×budget, NOT at the hard ceiling.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hk4u1mbQuitSender is a minimal quitSender stub.
type hk4u1mbQuitSender struct{ calls atomic.Int64 }

func (q *hk4u1mbQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hk4u1mbKiller records Kill calls.
type hk4u1mbKiller struct{ calls atomic.Int64 }

func (k *hk4u1mbKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

func hk4u1mbHeartbeatEnv() core.EventEnvelope {
	return core.EventEnvelope{
		EventID: core.EventID(uuid.Must(uuid.NewV7())),
		Type:    string(core.EventTypeAgentHeartbeat),
	}
}

// TestQuitOnReviewFile_HeartbeatCeiling_BoundsExtension_hk4u1mb is the
// primary regression test for hk-4u1mb.
//
// A reviewer with a small diff (budget = base) sends continuous heartbeats but
// never writes review.json.  Kill must fire around 2×budget (the heartbeat
// extension ceiling), not at the flat hard ceiling.
func TestQuitOnReviewFile_HeartbeatCeiling_BoundsExtension_hk4u1mb(t *testing.T) {
	const (
		budget       = 20 * time.Millisecond
		grace        = 50 * time.Millisecond  // > budget so HB would extend past budget
		hardCeiling  = 200 * time.Millisecond // 10× budget — far above 2×budget=40ms
		pollInterval = 3 * time.Millisecond
		killDelay    = 2 * time.Millisecond
	)

	// Override timing vars.
	origBase := *daemon.ExportedReviewFileTimeout
	origGrace := *daemon.ExportedReviewerHeartbeatActiveGrace
	origCeil := *daemon.ExportedReviewFileHardCeiling
	origPoll := *daemon.ExportedReviewFilePollInterval
	origKill := *daemon.ExportedNoChangeKillDelay
	origPerK := *daemon.ExportedReviewFilePerKLineBudget
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedReviewFileTimeout = budget
	*daemon.ExportedReviewerHeartbeatActiveGrace = grace
	*daemon.ExportedReviewFileHardCeiling = hardCeiling
	*daemon.ExportedReviewFilePollInterval = pollInterval
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedReviewFilePerKLineBudget = 5 * time.Minute // unused (no real diff dir)
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour          // won't be reached
	defer func() {
		*daemon.ExportedReviewFileTimeout = origBase
		*daemon.ExportedReviewerHeartbeatActiveGrace = origGrace
		*daemon.ExportedReviewFileHardCeiling = origCeil
		*daemon.ExportedReviewFilePollInterval = origPoll
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedReviewFilePerKLineBudget = origPerK
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}()

	wtPath := t.TempDir()
	eventCh := make(chan core.EventEnvelope, 32)

	qs := &hk4u1mbQuitSender{}
	kl := &hk4u1mbKiller{}

	// Pump heartbeats every 5ms for the entire test — far beyond 2×budget.
	// Pre-hk-4u1mb: Kill would fire at ~hardCeiling (200ms).
	// Post-hk-4u1mb: Kill must fire at ~2×budget (40ms).
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
				case eventCh <- hk4u1mbHeartbeatEnv():
				default:
				}
			}
		}
	}()
	defer close(stopHB)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	startedAt := time.Now()
	// No review.json is ever written — reviewer is "alive but not progressing".
	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, eventCh, 0)
	elapsed := time.Since(startedAt)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1, got %d", got)
	}

	// Must fire after the initial budget period.
	if elapsed < budget-pollInterval {
		t.Errorf("Kill fired too early (%v): must wait at least budget (%v)", elapsed, budget)
	}

	// The heartbeat extension ceiling (2×budget), NOT the flat hard ceiling,
	// must be what bounded the run. Assert this on the RECORDED KILL REASON
	// rather than on wall-clock elapsed: under CPU starvation (~16 parallel
	// -race binaries) an elapsed-vs-100ms bound balloons even though the logic
	// is correct, so a wall-clock upper bound flakes. The production code writes
	// a budget sentinel with the kill reason; read it and assert the reason is
	// the heartbeat-ceiling path (hk-4u1mb), scheduler-independent.
	sentinel, err := daemon.ReadReviewerBudgetSentinel(wtPath)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinel(%s): %v", wtPath, err)
	}
	if sentinel == nil {
		t.Fatalf("budget sentinel absent: expected a recorded kill with reason %q (hk-4u1mb)", "heartbeat-ceiling")
	}
	if sentinel.Reason != "heartbeat-ceiling" {
		t.Errorf("kill reason = %q, want %q: the heartbeat extension ceiling "+
			"(2×budget), not the flat hard ceiling, must have bounded the run (hk-4u1mb regression)",
			sentinel.Reason, "heartbeat-ceiling")
	}
}
