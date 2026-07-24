// Package runloop holds the run machine's boundary contract: the narrow,
// structural PORT interfaces through which the run shell reaches its behavioral
// dependencies, and the two value/handle BUNDLES (RunEnv, SharedHandles, plus
// the RunPorts port bundle) that carry immutable per-run values and
// shared-by-reference cross-goroutine state.
//
// This is the P2 LIFT foundation (plans/2026-07-21-p2-extraction, chunk L0):
// the run-path FILES (beadRunOne, reviewloop, the DOT cascade, the run bridge,
// …) still live in internal/daemon and move here over chunks L1–L9. L0 lifts
// only the shared port/bundle SURFACE they all consume, so those later chunks
// are pure `git mv`s onto a package that already compiles daemon-free.
//
// Direction: daemon → runloop is the only legal edge. runloop is a leaf; it
// must NOT import internal/daemon (depguard fences that edge, and the
// runloop-freeze-gate forbids re-declaring these symbols back in daemon). The
// concrete adapters that satisfy these interfaces (daemonLedger, daemonMerge,
// daemonGate, daemonBudget, daemonRunRegistry, …) and the constructors that
// assemble the bundles stay in internal/daemon, close over *workLoopDeps, and
// are compiler-checked against these interfaces with var _ assertions.
//
// Idiom mirror: internal/keeper/ports.go (structural narrow interfaces;
// EmitterPort = Emitter type alias).
package runloop

import (
	"context"
	"encoding/json"
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
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
)

// BeadLedger is the subset of brcli.Adapter used by the work loop.  It is
// extracted as an interface so that workloop_test.go can substitute a stub.
//
// # Bead body access — architectural note (hk-33tcf / T6 finding F-T6-004)
//
// The work loop intentionally does NOT read the bead body (description field)
// from Beads-SQLite before or after claiming.  The bead body is the agent's
// work brief, not the daemon's.  The daemon's responsibility is lifecycle
// management (Ready → claim → dispatch → close/reopen); interpretation of the
// brief is the handler subprocess's responsibility.
//
// Consequence — handler contract: the handler subprocess is responsible for
// calling `br show <beadID> --format json` to obtain the work spec.  For MVH,
// the bead ID is supplied to the handler via the implementer-protocol brief
// in the SCOPE line (i.e., as content of the prompt passed by the operator to
// claude).  Programmatic injection of the bead ID (e.g. a HARMONIK_BEAD_ID
// env var) is a post-MVH hardening task; no bead exists for that yet.
//
// # ShowBead — pre-claim status guard (hk-p4xbw)
//
// ShowBead is called between Ready and ClaimBead to confirm the bead is still
// "open" before dispatching.  This is the harmonik-side guard against double-
// dispatch when two concurrent work loops both observe the same bead in the
// Ready list.  The guard has a TOCTOU window (another loop could claim between
// Show and Claim), but this is acceptable at MaxConcurrent>1 because the claim
// semaphore (hk-e61c3.3) serialises claims on this daemon to N at a time.
// Cross-daemon double-dispatch (post-MVH multi-daemon) is addressed by the
// deferred upstream br patch (option 2, out of scope for this bead).
type BeadLedger interface {
	Ready(ctx context.Context) ([]core.BeadRecord, error)
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
	ClaimBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID) error
	CloseBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, needsAttention bool) error
	ReopenBead(ctx context.Context, intentLogDir string, cfg brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, beadID core.BeadID, reason string) error
}

// HookStore is the interface over hook-session state used by the work loop
// and waitWithSocketGrace. The concrete *hookSessionStore implements it (its
// embedded *hook.SessionStore promotes every method); tests may supply a
// lightweight stub via workLoopDeps to avoid the 3-second stopHookGrace window.
//
// Bead ref: hk-kqdpf.1.
type HookStore interface {
	RegisterHookSession(runID, claudeSessionID string)
	CloseHookSession(runID, claudeSessionID string)
	LatestOutcome(runID, claudeSessionID string) *json.RawMessage
	WaitForOutcome(ctx context.Context, runID, claudeSessionID string) (json.RawMessage, error)

	// SetAgentReadyCallback registers a callback that is called (once) when the
	// daemon socket receives an agent_ready relay message for (runID,
	// claudeSessionID). The callback is invoked from the socket-acceptor goroutine
	// and MUST be non-blocking. Used by the work loop to forward relay-synthesized
	// agent_ready into the per-run event tap so waitAgentReady can observe it
	// (CHB-013 / HC-039).
	SetAgentReadyCallback(runID, claudeSessionID string, cb func())
}

// EmitterPort is the event-emission surface of the run shell. It is a type
// alias for the production emitter interface (the keeper ports.go trick):
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

// MergePort is the merge exclusion-domain surface of the run path (RSM-015). It
// exposes the strictly-FIFO single-owner submit entry point that serialises the
// commit-phase merge, the post-merge escaped-worktree check, and the remote
// base-sync + worktree-add.
type MergePort interface {
	// Submit returns the exclusion-domain entry point (mergeq.Queue.Submit when a
	// queue is wired, else the inline nil-queue fallback).
	Submit() runmerge.Submit
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

// RunHandlePort is the consumer-defined surface the run path uses to update the
// live RunHandle for its OWN run_id (LIFT crit 4). The run path reaches a handle
// only through RunRegistryPort.Get and performs exactly these six run-scoped
// operations; narrowing to this interface means the run path names neither the daemon
// *RunHandle type nor its unexported `aborted` field, so it can compile in
// package runloop without importing daemon. *RunHandle is the production
// adapter (structural satisfaction) — see the var _ assertion beside the adapter.
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
	HookStore         HookStore
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
	BrAdapter        BeadLedger
	Runner           tmuxpkg.CommandRunner
	WorktreeFactory  func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)
	WorktreeCreateMu *sync.Mutex
}
