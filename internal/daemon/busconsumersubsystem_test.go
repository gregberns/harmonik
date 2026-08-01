package daemon

// busconsumersubsystem_test.go — subsystem partitioning of the pre-Seal bus
// consumers the daemon wires in wireSpendAndQueueConsumers and
// wireWatchersAndObservers.
//
// Both states are driven through the REAL config edge: a .harmonik/config.yaml
// written to disk and read by projectconfig.LoadProjectConfig. Nothing here
// hand-builds a SubsystemsConfig, because the thing under test is the whole path
// from operator YAML to construction seam.
//
// "Off" is asserted as ABSENT FROM THE BUS, not as a boolean. A bus consumer has
// no goroutine of its own — it runs on the bus worker pool when an event it
// matches arrives — so the runtime fact that separates present from absent is
// whether a subscription carrying its ConsumerID is registered. Reading
// eventbus.BusSubscribedConsumerIDs is that fact, read out of the live bus
// rather than inferred from a flag. A constructed-but-inert consumer still holds
// a subscription and would fail these tests.
//
// The DEFAULT half of each pair is what keeps the disabled half from being
// vacuous: it asserts the same ConsumerID IS on the bus with no subsystems:
// block, so an absence caused by a typo in the prefix cannot pass as a partition.
//
// Helper prefix: buspart.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// buspartCase describes one gated bus consumer: the operator-facing switch, the
// ConsumerID prefix its subscriptions carry, and the partition line the daemon
// must print when it is switched off.
type buspartCase struct {
	name    string
	subsys  projectconfig.SubsystemName
	prefix  string
	logWant string
}

// buspartCases is the set of pre-Seal bus consumers this file gates.
//
// PerQueueSpendMeter and QueueOperatorEventConsumer are deliberately NOT here:
// both hold queue state, so switching either off changes queue behaviour and
// each needs its own decision. StaleWatcher and QuiesceArbiter are not here
// either: both are reached from inside injectWorkLoopDeps and
// startBackgroundLoops, which the composition-root step rewrites, so gating them
// is that step's work rather than this one's.
var buspartCases = []buspartCase{
	{
		name:    "HandlerPausePolicyGoroutine",
		subsys:  projectconfig.SubsystemHandlerPausePolicy,
		prefix:  "handler-pause-policy-",
		logWant: "handler-pause policy not constructed",
	},
	{
		name:    "DaemonSpendMeter",
		subsys:  projectconfig.SubsystemDaemonSpendMeter,
		prefix:  "daemon-spend-meter-",
		logWant: "daemon spend meter not constructed",
	},
	{
		name:    "ReviewGateAnomalyWatcher",
		subsys:  projectconfig.SubsystemReviewGateAnomaly,
		prefix:  "review-gate-anomaly-",
		logWant: "review-gate anomaly watcher not constructed",
	},
	{
		name:    "CatBL2Handler",
		subsys:  projectconfig.SubsystemLedgerImportRecovery,
		prefix:  "cat-bl2-ledger-import-failure",
		logWant: "bead-ledger import recovery not constructed",
	},
}

// buspartBootState builds a bootState carrying a REAL event bus, with its
// ProjectCfg loaded from yamlContent through the production config loader.
//
// BrPath is set because the Cat-BL2 handler is wired only when ProjectDir and
// BrPath are both non-empty. Without it that consumer would be absent in BOTH
// states and its disabled test would prove nothing. JSONLLogPath is left empty
// so no event log is opened — the bus is still real, it just writes nowhere.
func buspartBootState(t *testing.T, yamlContent string) (*bootState, *sockpartSyncBuffer) {
	t.Helper()
	pc, root := subpartLoadConfig(t, yamlContent)
	logBuf := &sockpartSyncBuffer{}
	bs := &bootState{cfg: Config{
		ProjectDir: root,
		ProjectCfg: pc,
		BrPath:     sockpartStubBr(t),
		LogWriter:  logBuf,
	}}
	if _, err := bs.constructBusAndRegistries(); err != nil {
		t.Fatalf("buspartBootState: constructBusAndRegistries: %v", err)
	}
	return bs, logBuf
}

// buspartWire runs both pre-Seal wiring phases, which is where every consumer in
// buspartCases is constructed and subscribed. The bus is left unsealed. These
// tests read the subscription list. They do not emit.
func buspartWire(t *testing.T, bs *bootState) {
	t.Helper()
	if err := bs.wireSpendAndQueueConsumers(); err != nil {
		t.Fatalf("buspartWire: wireSpendAndQueueConsumers: %v", err)
	}
	if err := bs.wireWatchersAndObservers(context.Background()); err != nil {
		t.Fatalf("buspartWire: wireWatchersAndObservers: %v", err)
	}
}

// buspartSubscribed reports whether the bus carries a subscription whose
// ConsumerID starts with prefix. A consumer that registers several
// subscriptions shares one prefix, so this answers "is this consumer on the
// bus" rather than "how many patterns did it register".
func buspartSubscribed(t *testing.T, bs *bootState, prefix string) bool {
	t.Helper()
	ids := eventbus.BusSubscribedConsumerIDs(bs.bus)
	if ids == nil {
		t.Fatal("buspartSubscribed: the bus does not report its consumer ids; these tests cannot observe absence")
	}
	for _, id := range ids {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// Default state: no subsystems: block → every gated consumer is on the bus,
// exactly as before partitioning existed. This is the half that makes the
// disabled half below mean something.
func TestSubsystemPartition_BusConsumers_DefaultSubscribe(t *testing.T) {
	t.Parallel()

	bs, logBuf := buspartBootState(t, "schema_version: 1\n")
	buspartWire(t, bs)

	for _, tc := range buspartCases {
		if !buspartSubscribed(t, bs, tc.prefix) {
			t.Errorf("%s: no subscription with ConsumerID prefix %q on the bus with no subsystems: block; absent config must not disable anything",
				tc.name, tc.prefix)
		}
		if strings.Contains(logBuf.String(), string(tc.subsys)) {
			t.Errorf("%s: the partition was announced with no subsystems: block; log = %q", tc.name, logBuf.String())
		}
	}
}

// Disabled state: the consumer is ABSENT — never constructed, so no subscription
// carrying its ConsumerID reaches the bus. Each case switches off ONE subsystem,
// so a gate that over-reaches and removes a neighbour fails here.
func TestSubsystemPartition_BusConsumers_DisabledIsAbsentFromBus(t *testing.T) {
	t.Parallel()

	for _, tc := range buspartCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bs, logBuf := buspartBootState(t, bootpartDisabledYAML(tc.subsys))
			buspartWire(t, bs)

			if buspartSubscribed(t, bs, tc.prefix) {
				t.Errorf("a subscription with ConsumerID prefix %q is on the bus with subsystems.%s.enabled: false; off means NEVER CONSTRUCTED, not constructed-and-inert",
					tc.prefix, tc.subsys)
			}
			if !strings.Contains(logBuf.String(), tc.logWant) {
				t.Errorf("the daemon did not report the partition; a silent partition is indistinguishable from a config that did not take effect. want %q, log = %q",
					tc.logWant, logBuf.String())
			}
			// Every OTHER gated consumer must stay on the bus: one switch
			// partitions one subsystem.
			for _, other := range buspartCases {
				if other.subsys == tc.subsys {
					continue
				}
				if !buspartSubscribed(t, bs, other.prefix) {
					t.Errorf("%s left the bus when only %s was switched off; the gate reaches further than its own subsystem",
						other.name, tc.subsys)
				}
			}
		})
	}
}

// --- all of them at once, through a real daemon boot -------------------------

// buspartAllDisabledYAML switches off every bus consumer this file gates, as an
// operator would write it in one block.
const buspartAllDisabledYAML = sockpartBaseConfigYAML + `
subsystems:
  handler_pause_policy:
    enabled: false
  daemon_spend_meter:
    enabled: false
  review_gate_anomaly:
    enabled: false
  ledger_import_recovery:
    enabled: false
`

// The switches compose, and the daemon still reaches its work loop with all of
// them off. Pre-Seal wiring is the phase that runs BEFORE the bus is sealed, so
// a gate that returned an error rather than skipping a construction would fail
// the boot outright instead of failing one assertion.
func TestSubsystemPartition_BusConsumers_DisabledDaemonStillReachesWorkLoop(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, buspartAllDisabledYAML)
	logs := sockpartRunDaemon(t, projectDir, jsonlPath, 1500*time.Millisecond)

	for _, tc := range buspartCases {
		if !strings.Contains(logs, tc.logWant) {
			t.Errorf("boot log never reported %q; a silent partition is indistinguishable from a config that did not take effect", tc.logWant)
		}
	}
	if !strings.Contains(logs, "composition-root wiring audit") {
		t.Error("daemon did not reach startBackgroundLoops with these consumers switched off; the core must run without them")
	}
}
