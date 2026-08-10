package keepertest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

// Scenario builds a cycle from resolved policy and recording external ports.
// Policy stays private so tests use named behavior methods instead of raw zeroes.
type Scenario struct {
	policy   keeper.CyclePolicy
	env      keeper.CycleEnv
	clock    *substrate.FakeClock
	record   *RecordingPorts
	disabled map[string]string
}

func NewScenario(policy keeper.CyclePolicy) *Scenario {
	clock := substrate.NewFakeClock(time.Unix(1_700_000_000, 0))
	r := &RecordingPorts{
		Managed: true, Idle: true,
		Gauge: &keeper.CtxFile{Pct: 95, SessionID: "11111111-1111-4111-8111-111111111111"},
	}
	return &Scenario{
		policy: policy, env: keeper.CycleEnv{AgentName: "scenario", TmuxTarget: "scenario:0"},
		clock: clock, record: r, disabled: map[string]string{},
	}
}

func (s *Scenario) Build() (*keeper.Cycler, error) {
	return keeper.NewCyclerWithDeps(s.policy, s.env, s.record.deps(s.clock))
}

func (s *Scenario) Ports() *RecordingPorts      { return s.record }
func (s *Scenario) Clock() *substrate.FakeClock { return s.clock }
func (s *Scenario) DisabledGates() map[string]string {
	out := make(map[string]string, len(s.disabled))
	for k, v := range s.disabled {
		out[k] = v
	}
	return out
}

func (s *Scenario) disable(name, reason string, apply func()) error {
	if reason == "" {
		return errors.New("keeper scenario: disable reason is required for " + name)
	}
	apply()
	s.disabled[name] = reason
	return nil
}

func (s *Scenario) DisableBootGrace(reason string) error {
	return s.disable("boot_grace", reason, func() { s.policy.BootGracePeriod = 0; s.policy.MaxBootGraceTotal = 0 })
}

func (s *Scenario) DisableOperatorTurnGate(reason string) error {
	return s.disable("operator_turn", reason, func() { s.policy.OperatorTurnLookback = 0 })
}

func (s *Scenario) DisablePostAnswerGrace(reason string) error {
	return s.disable("post_answer_grace", reason, func() { s.policy.PostAnswerGrace = 0 })
}

// RecordingPorts is the controlled external world for a Scenario.
type RecordingPorts struct {
	mu                                      sync.Mutex
	Effects                                 []string
	Gauge                                   *keeper.CtxFile
	Managed, Idle, Dispatch                 bool
	SleepingState, HeldState, AttachedState bool
	UserTurn, AssistantTurn, IdleMarker     time.Time
	HandoffText                             string
	HandoffMtime                            time.Time
	Journal                                 *keeper.CycleJournal
	NextCycleID                             string
}

func (r *RecordingPorts) add(effect string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Effects = append(r.Effects, effect)
}

func (r *RecordingPorts) Next() string {
	if r.NextCycleID != "" {
		return r.NextCycleID
	}
	return "cyc-scenario"
}

func (r *RecordingPorts) Inject(_ context.Context, _, text string) error {
	r.add("inject:" + text)
	return nil
}
func (r *RecordingPorts) SendEscape(context.Context, string) error { r.add("escape"); return nil }
func (r *RecordingPorts) SetEnv(_ context.Context, _, key, value string) error {
	r.add("env:" + key + "=" + value)
	return nil
}

func (r *RecordingPorts) ReadGauge() (*keeper.CtxFile, time.Time, error) {
	return r.Gauge, time.Time{}, nil
}
func (r *RecordingPorts) SetManagedSession(sid string) error { r.add("managed:" + sid); return nil }
func (r *RecordingPorts) ClearPrecompactTrigger() error      { r.add("precompact:clear"); return nil }
func (r *RecordingPorts) IdleMarkerModTime() (time.Time, bool) {
	return r.IdleMarker, !r.IdleMarker.IsZero()
}

func (r *RecordingPorts) LastUserTurn(string) (time.Time, bool) {
	return r.UserTurn, !r.UserTurn.IsZero()
}

func (r *RecordingPorts) LastAssistantTurn(string) (time.Time, bool) {
	return r.AssistantTurn, !r.AssistantTurn.IsZero()
}
func (r *RecordingPorts) IsManaged() bool       { return r.Managed }
func (r *RecordingPorts) CrispIdle() bool       { return r.Idle }
func (r *RecordingPorts) HoldingDispatch() bool { return r.Dispatch }
func (r *RecordingPorts) Sleeping(string) bool  { return r.SleepingState }
func (r *RecordingPorts) Held() bool            { return r.HeldState }
func (r *RecordingPorts) Attached(string) bool  { return r.AttachedState }

func (r *RecordingPorts) deps(clock substrate.ClockPort) keeper.CycleDeps {
	return keeper.CycleDeps{
		Clock: clock, CycleIDs: r, Pane: r, Context: r, Activity: r,
		Managed: r, Idle: r, Dispatch: r, Sleep: r, Hold: r, Operator: r,
		Handoff: scenarioHandoff{r}, Journal: scenarioJournal{r},
	}
}

type scenarioHandoff struct{ r *RecordingPorts }

func (h scenarioHandoff) Path() string          { return "/tmp/HANDOFF-scenario.md" }
func (h scenarioHandoff) Read() (string, error) { return h.r.HandoffText, nil }
func (h scenarioHandoff) ModTime() (time.Time, bool) {
	return h.r.HandoffMtime, !h.r.HandoffMtime.IsZero()
}
func (h scenarioHandoff) ScrubNonce() error { h.r.add("handoff:scrub"); return nil }

type scenarioJournal struct{ r *RecordingPorts }

func (j scenarioJournal) Write(v *keeper.CycleJournal) error {
	copy := *v
	j.r.Journal = &copy
	j.r.add("journal:" + v.Phase)
	return nil
}
func (j scenarioJournal) Read() (*keeper.CycleJournal, error) { return j.r.Journal, nil }
