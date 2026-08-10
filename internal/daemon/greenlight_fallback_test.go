package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/orchestrator"
	"github.com/gregberns/harmonik/internal/queue"
)

// greenlight_fallback_test.go — the greenlight gate's half of the head-of-line
// fallback (hk-nown4).
//
// The claim cooldown and the greenlight gate held a bead the same wrong way:
// each refused the ITEM and then spent the whole TICK, so every ready item
// behind the refused one waited. The cooldown made that a five-minute stall.
// Greenlight made it unbounded — the bead is held until a captain runs
// `harmonik greenlight`, and until then the queue dispatched nothing at all.
//
// This test pins the daemon half. The selector half — stepping over a refused
// item rather than stopping at the head — is pinned in
// internal/orchestrator/select_skip_fallback_test.go. Both are needed: the
// selector cannot step over an item the loop never told it to refuse.

// TestGreenlightHoldDoesNotBlockTheQueue proves a bead held for greenlight does
// not hold up the ready bead behind it.
//
// The fixture puts a needs-greenlight bead at the head of a queue and a plain
// open bead behind it. The gate must hold the labeled bead AND the loop must go
// on to claim the sibling.
//
// Two controls make the claim mean something:
//
//   - The labeled bead must never be claimed. Without this the test would pass
//     on a build where the gate stopped firing entirely.
//   - The label must have been read — ShowBead is the only route by which it
//     reaches the loop, so a zero read count means the gate never saw it.
//
// Break the fix and this goes red: drop the refusal that the greenlight hold
// arms, and selection offers the labeled head on every tick, so the sibling is
// never claimed.
func TestGreenlightHoldDoesNotBlockTheQueue(t *testing.T) {
	t.Parallel()

	const heldID core.BeadID = "hk-nown4-greenlight-held"
	const readyID core.BeadID = "hk-nown4-ready-sibling"

	ledger := newAdmissionLedger()
	ledger.showLabelsByBead = map[core.BeadID][]string{
		heldID: {orchestrator.LabelNeedsGreenlight},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("main",
		queue.Item{BeadID: heldID, Status: queue.ItemStatusPending},
		queue.Item{BeadID: readyID, Status: queue.ItemStatusPending},
	))
	deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

	runAdmissionLoop(t, qs,
		func(c context.Context) {
			daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() {},
	)
	ledger.assertNoRunPathCalls(t)

	if got := ledger.showCount(heldID); got == 0 {
		t.Fatal("ShowBead was never called for the held bead, so the greenlight gate never read the " +
			"label. The hold below would then be caused by something other than the label.")
	}
	if got := ledger.claimCount(heldID); got != 0 {
		t.Errorf("ClaimBead was called %d time(s) for the needs-greenlight bead; want 0. "+
			"The gate must still hold it — the fallback lets the queue move PAST a held bead, "+
			"it does not dispatch one.", got)
	}
	if got := ledger.claimCount(readyID); got == 0 {
		t.Errorf("ClaimBead was never called for the ready bead behind the held one. " +
			"The greenlight hold consumed every tick, which is the head-of-line stall hk-nown4 " +
			"exists to close: the queue must refuse the ITEM and go on to the next eligible one.")
	}
}

// TestGreenlightWalkTerminates covers what the single-held-bead case above
// structurally cannot see: a walk longer than one step, and whether it ends.
//
// The greenlight path re-selects at once rather than sleeping, so several held
// beads in a row are walked WITHIN one tick. That walk is only bounded because
// the refusals it accumulates have no clock. Give them one and the earliest
// lapses while the loop is still walking the later ones — each step costs a
// ShowBead — so the loop re-offers a bead it already refused and never reaches
// its sleep. That failure was measured at 186,176 passes in 600 ms.
//
// Three held beads sit ahead of one ready bead. Two assertions, and the second
// is the one the earlier fixture could not make:
//
//  1. the ready bead is claimed, so the walk crossed all three; and
//  2. ShowBead is called a SANE number of times, so the walk ended rather than
//     spun. A spin shows up here as a count in the tens of thousands.
func TestGreenlightWalkTerminates(t *testing.T) {
	t.Parallel()

	held := []core.BeadID{
		"hk-nown4-walk-held-1",
		"hk-nown4-walk-held-2",
		"hk-nown4-walk-held-3",
	}
	const readyID core.BeadID = "hk-nown4-walk-ready"

	ledger := newAdmissionLedger()
	ledger.showLabelsByBead = map[core.BeadID][]string{}
	items := make([]queue.Item, 0, len(held)+1)
	for _, id := range held {
		ledger.showLabelsByBead[id] = []string{orchestrator.LabelNeedsGreenlight}
		items = append(items, queue.Item{BeadID: id, Status: queue.ItemStatusPending})
	}
	items = append(items, queue.Item{BeadID: readyID, Status: queue.ItemStatusPending})

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("main", items...))
	deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

	runAdmissionLoop(t, qs,
		func(c context.Context) {
			daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() {},
	)
	ledger.assertNoRunPathCalls(t)

	for _, id := range held {
		if got := ledger.claimCount(id); got != 0 {
			t.Errorf("ClaimBead was called %d time(s) for held bead %s; want 0", got, id)
		}
	}
	if got := ledger.claimCount(readyID); got == 0 {
		t.Error("ClaimBead was never called for the ready bead sitting behind THREE held beads. " +
			"The walk did not cross them, so the fallback steps over one refused item and then gives up.")
	}

	// The spin guard. The ceiling here is the harness's 2 ms wake pump, NOT the
	// CPU: the loop only ticks as fast as it is woken, so a fast box does not
	// inflate this count and the bound does not flake. Measured at roughly 494
	// reads per held bead, so the bound carries about 4x headroom — against a
	// spin that produces five orders of magnitude more.
	const spinBound = 2000
	for _, id := range held {
		if got := ledger.showCount(id); got > spinBound {
			t.Fatalf("ShowBead was called %d time(s) for held bead %s, over the %d bound. "+
				"The walk is spinning: it re-offers a bead it already refused instead of reaching its "+
				"sleep. The tick refusal set must have NO clock — a window that lapses mid-walk is "+
				"exactly this failure.", got, id, spinBound)
		}
	}
}

// TestGreenlightHoldDoesNotParkTheLoop is the wedge guard, and the reason it
// runs its own loop harness instead of runAdmissionLoop is the whole point.
//
// runAdmissionLoop pokes QueueStore.Wake() every 2 ms. That pump is exactly the
// signal production does NOT have, so every other test in this family is blind
// to the failure below and would pass straight through it.
//
// The failure: once the walk refuses every eligible item, the queue stops being
// a candidate and selection returns no-selection. The loop then takes the idle
// branch, which blocks on the wake channel with no timer. Nothing fires that
// channel when a captain runs `harmonik greenlight`, because clearing a label is
// a ledger edit the daemon never observes. The queue parks for good — a worse
// stall than the five-minute one this whole change set exists to remove.
//
// So this test runs the loop with NO pump and watches whether the daemon re-reads
// the held bead on its own. A parked loop reads it a fixed number of times and
// then stops forever; a correctly polling loop keeps re-reading at the poll
// interval. The assertion is on that growth, not on an absolute count.
func TestGreenlightHoldDoesNotParkTheLoop(t *testing.T) {
	t.Parallel()

	const heldID core.BeadID = "hk-nown4-wedge-held"

	ledger := newAdmissionLedger()
	ledger.showLabelsByBead = map[core.BeadID][]string{
		heldID: {orchestrator.LabelNeedsGreenlight},
	}

	// ONE item, and it is held. That is what makes the queue a non-contributor
	// and routes the tick into the idle branch under test.
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("main", queue.Item{BeadID: heldID, Status: queue.ItemStatusPending}))
	deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
	}()

	// Let the loop settle into the idle branch, then sample across several poll
	// intervals. workloopPollInterval is 2 s, so this window holds a handful.
	time.Sleep(3 * time.Second)
	early := ledger.showCount(heldID)
	time.Sleep(6 * time.Second)
	late := ledger.showCount(heldID)

	cancel()
	awaitLoopTeardown(t, loopDone, "greenlight-fallback work loop")

	// Positive control: the loop must have reached the bead at all. A count of
	// zero would mean the fixture never dispatched, and the growth assertion
	// below would then be measuring nothing.
	if early == 0 {
		t.Fatal("ShowBead was never called for the held bead, so the loop never reached the " +
			"greenlight gate and this fixture cannot see the wedge either way.")
	}
	if late <= early {
		t.Fatalf("ShowBead count was %d at 3s and still %d at 9s — the loop stopped re-reading the "+
			"held bead. It is parked in the idle wait, which has no timer, and nothing wakes it when a "+
			"captain clears the label: `harmonik greenlight` edits the ledger and the daemon never sees "+
			"it. A refused item must keep the loop on a bounded poll.", early, late)
	}
}
