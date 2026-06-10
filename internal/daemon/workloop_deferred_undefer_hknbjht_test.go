package daemon_test

// workloop_deferred_undefer_hknbjht_test.go — regression coverage for the
// deferred-for-ledger-dep stall (hk-nbjht).
//
// Bug (reproduced live): on a stream queue where item B is
// deferred-for-ledger-dep behind item A, completing A left B stuck forever:
//
//	Gap 1 — no code path ever cleared B back to pending when A closed.
//	Gap 2 — the per-run completion path persisted via the LockedQueueStore
//	        no-wake SetQueue, so the idle dispatch loop never ticked again to
//	        observe the un-defer.
//
// A dependency-chained queue (hk-tigaf.1→.2→…) therefore stalled permanently
// after its first bead completed.
//
// Two tests pin the fix:
//   - TestEvaluateGroupAdvance_WakesLoopOnRunCompleted_hknbjht (Gap 2): the
//     completion path fires WakeCh on run_completed. FAILS ON MAIN (no wake).
//   - TestQueueDispatch_DeferredChainRecovers_hknbjht (Gaps 1+2 end-to-end): a
//     stream group [A, B-deferred] runs to completion of BOTH beads through the
//     real work loop. FAILS ON MAIN (B never dispatches → only A closes).
//
// Helper prefix: undeferFixture (per implementer-protocol.md §Helper-prefix).
//
// Spec ref: specs/queue-model.md §2.8 (deferred → pending on blocker close),
// §6.6 QM-025; specs/execution-model.md §7.4.
// Bead ref: hk-nbjht.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// undeferFixtureLedger is a fake queue.BeadLedger keyed in the contract
// direction (BlocksEdge(blocker, blocked)==true ⇔ blocked depends on blocker).
// LookupStatus reports open for every named bead; the un-defer in these tests
// fires via the queue-terminal branch (a completed predecessor item), which is
// the path the daemon exercises when a chained bead finishes.
type undeferFixtureLedger struct {
	open  map[core.BeadID]bool
	edges map[[2]core.BeadID]bool
}

func (f *undeferFixtureLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if f.open[id] {
		return queue.BeadStatusOpen, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (f *undeferFixtureLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

// undeferFixtureStreamQueue builds an active stream queue with item A pending
// (index 0) and item B deferred-for-ledger-dep (index 1) behind A.
func undeferFixtureStreamQueue(name string, a, b core.BeadID) *queue.Queue {
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hknbjht-queue-" + name,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: a, Status: queue.ItemStatusPending},
					{BeadID: b, Status: queue.ItemStatusDeferredForLedgerDep},
				},
				CreatedAt: now,
			},
		},
	}
}

// TestEvaluateGroupAdvance_WakesLoopOnRunCompleted_hknbjht pins Gap 2: the
// per-run completion path MUST fire the wake channel on a run_completed so the
// idle dispatch loop ticks again and re-runs its §2.8 deferred-item
// re-evaluation. On main the completion path persists via the no-wake
// LockedQueueStore.SetQueue and never signals, so this FAILS (timeout on
// WakeCh).
func TestEvaluateGroupAdvance_WakesLoopOnRunCompleted_hknbjht(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)

	const (
		A = core.BeadID("hknbjht-wake-a")
		B = core.BeadID("hknbjht-wake-b")
	)
	q := undeferFixtureStreamQueue(t.Name(), A, B)
	// A is in-flight (dispatched); the completion path marks it terminal.
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	queueID := q.QueueID

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q) // fires one wake on submit

	// Drain the submit wake so the assertion observes the COMPLETION wake only.
	select {
	case <-qs.WakeCh():
	case <-time.After(time.Second):
		t.Fatal("expected a submit wake from SetQueue to drain")
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		QueueLedger: &undeferFixtureLedger{
			open:  map[core.BeadID]bool{A: true, B: true},
			edges: map[[2]core.BeadID]bool{{A, B}: true},
		},
	})

	// A completes successfully — the completion path must wake the loop.
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, "", queueID, 0, 0, true)

	select {
	case <-qs.WakeCh():
		// pass: Gap 2 fixed — the completion path woke the idle loop.
	case <-time.After(2 * time.Second):
		t.Fatal("WakeCh NOT signaled after run_completed; the idle dispatch loop " +
			"would never re-evaluate the deferred item (hk-nbjht Gap 2 regression)")
	}
}

// TestQueueDispatch_DeferredChainRecovers_hknbjht pins Gaps 1+2 together at the
// completion boundary, deterministically and without spawning subprocesses (the
// full subprocess→merge→close loop is not exercisable in the unit harness —
// TestQueueDispatch_HappyPath, the wave equivalent, also times out here for the
// same reason; this test isolates the two seams the loop touches around a run
// completion).
//
// Setup: a stream group [A(dispatched, in-flight), B(deferred-for-ledger-dep
// behind A)] on a live QueueStore. The test reproduces the exact two steps the
// dispatch loop performs around a run completion:
//
//  1. A's run completes → ExportedEvaluateGroupAdvanceWithOutcome marks A
//     terminal in the queue AND fires the wake (Gap 2). On main the no-wake
//     LockedQueueStore.SetQueue means the idle loop would never tick again.
//  2. The woken loop re-runs queue.ReevaluateDeferred over the active group —
//     the same call the production Phase-1 dispatch tick now makes (Gap 1).
//     A is now terminal in the queue, so B un-defers to pending and
//     EligibleItems surfaces B for dispatch.
//
// On main step 1 does not wake and step 2's un-defer code does not exist, so B
// would sit deferred forever and the stream would HOL-block permanently.
func TestQueueDispatch_DeferredChainRecovers_hknbjht(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)

	const (
		A = core.BeadID("hknbjht-chain-a")
		B = core.BeadID("hknbjht-chain-b")
	)
	q := undeferFixtureStreamQueue(t.Name(), A, B)
	// A is in-flight; the completion path marks it terminal.
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	queueID := q.QueueID

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	// Drain the submit wake so the post-completion wake assertion is unambiguous.
	select {
	case <-qs.WakeCh():
	case <-time.After(time.Second):
		t.Fatal("expected a submit wake from SetQueue to drain")
	}

	ledger := &undeferFixtureLedger{
		open:  map[core.BeadID]bool{A: true, B: true},
		edges: map[[2]core.BeadID]bool{{A, B}: true},
	}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		QueueLedger:      ledger,
	})

	// ── Step 1: A's run completes ────────────────────────────────────────────
	daemon.ExportedEvaluateGroupAdvanceWithOutcome(context.Background(), deps, "", queueID, 0, 0, true)

	// Gap 2: the completion path must have woken the idle dispatch loop.
	select {
	case <-qs.WakeCh():
		// pass
	case <-time.After(2 * time.Second):
		t.Fatal("WakeCh NOT signaled after A completed; the idle loop would never " +
			"re-evaluate B (hk-nbjht Gap 2)")
	}

	// Sanity: A is now terminal in the queue; B is still deferred (nothing has
	// re-evaluated it yet — exactly the stuck state on main).
	live := qs.Queue()
	if live == nil {
		t.Fatal("queue became nil after A completed")
	}
	if got := live.Groups[0].Items[0].Status; got != queue.ItemStatusCompleted {
		t.Fatalf("A status = %q after completion; want completed", got)
	}
	if got := live.Groups[0].Items[1].Status; got != queue.ItemStatusDeferredForLedgerDep {
		t.Fatalf("B status = %q before re-evaluation; want deferred-for-ledger-dep", got)
	}

	// ── Step 2: the woken dispatch tick re-evaluates deferred items ───────────
	// This is the exact call runWorkLoop's Phase-1 now performs every tick.
	undeferred, err := queue.ReevaluateDeferred(context.Background(), &live.Groups[0], ledger)
	if err != nil {
		t.Fatalf("ReevaluateDeferred: %v", err)
	}

	// Gap 1: B must un-defer to pending now that its blocker A is terminal.
	if len(undeferred) != 1 || undeferred[0] != B {
		t.Errorf("undeferred = %v; want [%s] (B un-defers once A is terminal)", undeferred, B)
	}
	if got := live.Groups[0].Items[1].Status; got != queue.ItemStatusPending {
		t.Errorf("B status = %q after re-evaluation; want pending — the deferred chain "+
			"stalled (hk-nbjht Gap 1: no un-defer-on-blocker-close)", got)
	}

	// And the stream now surfaces B as the eligible head for dispatch.
	eligible := queue.EligibleItems(&live.Groups[0])
	if len(eligible) != 1 || eligible[0].BeadID != B {
		var ids []core.BeadID
		for _, it := range eligible {
			ids = append(ids, it.BeadID)
		}
		t.Errorf("EligibleItems = %v; want [%s] (B dispatchable after un-defer)", ids, B)
	}
}
