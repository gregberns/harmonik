package keepertest

import (
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
