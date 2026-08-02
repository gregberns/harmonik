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
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
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

	deps, depsErr := bs.buildWorkLoopDeps(ctx, workflowModeDefault)
	if depsErr != nil {
		return depsErr
	}
	governor, governorEnabled, governorErr := newGovernorPort(bs.cfg, daemonStartTime)
	if governorErr != nil {
		return governorErr
	}
	coordinatorReap := newCoordinatorReapPort(bs.cfg)
	eagerRefill := newEagerRefillPort(bs.cfg)
	loadEagerRefillLedger(&eagerRefill)                    //nolint:contextcheck // The retained ledger helper is path-only.
	scheduleStore, scheduleErr := newScheduleStore(bs.cfg) //nolint:contextcheck // Schedule registration is a bootstrap file mutation with no context-aware API.
	if scheduleErr != nil {
		return scheduleErr
	}
	if injectErr := bs.injectWorkLoopDeps(ctx, &deps, scheduleStore, bootBackoffDelay); injectErr != nil {
		return injectErr
	}
	bs.startBackgroundLoops(ctx, &deps)
	bs.wireStaleWatcherReapSeams(ctx, &deps, eagerRefill)

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- runWorkLoop(ctx, deps, coordinatorReap, eagerRefill, governor, governorEnabled)
	}()
	// Block until the work loop exits (either ctx cancelled or fatal error).
	<-loopDone
	return nil
}

func loadEagerRefillLedger(port *eagerRefillPort) {
	if port.followUpLedgerPath == "" {
		return
	}
	ledger, loadErr := loadFollowUpLedger(port.followUpLedgerPath)
	if loadErr != nil {
		log.Printf("warn: daemon.Start: load follow-up ledger: %v", loadErr)
		return
	}
	port.followUpLedger = ledger
}

// newCoordinatorReapPort builds the periodic coordinator-session reaper's
// dependencies from daemon config. An absent tmux substrate leaves adapter nil,
// which keeps the periodic reaper disabled.
func newCoordinatorReapPort(cfg Config) coordinatorReapPort {
	var adapter ltmux.Adapter
	if substrate, ok := cfg.Substrate.(substrateWithAdapter); ok {
		adapter = substrate.tmuxAdapter()
	}
	return coordinatorReapPort{
		projectDir:  cfg.ProjectDir,
		projectHash: lifecycle.ComputeProjectHash(cfg.ProjectDir),
		adapter:     adapter,
	}
}

// buildWorkLoopDeps constructs the work-loop deps (newWorkLoopDeps) and
// boot-seeds emittedEpics (C1, hk-o50hy) from the durable log so a restart does
// not re-emit.
func (bs *bootState) buildWorkLoopDeps(ctx context.Context, workflowModeDefault core.WorkflowMode) (workLoopDeps, error) {
	cfg := bs.cfg

	deps, depsErr := newWorkLoopDeps(ctx, cfg, bs.bus, workflowModeDefault, bs.adapterReg, bs.hookStore)
	if depsErr != nil {
		return workLoopDeps{}, fmt.Errorf("daemon.Start: work loop deps: %w", depsErr)
	}

	// C1 boot-seed (hk-o50hy): populate emittedEpics from the durable event log so
	// a restart does not re-emit epic_completed for an already-completed epic (AC-5).
	if cfg.JSONLLogPath != "" {
		deps.emittedEpics = scanEmittedEpics(cfg.JSONLLogPath)
		deps.emittedEpicsMu = &sync.Mutex{}
	}

	return deps, nil
}

// newGovernorPort constructs the governor's configuration and mutable state.
// The enabled result reads only the movement-governor subsystem switch. A
// missing config file still returns an enabled port with zero thresholds, so the
// observe pass remains active while the liveness gate stays disabled.
func newGovernorPort(cfg Config, daemonStartTime time.Time) (governorPort, bool, error) {
	enabled := cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemMovementGovernor)
	if !enabled {
		return governorPort{}, false, nil
	}

	port := governorPort{state: &sentinel.GovernorState{DaemonStartedAt: daemonStartTime}}
	if cfg.ProjectDir == "" {
		return port, true, nil
	}

	sentinelCfg, sentinelErr := digest.LoadSentinelConfig(cfg.ProjectDir)
	if sentinelErr != nil {
		return governorPort{}, true, fmt.Errorf("daemon.Start: sentinel config: %w", sentinelErr)
	}
	governorCfg, govErr := sentinelCfg.GovernorConfig()
	if govErr != nil {
		configPath := filepath.Join(cfg.ProjectDir, ".harmonik", "config.yaml")
		if _, statErr := os.Stat(configPath); statErr == nil {
			return governorPort{}, true, fmt.Errorf("daemon.Start: governor config: %w", govErr)
		}
		// No config.yaml: leave governorCfg zero-valued (gate disabled).
	}
	port.config = governorCfg
	port.mode = sentinelCfg.Mode
	port.phase2Classes = sentinelCfg.Phase2Classes()
	return port, true, nil
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

// newScheduleStore builds the recurring-job store and registers the daemon-owned
// jobs. An absent file is a normal empty store. A present-but-unparseable file is
// fatal so the operator can inspect it.
func newScheduleStore(cfg Config) (*schedule.Store, error) {
	store := schedule.NewStore(cfg.ProjectDir)
	if loadErr := store.Load(); loadErr != nil {
		return nil, fmt.Errorf("daemon.Start: load schedule store: %w", loadErr)
	}
	ensureOpsMonitorSchedule(store, cfg.ProjectCfg.Opsmonitor)
	ensureCtxWatchdogSchedule(store, cfg.ProjectCfg.Watchdog.Enabled)
	// ensureWatchLivenessSchedule declares its third parameter as `_ string` in
	// watch_liveness_schedule.go, so it cannot affect registration. Keep an
	// explicit empty value.
	ensureWatchLivenessSchedule(store, cfg.ProjectCfg.Watch, "")
	return store, nil
}

// injectWorkLoopDeps wires the shared singletons + config toggles into the work
// loop deps: the queue store + wake channel, the loaded schedule store
// (codename:schedule, hk-0es), the crew handler, the pause/decision/concurrency
// controllers, the live worker-toggle (hk-xjbvi), the shared RunRegistry, the
// test-only overrides, and the post-boot spawn-substrate readiness gate (hk-bk33).
func (bs *bootState) injectWorkLoopDeps(ctx context.Context, deps *workLoopDeps, scheduleStore *schedule.Store, bootBackoffDelay time.Duration) error {
	cfg := bs.cfg

	// Queue store + submit-wake channel (QM-060; hk-24xn1).
	deps.queueStore = bs.qs
	deps.submitWakeC = bs.qs.WakeCh()

	// Recurring-job surface (codename:schedule, hk-0es). The composition root
	// supplies the loaded store to both the work loop and the quiesce arbiter.
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
	// Both reapers are constructed in buildCommsAndCrewHandlers, under the
	// socket-listener switch AND their own (crew_idle_reap, branch_reaper), so
	// either can be nil here. Guarded at the call site rather than inside the
	// watchers: absence is a composition-root fact, and neither type has any
	// business pretending a nil watcher is a watcher.
	//
	// BranchReapWatcher.StartWatcher is the sharp one — it spawns loop, which
	// dereferences w.cfg.ScanInterval immediately and would take the whole daemon
	// down from a goroutine. CrewIdleReaper.StartWatcher currently tolerates a nil
	// receiver, but only because its body happens to be empty (the operator's
	// 2026-07-18 disable of the sweep); that is a property of the sweep being
	// inert, not of the type, so it is guarded on the same terms.
	if bs.crewIdleReaper != nil {
		bs.crewIdleReaper.StartWatcher(ctx)
	}
	if bs.branchReapWatcher != nil {
		bs.branchReapWatcher.StartWatcher(ctx)
	}

	// Every composition-root singleton is established at this point; the audit
	// log reads them off the live bootState, so it is a stable diff surface for
	// catching silent drops between daemon versions.
	bs.logCompositionRoot(ctx, cfg.LogWriter)

	// RC-020a dispatch point (c): scheduled detector cadence (default 1h),
	// subject to subsystem partitioning (see startReconciliationSchedulerIfEnabled).
	startReconciliationSchedulerIfEnabled(ctx, cfg.ProjectCfg, ReconciliationSchedulerConfig{
		ProjectDir:   cfg.ProjectDir,
		BrPath:       cfg.BrPath,
		TargetBranch: "", // defaults to "main" inside the scheduler
		Interval:     cfg.ReconciliationScanCadence,
		Emitter:      bs.bus,
		LogWriter:    cfg.LogWriter,
	})

	// WR3 (hk-jn3u): recurring worker-report poll, subject to subsystem
	// partitioning (see startWorkerReportLoopIfEnabled).
	bs.startWorkerReportLoopIfEnabled(ctx, deps.workerRegistry)
}

// startWorkerReportLoopIfEnabled applies subsystem partitioning to the WR3
// worker-report poll: it is the ONE construction seam for that goroutine.
//
// When `subsystems.worker_report_loop.enabled: false` is set, the goroutine is
// never spawned. RunReportLoop already returns immediately when no worker in
// .harmonik/workers.yaml is enabled, but the daemon has to START it to find that
// out; this decides it at the composition root instead, so "off" is absent rather
// than a goroutine that exists just long enough to disagree.
//
// Absent config enables the loop, so a deployment without a subsystems: block
// behaves exactly as it did before the block existed.
//
// Returns true when the goroutine was spawned. The return value is the observable
// decision; the production caller ignores it.
func (bs *bootState) startWorkerReportLoopIfEnabled(ctx context.Context, reg *workers.Registry) bool {
	cfg := bs.cfg
	if !cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemWorkerReportLoop) {
		bs.logSubsystemDisabled(projectconfig.SubsystemWorkerReportLoop, "worker-report poll loop not constructed")
		return false
	}
	var reportEmit workers.EmitFunc
	if bs.bus != nil {
		reportEmit = bs.bus.Emit
	}
	go workers.RunReportLoop(ctx, cfg.Workers, reg, workers.ProductionRunnerForWorker, reportEmit)
	return true
}

// wireStaleWatcherReapSeams wires the StaleWatcher force-reap watchdog seams
// (hk-mdus1) now that deps (queueStore, emitter) is fully built. Two-phase because
// the watcher was constructed + started (StartWatcher) far earlier, before
// workLoopDeps existed.
func (bs *bootState) wireStaleWatcherReapSeams(ctx context.Context, deps *workLoopDeps, eagerRefill eagerRefillPort) {
	cfg := bs.cfg
	reapPort := newReapSeamPort(*deps, eagerRefill)

	// ForceReap: on a wedged run's force-Unregister, emit a terminal run_failed and
	// drive the owning queue item terminal so the group advances.
	bs.staleWatcher.SetForceReap(func(runID core.RunID, handle *RunHandle) {
		emitRunCompleted(ctx, bs.bus, runID, string(handle.BeadID), handle.OwningEpicID, handle.OwningEpicAssignee, false,
			"force-reaped: run wedged past cancel grace; concurrency slot reclaimed (hk-mdus1)",
			handle.QueueID, handle.QueueGroupIndex, nil)
		if handle.QueueName != "" && handle.QueueID != nil && handle.QueueGroupIndex != nil && handle.QueueItemIndex >= 0 {
			evaluateGroupAdvanceWithOutcome(ctx, reapPort, handle.QueueName, *handle.QueueID, *handle.QueueGroupIndex, handle.QueueItemIndex, false)
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
