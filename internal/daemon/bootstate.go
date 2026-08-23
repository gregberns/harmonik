package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/workers"
)

type bootState struct {
	cfg   Config
	hooks daemonTestHooks

	// P4 (constructBusAndRegistries) outputs.
	bus                     eventbus.EventBus
	clockRegressionDetected bool
	qs                      *queuewiring.QueueStore
	handlerPauseCtrl        *HandlerPauseController
	sharedRunRegistry       *RunRegistry
	workerRegistry          *workers.Registry
	workerRegistryBuilt     bool
	pollGate                *PollGate

	// P5 (wireWatchersAndObservers) outputs consumed by later phases.
	// stallFeed is the one route from the stall detector to a run's dispatch
	// machine. It is built here because the detector (StaleWatcher) is built
	// here, and it is read by P13, which hands it to every launch.
	stallFeed      *runloop.StallFeed
	staleWatcher   *StaleWatcher
	tunerBackstop  *bandwidthTunerBackstop
	quiesceArbiter *QuiesceArbiter
	subscribeHub   *SubscribeHub

	// P9-P11 (wireSocketListener) outputs consumed by P13 (work loop).
	adapterReg          *handlercontract.AdapterRegistry
	hookStore           *hookSessionStore
	decisionBlocker     *DecisionBlocker
	opPauseCtrl         *OperatorPauseController
	concurrencyCtrl     *ConcurrencyController
	queueHandlerAdapter *queue.HandlerAdapter
	drainDet            *DrainDetector
	// recoveryLedger is the QM-052b preflight reader for `queue-recover`. It is
	// nil when the daemon booted without a Beads adapter, which makes recovery
	// decide on queue state alone.
	recoveryLedger    queuewiring.RecoveryBeadReader
	crewHandler       crewrun.CrewHandler
	crewIdleReaper    *crewrun.CrewIdleReaper
	branchReapWatcher *BranchReapWatcher
}

func (bs *bootState) constructBusAndRegistries() (*eventbus.JSONLWriter, error) {
	cfg := bs.cfg

	registry := handlercontract.NewRedactionRegistry()

	var jsonlWriter *eventbus.JSONLWriter
	if cfg.JSONLLogPath != "" {
		var openErr error
		jsonlWriter, openErr = eventbus.OpenJSONLWriter(cfg.JSONLLogPath)
		if openErr != nil {
			return nil, fmt.Errorf("daemon.Start: open JSONL log %q: %w",
				filepath.Base(cfg.JSONLLogPath), openErr)
		}
	}

	var hwmGen *core.EventIDGenerator
	var hwmPath string
	if cfg.ProjectDir != "" {
		hwmPath = lifecycle.EventIDHWMPath(cfg.ProjectDir)
		hwm, hwmExists, hwmErr := core.ReadEventIDHWM(hwmPath)
		switch {
		case hwmErr != nil:
			log.Printf("daemon.Start: event_id HWM at %s unreadable: %v; seeding from wall clock — cross-restart ordering not guaranteed", hwmPath, hwmErr)
			hwmGen = core.NewEventIDGenerator()
		case !hwmExists:
			log.Printf("daemon.Start: event_id HWM not found at %s (first run or .harmonik/ wiped); seeding from wall clock — cross-restart ordering not guaranteed", hwmPath)
			hwmGen = core.NewEventIDGenerator()
		default:
			hwmGen = core.NewEventIDGeneratorWithHWM(hwm)
			if core.IsHWMClockRegression(hwm, time.Now()) {
				bs.clockRegressionDetected = true
			}
		}
	}
	if hwmGen == nil {
		hwmGen = core.NewEventIDGenerator()
	}

	bs.bus = eventbus.NewBusImplWithWriterAndHWM(registry, jsonlWriter, hwmGen, hwmPath, cfg.JSONLLogPath)

	qs := cfg.QueueStore
	if qs == nil {
		qs = queuewiring.NewQueueStore()
	}
	bs.qs = qs
	bs.handlerPauseCtrl = NewHandlerPauseController(bs.bus, nil)
	bs.sharedRunRegistry = NewRunRegistry()
	bs.pollGate = &PollGate{}

	return jsonlWriter, nil
}

func (bs *bootState) wireSpendAndQueueConsumers() error {
	cfg := bs.cfg
	bus := bs.bus

	if cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemHandlerPausePolicy) {
		pausePolicy := NewHandlerPausePolicyGoroutine(HandlerPausePolicyConfig{
			AgentType:  core.AgentTypeClaudeCode,
			Controller: bs.handlerPauseCtrl,
			Registry:   bs.sharedRunRegistry,
		})
		if subscribeErr := pausePolicy.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: HandlerPausePolicyGoroutine.Subscribe: %w", subscribeErr)
		}
	} else {
		bs.logSubsystemDisabled(projectconfig.SubsystemHandlerPausePolicy, "handler-pause policy not constructed")
	}

	if cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemDaemonSpendMeter) {
		spendMeter := NewDaemonSpendMeter(bus)
		if subscribeErr := spendMeter.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: DaemonSpendMeter.Subscribe: %w", subscribeErr)
		}
		if bs.hooks.spendMeterObserver != nil {
			bs.hooks.spendMeterObserver(spendMeter)
		}
	} else {
		bs.logSubsystemDisabled(projectconfig.SubsystemDaemonSpendMeter, "daemon spend meter not constructed")
	}

	perQueueSpendMeter := NewPerQueueSpendMeter(bs.sharedRunRegistry, bs.qs, cfg.ProjectDir)
	if subscribeErr := perQueueSpendMeter.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: PerQueueSpendMeter.Subscribe: %w", subscribeErr)
	}

	queueOpConsumer := queuewiring.NewQueueOperatorEventConsumer(queuewiring.QueueOperatorEventConsumerConfig{
		QueueStore: bs.qs,
		ProjectDir: cfg.ProjectDir,
		Bus:        bus,
	})
	if subscribeErr := queueOpConsumer.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: QueueOperatorEventConsumer.Subscribe: %w", subscribeErr)
	}

	if cfg.NotifyStream != nil {
		notifyConsumer := NewNotifyStreamConsumer(cfg.NotifyStream)
		if subscribeErr := notifyConsumer.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: NotifyStreamConsumer.Subscribe: %w", subscribeErr)
		}
	}

	if cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemSubscribeHub) {
		subscribeHubCfg := SubscribeHubConfig{
			Bus:             bus,
			ActiveRuns:      bs.sharedRunRegistry,
			EventsJSONLPath: cfg.JSONLLogPath, // for since_event_id replay (hk-a5sil)
		}
		if pe, ok := bus.(eventbus.CommsPresenceEmitter); ok {
			subscribeHubCfg.PresenceEmitter = pe
		}
		bs.subscribeHub = NewSubscribeHub(subscribeHubCfg)
		if subscribeErr := bs.subscribeHub.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: SubscribeHub.Subscribe: %w", subscribeErr)
		}
	} else {
		bs.logSubsystemDisabled(projectconfig.SubsystemSubscribeHub, "subscribe hub not constructed")
	}

	return nil
}

func (bs *bootState) wireWatchersAndObservers(ctx context.Context) error {
	cfg := bs.cfg
	bus := bs.bus

	bs.stallFeed = runloop.NewStallFeed()
	bs.staleWatcher = NewStaleWatcher(StaleWatcherConfig{
		SubscribeBus: bus,
		Emitter:      bus,
		Registry:     bs.sharedRunRegistry,
		Gate:         bs.pollGate,
		StallFeed:    bs.stallFeed,
	})
	if subscribeErr := bs.staleWatcher.Subscribe(); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: StaleWatcher.Subscribe: %w", subscribeErr)
	}

	if cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemReviewGateAnomaly) {
		reviewGateWatcher := NewReviewGateAnomalyWatcher(bus)
		if subscribeErr := reviewGateWatcher.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: ReviewGateAnomalyWatcher.Subscribe: %w", subscribeErr)
		}
	} else {
		bs.logSubsystemDisabled(projectconfig.SubsystemReviewGateAnomaly, "review-gate anomaly watcher not constructed")
	}

	if bs.socketListenerEnabled() && bs.bandwidthTunerEnabled() {
		bs.tunerBackstop = &bandwidthTunerBackstop{}
		if subscribeErr := bs.tunerBackstop.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: bandwidth-tuner backstop subscribe: %w", subscribeErr)
		}
		bs.tunerBackstop.SetRunRegistry(bs.sharedRunRegistry) // PI-073: isolate Pi events
	}

	var quiesceAdapter ltmux.Adapter
	if sa, ok := cfg.Substrate.(substrateWithAdapter); ok {
		quiesceAdapter = sa.tmuxAdapter()
	}
	var quiesceCommsBus eventbus.CommsMessageEmitter
	if ce, ok := bus.(eventbus.CommsMessageEmitter); ok {
		quiesceCommsBus = ce
	}
	var quiesceHash core.ProjectHash
	if cfg.ProjectDir != "" {
		quiesceHash = lifecycle.ComputeProjectHash(cfg.ProjectDir)
	}
	bs.quiesceArbiter = NewQuiesceArbiter(QuiesceArbiterConfig{
		ProjectDir:  cfg.ProjectDir,
		ProjectHash: quiesceHash,
		Adapter:     quiesceAdapter,
		QueueStore:  bs.qs,
		CommsBus:    quiesceCommsBus,
	})
	if subscribeErr := bs.quiesceArbiter.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: QuiesceArbiter.Subscribe: %w", subscribeErr)
	}

	if hookSetter, ok := cfg.Substrate.(substrateDiagnosticHookSetter); ok {
		hookSetter.setDiagnosticHooks(
			func(waited time.Duration, inUse, capSize int) {
				runlaunch.EmitSpawnCapBlocked(ctx, bus, core.RunID{}, waited, inUse, capSize)
			},
			func(waited time.Duration) {
				runlaunch.EmitTmuxNewWindowTimeout(ctx, bus, core.RunID{}, waited)
			},
		)
	}

	if !cfg.ProjectCfg.Subsystems.Enabled(projectconfig.SubsystemLedgerImportRecovery) {
		bs.logSubsystemDisabled(projectconfig.SubsystemLedgerImportRecovery, "bead-ledger import recovery not constructed")
	} else if cfg.ProjectDir != "" && cfg.BrPath != "" {
		catBL2Handler := NewCatBL2Handler(CatBL2HandlerConfig{
			ProjectDir: cfg.ProjectDir,
			BrPath:     cfg.BrPath,
			Emitter:    bus,
		})
		if subscribeErr := catBL2Handler.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: CatBL2Handler.Subscribe: %w", subscribeErr)
		}
	}

	if bs.hooks.busObserver != nil {
		bs.hooks.busObserver(bus)
	}

	return nil
}

func (bs *bootState) emitStartupEvents(ctx context.Context, clockRegressionDetected bool, resolvedTargetBranch string) (time.Time, error) {
	cfg := bs.cfg
	bus := bs.bus

	if clockRegressionDetected {
		degradedPayload := core.DaemonDegradedPayload{
			DetectedAt: time.Now().UTC().Format(time.RFC3339),
			Reason:     core.DaemonDegradedReasonClockRegression,
		}
		if degradedBytes, marshalErr := json.Marshal(degradedPayload); marshalErr == nil {
			if emitErr := bus.Emit(ctx, core.EventTypeDaemonDegraded, degradedBytes); emitErr != nil {
				log.Printf("warn: daemon.Start: emit daemon_degraded: %v", emitErr)
			}
		}
	}

	bs.staleWatcher.StartWatcher(ctx)

	binaryCommitHash := cfg.BinaryCommitHash
	if binaryCommitHash == "" {
		binaryCommitHash = "unknown"
	}
	daemonStartTime := time.Now().UTC()
	startedPayload := core.DaemonStartedPayload{
		StartedAt:        daemonStartTime.Format(time.RFC3339),
		PID:              os.Getpid(),
		BinaryCommitHash: binaryCommitHash,
	}
	payloadBytes, marshalErr := json.Marshal(startedPayload)
	if marshalErr != nil {
		return time.Time{}, fmt.Errorf("daemon.Start: marshal daemon_started payload: %w", marshalErr)
	}
	if emitErr := bus.Emit(ctx, core.EventTypeDaemonStarted, payloadBytes); emitErr != nil {
		return time.Time{}, fmt.Errorf("daemon.Start: emit daemon_started: %w", emitErr)
	}

	if cfg.JSONLLogPath != "" {
		detectAndEmitSupervisorRevival(ctx, cfg.JSONLLogPath, bus)
	}

	bs.emitDaemonConfig(ctx, resolvedTargetBranch)

	return daemonStartTime, nil
}

func (bs *bootState) emitDaemonConfig(ctx context.Context, resolvedTargetBranch string) {
	cfg := bs.cfg
	cfgPayload := core.DaemonConfigPayload{
		TargetBranch:             resolvedTargetBranch,
		ProtectBranches:          cfg.ProtectBranches,
		ForbidUnprotectedDefault: cfg.ForbidUnprotectedDefault,
		WorkflowMode:             string(cfg.WorkflowModeDefault),
		MaxConcurrent:            cfg.MaxConcurrent,
		NoAutoPull:               cfg.NoAutoPull,
	}
	if !cfgPayload.Valid() {
		return
	}
	if cfgBytes, cfgMarshalErr := json.Marshal(cfgPayload); cfgMarshalErr == nil {
		if emitErr := bs.bus.Emit(ctx, core.EventTypeDaemonConfig, cfgBytes); emitErr != nil {
			log.Printf("warn: daemon.Start: emit daemon_config: %v", emitErr)
		}
	}
}
