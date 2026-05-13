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

	// HookStore is the hook-session registry shared between RunSocketListener
	// and the work loop completion path (hk-gql20.21). When nil,
	// ExportedWorkLoopDeps creates a fresh store — tests that do not exercise
	// the hook-relay path may omit this field.
	//
	// Bead ref: hk-gql20.21.
	HookStore *hookSessionStore
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

	// Use the caller-supplied HookStore or create a fresh one (hk-gql20.21).
	hookStore := p.HookStore
	if hookStore == nil {
		hookStore = newHookSessionStore()
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
	r := runReviewLoop(ctx, deps, runID, beadID, wtPath, parentSHA)
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
func ExportedHookStoreOf(deps workLoopDeps) *hookSessionStore {
	return deps.hookStore
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
// Bead ref: hk-gql20.13.
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
	BaseEnv           []string
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
		baseEnv:           rc.BaseEnv,
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

// ─────────────────────────────────────────────────────────────────────────────
// buildClaudeLaunchSpec test seams (hk-gql20.13)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedClaudeRunCtx is the exported test-seam shape for claudeRunCtx with
// PascalCase fields so package daemon_test can populate one directly.
//
// Bead ref: hk-gql20.13.
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
	BaseEnv           []string
}

// ExportedClaudeRunArtifacts is the exported test-seam shape for claudeRunArtifacts.
type ExportedClaudeRunArtifacts struct {
	ClaudeSessionID  string
	SessionLogPath   string
	HandlerSessionID string
	PreExecMsgs      []json.RawMessage
}

// ExportedBuildClaudeLaunchSpec exposes buildClaudeLaunchSpec for tests in
// package daemon_test. Maps the exported PascalCase shape onto the internal
// camelCase claudeRunCtx and returns the exported artifacts shape.
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
		baseEnv:           rc.BaseEnv,
	}
	spec, arts, err := buildClaudeLaunchSpec(ctx, internal)
	exp := ExportedClaudeRunArtifacts{
		ClaudeSessionID:  arts.claudeSessionID,
		SessionLogPath:   arts.sessionLogPath,
		HandlerSessionID: arts.handlerSessionID,
		PreExecMsgs:      arts.preExecMsgs,
	}
	return spec, exp, err
}
