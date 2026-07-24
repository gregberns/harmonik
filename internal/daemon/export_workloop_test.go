package daemon

// export_workloop_test.go — the residue of the RT19 split of the former
// export_test.go: the work-loop core accessors and the remaining
// single-owner seams (launch-spec builders, composition-root wiring,
// stale-watch, bandwidth-tuner, heartbeat, paste-inject, queue accessors,
// pane-liveness, cognition builder, stranded-bead guard) that no topic file
// claimed. See the sibling export_*_test.go files for the topic-grouped seams.
//
// This file is compiled only when running tests (it lives in package daemon,
// not daemon_test). It exports otherwise-unexported symbols so that
// workloop_test.go (package daemon_test) can inject stub dependencies without
// modifying the production API surface.
//
// Bead: hk-ecrxy.

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
)

// ExportedWorkLoopDefaultHarness returns the defaultHarness field from deps so
// tests can assert Config.DefaultHarness is correctly wired into the dispatch
// path (hk-ytzj2).
func ExportedWorkLoopDefaultHarness(deps workLoopDeps) core.AgentType {
	return deps.defaultHarness
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

// ─────────────────────────────────────────────────────────────────────────────
// CHB-025 test seams (hk-w5vra.11)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedProductionWorktreeFactory exposes productionWorktreeFactory for tests
// that need to wrap or observe real git worktree creation (e.g. merge-to-main
// integration tests).
//
// Bead ref: hk-kqdpf.1.
var ExportedProductionWorktreeFactory = productionWorktreeFactory

// ExportedNoCommitGuardShouldReopen exposes noCommitGuardShouldReopen for the
// single-mode no-commit guard regression test (hk-4ie1z).
func ExportedNoCommitGuardShouldReopen(ctx context.Context, projectDir, curHeadSHA, parentSHA string, beadID core.BeadID) bool {
	return noCommitGuardShouldReopen(ctx, projectDir, curHeadSHA, parentSHA, beadID)
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

// (duplicate buildClaudeLaunchSpec stubs removed — canonical declarations above at lines ~295-356)

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

// ─────────────────────────────────────────────────────────────────────────────
// Project config + model resolution test seams (hk-bfvk7)
// ─────────────────────────────────────────────────────────────────────────────

// HandlerEnvOf returns the handlerEnv field from deps.
// Used by tests to assert HARMONIK_PROJECT_HASH injection (hk-nvrvp).
func HandlerEnvOf(deps workLoopDeps) []string {
	return deps.handlerEnv
}

// ─────────────────────────────────────────────────────────────────────────────
// QueueStore test seams (hk-j808w)
// ─────────────────────────────────────────────────────────────────────────────

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

// The two handler-invoking shims (ExportedQueueOpConsumerHandlePauseStatus /
// ExportedQueueOpConsumerHandleResuming) moved to
// internal/queuewiring/export_test.go with the consumer they drive (P2 E3a):
// their bodies reach unexported methods package daemon can no longer see, and
// every caller moved with them.

// The brQueueLedger test seam (hk-dv8qv — ledger-dep direction regression) moved
// to internal/queuewiring/export_test.go as ExportedQueueLedger /
// ExportedNewBRQueueLedger, along with the bridge and its only caller (P2 E3a).

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
// Pi billing guard test seams (hk-l1bkp PI-040/042/043)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedStrandedBeadHasOnDiskRun exposes strandedBeadHasOnDiskRun for tests
// in package daemon_test, so the race-conservative-on-List-error behavior
// (hk-r9edj) can be exercised directly without driving the full workloop.
func ExportedStrandedBeadHasOnDiskRun(projectDir string, beadID core.BeadID) bool {
	return strandedBeadHasOnDiskRun(projectDir, beadID)
}
