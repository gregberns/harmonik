package keepertest

import (
	"context"
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
