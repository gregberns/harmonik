package keepertest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func productionLikePolicy() keeper.CyclePolicy {
	p := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
	p.BootGracePeriod = keeper.DefaultBootGracePeriod
	p.MaxBootGraceTotal = 2 * p.BootGracePeriod
	p.OperatorTurnLookback = 5 * time.Minute
	p.PostAnswerGrace = 30 * time.Second
	return p
}

func waitForScenarioEffect(t *testing.T, ports *RecordingPorts, count int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		effects := ports.EffectsSnapshot()
		if len(effects) >= count {
			return effects
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d effects; got %v", count, ports.EffectsSnapshot())
	return nil
}

func TestScenarioRequiresReasonsForGateOptOuts(t *testing.T) {
	s := NewScenario(productionLikePolicy())
	for name, disable := range map[string]func(string) error{
		"boot": s.DisableBootGrace, "operator": s.DisableOperatorTurnGate,
		"answer": s.DisablePostAnswerGrace,
	} {
		if err := disable(""); err == nil {
			t.Fatalf("%s accepted empty reason", name)
		}
		if err := disable("fixture needs immediate entry"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(s.DisabledGates()) != 3 {
		t.Fatalf("disabled = %v", s.DisabledGates())
	}
}

func TestScenarioBuildsValidatedCycler(t *testing.T) {
	s := NewScenario(productionLikePolicy())
	if _, err := s.Build(); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioEachObservationGateDefersAlone(t *testing.T) {
	base := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
	base.BootGracePeriod = 0
	base.MaxBootGraceTotal = 0

	tests := map[string]func(*Scenario){
		"managed":           func(s *Scenario) { s.SetManaged(false) },
		"crisp idle":        func(s *Scenario) { s.SetIdle(false) },
		"dispatch":          func(s *Scenario) { s.SetDispatchHeld(true) },
		"sleep":             func(s *Scenario) { s.SetSleeping(true) },
		"manual hold":       func(s *Scenario) { s.SetHeld(true) },
		"operator attached": func(s *Scenario) { s.SetOperatorAttached(true) },
	}
	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			s := NewScenario(base)
			arrange(s)
			cycler, err := s.Build()
			if err != nil {
				t.Fatal(err)
			}
			if err := cycler.MaybeRun(context.Background(), s.Ports().Gauge); err != nil {
				t.Fatal(err)
			}
			if effects := s.Ports().Effects; len(effects) != 0 {
				t.Fatalf("gate allowed effects: %v", effects)
			}
		})
	}
}

func TestScenarioConversationGatesDeferAlone(t *testing.T) {
	tests := map[string]func(*Scenario){
		"recent operator turn": func(s *Scenario) { s.OperatorSays(s.Clock().Now().Add(-time.Minute)) },
		"recent answer":        func(s *Scenario) { s.AgentAnswers(s.Clock().Now().Add(-10 * time.Second)) },
	}
	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			policy := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
			policy.OperatorTurnLookback = 5 * time.Minute
			policy.PostAnswerGrace = 30 * time.Second
			s := NewScenario(policy)
			arrange(s)
			cycler, err := s.Build()
			if err != nil {
				t.Fatal(err)
			}
			if err := cycler.MaybeRun(context.Background(), s.Ports().Gauge); err != nil {
				t.Fatal(err)
			}
			if effects := s.Ports().Effects; len(effects) != 0 {
				t.Fatalf("gate allowed effects: %v", effects)
			}
		})
	}
}

func TestScenarioOperatorTurnDuringHandoffParksAndCanRetry(t *testing.T) {
	policy := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
	policy.BootGracePeriod = 0
	policy.MaxBootGraceTotal = 0
	policy.OperatorTurnLookback = 5 * time.Minute
	policy.PostAnswerGrace = 0
	policy.PollInterval = 3 * time.Second
	policy.HandoffTimeout = 30 * time.Second

	s := NewScenario(policy)
	s.Ports().NextCycleID = "cyc-collision"
	cycler, err := s.Build()
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- cycler.MaybeRun(context.Background(), s.Ports().Gauge) }()
	waitForScenarioEffect(t, s.Ports(), 4)

	// The slash-command transcript artifact is not a real user turn. The real
	// operator turn arrives outside its two-second exclusion window while the
	// handoff file already carries this cycle's nonce.
	s.Ports().HandoffText = "# current handoff\n<!-- KEEPER:cyc-collision -->\n"
	s.OperatorSays(s.Clock().Now().Add(3 * time.Second))
	s.Clock().Advance(3 * time.Second)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if s.Ports().Journal == nil || s.Ports().Journal.Phase != "parked" {
		t.Fatalf("journal = %+v, want parked", s.Ports().Journal)
	}
	for _, effect := range s.Ports().EffectsSnapshot() {
		if strings.Contains(effect, "inject:/clear") {
			t.Fatalf("operator collision cleared the pane: %v", s.Ports().EffectsSnapshot())
		}
	}

	// A parked cycle does not arm anti-loop suppression. Once the real turn is
	// outside the lookback, the same session can enter a fresh cycle.
	s.Clock().Advance(6 * time.Minute)
	s.Ports().NextCycleID = "cyc-retry"
	beforeRetry := len(s.Ports().EffectsSnapshot())
	retryCtx, cancelRetry := context.WithCancel(context.Background())
	retryDone := make(chan error, 1)
	go func() { retryDone <- cycler.MaybeRun(retryCtx, s.Ports().Gauge) }()
	waitForScenarioEffect(t, s.Ports(), beforeRetry+4)
	cancelRetry()
	s.Clock().Advance(policy.PollInterval)
	if err := <-retryDone; err != nil {
		t.Fatal(err)
	}
	foundRetry := false
	for _, effect := range s.Ports().EffectsSnapshot()[beforeRetry:] {
		if strings.Contains(effect, "KEEPER:cyc-retry") {
			foundRetry = true
		}
		if strings.Contains(effect, "inject:/clear") {
			t.Fatalf("cancelled retry cleared the pane: %v", s.Ports().EffectsSnapshot())
		}
	}
	if !foundRetry {
		t.Fatalf("parked cycle did not retry: %v", s.Ports().EffectsSnapshot())
	}
}
