package daemon

// composition_registry_hkndysh.go — typed composition-root pre-Seal subscriber registry.
//
// This file replaces the hand-maintained structural scan in SC-6 sub-test 3 with
// a typed registry that maps subsystem names to the ConsumerIDs they register via
// Subscribe. SC-6 (spawn_path_sc6_hknx5wu_test.go) enumerates this registry and
// verifies each entry is observed in the bus before Seal — failures name the
// subsystem, not just the count.
//
// When a new subsystem calls Subscribe in daemon.Start:
//   1. Add a PreSealSubsystem const below.
//   2. Add a RequiredPreSealSubscribers entry (or NotifyStreamSubscribers if conditional).
//   3. SC-6 will enforce the contract automatically.
//
// Bead ref: hk-ndysh.

// PreSealSubsystem is a typed name for a daemon subsystem that MUST subscribe
// to the event bus before bus.Seal() is called.
type PreSealSubsystem string

const (
	// PreSealSubsystemHandlerPausePolicy is the HandlerPausePolicyGoroutine
	// (agent_rate_limit_status + budget_exhausted consumers; hk-37zy8).
	PreSealSubsystemHandlerPausePolicy PreSealSubsystem = "HandlerPausePolicyGoroutine"

	// PreSealSubsystemQueueOpConsumer is the QueueOperatorEventConsumer
	// (operator_pause_status + operator_resuming consumers; hk-7urls).
	PreSealSubsystemQueueOpConsumer PreSealSubsystem = "QueueOperatorEventConsumer"

	// PreSealSubsystemSubscribeHub is the SubscribeHub wildcard observer
	// that fans events to "subscribe" socket-op connections (hk-6ynv4).
	PreSealSubsystemSubscribeHub PreSealSubsystem = "SubscribeHub"

	// PreSealSubsystemNotifyStream is the NotifyStreamConsumer, wired only
	// when cfg.NotifyStream is set (hk-ibilr).
	PreSealSubsystemNotifyStream PreSealSubsystem = "NotifyStreamConsumer"

	// PreSealSubsystemStaleWatcher is the StaleWatcher wildcard observer that
	// emits run_stale when an active run produces no event for M minutes (hk-wkzlc).
	PreSealSubsystemStaleWatcher PreSealSubsystem = "StaleWatcher"

	// PreSealSubsystemSpendMeter is the DaemonSpendMeter (run_started max-runs
	// counter + budget_accrual bytes proxy; hk-k3f8g).
	PreSealSubsystemSpendMeter PreSealSubsystem = "DaemonSpendMeter"

	// PreSealSubsystemReviewGateAnomaly is the ReviewGateAnomalyWatcher
	// (bead_closed + reviewer_verdict consumers; hk-tnmjy).
	PreSealSubsystemReviewGateAnomaly PreSealSubsystem = "ReviewGateAnomalyWatcher"

	// PreSealSubsystemBandwidthTunerBackstop is the bandwidthTunerBackstop that
	// subscribes to agent_rate_limited bus events as a rate-limit backstop (hk-81n9r).
	PreSealSubsystemBandwidthTunerBackstop PreSealSubsystem = "bandwidthTunerBackstop"
)

// SubscribeContract declares the ConsumerIDs a subsystem registers via Subscribe.
// SC-6 verifies each ID appears in the bus's pre-Seal subscription list.
type SubscribeContract struct {
	// ConsumerIDs are the core.Subscription.ConsumerID values this subsystem
	// registers. Every listed ID must be present in the bus before Seal.
	ConsumerIDs []string
}

// RequiredPreSealSubscribers is the canonical set of subsystems that MUST
// subscribe regardless of daemon configuration. SC-6 asserts all entries are
// satisfied before bus.Seal() is called.
//
// Bead ref: hk-ndysh.
var RequiredPreSealSubscribers = map[PreSealSubsystem]SubscribeContract{
	PreSealSubsystemHandlerPausePolicy: {ConsumerIDs: []string{
		"handler-pause-policy-rate-limit-claude-code",
		"handler-pause-policy-budget-exhausted-claude-code",
	}},
	PreSealSubsystemQueueOpConsumer: {ConsumerIDs: []string{
		"queue-operator-drain-pause",
		"queue-operator-drain-resume",
	}},
	PreSealSubsystemSubscribeHub: {ConsumerIDs: []string{
		"subscribe-hub",
	}},
	PreSealSubsystemStaleWatcher: {ConsumerIDs: []string{
		"stale-watcher",
	}},
	PreSealSubsystemSpendMeter: {ConsumerIDs: []string{
		"daemon-spend-meter-run-started",
		"daemon-spend-meter-budget-accrual",
	}},
	PreSealSubsystemReviewGateAnomaly: {ConsumerIDs: []string{
		"review-gate-anomaly-bead-closed",
		"review-gate-anomaly-reviewer-verdict",
	}},
	PreSealSubsystemBandwidthTunerBackstop: {ConsumerIDs: []string{
		"bandwidth-tuner-rate-limit-backstop",
	}},
}

// NotifyStreamSubscribers lists subsystems that subscribe only when
// cfg.NotifyStream is non-nil. SC-6 checks these in the with-notify-stream path.
//
// Bead ref: hk-ndysh.
var NotifyStreamSubscribers = map[PreSealSubsystem]SubscribeContract{
	PreSealSubsystemNotifyStream: {ConsumerIDs: []string{
		"notify-stream-run-started",
		"notify-stream-merge-status",
		"notify-stream-run-completed",
		"notify-stream-run-failed",
	}},
}
