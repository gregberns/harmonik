package daemon

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
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
)

const (
	// ReconciliationScanCadenceDefault is the default for the scheduled
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

func startReconciliationSchedulerIfEnabled(ctx context.Context, pc projectconfig.ProjectConfig, cfg ReconciliationSchedulerConfig) bool {
	if !pc.Subsystems.Enabled(projectconfig.SubsystemReconciliationScheduler) {
		logW := cfg.LogWriter
		if logW == nil {
			logW = os.Stderr
		}
		fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; scheduler not constructed\n", //nolint:errcheck // best-effort stderr status log
			projectconfig.SubsystemReconciliationScheduler)
		return false
	}
	StartReconciliationScheduler(ctx, cfg)
	return true
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

func runScheduledReconciliationScan(ctx context.Context, cfg ReconciliationSchedulerConfig, logW io.Writer) {
	reconciliationRunID, uidErr := uuid.NewV7()
	if uidErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: generate run ID: %v (skipping tick)\n", uidErr) //nolint:errcheck // best-effort stderr status log
		return
	}
	runID := core.RunID(reconciliationRunID)
	payload := core.ReconciliationStartedPayload{
		ReconciliationRunID: runID,
		Trigger:             core.ReconciliationTriggerScheduled,
	}
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: marshal reconciliation_started: %v (skipping tick)\n", marshalErr) //nolint:errcheck // best-effort stderr status log
		return
	}
	if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeReconciliationStarted, payloadBytes); emitErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: emit reconciliation_started: %v\n", emitErr) //nolint:errcheck // best-effort stderr status log
	}

	var beadsExamined, beadsClosed, beadsReset int

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
			_ = cfg.Emitter.Emit(ctx, core.EventTypeReconciliationCompleted, completedBytes) //nolint:errcheck // best-effort audit emit; reconciliation_completed pairs the started event for hang detection only
		}
	}()

	if cfg.BrPath == "" {
		return
	}

	adapter, adapterErr := brcli.NewForProject(cfg.BrPath, cfg.ProjectDir)
	if adapterErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: br adapter: %v (skipping Cat 3c scan)\n", adapterErr) //nolint:errcheck // best-effort stderr status log
		return
	}

	scanCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	beads, listErr := adapter.ListInFlightBeads(scanCtx)
	if listErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler: list in_progress beads: %v\n", listErr) //nolint:errcheck // best-effort stderr status log
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
			fmt.Fprintf(logW, "reconciliation scheduler: bead %s git scan: %v (skipping)\n", bead.BeadID, scanErr) //nolint:errcheck // best-effort stderr status log
			continue
		}
		if !merged {
			continue
		}
		if closeErr := adapter.SweepCloseBead(scanCtx, timeoutCfg, bead.BeadID); closeErr != nil {
			fmt.Fprintf(logW, "reconciliation scheduler: bead %s close: %v\n", bead.BeadID, closeErr) //nolint:errcheck // best-effort stderr status log
			continue
		}
		beadsClosed++
		fmt.Fprintf(logW, "reconciliation scheduler: bead %s closed (Cat 3c scheduled)\n", bead.BeadID) //nolint:errcheck // best-effort stderr status log
	}

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
//
//nolint:gocognit,cyclop // over the threshold; splitting the queue-scan + per-bead repair mid-release is riskier than the marginal complexity
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

	beadsActivelyDispatched := make(map[core.BeadID]struct{})
	beadsInAnyQueue := make(map[core.BeadID]struct{})
	names, enumErr := queue.EnumerateQueueNames(cfg.ProjectDir)
	if enumErr != nil {
		fmt.Fprintf(logW, "reconciliation scheduler (Class B): EnumerateQueueNames: %v (skipping repair)\n", enumErr) //nolint:errcheck // best-effort stderr status log
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

	intentLogDir := lifecycle.BeadsIntentsDir(cfg.ProjectDir)
	projectHash := lifecycle.ComputeProjectHash(cfg.ProjectDir)
	repairNS := observedAt.UnixNano()

	for _, rec := range inFlight {
		if _, activelyDispatched := beadsActivelyDispatched[rec.BeadID]; activelyDispatched {
			continue // run is live — not a Class B orphan
		}

		mismatchClass := "bead_inprogress_queue_absent"
		if _, inAnyQueue := beadsInAnyQueue[rec.BeadID]; inAnyQueue {
			mismatchClass = "bead_inprogress_queue_terminal"
		}

		fmt.Fprintf(logW, "reconciliation scheduler (Class B): bead %s in_progress but %s\n", rec.BeadID, mismatchClass) //nolint:errcheck // best-effort stderr status log

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
				fmt.Fprintf(logW, "reconciliation scheduler (Class B): marshal mismatch payload for %s: %v\n", rec.BeadID, marshalErr) //nolint:errcheck // best-effort stderr status log
			} else if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeReconciliationMismatchObserved, payloadBytes); emitErr != nil {
				fmt.Fprintf(logW, "reconciliation scheduler (Class B): emit mismatch event for %s: %v\n", rec.BeadID, emitErr) //nolint:errcheck // best-effort stderr status log
			}
		}

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
			fmt.Fprintf(logW, "reconciliation scheduler (Class B): ResetBead %s: %v\n", rec.BeadID, resetErr) //nolint:errcheck // best-effort stderr status log
			continue
		}
		resetCount++
		fmt.Fprintf(logW, "reconciliation scheduler (Class B): bead %s reset to open (%s)\n", rec.BeadID, mismatchClass) //nolint:errcheck // best-effort stderr status log
	}
	return resetCount
}
