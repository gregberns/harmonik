package keeper

// step_test.go — L0 unit tests for the PURE Step reactor (T7 acceptance §4):
// table-driven transition cases proving the machine independently of the
// shell, plus a substrate.Run round-trip (SyntheticSource + FakeEffector)
// proving the seam instantiation (design §0 / SK-009).

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// stepTestConfig returns a defaulted CyclerConfig suitable for driving the
// pure machine directly (no ports are ever touched by Step).
func stepTestConfig() *CyclerConfig {
	cfg := &CyclerConfig{
		AgentName:  "step-agent",
		ProjectDir: "/nonexistent",
		TmuxTarget: "fake-pane",
		ActPct:     90.0,
		WarnPct:    80.0,
	}
	cfg.applyDefaults()
	return cfg
}

func kinds(actions []Action) []ActionKind {
	out := make([]ActionKind, len(actions))
	for i, a := range actions {
		out[i] = a.Kind
	}
	return out
}

func assertKinds(t *testing.T, got []Action, want []ActionKind) {
	t.Helper()
	gk := kinds(got)
	if len(gk) != len(want) {
		t.Fatalf("action kinds = %v; want %v", gk, want)
	}
	for i := range want {
		if gk[i] != want[i] {
			t.Fatalf("action[%d] = %v; want %v (full: %v)", i, gk[i], want[i], gk)
		}
	}
}

// gaugeTickAt builds a passing-ladder GaugeTick entry event.
func gaugeTickAt(at time.Time, sid, cycleID string) Event {
	return Event{
		Kind:    EvGaugeTick,
		At:      at,
		CF:      &CtxFile{Pct: 95.0, SessionID: sid},
		Gates:   GateSnapshot{Managed: true, CrispIdle: true},
		CycleID: cycleID,
	}
}

// TestStep_IdleToAwaitingHandoff_LadderPass proves the Idle → AwaitingHandoff
// transition on a passing 11-gate ladder emits the exact cycle-open batch
// (§3c Idle pass row): journal(opened) → Emit(handoff_started) → SendEscape →
// InjectHandoffCmd → journal(handoff_injected) → ArmTimer(handoff_timeout).
func TestStep_IdleToAwaitingHandoff_LadderPass(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	actions := m.Step(gaugeTickAt(at, "sess-1", "cyc-step-001"))

	assertKinds(t, actions, []ActionKind{
		ActWriteJournal, ActEmit, ActSendEscape, ActInjectHandoffCmd,
		ActWriteJournal, ActArmTimer,
	})
	if actions[0].Journal.Phase != "opened" || actions[4].Journal.Phase != "handoff_injected" {
		t.Fatalf("journal phases = %q, %q; want opened, handoff_injected",
			actions[0].Journal.Phase, actions[4].Journal.Phase)
	}
	if actions[5].Timer != TimerHandoffTimeout || actions[5].D != cfg.HandoffTimeout {
		t.Fatalf("armed timer = %v/%v; want handoff_timeout/%v", actions[5].Timer, actions[5].D, cfg.HandoffTimeout)
	}
	if st := m.State(); st.Phase != PhaseAwaitingHandoff || st.CycleID != "cyc-step-001" {
		t.Fatalf("state = %+v; want AwaitingHandoff/cyc-step-001", st)
	}
	if !m.InCycle() {
		t.Fatal("InCycle() = false after ladder pass; want true")
	}
}

// TestStep_LadderPass_StaleNonceTruncates proves the pure stale-nonce
// predicate (hk-vpnp Bug 3b): a leftover PRIOR-cycle nonce in the sampled
// handoff content adds TruncateHandoff to the open batch; a genuine handoff
// without a keeper nonce is preserved (no truncate).
func TestStep_LadderPass_StaleNonceTruncates(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	at := time.Unix(1_700_000_000, 0)

	stale := gaugeTickAt(at, "sess-1", "cyc-step-002")
	stale.HandoffReadOK = true
	stale.HandoffContent = "# old\n<!-- KEEPER:cyc-prior-999 -->\n"
	actions := NewCycle(cfg).Step(stale)
	assertKinds(t, actions, []ActionKind{
		ActWriteJournal, ActEmit, ActTruncateHandoff, ActSendEscape,
		ActInjectHandoffCmd, ActWriteJournal, ActArmTimer,
	})

	genuine := gaugeTickAt(at, "sess-1", "cyc-step-003")
	genuine.HandoffReadOK = true
	genuine.HandoffContent = "# real operator handoff, no keeper nonce\n"
	actions = NewCycle(cfg).Step(genuine)
	for _, a := range actions {
		if a.Kind == ActTruncateHandoff {
			t.Fatal("genuine handoff (no keeper nonce) was truncated; must be preserved")
		}
	}
}

// TestStep_LadderFail_Gate5d_SetHoldPrelude proves the ladder-FAIL path still
// emits the unconditional prelude side effect (SK-011): a recent operator
// user turn defers ACT and emits SetHold, leaving the machine in Idle.
func TestStep_LadderFail_Gate5d_SetHoldPrelude(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	cfg.OperatorTurnLookback = 2 * time.Minute
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	ev := gaugeTickAt(at, "sess-1", "")
	ev.Gates.LastUserTurnAt = at.Add(-30 * time.Second) // within lookback

	actions := m.Step(ev)
	assertKinds(t, actions, []ActionKind{ActSetHold})
	if m.InCycle() {
		t.Fatal("machine left Idle on a Gate-5d deferral")
	}
}

// TestStep_HandoffTimeout_NoFresh_Aborts proves the TimerFired(handoff_
// timeout)-without-fresh-handoff edge: the ONLY path that never sends /clear
// (SK §8.2) — journal(aborted) + Emit(cycle_aborted), anti-loop suppression
// armed with the abort marker (hk-vpnp Bug 3a), terminal Aborted.
func TestStep_HandoffTimeout_NoFresh_Aborts(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "sess-1", "cyc-step-004"))
	actions := m.Step(Event{
		Kind: EvTimerFired, Timer: TimerHandoffTimeout,
		CycleID: "cyc-step-004", At: at.Add(cfg.HandoffTimeout),
	})

	assertKinds(t, actions, []ActionKind{ActWriteJournal, ActEmit})
	if actions[0].Journal.Phase != "aborted" || actions[0].Journal.Reason != "handoff_timeout" {
		t.Fatalf("abort journal = %+v; want aborted/handoff_timeout", actions[0].Journal)
	}
	if actions[1].Type != core.EventTypeSessionKeeperCycleAborted {
		t.Fatalf("emit type = %v; want cycle_aborted", actions[1].Type)
	}
	for _, a := range actions {
		if a.Kind == ActInjectClear || a.Kind == ActInjectBrief {
			t.Fatal("abort path injected /clear or brief; the abort path must NEVER clear")
		}
	}
	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "aborted" {
		t.Fatalf("state = %v/%v; want Idle/aborted", st.Phase, st.LastTerminal)
	}
	if st.LastFiredSID != "sess-1" || !st.LastFireWasAbort || st.SeenLowPctAfterLastFire {
		t.Fatalf("anti-loop state = %+v; want LastFiredSID=sess-1, LastFireWasAbort=true", st)
	}
}

// TestStep_HandoffTimeout_FreshRecovers proves the hk-fi78d recovery edge:
// HandoffFreshSeen before the timeout makes TimerFired(handoff_timeout) take
// the confirmed(reason=handoff_timeout_recovered) path into AwaitModelDone
// instead of aborting.
func TestStep_HandoffTimeout_FreshRecovers(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "sess-1", "cyc-step-005"))
	m.Step(Event{Kind: EvHandoffFreshSeen, CycleID: "cyc-step-005", Mtime: at.Add(time.Second), At: at.Add(cfg.HandoffTimeout)})
	actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: "cyc-step-005", At: at.Add(cfg.HandoffTimeout)})

	assertKinds(t, actions, []ActionKind{ActWriteJournal})
	if actions[0].Journal.Phase != "confirmed" || actions[0].Journal.Reason != "handoff_timeout_recovered" {
		t.Fatalf("journal = %+v; want confirmed/handoff_timeout_recovered", actions[0].Journal)
	}
	if st := m.State(); st.Phase != PhaseAwaitModelDone {
		t.Fatalf("phase = %v; want AwaitModelDone", st.Phase)
	}
}

// TestStep_FullHappyPath_ThroughAwaitModelDone drives the complete clean
// cycle: nonce → model-done → session flip → briefing terminal, asserting
// the /clear batch and the completion bookkeeping (SR3 structurally: clear
// only after the confirm edges; SR6: new_session_up-XOR-clear_unconfirmed is
// T8's emission — here the managed rebind carries the confirmed SID).
func TestStep_FullHappyPath_ThroughAwaitModelDone(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "sess-1", "cyc-step-006"))
	confirm := m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-step-006", At: at.Add(time.Second)})
	assertKinds(t, confirm, []ActionKind{ActWriteJournal, ActCancelTimer})
	if confirm[0].Journal.Phase != "confirmed" || confirm[0].Journal.Reason != "" {
		t.Fatalf("confirm journal = %+v; want confirmed/\"\"", confirm[0].Journal)
	}
	if st := m.State(); st.Phase != PhaseAwaitModelDone {
		t.Fatalf("phase after nonce = %v; want AwaitModelDone", st.Phase)
	}

	clearing := m.Step(Event{Kind: EvModelDone, CycleID: "cyc-step-006", SessionID: "sess-1", Source: "immediate", At: at.Add(2 * time.Second)})
	assertKinds(t, clearing, []ActionKind{
		ActSetTmuxEnv, ActInjectClear, ActWriteJournal, ActArmTimer, ActArmTimer,
	})
	if clearing[2].Journal.Phase != "cleared" {
		t.Fatalf("journal = %q; want cleared", clearing[2].Journal.Phase)
	}
	if clearing[3].Timer != TimerClearBackstop || clearing[4].Timer != TimerClearSettle {
		t.Fatalf("armed timers = %v,%v; want clear_backstop,clear_settle", clearing[3].Timer, clearing[4].Timer)
	}

	brief := m.Step(Event{Kind: EvSessionChanged, CycleID: "cyc-step-006", PrevSID: "sess-1", NewSID: "sess-2", At: at.Add(3 * time.Second)})
	assertKinds(t, brief, []ActionKind{
		ActSetManagedSession, ActCancelTimer, ActCancelTimer,
		ActInjectBrief, ActWriteJournal, ActWriteJournal, ActEmit,
	})
	if brief[0].SID != "sess-2" {
		t.Fatalf("managed rebind SID = %q; want sess-2", brief[0].SID)
	}
	if brief[4].Journal.Phase != "resumed" || brief[5].Journal.Phase != "complete" {
		t.Fatalf("journal tail = %q,%q; want resumed,complete", brief[4].Journal.Phase, brief[5].Journal.Phase)
	}
	if brief[6].Type != core.EventTypeSessionKeeperCycleComplete {
		t.Fatalf("emit = %v; want cycle_complete", brief[6].Type)
	}
	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "complete" || st.LastFireWasAbort {
		t.Fatalf("terminal state = %+v; want Idle/complete", st)
	}
}

// TestStep_ClearBackstop_Unconfirmed proves the Clearing backstop edge:
// TimerFired(clear_backstop) emits clear_unconfirmed + clears the managed
// binding, and the brief STILL fires (degraded completion, SK §8.3 — not a
// terminal by itself).
func TestStep_ClearBackstop_Unconfirmed(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "sess-1", "cyc-step-007"))
	m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-step-007", At: at})
	m.Step(Event{Kind: EvModelDone, CycleID: "cyc-step-007", SessionID: "sess-1", Source: "immediate", At: at})

	actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: "cyc-step-007", At: at.Add(cfg.ClearConfirmBackstop)})
	assertKinds(t, actions, []ActionKind{
		ActEmit, ActSetManagedSession, ActCancelTimer, ActCancelTimer,
		ActInjectBrief, ActWriteJournal, ActWriteJournal, ActEmit,
	})
	if actions[0].Type != core.EventTypeSessionKeeperClearUnconfirmed {
		t.Fatalf("emit[0] = %v; want clear_unconfirmed", actions[0].Type)
	}
	if actions[1].SID != "" {
		t.Fatalf("managed rebind = %q; want \"\" (cleared for .sid rebind)", actions[1].SID)
	}
	if st := m.State(); st.Phase != PhaseIdle || st.LastTerminal != "complete" {
		t.Fatalf("state = %v/%v; want Idle/complete (degraded completion)", st.Phase, st.LastTerminal)
	}
}

// TestStep_ClearSettle_RetriesThenExhausts proves the hk-vdqe2 settle-retry
// discipline: each TimerFired(clear_settle) with retries left re-injects
// /clear and re-arms; once ClearConfirmRetries windows have elapsed the
// unconfirmed path fires.
func TestStep_ClearSettle_RetriesThenExhausts(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	cfg.ClearConfirmRetries = 3
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "sess-1", "cyc-step-008"))
	m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-step-008", At: at})
	m.Step(Event{Kind: EvModelDone, CycleID: "cyc-step-008", SessionID: "sess-1", Source: "immediate", At: at})

	// Windows 1 and 2: retries left → defensive re-inject + re-arm.
	for i := 0; i < 2; i++ {
		actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerClearSettle, CycleID: "cyc-step-008", At: at})
		assertKinds(t, actions, []ActionKind{ActInjectClear, ActArmTimer})
	}
	// Window 3: attempt == retries → unconfirmed + brief.
	actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerClearSettle, CycleID: "cyc-step-008", At: at})
	if actions[0].Kind != ActEmit || actions[0].Type != core.EventTypeSessionKeeperClearUnconfirmed {
		t.Fatalf("exhausted settle action[0] = %+v; want Emit(clear_unconfirmed)", actions[0])
	}
	if st := m.State(); st.Phase != PhaseIdle {
		t.Fatalf("phase = %v; want Idle", st.Phase)
	}
}

// TestStep_SubstrateRunRoundTrip proves the seam instantiation (design §0):
// a SyntheticSource[Event] + the reactor's Step + a FakeEffector[Action]
// under substrate.Run reproduce the full happy-path action log.
func TestStep_SubstrateRunRoundTrip(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	src := substrate.NewSyntheticSource([]Event{
		gaugeTickAt(at, "sess-1", "cyc-step-009"),
		{Kind: EvNonceObserved, CycleID: "cyc-step-009", At: at.Add(time.Second)},
		{Kind: EvModelDone, CycleID: "cyc-step-009", SessionID: "sess-1", Source: "immediate", At: at.Add(2 * time.Second)},
		{Kind: EvSessionChanged, CycleID: "cyc-step-009", PrevSID: "sess-1", NewSID: "sess-2", At: at.Add(3 * time.Second)},
	})
	eff := &substrate.FakeEffector[Action]{}

	if err := m.Run(context.Background(), src, eff); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := kinds(eff.Actions())
	want := []ActionKind{
		// cycle open
		ActWriteJournal, ActEmit, ActSendEscape, ActInjectHandoffCmd, ActWriteJournal, ActArmTimer,
		// confirm
		ActWriteJournal, ActCancelTimer,
		// clearing
		ActSetTmuxEnv, ActInjectClear, ActWriteJournal, ActArmTimer, ActArmTimer,
		// briefing terminal
		ActSetManagedSession, ActCancelTimer, ActCancelTimer,
		ActInjectBrief, ActWriteJournal, ActWriteJournal, ActEmit,
	}
	if len(got) != len(want) {
		t.Fatalf("action log = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("action[%d] = %v; want %v", i, got[i], want[i])
		}
	}
	if st := m.State(); st.Phase != PhaseIdle || st.LastTerminal != "complete" {
		t.Fatalf("terminal state = %v/%v; want Idle/complete", st.Phase, st.LastTerminal)
	}
}

// TestStep_PrecompactBlocked_FailPathPrelude proves the precompact entry's
// fail rows keep the always-clear-marker + per-gate emission contract,
// including the empty-SID hold_dispatch_skip quirk (§3c Idle fail row).
func TestStep_PrecompactBlocked_FailPathPrelude(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	at := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name   string
		ev     Event
		action string
	}{
		{
			name:   "not_managed",
			ev:     Event{Kind: EvPrecompactTrigger, At: at, CF: &CtxFile{SessionID: "s"}, Gates: GateSnapshot{}},
			action: "not_managed",
		},
		{
			name:   "empty_sid_quirk",
			ev:     Event{Kind: EvPrecompactTrigger, At: at, CF: &CtxFile{}, Gates: GateSnapshot{Managed: true}},
			action: "hold_dispatch_skip",
		},
		{
			name:   "holding_dispatch",
			ev:     Event{Kind: EvPrecompactTrigger, At: at, CF: &CtxFile{SessionID: "s"}, Gates: GateSnapshot{Managed: true, HoldingDispatch: true}},
			action: "hold_dispatch_skip",
		},
		{
			name:   "held",
			ev:     Event{Kind: EvPrecompactTrigger, At: at, CF: &CtxFile{SessionID: "s"}, Gates: GateSnapshot{Managed: true, Held: true}},
			action: "hold_skip",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := NewCycle(cfg)
			actions := m.Step(tc.ev)
			assertKinds(t, actions, []ActionKind{ActEmit, ActClearPrecompact})
			if actions[0].Type != core.EventTypeSessionKeeperPrecompactBlocked {
				t.Fatalf("emit type = %v; want precompact_blocked", actions[0].Type)
			}
			if m.InCycle() {
				t.Fatal("blocked precompact left the machine off-Idle")
			}
		})
	}
}

// TestStep_TimerEventsIgnoredInIdle proves the §3c Idle row "any
// timer/detection event → ignored (no cycle in flight)".
func TestStep_TimerEventsIgnoredInIdle(t *testing.T) {
	t.Parallel()
	m := NewCycle(stepTestConfig())
	at := time.Unix(1_700_000_000, 0)
	for _, ev := range []Event{
		{Kind: EvTimerFired, Timer: TimerHandoffTimeout, At: at},
		{Kind: EvNonceObserved, CycleID: "cyc-x", At: at},
		{Kind: EvSessionChanged, PrevSID: "a", NewSID: "b", At: at},
		{Kind: EvModelDone, CycleID: "cyc-x", At: at},
	} {
		if actions := m.Step(ev); len(actions) != 0 {
			t.Fatalf("event %v in Idle produced actions %v; want none", ev.Kind, kinds(actions))
		}
		if m.InCycle() {
			t.Fatalf("event %v in Idle moved the machine off-Idle", ev.Kind)
		}
	}
}
