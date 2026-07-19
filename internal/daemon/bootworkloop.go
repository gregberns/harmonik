package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/eventbus"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/sentinel"
	"github.com/gregberns/harmonik/internal/workers"
)

// launchWorkLoop performs PL-005 step 4 P13: build + inject the work-loop deps,
// start the background loops (quiesce arbiter, crew/branch reapers, reconciliation
// scheduler, worker-report poll), wire the StaleWatcher force-reap seams, then run
// the work loop and block until ctx cancels or it exits. Skipped when BrPath is
// unset (unit-test mode). Split into sub-helpers, each under the funlen/cyclop
// ceilings. Extracted for giant-retirement boot-config B6.
func (bs *bootState) launchWorkLoop(ctx context.Context, daemonStartTime time.Time, bootBackoffDelay time.Duration, workflowModeDefault core.WorkflowMode) error {
	if bs.cfg.BrPath == "" {
		return nil
	}

	deps, depsErr := bs.buildWorkLoopDeps(ctx, daemonStartTime, workflowModeDefault)
	if depsErr != nil {
		return depsErr
	}
	if injectErr := bs.injectWorkLoopDeps(ctx, &deps, bootBackoffDelay); injectErr != nil {
		return injectErr
	}
	bs.startBackgroundLoops(ctx, &deps)
	bs.wireStaleWatcherReapSeams(ctx, &deps)

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- runWorkLoop(ctx, deps)
	}()
	// Block until the work loop exits (either ctx cancelled or fatal error).
	<-loopDone
	return nil
}

// buildWorkLoopDeps constructs the work-loop deps (newWorkLoopDeps), initialises
// the sentinel governor deps from config (FW1/FW2, hk-y9fn/hk-z1lr), and boot-seeds
// emittedEpics (C1, hk-o50hy) + the follow-up ledger (AC1, hk-3ndb) from the
// durable log so a restart does not re-emit. Governor-config errors are fatal only
// when the operator actually has a .harmonik/config.yaml.
func (bs *bootState) buildWorkLoopDeps(ctx context.Context, daemonStartTime time.Time, workflowModeDefault core.WorkflowMode) (workLoopDeps, error) {
	cfg := bs.cfg

	deps, depsErr := newWorkLoopDeps(ctx, cfg, bs.bus, workflowModeDefault, bs.adapterReg, bs.hookStore)
	if depsErr != nil {
		return workLoopDeps{}, fmt.Errorf("daemon.Start: work loop deps: %w", depsErr)
	}

	// FW1/FW2 (hk-y9fn/hk-z1lr): init sentinel governor deps from config.
	if govErr := bs.seedGovernorDeps(&deps, daemonStartTime); govErr != nil {
		return workLoopDeps{}, govErr
	}

	// C1 boot-seed (hk-o50hy): populate emittedEpics from the durable event log so
	// a restart does not re-emit epic_completed for an already-completed epic (AC-5).
	if cfg.JSONLLogPath != "" {
		deps.emittedEpics = scanEmittedEpics(cfg.JSONLLogPath)
		deps.emittedEpicsMu = &sync.Mutex{}
	}

	// AC1 boot-seed (hk-3ndb): load the durable follow-up ledger so a restart does
	// not re-emit staged beads already created in a prior session. Non-fatal.
	if deps.followUpLedgerPath != "" {
		if ledger, loadErr := loadFollowUpLedger(deps.followUpLedgerPath); loadErr != nil {
			log.Printf("warn: daemon.Start: load follow-up ledger: %v", loadErr)
		} else {
			deps.followUpLedger = ledger
		}
	}

	return deps, nil
}

// seedGovernorDeps initialises the sentinel governor deps from config (FW1
// hk-y9fn / FW2 hk-z1lr). hk-drygf (FIX-B): a missing liveness_no_progress_n key
// (or read error) fails the load loud ONLY when the operator has a config.yaml;
// absence means "sentinel not configured", not "misconfigured".
func (bs *bootState) seedGovernorDeps(deps *workLoopDeps, daemonStartTime time.Time) error {
	cfg := bs.cfg
	if cfg.ProjectDir == "" {
		return nil
	}
	sentinelCfg, sentinelErr := digest.LoadSentinelConfig(cfg.ProjectDir)
	if sentinelErr != nil {
		return fmt.Errorf("daemon.Start: sentinel config: %w", sentinelErr)
	}
	governorCfg, govErr := sentinelCfg.GovernorConfig()
	if govErr != nil {
		configPath := filepath.Join(cfg.ProjectDir, ".harmonik", "config.yaml")
		if _, statErr := os.Stat(configPath); statErr == nil {
			return fmt.Errorf("daemon.Start: governor config: %w", govErr)
		}
		// No config.yaml: leave governorCfg zero-valued (gate disabled).
	}
	deps.governorCfg = governorCfg
	deps.governorState = &sentinel.GovernorState{DaemonStartedAt: daemonStartTime}
	deps.sentinelMode = sentinelCfg.Mode
	deps.sentinelPhase2Classes = sentinelCfg.Phase2Classes()
	return nil
}

// scanEmittedEpics reads the durable event log and returns the set of epic IDs
// that already emitted epic_completed, so a restart does not re-emit (C1, AC-5,
// hk-o50hy). Extracted from buildWorkLoopDeps for giant-retirement boot-config B6.
func scanEmittedEpics(jsonlLogPath string) map[core.BeadID]struct{} {
	seed := make(map[core.BeadID]struct{})
	for ev := range eventbus.ScanAfter(jsonlLogPath, core.EventID{}) {
		if core.EventType(ev.Type) != core.EventTypeEpicCompleted {
			continue
		}
		var pl core.EpicCompletedPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil || !pl.Valid() {
			continue
		}
		seed[pl.EpicID] = struct{}{}
	}
	return seed
}

// injectWorkLoopDeps wires the shared singletons + config toggles into the work
// loop deps: the queue store + wake channel, the schedule store (codename:schedule,
// hk-0es), the crew handler, the pause/decision/concurrency controllers, the live
// worker-toggle (hk-xjbvi), the shared RunRegistry, the test-only overrides, and
// the post-boot spawn-substrate readiness gate (hk-bk33). A present-but-unparseable
// schedule file is fatal.
func (bs *bootState) injectWorkLoopDeps(ctx context.Context, deps *workLoopDeps, bootBackoffDelay time.Duration) error {
	cfg := bs.cfg

	// Queue store + submit-wake channel (QM-060; hk-24xn1).
	deps.queueStore = bs.qs
	deps.submitWakeC = bs.qs.WakeCh()

	// Recurring-job surface (codename:schedule, hk-0es). A present-but-unparseable
	// file is fatal; an absent file is a normal empty store.
	scheduleStore := schedule.NewStore(cfg.ProjectDir)
	if loadErr := scheduleStore.Load(); loadErr != nil {
		return fmt.Errorf("daemon.Start: load schedule store: %w", loadErr)
	}
	ensureOpsMonitorSchedule(scheduleStore, cfg.ProjectCfg.Opsmonitor)
	ensureCtxWatchdogSchedule(scheduleStore, cfg.ProjectCfg.Watchdog.Enabled)
	ensureWatchLivenessSchedule(scheduleStore, cfg.ProjectCfg.Watch, deps.daemonBinaryPath)
	deps.scheduleStore = scheduleStore
	deps.scheduleWakeC = scheduleStore.WakeCh()
	// `harmonik sleep` suspends enabled jobs; `wake --all` restores them (hk-xjr1n).
	bs.quiesceArbiter.SetScheduleStore(scheduleStore)
	deps.crewHandler = bs.crewHandler // may be nil in unit-test mode (no socket)
	deps.commsWhoQuerier = shellCommsWho(deps.daemonBinaryPath, cfg.ProjectDir)
	deps.commsSend = shellCommsSend(deps.daemonBinaryPath, cfg.ProjectDir)

	// Dispatcher skip-on-paused gate (hk-kac8g): nil → gate disabled.
	deps.handlerPauseController = cfg.HandlerPauseController
	// harmonik run <bead-id> drain/exit cancels (hk-icecw, hk-8jh26 Fix 1).
	deps.cancelOnQueueDrain = cfg.CancelOnQueueDrain
	deps.cancelOnQueueExit = cfg.CancelOnQueueExit
	// Stop-dispatch context (hk-2o2i9): nil falls back to ctx.
	deps.stopDispatchCtx = cfg.StopDispatchCtx
	// HandlerPauseController for the dispatch gate (hk-m0k0a); overrides the
	// cfg-supplied value above with the daemon-owned controller.
	deps.handlerPauseController = bs.handlerPauseCtrl
	// OperatorPauseController br-ready dispatch gate (hk-ry8q1); nil in unit-test mode.
	deps.operatorPauseCtrl = bs.opPauseCtrl
	// DecisionBlocker dispatch gate (EV-043, EV-043a; hk-pbmsq).
	deps.decisionBlocker = bs.decisionBlocker
	// ConcurrencyController live ceiling (hk-ohiaf); nil falls back to the static field.
	deps.concurrencyCtrl = bs.concurrencyCtrl

	// Live worker enable/disable toggle (hk-xjbvi): the closure captures the SAME
	// registry the dispatch path reads via SelectWorker.
	if bs.queueHandlerAdapter != nil {
		workerReg := deps.workerRegistry
		bs.queueHandlerAdapter.SetWorkerToggleFunc(func(name string, enabled bool) (string, error) {
			if workerReg == nil {
				return "", fmt.Errorf("no such worker %q: no remote worker configured (.harmonik/workers.yaml is empty)", name)
			}
			return workerReg.SetEnabledByName(name, enabled)
		})
	}

	// Shared RunRegistry so the work loop + policy goroutine share one snapshot (hk-37zy8).
	deps.runRegistry = bs.sharedRunRegistry

	// Test-only overrides (WithWorktreeFactory / WithMergeQueue).
	if bs.hooks.worktreeFactory != nil {
		deps.worktreeFactory = bs.hooks.worktreeFactory
	}
	// Inject the test-only merge-queue override when set via WithMergeQueue
	// (RSM-015, 9eceafc0). Nil (the default) keeps production's own queue from
	// newWorkLoopDeps/runWorkLoop, so production merges stay serialised through
	// the exclusion domain.
	if bs.hooks.mergeQ != nil {
		deps.mergeQ = bs.hooks.mergeQ
	}

	// hk-bk33: spawn-substrate readiness gate for post-boot re-dispatch. runWorkLoop
	// waits on this channel before the first dispatch tick after a restart-backoff.
	if bootBackoffDelay > 0 {
		if prober, ok := cfg.Substrate.(substrateSpawnReadier); ok {
			readyCh := make(chan struct{})
			go func() {
				defer close(readyCh)
				if probeErr := prober.ProbeSpawnReady(ctx); probeErr != nil {
					log.Printf("warn: daemon.Start: spawn-substrate readiness probe (non-fatal): %v", probeErr)
				}
			}()
			deps.spawnSubstrateReadyCh = readyCh
		}
	}

	return nil
}

// startBackgroundLoops starts the post-Seal background goroutines: the quiesce
// arbiter, the idle-crew reaper (SD-3, hk-s2eac), the periodic branch reaper
// (hk-2i36s), the scheduled reconciliation detector (RC-020a, hk-63oh.21), and the
// recurring worker-report poll (WR3, hk-jn3u). It also emits the composition-root
// wiring audit log (HARMONIK_DEBUG_WIRING=1, hk-4mupj).
func (bs *bootState) startBackgroundLoops(ctx context.Context, deps *workLoopDeps) {
	cfg := bs.cfg

	bs.quiesceArbiter.Start(ctx)
	bs.crewIdleReaper.StartWatcher(ctx)
	bs.branchReapWatcher.StartWatcher(ctx)

	// All 31 wiring points are established at this point; the audit log is a stable
	// diff surface for catching silent drops between daemon versions.
	logCompositionRoot(cfg.LogWriter)

	// RC-020a dispatch point (c): scheduled detector cadence (default 1h).
	StartReconciliationScheduler(ctx, ReconciliationSchedulerConfig{
		ProjectDir:   cfg.ProjectDir,
		BrPath:       cfg.BrPath,
		TargetBranch: "", // defaults to "main" inside the scheduler
		Interval:     cfg.ReconciliationScanCadence,
		Emitter:      bs.bus,
		LogWriter:    cfg.LogWriter,
	})

	// WR3 (hk-jn3u): recurring worker-report poll. Phase-1 observability only;
	// off-by-default when no worker is enabled (byte-identical with no workers.yaml).
	var reportEmit workers.EmitFunc
	if bs.bus != nil {
		reportEmit = bs.bus.Emit
	}
	go workers.RunReportLoop(ctx, cfg.Workers, deps.workerRegistry, workers.ProductionRunnerForWorker, reportEmit)
}

// wireStaleWatcherReapSeams wires the StaleWatcher force-reap watchdog seams
// (hk-mdus1) now that deps (queueStore, emitter) is fully built. Two-phase because
// the watcher was constructed + started (StartWatcher) far earlier, before
// workLoopDeps existed.
func (bs *bootState) wireStaleWatcherReapSeams(ctx context.Context, deps *workLoopDeps) {
	cfg := bs.cfg

	// ForceReap: on a wedged run's force-Unregister, emit a terminal run_failed and
	// drive the owning queue item terminal so the group advances.
	bs.staleWatcher.SetForceReap(func(runID core.RunID, handle *RunHandle) {
		emitRunCompleted(ctx, bs.bus, runID, string(handle.BeadID), handle.OwningEpicID, handle.OwningEpicAssignee, false,
			"force-reaped: run wedged past cancel grace; concurrency slot reclaimed (hk-mdus1)",
			handle.QueueID, handle.QueueGroupIndex, nil)
		if handle.QueueName != "" && handle.QueueID != nil && handle.QueueGroupIndex != nil && handle.QueueItemIndex >= 0 {
			evaluateGroupAdvanceWithOutcome(ctx, *deps, handle.QueueName, *handle.QueueID, *handle.QueueGroupIndex, handle.QueueItemIndex, false)
		}
	})

	// RunProcessDead: fast dead-process reap probe via the substrate #{pane_pid}
	// liveness. Best-effort: any lookup error → "not dead" (never a spurious reap).
	sa, ok := cfg.Substrate.(substrateWithAdapter)
	if !ok {
		return
	}
	reapAdapter := sa.tmuxAdapter()
	if reapAdapter == nil || cfg.ProjectDir == "" {
		return
	}
	bs.staleWatcher.SetRunProcessDead(func(runID core.RunID, _ *RunHandle) bool {
		return bs.probeRunProcessDead(ctx, reapAdapter, runID)
	})
}

// probeRunProcessDead reports whether the tmux session backing runID has a dead
// (or gone) pane PID, resolving the session from the .harmonik/runs/ record.
// Best-effort: any lookup error → false (never a spurious reap). Extracted from
// wireStaleWatcherReapSeams for giant-retirement boot-config (B6 complexity).
func (bs *bootState) probeRunProcessDead(ctx context.Context, reapAdapter ltmux.Adapter, runID core.RunID) bool {
	recs, listErr := runpkg.List(bs.cfg.ProjectDir)
	if listErr != nil {
		return false
	}
	for _, r := range recs {
		if r.RunID != runID.String() {
			continue
		}
		if r.SessionName == "" {
			return false
		}
		pid, pidErr := reapAdapter.WindowPanePID(ctx, ltmux.WindowHandle(r.SessionName+":"))
		if pidErr != nil {
			return false
		}
		if pid == 0 {
			return true
		}
		return processDead(pid)
	}
	return false
}
