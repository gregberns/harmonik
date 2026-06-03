package daemon_test

// workloop_crossqueue_dedup_completed_hkdorz9_test.go — Regression test for the
// completed-bead gap in the cross-queue dedup guard (hk-dorz9).
//
// Bug: The Phase 3 cross-queue check (hk-a11re) matched only ItemStatusDispatched.
// A bead that finished in queue A (ItemStatusCompleted) then submitted to queue B
// was NOT caught and would re-dispatch — double-applying work.
//
// Fix (hk-dorz9): extend the guard to also match ItemStatusCompleted, treating a
// completed bead the same as a dispatched one: fail the duplicate immediately with
// LastFailureReason="cross_queue_duplicate".
//
// Test shape:
//  1. Install two queues in the QueueStore:
//     - "alpha": stream group active, bead X = completed (simulates a bead that
//       already ran successfully in queue A).
//     - "beta":  stream group active, bead X = pending (the duplicate to be guarded).
//  2. Run the workloop (noAutoPull=true) with a context that cancels after a
//     short wall-clock timeout.
//  3. After exit, assert that:
//     (a) bead X in queue "beta" is ItemStatusFailed with
//         LastFailureReason containing "cross_queue_duplicate", and
//     (b) ClaimBead was never called (guard fires before the claim write).
//
// Helper prefix: crossQueueDedupCompleted (hk-dorz9; per implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
//
// Spec ref: specs/queue-model.md §6.3 QM-022 (no double dispatch from any source).
// Bead ref: hk-dorz9.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// TestWorkLoop_CrossQueueDedup_CompletedBead_FailsDuplicateItem is the
// load-bearing regression for hk-dorz9.
//
// A bead that completed in queue "alpha" (status: completed) and is then
// submitted as pending in queue "beta" MUST be caught by the cross-queue dedup
// guard and failed with reason "cross_queue_duplicate" — no ClaimBead call.
func TestWorkLoop_CrossQueueDedup_CompletedBead_FailsDuplicateItem(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadX   = core.BeadID("hk-dorz9-completed-bead")
		alphaID = "qid-alpha-dorz9"
		betaID  = "qid-beta-dorz9"
	)

	// alpha: bead X already completed (simulates a bead that finished in queue A).
	qAlpha := crossQueueDedupMakeQueue("alpha-dorz9", alphaID, beadX, queue.ItemStatusCompleted)
	// beta: bead X still pending (the duplicate that must be guarded).
	qBeta := crossQueueDedupMakeQueue("beta-dorz9", betaID, beadX, queue.ItemStatusPending)

	ctx := t.Context()
	if err := queue.Persist(ctx, projectDir, qAlpha); err != nil {
		t.Fatalf("persist alpha: %v", err)
	}
	if err := queue.Persist(ctx, projectDir, qBeta); err != nil {
		t.Fatalf("persist beta: %v", err)
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueueByName("alpha-dorz9", qAlpha)
	qs.SetQueueByName("beta-dorz9", qBeta)

	ledger := &crossQueueDedupLedger{}
	bus := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		NoAutoPull:       true,
		MaxConcurrent:    1,
	})

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_ = daemon.ExportedRunWorkLoop(runCtx, deps)

	// (a) bead X in queue "beta-dorz9" must be ItemStatusFailed with reason
	// "cross_queue_duplicate".
	betaFinal, loadErr := queue.Load(ctx, projectDir, "beta-dorz9")
	if loadErr != nil {
		t.Fatalf("reload beta from disk: %v", loadErr)
	}
	if betaFinal == nil {
		t.Fatal("beta-dorz9 queue not found on disk — expected paused-by-failure, not unlinked")
	}
	if len(betaFinal.Groups) == 0 || len(betaFinal.Groups[0].Items) == 0 {
		t.Fatal("beta-dorz9 queue has no items after workloop")
	}
	item := betaFinal.Groups[0].Items[0]
	if item.Status != queue.ItemStatusFailed {
		t.Errorf("bead %s in queue beta-dorz9: got status %q, want %q (completed-bead dedup guard did not fire)",
			beadX, item.Status, queue.ItemStatusFailed)
	}
	if !strings.Contains(item.LastFailureReason, "cross_queue_duplicate") {
		t.Errorf("bead %s in queue beta-dorz9: LastFailureReason=%q, want it to contain %q",
			beadX, item.LastFailureReason, "cross_queue_duplicate")
	}

	// (b) ClaimBead must NOT have been called.
	if n := ledger.claimCount.Load(); n != 0 {
		t.Errorf("ClaimBead called %d time(s); expected 0 (completed-bead dedup guard must fire before claim)", n)
		t.Logf("claimed IDs: %v", ledger.claimedIDs())
	}
}
