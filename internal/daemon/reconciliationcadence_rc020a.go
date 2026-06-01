package daemon

// reconciliationcadence_rc020a.go — scheduled detector cadence for RC-020a.
//
// RC-020a declares three detector dispatch points:
//   (a) Daemon startup — handled in daemon.Start after orphan sweep.
//   (b) On-demand operator command — `harmonik reconcile [--run <run_id>]`.
//   (c) Scheduled cadence — this file: background scan at configurable interval.
//
// The scheduled scan emits reconciliation_started{trigger:"scheduled-hourly"}
// and then runs the Cat 3c auto-resolver (bead in_progress + merge commit on
// target branch → br close). The scan is idempotent across cadence ticks per
// RC-020a: same (target_run_id, snapshot) always produces the same category.
//
// Default interval: 3600 s (hourly) per reconciliation/spec.md §4.3 RC-020a
// and operator-nfr.md §4.3 knob reconciliation_scan_cadence.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Bead ref: hk-63oh.21.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

const (
	// ReconciliationScanCadenceDefault is the MVH default for the scheduled
	// detector cadence (hourly) per RC-020a and operator-nfr.md §4.3.
	ReconciliationScanCadenceDefault = time.Hour
)

// ReconciliationSchedulerConfig holds the construction-time parameters for
// the scheduled cadence scan launched by StartReconciliationScheduler.
type ReconciliationSchedulerConfig struct {
	// ProjectDir is the harmonik project root. Must be non-empty.
	ProjectDir string

	// BrPath is the absolute path to the `br` binary. Must be non-empty for
	// the Cat 3c auto-resolver to run; when empty the scan emits the
	// reconciliation_started event but skips bead-ledger operations.
	BrPath string

	// TargetBranch is the git branch the merge-commit scanner checks.
	// Defaults to "main" when empty.
	TargetBranch string

	// Interval is the scan cadence. Zero or negative falls back to
	// ReconciliationScanCadenceDefault (hourly).
	Interval time.Duration

	// Emitter is used to emit reconciliation_started on each cadence tick.
	// Required.
	Emitter interface {
		Emit(ctx context.Context, eventType core.EventType, payload []byte) error
	}

	// LogWriter receives non-fatal scan status messages. Nil → os.Stderr.
	LogWriter io.Writer
}

// StartReconciliationScheduler launches the RC-020a scheduled detector cadence
// as a background goroutine. The goroutine runs until ctx is cancelled.
//
// On each tick it:
//  1. Emits reconciliation_started{trigger:"scheduled-hourly"}.
//  2. Lists all in_progress beads.
//  3. For each, checks git for a merge commit (Cat 3c auto-resolve).
//  4. Closes any subsumed beads via br close.
//
// Non-fatal: scan errors are logged and skipped; the goroutine continues.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a — "Scheduled cadence."
func StartReconciliationScheduler(ctx context.Context, cfg ReconciliationSchedulerConfig) {
	interval := cfg.Interval
	if interval <= 0 {
		interval = ReconciliationScanCadenceDefault
	}
	logW := cfg.LogWriter
	if logW == nil {
		logW = os.Stderr
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runScheduledReconciliationScan(ctx, cfg, logW)
			}
		}
	}()
}

// runScheduledReconciliationScan performs one scheduled detector scan:
// emits reconciliation_started, then runs the Cat 3c auto-resolver.
func runScheduledReconciliationScan(ctx context.Context, cfg ReconciliationSchedulerConfig, logW io.Writer) {
	// Emit reconciliation_started{trigger:"scheduled-hourly"} (RC-020a).
	reconciliationRunID, uidErr := uuid.NewV7()
	if uidErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: generate run ID: %v (skipping tick)\n", uidErr)
		return
	}
	payload := core.ReconciliationStartedPayload{
		ReconciliationRunID: core.RunID(reconciliationRunID),
		Trigger:             core.ReconciliationTriggerScheduled,
	}
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: marshal reconciliation_started: %v (skipping tick)\n", marshalErr)
		return
	}
	if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeReconciliationStarted, payloadBytes); emitErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: emit reconciliation_started: %v\n", emitErr)
		// Non-fatal: continue with the Cat 3c scan regardless.
	}

	// Skip bead-ledger operations when br is not configured.
	if cfg.BrPath == "" {
		return
	}

	adapter, adapterErr := brcli.NewForProject(cfg.BrPath, cfg.ProjectDir)
	if adapterErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: br adapter: %v (skipping Cat 3c scan)\n", adapterErr)
		return
	}

	scanCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	beads, listErr := adapter.ListInFlightBeads(scanCtx)
	if listErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: list in_progress beads: %v\n", listErr)
		return
	}
	if len(beads) == 0 {
		return
	}

	targetBranch := cfg.TargetBranch
	if targetBranch == "" {
		targetBranch = "main"
	}
	mergeScanner := lifecycle.GitMergeCommitScanner{
		ProjectDir:   cfg.ProjectDir,
		TargetBranch: targetBranch,
	}
	timeoutCfg := brcli.TimeoutConfig{}

	for _, bead := range beads {
		merged, scanErr := mergeScanner.HasMergeCommitForBead(scanCtx, bead.BeadID)
		if scanErr != nil {
			fmt.Fprintf(logW, "reconciliation scheduler: bead %s git scan: %v (skipping)\n", bead.BeadID, scanErr)
			continue
		}
		if !merged {
			continue
		}
		// Cat 3c auto-resolve: implementation has landed; close the bead.
		if closeErr := adapter.SweepCloseBead(scanCtx, timeoutCfg, bead.BeadID); closeErr != nil {
			fmt.Fprintf(logW, "reconciliation scheduler: bead %s close: %v\n", bead.BeadID, closeErr)
			continue
		}
		fmt.Fprintf(logW, "reconciliation scheduler: bead %s closed (Cat 3c scheduled)\n", bead.BeadID)
	}
}
