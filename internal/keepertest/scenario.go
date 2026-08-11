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

// NewScenario returns a Scenario for the given policy with a fake clock and
// recording ports that start managed, idle, and 90 percent full. This is above
// the ACT threshold and below the force threshold, so each normal gate remains
// observable.
func NewScenario(policy keeper.CyclePolicy) *Scenario {
	clock := substrate.NewFakeClock(time.Unix(1_700_000_000, 0))
	r := &RecordingPorts{
		Managed: true, Idle: true,
		Gauge: &keeper.CtxFile{Pct: 90, SessionID: "11111111-1111-4111-8111-111111111111"},
	}
	return &Scenario{
		policy: policy, env: keeper.CycleEnv{AgentName: "scenario", TmuxTarget: "scenario:0"},
		clock: clock, record: r, disabled: map[string]string{},
	}
}

// Build makes the cycle under test from the policy as it stands now, wired to
// the recording ports. Call it after every Disable call, because Build copies
// the policy.
func (s *Scenario) Build() (*keeper.Cycler, error) {
	return keeper.NewCyclerWithDeps(s.policy, s.env, s.record.deps(s.clock))
}

// Ports returns the recording ports that carry the test inputs and the
// recorded effects.
func (s *Scenario) Ports() *RecordingPorts { return s.record }

// Clock returns the fake clock that drives the cycle.
func (s *Scenario) Clock() *substrate.FakeClock { return s.clock }

// Dependencies returns the narrow dependency graph backed by these recording
// ports and the given clock.
func (r *RecordingPorts) Dependencies(clock substrate.ClockPort) keeper.CycleDeps {
	return r.deps(clock)
}

// DisabledGates returns a copy of the gates the test turned off, with the
// reason it gave for each one.
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

// DisableBootGrace removes the wait that holds the cycle off a session that
// only just started. Give the reason the case does not need that wait. An
// empty reason is an error.
func (s *Scenario) DisableBootGrace(reason string) error {
	return s.disable("boot_grace", reason, func() { s.policy.BootGracePeriod = 0; s.policy.MaxBootGraceTotal = 0 })
}

// DisableOperatorTurnGate stops the cycle from parking because the operator
// spoke a moment ago. Give the reason the case does not need that gate. An
// empty reason is an error.
func (s *Scenario) DisableOperatorTurnGate(reason string) error {
	return s.disable("operator_turn", reason, func() { s.policy.OperatorTurnLookback = 0 })
}

// DisablePostAnswerGrace removes the pause the cycle takes after the agent
// answers the operator. Give the reason the case does not need that pause. An
// empty reason is an error.
func (s *Scenario) DisablePostAnswerGrace(reason string) error {
	return s.disable("post_answer_grace", reason, func() { s.policy.PostAnswerGrace = 0 })
}

// SetManaged controls the managed-state gate.
func (s *Scenario) SetManaged(managed bool) { s.record.Managed = managed }

// SetIdle controls the crisp-idle gate.
func (s *Scenario) SetIdle(idle bool) { s.record.Idle = idle }

// SetDispatchHeld controls the in-flight dispatch gate.
func (s *Scenario) SetDispatchHeld(held bool) { s.record.Dispatch = held }

// SetSleeping controls the sleeping-session gate.
func (s *Scenario) SetSleeping(sleeping bool) { s.record.SleepingState = sleeping }

// SetHeld controls the manual keeper-hold gate.
func (s *Scenario) SetHeld(held bool) { s.record.HeldState = held }

// SetOperatorAttached controls the live operator-presence gate.
func (s *Scenario) SetOperatorAttached(attached bool) { s.record.AttachedState = attached }

// OperatorSays records a real operator turn at the given time.
func (s *Scenario) OperatorSays(at time.Time) { s.record.UserTurn = at }

// AgentAnswers records a real assistant turn at the given time.
func (s *Scenario) AgentAnswers(at time.Time) { s.record.AssistantTurn = at }

// Effects returns a stable copy of the recorded effect order.
func (r *RecordingPorts) EffectsSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.Effects...)
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

// Next returns NextCycleID when the test set one, and a fixed value otherwise.
func (r *RecordingPorts) Next() string {
	if r.NextCycleID != "" {
		return r.NextCycleID
	}
	return "cyc-scenario"
}

// Inject records the injected text. Nothing reaches a real tmux pane.
func (r *RecordingPorts) Inject(_ context.Context, _, text string) error {
	r.add("inject:" + text)
	return nil
}

// SendEscape records an Escape keystroke.
func (r *RecordingPorts) SendEscape(context.Context, string) error { r.add("escape"); return nil }

// SetEnv records the tmux environment name and value.
func (r *RecordingPorts) SetEnv(_ context.Context, _, key, value string) error {
	r.add("env:" + key + "=" + value)
	return nil
}

// ReadGauge returns the Gauge field and a zero modification time.
func (r *RecordingPorts) ReadGauge() (*keeper.CtxFile, time.Time, error) {
	return r.Gauge, time.Time{}, nil
}

// SetManagedSession records the session id it was given.
func (r *RecordingPorts) SetManagedSession(sid string) error { r.add("managed:" + sid); return nil }

// ClearPrecompactTrigger records that the precompact trigger was cleared.
func (r *RecordingPorts) ClearPrecompactTrigger() error { r.add("precompact:clear"); return nil }

// IdleMarkerModTime returns the IdleMarker field. A zero time reports false.
func (r *RecordingPorts) IdleMarkerModTime() (time.Time, bool) {
	return r.IdleMarker, !r.IdleMarker.IsZero()
}

// LastUserTurn returns the UserTurn field. A zero time reports false. The
// session id is ignored.
func (r *RecordingPorts) LastUserTurn(string) (time.Time, bool) {
	return r.UserTurn, !r.UserTurn.IsZero()
}

// LastAssistantTurn returns the AssistantTurn field. A zero time reports
// false. The session id is ignored.
func (r *RecordingPorts) LastAssistantTurn(string) (time.Time, bool) {
	return r.AssistantTurn, !r.AssistantTurn.IsZero()
}

// IsManaged returns the Managed field.
func (r *RecordingPorts) IsManaged() bool { return r.Managed }

// CrispIdle returns the Idle field.
func (r *RecordingPorts) CrispIdle() bool { return r.Idle }

// HoldingDispatch returns the Dispatch field.
func (r *RecordingPorts) HoldingDispatch() bool { return r.Dispatch }

// Sleeping returns the SleepingState field for every session id.
func (r *RecordingPorts) Sleeping(string) bool { return r.SleepingState }

// Held returns the HeldState field.
func (r *RecordingPorts) Held() bool { return r.HeldState }

// Attached returns the AttachedState field for every target.
func (r *RecordingPorts) Attached(string) bool { return r.AttachedState }

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
	saved := *v
	j.r.Journal = &saved
	j.r.add("journal:" + v.Phase)
	return nil
}
func (j scenarioJournal) Read() (*keeper.CycleJournal, error) { return j.r.Journal, nil }
