package queue_test

// reevaluate_deferred_hknbjht_test.go — unit coverage for the §2.8 un-defer
// pass (hk-nbjht).
//
// hk-nbjht Gap 1: ItemStatusDeferredForLedgerDep was only ever SET (at
// submit/append time per QM-025); no code path ever cleared it back to pending
// when the blocker closed. queue.ReevaluateDeferred is the un-defer counterpart
// to the submit-time deferral: both consult BeadLedger.BlocksEdge, so the
// un-defer condition is the exact inverse of the deferral condition.
//
// These tests pin the un-defer contract independently of the daemon dispatch
// loop:
//   - A deferred item un-defers once its blocker is terminal in the queue.
//   - A deferred item un-defers once its blocker is closed in the ledger
//     (LookupStatus no longer open), even if the structural blocks edge persists.
//   - A deferred item stays deferred while ANY blocker is still open.
//   - A chained queue un-defers one link at a time (root → A → B).

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// reevalFixtureLedger is a fake BeadLedger keyed in the contract direction:
// edges[[2]{blocker, blocked}] == true means blocker must complete before
// blocked may start. statuses drives LookupStatus; an absent ID reports
// not-found (treated as closed for un-defer purposes).
type reevalFixtureLedger struct {
	statuses map[core.BeadID]queue.BeadStatus
	edges    map[[2]core.BeadID]bool
}

func (f *reevalFixtureLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if s, ok := f.statuses[id]; ok {
		return s, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (f *reevalFixtureLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

// reevalStreamGroup builds an active stream group from (beadID, status) pairs in
// list order.
func reevalStreamGroup(items ...queue.Item) *queue.Group {
	return &queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusActive,
		Items:      items,
	}
}

// TestReevaluateDeferred_UndefersWhenBlockerTerminalInQueue: B is deferred
// behind A; once A is completed (terminal in the queue), B un-defers to pending
// even though the ledger still reports A as open (the structural blocks edge
// persists; the queue-terminal branch satisfies the blocker).
func TestReevaluateDeferred_UndefersWhenBlockerTerminalInQueue(t *testing.T) {
	t.Parallel()

	const (
		A core.BeadID = "hk-nbjht.A"
		B core.BeadID = "hk-nbjht.B"
	)
	ledger := &reevalFixtureLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			A: queue.BeadStatusOpen, // still "open" in the ledger
			B: queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{A, B}: true, // A blocks B (B depends on A)
		},
	}
	g := reevalStreamGroup(
		queue.Item{BeadID: A, Status: queue.ItemStatusCompleted}, // A finished in-queue
		queue.Item{BeadID: B, Status: queue.ItemStatusDeferredForLedgerDep},
	)

	undeferred, err := queue.ReevaluateDeferred(context.Background(), g, ledger)
	if err != nil {
		t.Fatalf("ReevaluateDeferred: unexpected error: %v", err)
	}
	if g.Items[1].Status != queue.ItemStatusPending {
		t.Errorf("B status = %q after blocker completed in queue; want %q",
			g.Items[1].Status, queue.ItemStatusPending)
	}
	if len(undeferred) != 1 || undeferred[0] != B {
		t.Errorf("undeferred = %v; want [%s]", undeferred, B)
	}
}

// TestReevaluateDeferred_UndefersWhenBlockerClosedInLedger: the blocker is still
// a non-terminal item in the queue (e.g. an externally-closed bead the daemon
// has not yet marked terminal), but LookupStatus reports it closed/not-found —
// so B un-defers via the ledger branch.
func TestReevaluateDeferred_UndefersWhenBlockerClosedInLedger(t *testing.T) {
	t.Parallel()

	const (
		A core.BeadID = "hk-nbjht.A"
		B core.BeadID = "hk-nbjht.B"
	)
	ledger := &reevalFixtureLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			// A omitted → LookupStatus = not-found (closed/absent).
			B: queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{A, B}: true, // structural edge persists even though A is closed
		},
	}
	g := reevalStreamGroup(
		queue.Item{BeadID: A, Status: queue.ItemStatusPending}, // not terminal in-queue
		queue.Item{BeadID: B, Status: queue.ItemStatusDeferredForLedgerDep},
	)

	undeferred, err := queue.ReevaluateDeferred(context.Background(), g, ledger)
	if err != nil {
		t.Fatalf("ReevaluateDeferred: unexpected error: %v", err)
	}
	if g.Items[1].Status != queue.ItemStatusPending {
		t.Errorf("B status = %q after blocker closed in ledger; want %q",
			g.Items[1].Status, queue.ItemStatusPending)
	}
	if len(undeferred) != 1 || undeferred[0] != B {
		t.Errorf("undeferred = %v; want [%s]", undeferred, B)
	}
}

// TestReevaluateDeferred_StaysDeferredWhileBlockerOpen: B's blocker A is still
// open (and not terminal in-queue), so B remains deferred — the un-defer
// condition is the exact inverse of the submit-time deferral.
func TestReevaluateDeferred_StaysDeferredWhileBlockerOpen(t *testing.T) {
	t.Parallel()

	const (
		A core.BeadID = "hk-nbjht.A"
		B core.BeadID = "hk-nbjht.B"
	)
	ledger := &reevalFixtureLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			A: queue.BeadStatusOpen,
			B: queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{A, B}: true,
		},
	}
	g := reevalStreamGroup(
		queue.Item{BeadID: A, Status: queue.ItemStatusDispatched}, // in-flight, not terminal
		queue.Item{BeadID: B, Status: queue.ItemStatusDeferredForLedgerDep},
	)

	undeferred, err := queue.ReevaluateDeferred(context.Background(), g, ledger)
	if err != nil {
		t.Fatalf("ReevaluateDeferred: unexpected error: %v", err)
	}
	if g.Items[1].Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("B status = %q while blocker still open; want %q (no un-defer)",
			g.Items[1].Status, queue.ItemStatusDeferredForLedgerDep)
	}
	if len(undeferred) != 0 {
		t.Errorf("undeferred = %v; want none (blocker still open)", undeferred)
	}
}

// TestReevaluateDeferred_ChainUndefersOneLinkAtATime: a 3-link chain R←A←B with
// A and B both deferred. When R completes, only A un-defers (its sole blocker is
// satisfied); B remains deferred because A is still open. This is the
// hk-tigaf.1→.2→… chain-stall scenario reduced to its core.
func TestReevaluateDeferred_ChainUndefersOneLinkAtATime(t *testing.T) {
	t.Parallel()

	const (
		R core.BeadID = "hk-tigaf.1"
		A core.BeadID = "hk-tigaf.2"
		B core.BeadID = "hk-tigaf.3"
	)
	ledger := &reevalFixtureLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			R: queue.BeadStatusOpen, // R still "open" in ledger; satisfied via queue-terminal
			A: queue.BeadStatusOpen,
			B: queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{R, A}: true, // R blocks A
			{A, B}: true, // A blocks B
		},
	}
	g := reevalStreamGroup(
		queue.Item{BeadID: R, Status: queue.ItemStatusCompleted}, // R finished
		queue.Item{BeadID: A, Status: queue.ItemStatusDeferredForLedgerDep},
		queue.Item{BeadID: B, Status: queue.ItemStatusDeferredForLedgerDep},
	)

	undeferred, err := queue.ReevaluateDeferred(context.Background(), g, ledger)
	if err != nil {
		t.Fatalf("ReevaluateDeferred: unexpected error: %v", err)
	}
	if g.Items[1].Status != queue.ItemStatusPending {
		t.Errorf("A status = %q after R completed; want %q", g.Items[1].Status, queue.ItemStatusPending)
	}
	if g.Items[2].Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("B status = %q while A still open; want %q (B must NOT skip ahead)",
			g.Items[2].Status, queue.ItemStatusDeferredForLedgerDep)
	}
	if len(undeferred) != 1 || undeferred[0] != A {
		t.Errorf("undeferred = %v; want [%s] (only the next link un-defers)", undeferred, A)
	}
}

// TestReevaluateDeferred_NilLedgerNoOp confirms the nil-ledger guard: legacy /
// test callers without a ledger seam leave deferred items untouched.
func TestReevaluateDeferred_NilLedgerNoOp(t *testing.T) {
	t.Parallel()

	g := reevalStreamGroup(
		queue.Item{BeadID: "hk-nbjht.X", Status: queue.ItemStatusDeferredForLedgerDep},
	)
	undeferred, err := queue.ReevaluateDeferred(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("ReevaluateDeferred(nil ledger): unexpected error: %v", err)
	}
	if len(undeferred) != 0 {
		t.Errorf("undeferred = %v; want none (nil ledger is a no-op)", undeferred)
	}
	if g.Items[0].Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("item status mutated under nil ledger: %q", g.Items[0].Status)
	}
}
