package daemon

// workloopsubsystems_test.go — subsystem partitioning of the two non-core
// subsystems that used to be welded inline into runWorkLoop: the dashboard
// forcing gate and the sentinel movement governor.
//
// Both states are driven through the REAL config edge — a .harmonik/config.yaml
// written to disk and read by projectconfig.LoadProjectConfig (subpartLoadConfig,
// shared with reconciliationsubsystem_test.go). Nothing here hand-builds a
// SubsystemsConfig, because the thing under test is the whole path from operator
// YAML to construction seam.
//
// "Off" is asserted BEHAVIOURALLY, not just as a nil pointer. Each subsystem is
// given a world that makes it do something loud and observable when present — a
// dashboard that has never been written (so the gate MUST trip and block a
// curated queue), a `br ready` adapter that counts its calls (so the governor
// MUST shell out and emit governor_signal) — and the disabled case asserts that
// nothing at all happened. A constructed-but-inert subsystem would still make
// those calls; an absent one cannot.
//
// Helper prefix: wlsub — work-loop subsystems, the behaviour these helpers serve.

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
	"github.com/gregberns/harmonik/internal/sentinel"
)

// ─────────────────────────────────────────────────────────────────────────────
// wlsub* stubs
// ─────────────────────────────────────────────────────────────────────────────

// wlsubBus is a recording EventEmitter. It counts events by type so a test can
// assert both "this fired" and "nothing fired at all".
type wlsubBus struct {
	mu sync.Mutex
	n  map[core.EventType]int
}

func (b *wlsubBus) Emit(_ context.Context, t core.EventType, _ []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n == nil {
		b.n = make(map[core.EventType]int)
	}
	b.n[t]++
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

// wlsubCountingLedger is a beadLedger whose only job is to report how many times
// the governor shelled out to `br ready`. That call is the governor's dominant
// per-evaluation cost and the sharpest evidence that it ran.
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

// ─────────────────────────────────────────────────────────────────────────────
// Dashboard forcing gate — subsystems.dashboard_gate
// ─────────────────────────────────────────────────────────────────────────────

// wlsubSeedDashboardWorld writes the two files that make the forcing gate trip:
// a dashboard: block with a max_staleness (without it the gate is not
// "Configured" and stays off by data), and a lanes.json naming one captain-
// curated queue (the scope of the block). dashboard.json is deliberately NOT
// written — never-written is treated as maximally stale.
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
	gate.tick(context.Background(), workLoopDeps{projectDir: root, bus: bus}, time.Now())

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
	// The loop calls these unconditionally; on an absent gate they must be inert.
	gate.tick(context.Background(), workLoopDeps{projectDir: root, bus: bus}, time.Now())

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

// ─────────────────────────────────────────────────────────────────────────────
// Sentinel movement governor — subsystems.movement_governor
// ─────────────────────────────────────────────────────────────────────────────

// wlsubGovernorDeps builds the deps the governor reads, seeded the way
// bootState.seedGovernorDeps seeds them in production: a non-nil governorState
// and the default (observe) mode, which is what every production daemon runs.
func wlsubGovernorDeps(root string, bus *wlsubBus, ledger *wlsubCountingLedger) workLoopDeps {
	return workLoopDeps{
		projectDir:    root,
		bus:           bus,
		brAdapter:     ledger,
		governorState: &sentinel.GovernorState{DaemonStartedAt: time.Now()},
		sentinelMode:  "", // "" and "observe" are the same branch
	}
}

// Default state: no subsystems: block → the governor is constructed and its
// observe-mode evaluation runs, shelling out to `br ready` and emitting
// governor_signal, exactly as on every production daemon today.
func TestSubsystemPartition_MovementGovernor_DefaultRuns(t *testing.T) {
	t.Parallel()

	pc, root := subpartLoadConfig(t, "schema_version: 1\n")
	bus := &wlsubBus{}
	ledger := &wlsubCountingLedger{}
	deps := wlsubGovernorDeps(root, bus, ledger)
	deps.projectCfg = pc

	governor := newMovementGovernorIfEnabled(deps, io.Discard)
	if governor == nil {
		t.Fatal("newMovementGovernorIfEnabled = nil with no subsystems: block; want a constructed governor (absent config must not disable anything)")
	}

	governor.tick(context.Background(), deps)

	if got := ledger.readyCalls(); got != 1 {
		t.Errorf("brAdapter.Ready called %d times; want 1 (the governor's per-evaluation shell-out must happen when it is enabled)", got)
	}
	if got := bus.count(core.EventTypeGovernorSignal); got != 1 {
		t.Errorf("governor_signal emitted %d times; want 1", got)
	}

	// The eval-cadence gate is load-bearing, not politeness: evaluating on every
	// 2 s poll tick cost 25–50% daemon CPU on large event logs (hk-usn8o). A
	// second immediate tick, far inside the default cadence, must do nothing.
	governor.tick(context.Background(), deps)
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

	pc, root := subpartLoadConfig(t, `
schema_version: 1
subsystems:
  movement_governor:
    enabled: false
`)
	bus := &wlsubBus{}
	ledger := &wlsubCountingLedger{}
	deps := wlsubGovernorDeps(root, bus, ledger)
	deps.projectCfg = pc

	governor := newMovementGovernorIfEnabled(deps, io.Discard)
	if governor != nil {
		t.Fatal("newMovementGovernorIfEnabled returned a governor with subsystems.movement_governor.enabled: false; it must be ABSENT, not constructed")
	}

	// The loop calls these unconditionally; on an absent governor they are inert.
	for range 5 {
		governor.tick(context.Background(), deps)
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
	// Exactly what LoadDecisionAckState does at boot for a pending sentinel trip.
	blocker.AddQueueBlock(sentinelSubjectIDACT, "stale-trip-token")

	enabledPC, enabledRoot := subpartLoadConfig(t, "schema_version: 1\n")
	enabledDeps := wlsubGovernorDeps(enabledRoot, &wlsubBus{}, &wlsubCountingLedger{})
	enabledDeps.projectCfg = enabledPC
	enabledDeps.decisionBlocker = blocker

	enabled := newMovementGovernorIfEnabled(enabledDeps, io.Discard)
	if !enabled.dispatchBlocked(enabledDeps) {
		t.Fatal("dispatchBlocked = false with the governor enabled and a pending sentinel trip restored; the FW3 queue gate must still hold dispatch")
	}

	disabledPC, disabledRoot := subpartLoadConfig(t, `
schema_version: 1
subsystems:
  movement_governor:
    enabled: false
`)
	disabledDeps := wlsubGovernorDeps(disabledRoot, &wlsubBus{}, &wlsubCountingLedger{})
	disabledDeps.projectCfg = disabledPC
	disabledDeps.decisionBlocker = blocker

	disabled := newMovementGovernorIfEnabled(disabledDeps, io.Discard)
	if disabled.dispatchBlocked(disabledDeps) {
		t.Error("dispatchBlocked = true with subsystems.movement_governor.enabled: false; a switched-off subsystem must not hold the dispatcher shut through a gate nothing can open")
	}
}
