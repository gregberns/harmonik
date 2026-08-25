package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
)

var (
	_ runloop.LedgerPort      = daemonLedger{}
	_ runloop.MergePort       = daemonMerge{}
	_ runloop.GatePort        = daemonGate{}
	_ runloop.WorktreePort    = daemonWorktree{}
	_ runloop.LaunchPort      = daemonLaunch{}
	_ runloop.BudgetPort      = daemonBudget{}
	_ runloop.RunHandlePort   = (*runregistry.RunHandle)(nil)
	_ runloop.RunRegistryPort = daemonRunRegistry{}
)

type daemonLedger struct {
	brAdapter             beadLedger
	intentLogDir          string
	brTimeoutCfg          brcli.TimeoutConfig
	projectDir            string
	skipBrHistoryRotation bool
}

func (l daemonLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return l.brAdapter.ShowBead(ctx, id)
}

func (l daemonLedger) ReopenBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error {
	return l.brAdapter.ReopenBead(ctx, l.intentLogDir, l.brTimeoutCfg, runID, transitionID, beadID, reason)
}

func (l daemonLedger) CloseBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error {
	if !l.skipBrHistoryRotation && l.projectDir != "" {
		if rotationErr := runBrHistoryRotationPreflight(ctx, l.projectDir, brHistoryCloseTrimKeep); rotationErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: pre-close br history trim: %v\n", rotationErr)
		}
	}
	return l.brAdapter.CloseBead(ctx, l.intentLogDir, l.brTimeoutCfg, runID, transitionID, beadID, needsAttention)
}

func newLedgerPort(adapter beadLedger, intentLogDir string, timeout brcli.TimeoutConfig, projectDir string, skipHistoryRotation bool) runloop.LedgerPort {
	return daemonLedger{brAdapter: adapter, intentLogDir: intentLogDir, brTimeoutCfg: timeout, projectDir: projectDir, skipBrHistoryRotation: skipHistoryRotation}
}

func newEmitterPort(bus handlercontract.EventEmitter) runloop.EmitterPort {
	return bus
}

func clockOrSystem(clock substrate.ClockPort) substrate.ClockPort {
	if clock != nil {
		return clock
	}
	return substrate.SystemClock{}
}

type daemonMerge struct {
	q *mergeq.Queue
}

func (m daemonMerge) Submit() runmerge.Submit {
	if m.q != nil {
		return m.q.Submit
	}
	return runmerge.InlineSubmit
}

func newMergePort(mergeQueue *mergeq.Queue) runloop.MergePort {
	return daemonMerge{q: mergeQueue}
}

type daemonGate struct {
	reg core.Registry
}

func (g daemonGate) LookupGate(gateRef core.GateRef) (core.ControlPoint, bool, bool) {
	if g.reg == nil {
		return core.ControlPoint{}, false, false
	}
	cp, ok := g.reg.LookupByName(string(gateRef))
	return cp, ok, true
}

func newGatePort(registry core.Registry) runloop.GatePort {
	return daemonGate{reg: registry}
}

type daemonWorktree struct {
	factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)
}

func (w daemonWorktree) Create(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error) {
	return w.factory(ctx, projectDir, runID, headSHA)
}

func worktreePort(factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)) runloop.WorktreePort {
	return daemonWorktree{factory: factory}
}

type daemonLaunch struct {
	builder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
}

func (l daemonLaunch) BuildSpec(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return l.builder(ctx, rc)
}

func launchPort(builder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)) runloop.LaunchPort {
	return daemonLaunch{builder: builder}
}

type daemonBudget struct {
	queueStore *queuewiring.QueueStore
	projectDir string
}

// ChargeReviewLoopFailure increments the dispatched item's ReviewLoopFailures
// counter under LockForMutation and reports whether the retry-spend budget is now
// exhausted (>= MaxReviewLoopFailures). Returns false when no queue surface is
// wired (nil queueStore / queueID / groupIndex). Byte-identical to the pre-port
// inline block (hk-c1ah6 / hk-tigaf.4): resolve the queue BY NAME, mutate the
// matching item, persist, and report exhaustion.
func (b daemonBudget) ChargeReviewLoopFailure(ctx context.Context, queueName string, queueID *string, groupIndex *int, itemIndex int, beadID core.BeadID) bool {
	if queueID == nil || groupIndex == nil || itemIndex < 0 || b.queueStore == nil {
		return false
	}
	budgetExhausted := false
	lq := b.queueStore.LockForMutation()
	normName := queue.NormaliseQueueName(queueName)
	liveQ := lq.LockedQueueByName(normName)
	if liveQ != nil {
		for gi := range liveQ.Groups {
			if liveQ.Groups[gi].GroupIndex != *groupIndex {
				continue
			}
			if itemIndex < len(liveQ.Groups[gi].Items) &&
				liveQ.Groups[gi].Items[itemIndex].BeadID == beadID {
				liveQ.Groups[gi].Items[itemIndex].ReviewLoopFailures++
				if liveQ.Groups[gi].Items[itemIndex].ReviewLoopFailures >= queue.MaxReviewLoopFailures {
					budgetExhausted = true
					liveQ.Groups[gi].Items[itemIndex].LastFailureReason = "review_loop_budget_exhausted"
				}
				break
			}
		}
		lq.LockedSetQueueByName(normName, liveQ)
		if persistErr := queue.Persist(ctx, b.projectDir, liveQ); persistErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: Persist rl-failures queueID=%s: %v\n",
				liveQ.QueueID, persistErr)
		}
	}
	lq.Done()
	return budgetExhausted
}

func newBudgetPort(queueStore *queuewiring.QueueStore, projectDir string) runloop.BudgetPort {
	return daemonBudget{queueStore: queueStore, projectDir: projectDir}
}

type daemonRunRegistry struct {
	reg *runregistry.RunRegistry
}

func (a daemonRunRegistry) Get(runID core.RunID) (runloop.RunHandlePort, bool) {
	h, ok := a.reg.Get(runID)
	if !ok || h == nil {
		return nil, false
	}
	return h, true
}

func newStaticRunPorts(adapter beadLedger, bus handlercontract.EventEmitter, intentLogDir string, timeout brcli.TimeoutConfig, projectDir string, skipHistoryRotation bool, mergeQueue *mergeq.Queue, registry core.Registry, clock substrate.ClockPort) runloop.RunPorts {
	return runloop.RunPorts{
		Ledger:  newLedgerPort(adapter, intentLogDir, timeout, projectDir, skipHistoryRotation),
		Emitter: newEmitterPort(bus),
		Merge:   newMergePort(mergeQueue),
		Gate:    newGatePort(registry),
		Clock:   clockOrSystem(clock),
	}
}

func runEnvWithDispatch(base runloop.RunEnv,
	runID core.RunID,
	beadRecord core.BeadRecord,
	queueName string,
	queueID *string,
	queueGroupIndex *int,
	queueItemIndex int,
	itemWorkflow runloop.QueueWorkflowInput,
	itemTemplateParams map[string]string,
	itemLocalOnly bool,
	itemWorkerTarget string,
	queueDefaultHarness core.AgentType,
) runloop.RunEnv {
	base.RunID = runID
	base.BeadRecord = beadRecord
	base.QueueName = queueName
	base.QueueID = queueID
	base.QueueGroupIndex = queueGroupIndex
	base.QueueItemIndex = queueItemIndex
	base.QueueDefaultHarness = queueDefaultHarness
	base.ItemWorkflow = itemWorkflow
	base.ItemTemplateParams = itemTemplateParams
	base.ItemLocalOnly = itemLocalOnly
	base.ItemWorkerTarget = itemWorkerTarget
	return base
}

func newSharedHandles(runRegistry *runregistry.RunRegistry, localInFlight *atomic.Int32, agentSpawnSem chan struct{}, workerRegistry *workers.Registry, queueStore *queuewiring.QueueStore, projectDir string, harnessRegistry *handlercontract.HarnessRegistry, adapterRegistry *handlercontract.AdapterRegistry, hookStore hookStoreIface, substratePort, reviewerSubstrate handler.Substrate, tidGen runloop.TransitionIDSource, emittedEpics map[core.BeadID]struct{}, emittedEpicsMu *sync.Mutex, adapter beadLedger, runner tmuxpkg.CommandRunner, worktreeFactory func(context.Context, string, string, string) (string, func(), error), worktreeCreateMu *sync.Mutex) runloop.SharedHandles {
	return runloop.SharedHandles{
		RunRegistry: daemonRunRegistry{reg: runRegistry}, LocalInFlight: localInFlight,
		AgentSpawnSem: agentSpawnSem, Workers: workerRegistry, Budget: newBudgetPort(queueStore, projectDir),
		HarnessRegistry: harnessRegistry, AdapterRegistry: adapterRegistry, HookStore: hookStore,
		Substrate: substratePort, ReviewerSubstrate: reviewerSubstrate, TIDGen: tidGen,
		EmittedEpics: emittedEpics, EmittedEpicsMu: emittedEpicsMu, BrAdapter: adapter,
		Runner: runner, WorktreeFactory: worktreeFactory, WorktreeCreateMu: worktreeCreateMu,
	}
}

func buildRunBundles(basePorts runloop.RunPorts, handles runloop.SharedHandles, env runloop.RunEnv, injectedBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)) (runloop.RunPorts, runloop.SharedHandles) {
	builder := injectedBuilder
	if builder == nil {
		if handles.HarnessRegistry != nil {
			builder = routedLaunchSpecBuilder(handles.HarnessRegistry, env.BeadRecord, env.QueueDefaultHarness, core.AgentType(""), env.DefaultHarness, basePorts.Emitter)
		} else {
			builder = claude.BuildLaunchSpec
		}
	}
	basePorts.Launch = launchPort(builder)
	basePorts.LaunchBuilder = builder
	return basePorts, handles
}
