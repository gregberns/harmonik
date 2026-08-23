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

	time.Sleep(3 * time.Second)
	early := readTicks()
	time.Sleep(6 * time.Second)
	late := readTicks()

	cancel()
	awaitLoopTeardown(t, loopDone, "cooldown-wedge work loop")

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
