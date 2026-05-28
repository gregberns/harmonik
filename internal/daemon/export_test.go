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
	"io"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
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

	// ProjectCfg is the decoded .harmonik/config.yaml for EM-012b tier-2 resolution.
	// The zero value is safe: LookupAgent returns ("","") for all agent types.
	//
	// Bead ref: hk-bfvk7.
	ProjectCfg ProjectConfig

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

	h := handler.NewHandler(p.Bus, handlercontract.NoopWatcherDeadLetter{}, adapterReg)

	// Derive the submit-wake channel from the QueueStore when one is provided
	// (hk-24xn1). Mirrors the daemon.Start wiring so queue-aware tests observe
	// the same wake-on-submit behaviour as production.
	var submitWakeC <-chan struct{}
	if p.QueueStore != nil {
		submitWakeC = p.QueueStore.WakeCh()
	}

	return workLoopDeps{
		brAdapter:              p.BrAdapter,
		bus:                    p.Bus,
		h:                      h,
		intentLogDir:           p.IntentLogDir,
		projectDir:             p.ProjectDir,
		handlerBinary:          binary,
		handlerArgs:            p.HandlerArgs,
		handlerEnv:             nil,
		brTimeoutCfg:           brcli.TimeoutConfig{},
		tidGen:                 core.NewTransitionIDGenerator(),
		workflowModeDefault:    wmd,
		runRegistry:            reg,
		maxConcurrent:          maxConcurrent,
		hookStore:              hookStore,
		launchSpecBuilder:      lsb,
		worktreeFactory:        wtf,
		adapterRegistry:        p.AdapterRegistry2,
		substrate:              p.Substrate,
		agentReadyTimeout:      p.AgentReadyTimeout,
		projectCfg:             p.ProjectCfg,
		queueStore:             p.QueueStore,
		submitWakeC:            submitWakeC,
		cancelOnQueueDrain:     p.CancelOnQueueDrain,
		cancelOnQueueExit:      p.CancelOnQueueExit,
		stopDispatchCtx:        p.StopDispatchCtx,
		handlerPauseController: p.HandlerPauseController,
		staleBlockerCloser:     p.StaleBlockerCloser, // hk-rnsjs
	}
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
	r := runReviewLoop(ctx, deps, runID, beadID, "", "", wtPath, parentSHA, "", "", "", "")
	return ReviewLoopResultExported{
		Success:          r.success,
		CompletionReason: string(r.completionReason),
		Summary:          r.summary,
		NeedsAttention:   r.needsAttention,
	}
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
	r := driveDotWorkflow(ctx, deps, runID, beadID, "", "", wtPath, parentSHA, graph, "", "", "", "")
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

// ExportedForceTeardownSession exposes forceTeardownSession for the hk-68pvl
// worktree-teardown-ordering regression test.
func ExportedForceTeardownSession(sess handler.Session) {
	forceTeardownSession(sess)
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

// ExportedPasteInjectQuitOnReviewFile exposes pasteInjectQuitOnReviewFile for
// tests in package daemon_test.
//
// Bead: hk-jimbc.
func ExportedPasteInjectQuitOnReviewFile(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	wtPath string,
	briefDelivered <-chan struct{},
) {
	pasteInjectQuitOnReviewFile(ctx, qs, killer, wtPath, briefDelivered)
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
// Bead ref: hk-umxx4.
func ExportedResolveBranching(ctx context.Context, beadBody, projectRoot string) (BranchingConfig, error) {
	return resolveBranching(ctx, beadBody, projectRoot)
}

// ExportedResolveParentCommit exposes resolveParentCommit for tests in package
// daemon_test. See branching.go for semantics.
//
// Bead ref: hk-oe6zt.
func ExportedResolveParentCommit(ctx context.Context, repoRoot, beadID, beadBody string) (string, error) {
	return resolveParentCommit(ctx, repoRoot, beadID, beadBody)
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

// ExportedPasteInjectOnLaunch exposes pasteInjectOnLaunch for tests in package
// daemon_test.  Returns the briefDelivered channel (hk-930o3).
//
// Bead ref: hk-zrj83, hk-930o3.
func ExportedPasteInjectOnLaunch(
	ctx context.Context,
	substrate handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
) <-chan struct{} {
	return pasteInjectOnLaunch(ctx, substrate, claudeSessID, phase, iterCount, wtPath)
}

// ExportedBufferName exposes the bufferName helper for tests in package
// daemon_test.
//
// Bead ref: hk-zrj83.
func ExportedBufferName(sessionID, purpose string) string {
	return bufferName(sessionID, purpose)
}

// ExportedNewPerRunSubstrate wraps newPerRunSubstrate for tests in package
// daemon_test that need per-run pane isolation without importing the unexported
// type directly.
//
// Returns nil when sub is nil or is not a *tmuxSubstrate (matching
// newPerRunSubstrate semantics). Tests that call WriteLastPane on the returned
// value must call SpawnWindow first to capture the pane target.
//
// Bead ref: hk-jfh59.
func ExportedNewPerRunSubstrate(sub handler.Substrate) handler.Substrate {
	prs := newPerRunSubstrate(sub)
	if prs == nil {
		return nil
	}
	return prs
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

// ExportedLoadProjectConfig exposes LoadProjectConfig for tests in package daemon_test.
//
// Bead ref: hk-bfvk7.
func ExportedLoadProjectConfig(repoRoot string) (ProjectConfig, error) {
	return LoadProjectConfig(repoRoot)
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
// Bead ref: hk-45ude.
func ExportedEvaluateGroupAdvanceWithOutcome(ctx context.Context, deps workLoopDeps, queueID string, groupIndex int, itemIdx int, success bool) {
	evaluateGroupAdvanceWithOutcome(ctx, deps, queueID, groupIndex, itemIdx, success)
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
	pasteInjectQuitOnCommit(ctx, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh)
}

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

// quitSenderExported is the exported alias for quitSender so the exported
// wrapper can accept it.
type quitSenderExported = quitSender

// ExportedBeadAlreadySubsumedInMain exposes beadAlreadySubsumedInMain for tests.
//
// Bead: hk-trjef.
func ExportedBeadAlreadySubsumedInMain(ctx context.Context, projectDir string, beadID core.BeadID) bool {
	return beadAlreadySubsumedInMain(ctx, projectDir, beadID)
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
