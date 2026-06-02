package daemon_test

// workloop_crossqueue_dedup_hka11re_test.go — Regression test for the
// cross-queue bead dedup guard (hk-a11re).
//
// Bug: When the same bead_id exists as ItemStatusPending in two different named
// queues, the dispatch loop stamps it dispatched in both (on consecutive ticks),
// then calls ClaimBead twice — producing two concurrent implementers for one bead.
//
// Fix: Phase 3 of the dispatch loop now performs a cross-queue check under the
// QueueStore write lock BEFORE stamping the item dispatched.  If the bead_id is
// already ItemStatusDispatched in any OTHER active queue, the duplicate item is
// immediately failed with LastFailureReason="cross_queue_duplicate".
//
// Test shape:
//  1. Install two queues in the QueueStore:
//     - "alpha": stream group active, bead X = dispatched (simulates queue that
//       already won the dispatch race).
//     - "beta":  stream group active, bead X = pending (the duplicate to be guarded).
//  2. Run the workloop (noAutoPull=true) with a context that cancels after a
//     short wall-clock timeout.
//  3. After exit, assert that:
//     (a) bead X in queue "beta" is ItemStatusFailed with
//         LastFailureReason containing "cross_queue_duplicate", and
//     (b) ClaimBead was never called (the guard fired before the claim write).
//
// Helper prefix: crossQueueDedup (hk-a11re; per implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
//
// Spec ref: specs/queue-model.md §6.3 QM-022 (no double dispatch from any source).
// Bead ref: hk-a11re.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// crossQueueDedupLedger is a stub beadLedger that records ClaimBead calls and
// fails the test if any claim is attempted (the cross-queue guard should fire
// before the claim write).
type crossQueueDedupLedger struct {
	mu         sync.Mutex
	claimCount atomic.Int64
	claimed    []core.BeadID
}

func (l *crossQueueDedupLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // noAutoPull=true — never called
}

func (l *crossQueueDedupLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *crossQueueDedupLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	l.claimCount.Add(1)
	l.mu.Lock()
	l.claimed = append(l.claimed, id)
	l.mu.Unlock()
	return nil // succeed so the test doesn't stall if the guard misses
}

func (l *crossQueueDedupLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *crossQueueDedupLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

func (l *crossQueueDedupLedger) claimedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.claimed))
	copy(out, l.claimed)
	return out
}

// crossQueueDedupMakeQueue builds an active stream queue for testing.
// The single item starts with the given status.
func crossQueueDedupMakeQueue(name, queueID string, beadID core.BeadID, itemStatus queue.ItemStatus) *queue.Queue {
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          name,
		SubmittedAt:   now,
		Workers:       1,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: itemStatus},
				},
				CreatedAt: now,
			},
		},
	}
}

// TestWorkLoop_CrossQueueDedup_FailsDuplicateItem is the load-bearing regression
// for hk-a11re.
//
// A bead present in queue "alpha" (status: dispatched) and queue "beta"
// (status: pending) MUST NOT produce two concurrent implementers.  The Phase 3
// cross-queue guard MUST fail the item in "beta" with reason
// "cross_queue_duplicate" before any ClaimBead write occurs.
func TestWorkLoop_CrossQueueDedup_FailsDuplicateItem(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadX   = core.BeadID("hk-a11re-duplicate-bead")
		alphaID = "qid-alpha-crossqueue"
		betaID  = "qid-beta-crossqueue"
	)

	// alpha: bead X already dispatched (simulates the winning queue).
	qAlpha := crossQueueDedupMakeQueue("alpha", alphaID, beadX, queue.ItemStatusDispatched)
	// beta: bead X still pending (the duplicate that must be guarded).
	qBeta := crossQueueDedupMakeQueue("beta", betaID, beadX, queue.ItemStatusPending)

	// Persist both queues to disk so evaluateGroupAdvanceWithOutcome can persist
	// the failed state.  The workloop's Phase 3 and evaluateGroupAdvanceWithOutcome
	// both call queue.Persist internally.
	ctx := t.Context()
	if err := queue.Persist(ctx, projectDir, qAlpha); err != nil {
		t.Fatalf("persist alpha: %v", err)
	}
	if err := queue.Persist(ctx, projectDir, qBeta); err != nil {
		t.Fatalf("persist beta: %v", err)
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueueByName("alpha", qAlpha)
	qs.SetQueueByName("beta", qBeta)

	ledger := &crossQueueDedupLedger{}
	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		NoAutoPull:       true,  // queue-only dispatch; no br-ready fallback
		MaxConcurrent:    1,
	})

	// Run the workloop for long enough to process one dispatch tick, then cancel.
	// The cross-queue guard should fire quickly (no handler is launched), so 2
	// seconds is a generous upper bound.
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_ = daemon.ExportedRunWorkLoop(runCtx, deps)

	// --- Assertions ---

	// (a) bead X in queue "beta" must be ItemStatusFailed with reason
	// "cross_queue_duplicate".  evaluateGroupAdvanceWithOutcome transitions the
	// group to complete-with-failures and the queue to paused-by-failure; the queue
	// persists to disk but the in-memory store may have been cleared.  Reload from
	// disk to get the authoritative state.
	betaFinal, loadErr := queue.Load(ctx, projectDir, "beta")
	if loadErr != nil {
		t.Fatalf("reload beta from disk: %v", loadErr)
	}
	if betaFinal == nil {
		t.Fatal("beta queue not found on disk after workloop — expected paused-by-failure, not unlinked")
	}
	if len(betaFinal.Groups) == 0 || len(betaFinal.Groups[0].Items) == 0 {
		t.Fatal("beta queue has no items after workloop")
	}
	item := betaFinal.Groups[0].Items[0]
	if item.Status != queue.ItemStatusFailed {
		t.Errorf("bead %s in queue beta: got status %q, want %q (cross-queue dedup guard did not fire)",
			beadX, item.Status, queue.ItemStatusFailed)
	}
	if !strings.Contains(item.LastFailureReason, "cross_queue_duplicate") {
		t.Errorf("bead %s in queue beta: LastFailureReason=%q, want it to contain %q",
			beadX, item.LastFailureReason, "cross_queue_duplicate")
	}

	// (b) ClaimBead must NOT have been called — the guard fires before the claim write.
	if n := ledger.claimCount.Load(); n != 0 {
		t.Errorf("ClaimBead called %d time(s); expected 0 (cross-queue guard must fire before claim)", n)
		t.Logf("claimed IDs: %v", ledger.claimedIDs())
	}
}
