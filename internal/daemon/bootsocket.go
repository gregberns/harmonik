package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gregberns/harmonik/internal/agentmanifest"
	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/workspace"
)

func (bs *bootState) wireSocketListener(ctx context.Context, daemonStartTime time.Time) error {
	if err := bs.registerAdaptersAndHookStore(); err != nil {
		return err
	}
	if err := bs.loadStartupState(ctx, daemonStartTime); err != nil {
		return err
	}
	if bs.cfg.ProjectDir == "" {
		return nil
	}
	_, bindErr := bs.bindSocketIfEnabled(ctx)
	return bindErr
}

func (bs *bootState) socketListenerEnabled() bool {
	return bs.cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemSocketListener)
}

func (bs *bootState) bindSocketIfEnabled(ctx context.Context) (bool, error) {
	if !bs.socketListenerEnabled() {
		logW := bs.cfg.LogWriter
		if logW == nil {
			logW = os.Stderr
		}
		fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; socket listener and its handler subtree not constructed\n", //nolint:errcheck // best-effort stderr status log
			projectconfig.SubsystemSocketListener)
		return false, nil
	}
	return true, bs.bindSocket(ctx)
}

func (bs *bootState) registerAdaptersAndHookStore() error {
	adapterReg := handlercontract.NewAdapterRegistry()
	if regErr := handler.Register(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register ClaudeCodeAdapter: %w", regErr)
	}
	if regErr := handler.RegisterCodex(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register CodexAdapter: %w", regErr)
	}
	if regErr := handler.RegisterPi(adapterReg); regErr != nil {
		return fmt.Errorf("daemon.Start: register PiAdapter: %w", regErr)
	}
	claudeCodeAdapter, forAgentErr := adapterReg.ForAgent(core.AgentTypeClaudeCode)
	if forAgentErr != nil {
		return fmt.Errorf("daemon.Start: seal adapter registry: %w", forAgentErr)
	}
	bs.adapterReg = adapterReg

	bs.handlerPauseCtrl.SetAdapter(claudeCodeAdapter)

	hookStore := newDaemonHookStore(bs.bus)
	bs.hookStore = hookStore

	return nil
}

func (bs *bootState) loadStartupState(ctx context.Context, daemonStartTime time.Time) error {
	cfg := bs.cfg

	if loadErr := loadStartupQueues(ctx, cfg, bs.hooks, bs.bus, bs.qs, daemonStartTime); loadErr != nil {
		return loadErr
	}

	if cfg.ProjectDir != "" {
		harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")
		bs.handlerPauseCtrl.SetPersistFn(MakeHandlerPausePersistFn(harmonikDir))
		if loadErr := LoadHandlerPauseState(ctx, harmonikDir, bs.handlerPauseCtrl); loadErr != nil {
			return fmt.Errorf("daemon.Start: handler-state.json load: %w", loadErr)
		}
	}

	bs.decisionBlocker = NewDecisionBlocker()
	if cfg.ProjectDir != "" {
		if loadErr := LoadDecisionAckState(ctx, cfg.ProjectDir, bs.decisionBlocker); loadErr != nil {
			return fmt.Errorf("daemon.Start: decision_acks load: %w", loadErr)
		}
	}

	return nil
}

func (bs *bootState) bindSocket(ctx context.Context) error {
	cfg := bs.cfg
	sockPath := filepath.Join(cfg.ProjectDir, ".harmonik", "daemon.sock")

	if lenErr := lifecycle.ValidateSocketPathLength(sockPath); lenErr != nil {
		log.Printf("daemon.Start: %v", lenErr)
	}
	if mkErr := os.MkdirAll(filepath.Dir(sockPath), core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("daemon.Start: mkdir-p .harmonik (socket): %w", mkErr)
	}

	queueHandler := bs.buildQueueHandler(ctx)
	bs.buildPauseConcurrencyTuner(ctx, queueHandler)
	commsSendHandler := bs.buildCommsAndCrewHandlers()
	bs.startSocketListener(ctx, sockPath, queueHandler, commsSendHandler)
	return nil
}

func (bs *bootState) buildQueueHandler(ctx context.Context) QueueHandler {
	cfg := bs.cfg
	var queueHandler QueueHandler
	if cfg.BrPath == "" {
		return queueHandler
	}
	brAdapterForHandler, brHandlerErr := newBrAdapter(bs.hooks, cfg.BrPath, cfg.ProjectDir)
	if brHandlerErr != nil {
		_ = brcli.BrErrReconciliationCategoryWithEmit(ctx, brHandlerErr, "br-new-for-project-handler", bs.bus)
		return queueHandler
	}
	bs.recoveryLedger = queuewiring.NewBRQueueLedger(brAdapterForHandler)
	adapter := queue.NewHandlerAdapter(queuewiring.NewBRQueueLedger(brAdapterForHandler), cfg.ProjectDir, bs.qs, bs.bus)
	adapter.SetGlobalMaxConcurrent(cfg.MaxConcurrent)
	queueHandler = adapter
	bs.queueHandlerAdapter = adapter

	bs.drainDet = NewDrainDetector(brAdapterForHandler, brAdapterForHandler, queuewiring.NewBRQueueLedger(brAdapterForHandler), bs.sharedRunRegistry, bs.qs, cfg.ProjectDir)
	bs.quiesceArbiter.SetDrain(bs.drainDet)
	return queueHandler
}

func (bs *bootState) buildPauseConcurrencyTuner(ctx context.Context, queueHandler QueueHandler) {
	cfg := bs.cfg

	bs.opPauseCtrl = NewOperatorPauseController(bs.bus)
	bs.opPauseCtrl.SetQueueStates(bs.qs)

	bs.concurrencyCtrl = NewConcurrencyController(cfg.MaxConcurrent)
	if ha, ok := queueHandler.(*queue.HandlerAdapter); ok {
		ha.SetConcurrencyFuncs(bs.concurrencyCtrl.Get, bs.concurrencyCtrl.Set)
		if ss, ok := cfg.Substrate.(substrateWithSpawnCap); ok {
			ha.SetSpawnCapFunc(ss.SpawnCapSize)
		}
		if ss, ok := cfg.Substrate.(substrateWithSpawnCapSetter); ok {
			ha.SetSpawnCapSetFunc(ss.SetSpawnCap)
			startupCap := 0
			if getter, ok := cfg.Substrate.(substrateWithSpawnCap); ok {
				startupCap = getter.SpawnCapSize()
			}
			ha.SetSpawnCapBounds(startupCap, hostSpawnCapCeiling())
		}
	}

	bs.startBandwidthTunerIfEnabled(ctx)
}

const spawnCapSessionsPerCPU = 4

func hostSpawnCapCeiling() int {
	return runtime.NumCPU() * spawnCapSessionsPerCPU
}

func (bs *bootState) bandwidthTunerEnabled() bool {
	return bs.cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemBandwidthTuner)
}

func (bs *bootState) startBandwidthTunerIfEnabled(ctx context.Context) bool {
	cfg := bs.cfg
	if !bs.bandwidthTunerEnabled() {
		bs.logSubsystemDisabled(projectconfig.SubsystemBandwidthTuner, "bandwidth tuner and its rate-limit backstop not constructed")
		return false
	}
	if cfg.SubscriptionTokenCeiling <= 0 {
		return false
	}
	maxN := cfg.MaxConcurrent
	if maxN <= 0 {
		maxN = 1
	}
	projectsDir := workspace.DefaultClaudeProjectsDir()
	if projectsDir == "" {
		return false
	}
	tuner := NewBandwidthTuner(bs.concurrencyCtrl, maxN, cfg.SubscriptionTokenCeiling, projectsDir)
	tuner.SetGate(bs.pollGate)       // SS-007: OFF at INACTIVE (hk-w6q7)
	bs.tunerBackstop.SetTuner(tuner) // arm the pre-Seal backstop subscriber
	go tuner.Run(ctx)
	return true
}

func (bs *bootState) buildCommsAndCrewHandlers() CommsSendHandler {
	cfg := bs.cfg

	commsSendHandler := NewCommsSendHandler(bs.bus)
	if impl, ok := commsSendHandler.(*commsSendHandlerImpl); ok && cfg.ProjectDir != "" {
		pollCursorDir := filepath.Join(cfg.ProjectDir, ".harmonik", "comms", "cursors")
		liveCursorDir := filepath.Join(cfg.ProjectDir, ".harmonik", "comms", "cursors-live")
		pollCursorStore := NewCursorStore(pollCursorDir)
		liveCursorStore := NewCursorStore(liveCursorDir)
		impl.SetRecvDeps(pollCursorStore, liveCursorStore, cfg.JSONLLogPath)
		if bs.subscribeHub != nil {
			bs.subscribeHub.SetCommsCursorStore(liveCursorStore)
		}
	}

	var crewCommsEmitter crewKeeperCommsBus
	if ce, ok := bs.bus.(crewKeeperCommsBus); ok {
		crewCommsEmitter = ce
	}
	bs.crewHandler = NewCrewHandler(
		cfg.HandlerBinary, cfg.ProjectDir, cfg.ProjectCfg.Daemon.RemoteControlPrefix, cfg.Substrate, bs.opPauseCtrl,
		WithKeeperProbe(cfg.ProjectCfg.Keeper, bs.bus, crewCommsEmitter),
		WithCrewsConfig(cfg.ProjectCfg.Crews),
	)

	bs.crewIdleReaper = bs.newCrewIdleReaperIfEnabled()

	bs.branchReapWatcher = bs.newBranchReapWatcherIfEnabled()

	return commsSendHandler
}

func (bs *bootState) newCrewIdleReaperIfEnabled() *crewrun.CrewIdleReaper {
	cfg := bs.cfg
	if !cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemCrewIdleReap) {
		bs.logSubsystemDisabled(projectconfig.SubsystemCrewIdleReap, "crew idle reaper not constructed")
		return nil
	}
	agentsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "agents")
	return crewrun.NewCrewIdleReaper(crewrun.CrewIdleReaperConfig{
		ProjectDir: cfg.ProjectDir,
		Queues:     bs.qs,
		Stopper:    bs.crewHandler,
		// GATE-0 (hk-dy5gw): a persistent oversight role (manifest lifecycle.persistent)
		// is never reclaimed; a load error reads as non-persistent.
		PersistentType: func(typeName string) bool {
			tf, err := agentmanifest.Load(agentsDir, typeName)
			if err != nil {
				return false
			}
			return tf.Manifest.Lifecycle.Persistent
		},
	})
}

func (bs *bootState) newBranchReapWatcherIfEnabled() *BranchReapWatcher {
	if !bs.cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemBranchReaper) {
		bs.logSubsystemDisabled(projectconfig.SubsystemBranchReaper, "branch reaper not constructed")
		return nil
	}
	return NewBranchReapWatcher(BranchReapWatcherConfig{
		RepoDir: bs.cfg.ProjectDir,
	})
}

func (bs *bootState) logSubsystemDisabled(name projectconfig.SubsystemName, detail string) {
	logW := bs.cfg.LogWriter
	if logW == nil {
		logW = os.Stderr
	}
	fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; %s\n", name, detail) //nolint:errcheck // best-effort stderr status log
}

func (bs *bootState) startSocketListener(ctx context.Context, sockPath string, queueHandler QueueHandler, commsSendHandler CommsSendHandler) {
	cfg := bs.cfg

	stateBuilder := NewLiveStateBuilder(bs.sharedRunRegistry, bs.qs, bs.drainDet, bs.concurrencyCtrl, cfg.MaxConcurrent, cfg.ProjectDir, cfg.ProjectCfg.Keeper)
	stateHandler := NewLiveStateSocketHandler(stateBuilder)

	dashBuilder := NewDashboardBuilder(stateBuilder, cfg.ProjectDir, cfg.JSONLLogPath)
	dashHandler := NewLiveDashboardSocketHandler(dashBuilder)

	startPollGate(ctx, bs.pollGate, stateBuilder)

	socketDone := make(chan error, 1)
	handlers := SocketHandlers{
		Request:   &noopRequestHandler{},
		HookRelay: bs.hookStore,
		Queue:     queueHandler,
		Recovery:  NewQueueRecoveryController(bs.qs, cfg.ProjectDir, bs.recoveryLedger),
		Operator:  bs.opPauseCtrl,
		Comms:     commsSendHandler,
		Crew:      bs.crewHandler,
		SleepWake: bs.quiesceArbiter,
		State:     stateHandler,
		Dashboard: dashHandler,
	}
	handlers.SessionStart = sessionStartAcknowledgementHandler{
		projectDir: cfg.ProjectDir,
		resolveAdapter: newSessionStartAdapterResolver(
			extractTmuxAdapterFromSubstrate(cfg.Substrate), cfg.Workers,
		),
	}
	if bs.subscribeHub != nil {
		handlers.Subscribe = bs.subscribeHub
	}
	go func() {
		socketDone <- Serve(ctx, sockPath, handlers)
	}()
	go func() { <-socketDone }() // drain: non-fatal; socket bind error discarded (see comment above)
}
