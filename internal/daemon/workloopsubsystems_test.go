package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/sentinel"
)

type wlsubBus struct {
	mu      sync.Mutex
	n       map[core.EventType]int
	payload map[core.EventType][]byte
}

func (b *wlsubBus) Emit(_ context.Context, t core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n == nil {
		b.n = make(map[core.EventType]int)
	}
	b.n[t]++
	if b.payload == nil {
		b.payload = make(map[core.EventType][]byte)
	}
	b.payload[t] = append([]byte(nil), payload...)
	return nil
}

func (b *wlsubBus) EmitWithRunID(ctx context.Context, _ core.RunID, t core.EventType, p []byte) error {
	return b.Emit(ctx, t, p)
}

func (b *wlsubBus) count(t core.EventType) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n[t]
}

func (b *wlsubBus) lastPayload(t core.EventType) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.payload[t]...)
}

type wlsubCountingLedger struct {
	mu    sync.Mutex
	ready int
}

func (l *wlsubCountingLedger) readyCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ready
}

func (l *wlsubCountingLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ready++
	return []core.BeadRecord{{BeadID: "wlsub-1", Status: core.CoarseStatusOpen}}, nil
}

func (l *wlsubCountingLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *wlsubCountingLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *wlsubCountingLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *wlsubCountingLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

func wlsubSeedDashboardWorld(t *testing.T, root string) {
	t.Helper()
	lanes := filepath.Join(root, lanesJSONPath)
	if err := os.MkdirAll(filepath.Dir(lanes), 0o750); err != nil {
		t.Fatalf("wlsubSeedDashboardWorld: MkdirAll: %v", err)
	}
	if err := os.WriteFile(lanes, []byte(`{"lanes":[{"queue":"curated"}]}`), 0o600); err != nil {
		t.Fatalf("wlsubSeedDashboardWorld: write lanes.json: %v", err)
	}
}

const wlsubDashboardYAML = `
schema_version: 1
dashboard:
  max_staleness: 60s
`

// Default state: no subsystems: block → the gate is constructed, evaluates, and
// forces — exactly as before subsystem partitioning existed.
func TestSubsystemPartition_DashboardGate_DefaultRuns(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, wlsubDashboardYAML)
	wlsubSeedDashboardWorld(t, root)

	gate := newDashboardGateIfEnabled(pc, io.Discard)
	if gate == nil {
		t.Fatal("newDashboardGateIfEnabled = nil with no subsystems: block; want a constructed gate (absent config must not disable anything)")
	}

	bus := &wlsubBus{}
	gate.tick(context.Background(), root, bus, time.Now())

	if !gate.blockedQueueSet()["curated"] {
		t.Errorf("blockedQueueSet() = %v; want the captain-curated queue %q blocked by the never-written dashboard.json",
			gate.blockedQueueSet(), "curated")
	}
	if got := bus.count(core.EventTypeDashboardStale); got != 1 {
		t.Errorf("dashboard_stale emitted %d times; want 1 on the blocking transition edge", got)
	}
}

// Disabled state: the gate is ABSENT. Same world, same trip conditions — and
// nothing reads dashboard.json, nothing emits, nothing is withheld from dispatch.
func TestSubsystemPartition_DashboardGate_DisabledIsAbsent(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, wlsubDashboardYAML+`
subsystems:
  dashboard_gate:
    enabled: false
`)
	wlsubSeedDashboardWorld(t, root)

	gate := newDashboardGateIfEnabled(pc, io.Discard)
	if gate != nil {
		t.Fatal("newDashboardGateIfEnabled returned a gate with subsystems.dashboard_gate.enabled: false; it must be ABSENT, not constructed")
	}

	bus := &wlsubBus{}
	gate.tick(context.Background(), root, bus, time.Now())

	if got := gate.blockedQueueSet(); got != nil {
		t.Errorf("blockedQueueSet() = %v after the subsystem was switched off; want nil (no queue may be withheld by an absent gate)", got)
	}
	if got := bus.count(core.EventTypeDashboardStale); got != 0 {
		t.Errorf("dashboard_stale emitted %d times after the subsystem was switched off; want 0 (off means absent, not inert)", got)
	}
	if got := bus.count(core.EventTypeDashboardRefreshed); got != 0 {
		t.Errorf("dashboard_refreshed emitted %d times after the subsystem was switched off; want 0", got)
	}
}

func wlsubGovernorDeps(root string, bus *wlsubBus, ledger *wlsubCountingLedger) testRuntime {
	return testRuntime{
		env:    runloop.RunEnv{ProjectDir: root},
		ports:  runloop.RunPorts{Emitter: bus},
		ledger: ledger,
	}
}

func wlsubGovernorPort() governorPort {
	return governorPort{state: &sentinel.GovernorState{DaemonStartedAt: time.Now()}}
}

// Default state: no subsystems: block → the governor is constructed and its
// observe-mode evaluation runs, shelling out to `br ready` and emitting
// governor_signal, exactly as on every production daemon today.
func TestSubsystemPartition_MovementGovernor_DefaultRuns(t *testing.T) {
	t.Parallel()

	_, root := subpartLoadConfig(t, "schema_version: 1\n")
	bus := &wlsubBus{}
	ledger := &wlsubCountingLedger{}
	deps := wlsubGovernorDeps(root, bus, ledger)
	governor := newMovementGovernorIfEnabled(wlsubGovernorPort(), true, io.Discard)
	if governor == nil {
		t.Fatal("newMovementGovernorIfEnabled = nil with no subsystems: block; want a constructed governor (absent config must not disable anything)")
	}

	governor.tick(context.Background(), governorInputPort{projectDir: deps.env.ProjectDir, ledger: deps.ledger}, schedulePort{}, newDispatchGatesPortFromDeps(deps))

	if got := ledger.readyCalls(); got != 1 {
		t.Errorf("brAdapter.Ready called %d times; want 1 (the governor's per-evaluation shell-out must happen when it is enabled)", got)
	}
	if got := bus.count(core.EventTypeGovernorSignal); got != 1 {
		t.Errorf("governor_signal emitted %d times; want 1", got)
	}
	if _, err := (core.Event{Type: core.EventTypeGovernorSignal, Payload: bus.lastPayload(core.EventTypeGovernorSignal)}).DecodePayload(); err != nil {
		t.Fatalf("emitted governor_signal does not decode through the production registry: %v", err)
	}

	governor.tick(context.Background(), governorInputPort{projectDir: deps.env.ProjectDir, ledger: deps.ledger}, schedulePort{}, newDispatchGatesPortFromDeps(deps))
	if got := ledger.readyCalls(); got != 1 {
		t.Errorf("brAdapter.Ready called %d times after a second immediate tick; want still 1 (the eval cadence must suppress it)", got)
	}
	if got := bus.count(core.EventTypeGovernorSignal); got != 1 {
		t.Errorf("governor_signal emitted %d times after a second immediate tick; want still 1", got)
	}
}

// Disabled state: the governor is ABSENT. No state, no `br ready` shell-out, no
// events.jsonl scan, no governor_signal — no matter how many ticks the loop takes.
func TestSubsystemPartition_MovementGovernor_DisabledIsAbsent(t *testing.T) {
	t.Parallel()

	_, root := subpartLoadConfig(t, `
schema_version: 1
subsystems:
  movement_governor:
    enabled: false
`)
	bus := &wlsubBus{}
	ledger := &wlsubCountingLedger{}
	deps := wlsubGovernorDeps(root, bus, ledger)
	governor := newMovementGovernorIfEnabled(wlsubGovernorPort(), false, io.Discard)
	if governor != nil {
		t.Fatal("newMovementGovernorIfEnabled returned a governor with subsystems.movement_governor.enabled: false; it must be ABSENT, not constructed")
	}

	for range 5 {
		governor.tick(context.Background(), governorInputPort{projectDir: deps.env.ProjectDir, ledger: deps.ledger}, schedulePort{}, newDispatchGatesPortFromDeps(deps))
	}
	if governor.halted() {
		t.Error("halted() = true on an absent governor; only the governor itself can request the G-liveness halt")
	}

	if got := ledger.readyCalls(); got != 0 {
		t.Errorf("brAdapter.Ready called %d times after the subsystem was switched off; want 0 (off means absent, not inert)", got)
	}
	if got := bus.count(core.EventTypeGovernorSignal); got != 0 {
		t.Errorf("governor_signal emitted %d times after the subsystem was switched off; want 0", got)
	}
}

// The dispatch gate must vanish with the subsystem, not outlive it.
//
// The "sentinel" queue block has one writer — ACT mode — plus a boot-time
// restore of that writer's ack file (LoadDecisionAckState, EV-043a), and one
// clearer: ACT mode's dormant branch. If the READ survived the subsystem being
// switched off, a single stale pending ack file would wedge every dispatch on
// every subsequent boot with no code path able to open the gate. This pins the
// decision that the readers are gated too.
func TestSubsystemPartition_MovementGovernor_DisabledReleasesDispatchGate(t *testing.T) {
	t.Parallel()

	blocker := NewDecisionBlocker()
	blocker.AddQueueBlock(sentinelSubjectIDACT, "stale-trip-token")

	enabledPC, enabledRoot := subpartLoadConfig(t, "schema_version: 1\n")
	enabledDeps := wlsubGovernorDeps(enabledRoot, &wlsubBus{}, &wlsubCountingLedger{})
	_ = enabledPC
	enabledDeps.dispatchGates = newDispatchGatesPort(nil, nil, nil, blocker)

	enabled := newMovementGovernorIfEnabled(wlsubGovernorPort(), true, io.Discard)
	if !enabled.dispatchBlocked(newDispatchGatesPortFromDeps(enabledDeps)) {
		t.Fatal("dispatchBlocked = false with the governor enabled and a pending sentinel trip restored; the FW3 queue gate must still hold dispatch")
	}

	disabledPC, disabledRoot := subpartLoadConfig(t, `
schema_version: 1
subsystems:
  movement_governor:
    enabled: false
`)
	disabledDeps := wlsubGovernorDeps(disabledRoot, &wlsubBus{}, &wlsubCountingLedger{})
	_ = disabledPC
	disabledDeps.dispatchGates = newDispatchGatesPort(nil, nil, nil, blocker)

	disabled := newMovementGovernorIfEnabled(wlsubGovernorPort(), false, io.Discard)
	if disabled.dispatchBlocked(newDispatchGatesPortFromDeps(disabledDeps)) {
		t.Error("dispatchBlocked = true with subsystems.movement_governor.enabled: false; a switched-off subsystem must not hold the dispatcher shut through a gate nothing can open")
	}
}

const wlsubMalformedSentinelYAML = "schema_version: 1\nsentinel:\n  suppression_ttl: \"not-a-duration\"\n"

func TestSubsystemPartition_MovementGovernor_DisabledSkipsFatalBootConfig(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, wlsubMalformedSentinelYAML+
		"subsystems:\n  movement_governor:\n    enabled: false\n")
	port, enabled, err := newGovernorPort(Config{ProjectDir: root, ProjectCfg: pc}, time.Now())
	if err != nil {
		t.Fatalf("newGovernorPort = %v with subsystems.movement_governor.enabled: false; want nil — "+
			"a switched-off subsystem must not refuse the daemon's boot over config it no longer reads", err)
	}
	if enabled || port.state != nil {
		t.Error("disabled movement governor constructed a port")
	}
}

func TestSubsystemPartition_MovementGovernor_EnabledStillFailsOnBadConfig(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, wlsubMalformedSentinelYAML)
	_, enabled, err := newGovernorPort(Config{ProjectDir: root, ProjectCfg: pc}, time.Now())
	if !enabled || err == nil {
		t.Fatal("newGovernorPort accepted a malformed sentinel: block with the subsystem ENABLED; " +
			"the partition gate must not double as an error suppressor")
	}
}

func TestSubsystemPartition_MovementGovernor_NoConfigStillObservesWithoutLivenessHalt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	port, enabled, err := newGovernorPort(Config{ProjectDir: root}, time.Now())
	if err != nil {
		t.Fatalf("newGovernorPort with no config: %v", err)
	}
	if !enabled || port.state == nil {
		t.Fatal("no-config boot did not construct an enabled governor port")
	}
	if port.config.LivenessNoProgressN != 0 {
		t.Fatalf("no-config liveness threshold = %d, want 0", port.config.LivenessNoProgressN)
	}

	bus := &wlsubBus{}
	ledger := &wlsubCountingLedger{}
	deps := wlsubGovernorDeps(root, bus, ledger)
	governor := newMovementGovernorIfEnabled(port, enabled, io.Discard)
	governor.tick(context.Background(), governorInputPort{projectDir: deps.env.ProjectDir, ledger: deps.ledger}, schedulePort{}, newDispatchGatesPortFromDeps(deps))

	if got := ledger.readyCalls(); got != 1 {
		t.Errorf("brAdapter.Ready called %d times, want 1 so no-config still observes", got)
	}
	if got := bus.count(core.EventTypeGovernorSignal); got != 1 {
		t.Errorf("governor_signal emitted %d times, want 1 so no-config still ticks", got)
	}
	if governor.halted() {
		t.Error("no-config zero threshold armed a liveness halt")
	}

	port.mode = "act"
	actGovernor := newMovementGovernorIfEnabled(port, enabled, io.Discard)
	actGovernor.tick(context.Background(), governorInputPort{projectDir: deps.env.ProjectDir, ledger: deps.ledger}, schedulePort{}, newDispatchGatesPortFromDeps(deps))
	if actGovernor.halted() {
		t.Error("zero liveness threshold armed a halt in ACT mode")
	}
	if got := bus.count(core.EventTypeLivenessHalt); got != 0 {
		t.Errorf("liveness_halt emitted %d times with zero threshold, want 0", got)
	}
}
