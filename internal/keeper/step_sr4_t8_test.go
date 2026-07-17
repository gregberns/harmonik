package keeper

// step_sr4_t8_test.go — T8 acceptance: SR4 (SK-014 / SK-INV-002, "/clear MUST
// NOT be injected before model-done") asserted over the PURE reactor, plus the
// model_done_timeout fail-open path (SR9: proceed degraded, never silence).
//
// SR4 is structural in the reactor: injectClearAction is the only
// ActInjectClear constructor and refuses until CycleState.ModelDoneSource is
// recorded by stepEnterClearing (the single AwaitModelDone → Clearing edge).
// These tests drive Step sequences and assert the action ordering the
// structure guarantees.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// containsKind reports whether any action in the batch has the given kind.
func containsKind(actions []Action, kind ActionKind) bool {
	for _, a := range actions {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

// TestStep_SR4_NoInjectClearBeforeModelDone drives a Step sequence through
// cycle open and handoff confirmation, then bombards AwaitModelDone with
// every non-model-done event, asserting NO InjectClear action is ever
// produced before a ModelDone event has been processed for the cycle — and
// that the very batch that does process ModelDone orders the model_done
// emission BEFORE the injection.
func TestStep_SR4_NoInjectClearBeforeModelDone(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	var preModelDone [][]Action
	preModelDone = append(preModelDone,
		m.Step(gaugeTickAt(at, "cyc-sr4-001")),
		// Detection noise while awaiting the handoff.
		m.Step(Event{Kind: EvHandoffFreshSeen, CycleID: "cyc-sr4-001", Mtime: at, At: at}),
		m.Step(Event{Kind: EvSessionChanged, CycleID: "cyc-sr4-001", PrevSID: "sess-1", NewSID: "sess-2", At: at}),
		m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-sr4-001", At: at.Add(time.Second)}),
	)
	if st := m.State(); st.Phase != PhaseAwaitModelDone {
		t.Fatalf("phase = %v; want AwaitModelDone", st.Phase)
	}
	// Every non-model-done event in AwaitModelDone: none may yield /clear.
	preModelDone = append(preModelDone,
		m.Step(gaugeTickAt(at.Add(2*time.Second), "")),
		m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-sr4-001", At: at.Add(2 * time.Second)}),
		m.Step(Event{Kind: EvSessionChanged, CycleID: "cyc-sr4-001", PrevSID: "sess-1", NewSID: "sess-2", At: at.Add(2 * time.Second)}),
		m.Step(Event{Kind: EvTimerFired, Timer: TimerHandoffTimeout, CycleID: "cyc-sr4-001", At: at.Add(2 * time.Second)}),
		m.Step(Event{Kind: EvTimerFired, Timer: TimerClearSettle, CycleID: "cyc-sr4-001", At: at.Add(2 * time.Second)}),
		m.Step(Event{Kind: EvTimerFired, Timer: TimerClearBackstop, CycleID: "cyc-sr4-001", At: at.Add(2 * time.Second)}),
	)
	for i, batch := range preModelDone {
		if containsKind(batch, ActInjectClear) {
			t.Fatalf("SR4 violation: batch %d produced InjectClear before ModelDone: %v", i, kinds(batch))
		}
	}
	if st := m.State(); st.Phase != PhaseAwaitModelDone {
		t.Fatalf("phase after noise = %v; want AwaitModelDone (nothing else may advance it)", st.Phase)
	}

	// The ModelDone batch itself: model_done is emitted BEFORE InjectClear.
	batch := m.Step(Event{Kind: EvModelDone, CycleID: "cyc-sr4-001", SessionID: "sess-1", Source: "idle_marker", At: at.Add(3 * time.Second)})
	modelDoneIdx, clearIdx := -1, -1
	for i, a := range batch {
		if a.Kind == ActEmit && a.Type == core.EventTypeSessionKeeperModelDone && modelDoneIdx < 0 {
			modelDoneIdx = i
		}
		if a.Kind == ActInjectClear && clearIdx < 0 {
			clearIdx = i
		}
	}
	if modelDoneIdx < 0 || clearIdx < 0 {
		t.Fatalf("ModelDone batch = %v; want both Emit(model_done) and InjectClear", kinds(batch))
	}
	if modelDoneIdx > clearIdx {
		t.Fatalf("SR4 ordering: model_done emit at %d AFTER InjectClear at %d", modelDoneIdx, clearIdx)
	}
}

// TestStep_SR4_StructurallyUnconstructible probes the guard itself: the only
// ActInjectClear constructor returns nothing until the cycle's model-done
// signal is recorded, and stepEnterClearing is what records it.
func TestStep_SR4_StructurallyUnconstructible(t *testing.T) {
	t.Parallel()
	s := &CycleState{Phase: PhaseAwaitModelDone, CycleID: "cyc-sr4-002"}
	if _, ok := injectClearAction(s); ok {
		t.Fatal("injectClearAction constructed /clear with no model-done signal recorded")
	}
	s.ModelDoneSource = "idle_marker"
	a, ok := injectClearAction(s)
	if !ok || a.Kind != ActInjectClear {
		t.Fatalf("injectClearAction after model-done = (%v,%v); want InjectClear", a, ok)
	}
}

// TestStep_ModelDoneTimeout_FailOpenDegraded is the SK-014/SR9 timeout path:
// TimerFired(model_done_timeout) emits model_done{source:"timeout",
// degraded:true} and PROCEEDS to Clearing (the degraded mode is the
// pre-rebuild clear-immediately behavior; a lost .idle write cannot wedge).
func TestStep_ModelDoneTimeout_FailOpenDegraded(t *testing.T) {
	t.Parallel()
	cfg := stepTestConfig()
	m := NewCycle(cfg)
	at := time.Unix(1_700_000_000, 0)

	m.Step(gaugeTickAt(at, "cyc-sr4-003"))
	confirm := m.Step(Event{Kind: EvNonceObserved, CycleID: "cyc-sr4-003", At: at.Add(time.Second)})
	// The model-done bound is armed on entry to AwaitModelDone.
	armed := false
	for _, a := range confirm {
		if a.Kind == ActArmTimer && a.Timer == TimerModelDone && a.D == cfg.ModelDoneTimeout {
			armed = true
		}
	}
	if !armed {
		t.Fatalf("confirm batch %v; want ArmTimer(model_done_timeout, %v)", kinds(confirm), cfg.ModelDoneTimeout)
	}

	batch := m.Step(Event{
		Kind: EvTimerFired, Timer: TimerModelDone,
		CycleID: "cyc-sr4-003", At: at.Add(time.Second + cfg.ModelDoneTimeout),
	})
	if batch[0].Kind != ActEmit || batch[0].Type != core.EventTypeSessionKeeperModelDone {
		t.Fatalf("timeout batch[0] = %+v; want Emit(model_done)", batch[0])
	}
	var md core.SessionKeeperModelDonePayload
	if err := json.Unmarshal(batch[0].Payload, &md); err != nil {
		t.Fatalf("unmarshal model_done: %v", err)
	}
	if md.Source != "timeout" || !md.Degraded || md.CycleID != "cyc-sr4-003" || !md.Valid() {
		t.Fatalf("model_done payload = %+v; want source=timeout degraded=true cycle_id=cyc-sr4-003", md)
	}
	if !containsKind(batch, ActInjectClear) {
		t.Fatalf("timeout batch %v; want InjectClear (fail-open PROCEEDS to Clearing)", kinds(batch))
	}
	if st := m.State(); st.Phase != PhaseClearing || st.ModelDoneSource != "timeout" {
		t.Fatalf("state = %v/%q; want Clearing/timeout", st.Phase, st.ModelDoneSource)
	}

	// SR9 continuation: the degraded cycle still reaches its terminal.
	m.Step(Event{Kind: EvSessionChanged, CycleID: "cyc-sr4-003", PrevSID: "sess-1", NewSID: "sess-2", At: at.Add(2 * time.Second)})
	if st := m.State(); st.Phase != PhaseIdle || st.LastTerminal != "complete" {
		t.Fatalf("terminal = %v/%v; want Idle/complete", st.Phase, st.LastTerminal)
	}
}
