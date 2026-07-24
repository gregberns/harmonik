// runports.go — RunEnv / RunPorts / SharedHandles boundary bundles (RSM-010).
//
// C3 of the run-state-machine work (see .kerf/works/2026-07-14-run-state-machine
// /04-design/ports-design.md §1). This file introduces the narrow, structural
// ports through which the run shell reaches its behavioral dependencies, plus
// the two value/handle bundles (RunEnv, SharedHandles) that carry immutable
// per-run values and shared-by-reference cross-goroutine state respectively.
//
// Boundary-only: every port is a pass-through onto the exact concrete
// dependency already wired on workLoopDeps. No behavior changes — a port call
// resolves to the same underlying method the run path invoked directly before
// (RSM-010). The nil-default adapters preserve today's nil-means-production
// defaulting exactly (ports-design §3).
//
// Idiom mirror: internal/keeper/ports.go (structural narrow interfaces;
// EmitterPort = Emitter type alias, keeper ports.go:107).

package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
)

// EmitterPort is the event-emission surface of the run shell. It is a type
// alias for the production emitter interface (the keeper ports.go:107 trick):
// deps.bus already satisfies it, so wiring is identity.
type EmitterPort = handlercontract.EventEmitter

// LedgerPort is the Beads-ledger surface of the run path. It hides the ambient
// intentLogDir / brTimeoutCfg plumbing (carried internally by the adapter) and
// folds the pre-close .br_history trim into CloseBead (closeBeadWithHistoryTrim),
// so callers pass only the run-scoped identifiers.
type LedgerPort interface {
	// ShowBead returns the bead record (edges included) for id.
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
	// ReopenBead reopens beadID with reason, under the run's transition id.
	ReopenBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error
	// CloseBead trims .br_history then closes beadID (closeBeadWithHistoryTrim).
	CloseBead(ctx context.Context, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error
}

// daemonLedger is the production LedgerPort adapter. It wraps the live
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
func (deps *workLoopDeps) ledgerPort() LedgerPort {
	return daemonLedger{deps: deps}
}

// emitterPort returns the run shell's EmitterPort (identity over deps.bus).
func (deps *workLoopDeps) emitterPort() EmitterPort {
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

// MergePort is the merge exclusion-domain surface of the run path (RSM-015). It
// exposes the strictly-FIFO single-owner submit entry point that serialises the
// commit-phase merge, the post-merge escaped-worktree check, and the remote
// base-sync + worktree-add.
type MergePort interface {
	// Submit returns the exclusion-domain entry point (mergeq.Queue.Submit when a
	// queue is wired, else the inline nil-queue fallback).
	Submit() runmerge.Submit
}

// daemonMerge is the production MergePort adapter over the RT3 mergeq handle: the
// queue's Submit when non-nil, else inlineMergeSubmit (the nil-queue
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
func (deps *workLoopDeps) mergePort() MergePort {
	return daemonMerge{q: deps.mergeQ}
}

// GatePort is the DOT gate-node evaluation surface: it resolves a gate_ref to a
// Gate ControlPoint via the daemon's ControlPoint registry.
type GatePort interface {
	// LookupGate resolves gateRef. registryLoaded is false when no registry is
	// wired (nil cpRegistry); ok is false when gateRef is absent from a loaded
	// registry. The Kind check stays at the call site so the eval-failure reason
	// strings remain byte-identical to the pre-port path (ports-design §3).
	LookupGate(gateRef core.GateRef) (cp core.ControlPoint, ok bool, registryLoaded bool)
}

// daemonGate is the production GatePort adapter over deps.cpRegistry. A nil
// registry yields registryLoaded=false so the caller emits the exact
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
func (deps *workLoopDeps) gatePort() GatePort {
	return daemonGate{reg: deps.cpRegistry}
}

// WorktreePort creates the per-run worktree (local or on a remote worker) and
// returns its absolute path plus a cleanup func. Its production nil-default
// (deps.worktreeFactory == nil ⇒ productionWorktreeFactory for a local run, or
// the remote SSHRunner factory when a worker is selected) is assembled per-run
// inside beadRunOne where the remote-branch context is in scope; RT7 lifts that
// assembly onto this port. Declared here as part of the RSM-010 boundary.
type WorktreePort interface {
	Create(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)
}

// LaunchPort is the (wide) agent-launch surface: launch-spec build, agent spawn
// (substrate), harness/adapter registries, hook-store outcome wait, brief
// delivery, agent-ready timeouts, and sandbox. Every run mode uses the cluster
// together (ports-design §6), so it is one port. Its production nil-defaulting
// (nil launchSpecBuilder ⇒ routedLaunchSpecBuilder/buildClaudeLaunchSpec; nil
// hookStore ⇒ skip WaitForOutcome; nil harnessRegistry ⇒ builder fallthrough)
// is assembled per-run in beadRunOne today; RT7 lifts it onto this port.
type LaunchPort interface {
	// BuildSpec builds the handler LaunchSpec + artifacts for a run (the
	// launchSpecBuilder surface).
	BuildSpec(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
}

// BudgetPort is the one-method wrap of the queueStore review-loop-failure budget
// mutation (RSM-011): the ONLY run-path queueStore use. It hides QueueStore from
// the run path so the store does not become a run port.
type BudgetPort interface {
	// ChargeReviewLoopFailure increments the dispatched item's ReviewLoopFailures
	// counter under LockForMutation and returns true when the retry-spend budget
	// is now exhausted (>= queue.MaxReviewLoopFailures). Returns false when no
	// queue surface is wired.
	ChargeReviewLoopFailure(ctx context.Context, queueName string, queueID *string, groupIndex *int, itemIndex int, beadID core.BeadID) bool
}

// daemonWorktree is the production WorktreePort adapter (RSM-010, RT7): it wraps
// the per-run worktree factory closure assembled in beadRunOne (the remote
// SSHRunner factory when a worker is selected, else productionWorktreeFactory).
// Create is a pass-through onto that closure — byte-identical to the pre-port
// `wtFactory(qctx, activeRepo, runID.String(), headSHA)` call site.
type daemonWorktree struct {
	factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)
}

func (w daemonWorktree) Create(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error) {
	return w.factory(ctx, projectDir, runID, headSHA)
}

// worktreePort wraps a per-run worktree factory closure as a WorktreePort. The
// factory (local or remote) is assembled per-run in beadRunOne where the
// remote-branch context is in scope (RSM-010 / ports-design §6).
func worktreePort(factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)) WorktreePort {
	return daemonWorktree{factory: factory}
}

// daemonLaunch is the production LaunchPort adapter (RSM-010, RT7): it wraps the
// resolved launch-spec builder (the routed builder or a test-injected one).
// BuildSpec is a pass-through onto that builder — byte-identical to the pre-port
// `specBuilder(ctx, rc)` call site.
type daemonLaunch struct {
	builder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
}

func (l daemonLaunch) BuildSpec(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return l.builder(ctx, rc)
}

// launchPort wraps a resolved launch-spec builder as a LaunchPort. The builder
// is assembled per-run in beadRunOne (it needs the routed harness registry and
// the pre-built spec builder), so this is threaded there (RSM-010).
func launchPort(builder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)) LaunchPort {
	return daemonLaunch{builder: builder}
}

// daemonBudget is the production BudgetPort adapter (RSM-011): the sole run-path
// use of the queueStore. It folds the review-loop-failure charge (LockForMutation
// → increment ReviewLoopFailures → compare MaxReviewLoopFailures → Persist) that
// previously sat inline in beadRunOne, so the store never becomes a run port.
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
func (deps *workLoopDeps) budgetPort() BudgetPort {
	return daemonBudget{deps: deps}
}

// RunHandlePort is the consumer-defined surface the run path uses to update the
// live RunHandle for its OWN run_id (LIFT crit 4). The run path reaches a handle
// only through RunRegistryPort.Get and performs exactly these six run-scoped
// operations; narrowing to this interface means the run path names neither the daemon
// *RunHandle type nor its unexported `aborted` field, so it can compile in a
// future package runloop without importing daemon. *RunHandle is the production
// adapter (structural satisfaction) — see the var _ assertion below.
type RunHandlePort interface {
	// SetOwningEpic stamps the resolved parent-epic attribution onto the handle
	// (plain field writes, byte-identical to the pre-port assignment).
	SetOwningEpic(id, assignee string)
	// SetResolvedProvider records the resolved Pi provider identity (per-provider
	// slot accounting).
	SetResolvedProvider(provider string)
	// SetRemote marks the run as routed to a remote worker so LenForQueueLocal
	// stops counting it against the per-queue local cap (hk-4tjt6).
	SetRemote(remote bool)
	// SetAgentType records the resolved agent type once the launch-spec builder
	// resolves the harness (PI-073).
	SetAgentType(at core.AgentType)
	// SetMachine attaches the per-session lifecycle FSM after a successful
	// handler.Launch (HC-064..HC-067).
	SetMachine(m *hclifecycle.Machine)
	// Aborted reports whether the never-spawned reaper marked this run aborted
	// before cancelling its context (hk-0z5x). Maps to aborted.Load() — the
	// accessor that lifts the daemon-private `aborted` field across the port.
	Aborted() bool
}

// RunRegistryPort is the consumer-defined run-registry surface of the run path
// (LIFT crit 4). The run path looks up ONLY its own run's handle, and only to
// mutate it via RunHandlePort — so a single Get is the whole registry surface the
// run path needs. Returning RunHandlePort (not the concrete *RunHandle) is what
// actually breaks the run path's dependency on the daemon handle type: a bare Get
// returning *RunHandle would drag daemon internals (incl. the unexported
// `aborted` field) across the boundary and silently defeat the LIFT (concern #2).
// daemonRunRegistry is the production adapter over the shared *RunRegistry.
type RunRegistryPort interface {
	Get(runID core.RunID) (RunHandlePort, bool)
}

// daemonRunRegistry is the production RunRegistryPort adapter over the shared
// *RunRegistry. A wrapper is required (rather than *RunRegistry satisfying the
// port directly) because Get's return type is narrowed from *RunHandle to
// RunHandlePort, and Go interface satisfaction is invariant in return types. It
// collapses a miss (and a defensive nil handle) to (nil, false) so the returned
// interface is never a typed-nil pointer — the run path's `ok && rh != nil`
// guards stay correct.
type daemonRunRegistry struct {
	reg *RunRegistry
}

func (a daemonRunRegistry) Get(runID core.RunID) (RunHandlePort, bool) {
	h, ok := a.reg.Get(runID)
	if !ok || h == nil {
		return nil, false
	}
	return h, true
}

var (
	_ RunHandlePort   = (*RunHandle)(nil)
	_ RunRegistryPort = daemonRunRegistry{}
)

// RunPorts is the behavioral-dependency bundle of the run shell (ports-design
// §1). Narrow, structural. beadRunOne and the reviewloop/dot helpers reach their
// daemon dependencies through this bundle rather than the raw workLoopDeps.
//
// Worktree, Launch and LaunchBuilder are assembled per-run inside beadRunOne
// (they need the resolved remote-branch context and the pre-built routed spec
// builder); the deps-level runPorts constructor leaves them nil, and RT7 threads
// them.
type RunPorts struct {
	Ledger   LedgerPort
	Emitter  EmitterPort
	Worktree WorktreePort
	Merge    MergePort
	Launch   LaunchPort
	Gate     GatePort
	Clock    substrate.ClockPort

	// LaunchBuilder is the raw resolved spec builder (a reassignable func, NOT a
	// port interface) — the channel that replaces the by-value raw-field smuggle:
	// beadRunOne resolves it once and threads it here so the review-loop / DOT
	// sub-drivers reach it via ports.LaunchBuilder after the RT18 deps drop,
	// instead of through deps.launchBuilder() (RT18.11).
	LaunchBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
}

// RunEnv carries the immutable per-run values (no behavior) — the daemon-level
// configuration plus the dispatched item's identity and overrides.
type RunEnv struct {
	ProjectDir   string
	TargetBranch string
	BrPath       string

	ProtectBranches []string
	AllowedRepos    []string

	WorkflowModeDefault core.WorkflowMode
	DefaultHarness      core.AgentType
	ProjectCfg          projectconfig.ProjectConfig

	// Immutable daemon-level launch/handler config, straight copies of the
	// same-named workLoopDeps fields (RT18-W). Populated in runEnv(); the run
	// path reads them here after the RT18 signature drop replaces deps with env.
	HandlerBinary             string
	HandlerArgs               []string
	HandlerEnv                []string
	DaemonBinaryPath          string
	IntentLogDir              string
	AgentReadyTimeout         time.Duration
	RemoteAgentReadyTimeout   time.Duration
	PostAgentReadyHangTimeout time.Duration
	CodexNoWorkDurationFloor  time.Duration
	SandboxCfg                projectconfig.SandboxConfig
	BrTimeoutCfg              brcli.TimeoutConfig

	RunID      core.RunID
	BeadRecord core.BeadRecord

	QueueName       string
	QueueID         *string
	QueueGroupIndex *int
	QueueItemIndex  int

	ItemWorkflowMode   string
	ItemWorkflowRef    string
	ItemTemplateParams map[string]string
	ItemLocalOnly      bool
	ItemWorkerTarget   string
}

// SharedHandles is the cross-goroutine state shared by reference (ports-design
// §3): the run registry, the local-in-flight counter, the agent-spawn semaphore,
// the worker registry, the review-loop-failure budget port, and the harness/
// substrate/hook registries the run path launches agents through. Every field is
// a straight by-reference copy of a deps field — a registry pointer, a substrate
// handle, the hook-session store — so reaching one through the bundle is
// byte-identical to the pre-bundle deps access (RT18-W widening; adding a field
// to an existing bundle is not a new seam per _plan.md §1, only a new PORT
// INTERFACE would be).
type SharedHandles struct {
	RunRegistry   RunRegistryPort
	LocalInFlight *atomic.Int32
	AgentSpawnSem chan struct{}
	Workers       *workers.Registry
	Budget        BudgetPort

	HarnessRegistry   *handlercontract.HarnessRegistry
	AdapterRegistry   *handlercontract.AdapterRegistry
	HookStore         hookStoreIface
	Substrate         handler.Substrate
	ReviewerSubstrate handler.Substrate

	// TIDGen is the single shared TransitionID generator, shared by reference so
	// beadRunOne's monotonicity guarantee (EM-018a) holds — the bundle and the
	// outer-loop KEEP sites (runWorkLoop, adoptLiveRunSession) dereference the
	// SAME *core.TransitionIDGenerator (RT18.9).
	TIDGen *core.TransitionIDGenerator

	// EmittedEpics / EmittedEpicsMu are the epic_completed dedupe set and its
	// guard, shared by reference so maybeEmitEpicCompleted sees every prior run's
	// emissions across goroutines (RT18.9, precondition for the runBridge drop).
	EmittedEpics   map[core.BeadID]struct{}
	EmittedEpicsMu *sync.Mutex

	// beadRunOne-only shared handles (RT18.10): the beads adapter, the default
	// CommandRunner factory, the worktree factory func and its creation mutex —
	// each a by-reference copy of the same-named deps field.
	BrAdapter        beadLedger
	Runner           tmuxpkg.CommandRunner
	WorktreeFactory  func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)
	WorktreeCreateMu *sync.Mutex
}

// runPorts assembles the deps-level RunPorts bundle. Ledger/Emitter/Merge/Gate
// and the Clock are wired here; Worktree and Launch are left nil for per-run
// assembly in beadRunOne (RT7). Byte-identical to reaching the same deps fields
// directly.
func (deps *workLoopDeps) runPorts() RunPorts {
	return RunPorts{
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
) RunEnv {
	return RunEnv{
		ProjectDir:   deps.projectDir,
		TargetBranch: deps.targetBranch,
		BrPath:       deps.brPath,

		ProtectBranches: deps.protectBranches,
		AllowedRepos:    deps.allowedRepos,

		WorkflowModeDefault: deps.workflowModeDefault,
		DefaultHarness:      deps.defaultHarness,
		ProjectCfg:          deps.projectCfg,

		HandlerBinary:             deps.handlerBinary,
		HandlerArgs:               deps.handlerArgs,
		HandlerEnv:                deps.handlerEnv,
		DaemonBinaryPath:          deps.daemonBinaryPath,
		IntentLogDir:              deps.intentLogDir,
		AgentReadyTimeout:         deps.agentReadyTimeout,
		RemoteAgentReadyTimeout:   deps.remoteAgentReadyTimeout,
		PostAgentReadyHangTimeout: deps.postAgentReadyHangTimeout,
		CodexNoWorkDurationFloor:  deps.codexNoWorkDurationFloor,
		SandboxCfg:                deps.sandboxCfg,
		BrTimeoutCfg:              deps.brTimeoutCfg,

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
func (deps *workLoopDeps) sharedHandles() SharedHandles {
	return SharedHandles{
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
