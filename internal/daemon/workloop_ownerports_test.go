package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

func TestWorkLoopOwnerPorts_ProjectLifecycleAndLedgerRepair(t *testing.T) {
	t.Parallel()

	stopCtx, stopDispatch := context.WithCancel(context.Background())
	defer stopDispatch()
	var drained, exited bool
	lifecyclePort := newLoopLifecyclePort(Config{
		CancelOnQueueDrain: func() { drained = true },
		CancelOnQueueExit:  func() { exited = true },
		StopDispatchCtx:    stopCtx,
	})
	if lifecyclePort.stopDispatchCtx != stopCtx {
		t.Fatal("loop lifecycle port did not retain StopDispatchCtx")
	}
	lifecyclePort.cancelOnQueueDrain()
	lifecyclePort.cancelOnQueueExit()
	if !drained || !exited {
		t.Fatal("loop lifecycle port did not retain queue terminal cancels")
	}

	adapter := &ownerPortLedger{}
	projectDir := t.TempDir()
	repairPort := newLedgerRepairPort(workLoopDeps{brAdapter: adapter, projectDir: projectDir})
	if repairPort.staleBlockerCloser != adapter || repairPort.strandedInProgressResetter != adapter {
		t.Fatal("ledger repair port did not retain the adapter repair interfaces")
	}
	if want := lifecycle.ComputeProjectHash(projectDir); repairPort.strandedResetProjectHash != want {
		t.Fatalf("stranded reset project hash = %q, want %q", repairPort.strandedResetProjectHash, want)
	}
	if repairPort.strandedResetDaemonNS == 0 {
		t.Fatal("stranded reset daemon namespace is zero")
	}
}

func TestWorkLoopOwnerPorts_WaitsForSpawnReadiness(t *testing.T) {
	t.Parallel()

	spawnReady := make(chan struct{})
	adapter := &ownerPortLedger{readyCalled: make(chan struct{}, 1)}
	deps := workLoopDeps{
		brAdapter:     adapter,
		runRegistry:   newLocalRunRegistry(),
		localInFlight: new(atomic.Int32),
		maxConcurrent: 1,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runWorkLoop(ctx, deps, loopLifecyclePort{spawnSubstrateReadyCh: spawnReady}, newLedgerRepairPort(deps), schedulePort{}, coordinatorReapPort{}, newTestDiskReclaimPort(deps), eagerRefillPort{}, governorPort{}, false)
	}()

	select {
	case <-adapter.readyCalled:
		t.Fatal("work loop read ready beads before the spawn substrate became ready")
	case <-time.After(150 * time.Millisecond):
	}
	close(spawnReady)
	select {
	case <-adapter.readyCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("work loop did not resume after the spawn substrate became ready")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWorkLoop = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("work loop did not stop after context cancellation")
	}
}

type ownerPortLedger struct {
	readyCalled chan struct{}
}

func (l *ownerPortLedger) Ready(context.Context) ([]core.BeadRecord, error) {
	if l.readyCalled != nil {
		select {
		case l.readyCalled <- struct{}{}:
		default:
		}
	}
	return nil, nil
}

func (*ownerPortLedger) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, nil
}

func (*ownerPortLedger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}

func (*ownerPortLedger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return nil
}

func (*ownerPortLedger) ReopenBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, string) error {
	return nil
}

func (*ownerPortLedger) SweepCloseBead(context.Context, brcli.TimeoutConfig, core.BeadID) error {
	return nil
}

func (*ownerPortLedger) ResetBead(context.Context, string, brcli.TimeoutConfig, core.BeadID, core.ProjectHash, int64) error {
	return nil
}
