package orchestrator_test

// select_hktigaf4_test.go — NQ-B1 pure-decision truth-table for the cross-queue
// round-robin selector, migrated from internal/daemon
// workloop_perqueue_roundrobin_hktigaf4_test.go (M5 slice 3A). These cases drive
// orchestrator.SelectNextQueue over a FleetSnapshot directly — the daemon's
// lock/registry projection is exercised by the sibling tests that remain in
// package daemon.
//
// Bead: hk-tigaf.4 (NQ-B1)
// Spec refs:
//   - specs/queue-model.md §9.3 QM-062 (capacity composition)
//   - specs/queue-model.md §9.8 QM-067 (cross-queue round-robin dispatch policy)

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/orchestrator"
)

// orchTestQueue builds an active QueueSnapshot with `pending` eligible items and
// the given LOCAL in-flight count and worker ceiling. A zero-pending queue has
// no active group (non-candidate).
func orchTestQueue(name, queueID string, pending, localInFlight, workerCap int) orchestrator.QueueSnapshot {
	var g *orchestrator.GroupSnapshot
	if pending > 0 {
		items := make([]orchestrator.ItemSnapshot, pending)
		for i := range items {
			items[i] = orchestrator.ItemSnapshot{
				ItemIdx: i,
				BeadID:  core.BeadID(name + "-" + string(rune('a'+i))),
			}
		}
		g = &orchestrator.GroupSnapshot{GroupIndex: 0, Eligible: items}
	}
	return orchestrator.QueueSnapshot{
		Name:          name,
		QueueID:       queueID,
		Active:        true,
		LocalInFlight: localInFlight,
		WorkerCap:     workerCap,
		ActiveGroup:   g,
	}
}

// ---------------------------------------------------------------------------
// SelectNextQueue — per-queue cap gate
// ---------------------------------------------------------------------------

// TestSelectNextQueue_PerQueueCapBoundsDispatch verifies that a queue already at
// its Workers ceiling (LocalInFlight >= WorkerCap) is excluded from selection
// even when it still has pending items.
func TestSelectNextQueue_PerQueueCapBoundsDispatch(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{
			orchTestQueue("main", "qid-main", 3, 0, 3),       // room
			orchTestQueue("investigate", "qid-inv", 1, 1, 1), // at cap
		},
		RRCursor: 0,
	}
	sel, ok := orchestrator.SelectNextQueue(f)
	if !ok {
		t.Fatal("SelectNextQueue returned ok=false, want a selection from main")
	}
	if sel.QueueName != "main" {
		t.Errorf("selected queue = %q, want main (investigate is at its per-queue cap)", sel.QueueName)
	}
}

// TestSelectNextQueue_AllAtCapSelectsNothing verifies that when every queue is at
// its per-queue Workers ceiling, SelectNextQueue returns ok=false.
func TestSelectNextQueue_AllAtCapSelectsNothing(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{
			orchTestQueue("main", "qid-main", 3, 1, 1),
			orchTestQueue("investigate", "qid-inv", 1, 1, 1),
		},
	}
	if _, ok := orchestrator.SelectNextQueue(f); ok {
		t.Error("SelectNextQueue returned ok=true, want false (both queues at per-queue cap)")
	}
}

// TestSelectNextQueue_LocalCapVsGlobalAsymmetry is the hk-4tjt6 pure-decision
// guard: the selector gates ONLY on LocalInFlight vs WorkerCap. A queue whose
// LocalInFlight is 0 remains selectable at WorkerCap even though (conceptually)
// remote runs are in flight — remote runs are excluded from LocalInFlight by the
// daemon's projection, so they never gate the queue. The projection half of this
// invariant is covered by the registry-integration test in package daemon.
func TestSelectNextQueue_LocalCapVsGlobalAsymmetry(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{
			// jessica-sat: 3 pending, LocalInFlight=0 (all in-flight runs are
			// remote), WorkerCap=4. Must be selectable — no local runs to gate on.
			orchTestQueue("jessica-sat", "qid-jsat", 3, 0, 4),
		},
	}
	sel, ok := orchestrator.SelectNextQueue(f)
	if !ok {
		t.Fatal("SelectNextQueue returned ok=false for local=0/cap=4 queue — local cap must NOT block on remote runs (hk-4tjt6)")
	}
	if sel.QueueName != "jessica-sat" {
		t.Errorf("selected queue = %q, want jessica-sat", sel.QueueName)
	}
}

// ---------------------------------------------------------------------------
// SelectNextQueue — round-robin cursor advance (no starvation)
// ---------------------------------------------------------------------------

// TestSelectNextQueue_CursorAdvanceRotatesSelection is the load-bearing NQ-B1
// test: the round-robin cursor — when ADVANCED every tick (not reset to 0) —
// rotates dispatch fairly so the lexicographically-earlier queue ("investigate")
// does NOT starve the later one ("main").
func TestSelectNextQueue_CursorAdvanceRotatesSelection(t *testing.T) {
	t.Parallel()

	picks := map[string]int{}
	cursor := 0
	const ticks = 10
	for i := 0; i < ticks; i++ {
		f := orchestrator.FleetSnapshot{
			Queues: []orchestrator.QueueSnapshot{
				orchTestQueue("main", "qid-main", 10, 0, 10),
				orchTestQueue("investigate", "qid-inv", 10, 0, 10),
			},
			RRCursor: cursor,
		}
		sel, ok := orchestrator.SelectNextQueue(f)
		if !ok {
			t.Fatalf("tick %d: SelectNextQueue ok=false, want a selection", i)
		}
		picks[sel.QueueName]++
		cursor++ // ADVANCE every tick — the no-starvation invariant
	}

	if picks["main"] == 0 {
		t.Errorf("main was never selected over %d ticks — STARVATION (cursor not advancing?)", ticks)
	}
	if picks["investigate"] == 0 {
		t.Errorf("investigate was never selected over %d ticks", ticks)
	}
	if picks["main"] != ticks/2 || picks["investigate"] != ticks/2 {
		t.Errorf("uneven round-robin: main=%d investigate=%d, want %d each",
			picks["main"], picks["investigate"], ticks/2)
	}
}

// TestSelectNextQueue_CursorResetWouldStarve documents the failure mode the
// advancing cursor prevents: holding the cursor at 0 every tick makes the
// lexicographically-first queue win every time, starving the other.
func TestSelectNextQueue_CursorResetWouldStarve(t *testing.T) {
	t.Parallel()

	picks := map[string]int{}
	const ticks = 6
	for i := 0; i < ticks; i++ {
		f := orchestrator.FleetSnapshot{
			Queues: []orchestrator.QueueSnapshot{
				orchTestQueue("main", "qid-main", 10, 0, 10),
				orchTestQueue("investigate", "qid-inv", 10, 0, 10),
			},
			RRCursor: 0, // pinned — the anti-pattern
		}
		sel, ok := orchestrator.SelectNextQueue(f)
		if !ok {
			t.Fatalf("tick %d: ok=false", i)
		}
		picks[sel.QueueName]++
	}

	if picks["investigate"] != ticks {
		t.Errorf("cursor-pinned-at-0: investigate=%d, want %d (sorts first, wins every tick)",
			picks["investigate"], ticks)
	}
	if picks["main"] != 0 {
		t.Errorf("cursor-pinned-at-0: main=%d, want 0 (starved) — confirms why the cursor MUST advance",
			picks["main"])
	}
}

// TestSelectNextQueue_EmptyFleet verifies the zero-queue guard.
func TestSelectNextQueue_EmptyFleet(t *testing.T) {
	t.Parallel()
	if _, ok := orchestrator.SelectNextQueue(orchestrator.FleetSnapshot{}); ok {
		t.Error("SelectNextQueue(empty) returned ok=true, want false")
	}
}

// TestSelectNextQueue_ReturnsAbsoluteItemIdx verifies addendum fix #1: the picked
// item carries the ABSOLUTE index into the group's Items slice, not an eligible
// sub-slice index. Here the eligible head sits at absolute index 2.
func TestSelectNextQueue_ReturnsAbsoluteItemIdx(t *testing.T) {
	t.Parallel()

	f := orchestrator.FleetSnapshot{
		Queues: []orchestrator.QueueSnapshot{{
			Name:      "main",
			QueueID:   "qid-main",
			Active:    true,
			WorkerCap: 4,
			ActiveGroup: &orchestrator.GroupSnapshot{
				GroupIndex: 0,
				Eligible: []orchestrator.ItemSnapshot{
					{ItemIdx: 2, BeadID: "hk-head"}, // absolute index 2 (items 0,1 terminal)
				},
			},
		}},
	}
	sel, ok := orchestrator.SelectNextQueue(f)
	if !ok {
		t.Fatal("SelectNextQueue ok=false, want a selection")
	}
	if sel.Item.ItemIdx != 2 {
		t.Errorf("Item.ItemIdx = %d, want 2 (absolute index into Group.Items)", sel.Item.ItemIdx)
	}
	if sel.Item.BeadID != "hk-head" {
		t.Errorf("Item.BeadID = %q, want hk-head", sel.Item.BeadID)
	}
}
