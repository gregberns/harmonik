// runports.go — RunEnv / RunPorts / SharedHandles adapter + constructor layer.
//
// The port INTERFACES and value/handle BUNDLES themselves (RunPorts, RunEnv,
// SharedHandles, LedgerPort … RunRegistryPort) now live in internal/runloop
// (P2 LIFT chunk L0). What stays here is the daemon-side half of the boundary:
// the concrete adapters (daemonLedger, daemonMerge, daemonGate, daemonBudget,
// daemonWorktree, daemonLaunch, daemonRunRegistry) that close over
// *workLoopDeps and structurally satisfy the runloop.*Port interfaces, plus the
// (*workLoopDeps) constructors that assemble runloop.RunPorts / runloop.RunEnv /
// runloop.SharedHandles. This is the LEGAL direction: daemon → runloop.
//
// Boundary-only: every port is a pass-through onto the exact concrete
// dependency already wired on workLoopDeps. No behavior changes — a port call
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

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/substrate"
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
// *workLoopDeps so CloseBead routes through closeBeadWithHistoryTrim (history
// trim + brAdapter.CloseBead) and Reopen/Show route straight to brAdapter,
// carrying deps.intentLogDir and deps.brTimeoutCfg internally — byte-identical
// to the pre-port call sites.
type daemonLedger struct {
	deps *workLoopDeps
}

func (l daemonLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	return l.deps.brAdapter.ShowBead(ctx, id)
}

func (l daemonLedger) ReopenBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error {
	return l.deps.brAdapter.ReopenBead(ctx, l.deps.intentLogDir, l.deps.brTimeoutCfg, runID, transitionID, beadID, reason)
}

func (l daemonLedger) CloseBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error {
	return l.deps.closeBeadWithHistoryTrim(ctx, runID, transitionID, beadID, needsAttention)
}

// ledgerPort returns the production LedgerPort bound to these deps. nil brAdapter
// is impossible on the run path (newWorkLoopDeps enforces it), so no nil-default
// is required here (ports-design §3).
func (deps *workLoopDeps) ledgerPort() runloop.LedgerPort {
	return daemonLedger{deps: deps}
}

// emitterPort returns the run shell's EmitterPort (identity over deps.bus).
func (deps *workLoopDeps) emitterPort() runloop.EmitterPort {
	return deps.bus
}

// clockOrSystem returns the run shell's ClockPort, folding a nil field to the
// production SystemClock at the READ site. Struct-literal test deps that predate
// the clock field leave it nil; newWorkLoopDeps wires SystemClock in prod. Unlike
// the deleted `if deps.clock == nil { deps.clock = substrate.SystemClock{} }`
// copy-mutation, this NEVER writes the field — it is a pure read. The
// default-folding analog of emitterPort() (RT18).
func (deps *workLoopDeps) clockOrSystem() substrate.ClockPort {
	if deps.clock != nil {
		return deps.clock
	}
	return substrate.SystemClock{}
}

// daemonMerge is the production runloop.MergePort adapter over the RT3 mergeq
// handle: the queue's Submit when non-nil, else inlineMergeSubmit (the nil-queue
// single-beadRunOne fallback). This is the sole owner of that selection, which
// previously lived on the (now-removed) workLoopDeps.mergeSubmitFunc method.
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
func (deps *workLoopDeps) mergePort() runloop.MergePort {
	return daemonMerge{q: deps.mergeQ}
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
func (deps *workLoopDeps) gatePort() runloop.GatePort {
	return daemonGate{reg: deps.cpRegistry}
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
	deps *workLoopDeps
}

// ChargeReviewLoopFailure increments the dispatched item's ReviewLoopFailures
// counter under LockForMutation and reports whether the retry-spend budget is now
// exhausted (>= MaxReviewLoopFailures). Returns false when no queue surface is
// wired (nil queueStore / queueID / groupIndex). Byte-identical to the pre-port
// inline block (hk-c1ah6 / hk-tigaf.4): resolve the queue BY NAME, mutate the
// matching item, persist, and report exhaustion.
func (b daemonBudget) ChargeReviewLoopFailure(ctx context.Context, queueName string, queueID *string, groupIndex *int, itemIndex int, beadID core.BeadID) bool {
	if queueID == nil || groupIndex == nil || itemIndex < 0 || b.deps.queueStore == nil {
		return false
	}
	budgetExhausted := false
	lq := b.deps.queueStore.LockForMutation()
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
		if persistErr := queue.Persist(ctx, b.deps.projectDir, liveQ); persistErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: Persist rl-failures queueID=%s: %v\n",
				liveQ.QueueID, persistErr)
		}
	}
	lq.Done()
	return budgetExhausted
}

// budgetPort returns the production BudgetPort bound to these deps (RSM-011).
func (deps *workLoopDeps) budgetPort() runloop.BudgetPort {
	return daemonBudget{deps: deps}
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

// runPorts assembles the deps-level runloop.RunPorts bundle. Ledger/Emitter/
// Merge/Gate and the Clock are wired here; Worktree and Launch are left nil for
// per-run assembly in beadRunOne (RT7). Byte-identical to reaching the same deps
// fields directly.
func (deps *workLoopDeps) runPorts() runloop.RunPorts {
	return runloop.RunPorts{
		Ledger:  deps.ledgerPort(),
		Emitter: deps.emitterPort(),
		Merge:   deps.mergePort(),
		Gate:    deps.gatePort(),
		Clock:   deps.clockOrSystem(),
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
// ItemWorkflowRef holds the ref exactly as dispatched, BEFORE the EM-012a
// tier-0/tier-1 resolveWorkflowRef resolution that beadRunOne applies to its
// own local. Nothing on the run path may read env.ItemWorkflowRef.
func (deps *workLoopDeps) runEnv(
	runID core.RunID,
	beadRecord core.BeadRecord,
	queueName string,
	queueID *string,
	queueGroupIndex *int,
	queueItemIndex int,
	itemWorkflowMode string,
	itemWorkflowRef string,
	itemTemplateParams map[string]string,
	itemLocalOnly bool,
	itemWorkerTarget string,
	queueDefaultHarness core.AgentType,
) runloop.RunEnv {
	return runloop.RunEnv{
		ProjectDir:   deps.projectDir,
		TargetBranch: deps.targetBranch,
		BrPath:       deps.brPath,

		ProtectBranches: deps.protectBranches,
		AllowedRepos:    deps.allowedRepos,

		WorkflowModeDefault: deps.workflowModeDefault,
		QueueDefaultHarness: queueDefaultHarness,
		DefaultHarness:      deps.defaultHarness,
		ProjectCfg:          deps.projectCfg,

		HandlerBinary:            deps.handlerBinary,
		HandlerArgs:              deps.handlerArgs,
		HandlerEnv:               deps.handlerEnv,
		DaemonBinaryPath:         deps.daemonBinaryPath,
		IntentLogDir:             deps.intentLogDir,
		AgentReadyTimeout:        deps.agentReadyTimeout,
		RemoteAgentReadyTimeout:  deps.remoteAgentReadyTimeout,
		CodexNoWorkDurationFloor: deps.codexNoWorkDurationFloor,
		SandboxCfg:               deps.sandboxCfg,
		BrTimeoutCfg:             deps.brTimeoutCfg,

		RunID:      runID,
		BeadRecord: beadRecord,

		QueueName:       queueName,
		QueueID:         queueID,
		QueueGroupIndex: queueGroupIndex,
		QueueItemIndex:  queueItemIndex,

		ItemWorkflowMode:   itemWorkflowMode,
		ItemWorkflowRef:    itemWorkflowRef,
		ItemTemplateParams: itemTemplateParams,
		ItemLocalOnly:      itemLocalOnly,
		ItemWorkerTarget:   itemWorkerTarget,
	}
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
// still read deps.tidGen dereference the SAME *core.TransitionIDGenerator, so
// monotonicity (EM-018a) is preserved.
func (deps *workLoopDeps) sharedHandles() runloop.SharedHandles {
	return runloop.SharedHandles{
		RunRegistry:   daemonRunRegistry{reg: deps.runRegistry},
		LocalInFlight: deps.localInFlight,
		AgentSpawnSem: deps.agentSpawnSem,
		Workers:       deps.workerRegistry,
		Budget:        deps.budgetPort(),

		HarnessRegistry:   deps.harnessRegistry,
		AdapterRegistry:   deps.adapterRegistry,
		HookStore:         deps.hookStore,
		Substrate:         deps.substrate,
		ReviewerSubstrate: deps.reviewerSubstrate,

		TIDGen:         deps.tidGen,
		EmittedEpics:   deps.emittedEpics,
		EmittedEpicsMu: deps.emittedEpicsMu,

		BrAdapter:        deps.brAdapter,
		Runner:           deps.runner,
		WorktreeFactory:  deps.worktreeFactory,
		WorktreeCreateMu: deps.worktreeCreateMu,
	}
}
