package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/agentmanifest"
	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

// wireSocketListener performs PL-005 step 4 / step 8a and PL-003 (P9-P11):
// register agent adapters + the hook store, load persisted startup state (queues,
// handler-pause, decision-acks), then — when ProjectDir is set — build the socket
// controllers/handlers and bind the Unix-domain socket listener. Split into
// sub-helpers so each stays under the funlen/cyclop ceilings; the persistent
// singletons (adapterReg, hookStore, opPauseCtrl, concurrencyCtrl, crewHandler,
// …) thread through bootState for the work loop (P13). Extracted for
// giant-retirement boot-config B5.
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
	return bs.bindSocket(ctx)
}

// registerAdaptersAndHookStore performs Step 4 (hk-ecrxy) P9: construct + seal
// the AdapterRegistry (Claude/Codex/Pi), inject the ClaudeCode adapter into the
// handler-pause controller (HC-014a), and construct the hook-session store wired
// to the bus emitter (hk-lqtzq). Runs unconditionally (no ProjectDir guard).
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
	// Seal the registry via the first ForAgent call (explicit seal makes the
	// PL-020a ordering contract observable).
	claudeCodeAdapter, forAgentErr := adapterReg.ForAgent(core.AgentTypeClaudeCode)
	if forAgentErr != nil {
		return fmt.Errorf("daemon.Start: seal adapter registry: %w", forAgentErr)
	}
	bs.adapterReg = adapterReg

	// HC-014a (hk-tvsl7): inject the ClaudeCode adapter so the pause controller
	// can Diagnose on pause-trip and Resume.
	bs.handlerPauseCtrl.SetAdapter(claudeCodeAdapter)

	// Hook-session store (hk-gql20.21): forwarded to RunSocketListener and into
	// workLoopDeps. The pure session-store state machine lives in internal/hook;
	// newDaemonHookStore composes it with the bus emitter used by the rate-limit
	// routing path (hk-lqtzq). bus.Seal has run, so the emitter is used for Emit
	// (delivery, not subscription), which is valid post-Seal.
	hookStore := newDaemonHookStore(bs.bus)
	bs.hookStore = hookStore

	return nil
}

// loadStartupState performs PL-005 step 8a P10: load per-queue files
// (QM-002/QM-002a), seed the handler-pause controller from disk (HP-008), and
// load the decision-ack state (EV-043a). loadStartupQueues + handler-pause load
// self-guard on ProjectDir/BrPath; the DecisionBlocker is always constructed.
func (bs *bootState) loadStartupState(ctx context.Context, daemonStartTime time.Time) error {
	cfg := bs.cfg

	// Load per-queue files at startup BEFORE the socket listener / work loop.
	// Only runs when both ProjectDir and BrPath are set (production mode). The
	// queue-load body was extracted into loadStartupQueues upstream (3db50f1d,
	// hk-tigaf.3); loadStartupState calls it so the extraction stays live and the
	// ctx-threaded (4177c8d6) behaviour is shared with the standalone helper.
	if loadErr := loadStartupQueues(ctx, cfg, bs.hooks, bs.bus, bs.qs, daemonStartTime); loadErr != nil {
		return loadErr
	}

	// PL-005 step 8a (hk-m0k0a): patch the persistFn into the HandlerPauseController
	// (constructed pre-Seal with nil persistFn) and seed it from disk (HP-008).
	if cfg.ProjectDir != "" {
		harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")
		bs.handlerPauseCtrl.SetPersistFn(MakeHandlerPausePersistFn(harmonikDir))
		if loadErr := LoadHandlerPauseState(ctx, harmonikDir, bs.handlerPauseCtrl); loadErr != nil {
			return fmt.Errorf("daemon.Start: handler-state.json load: %w", loadErr)
		}
	}

	// DecisionBlocker (EV-043a, hk-pbmsq): shared by the socket listener and the
	// workloop. Always constructed; seeded from disk when ProjectDir is set.
	bs.decisionBlocker = NewDecisionBlocker()
	if cfg.ProjectDir != "" {
		if loadErr := LoadDecisionAckState(ctx, cfg.ProjectDir, bs.decisionBlocker); loadErr != nil {
			return fmt.Errorf("daemon.Start: decision_acks load: %w", loadErr)
		}
	}

	return nil
}

// bindSocket performs PL-003 / CHB-025 (hk-tjl40) P11: bind the Unix-domain
// socket so hook-relay subprocesses can deliver outcome_emitted envelopes. Only
// reached when ProjectDir is set. Builds the queue handler, pause/concurrency
// controllers + bandwidth tuner, comms/crew handlers, then starts the listener.
func (bs *bootState) bindSocket(ctx context.Context) error {
	cfg := bs.cfg
	sockPath := filepath.Join(cfg.ProjectDir, ".harmonik", "daemon.sock")

	// hk-ta6dg: a too-long sockPath is a PERMANENT bind failure and silently
	// defeats the reverse-tunnel readiness gate. PL-003 keeps socket-bind errors
	// non-fatal to daemon.Start, so this is a loud diagnostic, not an abort.
	if lenErr := lifecycle.ValidateSocketPathLength(sockPath); lenErr != nil {
		log.Printf("daemon.Start: %v", lenErr)
	}
	// .harmonik/ may not exist when ProjectDir is set with BrPath="" (test mode
	// skipping pidfile). MkdirAll is idempotent.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(filepath.Dir(sockPath), 0o755); mkErr != nil {
		return fmt.Errorf("daemon.Start: mkdir-p .harmonik (socket): %w", mkErr)
	}

	queueHandler := bs.buildQueueHandler(ctx)
	bs.buildPauseConcurrencyTuner(ctx, queueHandler)
	commsSendHandler := bs.buildCommsAndCrewHandlers()
	bs.startSocketListener(ctx, sockPath, queueHandler, commsSendHandler)

	// hk-220lv: the keeper-revive sweep starts HERE, with the socket listener,
	// rather than alongside the other background loops in startBackgroundLoops.
	// Deliberate divergence from the sibling reapers: startBackgroundLoops runs
	// inside launchWorkLoop, which returns early when BrPath is unset, whereas the
	// socket listener — and therefore the crew-start op, and therefore crews and
	// their keepers — is live whenever ProjectDir is set. Gating crew keeper
	// self-heal on the BEAD-DISPATCH path would leave a br-less daemon hosting
	// crews with no keeper protection at all: the same silently-inactive safety
	// net this bead exists to remove.
	bs.keeperReviveWatcher.StartWatcher(ctx)
	return nil
}

// buildQueueHandler constructs the QueueHandler adapter (nil when BrPath is
// unset; RunSocketListener returns -32099 for queue-* ops). It retains the
// concrete adapter (bs.queueHandlerAdapter) for the work-loop worker-toggle wiring
// (hk-xjbvi) and constructs the DrainDetector, wiring it into the quiesce arbiter
// veto gate (P1-c, hk-zqb3). Returns the QueueHandler for the listener.
func (bs *bootState) buildQueueHandler(ctx context.Context) QueueHandler {
	cfg := bs.cfg
	var queueHandler QueueHandler
	if cfg.BrPath == "" {
		return queueHandler
	}
	brAdapterForHandler, brHandlerErr := newBrAdapter(bs.hooks, cfg.BrPath, cfg.ProjectDir)
	if brHandlerErr != nil {
		// Classify + emit per BI-031b. Non-fatal: socket handler proceeds without
		// queue support (hk-th378).
		_ = brcli.BrErrReconciliationCategoryWithEmit(ctx, brHandlerErr, "br-new-for-project-handler", bs.bus)
		return queueHandler
	}
	adapter := queue.NewHandlerAdapter(newBRQueueLedger(brAdapterForHandler), cfg.ProjectDir, bs.qs, bs.bus)
	// Wire the global --max-concurrent so submit can default a queue's Workers
	// count (QM-066) and warn on oversubscription (hk-tigaf.4 NQ-B1).
	adapter.SetGlobalMaxConcurrent(cfg.MaxConcurrent)
	queueHandler = adapter
	bs.queueHandlerAdapter = adapter

	// SS-INV-005 veto gate into the quiesce arbiter (P1-c, hk-zqb3).
	bs.drainDet = NewDrainDetector(brAdapterForHandler, brAdapterForHandler, newBRQueueLedger(brAdapterForHandler), bs.sharedRunRegistry, bs.qs, cfg.ProjectDir)
	bs.quiesceArbiter.SetDrain(bs.drainDet)
	return queueHandler
}

// buildPauseConcurrencyTuner constructs the OperatorPauseController (hk-ry8q1)
// and ConcurrencyController (hk-ohiaf), wires the concurrency setters + spawn-cap
// funcs into the queue HandlerAdapter, and starts the bandwidth tuner when
// --subscription-token-ceiling is set (hk-ymav1).
func (bs *bootState) buildPauseConcurrencyTuner(ctx context.Context, queueHandler QueueHandler) {
	cfg := bs.cfg

	bs.opPauseCtrl = NewOperatorPauseController(bs.bus)

	bs.concurrencyCtrl = NewConcurrencyController(cfg.MaxConcurrent)
	if ha, ok := queueHandler.(*queue.HandlerAdapter); ok {
		ha.SetConcurrencyFuncs(bs.concurrencyCtrl.Get, bs.concurrencyCtrl.Set)
		// hk-vfeeo: wire spawn cap so set-concurrency can detect oversubscription.
		if ss, ok := cfg.Substrate.(substrateWithSpawnCap); ok {
			ha.SetSpawnCapFunc(ss.SpawnCapSize)
		}
		// hk-omvan: wire the live spawn-cap resize setter so set-concurrency RAISES
		// the cap to satisfy an oversubscribing request instead of refusing it.
		if ss, ok := cfg.Substrate.(substrateWithSpawnCapSetter); ok {
			ha.SetSpawnCapSetFunc(ss.SetSpawnCap)
		}
	}

	// Bandwidth tuner (hk-ymav1): adjusts concurrencyCtrl on every 60s tick from
	// rolling 5h token usage. normalised MaxConcurrent (zero → 1) is the N_max.
	if cfg.SubscriptionTokenCeiling > 0 {
		maxN := cfg.MaxConcurrent
		if maxN <= 0 {
			maxN = 1
		}
		if homeDir, homeDirErr := os.UserHomeDir(); homeDirErr == nil {
			tuner := NewBandwidthTuner(bs.concurrencyCtrl, maxN, cfg.SubscriptionTokenCeiling, homeDir)
			tuner.SetGate(bs.pollGate)       // SS-007: OFF at INACTIVE (hk-w6q7)
			bs.tunerBackstop.SetTuner(tuner) // arm the pre-Seal backstop subscriber
			go tuner.Run(ctx)
		}
	}
}

// buildCommsAndCrewHandlers constructs the comms-send handler (+recv cursor deps,
// hk-nnwaa / hk-8xspi), the C2 crew-start/stop handler (hk-5tg5o), the idle-crew
// reaper (SD-3, hk-s2eac), and the periodic branch reaper (hk-2i36s). Returns the
// comms-send handler for the listener; the reapers are started in the work loop.
func (bs *bootState) buildCommsAndCrewHandlers() CommsSendHandler {
	cfg := bs.cfg

	commsSendHandler := NewCommsSendHandler(bs.bus)
	// Wire comms-recv deps (T8): two INDEPENDENT cursor stores + events JSONL path.
	// The live store is shared with the SubscribeHub (hk-tafd4) so a follow/wait
	// session's drain and its live tail stay on one continuous cursor (hk-8xspi B1).
	if impl, ok := commsSendHandler.(*commsSendHandlerImpl); ok && cfg.ProjectDir != "" {
		pollCursorDir := filepath.Join(cfg.ProjectDir, ".harmonik", "comms", "cursors")
		liveCursorDir := filepath.Join(cfg.ProjectDir, ".harmonik", "comms", "cursors-live")
		pollCursorStore := NewCursorStore(pollCursorDir)
		liveCursorStore := NewCursorStore(liveCursorDir)
		impl.SetRecvDeps(pollCursorStore, liveCursorStore, cfg.JSONLLogPath)
		bs.subscribeHub.SetCommsCursorStore(liveCursorStore)
	}

	// C2 crew-start/stop handler (hk-5tg5o). Wire the keeper probe (hk-qgfme);
	// EmitAgentMessage lives on the optional CommsMessageEmitter capability.
	var crewCommsEmitter crewKeeperCommsBus
	if ce, ok := bs.bus.(crewKeeperCommsBus); ok {
		crewCommsEmitter = ce
	}
	bs.crewHandler = NewCrewHandler(
		cfg.HandlerBinary, cfg.ProjectDir, cfg.ProjectCfg.Daemon.RemoteControlPrefix, cfg.Substrate, bs.opPauseCtrl,
		WithKeeperProbe(cfg.ProjectCfg.Keeper, bs.bus, crewCommsEmitter),
		// hk-l63b9: third tier of the crew-scoped harness resolver (flag >
		// mission front-matter > per-crew config > default "claude").
		WithCrewsConfig(cfg.ProjectCfg.Crews),
	)

	// SD-3 (hk-s2eac): idle-completed-crew reaper. Started post-Seal in the work loop.
	crewIdleReaperAgentsDir := filepath.Join(cfg.ProjectDir, ".harmonik", "agents")
	bs.crewIdleReaper = NewCrewIdleReaper(CrewIdleReaperConfig{
		ProjectDir: cfg.ProjectDir,
		Queues:     bs.qs,
		Stopper:    bs.crewHandler,
		// GATE-0 (hk-dy5gw): a persistent oversight role (manifest lifecycle.persistent)
		// is never reclaimed; a load error reads as non-persistent.
		PersistentType: func(typeName string) bool {
			tf, err := agentmanifest.Load(crewIdleReaperAgentsDir, typeName)
			if err != nil {
				return false
			}
			return tf.Manifest.Lifecycle.Persistent
		},
	})

	// hk-220lv: keeper-revive watcher. The crew keeper watcher is launched
	// fire-and-forget as a tmux window, so when its process dies the flock it held
	// is dropped SILENTLY and the crew runs unmonitored indefinitely (43 h in the
	// field case). This sweep re-probes the flock periodically and re-arms the
	// keeper window. DEFAULT ON: absent config → compiled defaults; only an
	// explicit `keeper.timings.revive_scan_interval: 0s` disables it. Started
	// post-Seal in the work loop.
	keeperCfg := cfg.ProjectCfg.Keeper
	var keeperReArm func(ctx context.Context, crewName, session string) error
	if reArmer, ok := cfg.Substrate.(crewKeeperReArmer); ok {
		projectDir := cfg.ProjectDir
		keeperReArm = func(ctx context.Context, crewName, session string) error {
			return reArmer.ReArmCrewKeeperWindow(ctx, crewName, session, projectDir)
		}
	}
	// keeperReArm stays nil on a non-tmux substrate (HARMONIK_SUBSTRATE=codexdriver).
	// StartWatcher announces that INACTIVE state loudly at boot rather than going
	// quietly dark — see KeeperReviveWatcher.inactiveReason.
	bs.keeperReviveWatcher = NewKeeperReviveWatcher(KeeperReviveWatcherConfig{
		ProjectDir:   cfg.ProjectDir,
		Disabled:     KeeperReviveDisabledByConfig(keeperCfg),
		ScanInterval: keeperCfg.ReviveScanInterval,
		Grace:        keeperCfg.ReviveGrace,
		MaxAttempts:  keeperCfg.ReviveMaxAttempts,
		ReArmFn:      keeperReArm,
		Emit:         bs.bus,
		Comms:        crewCommsEmitter,
	})

	// hk-2i36s: periodic branch reaper. Started post-Seal in the work loop.
	bs.branchReapWatcher = NewBranchReapWatcher(BranchReapWatcherConfig{
		RepoDir: cfg.ProjectDir,
	})

	return commsSendHandler
}

// startSocketListener builds the live state + dashboard handlers, starts the
// poll-gate goroutine (SS-007, hk-w6q7 P2-b), and launches the socket listener in
// its own goroutine. Socket-bind errors are non-fatal (PL-003); the done channel
// is drained to avoid a goroutine leak.
func (bs *bootState) startSocketListener(ctx context.Context, sockPath string, queueHandler QueueHandler, commsSendHandler CommsSendHandler) {
	cfg := bs.cfg

	// Live state handler (hk-gv04 P2-a: `harmonik state`). drainDet may be nil;
	// LiveStateBuilder tolerates that and sets read_quality.unsure=true.
	stateBuilder := NewLiveStateBuilder(bs.sharedRunRegistry, bs.qs, bs.drainDet, bs.concurrencyCtrl, cfg.MaxConcurrent, cfg.ProjectDir, cfg.ProjectCfg.Keeper)
	stateHandler := NewLiveStateSocketHandler(stateBuilder)

	dashBuilder := NewDashboardBuilder(stateBuilder, cfg.ProjectDir, cfg.JSONLLogPath)
	dashHandler := NewLiveDashboardSocketHandler(dashBuilder)

	// Poll-gate goroutine (SS-007): gates StaleWatcher + BandwidthTuner when
	// INACTIVE. Must start after stateBuilder is ready.
	startPollGate(ctx, bs.pollGate, stateBuilder)

	// Non-fatal: socket bind errors do not abort the daemon (PL-003 intent). Drain
	// the done channel to avoid goroutine leaks; error discarded (same reasoning as
	// defer ln.Close() discards errors in RunSocketListener).
	socketDone := make(chan error, 1)
	go func() {
		socketDone <- Serve(ctx, sockPath, SocketHandlers{
			Request:   &noopRequestHandler{},
			HookRelay: bs.hookStore,
			Queue:     queueHandler,
			Subscribe: bs.subscribeHub,
			Operator:  bs.opPauseCtrl,
			Comms:     commsSendHandler,
			Crew:      bs.crewHandler,
			SleepWake: bs.quiesceArbiter,
			State:     stateHandler,
			Dashboard: dashHandler,
		})
	}()
	go func() { <-socketDone }() // drain: non-fatal; socket bind error discarded (see comment above)
}
