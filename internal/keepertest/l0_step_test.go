package keepertest_test

// L0 unit tier — pure keeper.Step transition tables + property tests
// (T10; RS-017 L0; measurement-design §3 "L0 unit" row).
//
// Everything here is PURE: the reactor is driven directly (or via
// substrate.SyntheticSource + substrate.FakeEffector + Cycle.Run), no IO, no
// wall clock, no tokens. Timers are events (SK-010), so timer edges are just
// TimerFired inputs.

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertwin"
	"github.com/gregberns/harmonik/internal/substrate"
)

// l0Base is the fixed virtual epoch for L0 tables.
var l0Base = time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

// passGates is the all-gates-pass snapshot.
func passGates() keeper.GateSnapshot {
	return keeper.GateSnapshot{Managed: true, CrispIdle: true}
}

// gaugeTick builds a cycle-entry GaugeTick event.
func gaugeTick(cycleID, sid string, pct float64, gates keeper.GateSnapshot, at time.Time) keeper.Event {
	return keeper.Event{
		Kind:    keeper.EvGaugeTick,
		At:      at,
		CF:      &keeper.CtxFile{Pct: pct, SessionID: sid},
		Gates:   gates,
		CycleID: cycleID,
	}
}

// runSynthetic drives the reactor over a fixed event slice via the substrate
// doubles (SyntheticSource + FakeEffector + Run) and returns the recorded
// actions plus the reactor for state inspection.
func runSynthetic(t *testing.T, cfg *keeper.CyclerConfig, events []keeper.Event) ([]keeper.Action, *keeper.Cycle) {
	t.Helper()
	cyc := keeper.NewCycle(cfg)
	src := substrate.NewSyntheticSource(events)
	eff := &substrate.FakeEffector[keeper.Action]{}
	if err := cyc.Run(context.Background(), src, eff); err != nil {
		t.Fatalf("run: %v", err)
	}
	return eff.Actions(), cyc
}

// ─── Gate-ladder table ───────────────────────────────────────────────────────

// TestL0_GateLadderTable walks the MaybeRun gate ladder branch by branch
// (SK-011): each failing gate must leave the machine Idle and start no cycle.
func TestL0_GateLadderTable(t *testing.T) {
	t.Parallel()
	const sid = "sid-l0-gates"

	cases := []struct {
		name       string
		mutate     func(cfg *keeper.CyclerConfig, ev *keeper.Event)
		wantFire   bool
		wantAction keeper.ActionKind // "" → want zero actions
	}{
		{"all_gates_pass_fires", func(*keeper.CyclerConfig, *keeper.Event) {}, true, ""},
		{"gate1_not_managed", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.Managed = false
		}, false, ""},
		{"gate2_empty_session_id", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.CF.SessionID = ""
		}, false, ""},
		{"gate3_below_act_threshold", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.CF.Pct = 50
		}, false, ""},
		{"gate4_not_crisp_idle", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.CrispIdle = false
		}, false, ""},
		{"gate4_force_overrides_crisp_idle", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.CrispIdle = false
			ev.CF.Pct = 97 // above ForceActPct
		}, true, ""},
		{"gate5_holding_dispatch", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.HoldingDispatch = true
		}, false, ""},
		{"gate5b_sleeping", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.Sleeping = true
		}, false, ""},
		{"gate5c_held", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.Held = true
		}, false, ""},
		{"gate5d_recent_operator_turn_sets_hold", func(cfg *keeper.CyclerConfig, ev *keeper.Event) {
			cfg.OperatorTurnLookback = time.Minute
			ev.Gates.LastUserTurnAt = ev.At.Add(-10 * time.Second)
		}, false, keeper.ActSetHold},
		{"gate5e_post_answer_grace", func(cfg *keeper.CyclerConfig, ev *keeper.Event) {
			cfg.PostAnswerGrace = time.Minute
			ev.Gates.LastAssistantTurnAt = ev.At.Add(-10 * time.Second)
		}, false, ""},
		{"gate7_operator_attached", func(_ *keeper.CyclerConfig, ev *keeper.Event) {
			ev.Gates.OperatorAttached = true
		}, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig("l0-gates")
			ev := gaugeTick("cyc-l0-gate", sid, 92, passGates(), l0Base)
			tc.mutate(cfg, &ev)

			cyc := keeper.NewCycle(cfg)
			actions := cyc.Step(ev)

			if got := cyc.InCycle(); got != tc.wantFire {
				t.Fatalf("InCycle = %v, want %v (actions %v)", got, tc.wantFire, actions)
			}
			if !tc.wantFire {
				switch tc.wantAction {
				case "":
					if len(actions) != 0 {
						t.Fatalf("want no actions on gated-out tick, got %v", actions)
					}
				default:
					if len(actions) != 1 || actions[0].Kind != tc.wantAction {
						t.Fatalf("want single %s action, got %v", tc.wantAction, actions)
					}
				}
			}
		})
	}
}

// ─── Path tables ─────────────────────────────────────────────────────────────

// TestL0_CleanCycleTable is the full clean-complete transition table:
// entry → nonce → model-done → SID flip → brief → Complete.
func TestL0_CleanCycleTable(t *testing.T) {
	t.Parallel()
	cfg := testConfig("l0-clean")
	events := []keeper.Event{
		gaugeTick("cyc-l0-clean", "sid-old", 92, passGates(), l0Base),
		{Kind: keeper.EvNonceObserved, CycleID: "cyc-l0-clean", At: l0Base.Add(5 * time.Second)},
		{Kind: keeper.EvModelDone, CycleID: "cyc-l0-clean", SessionID: "sid-old", Source: "idle_marker", At: l0Base.Add(6 * time.Second)},
		{Kind: keeper.EvSessionChanged, CycleID: "cyc-l0-clean", PrevSID: "sid-old", NewSID: "sid-new", At: l0Base.Add(10 * time.Second)},
	}
	actions, cyc := runSynthetic(t, cfg, events)

	wantEmits := []core.EventType{
		core.EventTypeSessionKeeperHandoffStarted,
		core.EventTypeSessionKeeperHandoffWritten,
		core.EventTypeSessionKeeperModelDone,
		core.EventTypeSessionKeeperClearSent,
		core.EventTypeSessionKeeperNewSessionUp,
		core.EventTypeSessionKeeperCycleComplete,
	}
	got := emittedTypes(actions)
	if len(got) != len(wantEmits) {
		t.Fatalf("emitted types = %v, want %v", got, wantEmits)
	}
	for i := range wantEmits {
		if got[i] != wantEmits[i] {
			t.Fatalf("emit[%d] = %s, want %s (all %v)", i, got[i], wantEmits[i], got)
		}
	}

	// Injection sequence: escape → handoff cmd → clear → brief, in order.
	var injects []keeper.ActionKind
	for _, a := range actions {
		switch a.Kind {
		case keeper.ActSendEscape, keeper.ActInjectHandoffCmd, keeper.ActInjectClear, keeper.ActInjectBrief:
			injects = append(injects, a.Kind)
		}
	}
	wantInjects := []keeper.ActionKind{
		keeper.ActSendEscape, keeper.ActInjectHandoffCmd, keeper.ActInjectClear, keeper.ActInjectBrief,
	}
	if len(injects) != len(wantInjects) {
		t.Fatalf("injection sequence = %v, want %v", injects, wantInjects)
	}
	for i := range wantInjects {
		if injects[i] != wantInjects[i] {
			t.Fatalf("inject[%d] = %s, want %s", i, injects[i], wantInjects[i])
		}
	}

	st := cyc.State()
	if st.Phase != keeper.PhaseIdle || st.LastTerminal != "complete" {
		t.Fatalf("terminal state = (%s, %s), want (idle, complete)", st.Phase, st.LastTerminal)
	}
	if cyc.InCycle() {
		t.Fatal("InCycle after terminal")
	}
}

// TestL0_AbortTable is the handoff-timeout abort table: the ONLY path that
// never sends /clear (SK §8.2).
func TestL0_AbortTable(t *testing.T) {
	t.Parallel()
	cfg := testConfig("l0-abort")
	events := []keeper.Event{
		gaugeTick("cyc-l0-abort", "sid-a", 92, passGates(), l0Base),
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerHandoffTimeout, CycleID: "cyc-l0-abort", At: l0Base.Add(cfg.HandoffTimeout)},
	}
	actions, cyc := runSynthetic(t, cfg, events)

	types := emittedTypes(actions)
	if countType(types, core.EventTypeSessionKeeperCycleAborted) != 1 {
		t.Fatalf("want exactly 1 cycle_aborted, got %v", types)
	}
	if countType(types, core.EventTypeSessionKeeperCycleComplete) != 0 {
		t.Fatalf("unexpected cycle_complete on abort path: %v", types)
	}
	for _, a := range actions {
		if a.Kind == keeper.ActInjectClear {
			t.Fatal("abort path must NEVER send /clear (hk-vpnp Bug 3)")
		}
		if a.Kind == keeper.ActEmit && a.Type == core.EventTypeSessionKeeperCycleAborted {
			var p core.SessionKeeperCycleAbortedPayload
			if err := json.Unmarshal(a.Payload, &p); err != nil {
				t.Fatalf("decode aborted payload: %v", err)
			}
			if p.Reason != "handoff_timeout" {
				t.Fatalf("abort reason = %q, want handoff_timeout (metric 4: explicit-reasoned)", p.Reason)
			}
		}
	}
	if st := cyc.State(); st.LastTerminal != "aborted" {
		t.Fatalf("LastTerminal = %q, want aborted", st.LastTerminal)
	}
}

// TestL0_FreshnessRecoveryTable is the hk-fi78d recovery edge: nonce echo
// times out but a fresh handoff was seen → proceed, handoff_written carries
// recovered:true, cycle_recovered rides the completion.
func TestL0_FreshnessRecoveryTable(t *testing.T) {
	t.Parallel()
	cfg := testConfig("l0-recover")
	mt := l0Base.Add(2 * time.Minute)
	events := []keeper.Event{
		gaugeTick("cyc-l0-rec", "sid-r", 92, passGates(), l0Base),
		{Kind: keeper.EvHandoffFreshSeen, CycleID: "cyc-l0-rec", Mtime: mt, At: l0Base.Add(cfg.HandoffTimeout)},
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerHandoffTimeout, CycleID: "cyc-l0-rec", At: l0Base.Add(cfg.HandoffTimeout)},
		{Kind: keeper.EvModelDone, CycleID: "cyc-l0-rec", SessionID: "sid-r", Source: "idle_marker", At: l0Base.Add(cfg.HandoffTimeout + time.Second)},
		{Kind: keeper.EvSessionChanged, CycleID: "cyc-l0-rec", PrevSID: "sid-r", NewSID: "sid-r2", At: l0Base.Add(cfg.HandoffTimeout + 5*time.Second)},
	}
	actions, cyc := runSynthetic(t, cfg, events)

	types := emittedTypes(actions)
	if countType(types, core.EventTypeSessionKeeperCycleComplete) != 1 {
		t.Fatalf("want cycle_complete on recovery path, got %v", types)
	}
	if countType(types, core.EventTypeSessionKeeperCycleRecovered) != 1 {
		t.Fatalf("want cycle_recovered on recovery path, got %v", types)
	}
	found := false
	for _, a := range actions {
		if a.Kind == keeper.ActEmit && a.Type == core.EventTypeSessionKeeperHandoffWritten {
			var p core.SessionKeeperHandoffWrittenPayload
			if err := json.Unmarshal(a.Payload, &p); err != nil {
				t.Fatalf("decode handoff_written payload: %v", err)
			}
			if !p.Recovered {
				t.Fatal("handoff_written.recovered = false, want true on the recovery edge (00b R1)")
			}
			if p.HandoffMtime == "" {
				t.Fatal("handoff_written.handoff_mtime empty on the recovery edge")
			}
			if p.Nonce != "" {
				t.Fatalf("handoff_written.nonce = %q, want empty on the recovery edge", p.Nonce)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no handoff_written emit found")
	}
	if st := cyc.State(); st.LastTerminal != "complete" {
		t.Fatalf("LastTerminal = %q, want complete", st.LastTerminal)
	}
}

// TestL0_ClearUnconfirmedBackstopTable is the degraded terminal: the clear
// backstop fires with no SID flip → clear_unconfirmed + cycle_complete
// (NOT an abort; SK §8.3), and the managed binding is cleared.
func TestL0_ClearUnconfirmedBackstopTable(t *testing.T) {
	t.Parallel()
	cfg := testConfig("l0-degraded")
	events := []keeper.Event{
		gaugeTick("cyc-l0-deg", "sid-d", 92, passGates(), l0Base),
		{Kind: keeper.EvNonceObserved, CycleID: "cyc-l0-deg", At: l0Base.Add(5 * time.Second)},
		{Kind: keeper.EvModelDone, CycleID: "cyc-l0-deg", SessionID: "sid-d", Source: "idle_marker", At: l0Base.Add(5 * time.Second)},
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerClearBackstop, CycleID: "cyc-l0-deg", At: l0Base.Add(5*time.Second + cfg.ClearConfirmBackstop)},
	}
	actions, cyc := runSynthetic(t, cfg, events)

	types := emittedTypes(actions)
	if countType(types, core.EventTypeSessionKeeperClearUnconfirmed) != 1 ||
		countType(types, core.EventTypeSessionKeeperCycleComplete) != 1 {
		t.Fatalf("want clear_unconfirmed + cycle_complete, got %v", types)
	}
	managedCleared := false
	for _, a := range actions {
		if a.Kind == keeper.ActSetManagedSession && a.SID == "" {
			managedCleared = true
		}
	}
	if !managedCleared {
		t.Fatal("managed binding not cleared on the unconfirmed path")
	}
	if st := cyc.State(); st.LastTerminal != "complete" {
		t.Fatalf("LastTerminal = %q, want complete (degraded completion is NOT an abort)", st.LastTerminal)
	}
}

// TestL0_SettleRetryTable is the hk-vdqe2 hard gate purely: each settle-window
// expiry defensively re-injects /clear with an incremented clear_sent attempt
// until retries exhaust, then the cycle degrades to clear_unconfirmed.
func TestL0_SettleRetryTable(t *testing.T) {
	t.Parallel()
	cfg := testConfig("l0-settle")
	cfg.ClearConfirmRetries = 3
	events := []keeper.Event{
		gaugeTick("cyc-l0-settle", "sid-s", 92, passGates(), l0Base),
		{Kind: keeper.EvNonceObserved, CycleID: "cyc-l0-settle", At: l0Base.Add(time.Second)},
		{Kind: keeper.EvModelDone, CycleID: "cyc-l0-settle", SessionID: "sid-s", Source: "idle_marker", At: l0Base.Add(time.Second)},
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerClearSettle, CycleID: "cyc-l0-settle", At: l0Base.Add(11 * time.Second)},
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerClearSettle, CycleID: "cyc-l0-settle", At: l0Base.Add(21 * time.Second)},
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerClearSettle, CycleID: "cyc-l0-settle", At: l0Base.Add(31 * time.Second)},
	}
	actions, cyc := runSynthetic(t, cfg, events)

	var attempts []int
	clears := 0
	for _, a := range actions {
		switch {
		case a.Kind == keeper.ActInjectClear:
			clears++
		case a.Kind == keeper.ActEmit && a.Type == core.EventTypeSessionKeeperClearSent:
			var p core.SessionKeeperClearSentPayload
			if err := json.Unmarshal(a.Payload, &p); err != nil {
				t.Fatalf("decode clear_sent payload: %v", err)
			}
			attempts = append(attempts, p.Attempt)
		}
	}
	// Entry attempt 1, then re-injects at settle firings 1 and 2 (attempts 2, 3);
	// the third firing finds ClearAttempt >= retries and degrades instead.
	if clears != 3 {
		t.Fatalf("InjectClear count = %d, want 3 (1 entry + 2 defensive re-injects)", clears)
	}
	if len(attempts) != 3 || attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 3 {
		t.Fatalf("clear_sent attempts = %v, want [1 2 3]", attempts)
	}
	types := emittedTypes(actions)
	if countType(types, core.EventTypeSessionKeeperClearUnconfirmed) != 1 ||
		countType(types, core.EventTypeSessionKeeperCycleComplete) != 1 {
		t.Fatalf("want clear_unconfirmed + cycle_complete after retries exhaust, got %v", types)
	}
	if cyc.InCycle() {
		t.Fatal("InCycle after retries-exhausted terminal")
	}
}

// TestL0_ModelDoneTimeoutFailOpen is the SK-014/SR9 fail-open bound: a lost
// model-done signal proceeds to Clearing DEGRADED (source "timeout",
// degraded:true) instead of wedging.
func TestL0_ModelDoneTimeoutFailOpen(t *testing.T) {
	t.Parallel()
	cfg := testConfig("l0-mdto")
	events := []keeper.Event{
		gaugeTick("cyc-l0-mdto", "sid-m", 92, passGates(), l0Base),
		{Kind: keeper.EvNonceObserved, CycleID: "cyc-l0-mdto", At: l0Base.Add(time.Second)},
		{Kind: keeper.EvTimerFired, Timer: keeper.TimerModelDone, CycleID: "cyc-l0-mdto", At: l0Base.Add(time.Second + cfg.ModelDoneTimeout)},
		{Kind: keeper.EvSessionChanged, CycleID: "cyc-l0-mdto", PrevSID: "sid-m", NewSID: "sid-m2", At: l0Base.Add(2 * time.Minute)},
	}
	actions, _ := runSynthetic(t, cfg, events)

	found := false
	for _, a := range actions {
		if a.Kind == keeper.ActEmit && a.Type == core.EventTypeSessionKeeperModelDone {
			var p core.SessionKeeperModelDonePayload
			if err := json.Unmarshal(a.Payload, &p); err != nil {
				t.Fatalf("decode model_done payload: %v", err)
			}
			if p.Source != "timeout" || !p.Degraded {
				t.Fatalf("model_done = (source %q, degraded %v), want (timeout, true)", p.Source, p.Degraded)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no model_done emit on the fail-open path")
	}
	types := emittedTypes(actions)
	if countType(types, core.EventTypeSessionKeeperCycleComplete) != 1 {
		t.Fatalf("want cycle_complete after fail-open, got %v", types)
	}
}

// ─── Stimulus-codec golden round-trip ────────────────────────────────────────

// TestL0_StimulusCodecRoundTrip encodes a synthesized schedule and decodes it
// back through the Twin (keeperCodec.DecodeLine): the decoded stream must be
// byte-equivalent to the source events (golden-decode; measurement-design §3
// L0 "keeperCodec golden-decode").
func TestL0_StimulusCodecRoundTrip(t *testing.T) {
	t.Parallel()
	sum := keepertwin.CycleSummary{
		CKey: "l0|cyc-l0-rt", AgentName: "l0", CycleID: "cyc-l0-rt",
		Outcome: "complete", ClearUnconfirmed: false,
		StartedAt: "2026-07-13T00:00:00Z", SessionIDStart: "sid-rt",
	}
	events, err := keepertwin.SynthesizeStimulus(sum)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	raw, err := keepertwin.EncodeStimulus(events)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	twin := keepertwin.New(bytes.NewReader(raw), keepertwin.FaultConfig{})
	var decoded []keeper.Event
	for ev := range twin.Events(ctx) {
		decoded = append(decoded, ev)
	}

	if len(decoded) != len(events) {
		t.Fatalf("decoded %d events, want %d", len(decoded), len(events))
	}
	for i := range events {
		wantJSON, _ := json.Marshal(events[i])
		gotJSON, _ := json.Marshal(decoded[i])
		if string(wantJSON) != string(gotJSON) {
			t.Fatalf("event %d round-trip mismatch:\n want %s\n got  %s", i, wantJSON, gotJSON)
		}
	}
}

// ─── Property tests (SR3/SR4/SR6/SR7 as pure postconditions) ─────────────────

// TestL0_Properties_SRPostconditions feeds seeded-random event sequences to
// the pure reactor and asserts the SR invariants as postconditions over the
// emitted action order (measurement-design §3 L0 "property tests"). The seed
// is fixed, so runs are deterministic across -count and -race.
func TestL0_Properties_SRPostconditions(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20260713)) //nolint:gosec // deterministic property-test seed

	sids := []string{"sid-p1", "sid-p2", "sid-p3"}
	timers := []keeper.TimerKind{
		keeper.TimerHandoffTimeout, keeper.TimerModelDone,
		keeper.TimerClearSettle, keeper.TimerClearBackstop,
	}
	sources := []string{"idle_marker", "transcript_turn"}

	const sequences = 300
	const maxEvents = 40

	for seq := 0; seq < sequences; seq++ {
		cfg := testConfig("l0-prop")
		cyc := keeper.NewCycle(cfg)
		at := l0Base
		cycleN := 0

		var actions []keeper.Action
		n := 2 + rng.Intn(maxEvents)
		for i := 0; i < n; i++ {
			at = at.Add(time.Duration(1+rng.Intn(120)) * time.Second)
			var ev keeper.Event
			switch rng.Intn(6) {
			case 0, 1: // entry tick (CycleID minted per entry, shell-style)
				cycleN++
				gates := passGates()
				if rng.Intn(4) == 0 {
					gates.CrispIdle = false
				}
				if rng.Intn(6) == 0 {
					gates.HoldingDispatch = true
				}
				pct := 70 + rng.Float64()*30
				ev = gaugeTick("cyc-prop-"+strconv.Itoa(seq)+"-"+strconv.Itoa(cycleN), sids[rng.Intn(len(sids))], pct, gates, at)
			case 2:
				ev = keeper.Event{Kind: keeper.EvNonceObserved, CycleID: cyc.State().CycleID, At: at}
			case 3:
				ev = keeper.Event{
					Kind: keeper.EvModelDone, CycleID: cyc.State().CycleID,
					SessionID: sids[rng.Intn(len(sids))], Source: sources[rng.Intn(len(sources))], At: at,
				}
			case 4:
				ev = keeper.Event{
					Kind: keeper.EvSessionChanged, CycleID: cyc.State().CycleID,
					PrevSID: sids[rng.Intn(len(sids))], NewSID: sids[rng.Intn(len(sids))], At: at,
				}
			case 5:
				ev = keeper.Event{
					Kind: keeper.EvTimerFired, Timer: timers[rng.Intn(len(timers))],
					CycleID: cyc.State().CycleID, At: at,
				}
			}
			actions = append(actions, cyc.Step(ev)...)
		}
		assertSRPostconditions(t, seq, actions)
	}
}

// assertSRPostconditions checks the pure-order SR invariants over one emitted
// action stream.
func assertSRPostconditions(t *testing.T, seq int, actions []keeper.Action) {
	t.Helper()
	open := false // a cycle is open (handoff_started seen, no terminal yet) — SR7
	sawWritten, sawModelDone, sawUp, sawUnconfirmed := false, false, false, false

	for i, a := range actions {
		if a.Kind == keeper.ActInjectClear && !sawModelDone {
			t.Fatalf("seq %d action %d: SR4 violated — InjectClear before model_done emit", seq, i)
		}
		if a.Kind != keeper.ActEmit {
			continue
		}
		switch a.Type {
		case core.EventTypeSessionKeeperHandoffStarted:
			if open {
				t.Fatalf("seq %d action %d: SR7 violated — handoff_started while a cycle is open", seq, i)
			}
			open = true
			sawWritten, sawModelDone, sawUp, sawUnconfirmed = false, false, false, false
		case core.EventTypeSessionKeeperHandoffWritten:
			sawWritten = true
		case core.EventTypeSessionKeeperModelDone:
			sawModelDone = true
		case core.EventTypeSessionKeeperClearSent:
			if !sawWritten {
				t.Fatalf("seq %d action %d: SR3 violated — clear_sent before handoff_written", seq, i)
			}
			if !sawModelDone {
				t.Fatalf("seq %d action %d: SR4 violated — clear_sent before model_done", seq, i)
			}
		case core.EventTypeSessionKeeperNewSessionUp:
			sawUp = true
		case core.EventTypeSessionKeeperClearUnconfirmed:
			sawUnconfirmed = true
		case core.EventTypeSessionKeeperCycleComplete:
			if !open {
				t.Fatalf("seq %d action %d: terminal exclusivity violated — cycle_complete with no open cycle", seq, i)
			}
			if !sawUp && !sawUnconfirmed {
				t.Fatalf("seq %d action %d: SR6 violated — cycle_complete before new_session_up (no degraded clear_unconfirmed either)", seq, i)
			}
			open = false
		case core.EventTypeSessionKeeperCycleAborted:
			if !open {
				t.Fatalf("seq %d action %d: terminal exclusivity violated — cycle_aborted with no open cycle", seq, i)
			}
			open = false
		}
	}
}
