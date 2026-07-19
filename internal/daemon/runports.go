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
	"sync/atomic"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queue"
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

// MergePort is the merge exclusion-domain surface of the run path (RSM-015). It
// exposes the strictly-FIFO single-owner submit entry point that serialises the
// commit-phase merge, the post-merge escaped-worktree check, and the remote
// base-sync + worktree-add.
type MergePort interface {
	// Submit returns the exclusion-domain entry point (mergeq.Queue.Submit when a
	// queue is wired, else the inline nil-queue fallback).
	Submit() mergeSubmit
}

// daemonMerge is the production MergePort adapter over the RT3 mergeq handle: the
// queue's Submit when non-nil, else inlineMergeSubmit (the nil-queue
// single-beadRunOne fallback). This is the sole owner of that selection, which
// previously lived on the (now-removed) workLoopDeps.mergeSubmitFunc method.
type daemonMerge struct {
	q *mergeq.Queue
}

func (m daemonMerge) Submit() mergeSubmit {
	if m.q != nil {
		return m.q.Submit
	}
	return inlineMergeSubmit
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
	BuildSpec(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)
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
	builder func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)
}

func (l daemonLaunch) BuildSpec(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return l.builder(ctx, rc)
}

// launchPort wraps a resolved launch-spec builder as a LaunchPort. The builder
// is assembled per-run in beadRunOne (it needs the routed harness registry and
// the pre-built spec builder), so this is threaded there (RSM-010).
func launchPort(builder func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)) LaunchPort {
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

// RunPorts is the behavioral-dependency bundle of the run shell (ports-design
// §1). Narrow, structural. beadRunOne and the reviewloop/dot helpers reach their
// daemon dependencies through this bundle rather than the raw workLoopDeps.
//
// Worktree and Launch are assembled per-run inside beadRunOne (they need the
// resolved remote-branch context and the pre-built routed spec builder); the
// deps-level runPorts constructor leaves them nil, and RT7 threads them.
type RunPorts struct {
	Ledger   LedgerPort
	Emitter  EmitterPort
	Worktree WorktreePort
	Merge    MergePort
	Launch   LaunchPort
	Gate     GatePort
	Clock    substrate.ClockPort
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
	ProjectCfg          ProjectConfig

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
// the worker registry, and the review-loop-failure budget port.
type SharedHandles struct {
	RunRegistry   *RunRegistry
	LocalInFlight *atomic.Int32
	AgentSpawnSem chan struct{}
	Workers       *workers.Registry
	Budget        BudgetPort
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
		Clock:   deps.clock,
	}
}
