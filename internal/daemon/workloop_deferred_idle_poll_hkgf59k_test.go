package daemon_test

// workloop_deferred_idle_poll_hkgf59k_test.go — regression coverage for the
// deferred-only idle-wait stall (hk-gf59k S2-F-S2-2).
//
// Root cause: when all queue items were deferred-for-ledger-dep (no eligible
// items), selectNextQueue returned !ok and the dispatch loop called
// scheduleAwareIdleWait → workloopIdleWait (indefinite block, no schedule armed).
// ReevaluateDeferred never ran again: a deferred chain was permanently stuck
// until an external re-submit sent a wake signal.  Observed as a 7-bead chain
// re-submitted 4× over 7.5h (iter20 queues 019f2b69/019f2bd8/019f2c90/019f2d03).
//
// Fix (hk-gf59k): after ReevaluateDeferred, track hasDeferredItems; if any
// remain in the idle path, use workloopSleep (bounded by workloopPollInterval)
// instead of workloopIdleWait, ensuring the next tick re-checks blocker status.
//
// Test design: a stream group [A(dispatched/in-flight), B(deferred behind A)].
// A is dispatched (not terminal in queue, not eligible) so selectNextQueue
// returns !ok → the idle path is hit. The fake ledger initially reports A as
// open (B stays deferred); on the second LookupStatus call it reports A as
// not-found (A closed externally). Without the fix, workloopIdleWait blocks
// forever — B never un-defers. With the fix, the bounded poll re-evaluates
// on the next tick, the ledger closes A, and B transitions to pending.
//
// Helper prefix: idlePollFixture (per implementer-protocol.md §Helper-prefix).
//
// Bead ref: hk-gf59k. Spec ref: specs/queue-model.md §2.8; specs/execution-model.md §7.4.

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// idlePollFixtureLedger is a fake queue.BeadLedger for the P3 idle-poll test.
// On the first LookupStatus call for blockerID, it returns open (B stays deferred).
// On subsequent calls it returns not-found, simulating an external br close.
type idlePollFixtureLedger struct {
	blockerID   core.BeadID
	blockedID   core.BeadID
	lookupCount atomic.Int64
}

func (f *idlePollFixtureLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if id == f.blockerID {
		n := f.lookupCount.Add(1)
		if n <= 1 {
			return queue.BeadStatusOpen, nil // first call: blocker still open
		}
		return queue.BeadStatusNotFound, nil // subsequent: blocker closed
	}
	return queue.BeadStatusOpen, nil
}

func (f *idlePollFixtureLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return blocker == f.blockerID && blocked == f.blockedID, nil
}

// TestWorkLoop_DeferredOnlyQueue_ReevaluatesWithoutResubmit verifies that when
// all queue items are deferred-for-ledger-dep (no eligible items), the dispatch
// loop re-evaluates them via ReevaluateDeferred within workloopPollInterval
// without requiring a new queue submit.
//
// Group: [A(dispatched — in-flight, not eligible), B(deferred behind A)].
// selectNextQueue returns !ok (neither A nor B is eligible). The idle path must
// use a bounded poll when hasDeferredItems is true so the next tick re-checks
// LookupStatus(A). Without the hk-gf59k fix, the loop blocks indefinitely in
// workloopIdleWait and B never un-defers.
//
// Bead ref: hk-gf59k S2-F-S2-2.
func TestWorkLoop_DeferredOnlyQueue_ReevaluatesWithoutResubmit(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		A core.BeadID = "hk-gf59k-idle-a" // in-flight blocker (dispatched, not eligible)
		B core.BeadID = "hk-gf59k-idle-b" // blocked item (deferred)
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hk-gf59k-idle-poll-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				// A is dispatched (in-flight): not eligible, not terminal.
				// B is deferred behind A: not eligible.
				// selectNextQueue → !ok → idle path triggers.
				Items: []queue.Item{
					{BeadID: A, Status: queue.ItemStatusDispatched},
					{BeadID: B, Status: queue.ItemStatusDeferredForLedgerDep},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &idlePollFixtureLedger{blockerID: A, blockedID: B}

	// BrAdapter: not consulted for dispatch in this test (no pending items reach
	// the claim path). ShowBead may be called for the pre-claim guard if B
	// transitions to pending — return open so it proceeds. ClaimBead fails to
	// keep the test self-contained (no real br CLI).
	brAdapter := &stubBeadLedger{}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        brAdapter,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		QueueLedger:      ledger,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		NoAutoPull:       true,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// workloopPollInterval is 2s; allow several ticks for re-evaluation.
	testTimeout := 20 * time.Second
	testCtx, testCancel := context.WithTimeout(loopCtx, testTimeout)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Poll for B to transition from deferred-for-ledger-dep to pending.
	// This happens once the second LookupStatus(A) call returns not-found
	// and ReevaluateDeferred un-defers B.
	deadline := time.Now().Add(testTimeout - time.Second)
	var finalStatus queue.ItemStatus
	for time.Now().Before(deadline) {
		liveQ := daemon.ExportedQueueStoreOf(deps).Queue()
		if liveQ != nil && len(liveQ.Groups) > 0 && len(liveQ.Groups[0].Items) > 1 {
			finalStatus = liveQ.Groups[0].Items[1].Status // B is index 1
			if finalStatus != queue.ItemStatusDeferredForLedgerDep {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancelLoop()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("runWorkLoop did not exit after context cancel (hk-gf59k)")
	}

	// B must have left deferred-for-ledger-dep. If still deferred, the loop
	// was stuck in workloopIdleWait (the P3 regression).
	if finalStatus == queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("item B status = %q after %s; want pending — "+
			"workloopIdleWait blocked re-evaluation of deferred items "+
			"(hk-gf59k S2-F-S2-2 regression: deferred-only queue never re-evaluated without re-submit)",
			finalStatus, testTimeout)
	}

	// LookupStatus(A) must have been called at least twice:
	// once returning open (B stays deferred), once returning not-found (B un-defers).
	if n := ledger.lookupCount.Load(); n < 2 {
		t.Errorf("LookupStatus(A) called %d time(s); want ≥2 — "+
			"loop must re-evaluate deferred items across multiple bounded-poll ticks (hk-gf59k S2-F-S2-2)", n)
	}
}
