package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/release"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// reconcileState threads the intermediate values produced by
// buildReconcileAdapters into the sweep/adopt/reconcile phase of the startup
// orphan-reconcile (P7). Extracted for giant-retirement boot-config B4.
type reconcileState struct {
	projectHash core.ProjectHash

	beadLedger          lifecycle.InFlightBeadLedger
	beadResetter        lifecycle.BeadResetter
	orphanStatusReader  beadStatusReader
	beadCat3cCloser     lifecycle.BeadCat3cCloser
	intentGCLedger      lifecycle.IntentGCLedger
	intentRedriveWriter lifecycle.IntentRedriveWriter
	intentLogDir        string

	queueDispatched  lifecycle.QueueDispatchedSet
	queueOwned       lifecycle.QueueOwnedSet
	sweepTmuxAdapter ltmux.Adapter
	daemonOwnSession string

	sweepResult OrphanSweepResult
}

// runStartupReconcile performs PL-005 / PL-006 step 3: the boot-time orphan
// sweep + in-flight-run reconcile, BEFORE any socket or listener bind. It holds
// the single ProjectDir guard and drives three sub-helpers (build adapters →
// sweep+adopt+reconcile → Cat-BL sweeps), each under the funlen/cyclop ceilings.
// All reconcile work uses context.Background() (matching the pre-extraction
// block); sweep/reconcile errors are non-fatal. The only fatal path is the
// BI-024a br --version handshake (exit code 8), surfaced from buildReconcileAdapters.
func (bs *bootState) runStartupReconcile(ctx context.Context, daemonStartTime time.Time, resolvedTargetBranch string) error {
	cfg := bs.cfg
	if cfg.ProjectDir == "" {
		return nil
	}
	st := &reconcileState{projectHash: lifecycle.ComputeProjectHash(cfg.ProjectDir)}

	if err := bs.buildReconcileAdapters(ctx, st); err != nil {
		return err
	}
	bs.runOrphanSweepAndAdopt(ctx, daemonStartTime, st)
	bs.runCatBLSweeps(ctx, resolvedTargetBranch)
	return nil
}

// buildReconcileAdapters constructs the BI bead adapter (with the BI-024a
// br --version handshake), reads the raw queue.json bead-provenance sets, and
// extracts the tmux adapter + daemon-own session name from the substrate. It
// returns a fatal error only when the br --version handshake fails structurally
// (exit code 8); a version delta is a NOTICE, and an adapter-construction
// failure is classified + emitted (non-fatal, queue-less proceed).
func (bs *bootState) buildReconcileAdapters(ctx context.Context, st *reconcileState) error {
	cfg := bs.cfg

	if err := bs.buildBeadAdapters(ctx, st); err != nil {
		return err
	}
	bs.loadQueueProvenance(ctx, st)

	// Extract the TmuxAdapter so the sweep can reap windows left by a prior
	// SIGKILL/OOM/crash (hk-xb5yi), via the package-private substrateWithAdapter.
	if sa, ok := cfg.Substrate.(substrateWithAdapter); ok {
		st.sweepTmuxAdapter = sa.tmuxAdapter()
	}

	// hk-9vp51: extract the daemon's own spawn-target session so the orphan sweep
	// EXCLUDES it (a fresh fallback session has only an idle window at boot and
	// would otherwise be classified orphaned and killed by the daemon's own sweep).
	if ss, ok := cfg.Substrate.(substrateWithSessionName); ok {
		st.daemonOwnSession = ss.daemonSessionName()
	}

	return nil
}

// buildBeadAdapters constructs the BI bead adapter and, on success, runs the
// BI-024a br --version handshake and populates the reconcile ledgers/resetters.
// An adapter-construction failure is classified + emitted (non-fatal); only a
// structural version-handshake failure is fatal (exit code 8).
func (bs *bootState) buildBeadAdapters(ctx context.Context, st *reconcileState) error {
	cfg := bs.cfg
	if cfg.BrPath == "" {
		return nil
	}
	brAdapter, brAdapterErr := newBrAdapter(bs.hooks, cfg.BrPath, cfg.ProjectDir)
	if brAdapterErr != nil {
		// Classify + emit divergence_inconclusive per BI-031b. Non-fatal.
		_ = brcli.BrErrReconciliationCategoryWithEmit(ctx, brAdapterErr, "br-new-for-project-sweep", bs.bus)
		return nil
	}
	if err := bs.brVersionHandshake(ctx, brAdapter); err != nil {
		return err
	}
	st.beadLedger = brAdapter
	st.beadResetter = brAdapter
	st.orphanStatusReader = brAdapter  // hk-mdus1 B3: in_progress guard reader
	st.beadCat3cCloser = brAdapter     // Cat 3c auto-reconciler (hk-lgtq2)
	st.intentGCLedger = brAdapter      // GCRetiredIntentsWithRedrive ledger (hk-cizvu)
	st.intentRedriveWriter = brAdapter // BI-031 step-4 re-drive (hk-aev8t)
	st.intentLogDir = lifecycle.BeadsIntentsDir(cfg.ProjectDir)
	return nil
}

// brVersionHandshake runs the BI-024a br --version handshake (hk-3pbox, hk-m6243):
// hk-m6243 + operator direction 2026-07-16: a version delta is a NOTICE (log +
// continue) — an expected, benign condition, not something wrong; only exec-failure
// or unparseable output is fatal (emits daemon_startup_failed, returns exit-code-8 error).
func (bs *bootState) brVersionHandshake(ctx context.Context, brAdapter *brcli.Adapter) error {
	versionErr := brAdapter.CheckBrVersion(ctx, release.BeadsVersion)
	if versionErr == nil {
		return nil
	}
	if errors.Is(versionErr, brcli.ErrBrVersionMismatch) {
		// Non-fatal, expected: br version differs from pin but br is usable.
		// A notice, not a warning — daemon continues.
		log.Printf("NOTICE: daemon.Start: br version differs from pin (BI-024a): %v — daemon continues normally; bump release.BeadsVersion when the fleet adopts a new br", versionErr)
		return nil
	}
	failedPayload := core.DaemonStartupFailedPayload{
		FailedAt:    time.Now().UTC().Format(time.RFC3339),
		ExitCode:    8,
		FailureMode: "br-version-incompatible",
	}
	if failedBytes, marshalErr := json.Marshal(failedPayload); marshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeDaemonStartupFailed, failedBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit daemon_startup_failed: %v", emitErr)
		}
	}
	return fmt.Errorf("daemon.Start: br --version handshake failed (BI-024a, exit code 8): %w", versionErr)
}

// loadQueueProvenance reads every named queue's queue.json (hk-2ty0g, widened to
// all queues by hk-nddg1) into the QueueOwned / QueueDispatched provenance sets for
// the orphan-sweep bead-reset. Non-fatal: enumerate/load errors yield partial (or
// empty) sets and the sweep falls back to intent-log provenance only (PL-006 sixth
// bullet).
func (bs *bootState) loadQueueProvenance(ctx context.Context, st *reconcileState) {
	// hk-nddg1: aggregate provenance across ALL named queues, not just main.
	// Dispatch happens from every named queue (queue.EnumerateQueueNames — see
	// LoadStartupQueues and the Class-B reconcile pass in reconciliationcadence),
	// so a bead dispatched via a crew queue (e.g. queues/paul.json) must contribute
	// to QueueOwned / QueueDispatched. Reading main.json alone let the orphan sweep
	// miss a crew-queue bead's dispatched-sentinel: SweepStaleInProgressBeads reset
	// it in_progress->open (the a-queue exclusion at orphansweepbeads.go did not
	// fire because the bead was absent from the main-only QueueDispatched set), and
	// LoadStartupQueues then re-dispatched it from its still-"dispatched" crew queue
	// = double-dispatch (the hk-2ty0g regression reintroduced for every non-main
	// queue). Non-fatal: enumerate/load errors fall back to whatever provenance was
	// gathered (or intent-log-only if none). EnumerateQueueNames includes main
	// (main.json lives in .harmonik/queues/), so this strictly widens coverage.
	names, enumErr := queue.EnumerateQueueNames(bs.cfg.ProjectDir)
	if enumErr != nil {
		log.Printf("warn: pre-sweep EnumerateQueueNames failed: %v — falling back to intent-log-only provenance", enumErr)
		return
	}
	if len(names) == 0 {
		return
	}
	st.queueDispatched = make(lifecycle.QueueDispatchedSet)
	st.queueOwned = make(lifecycle.QueueOwnedSet)
	for _, name := range names {
		rawQ, rawQErr := queue.Load(ctx, bs.cfg.ProjectDir, name)
		if rawQErr != nil || rawQ == nil {
			log.Printf("warn: pre-sweep queue.Load(%q) failed: %v — skipping this queue's provenance", name, rawQErr)
			continue
		}
		for gi := range rawQ.Groups {
			for _, item := range rawQ.Groups[gi].Items {
				st.queueOwned[item.BeadID] = struct{}{}
				if item.Status == queue.ItemStatusDispatched {
					st.queueDispatched[item.BeadID] = struct{}{}
				}
			}
		}
	}
}

// runOrphanSweepAndAdopt runs the orphan sweep, emits daemon_orphan_sweep_completed,
// adopts dead run-sessions, reconciles pre-restart in-flight runs, and emits the
// RC-020a reconciliation_started/completed markers. All steps are non-fatal
// (PL-006: never abort Start on sweep error). Runs BEFORE loadStartupQueues so
// QM-002a sees open (not in_progress) beads (QM-002a ordering, hk-o85ye).
func (bs *bootState) runOrphanSweepAndAdopt(ctx context.Context, daemonStartTime time.Time, st *reconcileState) {
	cfg := bs.cfg
	bus := bs.bus

	sweepResult, sweepErr := RunOrphanSweep(
		ctx,
		cfg.ProjectDir,
		st.projectHash,
		daemonStartTime,
		OrphanSweepConfig{
			BeadLedger:          st.beadLedger,
			BeadResetter:        st.beadResetter,
			BeadCat3cCloser:     st.beadCat3cCloser,
			IntentGCLedger:      st.intentGCLedger,
			IntentRedriveWriter: st.intentRedriveWriter, // BI-031 step-4 re-drive (hk-aev8t)
			// BeadProvenance: sentinel-file checker (hk-11xkn) — provenance when
			// all intent files have been cleared by prior crash-recovery runs.
			BeadProvenance: lifecycle.NewSentinelFileProvenanceChecker(
				lifecycle.BeadsOwnedDir(cfg.ProjectDir),
			),
			MergeCommitScanner: lifecycle.GitMergeCommitScanner{
				ProjectDir:   cfg.ProjectDir,
				TargetBranch: "", // defaults to "main" inside the scanner
			},
			IntentLogDir:       st.intentLogDir,
			DaemonStartNS:      daemonStartTime.UnixNano(),
			QueueDispatched:    st.queueDispatched,
			QueueOwned:         st.queueOwned,
			TmuxAdapter:        st.sweepTmuxAdapter, // hk-xb5yi: reap orphan windows from prior crash
			DaemonSpawnSession: st.daemonOwnSession, // hk-9vp51: never sweep the daemon's own session
		},
	)
	st.sweepResult = sweepResult

	// Build and emit daemon_orphan_sweep_completed (§8.7.14, O-class). Do NOT
	// abort Start on sweep error per PL-006.
	sweepPayloadBytes, sweepMarshalErr := json.Marshal(sweepResult.ToPayload())
	if sweepMarshalErr != nil {
		sweepPayloadBytes = []byte(`{}`)
	}
	if sweepEmitErr := bus.Emit(ctx, core.EventTypeDaemonOrphanSweepCompleted, sweepPayloadBytes); sweepEmitErr != nil {
		// Non-fatal: bus emit failure at this stage does not block startup.
		_ = sweepEmitErr
	}
	// Sweep errors are non-fatal (PL-006): recorded, never abort Start.
	_ = sweepErr

	// hk-o85ye: reset beads for bead-runs whose independent tmux sessions have
	// already exited. Must run before LoadQueueAtStartup (QM-002a). Non-fatal.
	adoptDeadRunSessions(
		ctx,
		cfg.ProjectDir,
		st.projectHash,
		daemonStartTime.UnixNano(),
		st.intentLogDir,
		st.sweepTmuxAdapter,
		st.beadResetter,
	)

	// Reconcile pre-restart in-flight runs (hk-r73qr / hk-iwu8a).
	bs.reconcileInFlightRuns(ctx, daemonStartTime, st)

	// RC-020a dispatch point (a): reconciliation_started + _completed markers.
	bs.emitReconciliationMarkers(ctx, sweepResult)
}

// reconcileInFlightRuns reconciles pre-restart in-flight runs: for any run with
// run_started but no terminal event, emit run_failed (hk-r73qr). Orphans are also
// sourced from the live dispatch-tracker (queueDispatched) so a bead crashed-on
// before any run_started still clears its dispatch-lock (hk-iwu8a); genuinely-live
// runs (in .harmonik/runs/) are excluded. Skipped when no JSONL log is configured.
func (bs *bootState) reconcileInFlightRuns(ctx context.Context, daemonStartTime time.Time, st *reconcileState) {
	cfg := bs.cfg
	if cfg.JSONLLogPath == "" {
		return
	}
	liveRunBeadIDs := make(map[core.BeadID]struct{})
	if liveRecs, liveErr := runpkg.List(cfg.ProjectDir); liveErr == nil {
		for _, rec := range liveRecs {
			if rec.BeadID != "" {
				liveRunBeadIDs[core.BeadID(rec.BeadID)] = struct{}{}
			}
		}
	}
	// hk-hju8n: snapshot the resettable-bead set in two bulk `br list` calls so the
	// reconcile does an O(1) map lookup per bead. On any bulk-list error the cache
	// is nil and we fall back to the per-bead reader.
	reconcileStatusReader := st.orphanStatusReader
	if lister, ok := st.orphanStatusReader.(bulkBeadLister); ok && lister != nil {
		if cached := newCachedOrphanStatusReader(ctx, lister); cached != nil {
			reconcileStatusReader = cached
		}
	}
	_ = reconcileOrphanedRunsOnResume(
		ctx,
		cfg.JSONLLogPath,
		bs.bus,
		st.beadResetter,
		reconcileStatusReader,
		st.intentLogDir,
		st.projectHash,
		daemonStartTime.UnixNano(),
		st.queueDispatched,
		liveRunBeadIDs,
	)
}

// emitReconciliationMarkers emits reconciliation_started{trigger:"startup"} then
// reconciliation_completed immediately after, so a hung startup reconciliation is
// detectable (F6/hk-mptxw, hk-63oh.21). Non-fatal.
func (bs *bootState) emitReconciliationMarkers(ctx context.Context, sweepResult OrphanSweepResult) {
	startupRunUID, startupUIDErr := uuid.NewV7()
	if startupUIDErr != nil {
		return
	}
	startupRunID := core.RunID(startupRunUID)
	startupRecPayload := core.ReconciliationStartedPayload{
		ReconciliationRunID: startupRunID,
		Trigger:             core.ReconciliationTriggerStartup,
	}
	if startupRecBytes, marshalErr := json.Marshal(startupRecPayload); marshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeReconciliationStarted, startupRecBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit reconciliation_started: %v", emitErr)
		}
	}
	startupCompPayload := core.ReconciliationCompletedPayload{
		ReconciliationRunID: startupRunID,
		Trigger:             core.ReconciliationTriggerStartup,
		BeadsExamined:       sweepResult.BeadInProgressReset + sweepResult.BeadCat3cClosed,
		BeadsClosed:         sweepResult.BeadCat3cClosed,
		BeadsReset:          sweepResult.BeadInProgressReset,
		CompletedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if startupCompBytes, marshalErr := json.Marshal(startupCompPayload); marshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeReconciliationCompleted, startupCompBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit reconciliation_completed: %v", emitErr)
		}
	}
}

// runCatBLSweeps runs the RC-020a Cat-BL1 (child-bead orphan) and Cat-BL3
// (merge-conflict-log audit) startup sweeps. Both are non-fatal and do not block
// daemon startup.
func (bs *bootState) runCatBLSweeps(ctx context.Context, resolvedTargetBranch string) {
	cfg := bs.cfg
	bus := bs.bus

	if err := RunCatBL1StartupSweep(ctx, CatBL1StartupSweepConfig{
		ProjectDir:   cfg.ProjectDir,
		BrPath:       cfg.BrPath,
		TargetBranch: resolvedTargetBranch,
		Emitter:      bus,
	}); err != nil {
		log.Printf("warn: daemon.Start: Cat-BL1 startup sweep (non-fatal): %v", err)
	}

	if err := RunCatBL3StartupSweep(ctx, CatBL3StartupSweepConfig{
		ProjectDir: cfg.ProjectDir,
		Emitter:    bus,
	}); err != nil {
		log.Printf("warn: daemon.Start: Cat-BL3 startup sweep (non-fatal): %v", err)
	}
}
