package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/sentinel"
)

const sentinelGateSubjectID = "sentinel"

const sentinelGateTripToken = "sentinel-gate-test-token"

func sentinelGateGovernorState() *sentinel.GovernorState {
	return &sentinel.GovernorState{DaemonStartedAt: time.Now()}
}

// TestSentinelGate_QueuePathHoldsWhileTheGovernorTripIsPending pins the sentinel
// queue-level gate (FW3 hk-4toh) on the queue dispatch path.
//
// The gate blocks ALL beads, not one bead, until real movement clears the trip.
// It sits above the pre-claim ShowBead, so a held bead spawns no `br show`
// either.
//
// Three subtests, and the third is the one that says why the seam exists:
//
//   - control: no trip pending → the bead IS claimed. Without this the hold below
//     could be any other gate, or a fixture that never reached dispatch.
//   - trip pending → no claim, and the item is left exactly as it was found.
//   - trip pending but the governor subsystem ABSENT → the bead IS claimed. A
//     switched-off subsystem does not get to hold the dispatcher shut, and this
//     is also the proof that an enabled governor makes the gate reachable.
func TestSentinelGate_QueuePathHoldsWhileTheGovernorTripIsPending(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-4toh-queue-path-bead"
	const parkedID core.BeadID = "hk-4toh-queue-parked-bead"

	observe := func(t *testing.T, tripPending, governorPresent bool) (claimCalls, showCalls, ticks int, item queue.Item) {
		t.Helper()
		ledger := newAdmissionLedger()

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
			admissionParkedItem(parkedID),
		))

		blocker := daemon.NewDecisionBlocker()
		if tripPending {
			blocker.AddQueueBlock(sentinelGateSubjectID, sentinelGateTripToken)
		}

		params := admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil)
		params.DecisionBlocker = blocker
		governorState := sentinelGateGovernorState()
		var tickMu sync.Mutex
		tickCount := 0
		deps := daemon.ExportedTestRuntime(params)
		diskReclaim := daemon.ExportedDiskReclaimPortForTesting(deps, time.Nanosecond,
			func(string) (uint64, error) {
				tickMu.Lock()
				tickCount++
				tickMu.Unlock()
				return 1 << 62, nil
			},
			func(context.Context, string, []string) error { return nil },
		)

		var snapshot *queue.Queue
		runAdmissionLoop(t, qs,
			func(c context.Context) {
				if governorPresent {
					daemon.ExportedRunWorkLoopWithGovernorAndDiskReclaim(c, deps, governorState, diskReclaim) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
					return
				}
				daemon.ExportedRunWorkLoopWithDiskReclaim(c, deps, diskReclaim) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() { snapshot = qs.Queue() },
		)
		ledger.assertNoRunPathCalls(t)
		tickMu.Lock()
		ticks = tickCount
		tickMu.Unlock()
		return ledger.claimCount(beadID), ledger.showCount(beadID), ticks, admissionFirstItem(t, snapshot)
	}

	t.Run("with no trip pending the bead is claimed", func(t *testing.T) {
		t.Parallel()
		claims, _, _, _ := observe(t, false, true)
		if claims == 0 {
			t.Fatal("ClaimBead was never called with a live governor and no trip pending. " +
				"The subtests below assert that a pending trip STOPS the claim, and that is empty unless " +
				"this same fixture demonstrably claims when no trip is pending.")
		}
	})

	t.Run("a pending trip holds the bead without claiming it", func(t *testing.T) {
		t.Parallel()
		claims, shows, ticks, item := observe(t, true, true)
		if ticks < admissionMinTicks {
			t.Fatalf("the loop completed %d tick(s) over %v, want at least %d. "+
				"The hold claim below is empty unless the loop really ran.",
				ticks, admissionObserveWindow, admissionMinTicks)
		}
		if claims != 0 {
			t.Errorf("ClaimBead called %d time(s) while a sentinel governor trip was pending, want 0.\n"+
				"The sentinel queue-level gate (FW3 hk-4toh) must hold every bead until real movement clears "+
				"the trip. A claim here means the gate no longer fires on the queue dispatch path.", claims)
		}
		if shows != 0 {
			t.Errorf("ShowBead called %d time(s) for a bead held by the sentinel gate, want 0 — "+
				"the gate sits above the pre-claim `br show`, so a held bead must spawn no subprocess", shows)
		}
		if item.Status != queue.ItemStatusPending {
			t.Errorf("held item status = %q, want %q — the sentinel gate must hold without stamping",
				item.Status, queue.ItemStatusPending)
		}
		if item.Attempts != 0 {
			t.Errorf("held item Attempts = %d, want 0 — a bead the daemon is deliberately holding must not "+
				"spend its dispatch budget", item.Attempts)
		}
		if item.RunID != nil {
			t.Errorf("held item carries RunID %v — a run was stamped for a bead that must not dispatch", *item.RunID)
		}
	})

	t.Run("with the governor subsystem absent the same trip does not gate dispatch", func(t *testing.T) {
		t.Parallel()
		claims, _, _, _ := observe(t, true, false)
		if claims == 0 {
			t.Errorf("ClaimBead was never called with the movement governor absent and a stale sentinel " +
				"block set.\n" +
				"dispatchBlocked returns false on an absent governor on purpose: the block has exactly one " +
				"writer and one clearer, both inside that subsystem. Keeping the read while removing the " +
				"subsystem leaves a gate no code path can open, and one stale ack file wedges every dispatch " +
				"on every later boot.")
		}
	})
}

// TestSentinelGate_ReadyPathHoldsWhileTheGovernorTripIsPending pins the same
// gate on the other dispatch path.
//
// The gate is written out twice, once per path, and the two copies differ only
// in their stderr string. Deleting either copy compiles clean and silently
// un-gates that path, so each needs its own test.
//
// The fixture is the br-ready shape used by
// TestAdmissionOrder_ReadyPathBoundsAttemptsBeforeHandlerPause: an EMPTY queue
// store, so selection finds nothing and the loop falls through to the br-ready
// poll, which the fake ledger answers with one open bead.
func TestSentinelGate_ReadyPathHoldsWhileTheGovernorTripIsPending(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-4toh-ready-path-bead"

	observe := func(t *testing.T, tripPending bool) (claimCalls, ticks int) {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.readyResult = []core.BeadRecord{{BeadID: beadID, Status: core.CoarseStatusOpen}}

		blocker := daemon.NewDecisionBlocker()
		if tripPending {
			blocker.AddQueueBlock(sentinelGateSubjectID, sentinelGateTripToken)
		}

		qs := daemon.ExportedNewQueueStore()
		params := admissionDeps(t, ledger, qs, &admissionQueueLedger{}, false, nil)
		params.DecisionBlocker = blocker
		governorState := sentinelGateGovernorState()

		var tickMu sync.Mutex
		tickCount := 0
		deps := daemon.ExportedTestRuntime(params)
		diskReclaim := daemon.ExportedDiskReclaimPortForTesting(deps, time.Nanosecond,
			func(string) (uint64, error) {
				tickMu.Lock()
				defer tickMu.Unlock()
				tickCount++
				return 1 << 62, nil
			},
			func(context.Context, string, []string) error { return nil },
		)

		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoopWithGovernorAndDiskReclaim(c, deps, governorState, diskReclaim) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() {},
		)
		ledger.assertNoRunPathCalls(t)
		tickMu.Lock()
		ticks = tickCount
		tickMu.Unlock()
		return ledger.claimCount(beadID), ticks
	}

	t.Run("with no trip pending the br-ready bead is claimed", func(t *testing.T) {
		t.Parallel()
		if claims, _ := observe(t, false); claims == 0 {
			t.Fatal("ClaimBead was never called on the br-ready path with no trip pending. " +
				"The negative assertion below is only evidence if this fixture can reach the claim.")
		}
	})

	t.Run("a pending trip holds the br-ready bead without claiming it", func(t *testing.T) {
		t.Parallel()
		claims, ticks := observe(t, true)
		if ticks < admissionMinTicks {
			t.Fatalf("the loop completed %d tick(s) over %v, want at least %d. "+
				"The hold claim below is empty unless the loop really ran.",
				ticks, admissionObserveWindow, admissionMinTicks)
		}
		if claims != 0 {
			t.Errorf("ClaimBead called %d time(s) on the br-ready path while a sentinel governor trip was "+
				"pending, want 0.\n"+
				"The gate is duplicated per dispatch path. Losing the br-ready copy leaves the queue-path "+
				"copy green and un-gates every bead the daemon pulls from `br ready`.", claims)
		}
	})
}

type sentinelGateLedger struct {
	*admissionLedger

	readyMu    sync.Mutex
	readyCalls int
	onReady    func()
}

func (l *sentinelGateLedger) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	l.readyMu.Lock()
	l.readyCalls++
	hook := l.onReady
	l.readyMu.Unlock()

	if hook != nil {
		hook()
	}
	return l.admissionLedger.Ready(ctx)
}

func (l *sentinelGateLedger) readyCount() int {
	l.readyMu.Lock()
	defer l.readyMu.Unlock()
	return l.readyCalls
}

// TestSentinelGate_GovernorTickArmsTheTripBeforeTheGateReadsIt pins the ordering
// half of constraint 6: governor.tick runs BEFORE the sentinel gate reads the
// block, so a trip armed during the governor's evaluation gates dispatch on the
// SAME tick it was armed.
//
// # Why the clause matters
//
// governor.tick is the LAST statement of tickBeforeSelect, and the gate is read
// live at the claim site further down the same tick. Move the read up, or turn
// it into a snapshot taken at the top of the pass, and the daemon dispatches one
// more bead on the tick a trip first fires. That is a real dispatch the trip was
// meant to stop, and it compiles clean.
//
// # How the trip is armed mid-tick
//
// Through brAdapter.Ready. NoAutoPull is set, so the dispatch loop's own
// `br ready` poll is switched off and governorGatherInput is the only caller
// left — the test asserts the call count to hold that. The hook arms the
// "sentinel" queue block on the first call, which is a point strictly inside
// governor.tick. The governor's evaluation cadence is two minutes and the
// observation window is under a second, so exactly one evaluation happens: the
// trip is armed once, on one known tick.
//
// The control is the same fixture with the hook doing nothing. It claims, which
// proves the fixture reaches dispatch on those ticks and that the hold below is
// caused by the arming and nothing else.
func TestSentinelGate_GovernorTickArmsTheTripBeforeTheGateReadsIt(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-4toh-same-tick-bead"
	const parkedID core.BeadID = "hk-4toh-same-tick-parked-bead"

	observe := func(t *testing.T, armDuringGovernorTick bool) (claimCalls, readyCalls int, item queue.Item) {
		t.Helper()
		base := newAdmissionLedger()
		ledger := &sentinelGateLedger{admissionLedger: base}

		blocker := daemon.NewDecisionBlocker()
		if armDuringGovernorTick {
			var once sync.Once
			ledger.onReady = func() {
				once.Do(func() { blocker.AddQueueBlock(sentinelGateSubjectID, sentinelGateTripToken) })
			}
		}

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
			admissionParkedItem(parkedID),
		))

		params := admissionDeps(t, base, qs, &admissionQueueLedger{}, true, nil)
		params.BrAdapter = ledger
		params.DecisionBlocker = blocker
		governorState := sentinelGateGovernorState()
		deps := daemon.ExportedTestRuntime(params)

		var snapshot *queue.Queue
		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoopWithGovernor(c, deps, governorState) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() { snapshot = qs.Queue() },
		)
		base.assertNoRunPathCalls(t)
		return base.claimCount(beadID), ledger.readyCount(), admissionFirstItem(t, snapshot)
	}

	t.Run("the governor evaluates once and an un-armed loop claims", func(t *testing.T) {
		t.Parallel()
		claims, readyCalls, _ := observe(t, false)
		if readyCalls != 1 {
			t.Fatalf("brAdapter.Ready called %d time(s), want exactly 1.\n"+
				"NoAutoPull is set, so governorGatherInput is the only caller and the eval cadence is two "+
				"minutes. A different count means the arming hook in the subtest below cannot be attributed "+
				"to one governor evaluation.", readyCalls)
		}
		if claims == 0 {
			t.Fatal("ClaimBead was never called with the arming hook inert. The same-tick assertion below " +
				"asserts the ABSENCE of a claim, which proves nothing unless this fixture claims.")
		}
	})

	t.Run("a trip armed inside governor.tick gates the same tick", func(t *testing.T) {
		t.Parallel()
		claims, readyCalls, item := observe(t, true)
		if readyCalls != 1 {
			t.Fatalf("brAdapter.Ready called %d time(s), want exactly 1 — the trip must be armed on one "+
				"known governor evaluation for the claim count below to mean anything", readyCalls)
		}
		if claims != 0 {
			t.Errorf("ClaimBead called %d time(s) after the trip was armed inside governor.tick, want 0.\n"+
				"governor.tick is the last statement of tickBeforeSelect and the sentinel gate is read live "+
				"further down the same tick. Exactly one claim means the gate now reads a value captured "+
				"BEFORE the governor ran, so the daemon dispatches one extra bead on the tick a trip first "+
				"fires. More than one means the gate is gone.", claims)
		}
		if item.Status != queue.ItemStatusPending {
			t.Errorf("held item status = %q, want %q", item.Status, queue.ItemStatusPending)
		}
		if item.Attempts != 0 {
			t.Errorf("held item Attempts = %d, want 0 — the bead was never stamped, so it spent no budget",
				item.Attempts)
		}
	})
}
