// runports.go — RunEnv / RunPorts / SharedHandles adapter + constructor layer.
//
// The port INTERFACES and value/handle BUNDLES themselves (RunPorts, RunEnv,
// SharedHandles, LedgerPort … RunRegistryPort) now live in internal/runloop
// (P2 LIFT chunk L0). What stays here is the daemon-side half of the boundary:
// the concrete adapters (daemonLedger, daemonMerge, daemonGate, daemonBudget,
// daemonWorktree, daemonLaunch, daemonRunRegistry) that close over
// *legacy aggregate and structurally satisfy the runloop.*Port interfaces, plus the
// (*legacy aggregate) constructors that assemble runloop.RunPorts / runloop.RunEnv /
// runloop.SharedHandles. This is the LEGAL direction: daemon → runloop.
//
// Boundary-only: every port is a pass-through onto the exact concrete
// dependency already wired on legacy aggregate. No behavior changes — a port call
// resolves to the same underlying method the run path invoked directly before
// (RSM-010). The nil-default adapters preserve today's nil-means-production
// defaulting exactly (ports-design §3).
//
// Idiom mirror: internal/keeper/ports.go (structural narrow interfaces;
// runloop.EmitterPort = Emitter type alias).

package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

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

// The daemon adapters below structurally satisfy the runloop.*Port interfaces —
// the compiler-checked expression of the daemon → runloop direction (LIFT L0).
var (
	_ runloop.LedgerPort      = daemonLedger{}
	_ runloop.MergePort       = daemonMerge{}
	_ runloop.GatePort        = daemonGate{}
	_ runloop.WorktreePort    = daemonWorktree{}
	_ runloop.LaunchPort      = daemonLaunch{}
	_ runloop.BudgetPort      = daemonBudget{}
	_ runloop.RunHandlePort   = (*RunHandle)(nil)
	_ runloop.RunRegistryPort = daemonRunRegistry{}
)

// daemonLedger is the production runloop.LedgerPort adapter. It wraps the live
// *legacy aggregate so CloseBead routes through closeBeadWithHistoryTrim (history
// trim + brAdapter.CloseBead) and Reopen/Show route straight to brAdapter,
// carrying deps.intentLogDir and deps.brTimeoutCfg internally — byte-identical
// to the pre-port call sites.
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

// ledgerPort returns the production LedgerPort bound to these deps. nil brAdapter
// is impossible on the run path (direct composition enforces it), so no nil-default
// is required here (ports-design §3).
func newLedgerPort(adapter beadLedger, intentLogDir string, timeout brcli.TimeoutConfig, projectDir string, skipHistoryRotation bool) runloop.LedgerPort {
	return daemonLedger{brAdapter: adapter, intentLogDir: intentLogDir, brTimeoutCfg: timeout, projectDir: projectDir, skipBrHistoryRotation: skipHistoryRotation}
}

// emitterPort returns the run shell's EmitterPort (identity over deps.bus).
func newEmitterPort(bus handlercontract.EventEmitter) runloop.EmitterPort {
	return bus
}

// clockOrSystem returns the run shell's ClockPort, folding a nil field to the
// production SystemClock at the READ site. Struct-literal test deps that predate
// the clock field leave it nil; direct composition wires SystemClock in prod. Unlike
// the deleted `if deps.clock == nil { deps.clock = substrate.SystemClock{} }`
// copy-mutation, this NEVER writes the field — it is a pure read. The
// default-folding analog of emitterPort() (RT18).
func clockOrSystem(clock substrate.ClockPort) substrate.ClockPort {
	if clock != nil {
		return clock
	}
	return substrate.SystemClock{}
}

// daemonMerge is the production runloop.MergePort adapter over the RT3 mergeq
// handle: the queue's Submit when non-nil, else inlineMergeSubmit (the nil-queue
// single-beadRunOne fallback). This is the sole owner of that selection, which
// previously lived on the (now-removed) legacy aggregate.mergeSubmitFunc method.
type daemonMerge struct {
	q *mergeq.Queue
}

func (m daemonMerge) Submit() runmerge.Submit {
	if m.q != nil {
		return m.q.Submit
	}
	return runmerge.InlineSubmit
}

// mergePort returns the production MergePort bound to deps.mergeQ.
func newMergePort(mergeQueue *mergeq.Queue) runloop.MergePort {
	return daemonMerge{q: mergeQueue}
}

// daemonGate is the production runloop.GatePort adapter over deps.cpRegistry. A
// nil registry yields registryLoaded=false so the caller emits the exact
// "no ControlPoint registry loaded in daemon" eval-failure Outcome.
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

// gatePort returns the production GatePort bound to deps.cpRegistry.
func newGatePort(registry core.Registry) runloop.GatePort {
	return daemonGate{reg: registry}
}

// daemonWorktree is the production runloop.WorktreePort adapter (RSM-010, RT7):
// it wraps the per-run worktree factory closure assembled in beadRunOne (the
// remote SSHRunner factory when a worker is selected, else
// productionWorktreeFactory). Create is a pass-through onto that closure —
// byte-identical to the pre-port
// `wtFactory(qctx, activeRepo, runID.String(), headSHA)` call site.
type daemonWorktree struct {
	factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)
}

func (w daemonWorktree) Create(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error) {
	return w.factory(ctx, projectDir, runID, headSHA)
}

// worktreePort wraps a per-run worktree factory closure as a runloop.WorktreePort.
// The factory (local or remote) is assembled per-run in beadRunOne where the
// remote-branch context is in scope (RSM-010 / ports-design §6).
func worktreePort(factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)) runloop.WorktreePort {
	return daemonWorktree{factory: factory}
}

// daemonLaunch is the production runloop.LaunchPort adapter (RSM-010, RT7): it
// wraps the resolved launch-spec builder (the routed builder or a test-injected
// one). BuildSpec is a pass-through onto that builder — byte-identical to the
// pre-port `specBuilder(ctx, rc)` call site.
type daemonLaunch struct {
	builder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
}

func (l daemonLaunch) BuildSpec(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return l.builder(ctx, rc)
}

// launchPort wraps a resolved launch-spec builder as a runloop.LaunchPort. The
// builder is assembled per-run in beadRunOne (it needs the routed harness
// registry and the pre-built spec builder), so this is threaded there (RSM-010).
func launchPort(builder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)) runloop.LaunchPort {
	return daemonLaunch{builder: builder}
}

// daemonBudget is the production runloop.BudgetPort adapter (RSM-011): the sole
// run-path use of the queueStore. It folds the review-loop-failure charge
// (LockForMutation → increment ReviewLoopFailures → compare MaxReviewLoopFailures
// → Persist) that previously sat inline in beadRunOne, so the store never
// becomes a run port.
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
	// NQ-B1: resolve the queue BY NAME (not the main-only shim) so a non-"main"
	// queue's budget is read/written against the right queue (hk-tigaf.4).
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

// budgetPort returns the production BudgetPort bound to these deps (RSM-011).
func newBudgetPort(queueStore *queuewiring.QueueStore, projectDir string) runloop.BudgetPort {
	return daemonBudget{queueStore: queueStore, projectDir: projectDir}
}

// daemonRunRegistry is the production runloop.RunRegistryPort adapter over the
// shared *RunRegistry. A wrapper is required (rather than *RunRegistry satisfying
// the port directly) because Get's return type is narrowed from *RunHandle to
// runloop.RunHandlePort, and Go interface satisfaction is invariant in return
// types. It collapses a miss (and a defensive nil handle) to (nil, false) so the
// returned interface is never a typed-nil pointer — the run path's
// `ok && rh != nil` guards stay correct.
type daemonRunRegistry struct {
	reg *RunRegistry
}

func (a daemonRunRegistry) Get(runID core.RunID) (runloop.RunHandlePort, bool) {
	h, ok := a.reg.Get(runID)
	if !ok || h == nil {
		return nil, false
	}
	return h, true
}

// newStaticRunPorts assembles the daemon-wide run ports. Worktree and Launch
// remain per-run values.
func newStaticRunPorts(adapter beadLedger, bus handlercontract.EventEmitter, intentLogDir string, timeout brcli.TimeoutConfig, projectDir string, skipHistoryRotation bool, mergeQueue *mergeq.Queue, registry core.Registry, clock substrate.ClockPort) runloop.RunPorts {
	return runloop.RunPorts{
		Ledger:  newLedgerPort(adapter, intentLogDir, timeout, projectDir, skipHistoryRotation),
		Emitter: newEmitterPort(bus),
		Merge:   newMergePort(mergeQueue),
		Gate:    newGatePort(registry),
		Clock:   clockOrSystem(clock),
	}
}

// runEnv assembles the immutable per-run value bundle: the daemon-level
// configuration read off deps plus the dispatched item's identity and
// per-item overrides passed in by the caller. Every RunEnv field is
// populated — a partially-built bundle is a trap for the next reader — and
// each one is a straight copy of the value the run path read directly
// before, so reaching a value through env.<Field> is byte-identical to the
// pre-bundle deps field access (RSM-010).
//
// itemWorkflow holds the workflow fields exactly as dispatched. The resolver
// owns the tier walk and preserves the raw values for audit.
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

// sharedHandles assembles the cross-goroutine handle bundle (ports-design §3).
// Every field is populated and every one is the same handle — the same pointer,
// the same channel, the same adapter — the run path reached directly off deps
// before, so the bundle shares state by reference exactly as RSM-011 requires
// and reaching a handle through the bundle is byte-identical to the pre-bundle
// deps field access.
//
// TIDGen and EmittedEpics/EmittedEpicsMu are shared by reference like every
// other handle here (RT18.9): the bundle and the outer-loop KEEP sites that
// still read deps.tidGen call the SAME TransitionIDSource, so monotonicity
// (EM-018a) is preserved.
func newSharedHandles(runRegistry *RunRegistry, localInFlight *atomic.Int32, agentSpawnSem chan struct{}, workerRegistry *workers.Registry, queueStore *queuewiring.QueueStore, projectDir string, harnessRegistry *handlercontract.HarnessRegistry, adapterRegistry *handlercontract.AdapterRegistry, hookStore hookStoreIface, substratePort, reviewerSubstrate handler.Substrate, tidGen runloop.TransitionIDSource, emittedEpics map[core.BeadID]struct{}, emittedEpicsMu *sync.Mutex, adapter beadLedger, runner tmuxpkg.CommandRunner, worktreeFactory func(context.Context, string, string, string) (string, func(), error), worktreeCreateMu *sync.Mutex) runloop.SharedHandles {
	return runloop.SharedHandles{
		RunRegistry: daemonRunRegistry{reg: runRegistry}, LocalInFlight: localInFlight,
		AgentSpawnSem: agentSpawnSem, Workers: workerRegistry, Budget: newBudgetPort(queueStore, projectDir),
		HarnessRegistry: harnessRegistry, AdapterRegistry: adapterRegistry, HookStore: hookStore,
		Substrate: substratePort, ReviewerSubstrate: reviewerSubstrate, TIDGen: tidGen,
		EmittedEpics: emittedEpics, EmittedEpicsMu: emittedEpicsMu, BrAdapter: adapter,
		Runner: runner, WorktreeFactory: worktreeFactory, WorktreeCreateMu: worktreeCreateMu,
	}
}

// buildRunBundles resolves the launch builder once for one dispatch.
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
