package codexreactor_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/codexreactor"
)

type scenarioStep struct {
	In  codexreactor.Event    `json:"in"`
	Out []codexreactor.Action `json:"out"`
}

func scenariosDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "codex-app-server", "reactor-scenarios")
}

func runScenario(t *testing.T, name string) {
	t.Helper()
	path := filepath.Join(scenariosDir(), name+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open scenario %q: %v", name, err)
	}
	t.Cleanup(func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close event file: %v", closeErr)
		}
	})

	r := codexreactor.New()

	sc := bufio.NewScanner(f)
	stepNum := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		stepNum++

		var step scenarioStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			t.Fatalf("step %d: unmarshal: %v\n  line: %s", stepNum, err, line)
		}

		got := r.Step(step.In)

		want := step.Out
		if len(want) == 0 {
			want = nil
		}
		if len(got) == 0 {
			got = nil
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("step %d (seq=%d type=%s): action mismatch\n  want: %s\n  got:  %s",
				stepNum, step.In.Seq, step.In.Type,
				formatActions(want), formatActions(got))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if stepNum == 0 {
		t.Fatalf("scenario %q: no steps found", name)
	}
	t.Logf("scenario %q: %d steps OK", name, stepNum)
}

func formatActions(actions []codexreactor.Action) string {
	if len(actions) == 0 {
		return "[]"
	}
	b, err := json.Marshal(actions)
	if err != nil {
		return "<marshal error: " + err.Error() + ">"
	}
	return string(b)
}

func TestReactor_Scenario_Happy(t *testing.T) {
	runScenario(t, "happy")
}

func TestReactor_Scenario_ToolCallMidTurn(t *testing.T) {
	runScenario(t, "tool-call-mid-turn")
}

func TestReactor_Scenario_Error(t *testing.T) {
	runScenario(t, "error")
}

func TestReactor_Scenario_Cancel(t *testing.T) {
	runScenario(t, "cancel")
}

func TestReactor_Scenario_ReconnectResume(t *testing.T) {
	runScenario(t, "reconnect-resume")
}

func TestReactor_Scenario_TokenPressure(t *testing.T) {
	runScenario(t, "token-pressure")
}

func TestReactor_Scenario_OutOfOrderDup(t *testing.T) {
	runScenario(t, "out-of-order-dup")
}

// TestReactor_Invariant_Backpressure verifies I1: a TurnCompleted while no
// turn is in-flight is a no-op (stale event).
func TestReactor_Invariant_Backpressure(t *testing.T) {
	r := codexreactor.New()

	got := r.Step(codexreactor.Event{
		Seq:      1,
		Type:     codexreactor.EventTypeTurnCompleted,
		ThreadID: "t1",
		TurnID:   "u1",
		Status:   "completed",
	})
	if len(got) != 0 {
		t.Errorf("stale TurnCompleted should produce no actions; got %s", formatActions(got))
	}

	r.Step(codexreactor.Event{Seq: 2, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u2"})
	r.Step(codexreactor.Event{Seq: 3, Type: codexreactor.EventTypeTurnCompleted, ThreadID: "t1", TurnID: "u2", Status: "completed"})

	spurious := r.Step(codexreactor.Event{Seq: 4, Type: codexreactor.EventTypeTurnCompleted, ThreadID: "t1", TurnID: "u2", Status: "completed"})
	if len(spurious) != 0 {
		t.Errorf("spurious second TurnCompleted should be no-op; got %s", formatActions(spurious))
	}

	if r.State().InFlight {
		t.Error("InFlight should be false after completed turn")
	}
}

// TestReactor_Invariant_DedupBySeq verifies I2: events with Seq ≤ LastSeq are
// dropped; Seq=0 always passes.
func TestReactor_Invariant_DedupBySeq(t *testing.T) {
	r := codexreactor.New()

	r.Step(codexreactor.Event{Seq: 3, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u1"})
	if r.State().LastSeq != 3 {
		t.Fatalf("LastSeq should be 3, got %d", r.State().LastSeq)
	}

	got := r.Step(codexreactor.Event{
		Seq:   2,
		Type:  codexreactor.EventTypeMessageDelta,
		Delta: "stale",
	})
	if len(got) != 0 {
		t.Errorf("out-of-order event (seq=2 < lastSeq=3) should be dropped; got %s", formatActions(got))
	}

	got = r.Step(codexreactor.Event{
		Seq:   3,
		Type:  codexreactor.EventTypeMessageDelta,
		Delta: "dup",
	})
	if len(got) != 0 {
		t.Errorf("duplicate event (seq=3 == lastSeq=3) should be dropped; got %s", formatActions(got))
	}

	got = r.Step(codexreactor.Event{
		Seq:      4,
		Type:     codexreactor.EventTypeMessageDelta,
		ThreadID: "t1",
		TurnID:   "u1",
		ItemID:   "i1",
		Delta:    "fresh",
	})
	if len(got) != 1 || got[0].Type != codexreactor.ActionTypeEmitOutput {
		t.Errorf("seq=4 should produce EmitOutput; got %s", formatActions(got))
	}
	if r.State().LastSeq != 4 {
		t.Errorf("LastSeq should be 4, got %d", r.State().LastSeq)
	}

	got = r.Step(codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeConnected})
	if len(got) != 0 {
		t.Errorf("Connected (seq=0) should produce no action; got %s", formatActions(got))
	}
	if r.State().LastSeq != 0 {
		t.Errorf("Connected should reset LastSeq to 0; got %d", r.State().LastSeq)
	}
}

// TestReactor_MidTurnSupersede verifies that a TurnStarted arriving while a
// turn is already in-flight emits a terminal for the orphaned turn (a
// superseded turn must not vanish without a terminal) and adopts the new turn.
func TestReactor_MidTurnSupersede(t *testing.T) {
	r := codexreactor.New()

	r.Step(codexreactor.Event{Seq: 1, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u1"})

	got := r.Step(codexreactor.Event{Seq: 2, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u2"})
	want := []codexreactor.Action{
		{Type: codexreactor.ActionTypeCompleteTurn, ThreadID: "t1", TurnID: "u1", Status: "superseded"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("supersede should emit a terminal for u1\n  want: %s\n  got:  %s", formatActions(want), formatActions(got))
	}
	if s := r.State(); s.TurnID != "u2" || !s.InFlight {
		t.Errorf("after supersede want TurnID=u2 InFlight=true; got TurnID=%q InFlight=%v", s.TurnID, s.InFlight)
	}

	got = r.Step(codexreactor.Event{Seq: 3, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u2"})
	if len(got) != 0 {
		t.Errorf("re-announcing the in-flight turn should emit no terminal; got %s", formatActions(got))
	}
}

// TestReactor_ConnectedResetsThreadID verifies that a reconnect clears the
// stale ThreadID so the first post-reconnect telemetry is not misattributed.
func TestReactor_ConnectedResetsThreadID(t *testing.T) {
	r := codexreactor.New()
	r.Step(codexreactor.Event{Seq: 1, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u1"})
	r.Step(codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeConnected})
	if s := r.State(); s.ThreadID != "" {
		t.Errorf("Connected should clear ThreadID; got %q", s.ThreadID)
	}
}

// TestReactor_Run_SyntheticSource verifies that Run drives the reactor loop via
// SyntheticSource and FakeEffector, collecting actions correctly.
func TestReactor_Run_SyntheticSource(t *testing.T) {
	events := []codexreactor.Event{
		{Seq: 1, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u1"},
		{Seq: 2, Type: codexreactor.EventTypeMessageDelta, ThreadID: "t1", TurnID: "u1", ItemID: "i1", Delta: "hello"},
		{Seq: 3, Type: codexreactor.EventTypeTurnCompleted, ThreadID: "t1", TurnID: "u1", Status: "completed"},
	}

	src := codexreactor.NewSyntheticSource(events)
	eff := &codexreactor.FakeEffector{}
	r := codexreactor.New()

	if err := r.Run(context.Background(), src, eff); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := eff.Actions()
	want := []codexreactor.Action{
		{Type: codexreactor.ActionTypeEmitOutput, ThreadID: "t1", TurnID: "u1", ItemID: "i1", Delta: "hello"},
		{Type: codexreactor.ActionTypeCompleteTurn, ThreadID: "t1", TurnID: "u1", Status: "completed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Run actions mismatch\n  want: %s\n  got:  %s", formatActions(want), formatActions(got))
	}

	if r.State().InFlight {
		t.Error("InFlight should be false after Run completes")
	}
}

// TestReactor_Run_ContextCancel verifies that Run stops cleanly when ctx is
// cancelled (even if the source has more events buffered).
func TestReactor_Run_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	events := []codexreactor.Event{
		{Seq: 1, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u1"},
	}
	src := codexreactor.NewSyntheticSource(events)
	eff := &codexreactor.FakeEffector{}
	r := codexreactor.New()

	if err := r.Run(ctx, src, eff); err != nil {
		t.Fatalf("Run with cancelled ctx: %v", err)
	}
	if got := eff.Actions(); len(got) != 0 {
		t.Errorf("expected no actions with cancelled ctx; got %s", formatActions(got))
	}
}

// TestFakeEffector_RecordAndReset verifies that FakeEffector records and resets correctly.
func TestFakeEffector_RecordAndReset(t *testing.T) {
	eff := &codexreactor.FakeEffector{}
	ctx := context.Background()

	a1 := codexreactor.Action{Type: codexreactor.ActionTypeEmitOutput, Delta: "a"}
	a2 := codexreactor.Action{Type: codexreactor.ActionTypeCompleteTurn, Status: "completed"}

	if err := eff.Execute(ctx, a1); err != nil {
		t.Fatalf("execute first action: %v", err)
	}
	if err := eff.Execute(ctx, a2); err != nil {
		t.Fatalf("execute second action: %v", err)
	}

	got := eff.Actions()
	if len(got) != 2 {
		t.Fatalf("expected 2 actions; got %d", len(got))
	}
	if got[0] != a1 || got[1] != a2 {
		t.Errorf("wrong actions: %v", got)
	}

	eff.Reset()
	if got = eff.Actions(); len(got) != 0 {
		t.Errorf("expected empty after Reset; got %v", got)
	}
}

// TestSyntheticSource_DeliverAll verifies all events are delivered in order.
func TestSyntheticSource_DeliverAll(t *testing.T) {
	events := []codexreactor.Event{
		{Seq: 1, Type: codexreactor.EventTypeTurnStarted},
		{Seq: 2, Type: codexreactor.EventTypeMessageDelta, Delta: "x"},
		{Seq: 3, Type: codexreactor.EventTypeTurnCompleted, Status: "completed"},
	}
	src := codexreactor.NewSyntheticSource(events)
	ch := src.Events(context.Background())

	var got []codexreactor.Event
	for ev := range ch {
		got = append(got, ev)
	}
	if !reflect.DeepEqual(got, events) {
		t.Errorf("events mismatch: want %v got %v", events, got)
	}
}

var _ = fmt.Sprintf
