package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon/bootconfig"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queuewiring"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/sentinel"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workers"
)

//nolint:gocognit,cyclop,funlen // Pre-existing boot wiring complexity; grouping the work-loop inputs does not add a decision.
func (bs *bootState) launchWorkLoop(ctx context.Context, daemonStartTime time.Time, bootBackoffDelay time.Duration, workflowModeDefault core.WorkflowMode) error {
	if bs.cfg.BrPath == "" {
		return nil
	}

	cfg := bs.cfg
	if cfg.ProjectDir == "" {
		return fmt.Errorf("daemon.Start: work loop deps: Config.ProjectDir is empty; required for worktree creation")
	}
	if bs.adapterReg == nil {
		return fmt.Errorf("daemon.Start: work loop deps: adapterRegistry is nil; required by waitAgentReady")
	}
	ledger, ledgerErr := brcli.NewForProject(cfg.BrPath, cfg.ProjectDir)
	if ledgerErr != nil {
		return fmt.Errorf("daemon.Start: work loop deps: brcli.NewForProject: %w", ledgerErr)
	}
	harnessRegistry, harnessErr := newHarnessRegistry(cfg.ProjectCfg.Harnesses.Pi)
	if harnessErr != nil {
		return fmt.Errorf("daemon.Start: work loop deps: newHarnessRegistry: %w", harnessErr)
	}
	if baseURL := cfg.ProjectCfg.Harnesses.Pi.BaseURL; baseURL != "" {
		if probeErr := pi.ProbeBaseURL(ctx, baseURL, 3*time.Second); probeErr != nil {
			log.Printf("warn: daemon.Start: %v", probeErr)
		}
	}
	if err := bs.ensureWorkerRegistry(ctx); err != nil {
		return err
	}
	workerRegistry := bs.workerRegistry
	handlerBinary := cfg.HandlerBinary
	if handlerBinary == "" {
		handlerBinary = "claude"
	}
	daemonBinaryPath := cfg.DaemonBinaryPath
	if daemonBinaryPath == "" {
		daemonBinaryPath = "harmonik"
	}
	projectHash := lifecycle.ComputeProjectHash(cfg.ProjectDir)
	handlerEnv := append([]string{lifecycle.ProvenanceEnvVar(projectHash)}, cfg.HandlerEnv...)
	intentLogDir := lifecycle.BeadsIntentsDir(cfg.ProjectDir)
	targetBranch := bootconfig.ResolveTargetBranch(cfg.TargetBranch)
	runRegistry := bs.sharedRunRegistry
	if runRegistry == nil {
		runRegistry = newLocalRunRegistry()
	}
	localInFlight := new(atomic.Int32)
	worktreeCreateMu := &sync.Mutex{}
	agentSpawnSem := make(chan struct{}, 3)
	emittedEpics := make(map[core.BeadID]struct{})
	if cfg.JSONLLogPath != "" {
		emittedEpics = scanEmittedEpics(cfg.JSONLLogPath)
	}
	emittedEpicsMu := &sync.Mutex{}
	var injectedWorktreeFactory func(context.Context, string, string, string) (string, func(), error)
	if bs.hooks.worktreeFactory != nil {
		injectedWorktreeFactory = bs.hooks.worktreeFactory
	}
	mergeQueue := bs.hooks.mergeQ
	baseEnv := runloop.RunEnv{ProjectDir: cfg.ProjectDir, TargetBranch: targetBranch, BrPath: cfg.BrPath, ProtectBranches: cfg.ProtectBranches, AllowedRepos: cfg.ProjectCfg.Daemon.AllowedRepos, WorkflowModeDefault: workflowModeDefault, DefaultHarness: cfg.DefaultHarness, ProjectCfg: cfg.ProjectCfg, HandlerBinary: handlerBinary, HandlerArgs: cfg.HandlerArgs, HandlerEnv: handlerEnv, DaemonBinaryPath: daemonBinaryPath, IntentLogDir: intentLogDir, AgentReadyTimeout: cfg.AgentReadyTimeout, RemoteAgentReadyTimeout: cfg.RemoteAgentReadyTimeout, SandboxCfg: cfg.ProjectCfg.Sandbox, BrTimeoutCfg: brcli.TimeoutConfig{}}
	basePorts := newStaticRunPorts(ledger, bs.bus, intentLogDir, baseEnv.BrTimeoutCfg, cfg.ProjectDir, cfg.SkipBrHistoryRotation, mergeQueue, cfg.CPRegistry, substrate.SystemClock{})
	handles := newSharedHandles(runRegistry, localInFlight, agentSpawnSem, workerRegistry, bs.qs, cfg.ProjectDir, harnessRegistry, bs.adapterReg, bs.hookStore, cfg.Substrate, cfg.ReviewerSubstrate, core.NewTransitionIDGenerator(), emittedEpics, emittedEpicsMu, ledger, cfg.Runner, injectedWorktreeFactory, worktreeCreateMu)
	handles.StallFeed = bs.stallFeed
	lifecyclePort := newLoopLifecyclePort(bs.cfg)
	ledgerRepair := newLedgerRepairPort(ledger, cfg.ProjectDir)
	governor, governorEnabled, governorErr := newGovernorPort(bs.cfg, daemonStartTime)
	if governorErr != nil {
		return governorErr
	}
	coordinatorReap := newCoordinatorReapPort(bs.cfg)
	diskReclaim := newDiskReclaimPort(cfg.ProjectDir, bs.bus, runRegistry)
	eagerRefill := newEagerRefillPort(bs.cfg)
	loadEagerRefillLedger(&eagerRefill)                    //nolint:contextcheck // The retained ledger helper is path-only.
	scheduleStore, scheduleErr := newScheduleStore(bs.cfg) //nolint:contextcheck // Schedule registration is a bootstrap file mutation with no context-aware API.
	if scheduleErr != nil {
		return scheduleErr
	}
	if bs.queueHandlerAdapter != nil {
		bs.queueHandlerAdapter.SetWorkerToggleFunc(func(name string, enabled bool) (string, error) {
			if workerRegistry == nil {
				return "", fmt.Errorf("no such worker %q: no remote worker configured (.harmonik/workers.yaml is empty)", name)
			}
			return workerRegistry.SetEnabledByName(name, enabled)
		})
	}
	if bootBackoffDelay > 0 {
		if prober, ok := cfg.Substrate.(substrateSpawnReadier); ok {
			readyCh := make(chan struct{})
			go func() {
				defer close(readyCh)
				if probeErr := prober.ProbeSpawnReady(ctx); probeErr != nil {
					log.Printf("warn: daemon.Start: spawn-substrate readiness probe (non-fatal): %v", probeErr)
				}
			}()
			lifecyclePort.spawnSubstrateReadyCh = readyCh
		}
	}
	maxConcurrent := bs.cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	capacity := newCapacityPort(maxConcurrent, bs.concurrencyCtrl)
	queueSurface := newQueueSurfacePort(bs.qs.WakeCh(), queuewiring.NewBRQueueLedger(ledger))
	queueSurface.completionGC = bs.qs
	queueSurface.projectDir = cfg.ProjectDir
	dispatchGates := newDispatchGatesPort(bs.bus, bs.handlerPauseCtrl, bs.opPauseCtrl, bs.decisionBlocker)
	schedulePort := newSchedulePort(daemonBinaryPath, cfg.ProjectDir, handlerEnv, scheduleStore, bs.crewHandler)
	bs.quiesceArbiter.SetScheduleStore(schedulePort.store)
	bs.startBackgroundLoops(ctx, workerRegistry)
	bs.wireStaleWatcherReapSeams(ctx, bs.bus, cfg.ProjectDir, targetBranch, bs.qs, runRegistry, lifecyclePort, capacity, queueSurface, eagerRefill)

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- runWorkLoop(ctx, workLoopInput{
			baseEnv: baseEnv, basePorts: basePorts, handles: handles, ledger: ledger,
			queueStore: bs.qs, runRegistry: runRegistry, substrate: cfg.Substrate, mergeQueue: mergeQueue,
		}, loopCollaborators{
			lifecycle: lifecyclePort, ledgerRepair: ledgerRepair, schedule: schedulePort,
			coordinatorReap: coordinatorReap, diskReclaim: diskReclaim, eagerRefill: eagerRefill,
			governor: governor, governorEnabled: governorEnabled, capacity: capacity,
			queueSurface: queueSurface, dispatchGates: dispatchGates,
		}, cfg.NoAutoPull)
	}()
	<-loopDone
	return nil
}

func (bs *bootState) ensureWorkerRegistry(ctx context.Context) error {
	if bs.workerRegistryBuilt {
		return nil
	}
	var emit workers.EmitFunc
	if bs.bus != nil {
		emit = bs.bus.Emit
	}
	bs.workerRegistry = workers.BuildRegistry(ctx, bs.cfg.Workers, emit)
	bs.workerRegistryBuilt = true
	if bs.cfg.WorkerRegistryObserver != nil {
		bs.cfg.WorkerRegistryObserver(bs.workerRegistry)
	}
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
	}
	port.config = governorCfg
	port.mode = sentinelCfg.Mode
	port.phase2Classes = sentinelCfg.Phase2Classes()
	return port, true, nil
}

func scanEmittedEpics(jsonlLogPath string) map[core.BeadID]struct{} {
	seed := make(map[core.BeadID]struct{})
	for ev := range eventbus.ScanAfter(jsonlLogPath, core.EventID{}) {
		if ev.Type != core.EventTypeEpicCompleted {
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

func newScheduleStore(cfg Config) (*schedule.Store, error) {
	store := schedule.NewStore(cfg.ProjectDir)
	if loadErr := store.Load(); loadErr != nil {
		return nil, fmt.Errorf("daemon.Start: load schedule store: %w", loadErr)
	}
	ensureOpsMonitorSchedule(store, cfg.ProjectCfg.Opsmonitor)
	ensureCtxWatchdogSchedule(store, cfg.ProjectCfg.Watchdog.Enabled)
	ensureWatchLivenessSchedule(store, cfg.ProjectCfg.Watch, "")
	return store, nil
}

func (bs *bootState) startBackgroundLoops(ctx context.Context, workerRegistry *workers.Registry) {
	cfg := bs.cfg

	bs.quiesceArbiter.Start(ctx)
	if bs.crewIdleReaper != nil {
		bs.crewIdleReaper.StartWatcher(ctx)
	}
	if bs.branchReapWatcher != nil {
		bs.branchReapWatcher.StartWatcher(ctx)
	}

	bs.logCompositionRoot(ctx, cfg.LogWriter)

	startReconciliationSchedulerIfEnabled(ctx, cfg.ProjectCfg, ReconciliationSchedulerConfig{
		ProjectDir:   cfg.ProjectDir,
		BrPath:       cfg.BrPath,
		TargetBranch: "", // defaults to "main" inside the scheduler
		Interval:     cfg.ReconciliationScanCadence,
		Emitter:      bs.bus,
		LogWriter:    cfg.LogWriter,
	})

	bs.startWorkerReportLoopIfEnabled(ctx, workerRegistry)
}

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

func (bs *bootState) wireStaleWatcherReapSeams(ctx context.Context, bus handlercontract.EventEmitter, projectDir, targetBranch string, queueStore *queuewiring.QueueStore, runRegistry *RunRegistry, loopLifecycle loopLifecyclePort, capacity capacityPort, queueSurface queueSurfacePort, eagerRefill eagerRefillPort) {
	cfg := bs.cfg
	reapPort := newReapSeamPort(bus, projectDir, targetBranch, queueStore, runRegistry, loopLifecycle, capacity, queueSurface, eagerRefill)

	bs.staleWatcher.SetForceReap(func(runID core.RunID, handle *RunHandle) {
		emitRunCompleted(ctx, bs.bus, runID, string(handle.BeadID), handle.OwningEpicID, handle.OwningEpicAssignee, false,
			"force-reaped: run wedged past cancel grace; concurrency slot reclaimed (hk-mdus1)",
			handle.QueueID, handle.QueueGroupIndex, nil)
		if handle.QueueName != "" && handle.QueueID != nil && handle.QueueGroupIndex != nil && handle.QueueItemIndex >= 0 {
			evaluateGroupAdvanceWithOutcome(ctx, reapPort, handle.QueueName, *handle.QueueID, *handle.QueueGroupIndex, handle.QueueItemIndex, false, time.Now())
		}
	})

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

func (bs *bootState) probeRunProcessDead(ctx context.Context, reapAdapter ltmux.Adapter, runID core.RunID) bool {
	registry, listErr := runpkg.ScanRegistry(bs.cfg.ProjectDir)
	if listErr != nil {
		return false
	}
	for _, r := range registry.Legacy {
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
	for _, r := range registry.Dispatch {
		if r.RunID != runID || r.SessionName == "" {
			continue
		}
		pid, pidErr := reapAdapter.WindowPanePID(ctx, ltmux.WindowHandle(r.SessionName+":"))
		if pidErr != nil {
			return false
		}
		return pid == 0 || processDead(pid)
	}
	return false
}
