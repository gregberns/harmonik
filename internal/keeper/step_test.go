package keeper

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

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

func gaugeTickAt(at time.Time, cycleID string) Event {
	return Event{
		Kind:    EvGaugeTick,
		At:      at,
		CF:      &CtxFile{Pct: 95.0, SessionID: "sess-1"},
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

	actions := m.Step(gaugeTickAt(at, "cyc-step-001"))

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

	stale := gaugeTickAt(at, "cyc-step-002")
	stale.HandoffReadOK = true
	stale.HandoffContent = "# old\n<!-- KEEPER:cyc-prior-999 -->\n"
	actions := NewCycle(cfg).Step(stale)
	assertKinds(t, actions, []ActionKind{
		ActWriteJournal, ActEmit, ActTruncateHandoff, ActSendEscape,
		ActInjectHandoffCmd, ActWriteJournal, ActArmTimer,
	})

	genuine := gaugeTickAt(at, "cyc-step-003")
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
func TestStep_LadderFail_Gate5dDefersTransiently(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	cfg.OperatorTurnLookback = 2 * time.Minute
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	ev := gaugeTickAt(at, "")
	ev.Gates.LastUserTurnAt = at.Add(-30 * time.Second) // within lookback

	actions := m.Step(ev)
	assertKinds(t, actions, nil)
	if m.InCycle() {
		t.Fatal("machine left Idle on a Gate-5d deferral")
	}

	retry := gaugeTickAt(at.Add(3*time.Minute), "cyc-step-retry")
	retry.Gates.LastUserTurnAt = ev.Gates.LastUserTurnAt
	actions = m.Step(retry)
	if len(actions) == 0 || actions[0].Kind != ActWriteJournal {
		t.Fatalf("expired transient deferral did not start a new cycle: %+v", actions)
	}
}

// TestStep_HandoffObservationWake_NoMarkerStaysPending proves that an ended
// observation window returns control without treating useful work as failure.
func TestStep_HandoffObservationWake_NoMarkerStaysPending(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "cyc-step-004"))
	actions := m.Step(Event{
		Kind: EvTimerFired, Timer: TimerHandoffTimeout,
		CycleID: "cyc-step-004", At: at.Add(cfg.HandoffTimeout),
	})

	assertKinds(t, actions, []ActionKind{ActWriteJournal, ActEmit, ActCancelTimer})
	if actions[0].Journal.Phase != "pending" || actions[0].Journal.Reason != "handoff_pending" {
		t.Fatalf("pending journal = %+v; want pending/handoff_pending", actions[0].Journal)
	}
	if actions[1].Type != core.EventTypeSessionKeeperCycleParked {
		t.Fatalf("emit type = %v; want cycle_parked", actions[1].Type)
	}
	for _, a := range actions {
		if a.Kind == ActInjectClear || a.Kind == ActInjectBrief {
			t.Fatal("abort path injected /clear or brief; the abort path must NEVER clear")
		}
	}
	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "pending" {
		t.Fatalf("state = %v/%v; want Idle/pending", st.Phase, st.LastTerminal)
	}
	if st.LastFiredSID != "" || st.LastFireWasAbort {
		t.Fatalf("pending window armed abort suppression: %+v", st)
	}
}

func TestStep_PendingMarkedHandoffResumesOriginalRequest(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)
	m.Step(gaugeTickAt(at, "cyc-pending-original"))
	m.Step(Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: "cyc-pending-original", At: at.Add(cfg.HandoffTimeout)})

	actions := m.Step(Event{
		Kind: EvPendingHandoffSeen, CycleID: "cyc-pending-original",
		Mtime: at.Add(10 * time.Minute), At: at.Add(10 * time.Minute),
	})
	assertKinds(t, actions, []ActionKind{ActWriteJournal, ActEmit, ActCancelTimer, ActArmTimer})
	if st := m.State(); st.Phase != PhaseAwaitModelDone || st.CycleID != "cyc-pending-original" {
		t.Fatalf("state = %+v; want original request awaiting model done", st)
	}
}

func TestStep_CrashJournalRestoresPendingRequest(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)
	j := &CycleJournal{CycleID: "cyc-restored", SessionID: "sid-restored", Phase: "pending", OpenedAt: at.Add(-10 * time.Minute), UpdatedAt: at, Reason: "handoff_pending"}

	actions := m.Step(Event{Kind: EvCrashJournal, At: at, Journal: j})
	assertKinds(t, actions, nil)
	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "pending" {
		t.Fatalf("state = %v/%v; want Idle/pending", st.Phase, st.LastTerminal)
	}
	if st.CycleID != j.CycleID || st.PrevSID != j.SessionID || st.OpenedAt != j.OpenedAt {
		t.Fatalf("restored request = %+v; want journal identity %+v", st, j)
	}
}

func TestStep_ProductionHardBandDoesNotStartAtWarnBand(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	cfg.HardBandCycleOnly = true
	m := NewCycle(cfg)
	ev := gaugeTickAt(time.Unix(1_700_000_000, 0), "cyc-must-not-start")
	ev.CF.Tokens = cfg.ActAbsTokens
	ev.CF.WindowSize = 1_000_000
	ev.CF.Pct = cfg.ActPct

	if actions := m.Step(ev); len(actions) != 0 {
		t.Fatalf("warn band started an automatic cycle: %+v", actions)
	}
	if m.InCycle() {
		t.Fatal("warn band left the machine in a cycle")
	}
}

func TestStep_RecentOperatorTurn_ParksWithoutTimeoutEscalation(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "cyc-step-park"))
	m.state.ConsecutiveHandoffTimeouts = 2
	actions := m.Step(Event{Kind: EvOperatorTurnRecent, CycleID: "cyc-step-park", At: at.Add(time.Second)})

	assertKinds(t, actions, []ActionKind{ActWriteJournal, ActEmit, ActCancelTimer})
	if actions[0].Journal.Phase != "parked" || actions[0].Journal.Reason != "operator_turn_recent" {
		t.Fatalf("park journal = %+v", actions[0].Journal)
	}
	if actions[1].Type != core.EventTypeSessionKeeperCycleParked {
		t.Fatalf("emit type = %v; want cycle_parked", actions[1].Type)
	}
	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "parked" {
		t.Fatalf("state = %v/%v; want Idle/parked", st.Phase, st.LastTerminal)
	}
	if st.ConsecutiveHandoffTimeouts != 2 {
		t.Fatalf("timeout count = %d; want unchanged 2", st.ConsecutiveHandoffTimeouts)
	}
	if st.LastFiredSID != "" || st.LastFireWasAbort {
		t.Fatalf("park armed abort suppression: %+v", st)
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

	m.Step(gaugeTickAt(at, "cyc-step-005"))
	m.Step(Event{Kind: EvHandoffFreshSeen, CycleID: "cyc-step-005", Mtime: at.Add(time.Second), At: at.Add(cfg.HandoffTimeout)})
	actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: "cyc-step-005", At: at.Add(cfg.HandoffTimeout)})

	assertKinds(t, actions, []ActionKind{ActWriteJournal, ActEmit, ActArmTimer})
	if actions[0].Journal.Phase != "confirmed" || actions[0].Journal.Reason != "handoff_timeout_recovered" {
		t.Fatalf("journal = %+v; want confirmed/handoff_timeout_recovered", actions[0].Journal)
	}
	if actions[1].Type != core.EventTypeSessionKeeperHandoffWritten {
		t.Fatalf("emit = %v; want handoff_written", actions[1].Type)
	}
	var hw core.SessionKeeperHandoffWrittenPayload
	if err := json.Unmarshal(actions[1].Payload, &hw); err != nil {
		t.Fatalf("unmarshal handoff_written: %v", err)
	}
	if !hw.Recovered || hw.HandoffMtime == "" || hw.Nonce != "" {
		t.Fatalf("recovery payload = %+v; want recovered:true + handoff_mtime, no nonce", hw)
	}
	if actions[2].Timer != TimerModelDone || actions[2].D != cfg.ModelDoneTimeout {
		t.Fatalf("armed timer = %v/%v; want model_done_timeout/%v", actions[2].Timer, actions[2].D, cfg.ModelDoneTimeout)
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

	m.Step(gaugeTickAt(at, "cyc-step-006"))
	confirm := m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-step-006", At: at.Add(time.Second)})
	assertKinds(t, confirm, []ActionKind{ActWriteJournal, ActEmit, ActCancelTimer, ActArmTimer})
	if confirm[0].Journal.Phase != "confirmed" || confirm[0].Journal.Reason != "" {
		t.Fatalf("confirm journal = %+v; want confirmed/\"\"", confirm[0].Journal)
	}
	if confirm[1].Type != core.EventTypeSessionKeeperHandoffWritten {
		t.Fatalf("emit = %v; want handoff_written", confirm[1].Type)
	}
	var hw core.SessionKeeperHandoffWrittenPayload
	if err := json.Unmarshal(confirm[1].Payload, &hw); err != nil {
		t.Fatalf("unmarshal handoff_written: %v", err)
	}
	if hw.CycleID != "cyc-step-006" || hw.Nonce == "" || hw.Recovered {
		t.Fatalf("handoff_written payload = %+v; want cycle_id + nonce, not recovered", hw)
	}
	if confirm[3].Timer != TimerModelDone {
		t.Fatalf("armed timer = %v; want model_done_timeout", confirm[3].Timer)
	}
	if st := m.State(); st.Phase != PhaseAwaitModelDone {
		t.Fatalf("phase after nonce = %v; want AwaitModelDone", st.Phase)
	}

	clearing := m.Step(Event{Kind: EvModelDone, CycleID: "cyc-step-006", SessionID: "sess-1", Source: "idle_marker", At: at.Add(2 * time.Second)})
	assertKinds(t, clearing, []ActionKind{
		ActEmit, ActSetTmuxEnv, ActInjectClear, ActEmit, ActWriteJournal,
		ActCancelTimer, ActArmTimer, ActArmTimer,
	})
	if clearing[0].Type != core.EventTypeSessionKeeperModelDone {
		t.Fatalf("emit[0] = %v; want model_done", clearing[0].Type)
	}
	var md core.SessionKeeperModelDonePayload
	if err := json.Unmarshal(clearing[0].Payload, &md); err != nil {
		t.Fatalf("unmarshal model_done: %v", err)
	}
	if md.Source != "idle_marker" || md.Degraded || md.CycleID != "cyc-step-006" {
		t.Fatalf("model_done payload = %+v; want source=idle_marker, not degraded", md)
	}
	if clearing[3].Type != core.EventTypeSessionKeeperClearSent {
		t.Fatalf("emit[3] = %v; want clear_sent", clearing[3].Type)
	}
	var cs core.SessionKeeperClearSentPayload
	if err := json.Unmarshal(clearing[3].Payload, &cs); err != nil {
		t.Fatalf("unmarshal clear_sent: %v", err)
	}
	if cs.Attempt != 1 || cs.CycleID != "cyc-step-006" {
		t.Fatalf("clear_sent payload = %+v; want attempt:1", cs)
	}
	if clearing[4].Journal.Phase != "cleared" {
		t.Fatalf("journal = %q; want cleared", clearing[4].Journal.Phase)
	}
	if clearing[5].Timer != TimerModelDone {
		t.Fatalf("cancel timer = %v; want model_done_timeout", clearing[5].Timer)
	}
	if clearing[6].Timer != TimerClearBackstop || clearing[7].Timer != TimerClearSettle {
		t.Fatalf("armed timers = %v,%v; want clear_backstop,clear_settle", clearing[6].Timer, clearing[7].Timer)
	}

	brief := m.Step(Event{Kind: EvSessionChanged, CycleID: "cyc-step-006", PrevSID: "sess-1", NewSID: "sess-2", At: at.Add(3 * time.Second)})
	assertKinds(t, brief, []ActionKind{
		ActEmit, ActSetManagedSession, ActCancelTimer, ActCancelTimer,
		ActInjectBrief, ActWriteJournal, ActWriteJournal, ActEmit,
	})
	if brief[0].Type != core.EventTypeSessionKeeperNewSessionUp {
		t.Fatalf("emit[0] = %v; want new_session_up", brief[0].Type)
	}
	var nsu core.SessionKeeperNewSessionUpPayload
	if err := json.Unmarshal(brief[0].Payload, &nsu); err != nil {
		t.Fatalf("unmarshal new_session_up: %v", err)
	}
	if nsu.PrevSessionID != "sess-1" || nsu.NewSessionID != "sess-2" || !nsu.Valid() {
		t.Fatalf("new_session_up payload = %+v; want prev=sess-1 new=sess-2 Valid", nsu)
	}
	if brief[1].SID != "sess-2" {
		t.Fatalf("managed rebind SID = %q; want sess-2", brief[1].SID)
	}
	if brief[5].Journal.Phase != "resumed" || brief[6].Journal.Phase != "complete" {
		t.Fatalf("journal tail = %q,%q; want resumed,complete", brief[5].Journal.Phase, brief[6].Journal.Phase)
	}
	if brief[7].Type != core.EventTypeSessionKeeperCycleComplete {
		t.Fatalf("emit = %v; want cycle_complete", brief[7].Type)
	}
	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "complete" || st.LastFireWasAbort {
		t.Fatalf("terminal state = %+v; want Idle/complete", st)
	}
}

// TestStep_ClearBackstop_Unconfirmed proves that an unknown target session is
// a failed restart. The keeper clears its binding, but it does not inject a
// brief or claim cycle completion before it observes session turnover.
func TestStep_ClearBackstop_Unconfirmed(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "cyc-step-007"))
	m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-step-007", At: at})
	m.Step(Event{Kind: EvModelDone, CycleID: "cyc-step-007", SessionID: "sess-1", Source: "idle_marker", At: at})

	actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: "cyc-step-007", At: at.Add(cfg.ClearConfirmBackstop)})
	assertKinds(t, actions, []ActionKind{
		ActEmit, ActEmit, ActSetManagedSession, ActCancelTimer, ActCancelTimer,
		ActWriteJournal,
	})
	if actions[0].Type != core.EventTypeSessionKeeperClearUnconfirmed {
		t.Fatalf("emit[0] = %v; want clear_unconfirmed", actions[0].Type)
	}
	if actions[1].Type != core.EventTypeSessionKeeperCycleAborted {
		t.Fatalf("emit[1] = %v; want cycle_aborted", actions[1].Type)
	}
	if actions[2].SID != "" {
		t.Fatalf("managed rebind = %q; want \"\" (cleared for .sid rebind)", actions[2].SID)
	}
	if st := m.State(); st.Phase != PhaseIdle || st.LastTerminal != "failed" {
		t.Fatalf("state = %v/%v; want Idle/failed", st.Phase, st.LastTerminal)
	}
}

// TestStep_ClearSettle_ObservesWithoutResubmitting proves that a queued clear
// cannot become a clear storm. Settle ticks only re-arm observation. The
// independent backstop ends the restart if no new session appears.
func TestStep_ClearSettle_ObservesWithoutResubmitting(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	cfg.ClearConfirmRetries = 5
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "cyc-step-008"))
	m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-step-008", At: at})
	m.Step(Event{Kind: EvModelDone, CycleID: "cyc-step-008", SessionID: "sess-1", Source: "idle_marker", At: at})

	for range 3 {
		actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerClearSettle, CycleID: "cyc-step-008", At: at})
		assertKinds(t, actions, []ActionKind{ActArmTimer})
	}
	actions := m.Step(Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: "cyc-step-008", At: at})
	if actions[0].Kind != ActEmit || actions[0].Type != core.EventTypeSessionKeeperClearUnconfirmed {
		t.Fatalf("backstop action[0] = %+v; want Emit(clear_unconfirmed)", actions[0])
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
		gaugeTickAt(at, "cyc-step-009"),
		{Kind: EvNonceObserved, CycleID: "cyc-step-009", At: at.Add(time.Second)},
		{Kind: EvModelDone, CycleID: "cyc-step-009", SessionID: "sess-1", Source: "idle_marker", At: at.Add(2 * time.Second)},
		{Kind: EvSessionChanged, CycleID: "cyc-step-009", PrevSID: "sess-1", NewSID: "sess-2", At: at.Add(3 * time.Second)},
	})
	eff := &substrate.FakeEffector[Action]{}

	if err := m.Run(context.Background(), src, eff); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := kinds(eff.Actions())
	want := []ActionKind{
		ActWriteJournal, ActEmit, ActSendEscape, ActInjectHandoffCmd, ActWriteJournal, ActArmTimer,
		// confirm (journal + handoff_written + cancel + model-done arm, T8)
		ActWriteJournal, ActEmit, ActCancelTimer, ActArmTimer,
		// clearing (model_done + env + /clear + clear_sent + journal + cancel + arms)
		ActEmit, ActSetTmuxEnv, ActInjectClear, ActEmit, ActWriteJournal,
		ActCancelTimer, ActArmTimer, ActArmTimer,
		// briefing terminal (new_session_up first, T8)
		ActEmit, ActSetManagedSession, ActCancelTimer, ActCancelTimer,
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
