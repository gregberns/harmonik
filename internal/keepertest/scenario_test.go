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

func waitForScenarioEffect(t *testing.T, ports *RecordingPorts, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		effects := ports.EffectsSnapshot()
		if len(effects) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d effects; got %v", count, ports.EffectsSnapshot())
}

func waitForScenarioEffectContaining(t *testing.T, ports *RecordingPorts, want string) {
	waitForScenarioEffectCount(t, ports, want, 1)
}

func waitForScenarioEffectCount(t *testing.T, ports *RecordingPorts, want string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		effects := ports.EffectsSnapshot()
		found := 0
		for _, effect := range effects {
			if strings.Contains(effect, want) {
				found++
			}
		}
		if found >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d effect(s) containing %q; got %v", count, want, ports.EffectsSnapshot())
}

func countScenarioEffectsContaining(effects []string, want string) int {
	found := 0
	for _, effect := range effects {
		if strings.Contains(effect, want) {
			found++
		}
	}
	return found
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

func TestScenarioSuccessfulCycleRecordsOrderedEffects(t *testing.T) {
	policy := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
	policy.BootGracePeriod = 0
	policy.MaxBootGraceTotal = 0
	policy.OperatorTurnLookback = 0
	policy.PostAnswerGrace = 0
	policy.PollInterval = 100 * time.Millisecond
	policy.ClearSettle = time.Second
	policy.HandoffTimeout = 30 * time.Second
	policy.ModelDoneTimeout = 30 * time.Second

	s := NewScenario(policy)
	s.Ports().NextCycleID = "cyc-success"
	cycler, err := s.Build()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cycler.MaybeRun(context.Background(), s.Ports().Gauge) }()
	waitForScenarioEffectContaining(t, s.Ports(), "KEEPER:cyc-success")

	s.Ports().HandoffText = "# ready\n<!-- KEEPER:cyc-success -->\n"
	s.Clock().Advance(policy.PollInterval)
	waitForScenarioEffectContaining(t, s.Ports(), "journal:confirmed")

	s.Ports().IdleMarker = s.Clock().Now()
	s.Clock().Advance(policy.PollInterval)
	waitForScenarioEffectContaining(t, s.Ports(), "inject:/clear")

	s.Ports().Gauge = &keeper.CtxFile{Pct: 90, SessionID: "11111111-1111-4111-8111-111111111111"}
	s.Clock().Advance(policy.ClearSettle)
	if got := countScenarioEffectsContaining(s.Ports().EffectsSnapshot(), "inject:/clear"); got != 1 {
		t.Fatalf("clear attempts = %d, want 1 after settle observation: %v", got, s.Ports().EffectsSnapshot())
	}
	s.Ports().Gauge = &keeper.CtxFile{Pct: 2, SessionID: "22222222-2222-4222-8222-222222222222"}
	time.Sleep(time.Millisecond)
	var cycleErr error
	completed := false
	for range 5 {
		s.Clock().Advance(policy.PollInterval)
		select {
		case cycleErr = <-done:
			completed = true
		default:
			time.Sleep(time.Millisecond)
		}
		if completed {
			break
		}
	}
	if !completed {
		t.Fatalf("cycle did not complete: %v", s.Ports().EffectsSnapshot())
	}
	if cycleErr != nil {
		t.Fatal(cycleErr)
	}

	effects := s.Ports().EffectsSnapshot()
	wantOrdered := []string{
		"journal:opened", "escape", "KEEPER:cyc-success", "journal:handoff_injected",
		"journal:confirmed", "inject:/clear", "journal:cleared", "managed:22222222-2222-4222-8222-222222222222",
		"inject:harmonik agent brief", "journal:resumed", "journal:complete",
	}
	position := 0
	for _, effect := range effects {
		if position < len(wantOrdered) && strings.Contains(effect, wantOrdered[position]) {
			position++
		}
	}
	if position != len(wantOrdered) {
		t.Fatalf("ordered effects stopped at %q (%d/%d): %v", wantOrdered[position], position, len(wantOrdered), effects)
	}
	if s.Ports().Journal == nil || s.Ports().Journal.Phase != "complete" {
		t.Fatalf("journal = %+v, want complete", s.Ports().Journal)
	}
}

// TestScenarioHandoffTimeoutSuspendsWithoutClearing drives the whole cycle,
// shell and all, to the edge where the observation window closes with no
// marked handoff. The agent is working, not stuck, so the keeper suspends the
// request: journal phase "pending" with reason "handoff_pending", the same
// cycle id still on it, and none of the effects SK-025 forbids — no /clear and
// no managed-session write.
func TestScenarioHandoffTimeoutSuspendsWithoutClearing(t *testing.T) {
	policy := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
	policy.BootGracePeriod = 0
	policy.MaxBootGraceTotal = 0
	policy.OperatorTurnLookback = 0
	policy.PostAnswerGrace = 0
	policy.PollInterval = time.Second
	policy.HandoffTimeout = 3 * time.Second
	policy.MaxHandoffTimeouts = 3

	s := NewScenario(policy)
	s.Ports().NextCycleID = "cyc-timeout"
	cycler, err := s.Build()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cycler.MaybeRun(context.Background(), s.Ports().Gauge) }()
	waitForScenarioEffectContaining(t, s.Ports(), "KEEPER:cyc-timeout")
	s.Clock().Advance(policy.HandoffTimeout)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	journal := s.Ports().Journal
	if journal == nil || journal.Phase != "pending" || journal.Reason != "handoff_pending" {
		t.Fatalf("journal = %+v, want pending handoff_pending", journal)
	}
	if journal.CycleID != "cyc-timeout" {
		t.Fatalf("journal cycle id = %q, want the original request id cyc-timeout", journal.CycleID)
	}
	for _, effect := range s.Ports().EffectsSnapshot() {
		if strings.Contains(effect, "inject:/clear") {
			t.Fatalf("a pending handoff cleared the pane: %v", s.Ports().EffectsSnapshot())
		}
		if strings.Contains(effect, "managed:") {
			t.Fatalf("a pending handoff rewrote the managed session (SK-025 forbids it): %v",
				s.Ports().EffectsSnapshot())
		}
	}
}

func TestScenarioCrashRecoveryCompletesClearedJournal(t *testing.T) {
	policy := keeper.CyclePolicyFromConfig(keeper.CyclerConfig{})
	s := NewScenario(policy)
	s.Ports().Journal = &keeper.CycleJournal{
		CycleID: "cyc-recovery", Phase: "cleared",
	}
	cycler, err := s.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	effects := s.Ports().EffectsSnapshot()
	wantOrdered := []string{"inject:harmonik agent brief", "journal:complete"}
	position := 0
	for _, effect := range effects {
		if position < len(wantOrdered) && strings.Contains(effect, wantOrdered[position]) {
			position++
		}
	}
	if position != len(wantOrdered) {
		t.Fatalf("recovery effects stopped at %d/%d: %v", position, len(wantOrdered), effects)
	}
	if s.Ports().Journal == nil || s.Ports().Journal.Phase != "complete" {
		t.Fatalf("journal = %+v, want complete", s.Ports().Journal)
	}
}
