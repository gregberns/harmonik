package daemon

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
)

// bootState threads the shared singletons constructed across the daemon
// composition-root phases (startWithHooks) so each phase can be extracted into
// its own helper without a 20-parameter signature. Early phases WRITE the
// fields; later phases READ them. Extracted for giant-retirement boot-config
// (B3+). A missed hand-off surfaces as a nil-deref at boot, so each field's
// producer/consumer lifecycle is reviewed per slice.
//
// It deliberately does NOT own the two lifetime-spanning defers (pidfile,
// jsonlWriter): those stay in the outer startWithHooks shell so they fire at
// daemon-return, not when a phase helper returns (seam-1).
type bootState struct {
	cfg   Config
	hooks daemonTestHooks

	// P4 (constructBusAndRegistries) outputs.
	bus                     eventbus.EventBus
	clockRegressionDetected bool
	qs                      *QueueStore
	handlerPauseCtrl        *HandlerPauseController
	sharedRunRegistry       *RunRegistry
	pollGate                *PollGate

	// P5 (wireWatchersAndObservers) outputs consumed by later phases.
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
	crewHandler         CrewHandler
	crewIdleReaper      *CrewIdleReaper
	branchReapWatcher   *BranchReapWatcher
}

// constructBusAndRegistries performs PL-005 step 0 P4: it opens the JSONL event
// log, seeds the EventIDGenerator from the persisted HWM, constructs the
// EventBus, and instantiates the pre-Seal core registries (QueueStore,
// HandlerPauseController, RunRegistry) plus the shared poll gate.
//
// It RETURNS the *eventbus.JSONLWriter (seam-1) so the OUTER shell owns
// `defer jsonlWriter.Close()`: a helper-scope defer would close the event log
// immediately after wiring, before the work loop runs (boot-breaking). Returns
// (nil, nil) writer when no JSONL path is configured.
func (bs *bootState) constructBusAndRegistries() (*eventbus.JSONLWriter, error) {
	cfg := bs.cfg

	// RedactionRegistry (HC-032; hk-8i31.83). No seed patterns — handlers call
	// RegisterPattern when they are wired (PL-005 step 0 semantics).
	registry := handlercontract.NewRedactionRegistry()

	// Open the JSONL event log when a path is configured (hk-8mup.63). The dir
	// must exist before Start is called; daemon callers own the mkdir-p.
	var jsonlWriter *eventbus.JSONLWriter
	if cfg.JSONLLogPath != "" {
		var openErr error
		jsonlWriter, openErr = eventbus.OpenJSONLWriter(cfg.JSONLLogPath)
		if openErr != nil {
			return nil, fmt.Errorf("daemon.Start: open JSONL log %q: %w",
				filepath.Base(cfg.JSONLLogPath), openErr)
		}
	}

	// EV-002c: read the persisted event-ID HWM and seed the generator so all
	// post-restart event_ids are strictly greater than pre-restart ones. When
	// ProjectDir is empty (unit-test mode) skip HWM I/O and seed from wall clock.
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

	// Instantiate the EventBus (EV-035, EV-002c). Subscribers MUST be registered
	// before Seal (EV-009); the outer shell seals after the wiring helpers run.
	bs.bus = eventbus.NewBusImplWithWriterAndHWM(registry, jsonlWriter, hwmGen, hwmPath, cfg.JSONLLogPath)

	// PL-005 step 0 (hk-m0k0a, hk-37zy8, hk-7urls): construct QueueStore,
	// HandlerPauseController, RunRegistry, and the shared poll gate at the
	// composition root so all are available pre-Seal for their Subscribe calls.
	// When cfg.QueueStore is non-nil the caller-supplied instance is used
	// directly (hk-8jh26 Fix 2). The pause controller's persistFn is patched
	// later (post-Seal, when ProjectDir is checked).
	qs := cfg.QueueStore
	if qs == nil {
		qs = newQueueStore()
	}
	bs.qs = qs
	bs.handlerPauseCtrl = NewHandlerPauseController(bs.bus, nil)
	bs.sharedRunRegistry = NewRunRegistry()
	// pollGate: shared INACTIVE gate for StaleWatcher and BandwidthTuner (SS-007,
	// hk-w6q7 P2-b). Zero value is ungated so watchers run in unit-test mode.
	bs.pollGate = &PollGate{}

	return jsonlWriter, nil
}

// wireSpendAndQueueConsumers subscribes the spend/queue-operator/notify/subscribe
// consumers to the bus BEFORE Seal (EV-009): HandlerPausePolicyGoroutine,
// DaemonSpendMeter, PerQueueSpendMeter, QueueOperatorEventConsumer, the optional
// NotifyStreamConsumer, and the SubscribeHub. Part of the P5 pre-Seal wiring.
func (bs *bootState) wireSpendAndQueueConsumers() error {
	cfg := bs.cfg
	bus := bs.bus

	// HandlerPausePolicyGoroutine (hk-37zy8): the first production subscriber.
	pausePolicy := NewHandlerPausePolicyGoroutine(HandlerPausePolicyConfig{
		AgentType:  core.AgentTypeClaudeCode,
		Controller: bs.handlerPauseCtrl,
		Registry:   bs.sharedRunRegistry,
	})
	if subscribeErr := pausePolicy.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: HandlerPausePolicyGoroutine.Subscribe: %w", subscribeErr)
	}

	// DaemonSpendMeter (hk-k3f8g): daemon-wide run-count + output-bytes ceiling.
	spendMeter := NewDaemonSpendMeter(bus)
	if subscribeErr := spendMeter.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: DaemonSpendMeter.Subscribe: %w", subscribeErr)
	}
	if bs.hooks.spendMeterObserver != nil {
		bs.hooks.spendMeterObserver(spendMeter)
	}

	// PerQueueSpendMeter (NQ-X1, hk-tigaf.11): the stricter per-queue ceiling.
	perQueueSpendMeter := NewPerQueueSpendMeter(bs.sharedRunRegistry, bs.qs, cfg.ProjectDir)
	if subscribeErr := perQueueSpendMeter.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: PerQueueSpendMeter.Subscribe: %w", subscribeErr)
	}

	// QueueOperatorEventConsumer (hk-7urls): active ↔ paused-by-drain transitions.
	queueOpConsumer := NewQueueOperatorEventConsumer(QueueOperatorEventConsumerConfig{
		QueueStore: bs.qs,
		ProjectDir: cfg.ProjectDir,
		Bus:        bus,
	})
	if subscribeErr := queueOpConsumer.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: QueueOperatorEventConsumer.Subscribe: %w", subscribeErr)
	}

	// Per-bead completion notifier when --notify-stream is set (hk-ibilr).
	if cfg.NotifyStream != nil {
		notifyConsumer := NewNotifyStreamConsumer(cfg.NotifyStream)
		if subscribeErr := notifyConsumer.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: NotifyStreamConsumer.Subscribe: %w", subscribeErr)
		}
	}

	// SubscribeHub (hk-6ynv4): long-lived wildcard observer fanning events out to
	// "subscribe" socket-op connections. Always registered; dormant until used.
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

	return nil
}

// wireWatchersAndObservers subscribes the remaining pre-Seal watchers/observers
// to the bus (EV-009): StaleWatcher, ReviewGateAnomalyWatcher, the bandwidth
// tuner backstop, the QuiesceArbiter, the substrate launch-timeout diagnostic
// hooks, the Cat-BL2 ledger-recovery handler, and finally the test-only bus
// observer. It is the LAST P5 helper before the outer shell calls bus.Seal().
func (bs *bootState) wireWatchersAndObservers(ctx context.Context) error {
	cfg := bs.cfg
	bus := bs.bus

	// StaleWatcher (hk-wkzlc): emits run_stale. StartWatcher runs post-Seal.
	bs.staleWatcher = NewStaleWatcher(StaleWatcherConfig{
		SubscribeBus: bus,
		Emitter:      bus,
		Registry:     bs.sharedRunRegistry,
		Gate:         bs.pollGate,
	})
	if subscribeErr := bs.staleWatcher.Subscribe(); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: StaleWatcher.Subscribe: %w", subscribeErr)
	}

	// ReviewGateAnomalyWatcher (hk-tnmjy): fires review_gate_anomaly.
	reviewGateWatcher := NewReviewGateAnomalyWatcher(bus)
	if subscribeErr := reviewGateWatcher.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: ReviewGateAnomalyWatcher.Subscribe: %w", subscribeErr)
	}

	// Bandwidth-tuner rate-limit backstop (hk-lqtzq). Two-phase: Subscribe here
	// (pre-Seal, EV-009); SetTuner runs post-Seal where concurrencyCtrl exists.
	bs.tunerBackstop = &bandwidthTunerBackstop{}
	if subscribeErr := bs.tunerBackstop.Subscribe(bus); subscribeErr != nil {
		return fmt.Errorf("daemon.Start: bandwidth-tuner backstop subscribe: %w", subscribeErr)
	}
	bs.tunerBackstop.SetRunRegistry(bs.sharedRunRegistry) // PI-073: isolate Pi events

	// QuiesceArbiter (hk-jeby): Subscribe wake triggers pre-Seal; Start post-Seal
	// (inside if cfg.BrPath != ""). Constructed + subscribed even in unit-test
	// mode; project-dir-dependent fields are nil/empty-guarded.
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

	// Substrate launch-timeout diagnostic hooks (hk-oihnf): now that the bus is
	// live, install hooks that emit non-run-scoped diagnostic events onto it.
	if hookSetter, ok := cfg.Substrate.(substrateDiagnosticHookSetter); ok {
		hookSetter.setDiagnosticHooks(
			func(waited time.Duration, inUse, capSize int) {
				emitSpawnCapBlocked(ctx, bus, core.RunID{}, waited, inUse, capSize)
			},
			func(waited time.Duration) {
				emitTmuxNewWindowTimeout(ctx, bus, core.RunID{}, waited)
			},
		)
	}

	// Cat-BL2 reactive ledger-import-failure handler (§8.BL2, hk-k7va9). Only
	// wired when ProjectDir + BrPath are set (production pairing).
	if cfg.ProjectDir != "" && cfg.BrPath != "" {
		catBL2Handler := NewCatBL2Handler(CatBL2HandlerConfig{
			ProjectDir: cfg.ProjectDir,
			BrPath:     cfg.BrPath,
			Emitter:    bus,
		})
		if subscribeErr := catBL2Handler.Subscribe(bus); subscribeErr != nil {
			return fmt.Errorf("daemon.Start: CatBL2Handler.Subscribe: %w", subscribeErr)
		}
	}

	// Test-only observer (StartForTesting): inspect bus subscription state before
	// Seal locks it. Production Start passes a zero-value daemonTestHooks.
	if bs.hooks.busObserver != nil {
		bs.hooks.busObserver(bus)
	}

	return nil
}
