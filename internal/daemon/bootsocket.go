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
	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
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
	_, bindErr := bs.bindSocketIfEnabled(ctx)
	return bindErr
}

// socketListenerEnabled reports whether the socket-listener subsystem is
// switched on. It is the SINGLE reading of that switch: both construction sites
// for the subtree (bindSocketIfEnabled here, and the pre-Seal bandwidth-tuner
// backstop in wireWatchersAndObservers) call this, so the two can never
// disagree about whether the tuner will exist.
func (bs *bootState) socketListenerEnabled() bool {
	return bs.cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemSocketListener)
}

// bindSocketIfEnabled applies subsystem partitioning to the socket listener: it
// is the ONE construction seam for the whole socket subtree.
//
// When `subsystems.socket_listener.enabled: false` is set in
// .harmonik/config.yaml, NONE of bindSocket's fan-out is constructed — no comms
// or crew handler, no crew-idle reaper, no branch reaper, no live-state or
// dashboard surface, no bandwidth tuner, no operator-pause or concurrency
// controller, no drain detector, no queue handler adapter, and no listener
// goroutine. The Unix socket is never even created on disk. This is deliberately
// NOT "constructed but inert": inert code still holds the composition root
// hostage, and the bootState fields those constructors fill stay nil, which
// forces every downstream consumer seam to answer the nil question explicitly.
//
// Absent config (the zero ProjectConfig) enables the listener, so a deployment
// without a subsystems: block behaves exactly as it did before the block existed.
// The separate ProjectDir == "" early return in wireSocketListener is NOT this
// switch — that is the unit-test escape hatch, never taken in production.
//
// Returns true when the subtree was constructed. The return value is the
// observable decision; the production caller ignores it.
func (bs *bootState) bindSocketIfEnabled(ctx context.Context) (bool, error) {
	if !bs.socketListenerEnabled() {
		// Say so at boot: a silent partition is indistinguishable from a config
		// that did not take effect.
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
	if mkErr := os.MkdirAll(filepath.Dir(sockPath), core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("daemon.Start: mkdir-p .harmonik (socket): %w", mkErr)
	}

	queueHandler := bs.buildQueueHandler(ctx)
	bs.buildPauseConcurrencyTuner(ctx, queueHandler)
	commsSendHandler := bs.buildCommsAndCrewHandlers()
	bs.startSocketListener(ctx, sockPath, queueHandler, commsSendHandler)
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
	adapter := queue.NewHandlerAdapter(queuewiring.NewBRQueueLedger(brAdapterForHandler), cfg.ProjectDir, bs.qs, bs.bus)
	// Wire the global --max-concurrent so submit can default a queue's Workers
	// count (QM-066) and warn on oversubscription (hk-tigaf.4 NQ-B1).
	adapter.SetGlobalMaxConcurrent(cfg.MaxConcurrent)
	queueHandler = adapter
	bs.queueHandlerAdapter = adapter

	// SS-INV-005 veto gate into the quiesce arbiter (P1-c, hk-zqb3).
	bs.drainDet = NewDrainDetector(brAdapterForHandler, brAdapterForHandler, queuewiring.NewBRQueueLedger(brAdapterForHandler), bs.sharedRunRegistry, bs.qs, cfg.ProjectDir)
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
	// rolling 5h token usage. Subject to subsystem partitioning; see
	// startBandwidthTunerIfEnabled.
	bs.startBandwidthTunerIfEnabled(ctx)
}

// bandwidthTunerEnabled reports whether the bandwidth-tuner subsystem is switched
// on. It is the SINGLE reading of that switch: both halves of the tuner call it —
// the pre-Seal backstop subscriber in wireWatchersAndObservers and the tuner
// itself here — so the two can never disagree about whether the tuner will exist.
// That invariant is load-bearing, because startBandwidthTunerIfEnabled calls
// SetTuner on the backstop, which panics on a nil receiver.
func (bs *bootState) bandwidthTunerEnabled() bool {
	return bs.cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemBandwidthTuner)
}

// startBandwidthTunerIfEnabled applies subsystem partitioning to the bandwidth
// tuner: it is the ONE construction seam for the tuner goroutine.
//
// When `subsystems.bandwidth_tuner.enabled: false` is set, neither the tuner nor
// its bus subscriber exists — no BandwidthTuner object, no 60 s ticker goroutine,
// and (via the same switch read in wireWatchersAndObservers) no
// bandwidthTunerBackstop subscription on the event bus.
//
// ORDER IS LOAD-BEARING, for the same reason as newMovementGovernorIfEnabled: the
// subsystem check comes BEFORE the SubscriptionTokenCeiling check. Most
// deployments set no ceiling, so the ceiling check alone would swallow every
// disabled boot silently and an operator could never tell the switch took effect.
//
// Returns true when the tuner was started. The return value is the observable
// decision; the production caller ignores it.
func (bs *bootState) startBandwidthTunerIfEnabled(ctx context.Context) bool {
	cfg := bs.cfg
	if !bs.bandwidthTunerEnabled() {
		bs.logSubsystemDisabled(projectconfig.SubsystemBandwidthTuner, "bandwidth tuner and its rate-limit backstop not constructed")
		return false
	}
	// No token ceiling configured: there is no rate to tune against. This is the
	// pre-existing gate, not the subsystem switch — it stays quiet because it is
	// the default state of most deployments, not an operator's partition.
	if cfg.SubscriptionTokenCeiling <= 0 {
		return false
	}
	// normalised MaxConcurrent (zero → 1) is the N_max.
	maxN := cfg.MaxConcurrent
	if maxN <= 0 {
		maxN = 1
	}
	homeDir, homeDirErr := os.UserHomeDir()
	if homeDirErr != nil {
		return false
	}
	tuner := NewBandwidthTuner(bs.concurrencyCtrl, maxN, cfg.SubscriptionTokenCeiling, homeDir)
	tuner.SetGate(bs.pollGate)       // SS-007: OFF at INACTIVE (hk-w6q7)
	bs.tunerBackstop.SetTuner(tuner) // arm the pre-Seal backstop subscriber
	go tuner.Run(ctx)
	return true
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
		// Nil when subsystems.subscribe_hub.enabled: false. The shared cursor
		// only matters to a live tail, and there is no live tail without a hub,
		// so comms-recv polling keeps its own store and loses nothing.
		if bs.subscribeHub != nil {
			bs.subscribeHub.SetCommsCursorStore(liveCursorStore)
		}
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

	// SD-3 (hk-s2eac): idle-completed-crew reaper. Started post-Seal in the work
	// loop. Nil when partitioned away.
	bs.crewIdleReaper = bs.newCrewIdleReaperIfEnabled()

	// hk-2i36s: periodic branch reaper. Started post-Seal in the work loop. Nil
	// when partitioned away.
	bs.branchReapWatcher = bs.newBranchReapWatcherIfEnabled()

	return commsSendHandler
}

// newCrewIdleReaperIfEnabled builds the SD-3 idle-completed-crew reaper, or
// returns nil when `subsystems.crew_idle_reap.enabled: false` partitions it away.
//
// Nil is the OFF state and startBackgroundLoops guards on it. That guard is now
// load-bearing rather than incidental: it used to survive a nil receiver only
// because CrewIdleReaper.StartWatcher's body happens to be empty today (the
// operator-directed 2026-07-18 disable), which is an accident of the sweep being
// inert, not a property of the type.
//
// Absent config enables the reaper, so a deployment without a subsystems: block
// behaves exactly as it did before the block existed.
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

// newBranchReapWatcherIfEnabled builds the periodic branch reaper, or returns nil
// when `subsystems.branch_reaper.enabled: false` partitions it away.
//
// Nil is the OFF state; startBackgroundLoops already guards on it, because
// BranchReapWatcher.StartWatcher spawns a goroutine that dereferences the
// receiver immediately — an unguarded nil takes the whole daemon down from a
// goroutine rather than returning an error.
//
// Absent config enables the reaper, so a deployment without a subsystems: block
// behaves exactly as it did before the block existed.
func (bs *bootState) newBranchReapWatcherIfEnabled() *BranchReapWatcher {
	if !bs.cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemBranchReaper) {
		bs.logSubsystemDisabled(projectconfig.SubsystemBranchReaper, "branch reaper not constructed")
		return nil
	}
	return NewBranchReapWatcher(BranchReapWatcherConfig{
		RepoDir: bs.cfg.ProjectDir,
	})
}

// logSubsystemDisabled announces a partition on the daemon's log writer. Saying
// it is not politeness: a silent partition is indistinguishable from a config
// that did not take effect, which is how an operator ends up believing a
// subsystem was switched off while it kept running.
func (bs *bootState) logSubsystemDisabled(name projectconfig.SubsystemName, detail string) {
	logW := bs.cfg.LogWriter
	if logW == nil {
		logW = os.Stderr
	}
	fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; %s\n", name, detail) //nolint:errcheck // best-effort stderr status log
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
	handlers := SocketHandlers{
		Request:   &noopRequestHandler{},
		HookRelay: bs.hookStore,
		Queue:     queueHandler,
		Operator:  bs.opPauseCtrl,
		Comms:     commsSendHandler,
		Crew:      bs.crewHandler,
		SleepWake: bs.quiesceArbiter,
		State:     stateHandler,
		Dashboard: dashHandler,
	}
	// Assign Subscribe only when the hub exists. A nil *SubscribeHub written into
	// this INTERFACE field makes a NON-nil interface, so handleSubscribe's
	// `if sub == nil` refusal would not fire and the op would call a method on a
	// nil receiver instead. Leaving the field at its zero value keeps the
	// refusal path reachable.
	//
	// Note that this switch is what makes that refusal reachable for the first
	// time. Four of the six clients of this op do not check the response
	// envelope, so they mis-report the refusal. See hk-1dwk2 (P1) and the
	// SubsystemSubscribeHub doc in projectconfig before you set the switch.
	if bs.subscribeHub != nil {
		handlers.Subscribe = bs.subscribeHub
	}
	go func() {
		socketDone <- Serve(ctx, sockPath, handlers)
	}()
	go func() { <-socketDone }() // drain: non-fatal; socket bind error discarded (see comment above)
}
