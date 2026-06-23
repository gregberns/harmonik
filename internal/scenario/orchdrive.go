package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ErrScenarioTimeout is returned by DriveOrchestration when the scenario's
// wall-clock deadline (cfg.TimeoutSecs per SH-025) was exceeded before the
// daemon reached a terminal state.
//
// On receiving this error the caller MUST (SH-026, §4.6, §8.6):
//  1. Read the partial JSONL event log via ReadEventLog — a best-effort read.
//  2. Call EvaluateAssertions against the partial event set (SH-023 no-short-circuit
//     contract applies; assertions impossible due to missing events are recorded with
//     passed=false, not skipped).
//  3. Run TeardownFixture (idempotent; SH-015).
//  4. Emit ScenarioResult{Verdict: ScenarioVerdictTimeout,
//     FailureClass: FailureClassScenarioTimeout, AssertionResults: <from step 2>}.
//
// The verdict MUST remain timeout even if assertion evaluation finds failures —
// scenario-timeout (precedence rank 6 per §8.0) supersedes assertion-failed (rank 7).
//
// Spec ref: specs/scenario-harness.md §4.7 SH-026, §4.6 SH-023, §8.6.
var ErrScenarioTimeout = errors.New("scenario timeout: orchestration drive exceeded deadline")

// DaemonEntryPoint is the daemon composition-root function invoked by the
// orchestration drive per specs/scenario-harness.md §4.5 SH-017.
//
// It is exposed as a package-level variable so that cross-package tests can
// verify function identity — confirming that the harness uses the SAME
// composition-root function as production daemon mode, never a substitute or
// wrapper (specs/scenario-harness.md §10.2 SH-017 test obligation).
//
// This variable MUST NOT be reassigned in production code paths.
//
// Spec ref: specs/scenario-harness.md §4.5 SH-017, §10.2 (test obligation).
var DaemonEntryPoint = daemon.Start

// OrchestrationConfig holds the per-scenario configuration for the
// orchestration drive declared in specs/scenario-harness.md §4.5 SH-017.
//
// The harness constructs this from the loaded ScenarioFile, the resolved
// FixtureBootstrapResult, and the harness CLI flags before calling
// DriveOrchestration. All path fields must be absolute.
type OrchestrationConfig struct {
	// ProjectDir is the per-scenario synthetic project root (SH-016a).
	// The daemon writes .harmonik/ artifacts here; the operator's .harmonik/
	// tree is untouched (SH-014). Typically FixtureBootstrapResult.ProjectRoot.
	ProjectDir string

	// JSONLLogPath is the absolute path to the per-scenario JSONL event log
	// the daemon writes to and the assertion evaluator reads from (SH-014,
	// SH-020). Use EventLogPath(ProjectDir) for the canonical value.
	JSONLLogPath string

	// HandlerBinary is the resolved absolute path to the twin binary that
	// replaces the production agent for all roles in this scenario (SH-008,
	// SH-009). Must be non-empty.
	HandlerBinary string

	// HandlerArgs are additional arguments appended to the twin binary
	// invocation (SH-008 AgentOverride.Args merge semantics). May be nil.
	HandlerArgs []string

	// BrPath is the absolute path to the `br` CLI binary forwarded to the
	// daemon work loop. When empty the work loop is disabled.
	BrPath string

	// KerfPath is the absolute path to the `kerf` CLI binary. When empty,
	// eager-refill is disabled.
	KerfPath string

	// WorkflowMode is the daemon workflow mode for this scenario run.
	// When empty it defaults to core.WorkflowModeReviewLoop (PL-004a).
	WorkflowMode core.WorkflowMode

	// TimeoutSecs is the per-scenario wall-clock budget in seconds per
	// SH-025. Must be in [1, 7200] when set. Zero disables enforcement
	// (harness enforces this via ScenarioFile.Valid() at load time; the
	// orchestration drive honours whatever the caller supplies). When
	// non-zero, the orchestration drive enforces a monotonic-clock deadline
	// and returns ErrScenarioTimeout on exceedance per SH-026.
	//
	// TODO(SH-032/G-01): the harness CLI must populate this from
	// ScenarioFile.TimeoutSecs before calling DriveOrchestration. A zero
	// TimeoutSecs silently disables enforcement — the harness CLI MUST NOT
	// call DriveOrchestration with TimeoutSecs==0 when the scenario file
	// declares a budget.
	TimeoutSecs int
}

// DriveOrchestration invokes the production daemon entry-point against the
// per-scenario synthetic project root with handler-config overrides per
// specs/scenario-harness.md §4.5 SH-017.
//
// The daemon is configured with:
//   - ProjectDir = cfg.ProjectDir (per-scenario synthetic project root, SH-016a)
//   - HandlerBinary = cfg.HandlerBinary (twin binary override, SH-008)
//   - JSONLLogPath = cfg.JSONLLogPath (per-scenario event log, SH-014)
//   - MaxConcurrent = 1 (sequential execution per SH-031)
//   - Substrate = nil (no tmux in harness mode; exec.CommandContext fallback)
//   - CancelOnQueueExit wired to a child context so the daemon self-terminates
//     when the scenario's bead queue reaches a terminal state (all-success or
//     paused-by-failure).
//
// The daemon's startup sequence (PL-005 steps 0-9) runs in full. Reconciliation
// at step 8 is a no-op against fresh fixture state (SH-016a). The harness-mode
// skip flags (WAL checkpoint, br-history rotation, restart backoff, beads merge
// driver config) are set to suppress non-fatal pre-flights that are not
// applicable to synthetic project roots.
//
// Timeout enforcement (SH-026): when cfg.TimeoutSecs > 0, a monotonic-clock
// deadline is applied to the run context. If the daemon does not reach a
// terminal state before the deadline, the run context is cancelled (equivalent
// to a daemon stop per SH-026) and ErrScenarioTimeout is returned. The
// deadline check takes precedence over any coincident daemon error so that a
// timeout always routes to scenario-timeout rather than
// orchestration-internal-error (spec order-of-checks note at §7.1 step 3).
// The caller MUST then evaluate assertions best-effort against the partial
// event log and emit verdict=timeout / failure_class=scenario-timeout per
// SH-026, SH-023.
//
// On return, nil means the daemon reached a normal terminal state (queue
// exited). ErrScenarioTimeout means the deadline was exceeded. Any other
// non-nil error means the daemon startup, dispatch, or shutdown failed in a
// way that is not scenario-attributable; the caller MUST classify such errors
// as FailureClassOrchestrationInternalError per SH-019, capturing err.Error()
// in ScenarioResult.ErrorDetail verbatim.
//
// Spec ref: specs/scenario-harness.md §4.5 SH-017, SH-018, SH-019, §4.7
// SH-025, SH-026.
func DriveOrchestration(ctx context.Context, cfg OrchestrationConfig) error {
	mode := cfg.WorkflowMode
	if mode == "" {
		mode = core.WorkflowModeReviewLoop
	}

	// Create a child context that the daemon cancels via CancelOnQueueExit
	// when the scenario's bead queue reaches a terminal state. This ensures
	// daemon.Start returns promptly on scenario completion without requiring an
	// external stop RPC call.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	// Apply the scenario's wall-clock deadline (SH-026). The deadline context
	// is a child of the queue-exit context so that either cancellation signal
	// (queue exit or timeout) propagates to daemon.Start.
	//
	// Daemon-stop equivalence: context.WithTimeout cancellation propagates to
	// daemon.Start's context, triggering the daemon's graceful-drain path
	// (bounded by ON-029 drain-timeout per process-lifecycle.md §4.2 PL-003a).
	// This is architecturally equivalent to calling the daemon stop RPC: the
	// daemon observes context.Done() and initiates the same drain sequence it
	// would on a stop-RPC call, honoring the HC-018 per-handler cancellation
	// bounds. Using the context is the Go-idiomatic surface for this signal
	// when daemon.Start runs in-process (as in the harness — Substrate=nil).
	//
	// Go's monotonic clock component (present in time.Now()-derived Times) is
	// used automatically by context.WithTimeout per SH-025 monotonic-clock
	// requirement — NTP wall-clock regressions do not cause spurious timeouts.
	if cfg.TimeoutSecs > 0 {
		var deadlineCancel context.CancelFunc
		runCtx, deadlineCancel = context.WithTimeout(runCtx, time.Duration(cfg.TimeoutSecs)*time.Second)
		defer deadlineCancel()
	}

	daemonCfg := daemon.Config{
		ProjectDir:          cfg.ProjectDir,
		WorkflowModeDefault: mode,
		JSONLLogPath:        cfg.JSONLLogPath,
		BrPath:              cfg.BrPath,
		KerfPath:            cfg.KerfPath,
		HandlerBinary:       cfg.HandlerBinary,
		HandlerArgs:         cfg.HandlerArgs,
		MaxConcurrent:       1,
		// CancelOnQueueExit: daemon self-terminates when the scenario's bead queue
		// reaches a terminal state (all-success or paused-by-failure).
		CancelOnQueueExit: cancelRun,
		// Substrate nil: harness runs without tmux; daemon falls back to
		// exec.CommandContext per specs/process-lifecycle.md §4.7 PL-021b.
		// The $TMUX guard in cmd/harmonik/main.go is a CLI-layer check, not a
		// daemon.Start requirement.

		// Harness-mode skip flags: suppress pre-flights not applicable to
		// ephemeral synthetic project roots (SH-016a).
		SkipWALCheckpoint:          true,
		SkipBrHistoryRotation:      true,
		SkipRestartBackoff:         true,
		SkipBeadsMergeDriverConfig: true,
		// NoAutoPull: harness controls dispatch via the queue surface; it must
		// not self-seed from `br ready` which would dispatch beads outside the
		// scenario's declared scope.
		NoAutoPull: true,
	}

	err := DaemonEntryPoint(runCtx, daemonCfg)

	// Timeout detection: check deadline exceedance FIRST per spec order-of-checks
	// note at §7.1 step 3 — a coincident daemon error on the timeout path routes
	// to scenario-timeout, not orchestration-internal-error. The parent ctx must
	// still be live; if it is already cancelled, the cancellation was external
	// (SIGINT/SIGTERM — not a timeout) and should propagate as a normal error.
	if cfg.TimeoutSecs > 0 && ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return ErrScenarioTimeout
	}

	if err != nil {
		return fmt.Errorf("orchestration drive: %w", err)
	}
	return nil
}
