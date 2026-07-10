package daemon_test

// stalewatch_hk1s1or_test.go — unit tests for the launch_initiated → agent_ready
// stall detector in StaleWatcher (hk-1s1or).
//
// Blind spot covered: once launch_initiated arrives the launch-stall check
// (launchStallThreshold, ~30 s) is suppressed, and the never-spawned reaper
// (hk-0z5x) only CANCELS the run after NeverSpawnedReaperTimeout (~30 min).
// Between those two there was NO observable event for a hung launch→ready
// transition. agent_ready_stall_detected fires once, in a bounded few-minute
// window, so the hang is detectable long before the 30-min reaper.
//
// Test coverage:
//   - TestAgentReadyStall_FiresOnceAfterThreshold   — fires exactly once past the window
//   - TestAgentReadyStall_SuppressedIfAgentReady     — no event when agent_ready arrives in time
//   - TestAgentReadyStall_NotYetDue                  — no event before the window
//   - TestAgentReadyStall_SuppressedIfNoLaunchInitiated — no event without launch_initiated

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// arsCollectorBus is a sealed in-memory bus that records emitted
// agent_ready_stall_detected events into a collector.
type arsCollectorBus struct {
	bus     eventbus.EventBus
	mu      sync.Mutex
	emitted []core.AgentReadyStallDetectedPayload
}

func arsNewCollectorBus(t *testing.T) *arsCollectorBus {
	t.Helper()
	c := &arsCollectorBus{}
	c.bus = eventbus.NewBusImpl()
	sub := core.Subscription{
		ConsumerID:    "ars-test-collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			if evt.Type != string(core.EventTypeAgentReadyStallDetected) {
				return nil
			}
			var pl core.AgentReadyStallDetectedPayload
			if err := json.Unmarshal(evt.Payload, &pl); err != nil {
				return nil
			}
			c.mu.Lock()
			c.emitted = append(c.emitted, pl)
			c.mu.Unlock()
			return nil
		},
	}
	if _, err := c.bus.Subscribe(sub); err != nil {
		t.Fatalf("arsNewCollectorBus: Subscribe: %v", err)
	}
	if err := c.bus.Seal(); err != nil {
		t.Fatalf("arsNewCollectorBus: Seal: %v", err)
	}
	return c
}

func (c *arsCollectorBus) collected() []core.AgentReadyStallDetectedPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]core.AgentReadyStallDetectedPayload, len(c.emitted))
	copy(out, c.emitted)
	return out
}

// arsBuildWatcher builds a StaleWatcher wired to an agent_ready_stall collector,
// with a mutable clock and the given AgentReadyStallThreshold.
//
// StaleAfter is 24 h and NeverSpawnedReaperTimeout is 30 min — both far beyond
// the few-minute windows these tests simulate — so neither run_stale nor the
// never-spawned reaper interferes with assertions on the stall detector alone.
func arsBuildWatcher(t *testing.T, reg *daemon.RunRegistry, threshold time.Duration, clk *mutableClock) (*daemon.StaleWatcher, *arsCollectorBus) {
	t.Helper()
	col := arsNewCollectorBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus:              unsealed,
		Emitter:                   col.bus,
		Registry:                  reg,
		StaleAfter:                24 * time.Hour,
		ScanInterval:              time.Hour,
		NeverSpawnedReaperTimeout: 30 * time.Minute,
		AgentReadyStallThreshold:  threshold,
		Now:                       clk.Now,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("arsBuildWatcher: Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("arsBuildWatcher: Seal: %v", err)
	}
	return w, col
}

// TestAgentReadyStall_FiresOnceAfterThreshold verifies the detector emits
// agent_ready_stall_detected EXACTLY ONCE when launch_initiated was seen but
// agent_ready never arrives within AgentReadyStallThreshold, even across
// multiple scan passes.
func TestAgentReadyStall_FiresOnceAfterThreshold(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{
		BeadID:    "hk-1s1or-fire",
		StartedAt: epoch,
		// Cancel intentionally nil — detection must NOT depend on the reaper/cancel.
	}
	reg.Register(runID, handle)

	const threshold = 2 * time.Minute
	w, col := arsBuildWatcher(t, reg, threshold, clk)

	// run_started then launch_initiated, but NEVER agent_ready.
	nsrSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch.Add(1*time.Second))

	// Advance past the threshold (measured from launch_initiated) and scan
	// several times — the event must fire exactly once.
	clk.Set(epoch.Add(1*time.Second + threshold + time.Second))
	daemon.ExportedStalewatchScan(w, context.Background())
	daemon.ExportedStalewatchScan(w, context.Background())
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	got := col.collected()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 agent_ready_stall_detected event, got %d", len(got))
	}
	if got[0].RunID != runID.String() {
		t.Errorf("event run_id = %q, want %q", got[0].RunID, runID.String())
	}
	if got[0].BeadID != "hk-1s1or-fire" {
		t.Errorf("event bead_id = %q, want %q", got[0].BeadID, "hk-1s1or-fire")
	}
	if got[0].StallSeconds <= 0 {
		t.Errorf("event stall_seconds = %d, want > 0", got[0].StallSeconds)
	}
}

// TestAgentReadyStall_SuppressedIfAgentReady verifies the detector does NOT fire
// when agent_ready arrives before the threshold (the normal healthy path).
func TestAgentReadyStall_SuppressedIfAgentReady(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{BeadID: "hk-1s1or-ok", StartedAt: epoch}
	reg.Register(runID, handle)

	const threshold = 2 * time.Minute
	w, col := arsBuildWatcher(t, reg, threshold, clk)

	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)
	// agent_ready arrives well within the window.
	nsrSimulateEvent(w, clk, runID, core.EventTypeAgentReady, epoch.Add(2*time.Second))

	// Advance past the threshold and scan — detector must stay suppressed.
	clk.Set(epoch.Add(threshold + time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if n := len(col.collected()); n != 0 {
		t.Errorf("expected 0 agent_ready_stall_detected events when agent_ready arrived, got %d", n)
	}
}

// TestAgentReadyStall_NotYetDue verifies the detector does NOT fire before the
// threshold has been reached.
func TestAgentReadyStall_NotYetDue(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{BeadID: "hk-1s1or-early", StartedAt: epoch}
	reg.Register(runID, handle)

	const threshold = 2 * time.Minute
	w, col := arsBuildWatcher(t, reg, threshold, clk)

	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	// Only half the window has elapsed — no event yet.
	clk.Set(epoch.Add(threshold / 2))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if n := len(col.collected()); n != 0 {
		t.Errorf("expected 0 agent_ready_stall_detected events before the window, got %d", n)
	}
}

// TestAgentReadyStall_BeadLabelOverridesThreshold verifies a bead carrying
// "agent_ready_stall_threshold=<seconds>" widens the detection window past
// the daemon-global default, so a reasoning-model profile's legitimate
// multi-minute agent_ready latency (hk-4ir08) does not trip a spurious
// agent_ready_stall_detected within the default window.
func TestAgentReadyStall_BeadLabelOverridesThreshold(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{
		BeadID:    "hk-4ir08-dgx-reasoning",
		StartedAt: epoch,
		Labels:    []string{"profile:ornith-dgx-reasoning", "agent_ready_stall_threshold=1200"},
	}
	reg.Register(runID, handle)

	// Daemon-global default is 2 min — the bead's label widens it to 20 min.
	const globalDefault = 2 * time.Minute
	w, col := arsBuildWatcher(t, reg, globalDefault, clk)

	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	// Past the global default but well within the label's 20-min window —
	// must NOT fire.
	clk.Set(epoch.Add(5 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)
	if n := len(col.collected()); n != 0 {
		t.Fatalf("expected 0 agent_ready_stall_detected events within the label override window, got %d", n)
	}

	// Past the label's own 20-min window — now it must fire.
	clk.Set(epoch.Add(21 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)
	got := col.collected()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 agent_ready_stall_detected event past the label override window, got %d", len(got))
	}
	if got[0].BeadID != "hk-4ir08-dgx-reasoning" {
		t.Errorf("event bead_id = %q, want %q", got[0].BeadID, "hk-4ir08-dgx-reasoning")
	}
}

// TestAgentReadyStall_SuppressedIfNoLaunchInitiated verifies the detector does
// NOT fire when launch_initiated has not been observed (the launch-stall
// detector owns the run_started → launch_initiated gap, not this one).
func TestAgentReadyStall_SuppressedIfNoLaunchInitiated(t *testing.T) {
	t.Parallel()
	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	handle := &daemon.RunHandle{BeadID: "hk-1s1or-nolaunch", StartedAt: epoch}
	reg.Register(runID, handle)

	const threshold = 2 * time.Minute
	w, col := arsBuildWatcher(t, reg, threshold, clk)

	// Only run_started — no launch_initiated.
	nsrSimulateEvent(w, clk, runID, core.EventTypeRunStarted, epoch)

	clk.Set(epoch.Add(threshold + time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if n := len(col.collected()); n != 0 {
		t.Errorf("expected 0 agent_ready_stall_detected events without launch_initiated, got %d", n)
	}
}
