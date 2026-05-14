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
	"os"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
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
	// When nil, ExportedWorkLoopDeps installs a synthHookStore that immediately
	// synthesises a WORK_COMPLETE outcome_emitted on every RegisterHookSession
	// call. This prevents the 3-second stopHookGrace window from firing in
	// shell-fixture tests whose handlers exit quickly without a real Stop-hook
	// relay.
	//
	// Supply an explicit *hookSessionStore (via ExportedNewHookSessionStore) for
	// tests that need to observe or control hook-relay routing directly.
	//
	// Bead ref: hk-gql20.21, hk-kqdpf.1.
	HookStore hookStoreIface

	// AdapterRegistry2 is the sealed adapter registry forwarded to beadRunOne
	// for waitAgentReady (hk-gql20.14). Named AdapterRegistry2 to avoid
	// collision with the existing AdapterRegistry field (used for
	// handler.NewHandler). When nil, ExportedWorkLoopDeps stores nil in
	// workLoopDeps.adapterRegistry — waitAgentReady is then skipped.
	//
	// Bead ref: hk-gql20.14.
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

	// LaunchSpecBuilder, when non-nil, overrides the buildClaudeLaunchSpec
	// function called by beadRunOne. Production tests leave this nil; the
	// ExportedWorkLoopDeps default installs synthLaunchSpecBuilder, which
	// returns a minimal spec with no file I/O, avoiding MaterializeClaudeSettings
	// fsyncs that would otherwise slow down shell-fixture tests.
	//
	// Supply an explicit builder to exercise the real bridge setup path (e.g. to
	// test CHB-001..005 or CHB-024 behaviours).
	//
	// Bead ref: hk-kqdpf.1.
	LaunchSpecBuilder func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)

	// WorktreeFactory, when non-nil, overrides the worktree creation function
	// in beadRunOne. Production tests leave this nil; the ExportedWorkLoopDeps
	// default installs synthWorktreeFactory, which creates a plain temp directory
	// instead of a git worktree, avoiding git worktree contention under parallel
	// load. Tests that need a real git worktree (e.g. workspace integration tests)
	// should supply productionWorktreeFactory explicitly.
	//
	// Bead ref: hk-kqdpf.1.
	WorktreeFactory func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)
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

	// Use the caller-supplied HookStore or fall back to a synthHookStore (hk-kqdpf.1).
	// synthHookStore immediately returns a WORK_COMPLETE outcome_emitted on
	// WaitForOutcome, avoiding the 3-second stopHookGrace window in shell-fixture
	// tests whose handlers exit quickly without a real Stop-hook relay.
	var hookStore hookStoreIface
	if p.HookStore != nil {
		hookStore = p.HookStore
	} else {
		hookStore = newSynthHookStore()
	}

	// Use the caller-supplied LaunchSpecBuilder or fall back to synthLaunchSpecBuilder
	// (hk-kqdpf.1). synthLaunchSpecBuilder skips MaterializeClaudeSettings fsyncs and
	// other bridge file I/O, keeping shell-fixture tests fast.
	lsb := p.LaunchSpecBuilder
	if lsb == nil {
		lsb = synthLaunchSpecBuilder
	}

	// Use the caller-supplied WorktreeFactory or fall back to synthWorktreeFactory
	// (hk-kqdpf.1). synthWorktreeFactory uses os.MkdirTemp instead of git worktree
	// add, avoiding git subprocess overhead and cross-test git contention under
	// parallel load.
	wtf := p.WorktreeFactory
	if wtf == nil {
		wtf = synthWorktreeFactory
	}

	h := handler.NewHandler(p.Bus, handlercontract.NoopWatcherDeadLetter{}, adapterReg)

	return workLoopDeps{
		brAdapter:           p.BrAdapter,
		bus:                 p.Bus,
		h:                   h,
		intentLogDir:        p.IntentLogDir,
		projectDir:          p.ProjectDir,
		handlerBinary:       binary,
		handlerArgs:         p.HandlerArgs,
		handlerEnv:          nil,
		brTimeoutCfg:        brcli.TimeoutConfig{},
		tidGen:              core.NewTransitionIDGenerator(),
		workflowModeDefault: wmd,
		runRegistry:         reg,
		maxConcurrent:       maxConcurrent,
		hookStore:           hookStore,
		launchSpecBuilder:   lsb,
		worktreeFactory:     wtf,
		adapterRegistry:     p.AdapterRegistry2,
		substrate:           p.Substrate,
		agentReadyTimeout:   p.AgentReadyTimeout,
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
	r := runReviewLoop(ctx, deps, runID, beadID, "", "", wtPath, parentSHA)
	return ReviewLoopResultExported{
		Success:          r.success,
		CompletionReason: string(r.completionReason),
		Summary:          r.summary,
		NeedsAttention:   r.needsAttention,
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

// ─────────────────────────────────────────────────────────────────────────────
// synthWorktreeFactory — test-only stub that bypasses git worktree creation
// ─────────────────────────────────────────────────────────────────────────────

// synthWorktreeFactory is the default worktreeFactory for ExportedWorkLoopDeps
// (hk-kqdpf.1). It creates a plain temporary directory instead of a real git
// worktree, avoiding the git subprocess overhead and cross-test git contention
// that occurs under parallel test load on macOS.
//
// The cleanup removes the temp dir. Tests that need a real git worktree should
// supply WorktreeFactory: productionWorktreeFactory (exported via
// ExportedProductionWorktreeFactory) in WorkLoopDepsParams.
func synthWorktreeFactory(_ context.Context, _, runID, _ string) (string, func(), error) {
	wtPath, err := os.MkdirTemp("", "synth-wt-"+runID+"-")
	if err != nil {
		return "", nil, fmt.Errorf("synthWorktreeFactory: MkdirTemp: %w", err)
	}
	return wtPath, func() { _ = os.RemoveAll(wtPath) }, nil
}

// ExportedProductionWorktreeFactory exposes productionWorktreeFactory for tests
// that need a real git worktree (e.g. integration tests exercising workspace ops).
//
// Bead ref: hk-kqdpf.1.
var ExportedProductionWorktreeFactory = productionWorktreeFactory

// ─────────────────────────────────────────────────────────────────────────────
// synthLaunchSpecBuilder — test-only stub that bypasses bridge file I/O
// ─────────────────────────────────────────────────────────────────────────────

// synthLaunchSpecBuilder is the default launchSpecBuilder for ExportedWorkLoopDeps
// (hk-kqdpf.1). It returns a minimal handler.LaunchSpec — only Binary is set —
// with no file I/O (no MaterializeClaudeSettings fsyncs, no MintClaudeSessionID,
// no CheckSettingsLocalJSON). The claudeRunArtifacts carries a synthetic session
// ID so that RegisterHookSession / CloseHookSession calls (both no-ops in
// synthHookStore) have a non-empty key.
//
// The caller (beadRunOne) prepends any HandlerArgs to spec.Args after this
// function returns, so shell-fixture tests continue to work with only Binary set.
func synthLaunchSpecBuilder(_ context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	artifacts := claudeRunArtifacts{
		claudeSessionID: "synth-" + rc.runID.String(),
	}
	spec := handler.LaunchSpec{
		Binary: rc.handlerBinary,
	}
	return spec, artifacts, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// synthHookStore — test-only stub that synthesises WORK_COMPLETE outcomes
// ─────────────────────────────────────────────────────────────────────────────

// synthHookStore is a hookStoreIface stub used as the default HookStore in
// ExportedWorkLoopDeps (hk-kqdpf.1).
//
// It is designed for shell-fixture tests where no real Stop-hook relay connects.
// Rather than blocking for the 3-second stopHookGrace window in
// waitWithSocketGrace, WaitForOutcome returns (nil, nil) immediately so that
// branch 2 (exit=0 → CloseBead) or the default branch (exit≠0 → ReopenBead)
// handles the outcome based purely on the subprocess exit code.
//
// Tests that need to observe or control real hook-relay routing should supply
// an explicit *hookSessionStore via WorkLoopDepsParams.HookStore.
type synthHookStore struct{}

// newSynthHookStore returns a synthHookStore.
func newSynthHookStore() *synthHookStore { return &synthHookStore{} }

// RegisterHookSession is a no-op: synthHookStore never blocks on outcome delivery.
func (s *synthHookStore) RegisterHookSession(_, _ string) {}

// CloseHookSession is a no-op.
func (s *synthHookStore) CloseHookSession(_, _ string) {}

// LatestOutcome always returns nil — no outcome is pre-seeded.
func (s *synthHookStore) LatestOutcome(_, _ string) *json.RawMessage { return nil }

// WaitForOutcome returns (nil, nil) immediately, skipping the stopHookGrace window.
func (s *synthHookStore) WaitForOutcome(_ context.Context, _, _ string) (json.RawMessage, error) {
	return nil, nil
}

// SetAgentReadyCallback is a no-op: synthHookStore does not simulate relay events.
func (s *synthHookStore) SetAgentReadyCallback(_, _ string, _ func()) {}

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
	ExitCode int
	WaitErr  error
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
	return outcome, ExitInfoExported{ExitCode: ei.exitCode, WaitErr: ei.waitErr}
}

// ─────────────────────────────────────────────────────────────────────────────
// paste-inject test seams (hk-zrj83)
// ─────────────────────────────────────────────────────────────────────────────

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

// ExportedPasteInjectOnLaunch exposes pasteInjectOnLaunch for tests in package
// daemon_test.
//
// Bead ref: hk-zrj83.
func ExportedPasteInjectOnLaunch(
	ctx context.Context,
	substrate handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
) {
	pasteInjectOnLaunch(ctx, substrate, claudeSessID, phase, iterCount, wtPath)
}

// ExportedBufferName exposes the bufferName helper for tests in package
// daemon_test.
//
// Bead ref: hk-zrj83.
func ExportedBufferName(sessionID, purpose string) string {
	return bufferName(sessionID, purpose)
}
