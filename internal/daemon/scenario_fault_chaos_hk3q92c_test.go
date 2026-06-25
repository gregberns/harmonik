package daemon_test

// scenario_fault_chaos_hk3q92c_test.go — L4 fault/chaos + record-replay harness.
//
// Builds on L1 (hk-52xnr, FS-separation). Tests RESILIENCE: the failure modes
// a real remote server exercises worse than the happy path.
//
// # Fault classes under test
//
//   A. Stale git ref   — CreateReviewerWorktree fails when the HEAD SHA is
//                        unknown to the repo (simulates worker with stale origin).
//   B. Dropped read    — ExportedReadGateVerdictVia / ExportedGateVerdictExistsVia
//                        with an error-injecting runner (SSH connection drops
//                        during `cat` / `test -s`).
//   C. Agent timeout   — ExportedRunReviewLoopWithRunner with a sleeping handler +
//                        short context; review loop must return promptly, not hang.
//   D. Flaky net       — ExportedGateVerdictExistsVia with a runner that fails the
//                        first N calls then succeeds; shows alternating behaviour
//                        without panic.
//   E. Record-replay   — RecordingRunner captures gate-verdict route (proves command
//                        goes through the runner); hk3q92cReplayRunner replays the
//                        same call and produces the same result without the worker dir.
//
// Helper prefix: hk3q92c (bead hk-3q92c, per implementer-protocol §Helper-prefix).
// Spec ref: remote-substrate test-pyramid L4.
// Bead: hk-3q92c.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// ─────────────────────────────────────────────────────────────────────────────
// hk3q92cErrRunner — always-failing CommandRunner
// ─────────────────────────────────────────────────────────────────────────────

// hk3q92cErrRunner is a CommandRunner that returns exit code 1 for every
// command, simulating a broken transport (dropped SSH connection, etc.).
type hk3q92cErrRunner struct{}

func (r hk3q92cErrRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	//nolint:gosec // G204: test-controlled literal; not user input
	return exec.CommandContext(ctx, "false") // exits 1
}

// Compile-time assertion.
var _ tmux.CommandRunner = hk3q92cErrRunner{}

// ─────────────────────────────────────────────────────────────────────────────
// hk3q92cFlakyRunner — intermittent-failure CommandRunner
// ─────────────────────────────────────────────────────────────────────────────

// hk3q92cFlakyRunner fails the first failN Command() invocations (exits 1) then
// delegates to base. Models a transient SSH hiccup: the first N attempts see
// the file as absent, then after a notional reconnect the reads succeed.
type hk3q92cFlakyRunner struct {
	base   tmux.CommandRunner
	failN  int32
	called atomic.Int32
}

func (r *hk3q92cFlakyRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	n := r.called.Add(1)
	if n <= atomic.LoadInt32(&r.failN) {
		//nolint:gosec // G204: test-controlled literal; not user input
		return exec.CommandContext(ctx, "false") // exits 1 — simulates a transient failure
	}
	if r.base != nil {
		return r.base.Command(ctx, name, args...)
	}
	//nolint:gosec // G204: test-controlled literal; not user input
	return exec.CommandContext(ctx, "true") // exits 0
}

// Compile-time assertion.
var _ tmux.CommandRunner = (*hk3q92cFlakyRunner)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// hk3q92cReplayRunner — tape-playback CommandRunner
// ─────────────────────────────────────────────────────────────────────────────

// hk3q92cReplayRunner is the "tape" half of the record-replay scenario (E).
// It accepts any 'test' command as if the file exists (returns exit 0), and
// rejects everything else (returns exit 1). This allows ExportedGateVerdictExistsVia
// to return true without the actual worker dir present — proving the seam is
// fully mediated by the runner.
type hk3q92cReplayRunner struct{}

func (r hk3q92cReplayRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if name == "test" {
		//nolint:gosec // G204: test-controlled literal; not user input
		return exec.CommandContext(ctx, "true") // any 'test' call → "yes, exists"
	}
	//nolint:gosec // G204: test-controlled literal; not user input
	return exec.CommandContext(ctx, "false")
}

// Compile-time assertion.
var _ tmux.CommandRunner = hk3q92cReplayRunner{}

// ─────────────────────────────────────────────────────────────────────────────
// hk3q92cFixtureSleepingHandlerScript — timeout test helper
// ─────────────────────────────────────────────────────────────────────────────

// hk3q92cFixtureSleepingHandlerScript writes a /bin/sh handler that sleeps
// indefinitely. Used for agent-timeout tests where the handler is expected to
// be killed by context cancellation.
func hk3q92cFixtureSleepingHandlerScript(t *testing.T) string {
	t.Helper()
	script := "#!/bin/sh\nsleep 9999\n"
	path := filepath.Join(t.TempDir(), "hk3q92c_sleep.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("hk3q92cFixtureSleepingHandlerScript: WriteFile: %v", err)
	}
	return path
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — stale git ref (AC-A)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FaultChaos_StaleRef_ReviewerWorktreeCreateFails_hk3q92c verifies
// that workspace.CreateReviewerWorktree returns ErrWorktreeCreationFailed when
// the supplied HEAD SHA does not exist in the repo.
//
// Real-server analogue: the worker's git clone hasn't fetched the implementer's
// commit yet — `git worktree add --detach <wt> <sha>` exits non-zero and the
// daemon must surface a clean error, not panic.
func TestScenario_FaultChaos_StaleRef_ReviewerWorktreeCreateFails_hk3q92c(t *testing.T) {
	t.Parallel()

	projectDir := hk52xnrFixtureProjectSetup(t)

	// 40-char hex SHA that is unknown to the fresh git repo.
	staleSHA := "cafebabe000000000000000000000000cafebabe"

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, _, err := workspace.CreateReviewerWorktree(
		ctx, projectDir, "hk3q92c-stale-run", 1, staleSHA,
		workspace.NoWorktreeRootOverride(),
	)
	if err == nil {
		t.Fatal("FaultChaos StaleRef: CreateReviewerWorktree succeeded with non-existent SHA; " +
			"want ErrWorktreeCreationFailed")
	}
	if !errors.Is(err, workspace.ErrWorktreeCreationFailed) {
		t.Errorf("FaultChaos StaleRef: error = %v; want errors.Is(err, ErrWorktreeCreationFailed)", err)
	}
	t.Logf("FaultChaos StaleRef PASS: got expected ErrWorktreeCreationFailed for SHA %q: %v",
		staleSHA, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — dropped read (AC-B)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FaultChaos_DroppedRead_GateVerdictVia_hk3q92c verifies that
// ExportedReadGateVerdictVia propagates the runner error without panicking when
// the transport drops the `cat` command (exit 1).
func TestScenario_FaultChaos_DroppedRead_GateVerdictVia_hk3q92c(t *testing.T) {
	t.Parallel()

	_, verdictPath := hk52xnrFixtureGatePaths(t)

	_, err := daemon.ExportedReadGateVerdictVia(context.Background(), hk3q92cErrRunner{}, verdictPath)
	if err == nil {
		t.Error("FaultChaos DroppedRead GateVerdictVia: expected error (cat failed); got nil")
	}
}

// TestScenario_FaultChaos_DroppedRead_GateVerdictExists_hk3q92c verifies that
// ExportedGateVerdictExistsVia returns false (not panic) when the runner's
// `test -s` call fails.
func TestScenario_FaultChaos_DroppedRead_GateVerdictExists_hk3q92c(t *testing.T) {
	t.Parallel()

	_, verdictPath := hk52xnrFixtureGatePaths(t)

	got := daemon.ExportedGateVerdictExistsVia(context.Background(), hk3q92cErrRunner{}, verdictPath)
	if got {
		t.Error("FaultChaos DroppedRead GateVerdictExists: returned true; " +
			"want false (runner error → absent)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario C — agent timeout (AC-C)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FaultChaos_AgentTimeout_ReviewLoopCancels_hk3q92c verifies that
// ExportedRunReviewLoopWithRunner returns promptly when the outer context
// expires while the implementer handler is sleeping.
//
// Pinned invariant: the run loop MUST NOT outlive the context deadline regardless
// of what the handler subprocess is doing — callers must be able to bound wall-
// clock via a context rather than waiting for a subprocess to self-terminate.
func TestScenario_FaultChaos_AgentTimeout_ReviewLoopCancels_hk3q92c(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := hk52xnrFixtureProjectSetup(t)
	wtPath, parentSHA := hk52xnrFixtureImplWorktree(t, projectDir)
	scriptPath := hk3q92cFixtureSleepingHandlerScript(t)

	sub := &nilwatcherFixtureNilStdoutSubstrate{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		Substrate:           sub,
		// Empty sealed registry: ForAgent returns error → waitAgentReady skipped.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	// 4-second context: enough time for the subprocess to start and be killed,
	// the review loop to detect the cancellation, and return.
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()

	start := time.Now()
	result := daemon.ExportedRunReviewLoopWithRunner(
		ctx, deps,
		hk52xnrFixtureRunID(t),
		core.BeadID("hk-3q92c-timeout-001"),
		wtPath, parentSHA,
		nil,
	)
	elapsed := time.Since(start)

	// Pinned: the loop must return well within the test's own deadline.
	const maxAllowed = 8 * time.Second
	if elapsed > maxAllowed {
		t.Errorf("FaultChaos AgentTimeout: review loop ran for %v (> %v) — loop outlived context deadline; "+
			"result=%+v", elapsed, maxAllowed, result)
	}
	if result.Success {
		t.Errorf("FaultChaos AgentTimeout: expected success=false (sleeping handler commits nothing); got true")
	}
	t.Logf("FaultChaos AgentTimeout PASS: result=%+v elapsed=%v", result, elapsed)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario D — flaky net (AC-D)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FaultChaos_FlakyNet_GateVerdictExists_hk3q92c verifies that
// ExportedGateVerdictExistsVia faithfully reflects intermittent runner failures:
// the first 2 calls return false (runner exits 1), the 3rd returns true (runner
// delegates to the path-remapping runner, which routes to the seeded worker dir).
//
// Real-server analogue: a transient SSH hiccup drops the first two stat probes;
// on the 3rd attempt the connection is healthy and the file is found.
func TestScenario_FaultChaos_FlakyNet_GateVerdictExists_hk3q92c(t *testing.T) {
	t.Parallel()

	worktreesRoot, verdictPath := hk52xnrFixtureGatePaths(t)
	workerDir := hk52xnrFixtureWorkerDir(t)

	successRunner := hk52xnrPathRemapRunner{
		worktreesRoot: worktreesRoot,
		workerDir:     workerDir,
	}
	flaky := &hk3q92cFlakyRunner{base: successRunner, failN: 2}

	ctx := context.Background()

	// ── First two calls: runner still failing (hiccup window) ────────────────
	got1 := daemon.ExportedGateVerdictExistsVia(ctx, flaky, verdictPath)
	if got1 {
		t.Error("FaultChaos FlakyNet: call 1 returned true; want false (runner failing, simulated hiccup)")
	}

	got2 := daemon.ExportedGateVerdictExistsVia(ctx, flaky, verdictPath)
	if got2 {
		t.Error("FaultChaos FlakyNet: call 2 returned true; want false (runner still failing)")
	}

	// ── Third call: runner recovered — file present on worker ─────────────────
	got3 := daemon.ExportedGateVerdictExistsVia(ctx, flaky, verdictPath)
	if !got3 {
		t.Error("FaultChaos FlakyNet: call 3 returned false; want true (runner succeeded, file on worker)")
	}

	t.Logf("FaultChaos FlakyNet PASS: gate-verdict calls=[%v %v %v] (fail fail succeed)", got1, got2, got3)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario E — record-replay (AC-E)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FaultChaos_RecordReplay_GateVerdictRoute_hk3q92c verifies the
// gate-verdict-exists seam in three phases:
//
//  1. Record: run ExportedGateVerdictExistsVia with a RecordingRunner wrapping
//     the path-remap runner; capture the `test -s` call that was issued.
//
//  2. Verify: assert the recording captured exactly one 'test -s <verdictPath>'
//     call. The runner was invoked (not os.Stat) because verdictPath is absent
//     locally yet got1==true — proves the runner's CmdFunc found the file on
//     the worker dir.
//
//  3. Replay: re-run with hk3q92cReplayRunner (returns exit 0 for any 'test')
//     and confirm the same result (true) without the real worker dir — shows
//     any runner can serve the replay role.
func TestScenario_FaultChaos_RecordReplay_GateVerdictRoute_hk3q92c(t *testing.T) {
	t.Parallel()

	worktreesRoot, verdictPath := hk52xnrFixtureGatePaths(t)
	workerDir := hk52xnrFixtureWorkerDir(t)

	base := hk52xnrPathRemapRunner{
		worktreesRoot: worktreesRoot,
		workerDir:     workerDir,
	}

	ctx := context.Background()

	// ── Phase 1: Record ──────────────────────────────────────────────────────
	rr := &tmux.RecordingRunner{CmdFunc: base.Command}
	got1 := daemon.ExportedGateVerdictExistsVia(ctx, rr, verdictPath)
	if !got1 {
		t.Fatal("FaultChaos RecordReplay Phase 1: ExportedGateVerdictExistsVia returned false; " +
			"want true (worker dir seeded with gate-verdict.json)")
	}

	// ── Phase 2: Verify recording ────────────────────────────────────────────
	//
	// The RecordingRunner captures the args HANDED TO IT — the box-A verdictPath.
	// Path remapping happens INSIDE CmdFunc before the subprocess is launched, so
	// the recording shows the routing request (box-A path), not the translated
	// worker path.
	//
	// "Route went through the runner" is proven structurally:
	//   - verdictPath is absent on box A (hk52xnrFixtureGatePaths precondition)
	//   - got1 == true means something reported "file exists"
	//   - RecordingRunner recorded exactly one call
	//   => The runner's CmdFunc (path-remap) must have found the file on workerDir.
	calls := rr.Calls
	if len(calls) == 0 {
		t.Fatal("FaultChaos RecordReplay Phase 2: RecordingRunner captured 0 calls; expected 1")
	}
	first := calls[0]
	if first.Name != "test" {
		t.Errorf("FaultChaos RecordReplay Phase 2: recorded command = %q; want 'test'", first.Name)
	}
	if len(first.Args) < 2 || first.Args[0] != "-s" {
		t.Errorf("FaultChaos RecordReplay Phase 2: recorded args = %v; want ['-s', <path>]", first.Args)
	}
	// The recording captures the box-A verdictPath (what was handed to the runner).
	recordedPath := first.Args[len(first.Args)-1]
	if recordedPath != verdictPath {
		t.Errorf("FaultChaos RecordReplay Phase 2: recorded path = %q; want verdictPath %q "+
			"(recording should capture the box-A routing request)", recordedPath, verdictPath)
	}
	// Explicit precondition guard: if verdictPath exists locally, os.Stat could
	// explain got1=true and the runner-routing proof collapses.
	if _, statErr := os.Stat(verdictPath); statErr == nil {
		t.Error("FaultChaos RecordReplay Phase 2: verdictPath exists on box A; " +
			"precondition broken — cannot distinguish runner route from os.Stat path")
	}

	// ── Phase 3: Replay ──────────────────────────────────────────────────────
	// hk3q92cReplayRunner accepts any 'test' call as exit 0 ("file exists").
	// verdictPath is still absent on box A; the replay runner provides the "yes"
	// that the original recording obtained from the worker via the CmdFunc remap.
	got2 := daemon.ExportedGateVerdictExistsVia(ctx, hk3q92cReplayRunner{}, verdictPath)
	if !got2 {
		t.Error("FaultChaos RecordReplay Phase 3: replay returned false; " +
			"want true (replay runner must be trusted, file absent locally)")
	}

	t.Logf("FaultChaos RecordReplay PASS: recorded %d call(s); route through runner proved "+
		"(got1=true, verdictPath absent on box A); replay runner produced same result", len(calls))
}
