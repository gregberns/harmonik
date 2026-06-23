package scenario

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

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
// On return, nil means the daemon reached a normal terminal state (queue exited).
// A non-nil error means the daemon startup, dispatch, or shutdown failed in a
// way that is not scenario-attributable. The caller MUST classify such errors
// as FailureClassOrchestrationInternalError per SH-019, capturing err.Error()
// in ScenarioResult.ErrorDetail verbatim.
//
// Spec ref: specs/scenario-harness.md §4.5 SH-017, SH-018, SH-019.
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

	if err := DaemonEntryPoint(runCtx, daemonCfg); err != nil {
		return fmt.Errorf("orchestration drive: %w", err)
	}
	return nil
}
