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
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ExportedWorkLoopDefaultHarness returns the defaultHarness field from deps so
// tests can assert Config.DefaultHarness is correctly wired into the dispatch
// path (hk-ytzj2).
func ExportedWorkLoopDefaultHarness(deps workLoopDeps) core.AgentType {
	return deps.defaultHarness
}

// ExportedLoadStandardGraph parses the embedded standard-bead.dot so tests in
// package daemon_test can inspect node attrs (e.g. review.Harness for hk-ytzj2).
func ExportedLoadStandardGraph(params map[string]string) (*dot.Graph, error) {
	return loadStandardGraph(params)
}

// ExportedMaintState is an opaque test handle over the runWorkLoop-local
// loopMaintenanceState (RSM-011: the periodic-maintenance value fields were
// lifted off workLoopDeps). Tests create one via ExportedNewMaintState and
// thread it through ExportedRunPeriodicDiskCheck so per-run state (diskLow,
// last-probe timestamps) persists across calls, as it did on deps before.
type ExportedMaintState struct{ m loopMaintenanceState }

// ExportedNewMaintState returns a fresh maintenance-state handle.
func ExportedNewMaintState() *ExportedMaintState { return &ExportedMaintState{} }

// ExportedRunPeriodicDiskCheck calls runPeriodicDiskCheck with the given deps
// and maintenance-state handle. Used by diskcheck_hksxlb_test.go to drive the
// reaper directly without running the full work loop (hk-guez).
func ExportedRunPeriodicDiskCheck(ctx context.Context, deps *workLoopDeps, ms *ExportedMaintState) {
	runPeriodicDiskCheck(ctx, deps, &ms.m)
}

// ExportedNewRunRegistry creates a fresh RunRegistry for tests.
//
// Bead ref: hk-guez.
func ExportedNewRunRegistry() *RunRegistry {
	return NewRunRegistry()
}

// ExportedDiskCheckDiskLow reads the diskLow field from the maintenance-state
// handle. Used by diskcheck_hksxlb_test.go to assert post-call state (hk-guez).
func ExportedDiskCheckDiskLow(ms *ExportedMaintState) bool {
	return ms.m.diskLow
}

// ExportedDiskCheckSetCheckInterval overrides the disk-probe interval on deps
// so tests fire immediately. A zero override restores the production default
// (diskCheckInterval).
//
// Bead ref: hk-guez.
func ExportedDiskCheckSetCheckInterval(deps *workLoopDeps, d time.Duration) {
	deps.diskCheckIntervalOverride = d
}

// ExportedReclaimStaleWorktrees calls reclaimStaleWorktrees with the given deps
// and returns the count of stale worktrees removed. Used by
// diskcheck_hksxlb_test.go to drive the reclaim step directly (hk-5uezz).
func ExportedReclaimStaleWorktrees(ctx context.Context, deps *workLoopDeps) int {
	return reclaimStaleWorktrees(ctx, deps)
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

// ExportedStoreLocalInFlight preloads the split-gate local-in-flight counter on
// deps so a test can simulate local saturation (localInFlight >= gateMax) before
// running the work loop. localInFlight is a *atomic.Int32 shared through the
// by-value deps copy, so a store here is visible to the loop started via
// ExportedRunWorkLoop(deps).
//
// Bead ref: hk-l5saf.
func ExportedStoreLocalInFlight(deps workLoopDeps, n int32) {
	deps.localInFlight.Store(n)
}

// ExportedSetAgentReadyKillReapTimeout overrides the package-level
// runlaunch.KillReapTimeout for tests. Returns a restore function; pass it to
// t.Cleanup. NOT safe for use with t.Parallel() — modifies a package global.
//
// Bead ref: hk-4hso5.
func ExportedSetAgentReadyKillReapTimeout(d time.Duration) func() {
	orig := runlaunch.KillReapTimeout
	runlaunch.KillReapTimeout = d
	return func() { runlaunch.KillReapTimeout = orig }
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

// ExportedResolveHarnessAgentTypeQuiet exposes resolveHarnessAgentTypeQuiet for
// tests in package daemon_test. See harnessresolve.go (hk-pkugu).
func ExportedResolveHarnessAgentTypeQuiet(
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
) core.AgentType {
	return resolveHarnessAgentTypeQuiet(bead, queueDefault, nodeDefault, globalDefault)
}

// ExportedResolveGateAgentType exposes resolveGateAgentType for tests in package
// daemon_test. See sandboxgate.go for semantics (hk-r4p0l).
func ExportedResolveGateAgentType(implHarness handlercontract.Harness, fromArtifacts core.AgentType) core.AgentType {
	return resolveGateAgentType(implHarness, fromArtifacts)
}

// ExportedSandboxSpawnForRun exposes sandboxSpawnForRun for tests in package
// daemon_test, returning whether a SrtSpawnConfig would be attached (non-nil)
// for the given config + resolved agent type. See sandboxgate.go (hk-r4p0l).
func ExportedSandboxSpawnForRun(cfg SandboxConfig, agentType core.AgentType, in SandboxProfileInput) *SrtSpawnConfig {
	return sandboxSpawnForRun(cfg, agentType, in)
}

// ExportedSandboxWrapExecArgv exposes sandboxWrapExecArgv for tests in package
// daemon_test — the EXEC-path srt argv-wrap applied to a SessionIDCaptured
// (pi) run's LaunchSpec (spec.Substrate==nil). Returns (binary, args) unchanged
// when spawn is nil (strict no-op). See sandboxgate.go (hk-r4p0l part 2).
func ExportedSandboxWrapExecArgv(spawn *SrtSpawnConfig, binary string, args []string) (wrappedBinary string, wrappedArgs []string, err error) {
	return sandboxWrapExecArgv(spawn, binary, args)
}

// ExportedVerifySandboxEngaged exposes verifySandboxEngaged for tests in
// package daemon_test — the production-path srt sandbox-engagement proof
// (hk-5wdon, follow-up to hk-tch4t). See sandboxgate.go for semantics.
func ExportedVerifySandboxEngaged(ctx context.Context, spawn *SrtSpawnConfig, canaryPath string, logf func(format string, args ...any)) error {
	return verifySandboxEngaged(ctx, spawn, canaryPath, logf)
}

// ExportedSrtEngagementCanaryPath exposes srtEngagementCanaryPath for tests in
// package daemon_test. See sandboxgate.go (hk-5wdon).
func ExportedSrtEngagementCanaryPath(projectDir, runID string) string {
	return srtEngagementCanaryPath(projectDir, runID)
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

// runBeadOneTest mirrors the runWorkLoop goroutine caller for white-box tests:
// it builds the per-run bundles (including the RT18.11 launch-builder resolution
// that used to live inside beadRunOne) and invokes beadRunOne, so a test that
// constructs a workLoopDeps + RunEnv drives a single bead run exactly as
// production does.
func runBeadOneTest(ctx context.Context, deps workLoopDeps, env RunEnv, extraContext string, preSelected *workers.Worker, localSlotHeld bool) bool { //nolint:unparam // mirrors beadRunOne's parameter list for parity; current callers all pass "" for extraContext
	rp, handles := deps.buildRunBundles(env)
	return beadRunOne(ctx, env, rp, handles, extraContext, preSelected, localSlotHeld)
}

// runBundlesFromDeps assembles the env/ports/handles bundles a test shim passes
// into a run-path function after the RT18 signature drop, mirroring how the
// production runWorkLoop caller builds them — it routes through buildRunBundles,
// so the launch builder is resolved (routed / claude fallback) and threaded onto
// rp.LaunchBuilder exactly as production does, and a fixture-injected
// launchSpecBuilder still reaches the review/DOT sub-drivers.
func runBundlesFromDeps(deps workLoopDeps, runID core.RunID) (RunEnv, RunPorts, SharedHandles) {
	env := deps.runEnv(runID, core.BeadRecord{}, "", nil, nil, 0, "", "", nil, false, "")
	rp, handles := deps.buildRunBundles(env)
	return env, rp, handles
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

// ExportedMinimalLaunchSpecBuilder returns a launchSpecBuilder stub that
// produces a handler.LaunchSpec with a no-op binary (/bin/true) and
// zero-value shared.LaunchArtifacts. Used in tests that inject a spy substrate
// and need handler.Launch to reach Substrate.SpawnWindow without the full
// Claude build infrastructure (e.g. hk-wnqos single-mode terminal-spawn test).
func ExportedMinimalLaunchSpecBuilder() func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, _ shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		return handler.LaunchSpec{Binary: "/bin/true"}, shared.LaunchArtifacts{}, nil
	}
}

// ExportedCaptureExtraContextBuilder returns a launchSpecBuilder stub that
// sends the extraContext from the FIRST call into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert
// that node role= is injected into the agent brief (hk-m5lmo).
func ExportedCaptureExtraContextBuilder(ch chan<- string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- rc.ExtraContext:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedCaptureNodePromptBuilder returns a launchSpecBuilder stub that
// sends the nodePrompt from the FIRST call into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert
// that node prompt= is threaded into shared.LaunchCtx (hk-sdnzj).
func ExportedCaptureNodePromptBuilder(ch chan<- string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- rc.NodePrompt:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedCaptureRunnerBuilder returns a launchSpecBuilder stub that sends the
// CommandRunner from the FIRST call's shared.LaunchCtx into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert that
// the review-loop and DOT launch paths thread the run's CommandRunner into the
// shared.LaunchCtx so the worktree-trust / settings / agent-task writes land on the
// WORKER for a REMOTE run (hk-3sus).
func ExportedCaptureRunnerBuilder(ch chan<- tmuxPkg.CommandRunner) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- rc.Runner:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ModelEffortPair holds the model and effort values captured from a shared.LaunchCtx.
// Used by ExportedCaptureModelEffortBuilder tests (hk-q8nqr).
type ModelEffortPair struct {
	Model  string
	Effort string
}

// ExportedCaptureModelEffortBuilder returns a launchSpecBuilder stub that
// sends the (model, effort) pair from the FIRST call into ch (non-blocking),
// then returns an error to short-circuit the dispatch. Tests use this to
// assert that per-node model= / effort= overrides are threaded into
// shared.LaunchCtx (hk-q8nqr WG-042 §I.5 / EM-012b-NODE).
func ExportedCaptureModelEffortBuilder(ch chan<- ModelEffortPair) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- ModelEffortPair{Model: rc.Model, Effort: rc.Effort}:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
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

// ExportedIsRetryableMergeReason exposes isRetryableMergeReason for unit tests.
//
// Bead ref: hk-f9xzs.
var ExportedIsRetryableMergeReason = runmerge.IsRetryableReason

// ExportedForceTeardownSession exposes runlaunch.ForceTeardownSession for the hk-68pvl
// worktree-teardown-ordering regression test.
func ExportedForceTeardownSession(sess handler.Session) {
	runlaunch.ForceTeardownSession(sess)
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

// ExportedSpawnCapSize exposes the non-terminal spawn-cap ceiling of a
// substrate returned by NewTmuxSubstrate, for the hk-omvan live-resize tests.
// Returns 0 when sub is not a *tmuxSubstrate or has no cap configured.
func ExportedSpawnCapSize(sub handler.Substrate) int {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		return ts.SpawnCapSize()
	}
	return 0
}

// ExportedSetSpawnCap exposes SetSpawnCap on a substrate returned by
// NewTmuxSubstrate, for the hk-omvan live-resize tests. No-op when sub is not
// a *tmuxSubstrate.
func ExportedSetSpawnCap(sub handler.Substrate, n int) {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		ts.SetSpawnCap(n)
	}
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
func ExportedHookDispatch(s *hookSessionStore, env HookRelayEnvelopeExported) (status, reason string) {
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
func ExportedPersistClaudeSessionID(ctx context.Context, wtPath string, runID core.RunID, sessionID string) (commitSHA string, skipped bool, err error) {
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

// ExportedBeadNeverSpawnedTimeout exposes the package-private
// beadNeverSpawnedTimeout helper for unit testing.
//
// Bead ref: hk-8gixi.
func ExportedBeadNeverSpawnedTimeout(labels []string, defaultTimeout time.Duration) time.Duration {
	return beadNeverSpawnedTimeout(labels, defaultTimeout)
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
	_ = w.observe(ctx, evt) //nolint:errcheck // hk-0z5x: this seam is called in statement position by the stalewatch tests, which assert on watcher state rather than the observe error; surfacing it would push unchecked-error findings onto every caller.
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

// ExportedClaudeRunCtx is the launch DTO for tests in package daemon_test.
//
// It used to be a hand-maintained mirror struct plus a field-by-field
// translation in every builder below. P2 unit E1b-prep moved the real type out
// of internal/daemon into internal/harness/shared (where it belongs — it is the
// universal launch DTO, fed to codex and pi as well as claude), so the mirror
// collapsed into a plain alias and the translations became passthroughs.
//
// Bead ref: hk-gql20.13, hk-xo03m.
type ExportedClaudeRunCtx = shared.LaunchCtx

// ExportedClaudeRunArtifacts is the post-launch artifact bundle for tests in
// package daemon_test. Alias of the real type for the same reason as
// ExportedClaudeRunCtx above.
//
// Bead ref: hk-gql20.13.
type ExportedClaudeRunArtifacts = shared.LaunchArtifacts

// ExportedBuildClaudeLaunchSpec exposes claude.BuildLaunchSpec for tests in
// package daemon_test. Since E1b-prep the exported and internal DTOs are the
// same type, so this is a plain passthrough.
//
// RETAINED after P2 E1b: the builder itself moved to internal/harness/claude,
// but nine STAYING daemon test files still call this shim to assert
// daemon-composition claims (routed-vs-direct spec parity, harness pinning,
// review-loop resume, CHB-024 settings shadowing, the DOT prompt path). Those
// are claims about the daemon's wiring, not about the claude unit, so they do
// not move with it.
//
// Bead ref: hk-gql20.13.
func ExportedBuildClaudeLaunchSpec(ctx context.Context, rc ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	return claude.BuildLaunchSpec(ctx, rc)
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

// ExportedErrAgentReadyTimeout exposes runlaunch.ErrAgentReadyTimeout for tests.
//
// Bead ref: hk-gql20.18.
var ExportedErrAgentReadyTimeout = runlaunch.ErrAgentReadyTimeout

// ExportedErrPostAgentReadyHang exposes ErrPostAgentReadyHang for tests (hk-a2okh).
var ExportedErrPostAgentReadyHang = ErrPostAgentReadyHang

// ExportedDefaultPostAgentReadyHangTimeout exposes defaultPostAgentReadyHangTimeout
// for tests (hk-a2okh).
var ExportedDefaultPostAgentReadyHangTimeout = &defaultPostAgentReadyHangTimeout

// ExportedWaitPostAgentReadyProgress exposes waitPostAgentReadyProgress for
// unit tests (hk-a2okh) on the real system clock — the pre-RT19c shape.
func ExportedWaitPostAgentReadyProgress(ctx context.Context, eventCh <-chan core.EventEnvelope, timeout time.Duration) error {
	return waitPostAgentReadyProgress(ctx, substrate.SystemClock{}, eventCh, timeout)
}

// ExportedDefaultAgentReadyTimeout exposes runlaunch.DefaultAgentReadyTimeout
// (HC-056, internal/runlaunch/deadlines.go) so the WS3-Claude-C timing
// property/fuzz harness can PIN the
// real threshold constant — if the production default drifts, the pin assertion
// fails, surfacing that the harness's scaled band no longer models the real one.
var ExportedDefaultAgentReadyTimeout = runlaunch.DefaultAgentReadyTimeout

// ExportedEmitAgentReadyTimeout exposes runlaunch.EmitAgentReadyTimeout (hk-5cox8) so the
// WS3-Claude-C harness drives the REAL anomaly emitter (inv-3) rather than a
// fabricated stand-in.
var ExportedEmitAgentReadyTimeout = runlaunch.EmitAgentReadyTimeout

// ExportedEmitPostAgentReadyHang exposes emitPostAgentReadyHang (hk-a2okh) so the
// WS3-Claude-C harness drives the REAL post-agent_ready-hang anomaly emitter.
var ExportedEmitPostAgentReadyHang = emitPostAgentReadyHang

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
	outcome, ei := waitWithSocketGrace(ctx, substrate.SystemClock{}, store, watcher, sess, runID, claudeSessID)
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

// ExportedSplashDismissDelay / ExportedSetSplashDismissDelay read and write the
// package-level splashDismissDelay (now an atomic.Int64 of nanoseconds).  Tests
// set it to a short duration so the splash-dismiss wait inside the paste-inject
// helpers does not slow unit tests; atomic access keeps a parallel test's write
// from racing a production read.
//
// Bead: hk-7rgqs.
func ExportedSplashDismissDelay() time.Duration { return splashDismissDelayDur() }

func ExportedSetSplashDismissDelay(d time.Duration) {
	splashDismissDelayNs.Store(int64(d))
}

// PaneCapturerExported is an exported alias for the unexported paneCapturer
// interface so tests can supply a stub that drives the seed-paste
// land-verification + re-paste loop (hk-zexsj).
type PaneCapturerExported = paneCapturer

// ExportedPasteVerifyAttempts, ExportedPasteVerifyBackoff and
// ExportedPasteVerifyScrollback are pointers to the package-level seed-paste
// verify knobs so tests can bound the attempts and shrink the backoff without
// waiting real wall time (hk-zexsj).
var (
	ExportedPasteVerifyAttempts   = &pasteVerifyAttempts
	ExportedPasteVerifyScrollback = &pasteVerifyScrollback
)

// ExportedPasteVerifyBackoff / ExportedSetPasteVerifyBackoff read and write the
// package-level pasteVerifyBackoff (now an atomic.Int64 of nanoseconds) so a
// parallel test's shrink does not race a production read (hk-zexsj).
func ExportedPasteVerifyBackoff() time.Duration { return pasteVerifyBackoffDur() }

func ExportedSetPasteVerifyBackoff(d time.Duration) {
	pasteVerifyBackoffNs.Store(int64(d))
}

// ExportedPasteInjectReviewer exposes pasteInjectReviewer for unit tests that
// assert the reviewer kick-off delivery (splash-dismiss → paste → bounded submit
// Enter) directly (hk-7rgqs).
func ExportedPasteInjectReviewer(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	return pasteInjectReviewer(ctx, substrate.SystemClock{}, inj, claudeSessID, wtPath, nil)
}

// ExportedPasteInjectImplementerInitial exposes pasteInjectImplementerInitial for
// unit tests that assert the implementer-initial robust-submit hardening
// (hk-7rgqs).
func ExportedPasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	return pasteInjectImplementerInitial(ctx, substrate.SystemClock{}, inj, claudeSessID, wtPath, nil)
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
	pasteInjectQuitOnReviewFile(ctx, substrate.SystemClock{}, qs, killer, inj, claudeSessID, wtPath, briefDelivered, eventCh, overrideCeiling)
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
//
//nolint:gocritic // hk-sah87: the flat result tuple is the point of this seam — it lets daemon_test assert marker fields without access to the unexported reviewerBudgetSentinel struct; returning a struct would just re-export it.
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
func ExportedNewPerQueueSpendMeter(reg *RunRegistry, store *queuewiring.QueueStore, projectDir string) *PerQueueSpendMeter {
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
func ExportedQueueStoreSetQueue(s *queuewiring.QueueStore, q *queue.Queue) {
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
	subst handler.Substrate,
	claudeSessID string,
	phase handlercontract.ReviewLoopPhase,
	iterCount int,
	wtPath string,
) <-chan struct{} {
	return pasteInjectOnLaunch(ctx, substrate.SystemClock{}, subst, claudeSessID, phase, iterCount, wtPath, nil, core.RunID{})
}

// ExportedBufferName exposes the bufferName helper for tests in package
// daemon_test.
//
// Bead ref: hk-zrj83.
func ExportedBufferName(sessionID, purpose string) string {
	return bufferName(sessionID, purpose)
}

// ExportedInputBufferName exposes the interim tmux/paste InputPort buffer name
// for tests in package daemon_test. Since T8 (codename:agent-input-substrate) the
// daemon-run review-loop delivery routes through handler.InputPort.SubmitInput,
// whose interim tmux driver names its buffer per-run as
// "harmonik-<run-id>-input" (hk-9hvr0: the former hardcoded "harmonik-input"
// failed the bufferNameRe invariant). The name is per-run, so callers pass the
// substrate the SubmitInput will run on and get back the exact name it emits.
func ExportedInputBufferName(sub handler.Substrate) string {
	if p, ok := sub.(*perRunSubstrate); ok {
		return p.inputBufferName()
	}
	return ""
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

// ExportedResolvePiProfile exposes resolvePiProfile for tests in package
// daemon_test (pi-provider-switch C5-design). Mirrors the claim-time call
// shape at workloop.go:3099: the caller MUST pass the resolvedAgentType
// produced by ExportedResolveHarnessAgentTypeQuiet (hk-pkugu discipline) so a
// claude/codex-resolved bead never receives a pi tuple. Returns the zero
// PiProfileConfig (all-empty) for a non-pi agentType, an absent/conflicting
// profile: label, or a resolved profile; a *PiProfileUnknownError for an
// unknown profile: reference (fail-loud — the caller must reopen the bead
// rather than launch, matching workloop.go:3103-3109).
//
// Bead ref: hk-m6uu2.5.
func ExportedResolvePiProfile(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	piCfg PiHarnessConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (PiProfileConfig, error) {
	return resolvePiProfile(ctx, beadLabels, agentType, piCfg, bus, beadID)
}

// HandlerEnvOf returns the handlerEnv field from deps.
// Used by tests to assert HARMONIK_PROJECT_HASH injection (hk-nvrvp).
func HandlerEnvOf(deps workLoopDeps) []string {
	return deps.handlerEnv
}

// ─────────────────────────────────────────────────────────────────────────────
// QueueStore test seams (hk-j808w)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewQueueStore returns a queuewiring.QueueStore for tests in package
// daemon_test. The store itself moved to internal/queuewiring (P2 E3a); this
// shim is kept because 41 daemon_test files call it and none names the type, so
// keeping it is the difference between a bounded diff and a 60-file one.
//
// Bead ref: hk-j808w.
func ExportedNewQueueStore() *queuewiring.QueueStore {
	return queuewiring.NewQueueStore()
}

// ExportedEvaluateGroupAdvanceWithOutcome exposes evaluateGroupAdvanceWithOutcome
// for tests in package daemon_test. Drives EM-015f group-advance evaluation
// directly without running a full work loop cycle.
//
// queueName (NQ-B1) selects the queue slot the completion path resolves; pass
// "" for the main queue (it normalises to "main").
//
// Bead ref: hk-45ude, hk-tigaf.4.
func ExportedEvaluateGroupAdvanceWithOutcome(ctx context.Context, deps workLoopDeps, queueName, queueID string, groupIndex, itemIdx int, success bool) {
	evaluateGroupAdvanceWithOutcome(ctx, deps, queueName, queueID, groupIndex, itemIdx, success)
}

// ExportedQueueStoreOf returns deps.queueStore. Used by tests to observe the
// active queue after work-loop cycles in hk-45ude queue-dispatch tests.
//
// Bead ref: hk-45ude.
func ExportedQueueStoreOf(deps workLoopDeps) *queuewiring.QueueStore {
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
var ExportedResumeSubmitRetries = &resumeSubmitRetries

// ExportedResumeSubmitRetryDelay / ExportedSetResumeSubmitRetryDelay read and
// write the package-level resumeSubmitRetryDelay (now an atomic.Int64 of
// nanoseconds) so a parallel test's shrink does not race a production read
// (hk-ip33d).
func ExportedResumeSubmitRetryDelay() time.Duration { return resumeSubmitRetryDelayDur() }

func ExportedSetResumeSubmitRetryDelay(d time.Duration) {
	resumeSubmitRetryDelayNs.Store(int64(d))
}

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
	pasteInjectQuitOnCommit(ctx, substrate.SystemClock{}, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh, nil, core.RunID{})
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
	pasteInjectQuitOnCommit(ctx, substrate.SystemClock{}, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh, bus, runID)
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

// ExportedBeadAlreadySubsumedInMain exposes the main-history Refs-trailer probe
// for tests. The implementation left internal/daemon for
// shared.MainHistoryHasRefsTrailer (P2 E5 RT19b); this shim keeps the existing
// daemon_test callers compiling unchanged.
//
// Bead: hk-trjef.
func ExportedBeadAlreadySubsumedInMain(ctx context.Context, projectDir string, beadID core.BeadID) bool {
	return shared.MainHistoryHasRefsTrailer(ctx, projectDir, beadID)
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
// queuewiring.QueueOperatorEventConsumerConfig for tests in package daemon_test.
// Kept after the P2 E3a move because operatornfr_pause_inflight_hk95a2r_test.go
// (which stays in daemon) names it.
//
// Bead ref: hk-7urls.
type ExportedQueueOperatorEventConsumerConfig = queuewiring.QueueOperatorEventConsumerConfig

// ExportedNewQueueOperatorEventConsumer exposes
// queuewiring.NewQueueOperatorEventConsumer for tests in package daemon_test.
//
// Bead ref: hk-7urls.
var ExportedNewQueueOperatorEventConsumer = queuewiring.NewQueueOperatorEventConsumer

// The two handler-invoking shims (ExportedQueueOpConsumerHandlePauseStatus /
// ExportedQueueOpConsumerHandleResuming) moved to
// internal/queuewiring/export_test.go with the consumer they drive (P2 E3a):
// their bodies reach unexported methods package daemon can no longer see, and
// every caller moved with them.

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
// codex launch-spec test seams (hk-rgxwd C2/T7) — RETAINED after P2 E1a-1
// ─────────────────────────────────────────────────────────────────────────────
//
// The codex harness moved to internal/harness/codex in P2 unit E1a-1. Its own
// test seams moved with it (internal/harness/codex/export_test.go). These two
// stay because their consumers stayed: crossharness_empty_model_test.go and
// crossharness_seedprompt_test.go pin a pi-vs-codex invariant in ONE table and
// cannot move until pi is out too (E1c). They are thin aliases onto the now-
// exported codex.RunCtx / codex.BuildLaunchSpec, which exist for exactly this
// reason and go back to unexported once E1c lands.
//
// Plan: plans/2026-07-21-p2-extraction/E1a-codex-harness.md §3b, §4 step 13.

// ExportedCodexRunCtx is the exported shape of the codex per-launch run context.
//
// Bead refs: hk-rgxwd (T7), hk-tu48u (T11 billing-guard fields), hk-heh3t (model guard).
type ExportedCodexRunCtx = codex.RunCtx

// ExportedBuildCodexLaunchSpec exposes codex.BuildLaunchSpec for tests in
// package daemon_test.
//
// Bead ref: hk-rgxwd.
var ExportedBuildCodexLaunchSpec = codex.BuildLaunchSpec

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

// The brQueueLedger test seam (hk-dv8qv — ledger-dep direction regression) moved
// to internal/queuewiring/export_test.go as ExportedQueueLedger /
// ExportedNewBRQueueLedger, along with the bridge and its only caller (P2 E3a).

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
func ExportedNewPerRunEventTap(runID core.RunID) (tap *ExportedPerRunEventTap, events <-chan core.EventEnvelope) {
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

// ExportedNewClaudeHarness re-exports claude.NewHarness for tests in package
// daemon_test.
//
// RETAINED after P2 E1b for the same reason as ExportedNewCodexHarness below:
// regression_golden_no_selection_hkhwwlk_test.go asserts, from package
// daemon_test, that the claude harness is what a no-selection bead resolves to
// and that its Completion() mode discriminates from codex's — a
// daemon-composition claim, not a claude-unit claim.
//
// Bead ref: hk-3kyh3.
var ExportedNewClaudeHarness = claude.NewHarness

// ExportedNewHarnessRegistry exposes newHarnessRegistry for tests in package
// daemon_test. It returns the daemon's HarnessRegistry with ClaudeHarness
// registered for core.AgentTypeClaudeCode. Pi harness is registered with an
// empty PiHarnessConfig (test-only; Pi fields are non-empty only in production
// when harnesses.pi is configured).
//
// Bead ref: hk-hj9ld.
func ExportedNewHarnessRegistry() (*handlercontract.HarnessRegistry, error) {
	return newHarnessRegistry(PiHarnessConfig{})
}

// ExportedNewHarnessRegistryWithPi exposes newHarnessRegistry with a configured
// PiHarnessConfig for tests that verify the config→harness seam (hk-f8u5j).
func ExportedNewHarnessRegistryWithPi(piCfg PiHarnessConfig) (*handlercontract.HarnessRegistry, error) {
	return newHarnessRegistry(piCfg)
}

// ExportedPiHarnessFields returns the provider, model, apiKeyEnv and apiKeyFile
// of a pi.Harness for test assertions on the config→harness seam (hk-f8u5j;
// apiKeyFile added by hk-xmfoi). P2 unit E1c moved pi.Harness out of this
// package, so the fields are read through the harness accessors instead of
// directly.
func ExportedPiHarnessFields(h *pi.Harness) (provider, model, apiKeyEnv, apiKeyFile string) {
	return h.Provider(), h.Model(), h.APIKeyEnv(), h.APIKeyFile()
}

// ExportedEffectiveModel exposes effectiveModel for tests in package daemon_test.
// Bead ref: hk-7z6l8.
func ExportedEffectiveModel(h handlercontract.Harness, model string) string {
	return effectiveModel(h, shared.LaunchCtx{Model: model})
}

// ExportedPiProcessExitLaunchSpecBuilder returns a launchSpecBuilder that
// produces a handler.LaunchSpec for the provided shell script and stamps
// resolvedAgentType = core.AgentTypePi on the returned artifacts.
//
// Use in tests that exercise the hk-j6wm7 Pi retain-on-failure + stdout/stderr
// capture behaviour: wire an AdapterRegistry (empty or claude-only, so
// waitAgentReady is skipped for the shell fixture) and a HarnessRegistry from
// ExportedNewHarnessRegistry (which registers the Pi harness →
// SessionIDPolicy() == SessionIDCaptured → the exec path + StdoutWrapper fire),
// then supply this builder so beadRunOne resolves the run as a Pi run.
//
// Bead ref: hk-j6wm7.
func ExportedPiProcessExitLaunchSpecBuilder(scriptPath string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		spec := handler.LaunchSpec{
			Binary:  "/bin/sh",
			Args:    []string{scriptPath},
			WorkDir: rc.WorkspacePath,
			Role:    string(rc.Phase),
		}
		arts := shared.LaunchArtifacts{
			ResolvedAgentType: core.AgentTypePi,
		}
		return spec, arts, nil
	}
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
// = shared.LaunchCtx.WorkspacePath).  It MUST make a "Refs: <beadID>" git commit and
// exit 0; it MUST NOT emit agent_ready.
//
// Bead ref: hk-f6g7.
func ExportedCodexProcessExitLaunchSpecBuilder(scriptPath string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		spec := handler.LaunchSpec{
			Binary:  "/bin/sh",
			Args:    []string{scriptPath},
			WorkDir: rc.WorkspacePath,
			Role:    string(rc.Phase),
		}
		arts := shared.LaunchArtifacts{
			ResolvedAgentType: core.AgentTypeCodex,
		}
		return spec, arts, nil
	}
}

// ExportedRoutedLaunchSpecBuilder exposes routedLaunchSpecBuilder for tests in
// package daemon_test. It returns a builder that resolves the harness via the
// four-tier precedence walk and the HarnessRegistry, then (for the claude
// harness) delegates to buildClaudeLaunchSpec. The returned closure has the same
// shape as the workLoopDeps.launchSpecBuilder hook.
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
	return routedLaunchSpecBuilder(reg, bead, queueDefault, nodeDefault, globalDefault, bus)
}

// ExportedObservedRoutedLaunchSpecBuilder returns the REAL production
// routedLaunchSpecBuilder in the INTERNAL workLoopDeps.launchSpecBuilder shape
// (so it can be installed via WorkLoopDepsParams.LaunchSpecBuilder), wrapped in
// a call observer. It is exactly the builder beadRunOne installs in production
// (workloop.go: routedLaunchSpecBuilder(reg, beadRecord, "", "", defaultHarness,
// bus)), so a test can assert whether a downstream dispatch site CONSULTED it or
// correctly REPLACED it. onCall fires once per invocation, before the build runs.
//
// Bead ref: hk-01vs0.
func ExportedObservedRoutedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
	onCall func(),
) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	builder := routedLaunchSpecBuilder(reg, bead, core.AgentType(""), core.AgentType(""), globalDefault, bus)
	return func(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		if onCall != nil {
			onCall()
		}
		return builder(ctx, rc)
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
	return pinnedHarnessLaunchSpecBuilder(reg, bead, agentType, bus)
}

// ─────────────────────────────────────────────────────────────────────────────
// codex harness constructor seam (hk-m57va C2/T8) — RETAINED after P2 E1a-1
// ─────────────────────────────────────────────────────────────────────────────
//
// The JSONL-parser and thread-id seams that used to sit here moved with the
// harness to internal/harness/codex/export_test.go. This one stays because
// regression_golden_no_selection_hkhwwlk_test.go asserts, from package
// daemon_test, that registering codex did not change claude's selection
// behaviour — a daemon-composition claim, not a codex-unit claim.

// ExportedNewCodexHarness re-exports codex.NewHarness for tests in package
// daemon_test.
//
// Bead ref: hk-m57va.
var ExportedNewCodexHarness = codex.NewHarness

// ─────────────────────────────────────────────────────────────────────────────
// Refs:<bead> trailer VERIFY seam (hk-bpxci C2/T9) — RETAINED after P2 E1a-1
// ─────────────────────────────────────────────────────────────────────────────
//
// The codex-specific decision-table seams (EnsureRefsTrailer, the outcome
// constants, the seed-prompt instruction, the no-work detector) moved to
// internal/harness/codex/export_test.go with the harness. This one stays
// because picommit_test.go — a PI test — uses it to verify the shared VERIFY
// half, and pi does not leave until E1c.

// ExportedShellQuoteArg exposes shellQuoteArg for unit tests in package daemon_test.
//
// Bead ref: hk-rpr6.
func ExportedShellQuoteArg(s string) string {
	return shellQuoteArg(s)
}

// ─────────────────────────────────────────────────────────────────────────────
// srt argv-wrap test seams (hk-rlxgx)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedSrtSpawnConfig is a type alias for SrtSpawnConfig so tests in
// package daemon_test can reference the type without importing internal symbols.
//
// Bead: hk-rlxgx.
type ExportedSrtSpawnConfig = SrtSpawnConfig

// ExportedNewPerRunSubstrateWithSandbox wraps newPerRunSubstrate and sets
// sandboxSpawn for tests exercising the srt argv-wrap path (hk-rlxgx).
//
// Returns nil when sub is nil or is not a *tmuxSubstrate (matching
// newPerRunSubstrate semantics).
//
// Bead: hk-rlxgx.
func ExportedNewPerRunSubstrateWithSandbox(sub handler.Substrate, cfg *SrtSpawnConfig) handler.Substrate {
	prs := newPerRunSubstrate(sub, "", nil)
	if prs == nil {
		return nil
	}
	prs.sandboxSpawn = cfg
	return prs
}

// ─────────────────────────────────────────────────────────────────────────────
// Pi harness construction seam (PI-010, hk-4rmj1)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewPiHarness re-exports pi.NewHarness for tests in package
// daemon_test. Three STAYING daemon tests construct a pi harness directly
// (sandboxgate_hkr4p0l_test.go, hk_lfrub_dot_node_model_leak_test.go,
// hk_pkugu_pi_model_leak_test.go), so this shim survives P2 unit E1c.
//
// Bead ref: hk-4rmj1 (PI-010/012/013).
var ExportedNewPiHarness = pi.NewHarness

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

// ─────────────────────────────────────────────────────────────────────────────
// pi.BuildLaunchSpec test seams (hk-1c16h PI-015/020/021)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedPiRunCtx is the exported shape of the pi per-launch run context.
// P2 unit E1c moved it to internal/harness/pi; this is now an alias, kept
// because two cross-harness parity tables in package daemon_test
// (crossharness_empty_model_test.go, crossharness_seedprompt_test.go) assert
// about codex AND pi in one table each and therefore cannot move.
//
// Bead ref: hk-1c16h.
type ExportedPiRunCtx = pi.RunCtx

// ExportedBuildPiLaunchSpec exposes pi.BuildLaunchSpec for tests in package
// daemon_test.
//
// Bead ref: hk-1c16h.
var ExportedBuildPiLaunchSpec = pi.BuildLaunchSpec

// ─────────────────────────────────────────────────────────────────────────────
// Pi billing guard test seams (hk-l1bkp PI-040/042/043)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedStrandedBeadHasOnDiskRun exposes strandedBeadHasOnDiskRun for tests
// in package daemon_test, so the race-conservative-on-List-error behavior
// (hk-r9edj) can be exercised directly without driving the full workloop.
func ExportedStrandedBeadHasOnDiskRun(projectDir string, beadID core.BeadID) bool {
	return strandedBeadHasOnDiskRun(projectDir, beadID)
}

// ExportedLoadQueueProvenance runs bootState.loadQueueProvenance for projectDir
// and returns the aggregated QueueDispatched / QueueOwned provenance sets. It is
// the test seam for hk-nddg1: loadQueueProvenance must enumerate ALL named
// queues (queue.EnumerateQueueNames), not just main, so a bead dispatched via a
// crew queue (e.g. queues/paul.json) lands in QueueDispatched and the orphan
// sweep's (a-queue) exclusion protects it from a double-dispatch reset.
func ExportedLoadQueueProvenance(ctx context.Context, projectDir string) (lifecycle.QueueDispatchedSet, lifecycle.QueueOwnedSet) {
	bs := &bootState{cfg: Config{ProjectDir: projectDir}}
	st := &reconcileState{}
	bs.loadQueueProvenance(ctx, st)
	return st.queueDispatched, st.queueOwned
}

// ExportedNewCapturedSpawnProof exposes newCapturedSpawnProof (hk-47u9z) so the
// regression test drives the PRODUCTION spawn-proof closure rather than a
// restatement of it. A test that rebuilds the closure itself passes with the
// production wiring reverted — verified, and it is how the first draft of the
// hk-47u9z test was a false green.
func ExportedNewCapturedSpawnProof(ctx context.Context, emitter handlercontract.EventEmitter, runID core.RunID) func() {
	tap, _ := newPerRunEventTap(emitter, runID)
	return newCapturedSpawnProof(ctx, tap, runID)
}
