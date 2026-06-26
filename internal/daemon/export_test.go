package daemon

// export_test.go — test-seam exports for internal/daemon.
//
// This file is compiled only when running tests (it lives in package daemon,
// not daemon_test). It exports otherwise-unexported symbols so that
// workloop_test.go (package daemon_test) can inject stub dependencies without
// modifying the production API surface.
//
// Bead: hk-ecrxy.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
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
	// Zero value is normalised to 1 (MVH single-threaded default) mirroring
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
	// need adapters registered because Launch does not consult the registry
	// at MVH.
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
	// Nil at MVH.
	//
	// Bead ref: hk-gql20.14.
	Substrate handler.Substrate

	// AgentReadyTimeout is the HC-056 timeout for waitAgentReady (hk-gql20.14).
	// Zero → defaultAgentReadyTimeout (30s).
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
	ProjectCfg ProjectConfig

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
	LaunchSpecBuilder func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)

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
	QueueStore *QueueStore

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
	StopDispatchCtx context.Context

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

	// MergeMu, when non-nil, serialises every lockedMergeRunBranchToMain call
	// across concurrent beadRunOne goroutines (mirrors WithMergeMutex on the
	// daemon.Start path and the production newWorkLoopDeps default). When nil,
	// ExportedWorkLoopDeps installs a fresh mutex so concurrent merges to the
	// shared origin never race on refs/heads/main (hk-4f5ua / hk-bnm89).
	MergeMu *sync.Mutex

	// BeadAuditLogger, when non-nil, overrides the beadAuditLogger function
	// used by the pre-dispatch subsume check to detect reopen-for-fix beads
	// (hk-wcv). When nil (the test default), the check is skipped and the
	// conservative crash-restart assumption applies (pre-dispatch close fires).
	// Supply a non-nil value to exercise the bypass path in unit tests.
	//
	// Bead ref: hk-wcv.
	BeadAuditLogger func(ctx context.Context, id core.BeadID) ([]brcli.AuditEvent, error)

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

	// Runner, when non-nil, is threaded into workLoopDeps.runner and used as the
	// fallback dotRunner on the DOT run path when no remote worker is selected
	// (hk-hd2w6). Inject a *tmuxPkg.RecordingRunner to capture Command calls in
	// the contract test.
	//
	// Bead ref: hk-hd2w6.
	Runner tmuxPkg.CommandRunner
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

	// Normalise MaxConcurrent: zero value → 1 (MVH single-threaded default).
	maxConcurrent := p.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	// Use the caller-supplied RunRegistry or create a fresh one.
	reg := p.RunRegistry
	if reg == nil {
		reg = NewRunRegistry()
	}

	// Use the caller-supplied AdapterRegistry or create a fresh empty one.
	// Tests do not need adapters registered: Launch does not consult the
	// registry at MVH (hk-gql20.16).
	adapterReg := p.AdapterRegistry
	if adapterReg == nil {
		adapterReg = handlercontract.NewAdapterRegistry()
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

	// MergeMu: default to a fresh mutex (mirrors newWorkLoopDeps line ~575) so
	// concurrent beadRunOne goroutines serialise their merge-to-origin and never
	// race on refs/heads/main. Without this, an N>1 test (e.g. SC-2) sees one
	// bead's `git reset --hard` killed mid-merge by a sibling (hk-4f5ua).
	mergeMu := p.MergeMu
	if mergeMu == nil {
		mergeMu = &sync.Mutex{}
	}

	// CacheReapMu: default to a fresh RWMutex (hk-y3frr).
	cacheReapMu := p.CacheReapMu
	if cacheReapMu == nil {
		cacheReapMu = &sync.RWMutex{}
	}

	h := handler.NewHandler(p.Bus, handlercontract.NoopWatcherDeadLetter{}, adapterReg)

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
		h:                          h,
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
		skipBrHistoryRotation:      true,                         // hk-hypbi: tests use temp dirs without real .br_history
		targetBranch:               resolveTargetBranch(p.TargetBranch),
		protectBranches:            p.ProtectBranches,
		mergeMu:                    mergeMu,
		emittedEpics:               make(map[core.BeadID]struct{}), // hk-w6y70: fresh per-test guard
		emittedEpicsMu:             &sync.Mutex{},
		beadAuditLogger:            p.BeadAuditLogger, // hk-wcv: nil by default → conservative crash-restart assumption
		workerRegistry:             p.WorkerRegistry,  // hk-rs-b8-codesync-3fk0: nil → local run (no SSH steps)
		brPath:                     p.BrPath,          // hk-f722: staged-bead generator; empty → disabled
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
		cacheReapMu: cacheReapMu, // hk-y3frr: reap↔dispatch exclusion
		runner:      p.Runner,    // hk-hd2w6: Config.Runner injection seam
	}
}

// ExportedRunPeriodicDiskCheck calls runPeriodicDiskCheck with the given deps.
// Used by diskcheck_hksxlb_test.go to drive the reaper directly without
// running the full work loop (hk-guez).
func ExportedRunPeriodicDiskCheck(ctx context.Context, deps *workLoopDeps) {
	runPeriodicDiskCheck(ctx, deps)
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

// ExportedNewRunRegistry creates a fresh RunRegistry for tests.
//
// Bead ref: hk-guez.
func ExportedNewRunRegistry() *RunRegistry {
	return NewRunRegistry()
}

// ExportedDiskCheckDiskLow reads the diskLow field from deps.
// Used by diskcheck_hksxlb_test.go to assert post-call state (hk-guez).
func ExportedDiskCheckDiskLow(deps *workLoopDeps) bool {
	return deps.diskLow
}

// ExportedDiskCheckSetGoCacheCleanInterval overrides the proactive-reap
// interval on deps so tests don't have to wait 60 minutes. A zero override
// restores the production default (goCacheCleanInterval).
//
// Bead ref: hk-guez.
func ExportedDiskCheckSetGoCacheCleanInterval(deps *workLoopDeps, d time.Duration) {
	deps.goCacheCleanIntervalOverride = d
}

// ExportedDiskCheckSetCheckInterval overrides the disk-probe interval on deps
// so tests fire immediately. A zero override restores the production default
// (diskCheckInterval).
//
// Bead ref: hk-guez.
func ExportedDiskCheckSetCheckInterval(deps *workLoopDeps, d time.Duration) {
	deps.diskCheckIntervalOverride = d
}

// WorkflowModeDefaultOf returns the workflowModeDefault field from deps.
// This is the test-seam accessor for the claim path (T-WM-009) to observe
// the cached daemon-level default without exporting workLoopDeps itself.
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-004a.
// Bead ref: hk-7om2q.8.
func WorkflowModeDefaultOf(deps workLoopDeps) core.WorkflowMode {
	return deps.workflowModeDefault
}

// ExportedRunWorkLoop runs the work loop with the given deps until ctx is
// cancelled, mirroring runWorkLoop.
func ExportedRunWorkLoop(ctx context.Context, deps workLoopDeps) error {
	return runWorkLoop(ctx, deps)
}

// ExportedResolveWorkflowMode exposes resolveWorkflowMode for tests in package
// daemon_test. See moderesolve.go for semantics.
//
// Bead ref: hk-7om2q.9.
func ExportedResolveWorkflowMode(
	ctx context.Context,
	bead core.BeadRecord,
	daemonDefault core.WorkflowMode,
	bus handlercontract.EventEmitter,
) core.WorkflowMode {
	return resolveWorkflowMode(ctx, bead, daemonDefault, bus)
}

// ExportedResolveWorkflowRef exposes resolveWorkflowRef for tests in package
// daemon_test. See moderesolve.go for semantics.
//
// Bead ref: hk-30q6.
func ExportedResolveWorkflowRef(bead core.BeadRecord, itemWorkflowRef string) string {
	return resolveWorkflowRef(bead, itemWorkflowRef)
}

// ExportedResolveHarness exposes resolveHarness for tests in package daemon_test.
// See harnessresolve.go for semantics.
//
// Bead ref: hk-y01k6 [C4/T4].
func ExportedResolveHarness(
	ctx context.Context,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) core.AgentType {
	return resolveHarness(ctx, bead, queueDefault, nodeDefault, globalDefault, bus)
}

// ExportedModelPreferenceError is a type alias for ModelPreferenceError so tests
// in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-xo03m.
type ExportedModelPreferenceError = ModelPreferenceError

// ExportedBuildLaunchSpecImplementerInitial exposes buildLaunchSpecImplementerInitial
// for tests in package daemon_test. See launchspecbuild.go for semantics.
func ExportedBuildLaunchSpecImplementerInitial(base handlercontract.LaunchSpec, iterationCount int) (handlercontract.LaunchSpec, error) {
	return buildLaunchSpecImplementerInitial(base, iterationCount)
}

// ExportedBuildLaunchSpecImplementerResume exposes buildLaunchSpecImplementerResume
// for tests in package daemon_test. See launchspecbuild.go for semantics.
func ExportedBuildLaunchSpecImplementerResume(base handlercontract.LaunchSpec, iterationCount int, claudeSessionID string) (handlercontract.LaunchSpec, error) {
	return buildLaunchSpecImplementerResume(base, iterationCount, claudeSessionID)
}

// ExportedBuildLaunchSpecReviewer exposes buildLaunchSpecReviewer for tests in
// package daemon_test. See launchspecbuild.go for semantics.
func ExportedBuildLaunchSpecReviewer(base handlercontract.LaunchSpec, iterationCount int) (handlercontract.LaunchSpec, error) {
	return buildLaunchSpecReviewer(base, iterationCount)
}

// ReviewLoopResultExported is the exported shape of reviewLoopResult for tests
// in package daemon_test. Fields mirror reviewLoopResult verbatim.
//
// Bead ref: hk-7om2q.20.
type ReviewLoopResultExported struct {
	Success          bool
	CompletionReason string
	Summary          string
	NeedsAttention   bool
}

// ExportedRunReviewLoop exposes runReviewLoop for tests in package daemon_test.
// The result is converted to ReviewLoopResultExported to avoid exporting the
// internal reviewLoopResult type.
//
// Bead ref: hk-7om2q.20.
func ExportedRunReviewLoop(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	parentSHA string,
) ReviewLoopResultExported {
	// nil runner ⇒ LOCAL run: byte-identical to the pre-remote-substrate path.
	r := runReviewLoop(ctx, deps, runID, beadID, "", "", wtPath, parentSHA, "", "", "", "", nil, "", "", "", "")
	return ReviewLoopResultExported{
		Success:          r.success,
		CompletionReason: string(r.completionReason),
		Summary:          r.summary,
		NeedsAttention:   r.needsAttention,
	}
}

// ExportedRunAutoStatusInspection exposes runAutoStatusInspection for unit
// tests. The function is deterministic and LLM-free (AR-006 mechanism-tagged);
// this export allows tests to verify pass/fail outcomes without the full
// cascade machinery. It injects a nil runner (the LOCAL path), so these tests
// exercise the byte-identical local-substrate behavior (NFR7).
//
// Bead ref: hk-oo4.
func ExportedRunAutoStatusInspection(ctx context.Context, wtPath string) (core.Outcome, bool) {
	return runAutoStatusInspection(ctx, nil, wtPath)
}

// DotWorkflowResultExported is the exported shape of dotWorkflowResult for tests
// in package daemon_test. Fields mirror dotWorkflowResult verbatim.
//
// Bead ref: hk-3qjwl.
type DotWorkflowResultExported struct {
	Success        bool
	TerminalNodeID string
	NeedsAttention bool
	Summary        string
	// ApproveVerdict mirrors dotWorkflowResult.approveVerdict (hk-tnui).
	ApproveVerdict *workspace.ReviewVerdict
}

// ExportedDriveDotWorkflow exposes driveDotWorkflow for tests in package
// daemon_test. The result is converted to DotWorkflowResultExported to avoid
// exporting the internal dotWorkflowResult type.
//
// Bead ref: hk-3qjwl (DOT agentic-node dispatch must gate paste-inject on
// agent_ready, exactly as the single-mode and review-loop paths do).
func ExportedDriveDotWorkflow(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
) DotWorkflowResultExported {
	r := driveDotWorkflow(ctx, deps, runID, beadID, core.BeadRecord{}, "", "", wtPath, parentSHA, graph, "", "", "", "", nil, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
		ApproveVerdict: r.approveVerdict,
	}
}

// ExportedDriveDotWorkflowFull is like ExportedDriveDotWorkflow but exposes the
// beadTitle, beadDescription, and extraContext parameters so tests can assert on
// context injection (e.g. node role= surfacing, hk-m5lmo).
func ExportedDriveDotWorkflowFull(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	extraContext string,
) DotWorkflowResultExported {
	r := driveDotWorkflow(ctx, deps, runID, beadID, core.BeadRecord{}, beadTitle, beadDescription, wtPath, parentSHA, graph, "", "", extraContext, "", nil, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
		ApproveVerdict: r.approveVerdict,
	}
}

// ExportedMinimalLaunchSpecBuilder returns a launchSpecBuilder stub that
// produces a handler.LaunchSpec with a no-op binary (/bin/true) and
// zero-value claudeRunArtifacts. Used in tests that inject a spy substrate
// and need handler.Launch to reach Substrate.SpawnWindow without the full
// Claude build infrastructure (e.g. hk-wnqos single-mode terminal-spawn test).
func ExportedMinimalLaunchSpecBuilder() func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(_ context.Context, _ claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		return handler.LaunchSpec{Binary: "/bin/true"}, claudeRunArtifacts{}, nil
	}
}

// ExportedCaptureExtraContextBuilder returns a launchSpecBuilder stub that
// sends the extraContext from the FIRST call into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert
// that node role= is injected into the agent brief (hk-m5lmo).
func ExportedCaptureExtraContextBuilder(ch chan<- string) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(_ context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		select {
		case ch <- rc.extraContext:
		default:
		}
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedCaptureNodePromptBuilder returns a launchSpecBuilder stub that
// sends the nodePrompt from the FIRST call into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert
// that node prompt= is threaded into claudeRunCtx (hk-sdnzj).
func ExportedCaptureNodePromptBuilder(ch chan<- string) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(_ context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		select {
		case ch <- rc.nodePrompt:
		default:
		}
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedCaptureRunnerBuilder returns a launchSpecBuilder stub that sends the
// CommandRunner from the FIRST call's claudeRunCtx into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert that
// the review-loop and DOT launch paths thread the run's CommandRunner into the
// claudeRunCtx so the worktree-trust / settings / agent-task writes land on the
// WORKER for a REMOTE run (hk-3sus).
func ExportedCaptureRunnerBuilder(ch chan<- tmuxPkg.CommandRunner) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(_ context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		select {
		case ch <- rc.runner:
		default:
		}
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedRunReviewLoopWithRunner exposes runReviewLoop with an explicit
// CommandRunner so tests can assert the remote (runner != nil) path threads the
// runner into the implementer/reviewer claudeRunCtx (hk-3sus).
func ExportedRunReviewLoopWithRunner(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	parentSHA string,
	runner tmuxPkg.CommandRunner,
) ReviewLoopResultExported {
	r := runReviewLoop(ctx, deps, runID, beadID, "", "", wtPath, parentSHA, "", "", "", "", runner, "", "", "", "")
	return ReviewLoopResultExported{
		Success:          r.success,
		CompletionReason: string(r.completionReason),
		Summary:          r.summary,
		NeedsAttention:   r.needsAttention,
	}
}

// ExportedSetSubstrateRunnerObserver installs (or clears, with nil) the package
// test seam that captures the CommandRunner passed into newPerRunSubstrate at the
// review-loop and DOT agentic launch sites. Tests use this to assert the
// SUBSTRATE-spawn runner (distinct from the SPEC runner) is the real non-nil
// worker runner for a REMOTE run (hk-fxy9 / hk-538l).
func ExportedSetSubstrateRunnerObserver(f func(tmuxPkg.CommandRunner)) {
	substrateRunnerObserver = f
}

// ExportedDriveDotWorkflowWithRunner exposes driveDotWorkflow with an explicit
// CommandRunner so tests can assert the remote (runner != nil) path threads the
// runner into the DOT agentic-node claudeRunCtx (hk-3sus).
func ExportedDriveDotWorkflowWithRunner(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	runner tmuxPkg.CommandRunner,
) DotWorkflowResultExported {
	r := driveDotWorkflow(ctx, deps, runID, beadID, core.BeadRecord{}, beadTitle, beadDescription, wtPath, parentSHA, graph, "", "", "", "", runner, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
	}
}

// ModelEffortPair holds the model and effort values captured from a claudeRunCtx.
// Used by ExportedCaptureModelEffortBuilder tests (hk-q8nqr).
type ModelEffortPair struct {
	Model  string
	Effort string
}

// ExportedCaptureModelEffortBuilder returns a launchSpecBuilder stub that
// sends the (model, effort) pair from the FIRST call into ch (non-blocking),
// then returns an error to short-circuit the dispatch. Tests use this to
// assert that per-node model= / effort= overrides are threaded into
// claudeRunCtx (hk-q8nqr WG-042 §I.5 / EM-012b-NODE).
func ExportedCaptureModelEffortBuilder(ch chan<- ModelEffortPair) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(_ context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		select {
		case ch <- ModelEffortPair{Model: rc.model, Effort: rc.effort}:
		default:
		}
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedDriveDotWorkflowWithModelEffort exposes driveDotWorkflow with
// explicit resolvedModel and resolvedEffort parameters so tests can assert on
// per-node model/effort override vs. run-level default (hk-q8nqr).
func ExportedDriveDotWorkflowWithModelEffort(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	resolvedModel string,
	resolvedEffort string,
) DotWorkflowResultExported {
	r := driveDotWorkflow(ctx, deps, runID, beadID, core.BeadRecord{}, beadTitle, beadDescription, wtPath, parentSHA, graph, resolvedModel, resolvedEffort, "", "", nil, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-025 test seams (hk-w5vra.11)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedHookSessionStore exposes hookSessionStore for tests.
//
// Bead ref: hk-w5vra.11.
func ExportedNewHookSessionStore() *hookSessionStore {
	return newHookSessionStore()
}

// ExportedProductionWorktreeFactory exposes productionWorktreeFactory for tests
// that need to wrap or observe real git worktree creation (e.g. merge-to-main
// integration tests).
//
// Bead ref: hk-kqdpf.1.
var ExportedProductionWorktreeFactory = productionWorktreeFactory

// ExportedDiscardDirtyChurn exposes discardDirtyChurn for the pre-rebase
// churn-cleanup regression test (hk-3yz2d ledger, hk-aiw63 generalized).
func ExportedDiscardDirtyChurn(ctx context.Context, wtPath string) {
	discardDirtyChurn(ctx, wtPath)
}

// ExportedCommitResidualDelta exposes commitResidualDelta for the review-loop
// residual-delta merge regression test (hk-rljho class).
func ExportedCommitResidualDelta(ctx context.Context, wtPath string, runID core.RunID) {
	commitResidualDelta(ctx, wtPath, runID)
}

// ExportedCleanUntrackedFiles exposes cleanUntrackedFiles for the
// integration-test artifact pre-rebase cleanup regression test (hk-g9zz).
func ExportedCleanUntrackedFiles(ctx context.Context, wtPath string) {
	cleanUntrackedFiles(ctx, wtPath)
}

// ExportedForceTeardownSession exposes forceTeardownSession for the hk-68pvl
// worktree-teardown-ordering regression test.
func ExportedForceTeardownSession(sess handler.Session) {
	forceTeardownSession(sess)
}

// ExportedSpawnSlotsInUse exposes the spawn-semaphore slots-in-use count of a
// substrate returned by NewTmuxSubstrate, for the hk-4l7zs slot-leak tests.
// Returns 0 when sub is not a *tmuxSubstrate or has no cap configured.
func ExportedSpawnSlotsInUse(sub handler.Substrate) int {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		return ts.SpawnSlotsInUse()
	}
	return 0
}

// ExportedCrewSessionName exposes the crewSessionName method of a substrate
// returned by NewTmuxSubstrate, for fleet-portability T2 naming tests (hk-ohd).
// Returns ("", nil) when sub is not a *tmuxSubstrate; otherwise propagates the
// (name, err) result — err is non-nil when no project hash is configured
// (hk-rmy1, slice C: the legacy "hk-crew-<name>" fallback was removed).
func ExportedCrewSessionName(sub handler.Substrate, crewName string) (string, error) {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		return ts.crewSessionName(crewName)
	}
	return "", nil
}

// ExportedNoCommitGuardShouldReopen exposes noCommitGuardShouldReopen for the
// single-mode no-commit guard regression test (hk-4ie1z).
func ExportedNoCommitGuardShouldReopen(ctx context.Context, projectDir, curHeadSHA, parentSHA string, beadID core.BeadID) bool {
	return noCommitGuardShouldReopen(ctx, projectDir, curHeadSHA, parentSHA, beadID)
}

// ExportedHookRegister exposes RegisterHookSession for tests.
func ExportedHookRegister(s *hookSessionStore, runID, claudeSessionID string) {
	s.RegisterHookSession(runID, claudeSessionID)
}

// ExportedHookClose exposes CloseHookSession for tests.
func ExportedHookClose(s *hookSessionStore, runID, claudeSessionID string) {
	s.CloseHookSession(runID, claudeSessionID)
}

// ExportedHookLatestOutcome exposes LatestOutcome for tests.
func ExportedHookLatestOutcome(s *hookSessionStore, runID, claudeSessionID string) *json.RawMessage {
	return s.LatestOutcome(runID, claudeSessionID)
}

// ExportedHookDispatch exposes dispatchHookRelayEnvelope for tests.
func ExportedHookDispatch(s *hookSessionStore, env HookRelayEnvelopeExported) (string, string) {
	ack := s.dispatchHookRelayEnvelope(hookRelayEnvelope{
		Type:             env.Type,
		RunID:            env.RunID,
		ClaudeSessionID:  env.ClaudeSessionID,
		HandlerSessionID: env.HandlerSessionID,
		EmittedAtNs:      env.EmittedAtNs,
		Payload:          env.Payload,
	})
	return ack.Status, ack.Reason
}

// HookRelayEnvelopeExported is the exported shape of hookRelayEnvelope for tests.
type HookRelayEnvelopeExported struct {
	Type             string
	RunID            string
	ClaudeSessionID  string
	HandlerSessionID string
	EmittedAtNs      int64
	Payload          json.RawMessage
}

// ExportedHookWaitForOutcome exposes WaitForOutcome for tests.
//
// Bead ref: hk-gql20.20.
func ExportedHookWaitForOutcome(ctx context.Context, s *hookSessionStore, runID, claudeSessionID string) (json.RawMessage, error) {
	return s.WaitForOutcome(ctx, runID, claudeSessionID)
}

// ExportedHookStoreOf returns the hookStore field from deps.
// Used by integration tests to inspect store state after dispatching
// hook-relay envelopes through a running socket listener (hk-gql20.21).
func ExportedHookStoreOf(deps workLoopDeps) hookStoreIface {
	return deps.hookStore
}

// ExportedHookSetAgentReadyCallback exposes SetAgentReadyCallback for tests
// (hk-1rocd: relay-synthesized agent_ready dispatch path).
func ExportedHookSetAgentReadyCallback(s *hookSessionStore, runID, claudeSessionID string, cb func()) {
	s.SetAgentReadyCallback(runID, claudeSessionID, cb)
}

// ExportedPersistClaudeSessionID exposes persistClaudeSessionID for tests.
//
// Bead ref: hk-w5vra.6.
func ExportedPersistClaudeSessionID(ctx context.Context, wtPath string, runID core.RunID, sessionID string) (string, bool, error) {
	res, err := persistClaudeSessionID(ctx, wtPath, runID, sessionID)
	return res.CommitSHA, res.Skipped, err
}

// ─────────────────────────────────────────────────────────────────────────────
// SC-6 test seams (hk-nx5wu)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedWiringEntry is the exported shape of wiringEntry for SC-6 wiring-table tests.
//
// Bead ref: hk-nx5wu.
type ExportedWiringEntry struct {
	Symbol   string
	CallSite string
	Wires    string
}

// ExportedCompositionRootWirings returns the canonical wiring table as exported
// entries so SC-6 can verify all pre-Seal Subscribe entries are present.
//
// Bead ref: hk-nx5wu.
func ExportedCompositionRootWirings() []ExportedWiringEntry {
	out := make([]ExportedWiringEntry, len(compositionRootWirings))
	for i, e := range compositionRootWirings {
		out[i] = ExportedWiringEntry{Symbol: e.symbol, CallSite: e.callSite, Wires: e.wires}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// StaleWatcher test seams (hk-wkzlc)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedStalewatchScan triggers a single scan pass on w, identical to what
// the background loop does on each ticker tick. Allows tests to drive stale
// detection deterministically without real time passing.
//
// Bead ref: hk-wkzlc.
func ExportedStalewatchScan(w *StaleWatcher, ctx context.Context) {
	w.scan(ctx)
}

// ExportedBeadStaleAfter exposes the package-private beadStaleAfter helper for
// unit testing.
func ExportedBeadStaleAfter(labels []string, defaultAfter time.Duration) time.Duration {
	return beadStaleAfter(labels, defaultAfter)
}

// ExportedRunHandleIsAborted returns true if the RunHandle's aborted flag is set.
// Used by the never-spawned reaper tests (hk-0z5x).
func ExportedRunHandleIsAborted(h *RunHandle) bool {
	return h.aborted.Load()
}

// ExportedStalewatchObserve invokes the StaleWatcher's observe callback directly
// with the given event. The watcher's configured Now() function is used as the
// timestamp, so tests must set an appropriate clock before calling this.
//
// Bead ref: hk-0z5x.
func ExportedStalewatchObserve(w *StaleWatcher, ctx context.Context, evt core.Event) {
	_ = w.observe(ctx, evt)
}

// ─────────────────────────────────────────────────────────────────────────────
// BandwidthTuner test seams (hk-w6q7)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedBandwidthTunerTick triggers a single evaluation tick on t, identical
// to what the background loop does on each ticker tick. Allows tests to drive
// poll-gate behaviour deterministically without real time passing.
//
// Bead ref: hk-w6q7 (P2-b: poll-gating).
func ExportedBandwidthTunerTick(t *BandwidthTuner) {
	t.tick()
}

// ─────────────────────────────────────────────────────────────────────────────
// buildClaudeLaunchSpec test seams (hk-gql20.13)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedClaudeRunCtx is the exported shape of claudeRunCtx for tests.
// Fields mirror claudeRunCtx verbatim with exported names.
//
// Bead ref: hk-gql20.13, hk-xo03m.
type ExportedClaudeRunCtx struct {
	RunID             core.RunID
	BeadID            string
	WorkspacePath     string
	DaemonSocket      string
	WorkflowMode      core.WorkflowMode
	Phase             handlercontract.ReviewLoopPhase
	IterationCount    int
	PriorClaudeSessID *string
	HandlerBinary     string
	// DaemonBinaryPath is the absolute path to the running harmonik binary for
	// hook command materialization (hk-kqdpf.6). Empty in tests that don't need
	// real hook wiring.
	DaemonBinaryPath string
	BaseEnv          []string
	// Model is the resolved model alias from ModelPreference (HC-055a / EM-012b).
	// Non-empty → --model <value> appended to argv. Must satisfy ^[A-Za-z0-9._:/-]+$, ≤128 chars.
	// Empty → no flag emitted.
	Model string
	// Effort is the resolved effort level from ModelPreference (HC-055a / EM-012b).
	// Non-empty → --effort <value> appended to argv. Must be one of {low,medium,high,xhigh,max}.
	// Empty → no flag emitted.
	Effort string
	// WorktreeRootPath is the absolute path to the harmonik worktrees root
	// (e.g. <projectDir>/.harmonik/worktrees). When non-empty and workspacePath
	// canonicalizes to a path under this prefix, --dangerously-skip-permissions is
	// added to argv per HC-055b. Empty → path-check skipped, flag not emitted.
	WorktreeRootPath string

	// BeadDescription is the bead body verbatim from the Beads ledger.
	// Used to populate the "## Task Description" section in agent-task.md.
	BeadDescription string

	// NodePrompt is the optional inline LLM prompt from the DOT node's prompt=
	// attribute (WG-040 §I.3). When non-empty and phase is implementer, it
	// REPLACES BeadDescription as the CHB-028 Body channel (hk-sdnzj).
	NodePrompt string

	// Runner is the CommandRunner threaded into the materialization step for a
	// REMOTE run (hk-z8ek). Non-nil → the three launch-artifact writes go through
	// the runner onto the worker FS; nil → byte-identical box-A-local writes (NFR7).
	Runner tmuxPkg.CommandRunner

	// WorkerBinaryPath is the worker-side harmonik path used as the hook command
	// in the worker's settings.json for a REMOTE run (hk-z8ek). Empty → the
	// box-A DaemonBinaryPath is used unchanged.
	WorkerBinaryPath string
}

// ExportedClaudeRunArtifacts is the exported shape of claudeRunArtifacts for tests.
// Fields mirror claudeRunArtifacts verbatim with exported names.
//
// Bead ref: hk-gql20.13.
type ExportedClaudeRunArtifacts struct {
	ClaudeSessionID  string
	SessionLogPath   string
	HandlerSessionID string
	PreExecMsgs      []json.RawMessage
	Substrate        interface{}
}

// ExportedBuildClaudeLaunchSpec exposes buildClaudeLaunchSpec for tests in
// package daemon_test. The ExportedClaudeRunCtx is translated to the internal
// claudeRunCtx before calling.
//
// Bead ref: hk-gql20.13.
func ExportedBuildClaudeLaunchSpec(ctx context.Context, rc ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	internal := claudeRunCtx{
		runID:             rc.RunID,
		beadID:            rc.BeadID,
		workspacePath:     rc.WorkspacePath,
		daemonSocket:      rc.DaemonSocket,
		workflowMode:      rc.WorkflowMode,
		phase:             rc.Phase,
		iterationCount:    rc.IterationCount,
		priorClaudeSessID: rc.PriorClaudeSessID,
		handlerBinary:     rc.HandlerBinary,
		daemonBinaryPath:  rc.DaemonBinaryPath,
		baseEnv:           rc.BaseEnv,
		model:             rc.Model,
		effort:            rc.Effort,
		worktreeRootPath:  rc.WorktreeRootPath,
		beadDescription:   rc.BeadDescription,
		nodePrompt:        rc.NodePrompt,
		runner:            rc.Runner,
		workerBinaryPath:  rc.WorkerBinaryPath,
	}
	spec, arts, err := buildClaudeLaunchSpec(ctx, internal)
	if err != nil {
		return handler.LaunchSpec{}, ExportedClaudeRunArtifacts{}, err
	}
	return spec, ExportedClaudeRunArtifacts{
		ClaudeSessionID:  arts.claudeSessionID,
		SessionLogPath:   arts.sessionLogPath,
		HandlerSessionID: arts.handlerSessionID,
		PreExecMsgs:      arts.preExecMsgs,
		Substrate:        arts.substrate,
	}, nil
}

// ExportedNewSessionIDInterceptor exposes newSessionIDInterceptor for tests.
//
// Bead ref: hk-w5vra.6.
func ExportedNewSessionIDInterceptor(r io.Reader, cb func(string)) io.Reader {
	return newSessionIDInterceptor(r, cb)
}

// ExportedNewDaemonHeartbeatEmitter exposes newDaemonHeartbeatEmitter for
// tests in package daemon_test.
//
// Bead ref: hk-gql20.17.
func ExportedNewDaemonHeartbeatEmitter(bus handlercontract.EventEmitter, runID core.RunID) handler.HeartbeatEmitter {
	return newDaemonHeartbeatEmitter(bus, runID)
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-056 test seams (hk-gql20.18)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedErrAgentReadyTimeout exposes ErrAgentReadyTimeout for tests.
//
// Bead ref: hk-gql20.18.
var ExportedErrAgentReadyTimeout = ErrAgentReadyTimeout

// ExportedErrPostAgentReadyHang exposes ErrPostAgentReadyHang for tests (hk-a2okh).
var ExportedErrPostAgentReadyHang = ErrPostAgentReadyHang

// ExportedDefaultPostAgentReadyHangTimeout exposes defaultPostAgentReadyHangTimeout
// for tests (hk-a2okh).
var ExportedDefaultPostAgentReadyHangTimeout = &defaultPostAgentReadyHangTimeout

// ExportedWaitPostAgentReadyProgress exposes waitPostAgentReadyProgress for
// unit tests (hk-a2okh).
var ExportedWaitPostAgentReadyProgress = waitPostAgentReadyProgress

// AgentEventSourceExported is the exported alias for agentEventSource so that
// test stubs in package daemon_test can satisfy the interface.
//
// Because agentEventSource is unexported, daemon_test stubs cannot reference it
// directly. This exported alias carries the same method set, enabling type-safe
// injection via ExportedWaitAgentReady.
//
// Bead ref: hk-gql20.18.
type AgentEventSourceExported = agentEventSource

// ExportedWaitAgentReady exposes waitAgentReady for tests in package daemon_test.
//
// Bead ref: hk-gql20.18.
func ExportedWaitAgentReady(
	ctx context.Context,
	runID core.RunID,
	source AgentEventSourceExported,
	adapter handlercontract.Adapter,
	timeout time.Duration,
) error {
	return waitAgentReady(ctx, runID, source, adapter, timeout)
}

// (duplicate buildClaudeLaunchSpec stubs removed — canonical declarations above at lines ~295-356)

// ─────────────────────────────────────────────────────────────────────────────
// waitWithSocketGrace test seams (hk-gql20.22)
// ─────────────────────────────────────────────────────────────────────────────

// HookSessionStoreExported is a type alias for *hookSessionStore, exposed so
// tests in package daemon_test can declare helper-function parameters with the
// correct concrete type without relying on interface{}.
//
// Bead ref: hk-gql20.22.
type HookSessionStoreExported = hookSessionStore

// ExitInfoExported is the exported shape of exitInfo for tests in package
// daemon_test.
//
// Bead ref: hk-gql20.22.
type ExitInfoExported struct {
	ExitCode   int
	WaitErr    error
	StderrTail []byte
}

// ExportedStopHookGrace exposes the stopHookGrace constant so tests can assert
// that the fast-path returns well within the grace window.
//
// Bead: hk-3jmke.
const ExportedStopHookGrace = stopHookGrace

// ExportedWaitWithSocketGrace exposes waitWithSocketGrace for tests in package
// daemon_test.
//
// Bead ref: hk-gql20.22.
func ExportedWaitWithSocketGrace(
	ctx context.Context,
	store *hookSessionStore,
	watcher *handlercontract.Watcher,
	sess handler.Session,
	runID, claudeSessID string,
) (*handler.ExportedOutcomeEmittedPayload, ExitInfoExported) {
	outcome, ei := waitWithSocketGrace(ctx, store, watcher, sess, runID, claudeSessID)
	return outcome, ExitInfoExported{ExitCode: ei.exitCode, WaitErr: ei.waitErr, StderrTail: ei.stderrTail}
}

// ─────────────────────────────────────────────────────────────────────────────
// paste-inject test seams (hk-zrj83)
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// pasteInjectQuitOnReviewFile test seams (hk-jimbc)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedReviewFileTimeout is a pointer to the package-level reviewFileTimeout
// var.  Tests set *ExportedReviewFileTimeout to a short duration to exercise
// the timeout path without waiting 10 minutes.
//
// Bead: hk-jimbc.
var ExportedReviewFileTimeout = &reviewFileTimeout

// ExportedReviewFilePollInterval is a pointer to the package-level
// reviewFilePollInterval var.  Tests set *ExportedReviewFilePollInterval to a
// short duration to keep polling tight during unit tests.
//
// Bead: hk-jimbc.
var ExportedReviewFilePollInterval = &reviewFilePollInterval

// PasteInjecterExported is an exported alias for the unexported pasteInjecter
// interface so tests in package daemon_test can supply a structural stub as the
// inj re-seed target of ExportedPasteInjectQuitOnReviewFile (hk-7rgqs).
type PasteInjecterExported = pasteInjecter

// EnterSenderExported is an exported alias for the unexported enterSender
// interface so tests can assert on / drive the splash-dismiss + submit Enter
// path (hk-7rgqs).
type EnterSenderExported = enterSender

// ExportedReviewerReseedGrace is a pointer to the package-level
// reviewerReseedGrace var.  Tests set *ExportedReviewerReseedGrace to a short
// duration to exercise the hk-7rgqs one-shot reviewer re-seed path without
// waiting the production 75s.
//
// Bead: hk-7rgqs.
var ExportedReviewerReseedGrace = &reviewerReseedGrace

// ExportedImplementerReseedGrace is a pointer to the package-level
// implementerReseedGrace var.  Tests set *ExportedImplementerReseedGrace to a
// short duration to exercise the hk-76n5g one-shot reseed-Enter path in
// pasteInjectQuitOnCommit without waiting the production 75 s.
//
// Bead: hk-76n5g.
var ExportedImplementerReseedGrace = &implementerReseedGrace

// ExportedSplashDismissDelay is a pointer to the package-level splashDismissDelay
// var.  Tests set *ExportedSplashDismissDelay to a short duration so the
// splash-dismiss wait inside the paste-inject helpers does not slow unit tests.
//
// Bead: hk-7rgqs.
var ExportedSplashDismissDelay = &splashDismissDelay

// ExportedPasteInjectReviewer exposes pasteInjectReviewer for unit tests that
// assert the reviewer kick-off delivery (splash-dismiss → paste → bounded submit
// Enter) directly (hk-7rgqs).
func ExportedPasteInjectReviewer(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	return pasteInjectReviewer(ctx, inj, claudeSessID, wtPath, nil)
}

// ExportedPasteInjectImplementerInitial exposes pasteInjectImplementerInitial for
// unit tests that assert the implementer-initial robust-submit hardening
// (hk-7rgqs).
func ExportedPasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	return pasteInjectImplementerInitial(ctx, inj, claudeSessID, wtPath, nil)
}

// ExportedPasteInjectQuitOnReviewFile exposes pasteInjectQuitOnReviewFile for
// tests in package daemon_test.
//
// hk-7rgqs: now takes inj (pasteInjecter) + claudeSessID so the one-shot re-seed
// path is exercisable; pass nil inj to disable re-seed (the pre-hk-7rgqs
// behaviour).
//
// hk-60t8: now takes eventCh (heartbeat channel; nil = disabled) and
// overrideCeiling (0 = use reviewFileHardCeiling).
//
// Bead: hk-jimbc, hk-7rgqs, hk-60t8.
func ExportedPasteInjectQuitOnReviewFile(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	inj pasteInjecter,
	claudeSessID string,
	wtPath string,
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
	overrideCeiling time.Duration,
) {
	pasteInjectQuitOnReviewFile(ctx, qs, killer, inj, claudeSessID, wtPath, briefDelivered, eventCh, overrideCeiling)
}

// hk-sah87 diff-scaled reviewer-budget test seams.

// ExportedReviewFileHardCeiling is a pointer to the package-level
// reviewFileHardCeiling var (the absolute upper bound on the reviewer-verdict
// wait, regardless of diff size).
//
// Bead: hk-sah87.
var ExportedReviewFileHardCeiling = &reviewFileHardCeiling

// ExportedReviewFilePerKLineBudget is a pointer to the package-level
// reviewFilePerKLineBudget var (extra wait per 1000 changed lines).
//
// Bead: hk-sah87.
var ExportedReviewFilePerKLineBudget = &reviewFilePerKLineBudget

// ExportedReviewerHeartbeatActiveGrace is a pointer to the package-level
// reviewerHeartbeatActiveGrace var.  Tests set *ExportedReviewerHeartbeatActiveGrace
// to a short duration to exercise the heartbeat-based extension path without
// waiting 10 minutes.
//
// Bead: hk-60t8.
var ExportedReviewerHeartbeatActiveGrace = &reviewerHeartbeatActiveGrace

// ExportedReviewBudgetForDiff exposes reviewBudgetForDiff for unit tests.
//
// Bead: hk-sah87.
func ExportedReviewBudgetForDiff(changedLines int, base, perKLine, ceiling time.Duration) time.Duration {
	return reviewBudgetForDiff(changedLines, base, perKLine, ceiling)
}

// ExportedSumNumstatLines exposes sumNumstatLines for unit tests.
//
// Bead: hk-sah87.
func ExportedSumNumstatLines(numstat string) (int, bool) {
	return sumNumstatLines(numstat)
}

// ExportedReviewerBudgetSentinelName re-exports reviewerBudgetSentinelName so
// tests can assert the marker file basename.
//
// Bead: hk-sah87.
const ExportedReviewerBudgetSentinelName = reviewerBudgetSentinelName

// ExportedReadReviewerBudgetSentinelFields reads the reviewer budget-kill marker
// at wtPath and returns its fields (present is false when the marker is absent).
// Exposed so tests in package daemon_test can assert the marker contents without
// access to the unexported reviewerBudgetSentinel struct.
//
// Bead: hk-sah87.
func ExportedReadReviewerBudgetSentinelFields(wtPath string) (present bool, reason string, budgetMS, elapsedMS int64, changedLines int, err error) {
	s, rErr := ReadReviewerBudgetSentinel(wtPath)
	if rErr != nil {
		return false, "", 0, 0, 0, rErr
	}
	if s == nil {
		return false, "", 0, 0, 0, nil
	}
	return true, s.Reason, s.BudgetMS, s.ElapsedMS, s.ChangedLines, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// branching test seams (hk-oe6zt, hk-umxx4)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedBranchingConfig is the exported shape of BranchingConfig for tests.
//
// Bead ref: hk-oe6zt.
type ExportedBranchingConfig = BranchingConfig

// ExportedErrProjectBranchingConfig is a type alias for ErrProjectBranchingConfig
// so tests in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-umxx4.
type ExportedErrProjectBranchingConfig = ErrProjectBranchingConfig

// ExportedParseBranchingSection exposes parseBranchingSection for tests in
// package daemon_test. See branching.go for semantics.
//
// Bead ref: hk-oe6zt.
func ExportedParseBranchingSection(beadBody string) (BranchingConfig, error) {
	return parseBranchingSection(beadBody)
}

// ExportedResolveBranching exposes resolveBranching for tests in package daemon_test.
// See branching.go for semantics.
//
// Bead ref: hk-umxx4, hk-ncwb3.
func ExportedResolveBranching(ctx context.Context, beadBody, projectRoot, targetBranch string) (BranchingConfig, error) {
	return resolveBranching(ctx, beadBody, projectRoot, targetBranch)
}

// ExportedResolveParentCommit exposes resolveParentCommit for tests in package
// daemon_test. See branching.go for semantics.
//
// Bead ref: hk-oe6zt, hk-ncwb3.
func ExportedResolveParentCommit(ctx context.Context, repoRoot, beadID, beadBody, targetBranch string) (string, error) {
	return resolveParentCommit(ctx, repoRoot, beadID, beadBody, targetBranch)
}

// ExportedLandsOnProtectedError is a type alias for LandsOnProtectedError so
// tests in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-ncwb3.
type ExportedLandsOnProtectedError = LandsOnProtectedError

// ExportedCrossRepoUnsafeError is a type alias for CrossRepoUnsafeError so
// tests in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-xfuc.
type ExportedCrossRepoUnsafeError = CrossRepoUnsafeError

// ExportedIsInAllowedRepos exposes isInAllowedRepos for tests in package daemon_test.
//
// Bead ref: hk-xfuc.
func ExportedIsInAllowedRepos(targetRepo string, allowedRepos []string) bool {
	return isInAllowedRepos(targetRepo, allowedRepos)
}

// ─────────────────────────────────────────────────────────────────────────────
// landing strategy test seams (hk-icgp1)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedLandsOnRefError is a type alias for LandsOnRefError so tests in
// package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-icgp1.
type ExportedLandsOnRefError = LandsOnRefError

// ExportedResolveLandsOn exposes resolveLandsOn for tests in package daemon_test.
//
// Bead ref: hk-icgp1.
func ExportedResolveLandsOn(cfg BranchingConfig) string {
	return resolveLandsOn(cfg)
}

// ExportedLandTaskBranch exposes landTaskBranch for tests in package daemon_test.
//
// Bead ref: hk-icgp1.
func ExportedLandTaskBranch(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, runID, beadID string, cfg BranchingConfig) error {
	return landTaskBranch(ctx, repoRoot, mergeWorktreeDir, taskBranch, runID, beadID, cfg)
}

// ─────────────────────────────────────────────────────────────────────────────
// HandlerPausePolicyGoroutine test seams (hk-37zy8)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedHandlerPausePolicyConfig is a type alias for HandlerPausePolicyConfig
// so tests in package daemon_test can reference the type directly.
//
// Bead ref: hk-37zy8.
type ExportedHandlerPausePolicyConfig = HandlerPausePolicyConfig

// ExportedNewHandlerPausePolicyGoroutine exposes NewHandlerPausePolicyGoroutine
// for tests in package daemon_test.
//
// Bead ref: hk-37zy8.
var ExportedNewHandlerPausePolicyGoroutine = NewHandlerPausePolicyGoroutine

// ExportedPolicyHandleRateLimitStatus invokes the unexported
// handleRateLimitStatus method on a HandlerPausePolicyGoroutine for tests in
// package daemon_test.
//
// Bead ref: hk-37zy8.
func ExportedPolicyHandleRateLimitStatus(p *HandlerPausePolicyGoroutine, ctx context.Context, evt core.Event) error {
	return p.handleRateLimitStatus(ctx, evt)
}

// ExportedPolicyHandleBudgetExhausted invokes the unexported
// handleBudgetExhausted method on a HandlerPausePolicyGoroutine for tests in
// package daemon_test.
//
// Bead ref: hk-37zy8.
func ExportedPolicyHandleBudgetExhausted(p *HandlerPausePolicyGoroutine, ctx context.Context, evt core.Event) error {
	return p.handleBudgetExhausted(ctx, evt)
}

// ExportedNewDaemonSpendMeter constructs a DaemonSpendMeter backed by the
// given bus for tests in package daemon_test.
//
// Bead ref: hk-k3f8g.
func ExportedNewDaemonSpendMeter(bus eventbus.EventBus) *DaemonSpendMeter {
	return NewDaemonSpendMeter(bus)
}

// ExportedSpendMeterHandleRunStarted invokes the unexported handleRunStarted
// method on a DaemonSpendMeter for tests in package daemon_test.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterHandleRunStarted(m *DaemonSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleRunStarted(ctx, evt)
}

// ExportedSpendMeterHandleBudgetAccrual invokes the unexported handleBudgetAccrual
// method on a DaemonSpendMeter for tests in package daemon_test.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterHandleBudgetAccrual(m *DaemonSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleBudgetAccrual(ctx, evt)
}

// ExportedSpendMeterSetMaxRunsPerDay overrides the meter's maxRunsPerDay for
// deterministic test scenarios (avoids process-env mutation).
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterSetMaxRunsPerDay(m *DaemonSpendMeter, n int) {
	m.mu.Lock()
	m.maxRunsPerDay = n
	m.mu.Unlock()
}

// ExportedSpendMeterSetDailyCapBytes overrides the meter's dailyCapBytes for
// deterministic test scenarios.
//
// Bead ref: hk-k3f8g.
func ExportedSpendMeterSetDailyCapBytes(m *DaemonSpendMeter, b float64) {
	m.mu.Lock()
	m.dailyCapBytes = b
	m.mu.Unlock()
}

// ExportedNewPerQueueSpendMeter constructs a PerQueueSpendMeter for tests in
// package daemon_test (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedNewPerQueueSpendMeter(reg *RunRegistry, store *QueueStore, projectDir string) *PerQueueSpendMeter {
	return NewPerQueueSpendMeter(reg, store, projectDir)
}

// ExportedPerQueueSpendMeterHandleBudgetAccrual invokes the unexported
// handleBudgetAccrual method on a PerQueueSpendMeter for tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedPerQueueSpendMeterHandleBudgetAccrual(m *PerQueueSpendMeter, ctx context.Context, evt core.Event) error {
	return m.handleBudgetAccrual(ctx, evt)
}

// ExportedPerQueueSpendMeterSetDayKey overrides the meter's UTC day key to a
// past value so the next handled event forces a rollover (which resets counters
// and un-pauses paused-by-budget queues). Used to deterministically exercise the
// rollover un-pause path without waiting for real midnight (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedPerQueueSpendMeterSetDayKey(m *PerQueueSpendMeter, key string) {
	m.mu.Lock()
	m.dayKey = key
	m.mu.Unlock()
}

// ExportedPerQueueSpendMeterSetGlobalCapUSD overrides the meter's globalCapUSD
// for deterministic oversubscription-warning tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedPerQueueSpendMeterSetGlobalCapUSD(m *PerQueueSpendMeter, usd float64) {
	m.mu.Lock()
	m.globalCapUSD = usd
	m.mu.Unlock()
}

// ExportedQueueStoreSetQueue installs q into the QueueStore for tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedQueueStoreSetQueue(s *QueueStore, q *queue.Queue) {
	s.SetQueue(q)
}

// ExportedRunRegistryRegister registers a handle under runID for tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedRunRegistryRegister(r *RunRegistry, runID core.RunID, handle *RunHandle) {
	r.Register(runID, handle)
}

// ExportedPasteInjectOnLaunch exposes pasteInjectOnLaunch for tests in package
// daemon_test.  Returns the briefDelivered channel (hk-930o3).
//
// bus and runID are passed as zero values (nil / uuid.Nil) so tests that do
// not need pasteinject_failed event emission continue to work without changes.
//
// Bead ref: hk-zrj83, hk-930o3, hk-fra5l.
func ExportedPasteInjectOnLaunch(
	ctx context.Context,
	substrate handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
) <-chan struct{} {
	return pasteInjectOnLaunch(ctx, substrate, claudeSessID, phase, iterCount, wtPath, nil, core.RunID{})
}

// ExportedBufferName exposes the bufferName helper for tests in package
// daemon_test.
//
// Bead ref: hk-zrj83.
func ExportedBufferName(sessionID, purpose string) string {
	return bufferName(sessionID, purpose)
}

// ExportedSynthesiseClaudeSessionID exposes rlSynthesiseClaudeSessionID for
// tests in package daemon_test.  Tests use this to verify the produced ID
// satisfies the tmux buffer-name regex (hk-lckbv).
func ExportedSynthesiseClaudeSessionID() string {
	return rlSynthesiseClaudeSessionID()
}

// ExportedResolveIter1ClaudeSessionID exposes rlResolveIter1ClaudeSessionID for
// tests in package daemon_test (hk-za5mz). Verifies the iteration-1 session-id
// resolution order: interceptor id → real minted id → synthesis.
func ExportedResolveIter1ClaudeSessionID(interceptorID, realMintedID string) string {
	return rlResolveIter1ClaudeSessionID(interceptorID, realMintedID)
}

// ExportedNewPerRunSubstrate wraps newPerRunSubstrate for tests in package
// daemon_test that need per-run pane isolation without importing the unexported
// type directly.
//
// Returns nil when sub is nil or is not a *tmuxSubstrate (matching
// newPerRunSubstrate semantics). Tests that call WriteLastPane on the returned
// value must call SpawnWindow first to capture the pane target.
//
// Passes "" for handlerBinary so agentCommandFragments defaults to
// livePaneCommandSubstrings, preserving the existing test behaviour.
//
// Bead ref: hk-jfh59, hk-vhped.
func ExportedNewPerRunSubstrate(sub handler.Substrate) handler.Substrate {
	prs := newPerRunSubstrate(sub, "", nil)
	if prs == nil {
		return nil
	}
	return prs
}

// ExportedStatTaskFileVia exposes statTaskFileVia for unit tests in package
// daemon_test.  The runner is used for remote stat checks (hk-hh5e); nil runner
// falls back to local os.Stat (same as statTaskFile).
//
// Bead: hk-hh5e.
func ExportedStatTaskFileVia(ctx context.Context, runner tmuxPkg.CommandRunner, path string) error {
	return statTaskFileVia(ctx, runner, path)
}

// ─────────────────────────────────────────────────────────────────────────────
// Project config + model resolution test seams (hk-bfvk7)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedProjectConfig is a type alias for ProjectConfig so tests in package
// daemon_test can reference the type directly.
//
// Bead ref: hk-bfvk7.
type ExportedProjectConfig = ProjectConfig

// ExportedErrMalformedConfigYAML is a type alias so tests can use errors.As.
//
// Bead ref: hk-bfvk7.
type ExportedErrMalformedConfigYAML = ErrMalformedConfigYAML

// ExportedErrUnsupportedConfigVersion is a type alias so tests can use errors.As.
//
// Bead ref: hk-bfvk7.
type ExportedErrUnsupportedConfigVersion = ErrUnsupportedConfigVersion

// ExportedErrUnknownConfigKey is a type alias so tests can use errors.As to
// assert that an unknown key under keeper: is rejected (hk-9f3f).
//
// Bead ref: hk-9f3f.
type ExportedErrUnknownConfigKey = ErrUnknownConfigKey

// ExportedErrWorkflowModeFloorViolation is a type alias so tests can use errors.As.
//
// Bead ref: hk-rcp7.
type ExportedErrWorkflowModeFloorViolation = ErrWorkflowModeFloorViolation

// ExportedDaemonConfig is a type alias for DaemonConfig so tests in package
// daemon_test can reference the type directly without importing internal types.
//
// Bead ref: hk-rcp7.
type ExportedDaemonConfig = DaemonConfig

// ExportedKeeperConfig is a type alias for KeeperConfig so tests in package
// daemon_test can reference the type directly without importing internal types.
//
// Bead ref: hk-lhu2.
type ExportedKeeperConfig = KeeperConfig

// ExportedWatchdogConfig is a type alias for WatchdogConfig so tests in
// package daemon_test can read parsed watchdog config fields directly.
//
// Bead ref: hk-sbitr.
type ExportedWatchdogConfig = WatchdogConfig

// ExportedSuperviseConfig is a type alias for SuperviseConfig so tests in
// package daemon_test can read parsed supervise config fields directly.
type ExportedSuperviseConfig = SuperviseConfig

// ExportedLoadProjectConfig exposes LoadProjectConfig for tests in package daemon_test.
//
// Bead ref: hk-bfvk7.
func ExportedLoadProjectConfig(repoRoot string) (ProjectConfig, error) {
	return LoadProjectConfig(repoRoot)
}

// ExportedRawKeeperConfig is a type alias for rawKeeperConfig so tests in
// package daemon_test can construct keeper-block fixtures directly.
//
// Bead ref: hk-exg3.
type ExportedRawKeeperConfig = rawKeeperConfig

// ExportedRawKeeperContextThresholds is a type alias for the nested
// context_thresholds sub-struct so tests can set a single field.
//
// Bead ref: hk-exg3.
type ExportedRawKeeperContextThresholds = rawKeeperContextThresholds

// ExportedRawKeeperWarnMessages is a type alias for the nested warn_messages
// sub-struct so tests can set a single field.
//
// Bead ref: hk-exg3.
type ExportedRawKeeperWarnMessages = rawKeeperWarnMessages

// ExportedRawKeeperHardCeiling, ...Timings, ...Cadence, ...Budgets, and
// ...SelfService are type aliases for the keeper sub-blocks added in hk-9kgf so
// tests can construct single-field keeper fixtures for the keeperBlockAbsent
// per-field coverage (hk-exg3 invariant).
//
// Bead ref: hk-9kgf.
type ExportedRawKeeperHardCeiling = rawKeeperHardCeiling

// ExportedRawKeeperTimings — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
type ExportedRawKeeperTimings = rawKeeperTimings

// ExportedRawKeeperCadence — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
type ExportedRawKeeperCadence = rawKeeperCadence

// ExportedRawKeeperBudgets — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
type ExportedRawKeeperBudgets = rawKeeperBudgets

// ExportedRawKeeperSelfService — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
type ExportedRawKeeperSelfService = rawKeeperSelfService

// ExportedKeeperBlockAbsent exposes keeperBlockAbsent for tests in package
// daemon_test (hk-exg3): the explicit field-by-field zero check that replaces
// the `== (rawKeeperConfig{})` empty-block sentinel.
//
// Bead ref: hk-exg3.
func ExportedKeeperBlockAbsent(raw ExportedRawKeeperConfig) bool {
	return keeperBlockAbsent(raw)
}

// ExportedResolveModelPreference exposes ResolveModelPreference for tests.
//
// Bead ref: hk-bfvk7.
func ExportedResolveModelPreference(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (model, effort string) {
	return ResolveModelPreference(ctx, beadLabels, agentType, projectCfg, bus, beadID)
}

// WorkLoopDepsWithProjectCfg returns a copy of params with ProjectCfg set to cfg.
// Used by integration tests to inject a non-zero ProjectConfig into the work loop.
//
// Bead ref: hk-bfvk7.
func WorkLoopDepsWithProjectCfg(p WorkLoopDepsParams, cfg ProjectConfig) WorkLoopDepsParams {
	p.ProjectCfg = cfg
	return p
}

// HandlerEnvOf returns the handlerEnv field from deps.
// Used by tests to assert HARMONIK_PROJECT_HASH injection (hk-nvrvp).
func HandlerEnvOf(deps workLoopDeps) []string {
	return deps.handlerEnv
}

// ─────────────────────────────────────────────────────────────────────────────
// QueueStore test seams (hk-j808w)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewQueueStore exposes newQueueStore for tests in package daemon_test.
// QueueStore and its methods (SetQueue, Queue, ClearQueue, LockForMutation) are
// exported; only the constructor is unexported.
//
// Bead ref: hk-j808w.
func ExportedNewQueueStore() *QueueStore {
	return newQueueStore()
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
	return newWorkLoopDeps(cfg, bus, workflowModeDefault, registry, store)
}

// ExportedEvaluateGroupAdvanceWithOutcome exposes evaluateGroupAdvanceWithOutcome
// for tests in package daemon_test. Drives EM-015f group-advance evaluation
// directly without running a full work loop cycle.
//
// queueName (NQ-B1) selects the queue slot the completion path resolves; pass
// "" for the main queue (it normalises to "main").
//
// Bead ref: hk-45ude, hk-tigaf.4.
func ExportedEvaluateGroupAdvanceWithOutcome(ctx context.Context, deps workLoopDeps, queueName string, queueID string, groupIndex int, itemIdx int, success bool) {
	evaluateGroupAdvanceWithOutcome(ctx, deps, queueName, queueID, groupIndex, itemIdx, success)
}

// ExportedQueueStoreOf returns deps.queueStore. Used by tests to observe the
// active queue after work-loop cycles in hk-45ude queue-dispatch tests.
//
// Bead ref: hk-45ude.
func ExportedQueueStoreOf(deps workLoopDeps) *QueueStore {
	return deps.queueStore
}

// ExportedProjectCfgOf returns the projectCfg field from deps for inspection.
//
// Bead ref: hk-bfvk7.
func ExportedProjectCfgOf(deps workLoopDeps) ProjectConfig {
	return deps.projectCfg
}

// ─────────────────────────────────────────────────────────────────────────────
// pasteInjectQuitOnCommit timeout-recovery test seams (hk-trjef)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedBriefDeliveredTimeout is a pointer to the package-level
// briefDeliveredTimeout var.  Tests set *ExportedBriefDeliveredTimeout to a
// short duration to exercise the timeout path without waiting 2 minutes.
//
// Bead: hk-930o3.
var ExportedBriefDeliveredTimeout = &briefDeliveredTimeout

// ExportedCommitPollTimeout is a pointer to the package-level commitPollTimeout
// var.  Tests set *ExportedCommitPollTimeout to a short duration to avoid
// waiting 10 min for the timeout path.
//
// Bead: hk-trjef.
var ExportedCommitPollTimeout = &commitPollTimeout

// ExportedNoChangeKillDelay is a pointer to the package-level noChangeKillDelay
// var.  Tests set *ExportedNoChangeKillDelay to a short duration to avoid
// waiting 30 s for the kill path.
//
// Bead: hk-trjef.
var ExportedNoChangeKillDelay = &noChangeKillDelay

// ExportedPostQuitKillGrace is a pointer to the package-level postQuitKillGrace
// var.  Tests set *ExportedPostQuitKillGrace to a short duration to exercise the
// post-commit /quit watchdog without waiting 60 s of wall time.
//
// Bead: hk-5s7tg.
var ExportedPostQuitKillGrace = &postQuitKillGrace

// ExportedResumeSubmitRetries and ExportedResumeSubmitRetryDelay are pointers to
// the package-level implementer-resume submit-retry tunables.  Tests set the
// delay to a short duration so the bounded submit retry on the resume paste path
// (the hk-ip33d fix) runs without burning real wall time.
//
// Bead: hk-ip33d.
var (
	ExportedResumeSubmitRetries    = &resumeSubmitRetries
	ExportedResumeSubmitRetryDelay = &resumeSubmitRetryDelay
)

// ExportedCommitPollInterval is a pointer to the package-level commitPollInterval
// var.  Tests set *ExportedCommitPollInterval to a short duration to keep
// polling tight during timeout tests.
//
// Bead: hk-trjef.
var ExportedCommitPollInterval = &commitPollInterval

// ExportedSessionKiller is the exported alias for the sessionKiller interface so
// tests can implement it without naming the unexported type.
//
// Bead: hk-trjef.
type ExportedSessionKiller = sessionKiller

// ExportedPasteInjectQuitOnCommit exposes pasteInjectQuitOnCommit for tests.
//
// eventCh may be nil; when nil the heartbeat-staleness check is skipped and
// only the wall-clock commitPollTimeout acts as the kill trigger.
//
// This wrapper passes a nil bus and a zero runID (no implementer_budget_exceeded
// emission); use ExportedPasteInjectQuitOnCommitWithBus when the test needs to
// observe the hk-9vp51 diagnostic.
//
// Beads: hk-trjef, hk-930o3, hk-7srrd.
func ExportedPasteInjectQuitOnCommit(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
) {
	pasteInjectQuitOnCommit(ctx, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh, nil, core.RunID{})
}

// ExportedPasteInjectQuitOnCommitWithBus is like ExportedPasteInjectQuitOnCommit
// but threads a bus and runID so tests can observe the hk-9vp51
// implementer_budget_exceeded diagnostic emitted on a commit-budget kill.
//
// Bead: hk-9vp51.
func ExportedPasteInjectQuitOnCommitWithBus(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) {
	pasteInjectQuitOnCommit(ctx, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh, bus, runID)
}

// ExportedCommitHardCeiling is a pointer to the package-level commitHardCeiling
// var.  Tests set *ExportedCommitHardCeiling to a short duration to exercise the
// absolute-backstop kill path quickly (hk-9vp51).
var ExportedCommitHardCeiling = &commitHardCeiling

// ExportedHeartbeatStalenessThreshold is a pointer to the package-level
// heartbeatStalenessThreshold var.  Tests set *ExportedHeartbeatStalenessThreshold
// to a short duration to exercise the heartbeat-stale kill path quickly.
//
// Bead: hk-7srrd.
var ExportedHeartbeatStalenessThreshold = &heartbeatStalenessThreshold

// ExportedLaunchHeartbeatTimeout is a pointer to the package-level
// launchHeartbeatTimeout var.  Tests set *ExportedLaunchHeartbeatTimeout to a
// short duration to exercise the launch-verification kill path quickly.
//
// Bead: hk-3gq0b.
var ExportedLaunchHeartbeatTimeout = &launchHeartbeatTimeout

// ExportedLaunchSuppressionCeiling is a pointer to the package-level
// launchSuppressionCeiling var. Tests set *ExportedLaunchSuppressionCeiling to a
// short duration to prove the launch-verification suppression terminates even
// when the pane reports an active child process forever (hk-jgxqc).
//
// Bead: hk-jgxqc.
var ExportedLaunchSuppressionCeiling = &launchSuppressionCeiling

// quitSenderExported is the exported alias for quitSender so the exported
// wrapper can accept it.
type quitSenderExported = quitSender

// ExportedBeadAlreadySubsumedInMain exposes beadAlreadySubsumedInMain for tests.
//
// Bead: hk-trjef.
func ExportedBeadAlreadySubsumedInMain(ctx context.Context, projectDir string, beadID core.BeadID) bool {
	return beadAlreadySubsumedInMain(ctx, projectDir, beadID)
}

// ExportedBeadExplicitlyReopened exposes beadExplicitlyReopened for tests.
//
// Bead: hk-wcv.
func ExportedBeadExplicitlyReopened(ctx context.Context, auditLogger func(context.Context, core.BeadID) ([]brcli.AuditEvent, error), beadID core.BeadID) bool {
	return beadExplicitlyReopened(ctx, auditLogger, beadID)
}

// ExportedAutoCloseStaleBlockersOnClaimFailure exposes
// autoCloseStaleBlockersOnClaimFailure for unit tests via WorkLoopDepsParams.
//
// Bead: hk-rnsjs.
func ExportedAutoCloseStaleBlockersOnClaimFailure(ctx context.Context, p WorkLoopDepsParams, beadID core.BeadID) {
	autoCloseStaleBlockersOnClaimFailure(ctx, ExportedWorkLoopDeps(p), beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// pane liveness checker test seams (hk-fbydv)
// ─────────────────────────────────────────────────────────────────────────────

// PaneLivenessCheckerExported is an exported alias for the paneLivenessChecker
// interface so tests in package daemon_test can implement stubs without naming
// the unexported type.
//
// Bead: hk-fbydv.
type PaneLivenessCheckerExported = paneLivenessChecker

// PaneOutputSizerExported is an exported alias for the paneOutputSizer
// interface so tests in package daemon_test can implement stubs without naming
// the unexported type.
//
// Bead: hk-ue0u2.
type PaneOutputSizerExported = paneOutputSizer

// ExportedHasChildProcess exposes hasChildProcess for tests in package
// daemon_test.
//
// Bead: hk-fbydv.
func ExportedHasChildProcess(pid int) bool {
	return hasChildProcess(pid)
}

// ExportedLivePaneCommandSubstrings exposes the agent-command match list for
// the hk-tgqy5 self-command liveness path so tests can temporarily extend it to
// match the test binary's own comm name and exercise the branch with a real
// running PID.
//
// Bead: hk-tgqy5.
var ExportedLivePaneCommandSubstrings = &livePaneCommandSubstrings

// ─────────────────────────────────────────────────────────────────────────────
// QueueOperatorEventConsumer test seams (hk-7urls)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedQueueOperatorEventConsumerConfig is a type alias for
// QueueOperatorEventConsumerConfig for tests in package daemon_test.
//
// Bead ref: hk-7urls.
type ExportedQueueOperatorEventConsumerConfig = QueueOperatorEventConsumerConfig

// ExportedNewQueueOperatorEventConsumer exposes NewQueueOperatorEventConsumer
// for tests in package daemon_test.
//
// Bead ref: hk-7urls.
var ExportedNewQueueOperatorEventConsumer = NewQueueOperatorEventConsumer

// ExportedQueueOpConsumerHandlePauseStatus invokes the unexported
// handleOperatorPauseStatus method for tests in package daemon_test.
//
// Bead ref: hk-7urls.
func ExportedQueueOpConsumerHandlePauseStatus(c *QueueOperatorEventConsumer, ctx context.Context, evt core.Event) error {
	return c.handleOperatorPauseStatus(ctx, evt)
}

// ExportedQueueOpConsumerHandleResuming invokes the unexported
// handleOperatorResuming method for tests in package daemon_test.
//
// Bead ref: hk-7urls.
func ExportedQueueOpConsumerHandleResuming(c *QueueOperatorEventConsumer, ctx context.Context, evt core.Event) error {
	return c.handleOperatorResuming(ctx, evt)
}

// ─────────────────────────────────────────────────────────────────────────────
// runWait ctx-cancel test seams (hk-88nno)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedRunWaitResult is the exported result of a runWait call for tests.
//
// Bead ref: hk-88nno.
type ExportedRunWaitResult struct {
	ExitCode int
}

// ExportedRunWaitWithDeadFn drives tmuxSubstrateSession.runWait through a
// forced ctx.Done() and returns the exit code recorded in outcome.
//
// pid is set on the session. deadFn replaces processDead for this call —
// pass a function that returns true to simulate a dead process, false for alive.
// The caller-supplied ctx is cancelled immediately after runWait is launched so
// that the ctx.Done() branch fires on the first select iteration.
//
// Bead ref: hk-88nno.
func ExportedRunWaitWithDeadFn(pid int, deadFn func(int) bool) ExportedRunWaitResult {
	sess := &tmuxSubstrateSession{
		adapter:       &noopTmuxAdapter{},
		handle:        "test-session:hk-88nno-win",
		pid:           pid,
		waitDone:      make(chan struct{}),
		isProcessDead: deadFn,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so ctx.Done() fires on the first select
	sess.runWait(ctx)
	return ExportedRunWaitResult{ExitCode: sess.outcome.ExitCode}
}

// noopTmuxAdapter is a minimal tmux.Adapter stub that satisfies the interface
// for the runWait test seam. Only WindowPanePID is reachable from runWait, and
// only in the pid==0 slow-path; ExportedRunWaitWithDeadFn always sets pid>0.
type noopTmuxAdapter struct{}

func (n *noopTmuxAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (n *noopTmuxAdapter) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
func (n *noopTmuxAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (n *noopTmuxAdapter) NewWindowIn(_ context.Context, _ tmuxPkg.NewWindowIn) tmuxPkg.Outcome {
	return tmuxPkg.Outcome{}
}
func (n *noopTmuxAdapter) KillWindow(_ context.Context, _ tmuxPkg.WindowHandle) error { return nil }
func (n *noopTmuxAdapter) WindowPanePID(_ context.Context, _ tmuxPkg.WindowHandle) (int, error) {
	return 0, nil
}

func (n *noopTmuxAdapter) WindowPaneID(_ context.Context, _ tmuxPkg.WindowHandle) (string, error) {
	return "", nil
}
func (n *noopTmuxAdapter) KillSession(_ context.Context, _ string) error              { return nil }
func (n *noopTmuxAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error     { return nil }
func (n *noopTmuxAdapter) PasteBuffer(_ context.Context, _, _ string) error           { return nil }
func (n *noopTmuxAdapter) SendKeysLiteral(_ context.Context, _, _ string) error       { return nil }
func (n *noopTmuxAdapter) SendKeysEnter(_ context.Context, _ string) error            { return nil }
func (n *noopTmuxAdapter) SendKeysQuit(_ context.Context, _ string) error             { return nil }
func (n *noopTmuxAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error { return nil }

// Compile-time assertion: noopTmuxAdapter implements tmux.Adapter.
var _ tmuxPkg.Adapter = (*noopTmuxAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// buildCodexLaunchSpec test seams (hk-rgxwd C2/T7)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedCodexRunCtx is the exported shape of codexRunCtx for tests.
// Fields mirror codexRunCtx verbatim with exported names.
//
// Bead refs: hk-rgxwd (T7), hk-tu48u (T11 billing-guard fields).
type ExportedCodexRunCtx struct {
	CodexBinary   string
	WorkspacePath string
	BeadID        string
	PriorThreadID *string
	BaseEnv       []string
	CodexHome     string
	// BillingEmitter / RunID / SkipBillingGuard expose the C3/T11 positive
	// billing-guard seams (hk-tu48u).
	BillingEmitter   handlercontract.EventEmitter
	RunID            core.RunID
	SkipBillingGuard bool
}

// ExportedBuildCodexLaunchSpec exposes buildCodexLaunchSpec for tests in
// package daemon_test. The ExportedCodexRunCtx is translated to the internal
// codexRunCtx before calling.
//
// Bead ref: hk-rgxwd.
func ExportedBuildCodexLaunchSpec(rc ExportedCodexRunCtx) (handler.LaunchSpec, error) {
	return buildCodexLaunchSpec(codexRunCtx{
		codexBinary:      rc.CodexBinary,
		workspacePath:    rc.WorkspacePath,
		beadID:           rc.BeadID,
		priorThreadID:    rc.PriorThreadID,
		baseEnv:          rc.BaseEnv,
		codexHome:        rc.CodexHome,
		billingEmitter:   rc.BillingEmitter,
		runID:            rc.RunID,
		skipBillingGuard: rc.SkipBillingGuard,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// codex billing guard test seams (hk-tu48u C3/T11)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedMaterializeForcedLoginMethod exposes materializeForcedLoginMethod for
// tests in package daemon_test.
//
// Bead ref: hk-tu48u.
func ExportedMaterializeForcedLoginMethod(codexHome string) error {
	return materializeForcedLoginMethod(codexHome)
}

// ExportedAssertChatGPTPlan exposes the fail-closed assertChatGPTPlan for tests
// in package daemon_test.
//
// Bead ref: hk-tu48u.
func ExportedAssertChatGPTPlan(codexHome string) error {
	return assertChatGPTPlan(codexHome)
}

// ExportedRunCodexBillingGuard exposes runCodexBillingGuard (materialize + assert
// + emit) for tests in package daemon_test.
//
// Bead ref: hk-tu48u.
func ExportedRunCodexBillingGuard(bus handlercontract.EventEmitter, beadID, codexHome string) error {
	return runCodexBillingGuard(context.Background(), bus, core.RunID{}, beadID, codexHome)
}

// ExportedForcedLoginMethodValue is the value the guard materializes / asserts.
// Bead ref: hk-tu48u.
const ExportedForcedLoginMethodValue = forcedLoginMethodValue

// ─────────────────────────────────────────────────────────────────────────────
// buildCrewLaunchSpec test seams (hk-kbqto C2)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedCrewLaunchCtx is the exported shape of crewLaunchCtx for tests.
//
// Bead ref: hk-kbqto, hk-4z0gp.
type ExportedCrewLaunchCtx struct {
	ClaudeBinary string
	Name         string
	// RcPrefix is the per-project --remote-control label prefix (hk-igpg); "" =
	// bare label.
	RcPrefix   string
	SessionID  string
	ProjectDir string
	// Resume, when true, builds argv with --resume instead of --session-id
	// (stale re-launch path per c2-spec.md §7).
	Resume bool
	// Model is the optional per-crew model alias (hk-9j3z); empty injects no
	// --model flag.
	Model string
}

// ExportedBuildCrewLaunchSpec exposes buildCrewLaunchSpec for tests in package
// daemon_test. See crewlaunchspec.go for semantics.
//
// Bead ref: hk-kbqto, hk-4z0gp.
func ExportedBuildCrewLaunchSpec(rc ExportedCrewLaunchCtx) (handler.LaunchSpec, error) {
	return buildCrewLaunchSpec(crewLaunchCtx{
		claudeBinary: rc.ClaudeBinary,
		name:         rc.Name,
		rcPrefix:     rc.RcPrefix,
		sessionID:    rc.SessionID,
		projectDir:   rc.ProjectDir,
		resume:       rc.Resume,
		model:        rc.Model,
	})
}

// ExportedReadMissionModel exposes readMissionModel for tests in package
// daemon_test (hk-9j3z): reads the optional model: front-matter field.
func ExportedReadMissionModel(missionPath string) string {
	return readMissionModel(missionPath)
}

// ─────────────────────────────────────────────────────────────────────────────
// OperatorPauseController test seams (hk-ry8q1)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewOperatorPauseController exposes NewOperatorPauseController for
// tests in package daemon_test.
//
// Bead ref: hk-ry8q1.
var ExportedNewOperatorPauseController = NewOperatorPauseController

// ─────────────────────────────────────────────────────────────────────────────
// Auto-resume test seams (hk-0otqs)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedAutoResumeConfig is a type alias for AutoResumeConfig so tests in
// package daemon_test can reference the type directly.
//
// Bead ref: hk-0otqs.
type ExportedAutoResumeConfig = AutoResumeConfig

// ExportedHandlerPauseControllerSchedule exposes HandlerPauseController.Schedule
// for tests in package daemon_test.
//
// Bead ref: hk-0otqs.
func ExportedHandlerPauseControllerSchedule(c *HandlerPauseController, ctx context.Context, agentType core.AgentType, after time.Duration) {
	c.Schedule(ctx, agentType, after)
}

// ExportedHandlerPauseControllerSetAutoResumeCfg exposes
// HandlerPauseController.SetAutoResumeConfig for tests in package daemon_test.
//
// Bead ref: hk-0otqs.
func ExportedHandlerPauseControllerSetAutoResumeCfg(c *HandlerPauseController, agentType core.AgentType, cfg AutoResumeConfig) {
	c.SetAutoResumeConfig(agentType, cfg)
}

// ─────────────────────────────────────────────────────────────────────────────
// brQueueLedger test seam (hk-dv8qv — ledger-dep direction regression)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedQueueLedger is the read seam tests use to exercise the production
// brQueueLedger.BlocksEdge / LookupStatus against a real brcli.Adapter (wired
// to a mock `br` binary). It mirrors the queue.BeadLedger surface.
type ExportedQueueLedger interface {
	LookupStatus(ctx context.Context, id core.BeadID) (queue.BeadStatus, error)
	BlocksEdge(ctx context.Context, blocker, blocked core.BeadID) (bool, error)
}

// ExportedNewBRQueueLedger constructs the production brQueueLedger over adapter
// so package daemon_test can verify the ledger-dep edge direction (hk-dv8qv).
func ExportedNewBRQueueLedger(adapter *brcli.Adapter) ExportedQueueLedger {
	return newBRQueueLedger(adapter)
}

// ─────────────────────────────────────────────────────────────────────────────
// Escape-detector test seams (hk-ooexj — gitignored/pre-existing false positive)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedSnapshotUntrackedFiles exposes snapshotUntrackedFiles for the
// escape-detector baseline regression test (hk-ooexj).
func ExportedSnapshotUntrackedFiles(ctx context.Context, mainPath string) (map[string]struct{}, error) {
	return snapshotUntrackedFiles(ctx, mainPath)
}

// ExportedCheckMainWorkingTreeDirty exposes checkMainWorkingTreeDirty for the
// escape-detector regression tests (hk-ooexj, hk-xux36). baseline is the set
// of pre-existing untracked paths captured at run-start.
func ExportedCheckMainWorkingTreeDirty(ctx context.Context, mainPath string, baseline map[string]struct{}) (bool, []string, error) {
	return checkMainWorkingTreeDirty(ctx, mainPath, baseline)
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-run event-tap fan-out test seams (hk-37giq)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedPerRunEventTap is the exported alias for the per-run fan-out event
// tap so the competing-consumer race regression test can construct one and
// register multiple independent subscribers (hk-37giq).
type ExportedPerRunEventTap = perRunEventTap

// noopExportedEmitter is a no-op handlercontract.EventEmitter used as the tap's
// underlying bus in the fan-out regression test: it discards all emits so the
// test exercises ONLY the per-subscriber fan-out behaviour.
type noopExportedEmitter struct{}

func (noopExportedEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }
func (noopExportedEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

// ExportedNewPerRunEventTap constructs a perRunEventTap backed by a no-op
// underlying emitter and returns the tap plus its initial subscriber channel
// (the same channel newChanAgentEventSource/waitAgentReady consumes in
// production). Additional independent subscribers are obtained via
// tap.ExportedSubscribe (hk-37giq).
func ExportedNewPerRunEventTap(runID core.RunID) (*ExportedPerRunEventTap, <-chan core.EventEnvelope) {
	return newPerRunEventTap(noopExportedEmitter{}, runID)
}

// ExportedSubscribe registers and returns a new independent subscriber channel
// on the tap (hk-37giq).
func (t *ExportedPerRunEventTap) ExportedSubscribe() <-chan core.EventEnvelope {
	return t.Subscribe()
}

// ExportedEmit fans an event of eventType out to every subscriber via the tap's
// production Emit path (hk-37giq).
func (t *ExportedPerRunEventTap) ExportedEmit(ctx context.Context, eventType core.EventType) error {
	return t.Emit(ctx, eventType, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// ClaudeHarness test seams (hk-3kyh3 C1/T2)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewClaudeHarness re-exports NewClaudeHarness for tests.
//
// Bead ref: hk-3kyh3.
var ExportedNewClaudeHarness = NewClaudeHarness

// ExportedNewHarnessRegistry exposes newHarnessRegistry for tests in package
// daemon_test. It returns the daemon's HarnessRegistry with ClaudeHarness
// registered for core.AgentTypeClaudeCode (claude-only in C1/T3).
//
// Bead ref: hk-hj9ld.
func ExportedNewHarnessRegistry() (*handlercontract.HarnessRegistry, error) {
	return newHarnessRegistry()
}

// ExportedCodexProcessExitLaunchSpecBuilder returns a launchSpecBuilder that
// produces a handler.LaunchSpec for the provided shell script and stamps
// resolvedAgentType = core.AgentTypeCodex on the returned artifacts.
//
// Use in tests that exercise the hk-f6g7 ProcessExit-skips-waitAgentReady gate:
// wire AdapterRegistry2 with RegisterCodex and HarnessRegistry from
// ExportedNewHarnessRegistry, then supply this builder so beadRunOne looks up
// the codex adapter + harness (Completion() == CompletionProcessExit).
//
// The script is expected to run in the worktree directory (handler.LaunchSpec.WorkDir
// = claudeRunCtx.workspacePath).  It MUST make a "Refs: <beadID>" git commit and
// exit 0; it MUST NOT emit agent_ready.
//
// Bead ref: hk-f6g7.
func ExportedCodexProcessExitLaunchSpecBuilder(scriptPath string) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(_ context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		spec := handler.LaunchSpec{
			Binary:  "/bin/sh",
			Args:    []string{scriptPath},
			WorkDir: rc.workspacePath,
			Role:    string(rc.phase),
		}
		arts := claudeRunArtifacts{
			resolvedAgentType: core.AgentTypeCodex,
		}
		return spec, arts, nil
	}
}

// ExportedRoutedLaunchSpecBuilder exposes routedLaunchSpecBuilder for tests in
// package daemon_test. It returns a builder that resolves the harness via the
// four-tier precedence walk and the HarnessRegistry, then (for the claude
// harness) delegates to buildClaudeLaunchSpec. The returned closure has the same
// shape as the workLoopDeps.launchSpecBuilder hook; the artifacts are translated
// to the exported shape for comparison against ExportedBuildClaudeLaunchSpec.
//
// Bead ref: hk-hj9ld.
func ExportedRoutedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	builder := routedLaunchSpecBuilder(reg, bead, queueDefault, nodeDefault, globalDefault, bus)
	return func(ctx context.Context, rc ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
		internal := claudeRunCtx{
			runID:             rc.RunID,
			beadID:            rc.BeadID,
			workspacePath:     rc.WorkspacePath,
			daemonSocket:      rc.DaemonSocket,
			workflowMode:      rc.WorkflowMode,
			phase:             rc.Phase,
			iterationCount:    rc.IterationCount,
			priorClaudeSessID: rc.PriorClaudeSessID,
			handlerBinary:     rc.HandlerBinary,
			daemonBinaryPath:  rc.DaemonBinaryPath,
			baseEnv:           rc.BaseEnv,
			model:             rc.Model,
			effort:            rc.Effort,
			worktreeRootPath:  rc.WorktreeRootPath,
			beadDescription:   rc.BeadDescription,
			nodePrompt:        rc.NodePrompt,
		}
		spec, arts, err := builder(ctx, internal)
		if err != nil {
			return handler.LaunchSpec{}, ExportedClaudeRunArtifacts{}, err
		}
		return spec, ExportedClaudeRunArtifacts{
			ClaudeSessionID:  arts.claudeSessionID,
			SessionLogPath:   arts.sessionLogPath,
			HandlerSessionID: arts.handlerSessionID,
			PreExecMsgs:      arts.preExecMsgs,
			Substrate:        arts.substrate,
		}, nil
	}
}

// ExportedPinnedHarnessLaunchSpecBuilder exposes pinnedHarnessLaunchSpecBuilder
// for tests in package daemon_test. It returns a builder that uses agentType
// directly (bypassing resolveHarness) so tests can assert the node-level pin
// wins unconditionally over any bead label (hk-2jxqg).
func ExportedPinnedHarnessLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	agentType core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	builder := pinnedHarnessLaunchSpecBuilder(reg, bead, agentType, bus)
	return func(ctx context.Context, rc ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
		internal := claudeRunCtx{
			runID:             rc.RunID,
			beadID:            rc.BeadID,
			workspacePath:     rc.WorkspacePath,
			daemonSocket:      rc.DaemonSocket,
			workflowMode:      rc.WorkflowMode,
			phase:             rc.Phase,
			iterationCount:    rc.IterationCount,
			priorClaudeSessID: rc.PriorClaudeSessID,
			handlerBinary:     rc.HandlerBinary,
			daemonBinaryPath:  rc.DaemonBinaryPath,
			baseEnv:           rc.BaseEnv,
			model:             rc.Model,
			effort:            rc.Effort,
			worktreeRootPath:  rc.WorktreeRootPath,
			beadDescription:   rc.BeadDescription,
			nodePrompt:        rc.NodePrompt,
		}
		spec, arts, err := builder(ctx, internal)
		if err != nil {
			return handler.LaunchSpec{}, ExportedClaudeRunArtifacts{}, err
		}
		return spec, ExportedClaudeRunArtifacts{
			ClaudeSessionID:  arts.claudeSessionID,
			SessionLogPath:   arts.sessionLogPath,
			HandlerSessionID: arts.handlerSessionID,
			PreExecMsgs:      arts.preExecMsgs,
			Substrate:        arts.substrate,
		}, nil
	}
}

// ExportedRunCtxFromClaudeRunCtx converts an ExportedClaudeRunCtx into the
// handlercontract.RunCtx shape expected by ClaudeHarness.LaunchSpec.  This
// allows harness-golden tests to use the same fixture builders as the
// buildClaudeLaunchSpec tests and compare outputs side-by-side.
//
// Bead ref: hk-3kyh3.
func ExportedRunCtxFromClaudeRunCtx(rc ExportedClaudeRunCtx) handlercontract.RunCtx {
	return handlercontract.RunCtx{
		RunID:            rc.RunID,
		BeadID:           rc.BeadID,
		WorkspacePath:    rc.WorkspacePath,
		DaemonSocket:     rc.DaemonSocket,
		WorkflowMode:     rc.WorkflowMode,
		Phase:            rc.Phase,
		IterationCount:   rc.IterationCount,
		PriorSessionID:   rc.PriorClaudeSessID,
		HandlerBinary:    rc.HandlerBinary,
		DaemonBinaryPath: rc.DaemonBinaryPath,
		BaseEnv:          rc.BaseEnv,
		Model:            rc.Model,
		Effort:           rc.Effort,
		WorktreeRootPath: rc.WorktreeRootPath,
		BeadDescription:  rc.BeadDescription,
		NodePrompt:       rc.NodePrompt,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CodexHarness + codex JSONL parser test seams (hk-m57va C2/T8)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewCodexHarness re-exports NewCodexHarness for tests in package
// daemon_test.
//
// Bead ref: hk-m57va.
var ExportedNewCodexHarness = NewCodexHarness

// ExportedCodexEventKind mirrors the internal codexEventKind enum for tests.
type ExportedCodexEventKind = codexEventKind

// Exported codexEventKind constants for table-driven parser tests.
const (
	ExportedCodexEventKindOther         = CodexEventKindOther
	ExportedCodexEventKindThreadStarted = CodexEventKindThreadStarted
	ExportedCodexEventKindTurnStarted   = CodexEventKindTurnStarted
	ExportedCodexEventKindTurnCompleted = CodexEventKindTurnCompleted
	ExportedCodexEventKindTurnFailed    = CodexEventKindTurnFailed
)

// ExportedCodexEvent is the exported projection of the parsed codexEvent for
// test assertions.
type ExportedCodexEvent struct {
	Kind         ExportedCodexEventKind
	RawType      string
	ThreadID     string
	TurnID       string
	ErrorMessage string
}

// ExportedParseCodexJSONLEvent exposes parseCodexJSONLEvent for tests, returning
// the exported event projection.
//
// Bead ref: hk-m57va.
func ExportedParseCodexJSONLEvent(line []byte) (ExportedCodexEvent, error) {
	ev, err := parseCodexJSONLEvent(line)
	if err != nil {
		return ExportedCodexEvent{}, err
	}
	return ExportedCodexEvent{
		Kind:         ev.Kind,
		RawType:      ev.RawType,
		ThreadID:     ev.ThreadID,
		TurnID:       ev.TurnID,
		ErrorMessage: ev.ErrorMessage,
	}, nil
}

// ExportedCodexRunArtifacts is the exported projection of codexRunArtifacts for
// thread-id-capture tests.
type ExportedCodexRunArtifacts struct {
	CapturedThreadID   string
	TurnCompleted      bool
	TurnFailed         bool
	TurnFailureMessage string
}

// ExportedCaptureCodexThreadStream folds an ordered slice of raw JSONL lines
// through parseCodexJSONLEvent + captureCodexThreadID and returns the resulting
// run artifacts. Malformed lines are surfaced as an error (the production stream
// reader skips them, but tests assert exact behaviour). This exercises the
// thread-id capture-into-run-state requirement of T8.
//
// Bead ref: hk-m57va.
func ExportedCaptureCodexThreadStream(lines [][]byte) (ExportedCodexRunArtifacts, error) {
	var arts codexRunArtifacts
	for _, line := range lines {
		ev, err := parseCodexJSONLEvent(line)
		if err != nil {
			return ExportedCodexRunArtifacts{}, err
		}
		captureCodexThreadID(&arts, ev)
	}
	return ExportedCodexRunArtifacts{
		CapturedThreadID:   arts.capturedThreadID,
		TurnCompleted:      arts.turnCompleted,
		TurnFailed:         arts.turnFailed,
		TurnFailureMessage: arts.turnFailureMessage,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// codex Refs:<bead> trailer guarantee test seams (hk-bpxci C2/T9)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedCodexRefsOutcome mirrors the internal codexRefsOutcome enum for tests.
type ExportedCodexRefsOutcome = codexRefsOutcome

// Exported codexRefsOutcome constants for ensureCodexRefsTrailer assertions.
const (
	ExportedCodexRefsAlreadyPresent = codexRefsAlreadyPresent
	ExportedCodexRefsAmended        = codexRefsAmended
	ExportedCodexRefsCommitted      = codexRefsCommitted
	ExportedCodexRefsNoChange       = codexRefsNoChange
)

// ExportedWorktreeHEADHasRefsTrailer exposes worktreeHEADHasRefsTrailer (VERIFY).
//
// Bead ref: hk-bpxci.
func ExportedWorktreeHEADHasRefsTrailer(ctx context.Context, wtPath string, beadID core.BeadID) (bool, error) {
	return worktreeHEADHasRefsTrailer(ctx, nil, wtPath, beadID)
}

// ExportedEnsureCodexRefsTrailer exposes ensureCodexRefsTrailer (VERIFY +
// deterministic commit-after-exit FALLBACK).
//
// Bead ref: hk-bpxci.
func ExportedEnsureCodexRefsTrailer(ctx context.Context, wtPath, parentSHA string, beadID core.BeadID) (ExportedCodexRefsOutcome, error) {
	return ensureCodexRefsTrailer(ctx, nil, wtPath, parentSHA, beadID)
}

// ExportedCodexSeedPromptInstruction returns the codex seed prompt for beadID so
// tests can assert the INSTRUCT part (the prompt tells codex to commit with the
// Refs: trailer).
//
// Bead ref: hk-bpxci.
func ExportedCodexSeedPromptInstruction(beadID core.BeadID) string {
	return fmt.Sprintf(codexSeedPromptTemplate, string(beadID))
}

// ExportedShellQuoteArg exposes shellQuoteArg for unit tests in package daemon_test.
//
// Bead ref: hk-rpr6.
func ExportedShellQuoteArg(s string) string {
	return shellQuoteArg(s)
}

// ─────────────────────────────────────────────────────────────────────────────
// codex thread_id interceptor test seams (hk-mzgh)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewCodexThreadIDInterceptor exposes newCodexThreadIDInterceptor for
// tests in package daemon_test.
//
// Bead ref: hk-mzgh.
func ExportedNewCodexThreadIDInterceptor(inner io.Reader, cb func(string)) io.Reader {
	return newCodexThreadIDInterceptor(inner, cb)
}

// ─────────────────────────────────────────────────────────────────────────────
// Cognition signal test seams (hk-jay1 P2-c: SS-012)
// ─────────────────────────────────────────────────────────────────────────────

// NewLiveStateBuilderForTest constructs a LiveStateBuilder with the given
// projectDir, projectHash and kconfig so tests in package daemon_test can
// exercise buildCognition without a full daemon. No runs/queues/drain needed.
//
// Bead ref: hk-jay1.
func NewLiveStateBuilderForTest(projectDir string, projectHash core.ProjectHash, kconfig KeeperConfig) *LiveStateBuilder {
	return &LiveStateBuilder{
		projectDir:  projectDir,
		projectHash: projectHash,
		kconfig:     kconfig,
	}
}

// BuildCognitionForTest calls buildCognition on lb and returns the result.
// Exposed so tests can verify TooBigSignal and ContextStaticSignal population
// (SS-012) without requiring a live tmux session.
//
// Bead ref: hk-jay1.
func (lb *LiveStateBuilder) BuildCognitionForTest(agent, liveSID, declaredSID string, now time.Time) *SessionCognition {
	return lb.buildCognition(agent, liveSID, declaredSID, now)
}

// ExportedReadGateVerdictVia exposes readGateVerdictVia for tests in package
// daemon_test. It allows the contract test to verify that the gate-verdict.json
// read routes through runner on remote runs (hk-hd2w6).
//
// Bead ref: hk-hd2w6.
func ExportedReadGateVerdictVia(ctx context.Context, runner tmuxPkg.CommandRunner, verdictPath string) (core.GateAction, error) {
	return readGateVerdictVia(ctx, runner, verdictPath)
}

// ExportedGateVerdictExistsVia exposes gateVerdictExistsVia for tests in
// package daemon_test. Allows the contract test to assert that the os.Stat
// check on gate-verdict.json routes through runner on remote runs (hk-hd2w6).
//
// Bead ref: hk-hd2w6.
func ExportedGateVerdictExistsVia(ctx context.Context, runner tmuxPkg.CommandRunner, path string) bool {
	return gateVerdictExistsVia(ctx, runner, path)
}

// ─────────────────────────────────────────────────────────────────────────────
// buildPiLaunchSpec test seams (hk-1c16h PI-015/020/021)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedPiRunCtx is the exported shape of piRunCtx for tests.
// Fields mirror piRunCtx verbatim with exported names.
//
// Bead ref: hk-1c16h.
type ExportedPiRunCtx struct {
	PiBinary       string
	WorkspacePath  string
	BeadID         string
	Provider       string
	Model          string
	APIKeyEnv      string
	PriorSessionID *string
	BaseEnv        []string
	// SkipBillingGuard disables the pre-flight billing guard hook (PI-040).
	// Set to true in argv/env-shape tests that do not require a real key.
	SkipBillingGuard bool
}

// ExportedBuildPiLaunchSpec exposes buildPiLaunchSpec for tests in package
// daemon_test. The ExportedPiRunCtx is translated to the internal piRunCtx
// before calling.
//
// Bead ref: hk-1c16h.
func ExportedBuildPiLaunchSpec(rc ExportedPiRunCtx) (handler.LaunchSpec, error) {
	return buildPiLaunchSpec(piRunCtx{
		piBinary:         rc.PiBinary,
		workspacePath:    rc.WorkspacePath,
		beadID:           rc.BeadID,
		provider:         rc.Provider,
		model:            rc.Model,
		apiKeyEnv:        rc.APIKeyEnv,
		priorSessionID:   rc.PriorSessionID,
		baseEnv:          rc.BaseEnv,
		skipBillingGuard: rc.SkipBillingGuard,
	})
}

// ExportedBuildPiEnv exposes buildPiEnv for tests in package daemon_test.
// Allows direct verification of the allowlist-strip semantics (PI-021).
//
// Bead ref: hk-1c16h.
func ExportedBuildPiEnv(baseEnv []string, apiKeyEnv string) []string {
	return buildPiEnv(baseEnv, apiKeyEnv)
}

// ExportedResolvePiAPIKeyValue exposes resolvePiAPIKeyValue for tests in
// package daemon_test. Allows tests to verify the shared key-resolution helper
// reads the correct env var (PI-021).
//
// Bead ref: hk-1c16h.
func ExportedResolvePiAPIKeyValue(apiKeyEnv string) string {
	return resolvePiAPIKeyValue(apiKeyEnv)
}
