package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// cooldown_wedge_test.go — the in-progress cooldown's half of the refusal
// wedge (hk-nown4).
//
// # Why this needs its own file and its own harness
//
// Two things hide this failure, and both have to be defeated at once.
//
// The first is the wake pump. runAdmissionLoop pokes QueueStore.Wake() every
// 2 ms, which is exactly the signal production does not have. Any fixture built
// on it sails through a parked loop. So this file runs the loop itself, with no
// pump.
//
// The second is the choice of observable, and it is the subtler trap. ShowBead
// is useless here: suppressing `br show` for five minutes is the whole POINT of
// the cooldown (hk-403fw), so its count is 1 and flat whether the loop is
// polling or wedged solid. A test that watched ShowBead would report health in
// both worlds. This file counts LOOP TICKS instead, through the periodic disk
// probe with its cadence overridden to a nanosecond so it fires once per tick.
// Free space is reported far above the watermark, so the probe only counts — it
// never latches diskLow and never runs the reclaim subprocess.
//
// # The failure being pinned
//
// Once the tick refuses every eligible item, the queue stops being a candidate
// and selection returns no-selection. The loop then takes the idle branch, which
// blocks on the wake channel with NO timer. A queue that reaches it runs again
// only if something else fires that channel.
//
// The tempting reasoning — that the cooldown always has a real wake signal
// because the sibling run completes — is wrong, and the arming site is why. It
// fires on the ledger's coarse status alone and never asks the run registry
// whether THIS daemon owns a run for the bead. A stale on-disk run left by a
// dead daemon, which is what a plain restart produces, arms it just the same. No
// completion will ever come and the park is permanent, not five minutes.
//
// specs/queue-model.md §9.8 states the rule this pins: a queue whose every
// eligible item is refused MUST be re-examined on a bounded poll and MUST NOT be
// left waiting only on an external wake signal. It says every refusal, not one
// of them.

// TestCooldownRefusalDoesNotParkTheLoop measures whether the loop keeps ticking
// while its only item is refused by the in-progress cooldown.
//
// Two controls make the growth assertion mean something:
//
//   - ShowBead must be exactly 1. More would mean the cooldown stopped
//     suppressing the subprocess, which is hk-403fw's job and must not regress.
//     Zero would mean the fixture never reached the arm site at all, so nothing
//     was ever refused and the test would be measuring an idle loop.
//   - Ticks must be non-zero at the first sample, or the loop never ran.
func TestCooldownRefusalDoesNotParkTheLoop(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-nown4-cooldown-wedge"

	ledger := newAdmissionLedger()
	ledger.setStatus(core.CoarseStatusInProgress)

	qLedger := &admissionQueueLedger{}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending}))

	// Reach the cooldown the way production does. The resetter seam is wired
	// because production wires it unconditionally; registering a run for the bead
	// is what skips the stranded auto-reset branch and lands on the arm site.
	// QueueName is empty — the shape a br-ready-dispatched run has — so this
	// handle stays out of the "main" queue's in-flight tally and selection still
	// offers the item.
	reg := daemon.NewRunRegistry()
	daemon.ExportedRunRegistryRegister(reg, core.RunID(uuid.New()), &daemon.RunHandle{BeadID: beadID})

	params := admissionDeps(t, ledger, qs, qLedger, true, nil)
	params.RunRegistry = reg
	params.StrandedInProgressResetter = &admissionResetter{}
	deps := daemon.ExportedTestRuntime(params)

	var tickMu sync.Mutex
	tickCount := 0
	diskReclaim := daemon.ExportedDiskReclaimPortForTesting(deps, time.Nanosecond,
		func(string) (uint64, error) {
			tickMu.Lock()
			tickCount++
			tickMu.Unlock()
			return 1 << 62, nil
		},
		func(context.Context, string, []string) error { return nil },
	)
	readTicks := func() int {
		tickMu.Lock()
		defer tickMu.Unlock()
		return tickCount
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoopWithDiskReclaimAndTestPorts(ctx, deps, diskReclaim, params) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
	}()

	// Settle into the idle branch, then sample across several poll intervals.
	// workloopPollInterval is 2 s, so a 6 s gap holds a handful of them.
	time.Sleep(3 * time.Second)
	early := readTicks()
	time.Sleep(6 * time.Second)
	late := readTicks()

	cancel()
	select {
	case <-loopDone:
	case <-time.After(15 * time.Second):
		t.Error("work loop did not exit within 15s after context cancel")
	}

	if early == 0 {
		t.Fatal("the loop never ticked, so this fixture cannot tell a polling loop from a parked one")
	}
	if got := ledger.showCount(beadID); got != 1 {
		t.Fatalf("ShowBead was called %d time(s) for the refused bead, want exactly 1. "+
			"0 means the fixture never armed the cooldown and nothing was refused, so the growth "+
			"assertion below would be measuring an idle loop. More than 1 means the cooldown stopped "+
			"suppressing `br show` at poll cadence, which is the hk-403fw regression this must not cause.", got)
	}
	if late <= early {
		t.Fatalf("the loop ticked %d times by 3s and still %d by 9s — it is parked. The idle wait has no "+
			"timer, and the in-progress cooldown has no reliable wake signal: the arm site checks the "+
			"ledger status alone and never asks whether THIS daemon owns a run, so a stale on-disk run "+
			"from a dead daemon arms it with no completion ever coming. specs/queue-model.md §9.8 "+
			"requires a bounded poll for EVERY refusal, not just the greenlight one.", early, late)
	}
}
