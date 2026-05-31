package queue_test

// ledgerdep_chain_hkdv8qv_test.go — regression coverage for hk-dv8qv at the
// queue-submit boundary.
//
// hk-dv8qv: the daemon's ledger-dep deferral was inverted — a queue item was
// deferred-for-ledger-dep IFF some OTHER in-queue bead depended on it, and
// eligible IFF nothing in-queue depended on it. Correct semantics: an item is
// deferred IFF at least one bead IT DEPENDS ON (its blockers) is still open,
// and becomes eligible only when ALL its blockers are closed.
//
// The production inversion lived in daemon.brQueueLedger.BlocksEdge (pinned by
// daemon/queueledger_bridge_hkdv8qv_test.go). This test pins the queue-side
// contract: given a fake ledger that reports blocks edges in the contract
// direction — BlocksEdge(blocker, blocked)==true iff blocked depends on blocker
// — a submitted dependency chain defers the dependents and leaves the root
// eligible, and a dependent becomes eligible once its blocker closes.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// chainFixtureLedger is a fake BeadLedger whose blocks edges are keyed in the
// contract direction: edges[[2]{blocker, blocked}] == true means blocker must
// complete before blocked may start (blocked depends on blocker).
type chainFixtureLedger struct {
	statuses map[core.BeadID]queue.BeadStatus
	edges    map[[2]core.BeadID]bool
}

func (f *chainFixtureLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if s, ok := f.statuses[id]; ok {
		return s, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (f *chainFixtureLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

// itemStatusByID scans a queue's first group and returns a bead-ID→status map.
func itemStatusByID(q *queue.Queue) map[core.BeadID]queue.ItemStatus {
	out := make(map[core.BeadID]queue.ItemStatus)
	if q == nil || len(q.Groups) == 0 {
		return out
	}
	for _, it := range q.Groups[0].Items {
		out[it.BeadID] = it.Status
	}
	return out
}

// TestLedgerDepChain_RootEligible_DependentsDeferred_hkdv8qv submits a
// dependency chain R ← A ← B (A depends on R; B depends on A) as a single wave
// group and asserts the root is pending/eligible while the two dependents are
// deferred-for-ledger-dep.
func TestLedgerDepChain_RootEligible_DependentsDeferred_hkdv8qv(t *testing.T) {
	t.Parallel()

	const (
		R core.BeadID = "hk-tigaf.1"  // root: depends on nothing
		A core.BeadID = "hk-tigaf.2"  // depends on R
		B core.BeadID = "hk-tigaf.10" // depends on A
	)

	ledger := &chainFixtureLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			R: queue.BeadStatusOpen,
			A: queue.BeadStatusOpen,
			B: queue.BeadStatusOpen,
		},
		// Contract direction: blocker → blocked.
		edges: map[[2]core.BeadID]bool{
			{R, A}: true, // R blocks A (A depends on R)
			{A, B}: true, // A blocks B (B depends on A)
		},
	}

	projectDir := rpcFixtureTempProjectDir(t)
	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, R, A, B)},
	}

	_, q, _, rpcErr := queue.HandleQueueSubmit(context.Background(), req, ledger, projectDir)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}
	if q == nil {
		t.Fatal("returned *Queue is nil")
	}

	got := itemStatusByID(q)
	if got[R] != queue.ItemStatusPending {
		t.Errorf("root %s status = %q; want %q (root depends on nothing → eligible)",
			R, got[R], queue.ItemStatusPending)
	}
	if got[A] != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("dependent %s status = %q; want %q (A depends on open R → deferred)",
			A, got[A], queue.ItemStatusDeferredForLedgerDep)
	}
	if got[B] != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("dependent %s status = %q; want %q (B depends on open A → deferred)",
			B, got[B], queue.ItemStatusDeferredForLedgerDep)
	}

	// Wave eligibility must surface only the root (non-deferred) item.
	// EligibleItems requires an active group; the submitted queue starts
	// group 0 pending, so flip it active for the eligibility assertion.
	q.Groups[0].Status = queue.GroupStatusActive
	eligible := queue.EligibleItems(&q.Groups[0])
	if len(eligible) != 1 || eligible[0].BeadID != R {
		var ids []core.BeadID
		for _, it := range eligible {
			ids = append(ids, it.BeadID)
		}
		t.Errorf("EligibleItems = %v; want exactly [%s] (only the root is dispatchable)", ids, R)
	}
}

// TestLedgerDepChain_DependentEligibleAfterBlockerCloses_hkdv8qv verifies that
// once the root R closes, its direct dependent A is no longer blocked: a fresh
// submit (with R now closed/absent from the open set) leaves A eligible while B
// — still blocked by the open A — remains deferred.
func TestLedgerDepChain_DependentEligibleAfterBlockerCloses_hkdv8qv(t *testing.T) {
	t.Parallel()

	const (
		R core.BeadID = "hk-tigaf.1"
		A core.BeadID = "hk-tigaf.2"
		B core.BeadID = "hk-tigaf.10"
	)

	// R has closed: it is no longer open, so its blocks edge against A is
	// satisfied. A's only blocker is now resolved; B is still blocked by A.
	ledger := &chainFixtureLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			// R omitted → LookupStatus returns BeadStatusNotFound (closed/absent).
			A: queue.BeadStatusOpen,
			B: queue.BeadStatusOpen,
		},
		// R's edge against A is no longer reported (blocker satisfied); A still
		// blocks B.
		edges: map[[2]core.BeadID]bool{
			{A, B}: true,
		},
	}

	projectDir := rpcFixtureTempProjectDir(t)
	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, A, B)},
	}

	_, q, _, rpcErr := queue.HandleQueueSubmit(context.Background(), req, ledger, projectDir)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}

	got := itemStatusByID(q)
	if got[A] != queue.ItemStatusPending {
		t.Errorf("dependent %s status = %q; want %q (R closed → A eligible)",
			A, got[A], queue.ItemStatusPending)
	}
	if got[B] != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("dependent %s status = %q; want %q (A still open → B deferred)",
			B, got[B], queue.ItemStatusDeferredForLedgerDep)
	}
}
