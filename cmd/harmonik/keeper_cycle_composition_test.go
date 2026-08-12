package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertest"
)

func waitForCommandScenarioEffect(t *testing.T, ports *keepertest.RecordingPorts, contains string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, effect := range ports.EffectsSnapshot() {
			if strings.Contains(effect, contains) {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %v", contains, ports.EffectsSnapshot())
}

func TestConstructKeeperCyclerCompletesThroughCommandComposition(t *testing.T) {
	policy := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{
		PollInterval: 100 * time.Millisecond, ClearSettle: time.Second,
		HandoffTimeout: 30 * time.Second, ModelDoneTimeout: 30 * time.Second,
	})
	scenario := keepertest.NewScenario(policy)
	ports := scenario.Ports()
	ports.NextCycleID = "cyc-command"
	emitter := &keeper.RecordingEmitter{}
	deps := ports.Dependencies(scenario.Clock())
	deps.Emitter = emitter
	cycler, err := constructKeeperCycler(
		policy,
		keeper.CycleEnv{AgentName: "command-scenario", ProjectDir: t.TempDir(), TmuxTarget: "scenario:0"},
		deps,
	)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cycler.MaybeRun(context.Background(), ports.Gauge) }()
	waitForCommandScenarioEffect(t, ports, "KEEPER:cyc-command")

	ports.HandoffText = "# handoff\n<!-- KEEPER:cyc-command -->\n"
	time.Sleep(10 * time.Millisecond)
	scenario.Clock().Advance(policy.PollInterval)
	waitForCommandScenarioEffect(t, ports, "journal:confirmed")

	ports.IdleMarker = scenario.Clock().Now()
	time.Sleep(10 * time.Millisecond)
	scenario.Clock().Advance(policy.PollInterval)
	waitForCommandScenarioEffect(t, ports, "inject:/clear")

	ports.Gauge = &keeper.CtxFile{Pct: 2, SessionID: "22222222-2222-4222-8222-222222222222"}
	time.Sleep(10 * time.Millisecond)
	scenario.Clock().Advance(policy.PollInterval)
	waitForCommandScenarioEffect(t, ports, "inject:harmonik agent brief")
	select {
	case cycleErr := <-done:
		if cycleErr != nil {
			t.Fatal(cycleErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cycle completion")
	}
	if ports.Journal == nil || ports.Journal.Phase != "complete" {
		t.Fatalf("journal = %+v, want complete", ports.Journal)
	}
	if got := len(emitter.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != 1 {
		t.Fatalf("cycle-complete events = %d, want 1", got)
	}
}
