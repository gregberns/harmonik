package daemon

// reconciliationcadence_rc020a.go — scheduled detector cadence for RC-020a.
//
// RC-020a declares three detector dispatch points:
//   (a) Daemon startup — handled in daemon.Start after orphan sweep.
//   (b) On-demand operator command — `harmonik reconcile [--run <run_id>]`.
//   (c) Scheduled cadence — this file: background scan at configurable interval.
//
// The scheduled scan emits reconciliation_started{trigger:"scheduled-hourly"}
// and then runs:
//   - Cat 3c auto-resolver: bead in_progress + merge commit on target branch → br close.
//   - Class B orphan repair (hk-m3ydd): bead in_progress with no queue record
//     → reset to open so it can be re-dispatched.
//
// The scan is idempotent across cadence ticks per RC-020a: same
// (target_run_id, snapshot) always produces the same category.
//
// Default interval: 3600 s (hourly) per reconciliation/spec.md §4.3 RC-020a
// and operator-nfr.md §4.3 knob reconciliation_scan_cadence.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Bead ref: hk-63oh.21, hk-m3ydd.

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
	"github.com/gregberns/harmonik/internal/queue"
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
//  5. Runs the Class B orphan repair: resets any in_progress beads that have
//     no queue record back to open (hk-m3ydd).
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
// emits reconciliation_started, runs the Cat 3c auto-resolver, runs
// the Class B orphan repair pass, then emits reconciliation_completed.
func runScheduledReconciliationScan(ctx context.Context, cfg ReconciliationSchedulerConfig, logW io.Writer) {
	// Emit reconciliation_started{trigger:"scheduled-hourly"} (RC-020a).
	reconciliationRunID, uidErr := uuid.NewV7()
	if uidErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: generate run ID: %v (skipping tick)\n", uidErr)
		return
	}
	runID := core.RunID(reconciliationRunID)
	payload := core.ReconciliationStartedPayload{
		ReconciliationRunID: runID,
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

	var beadsExamined, beadsClosed, beadsReset int

	// Always emit reconciliation_completed paired with reconciliation_started so
	// that a hung reconciliation (started with no matching completed) is
	// detectable. No-op ticks emit with all-zero counts.
	defer func() {
		completedPayload := core.ReconciliationCompletedPayload{
			ReconciliationRunID: runID,
			Trigger:             core.ReconciliationTriggerScheduled,
			BeadsExamined:       beadsExamined,
			BeadsClosed:         beadsClosed,
			BeadsReset:          beadsReset,
			CompletedAt:         time.Now().UTC().Format(time.RFC3339),
		}
		if completedBytes, cErr := json.Marshal(completedPayload); cErr == nil {
			_ = cfg.Emitter.Emit(ctx, core.EventTypeReconciliationCompleted, completedBytes)
		}
	}()

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

	beadsExamined = len(beads)

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
		beadsClosed++
		fmt.Fprintf(logW, "reconciliation scheduler: bead %s closed (Cat 3c scheduled)\n", bead.BeadID)
	}

	// Class B orphan repair: reset any in_progress beads that have no queue
	// record back to open so they can be re-dispatched.
	//
	// Spec ref: hk-m3ydd — scheduled reconciliation must repair bead_inprogress_queue_absent.
	beadsReset = runScheduledClassBRepair(scanCtx, cfg, adapter, beads, logW)
}

// runScheduledClassBRepair implements the Class B orphan repair pass for the
// scheduled reconciliation cadence.
//
// It loads all live queue files to build a beadsActivelyDispatched set (beads
// with at least one item in dispatched state), then for each in-flight bead
// NOT actively dispatched:
//  1. Emits a reconciliation_mismatch_observed event (always, for visibility).
//  2. Resets the bead to open via ResetBead (in_progress → open, BI-010d).
//
// Two mismatch classes are distinguished:
//   - bead_inprogress_queue_absent: bead has no queue record at all.
//   - bead_inprogress_queue_terminal: bead is in a queue but its item is in a
//     terminal state (failed/completed), meaning the run ended but ReopenBead
//     failed (e.g. cancelled per-run context). hk-e3fy: prior code skipped this
//     class because it only checked presence-in-any-queue, not item status.
//
// The BI-030 intent-log idempotency key uses the repair-pass timestamp (not
// the daemon start time) so that each hourly tick can re-attempt beads that
// were not successfully reset on a prior tick.
//
// Returns the number of beads successfully reset to open.
// Non-fatal: failures for individual beads are logged and skipped.
//
// Spec ref: hk-m3ydd — reconciliation must repair bead_inprogress_queue_absent.
// Bead ref: hk-e3fy — extend to also repair bead_inprogress_queue_terminal.
func runScheduledClassBRepair(
	ctx context.Context,
	cfg ReconciliationSchedulerConfig,
	resetter lifecycle.BeadResetter,
	inFlight []core.BeadRecord,
	logW io.Writer,
) int {
	if len(inFlight) == 0 || cfg.ProjectDir == "" {
		return 0
	}

	var resetCount int

	observedAt := time.Now().UTC()
	observedAtStr := observedAt.Format(time.RFC3339Nano)

	// Build beadsActivelyDispatched: bead IDs with at least one item in
	// dispatched state (i.e., the run is live). Items in terminal states
	// (failed/completed) do NOT block the repair — their run has ended.
	//
	// Also build beadsInAnyQueue to distinguish the two mismatch classes.
	//
	// hk-e3fy: prior code used beadsInQueue (all items regardless of status).
	// A bead in a complete-with-failures group with a failed item was skipped
	// even though its run had ended and ReopenBead had failed (cancelled ctx).
	// Narrowing to dispatched items closes this strand class.
	beadsActivelyDispatched := make(map[core.BeadID]struct{})
	beadsInAnyQueue := make(map[core.BeadID]struct{})
	names, enumErr := queue.EnumerateQueueNames(cfg.ProjectDir)
	if enumErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler (Class B): EnumerateQueueNames: %v (skipping repair)\n", enumErr)
		return 0
	}
	for _, name := range names {
		q, loadErr := queue.Load(ctx, cfg.ProjectDir, name)
		if loadErr != nil || q == nil {
			continue // skip corrupt/missing queues; non-fatal
		}
		for gi := range q.Groups {
			for _, item := range q.Groups[gi].Items {
				beadsInAnyQueue[item.BeadID] = struct{}{}
				if item.Status == queue.ItemStatusDispatched {
					beadsActivelyDispatched[item.BeadID] = struct{}{}
				}
			}
		}
	}

	// Derive repair dependencies from cfg.ProjectDir.
	intentLogDir := lifecycle.BeadsIntentsDir(cfg.ProjectDir)
	projectHash := lifecycle.ComputeProjectHash(cfg.ProjectDir)
	// Use the repair-pass timestamp as the idempotency-key NS so each hourly
	// tick uses a fresh key (allows re-attempts if a prior tick's reset failed
	// but left no durable intent file).
	repairNS := observedAt.UnixNano()

	for _, rec := range inFlight {
		if _, activelyDispatched := beadsActivelyDispatched[rec.BeadID]; activelyDispatched {
			continue // run is live — not a Class B orphan
		}

		// Determine the mismatch class for observability.
		mismatchClass := "bead_inprogress_queue_absent"
		if _, inAnyQueue := beadsInAnyQueue[rec.BeadID]; inAnyQueue {
			mismatchClass = "bead_inprogress_queue_terminal"
		}

		fmt.Fprintf(logW, "reconciliation scheduler (Class B): bead %s in_progress but %s\n", rec.BeadID, mismatchClass)

		// Emit reconciliation_mismatch_observed for operator visibility.
		if cfg.Emitter != nil {
			p := core.ReconciliationMismatchObservedPayload{
				QueueID:       "",
				GroupIndex:    -1,
				BeadID:        string(rec.BeadID),
				MismatchClass: mismatchClass,
				LedgerStatus:  string(rec.Status),
				QueueStatus:   "",
				ObservedAt:    observedAtStr,
			}
			payloadBytes, marshalErr := json.Marshal(p)
			if marshalErr != nil {
				fmt.Fprintf(logW, "reconciliation scheduler (Class B): marshal mismatch payload for %s: %v\n", rec.BeadID, marshalErr)
			} else if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeReconciliationMismatchObserved, payloadBytes); emitErr != nil {
				fmt.Fprintf(logW, "reconciliation scheduler (Class B): emit mismatch event for %s: %v\n", rec.BeadID, emitErr)
			}
		}

		// Repair: reset in_progress → open so the bead can be re-dispatched.
		resetCtx, cancelReset := context.WithTimeout(ctx, 30*time.Second)
		resetErr := resetter.ResetBead(
			resetCtx,
			intentLogDir,
			brcli.TimeoutConfig{},
			rec.BeadID,
			projectHash,
			repairNS,
		)
		cancelReset()
		if resetErr != nil {
			fmt.Fprintf(logW, "reconciliation scheduler (Class B): ResetBead %s: %v\n", rec.BeadID, resetErr)
			continue
		}
		resetCount++
		fmt.Fprintf(logW, "reconciliation scheduler (Class B): bead %s reset to open (%s)\n", rec.BeadID, mismatchClass)
	}
	return resetCount
}
