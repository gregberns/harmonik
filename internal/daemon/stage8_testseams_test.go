package daemon

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runloop"
)

// testRuntime holds the same typed bundles and declared loop inputs as the
// production composition root. It is only a test edge.
type testRuntime struct {
	env     runloop.RunEnv
	ports   runloop.RunPorts
	handles runloop.SharedHandles

	ledger        beadLedger
	queueStore    *queuewiring.QueueStore
	runRegistry   *RunRegistry
	substratePort handler.Substrate
	mergeQueue    *mergeq.Queue
	launchBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
	capacity      capacityPort
	queueSurface  queueSurfacePort
	dispatchGates dispatchGatesPort
	noAutoPull    bool
}

func (r testRuntime) runEnv(id core.RunID, record core.BeadRecord, queueName, workerTarget string, queueHarness core.AgentType) runloop.RunEnv {
	return runEnvWithDispatch(r.env, id, record, queueName, nil, nil, 0, runloop.QueueWorkflowInput{}, nil, false, workerTarget, queueHarness)
}

func (r testRuntime) buildRunBundles(env runloop.RunEnv) (runloop.RunPorts, runloop.SharedHandles) {
	return buildRunBundles(r.ports, r.handles, env, r.launchBuilder)
}

func (r testRuntime) reap(eager eagerRefillPort) reapSeamPort {
	return newReapSeamPort(r.ports.Emitter, r.env.ProjectDir, r.env.TargetBranch, r.queueStore, r.runRegistry, loopLifecyclePort{}, r.capacity, r.queueSurface, eager)
}

func (r testRuntime) ledgerRepair() ledgerRepairPort {
	return newLedgerRepairPort(r.ledger, r.env.ProjectDir)
}

func (r testRuntime) diskReclaim() diskReclaimPort {
	return newDiskReclaimPort(r.env.ProjectDir, r.ports.Emitter, r.runRegistry)
}

func (r testRuntime) runPorts() runloop.RunPorts {
	return r.ports
}

func (r testRuntime) sharedHandles() runloop.SharedHandles {
	return r.handles
}

func newDispatchGatesPortFromDeps(r testRuntime) dispatchGatesPort {
	gates := r.dispatchGates
	if gates.bus == nil {
		gates.bus = r.ports.Emitter
	}
	if gates.heldEventDedup == nil {
		gates.heldEventDedup = make(map[string]struct{})
	}
	if gates.queueWriteErrorReported == nil {
		gates.queueWriteErrorReported = make(map[string]struct{})
	}
	return gates
}

func runTestWorkLoop(ctx context.Context, r testRuntime, lifecycle loopLifecyclePort, repair ledgerRepairPort, disk diskReclaimPort, governor governorPort, enabled bool, capacity capacityPort, surface queueSurfacePort, gates dispatchGatesPort, noAutoPull bool) error {
	return runWorkLoop(ctx, r.env, r.ports, r.handles, r.ledger, r.queueStore, r.runRegistry, r.substratePort, r.mergeQueue, r.launchBuilder, lifecycle, repair, schedulePort{}, coordinatorReapPort{}, disk, eagerRefillPort{}, governor, enabled, capacity, surface, gates, noAutoPull)
}
