package orchestrator

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestSelectStepsOverRefusedHead proves the queue dispatches the item BEHIND a
// refused head on the same call, not after the refusal expires.
func TestSelectStepsOverRefusedHead(t *testing.T) {
	t.Parallel()

	fleet := FleetSnapshot{
		Queues: []QueueSnapshot{{
			Name:      "beta",
			QueueID:   "qid-beta",
			Active:    true,
			WorkerCap: 2,
			ActiveGroup: &GroupSnapshot{
				GroupIndex: 0,
				Eligible: []ItemSnapshot{
					{ItemIdx: 0, BeadID: core.BeadID("dup-01")},
					{ItemIdx: 1, BeadID: core.BeadID("beta-own-01")},
				},
			},
		}},
		SkipBeads: map[string]bool{"dup-01": true},
	}

	sel, ok := SelectNextQueue(fleet)
	if !ok {
		t.Fatal("no selection: the queue has a ready item behind the refused head")
	}
	if sel.Item.BeadID != core.BeadID("beta-own-01") {
		t.Fatalf("selected bead %q; want %q — the selector stopped at the refused head",
			sel.Item.BeadID, "beta-own-01")
	}
	if sel.Item.ItemIdx != 1 {
		t.Fatalf("Selection.Item.ItemIdx = %d; want 1", sel.Item.ItemIdx)
	}
}

// TestSelectCarriesTheAbsoluteItemIndex is the off-by-one guard, and it is the
// reason the ItemIdx values here are NOT contiguous.
//
// ItemIdx is the absolute index into Group.Items; Eligible is a filtered
// sub-slice, so the two disagree whenever any item is ineligible. The daemon
// stamps the dispatch at ItemIdx. Return the eligible-relative position instead
// and the daemon stamps the WRONG item: the picked one never runs and an
// unrelated one is marked dispatched.
//
// A fixture whose eligible items sit at 0, 1, 2 cannot see that — both readings
// give the same number, which is why the case above does not cover this. So
// this one puts the eligible items at 3, 7 and 11 and refuses the first two.
// Eligible-relative would answer 2; absolute answers 11.
func TestSelectCarriesTheAbsoluteItemIndex(t *testing.T) {
	t.Parallel()

	sel, ok := SelectNextQueue(FleetSnapshot{
		Queues: []QueueSnapshot{{
			Name:      "main",
			QueueID:   "qid-main",
			Active:    true,
			WorkerCap: 2,
			ActiveGroup: &GroupSnapshot{
				GroupIndex: 0,
				Eligible: []ItemSnapshot{
					{ItemIdx: 3, BeadID: core.BeadID("refused-a")},
					{ItemIdx: 7, BeadID: core.BeadID("refused-b")},
					{ItemIdx: 11, BeadID: core.BeadID("offerable")},
				},
			},
		}},
		SkipBeads: map[string]bool{"refused-a": true, "refused-b": true},
	})
	if !ok {
		t.Fatal("no selection: the third eligible item is offerable")
	}
	if sel.Item.BeadID != core.BeadID("offerable") {
		t.Fatalf("selected bead %q; want %q", sel.Item.BeadID, "offerable")
	}
	if sel.Item.ItemIdx != 11 {
		t.Fatalf("Selection.Item.ItemIdx = %d; want 11 (the ABSOLUTE index into Group.Items). "+
			"2 means the eligible-relative position was returned, which makes the daemon stamp "+
			"the dispatch on the wrong queue item.", sel.Item.ItemIdx)
	}
}

// TestSelectSkipsMultipleRefusedItems proves the scan does not stop after one
// step: it walks to the first offerable item however many refusals precede it.
func TestSelectSkipsMultipleRefusedItems(t *testing.T) {
	t.Parallel()

	sel, ok := SelectNextQueue(FleetSnapshot{
		Queues: []QueueSnapshot{{
			Name:      "main",
			QueueID:   "qid-main",
			Active:    true,
			WorkerCap: 4,
			ActiveGroup: &GroupSnapshot{
				GroupIndex: 0,
				Eligible: []ItemSnapshot{
					{ItemIdx: 0, BeadID: core.BeadID("held-01")},
					{ItemIdx: 1, BeadID: core.BeadID("held-02")},
					{ItemIdx: 2, BeadID: core.BeadID("held-03")},
					{ItemIdx: 3, BeadID: core.BeadID("ready-01")},
				},
			},
		}},
		SkipBeads: map[string]bool{"held-01": true, "held-02": true, "held-03": true},
	})
	if !ok {
		t.Fatal("no selection: one item is offerable")
	}
	if sel.Item.BeadID != core.BeadID("ready-01") {
		t.Fatalf("selected bead %q; want %q", sel.Item.BeadID, "ready-01")
	}
}

// TestSelectHandsSlotToSibling is the shape the concurrent multi-queue scenario
// hits: two queues share a bead, one claims it, and the loser must not consume
// the tick. A queue whose every eligible item is refused is a NON-CONTRIBUTOR,
// so the round-robin gives the slot to a sibling with real work.
func TestSelectHandsSlotToSibling(t *testing.T) {
	t.Parallel()

	fleet := FleetSnapshot{
		Queues: []QueueSnapshot{
			{
				Name:      "alpha",
				QueueID:   "qid-alpha",
				Active:    true,
				WorkerCap: 1,
				ActiveGroup: &GroupSnapshot{
					GroupIndex: 0,
					Eligible:   []ItemSnapshot{{ItemIdx: 0, BeadID: core.BeadID("alpha-own-01")}},
				},
			},
			{
				Name:      "beta",
				QueueID:   "qid-beta",
				Active:    true,
				WorkerCap: 1,
				ActiveGroup: &GroupSnapshot{
					GroupIndex: 0,
					Eligible:   []ItemSnapshot{{ItemIdx: 0, BeadID: core.BeadID("dup-01")}},
				},
			},
		},
		SkipBeads: map[string]bool{"dup-01": true},
	}

	for cursor := 0; cursor < 4; cursor++ {
		fleet.RRCursor = cursor
		sel, ok := SelectNextQueue(fleet)
		if !ok {
			t.Fatalf("cursor %d: no selection; alpha has ready work", cursor)
		}
		if sel.QueueName != "alpha" {
			t.Fatalf("cursor %d: selected queue %q; want alpha — the refused queue took the slot",
				cursor, sel.QueueName)
		}
	}
}

// TestSelectRefusesWhenEveryQueueIsRefused proves the fallback does not invent
// work: when nothing is offerable the selector still declines.
//
// It deliberately does NOT assert SawNonContributing. That flag is dropped on
// the zero-candidate return — the early `return Selection{}, false` above the
// round-robin predates this change and does not carry it — and the daemon field
// it feeds (queueSelection.anyPausedOrEmpty) is written and never read. Pinning
// it here would pin a value nothing acts on. Recorded, not chased.
func TestSelectRefusesWhenEveryQueueIsRefused(t *testing.T) {
	t.Parallel()

	sel, ok := SelectNextQueue(FleetSnapshot{
		Queues: []QueueSnapshot{{
			Name:      "main",
			QueueID:   "qid-main",
			Active:    true,
			WorkerCap: 1,
			ActiveGroup: &GroupSnapshot{
				GroupIndex: 0,
				Eligible:   []ItemSnapshot{{ItemIdx: 0, BeadID: core.BeadID("dup-01")}},
			},
		}},
		SkipBeads: map[string]bool{"dup-01": true},
	})
	if ok {
		t.Fatalf("selected bead %q; want no selection", sel.Item.BeadID)
	}
}

// TestSelectUnaffectedWhenNothingIsRefused pins that the fallback is inert on
// the ordinary path: with no refusals the selector still offers the head, so
// this change cannot reorder normal dispatch.
func TestSelectUnaffectedWhenNothingIsRefused(t *testing.T) {
	t.Parallel()

	for _, skip := range []map[string]bool{nil, {}, {"unrelated-01": true}} {
		sel, ok := SelectNextQueue(FleetSnapshot{
			Queues: []QueueSnapshot{{
				Name:      "main",
				QueueID:   "qid-main",
				Active:    true,
				WorkerCap: 2,
				ActiveGroup: &GroupSnapshot{
					GroupIndex: 0,
					Eligible: []ItemSnapshot{
						{ItemIdx: 0, BeadID: core.BeadID("first-01")},
						{ItemIdx: 1, BeadID: core.BeadID("second-01")},
					},
				},
			}},
			SkipBeads: skip,
		})
		if !ok {
			t.Fatalf("skip=%v: no selection", skip)
		}
		if sel.Item.BeadID != core.BeadID("first-01") {
			t.Fatalf("skip=%v: selected bead %q; want the head %q", skip, sel.Item.BeadID, "first-01")
		}
	}
}
