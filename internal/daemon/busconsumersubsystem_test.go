package daemon

import (
	"context"
	"encoding/json"
	"maps"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

type buspartCase struct {
	name    string
	subsys  projectconfig.SubsystemName
	prefix  string
	logWant string
}

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
	{
		name:    "SubscribeHub",
		subsys:  projectconfig.SubsystemSubscribeHub,
		prefix:  "subscribe-hub",
		logWant: "subscribe hub not constructed",
	},
}

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

func buspartWire(t *testing.T, bs *bootState) {
	t.Helper()
	if err := bs.wireSpendAndQueueConsumers(); err != nil {
		t.Fatalf("buspartWire: wireSpendAndQueueConsumers: %v", err)
	}
	if err := bs.wireWatchersAndObservers(context.Background()); err != nil {
		t.Fatalf("buspartWire: wireWatchersAndObservers: %v", err)
	}
}

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
  subscribe_hub:
    enabled: false
`

// The switches compose, and the daemon still reaches its work loop with all of
// them off. Pre-Seal wiring is the phase that runs BEFORE the bus is sealed, so
// a gate that returned an error rather than skipping a construction would fail
// the boot outright instead of failing one assertion.
func TestSubsystemPartition_BusConsumers_DisabledDaemonStillReachesWorkLoop(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, buspartAllDisabledYAML)
	logs := sockpartRunDaemon(t, projectDir, jsonlPath)

	for _, tc := range buspartCases {
		if !strings.Contains(logs, tc.logWant) {
			t.Errorf("boot log never reported %q; a silent partition is indistinguishable from a config that did not take effect", tc.logWant)
		}
	}
	if !strings.Contains(logs, "composition-root wiring audit") {
		t.Error("daemon did not reach startBackgroundLoops with these consumers switched off; the core must run without them")
	}
}

const buspartSubscribeOpConfigYAML = sockpartBaseConfigYAML + `
subsystems:
  subscribe_hub:
    enabled: false
`

const buspartSubscribeReplayAll = `{"op":"subscribe","since_event_id":"00000000-0000-0000-0000-000000000000"}`

func buspartDialSocket(t *testing.T, projectDir string, timeout time.Duration) net.Conn {
	t.Helper()
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
		if err == nil {
			return conn
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("buspartDialSocket: no listener on %s within %s: %v", sockPath, timeout, lastErr)
	return nil
}

func buspartProbeSubscribeOp(t *testing.T, yamlContent string) (first map[string]json.RawMessage, gotAny bool) {
	t.Helper()
	projectDir, jsonlPath := sockpartProjectDir(t, yamlContent)
	cfg := Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              sockpartStubBr(t),
		WorkflowModeDefault: core.WorkflowModeDot,
		LogWriter:           &sockpartSyncBuffer{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- StartForTesting(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(daemonExitHangBudget):
			t.Errorf("daemon.Start did not return within %s after context cancellation", daemonExitHangBudget)
		}
	})

	conn := buspartDialSocket(t, projectDir, 10*time.Second)
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write([]byte(buspartSubscribeReplayAll)); writeErr != nil {
		t.Fatalf("buspartProbeSubscribeOp: write: %v", writeErr)
	}
	if deadlineErr := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); deadlineErr != nil {
		t.Fatalf("buspartProbeSubscribeOp: SetReadDeadline: %v", deadlineErr)
	}
	var raw map[string]json.RawMessage
	if decodeErr := json.NewDecoder(conn).Decode(&raw); decodeErr != nil {
		return nil, false
	}
	return raw, true
}

func buspartRefusalText(obj map[string]json.RawMessage) (string, bool) {
	rawErr, ok := obj["error"]
	if !ok {
		return "", false
	}
	var text string
	if err := json.Unmarshal(rawErr, &text); err != nil || text == "" {
		return "", false
	}
	return text, true
}

// Default state: the hub is present, so the subscribe op does NOT answer with a
// SocketResponse — the connection becomes the stream. This is the half that
// proves the disabled case below is reading a real difference.
func TestSubsystemPartition_SubscribeHub_DefaultServesTheSubscribeOp(t *testing.T) {
	first, gotAny := buspartProbeSubscribeOp(t, sockpartBaseConfigYAML)
	if !gotAny {
		t.Fatal("the subscribe op replayed nothing with no subsystems: block; this probe cannot tell a served stream from a refusal unless the served case answers")
	}
	if text, refused := buspartRefusalText(first); refused {
		t.Errorf("the subscribe op was refused with %q and no subsystems: block; absent config must not disable anything", text)
	}
}

// Disabled state: the op fails LOUDLY. The daemon must say the handler is not
// registered, which is the honest answer for a capability that was switched off.
// It must not panic on a nil receiver, and it must not hang.
func TestSubsystemPartition_SubscribeHub_DisabledFailsTheSubscribeOpLoudly(t *testing.T) {
	first, gotAny := buspartProbeSubscribeOp(t, buspartSubscribeOpConfigYAML)
	if !gotAny {
		t.Fatal("the subscribe op wrote nothing with subsystems.subscribe_hub.enabled: false; a switched-off capability must be refused, never faked or left to hang")
	}
	text, refused := buspartRefusalText(first)
	if !refused {
		t.Fatalf("the subscribe op served a stream with subsystems.subscribe_hub.enabled: false; first line carried fields %v", slices.Sorted(maps.Keys(first)))
	}
	if !strings.Contains(text, "SubscribeHandler not registered") {
		t.Errorf("the subscribe op was refused with %q; an absent hub must be reported as an unregistered handler, not as some other failure", text)
	}
}
