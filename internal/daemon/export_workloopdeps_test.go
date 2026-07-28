package daemon

// export_workloopdeps_test.go — workLoopDeps test-seam constructors for internal/daemon.
//
// Split out of export_test.go (RT19.1) so the five workLoopDeps shims that form
// RT15's entire future edit surface live in one bounded file: 157 daemon_test
// files reference them, so isolating them means RT15 later edits this ~560-line
// file rather than the 2,895-line export_test.go. Same package (daemon), so all
// daemon_test callers resolve daemon.ExportedX byte-identically.
//
// Bead: hk-ecrxy.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon/bootconfig"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/workers"
)

// WorkLoopDepsParams carries the parameters for ExportedWorkLoopDeps so callers
// can supply only the fields they care about; zero values use safe defaults.
type WorkLoopDepsParams struct {
	// BrAdapter is the stub bead ledger.  Required.
	BrAdapter beadLedger

	// Bus is the event collector.  Required.
	Bus handlercontract.EventEmitter

	// ProjectDir is the repo root.  Required.
	ProjectDir string

	// HandlerBinary is the binary to spawn.  Required.
	HandlerBinary string

	// HandlerArgs are extra args forwarded to the binary.  May be nil.
	HandlerArgs []string

	// IntentLogDir is the beads-intents directory path.  Required.
	IntentLogDir string

	// WorkflowModeDefault is the daemon-level default workflow mode per
	// PL-004a.  Zero value is normalised to WorkflowModeSingle in
	// ExportedWorkLoopDeps, mirroring daemon.Start step 0 behaviour.
	//
	// Bead ref: hk-7om2q.8.
	WorkflowModeDefault core.WorkflowMode

	// MaxConcurrent is the ceiling on simultaneously in-flight bead goroutines.
	// Zero value is normalised to 1 (single-threaded default) mirroring
	// newWorkLoopDeps behaviour. Set to >1 to exercise concurrent dispatch in
	// tests (hk-e61c3.2).
	MaxConcurrent int

	// RunRegistry is the in-flight run registry for the work loop. When nil,
	// ExportedWorkLoopDeps creates a fresh NewRunRegistry(). Supply an explicit
	// registry when the test needs to inspect or control it directly.
	//
	// Bead ref: hk-e61c3.2.
	RunRegistry *RunRegistry

	// AdapterRegistry is the sealed adapter registry forwarded into
	// handler.NewHandler as a latent seam (hk-gql20.16). When nil,
	// ExportedWorkLoopDeps creates a fresh empty registry — tests do not
	// need adapters registered because Launch does not consult the registry.
	AdapterRegistry *handlercontract.AdapterRegistry

	// HookStore is the hook-session store injected into the work loop for
	// RegisterHookSession / CloseHookSession / WaitForOutcome calls (hk-gql20.21,
	// hk-kqdpf.1).
	//
	// When nil, ExportedWorkLoopDeps installs a real hookSessionStore (via
	// newHookSessionStore). Shell-fixture tests whose handlers exit without a
	// real Stop-hook relay will hit the 3-second stopHookGrace window in
	// waitWithSocketGrace before proceeding on exit code.
	//
	// Supply an explicit *hookSessionStore (via ExportedNewHookSessionStore) for
	// tests that need to observe or control hook-relay routing directly.
	//
	// Bead ref: hk-gql20.21, hk-kqdpf.1, hk-ngw3d.
	HookStore hookStoreIface

	// AdapterRegistry2 is the sealed adapter registry forwarded to beadRunOne
	// for waitAgentReady (hk-gql20.14). Named AdapterRegistry2 to avoid
	// collision with the existing AdapterRegistry field (used for
	// handler.NewHandler). MUST be non-nil (hk-d8u1y deleted the nil-guard);
	// tests should use NewSealedAdapterRegistryForTest(t) for an empty-but-sealed
	// registry that satisfies the production precondition.
	//
	// Bead ref: hk-gql20.14; hk-d8u1y.
	AdapterRegistry2 *handlercontract.AdapterRegistry

	// Substrate is the optional tmux substrate for handler.Launch (hk-gql20.14).
	// Defaults to nil.
	//
	// Bead ref: hk-gql20.14.
	Substrate handler.Substrate

	// AgentReadyTimeout is the HC-056 timeout for waitAgentReady (hk-gql20.14).
	// Zero → runlaunch.DefaultAgentReadyTimeout (30s).
	//
	// Bead ref: hk-gql20.14.
	AgentReadyTimeout time.Duration

	// PostAgentReadyHangTimeout is the hang-detection timeout for the
	// post-agent_ready progress detector (hk-a2okh).
	// Zero → defaultPostAgentReadyHangTimeout (7 min).
	//
	// Bead ref: hk-a2okh.
	PostAgentReadyHangTimeout time.Duration

	// CPRegistry, when non-nil, is the ControlPoint registry used to resolve
	// gate_ref values during DOT workflow gate-node dispatch (hk-karlz). When
	// nil, gate nodes return a structural eval-failure Outcome without crashing.
	// Tests that exercise gate dispatch must supply a populated registry.
	//
	// Bead ref: hk-karlz.
	CPRegistry core.Registry

	// ProjectCfg is the decoded .harmonik/config.yaml for EM-012b tier-2 resolution.
	// The zero value is safe: LookupAgent returns ("","") for all agent types.
	//
	// Bead ref: hk-bfvk7.
	ProjectCfg projectconfig.ProjectConfig

	// HarnessRegistry, when non-nil, is forwarded into workLoopDeps.harnessRegistry
	// as the per-agent-type Harness route table. When nil, harnessRegistry is left
	// nil in workLoopDeps — the completion-mode check defaults to
	// CompletionEventStreamThenQuit (backward-compat: waitAgentReady always runs).
	//
	// Supply a non-nil value to exercise the hk-f6g7 ProcessExit-skips-waitAgentReady
	// path (codex harness) without using the production newHarnessRegistry wiring.
	//
	// Bead ref: hk-f6g7.
	HarnessRegistry *handlercontract.HarnessRegistry

	// LaunchSpecBuilder, when non-nil, overrides the buildClaudeLaunchSpec
	// function called by beadRunOne. When nil, the production buildClaudeLaunchSpec
	// is used (via the nil-guard in beadRunOne). Tests that use this path must
	// have projectDir pointing at a real git repository so that
	// productionWorktreeFactory can create a worktree before LaunchSpecBuilder
	// writes into it.
	//
	// Supply an explicit builder only to override specific CHB-001..005 / CHB-024
	// behaviours; prefer nil (production path) for correctness.
	//
	// Bead ref: hk-kqdpf.1, hk-ngw3d.
	LaunchSpecBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)

	// WorktreeFactory, when non-nil, overrides the worktree creation function
	// in beadRunOne. When nil, the production productionWorktreeFactory (real
	// git worktree) is used via the nil-guard in beadRunOne. Tests must therefore
	// have projectDir pointing at a real git repository with at least one commit
	// so that `git worktree add` succeeds.
	//
	// Supply an explicit factory only to intercept or wrap worktree creation
	// (e.g. mergeToMainCommittingFactory for merge-to-main tests).
	//
	// Bead ref: hk-kqdpf.1, hk-ngw3d.
	WorktreeFactory func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)

	// QueueStore, when non-nil, enables the queue-pull dispatch path in
	// runWorkLoop per execution-model.md §7.4 (TS-1). When nil the loop uses
	// the br-ready poll fallback (backward-compat for tests that don't use queues).
	//
	// Bead ref: hk-45ude.
	QueueStore *queuewiring.QueueStore

	// QueueLedger, when non-nil, is the queue.BeadLedger seam the dispatch loop
	// uses to re-evaluate deferred-for-ledger-dep items on every tick (§2.8).
	// Tests that exercise ledger-dep deferral/un-deferral inject a fake here.
	// When nil the re-evaluation pass no-ops (queue.ReevaluateDeferred returns
	// early on a nil ledger).
	//
	// Bead ref: hk-nbjht.
	QueueLedger queue.BeadLedger

	// CancelOnQueueDrain, when non-nil, is called once after the queue
	// transitions to all-success and ClearQueue completes.  Mirrors
	// daemon.Config.CancelOnQueueDrain; used by hk-icecw tests to verify
	// exit-on-empty behaviour without process-level signals.
	//
	// Bead ref: hk-icecw.
	CancelOnQueueDrain context.CancelFunc

	// CancelOnQueueExit, when non-nil, is called once when the queue reaches
	// any terminal state (all-success or paused-by-failure).  Mirrors
	// daemon.Config.CancelOnQueueExit; used by hk-8jh26 tests to verify
	// exit-on-failure behaviour.
	//
	// Bead ref: hk-8jh26.
	CancelOnQueueExit context.CancelFunc

	// StopDispatchCtx, when non-nil, is used by the work loop's outer poll to
	// halt dispatch without cancelling in-flight goroutines (hk-2o2i9). Mirrors
	// daemon.Config.StopDispatchCtx. When nil the loop falls back to the ctx
	// passed to runWorkLoop (backward-compat).
	//
	// Bead ref: hk-2o2i9.
	StopDispatchCtx context.Context //nolint:containedctx // hk-2o2i9: mirrors daemon.Config.StopDispatchCtx, which is a context by design.

	// HandlerPauseController, when non-nil, is wired into the work loop to
	// enable the skip-on-paused dispatch gate (hk-kac8g).  When nil the gate
	// is disabled: all items are dispatched regardless of handler pause state.
	//
	// Bead ref: hk-kac8g, hk-m0k0a.
	HandlerPauseController *HandlerPauseController

	// StaleBlockerCloser, when non-nil, enables the claim-failure auto-close
	// path (hk-rnsjs). When nil the behaviour is disabled (safe default for
	// tests that do not exercise this path).
	//
	// Bead ref: hk-rnsjs.
	StaleBlockerCloser lifecycle.BeadCat3cCloser

	// StrandedInProgressResetter, when non-nil, enables the stranded-bead
	// auto-reset path (hk-l2xd1). When nil the behaviour is disabled (safe
	// default for tests that do not exercise this path).
	//
	// Bead ref: hk-l2xd1.
	StrandedInProgressResetter strandedInProgressResetter

	// StrandedResetDaemonNS is the daemon-session epoch threaded into ResetBead
	// idempotency keys when StrandedInProgressResetter is set (hk-l2xd1).
	// Zero is valid (becomes the epoch at ExportedWorkLoopDeps call time in
	// production; tests that check the idempotency key shape set this explicitly).
	//
	// Bead ref: hk-l2xd1.
	StrandedResetDaemonNS int64

	// OperatorPauseCtrl, when non-nil, gates br-ready dispatch on operator
	// pause state (hk-ry8q1). When nil the gate is disabled.
	//
	// Bead ref: hk-ry8q1.
	OperatorPauseCtrl *OperatorPauseController

	// DecisionBlocker, when non-nil, is checked at every dispatch attempt to
	// gate dispatch for beads blocked by an unacknowledged decision_required
	// event (EV-043). When nil the gate is disabled (backward-compat default).
	//
	// Spec ref: specs/event-model.md §4.12 EV-043.
	// Bead ref: hk-a6e24.
	DecisionBlocker *DecisionBlocker

	// NoAutoPull, when true, disables the br-ready fallback poll path so the
	// work loop only dispatches via the queue surface (EM-066). The zero value
	// (false) preserves the existing test default: br-ready fallback enabled.
	// Set to true to test the quiet-daemon (queue-only) topology.
	//
	// Bead ref: hk-h5lv2 (EM-066 scenario test).
	NoAutoPull bool

	// ConcurrencyCtrl, when non-nil, replaces the static MaxConcurrent with a
	// runtime-mutable controller that tests can adjust mid-run (hk-ohiaf). When
	// nil the static MaxConcurrent field is used (backward-compat).
	//
	// Bead ref: hk-ohiaf.
	ConcurrencyCtrl *ConcurrencyController

	// TargetBranch is the branch merged into by lockedMergeRunBranchToMain.
	// Empty string is normalised to "main" (same as newWorkLoopDeps).
	//
	// Bead ref: hk-6r6xv.
	TargetBranch string

	// ProtectBranches is the set of branches the merge guard refuses to target.
	// Nil/empty disables the guard (no branch is protected).
	//
	// Bead ref: hk-6r6xv.
	ProtectBranches []string

	// MergeQueue, when non-nil, is the merge exclusion domain (RSM-015) that
	// concurrent beadRunOne goroutines serialise their commit-phase merge, escape
	// check, and base-sync+worktree-add through. It MUST already be Start()ed by
	// the test, which owns its lifecycle (runWorkLoop leaves an injected queue
	// untouched).
	//
	// When nil: a test that calls beadRunOne DIRECTLY runs the merge critical
	// section inline (correct for a single beadRunOne). A test that drives
	// ExportedRunWorkLoop instead gets a queue runWorkLoop creates, starts, and
	// owns — so a concurrent ExportedRunWorkLoop test needs neither this field nor
	// an inline-race workaround. Inject a started queue here only when driving
	// concurrent beadRunOne WITHOUT runWorkLoop (hk-4f5ua / hk-bnm89).
	MergeQueue *mergeq.Queue

	// WorktreeCreateMu, when non-nil, is threaded into WorktreeRootConfig for
	// remote bead runs so that workspace.CreateWorktree serialises the
	// git-worktree-add + HEAD-resolve retry loop (hk-5qp7z). When nil,
	// ExportedWorkLoopDeps installs a fresh mutex (mirrors production default).
	WorktreeCreateMu *sync.Mutex

	// AgentSpawnSem, when non-nil, is the per-worker cold-start spawn semaphore
	// (cap 3) that bounds concurrent remote claude cold-starts (hk-5z1f0). When
	// nil, ExportedWorkLoopDeps installs a fresh cap-3 channel (production default).
	AgentSpawnSem chan struct{}

	// WorkerRegistry, when non-nil, enables the DD1 remote code-sync path in
	// beadRunOne (remote-substrate B8). When nil (the test default), all runs
	// take the local path: no SSH fetch/push steps are inserted.
	//
	// Bead ref: hk-rs-b8-codesync-3fk0.
	WorkerRegistry *workers.Registry

	// BrPath is the absolute path to the `br` CLI binary used by the staged-bead
	// generator (hk-f722). When empty the generator is disabled (safe test default).
	BrPath string

	// SpawnSubstrateReadyCh, when non-nil, is forwarded to
	// workLoopDeps.spawnSubstrateReadyCh so tests can assert that dispatch is
	// gated on spawn-substrate readiness after a simulated restart-backoff boot
	// (hk-bk33). When nil the gate is disabled (safe default for tests that do
	// not exercise this path).
	//
	// Bead ref: hk-bk33.
	SpawnSubstrateReadyCh <-chan struct{}

	// AllowedRepos is the safelist of absolute repository paths the daemon is
	// permitted to dispatch cross-repo beads against (hk-xfuc). When nil/empty
	// no cross-repo dispatch is permitted. Supply a non-empty list to test the
	// cross-repo dispatch path (beads declaring target_repo in ## Branching).
	//
	// Bead ref: hk-xfuc.
	AllowedRepos []string

	// DiskFreeBytesFunc, when non-nil, overrides the diskFreeBytes call inside
	// runPeriodicDiskCheck. Tests use this to control the apparent free-space
	// reading without touching the real filesystem.
	//
	// Bead ref: hk-guez.
	DiskFreeBytesFunc func(string) (uint64, error)

	// GoCacheCleanFunc, when non-nil, overrides "go clean -cache" execution
	// inside runPeriodicDiskCheck. Tests use this to capture or stub the
	// reaper without side-effects on the build cache.
	//
	// Bead ref: hk-guez.
	GoCacheCleanFunc func() error

	// CacheReapMu, when non-nil, overrides the reap↔dispatch exclusion
	// RWMutex.  Tests that verify TOCTOU behaviour inject a controlled
	// *sync.RWMutex here.  When nil, ExportedWorkLoopDeps installs a fresh
	// *sync.RWMutex (mirrors the production newWorkLoopDeps default).
	//
	// Bead ref: hk-y3frr.
	CacheReapMu *sync.RWMutex

	// WorktreeReclaimFunc, when non-nil, overrides the git-worktree-remove
	// sequence inside reclaimStaleWorktrees. Tests inject this to observe which
	// stale paths would be removed and to control whether disk recovers after
	// reclaim (by pairing with a stateful DiskFreeBytesFunc).
	//
	// Bead ref: hk-5uezz.
	WorktreeReclaimFunc func(ctx context.Context, projectDir string, stalePaths []string) error

	// Runner, when non-nil, is threaded into workLoopDeps.runner and used as the
	// fallback dotRunner on the DOT run path when no remote worker is selected
	// (hk-hd2w6). Inject a *tmuxPkg.RecordingRunner to capture Command calls in
	// the contract test.
	//
	// Bead ref: hk-hd2w6.
	Runner tmuxPkg.CommandRunner

	// DefaultHarness is the tier-4 global harness default for the
	// harness-selection precedence walk. Mirrors Config.DefaultHarness;
	// empty → built-in claude-code fallback (hk-ytzj2).
	DefaultHarness core.AgentType
}

// ExportedWorkLoopDeps constructs a workLoopDeps from the supplied params and
// a real handler.Handler bound to the provided bus.  Use in tests to bypass
// newWorkLoopDeps (which requires a real br binary).
func ExportedWorkLoopDeps(p WorkLoopDepsParams) workLoopDeps {
	binary := p.HandlerBinary
	if binary == "" {
		binary = "claude"
	}

	// Normalise WorkflowModeDefault: zero value → WorkflowModeSingle, mirroring
	// daemon.Start step 0 per PL-004a.
	wmd := p.WorkflowModeDefault
	if wmd == "" {
		wmd = core.WorkflowModeSingle
	}

	// Normalise MaxConcurrent: zero value → 1 (single-threaded default).
	maxConcurrent := p.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	// Use the caller-supplied RunRegistry or create a fresh one.
	reg := p.RunRegistry
	if reg == nil {
		reg = NewRunRegistry()
	}

	// Use the caller-supplied HookStore or fall back to a real hookSessionStore
	// (hk-ngw3d). Shell-fixture tests whose handlers exit without a real
	// Stop-hook relay will hit the 3-second stopHookGrace window before
	// proceeding on exit code.
	var hookStore hookStoreIface
	if p.HookStore != nil {
		hookStore = p.HookStore
	} else {
		hookStore = newHookSessionStore()
	}

	// LaunchSpecBuilder and WorktreeFactory: pass the caller-supplied value
	// (which may be nil) directly to workLoopDeps. When nil, beadRunOne uses
	// the production nil-guards to wire buildClaudeLaunchSpec and
	// productionWorktreeFactory respectively (hk-ngw3d).
	lsb := p.LaunchSpecBuilder
	wtf := p.WorktreeFactory

	// MergeQueue: pass the caller-supplied domain (nil ⇒ inline merge). Concurrent
	// beadRunOne tests inject a started queue so their commit-phase merges never
	// race on refs/heads/main (hk-4f5ua); single-bead tests leave it nil.
	mergeQ := p.MergeQueue

	// WorktreeCreateMu: default to a fresh mutex (mirrors newWorkLoopDeps, hk-5qp7z).
	worktreeCreateMu := p.WorktreeCreateMu
	if worktreeCreateMu == nil {
		worktreeCreateMu = &sync.Mutex{}
	}

	// AgentSpawnSem: default to a fresh cap-3 semaphore (mirrors newWorkLoopDeps, hk-5z1f0).
	agentSpawnSem := p.AgentSpawnSem
	if agentSpawnSem == nil {
		agentSpawnSem = make(chan struct{}, 3)
	}

	// CacheReapMu: default to a fresh RWMutex (hk-y3frr).
	cacheReapMu := p.CacheReapMu
	if cacheReapMu == nil {
		cacheReapMu = &sync.RWMutex{}
	}

	// Derive the submit-wake channel from the QueueStore when one is provided
	// (hk-24xn1). Mirrors the daemon.Start wiring so queue-aware tests observe
	// the same wake-on-submit behaviour as production.
	var submitWakeC <-chan struct{}
	if p.QueueStore != nil {
		submitWakeC = p.QueueStore.WakeCh()
	}

	return workLoopDeps{
		brAdapter:                  p.BrAdapter,
		bus:                        p.Bus,
		intentLogDir:               p.IntentLogDir,
		projectDir:                 p.ProjectDir,
		handlerBinary:              binary,
		handlerArgs:                p.HandlerArgs,
		handlerEnv:                 nil,
		brTimeoutCfg:               brcli.TimeoutConfig{},
		tidGen:                     core.NewTransitionIDGenerator(),
		workflowModeDefault:        wmd,
		runRegistry:                reg,
		maxConcurrent:              maxConcurrent,
		cpRegistry:                 p.CPRegistry, // hk-karlz: ControlPoint registry for gate-node dispatch
		hookStore:                  hookStore,
		launchSpecBuilder:          lsb,
		worktreeFactory:            wtf,
		adapterRegistry:            p.AdapterRegistry2,
		harnessRegistry:            p.HarnessRegistry, // hk-f6g7: ProcessExit completion-mode check
		substrate:                  p.Substrate,
		agentReadyTimeout:          p.AgentReadyTimeout,
		postAgentReadyHangTimeout:  p.PostAgentReadyHangTimeout,
		projectCfg:                 p.ProjectCfg,
		queueStore:                 p.QueueStore,
		queueLedger:                p.QueueLedger, // hk-nbjht: §2.8 deferred-item re-eval seam
		submitWakeC:                submitWakeC,
		cancelOnQueueDrain:         p.CancelOnQueueDrain,
		cancelOnQueueExit:          p.CancelOnQueueExit,
		stopDispatchCtx:            p.StopDispatchCtx,
		handlerPauseController:     p.HandlerPauseController,
		staleBlockerCloser:         p.StaleBlockerCloser,         // hk-rnsjs
		strandedInProgressResetter: p.StrandedInProgressResetter, // hk-l2xd1
		strandedResetDaemonNS:      p.StrandedResetDaemonNS,      // hk-l2xd1
		operatorPauseCtrl:          p.OperatorPauseCtrl,          // hk-ry8q1
		decisionBlocker:            p.DecisionBlocker,            // hk-a6e24 EV-043
		noAutoPull:                 p.NoAutoPull,                 // hk-h5lv2 / EM-066
		concurrencyCtrl:            p.ConcurrencyCtrl,            // hk-ohiaf
		localInFlight:              new(atomic.Int32),            // hk-hs7ex: split gate — fresh counter for each test
		skipBrHistoryRotation:      true,                         // hk-hypbi: tests use temp dirs without real .br_history
		targetBranch:               bootconfig.ResolveTargetBranch(p.TargetBranch),
		protectBranches:            p.ProtectBranches,
		mergeQ:                     mergeQ,
		worktreeCreateMu:           worktreeCreateMu,
		agentSpawnSem:              agentSpawnSem,                  // hk-5z1f0: per-worker cold-start spawn semaphore
		emittedEpics:               make(map[core.BeadID]struct{}), // hk-w6y70: fresh per-test guard
		emittedEpicsMu:             &sync.Mutex{},
		workerRegistry:             p.WorkerRegistry, // hk-rs-b8-codesync-3fk0: nil → local run (no SSH steps)
		brPath:                     p.BrPath,         // hk-f722: staged-bead generator; empty → disabled
		followUpLedger:             make(map[string]struct{}),
		followUpLedgerMu:           &sync.Mutex{},
		spawnSubstrateReadyCh:      p.SpawnSubstrateReadyCh, // hk-bk33: post-boot re-dispatch gate
		allowedRepos:               p.AllowedRepos,          // hk-xfuc: cross-repo dispatch safelist
		diskFreeBytesFunc:          p.DiskFreeBytesFunc,     // hk-guez: merge-aware reaper test seam
		// hk-y3frr: default to a no-op clean in tests so that the now-enabled
		// proactive reap never calls real `go clean -cache` in scenario tests
		// (which have no goCacheCleanFunc set and would wipe the build cache on
		// the first work-loop tick, stalling long-running scenarios).
		// Disk-check unit tests always supply their own stub via GoCacheCleanFunc,
		// which overrides this default.
		goCacheCleanFunc: func() error {
			if p.GoCacheCleanFunc != nil {
				return p.GoCacheCleanFunc()
			}
			return nil // no-op: tests must not wipe the shared go-build cache
		},
		cacheReapMu:         cacheReapMu,           // hk-y3frr: reap↔dispatch exclusion
		worktreeReclaimFunc: p.WorktreeReclaimFunc, // hk-5uezz: stale-worktree reclaim seam
		runner:              p.Runner,              // hk-hd2w6: Config.Runner injection seam
		defaultHarness:      p.DefaultHarness,      // hk-ytzj2: tier-4 global harness default
	}
}

// ExportedWorkLoopDepsPtr returns a pointer to a workLoopDeps so tests can
// mutate fields (e.g. diskFreeBytesFunc) after construction. Callers must not
// pass the pointer to ExportedRunWorkLoop (the loop takes the struct by value).
//
// Bead ref: hk-guez.
func ExportedWorkLoopDepsPtr(p WorkLoopDepsParams) *workLoopDeps {
	d := ExportedWorkLoopDeps(p)
	return &d
}

// WorkLoopDepsWithProjectCfg returns a copy of params with ProjectCfg set to cfg.
// Used by integration tests to inject a non-zero ProjectConfig into the work loop.
//
// Bead ref: hk-bfvk7.
func WorkLoopDepsWithProjectCfg(p WorkLoopDepsParams, cfg projectconfig.ProjectConfig) WorkLoopDepsParams {
	p.ProjectCfg = cfg
	return p
}

// ExportedNewWorkLoopDepsWithStore exposes newWorkLoopDeps for tests in package
// daemon_test. The hookStore parameter is typed as *hookSessionStore (an exported
// concrete type via HookSessionStoreExported alias) so that callers in daemon_test
// can pass daemon.ExportedNewHookSessionStore() without naming the unexported
// hookStoreIface interface.
//
// It requires a real br binary on PATH; callers that skip when br is absent
// should use exec.LookPath("br") to guard.
//
// Bead ref: hk-nvrvp.
func ExportedNewWorkLoopDepsWithStore(cfg Config, bus handlercontract.EventEmitter, workflowModeDefault core.WorkflowMode, registry *handlercontract.AdapterRegistry, store *hookSessionStore) (workLoopDeps, error) {
	return newWorkLoopDeps(context.Background(), cfg, bus, workflowModeDefault, registry, store)
}
