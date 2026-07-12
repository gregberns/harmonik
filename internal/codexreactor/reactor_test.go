package codexreactor_test

// reactor_test.go — T3 gate tests for the codex-app-server reactor.
//
// Gate (T3 acceptance criteria):
//   - A growing scenario JSONL library (happy, tool-call-mid-turn, error,
//     cancel, reconnect-resume, token-pressure, out-of-order/dup) each
//     asserts the reactor's action sequence.
//   - Invariant I1 (one-turn-in-flight backpressure) is exercised directly.
//   - Invariant I2 (dedup-by-seq) is exercised by out-of-order-dup scenario.
//
// Scenario file format (testdata/codex-app-server/reactor-scenarios/*.jsonl):
// Each non-blank line is a JSON object with:
//
//	{"in": <Event>, "out": [<Action>, ...]}
//
// The test driver feeds "in" to Reactor.Step and asserts the returned actions
// match "out" (in order). An empty "out" array means no actions are expected.
//
// Bead: hk-5co9a [codex-app-server T3]

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

// ─── Scenario test driver ────────────────────────────────────────────────────

// scenarioStep is one line in a scenario JSONL file.
type scenarioStep struct {
	In  codexreactor.Event    `json:"in"`
	Out []codexreactor.Action `json:"out"`
}

// scenariosDir returns the path to the reactor-scenarios testdata directory.
func scenariosDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "codex-app-server", "reactor-scenarios")
}

// runScenario opens a JSONL scenario file, drives a fresh Reactor step-by-step,
// and asserts the action sequence at each step.
func runScenario(t *testing.T, name string) {
	t.Helper()
	path := filepath.Join(scenariosDir(), name+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open scenario %q: %v", name, err)
	}
	defer f.Close()

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

		// Normalise nil vs empty slice for comparison.
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
	b, _ := json.Marshal(actions)
	return string(b)
}

// ─── Scenario tests ──────────────────────────────────────────────────────────

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

// ─── Invariant unit tests ────────────────────────────────────────────────────

// TestReactor_Invariant_Backpressure verifies I1: a TurnCompleted while no
// turn is in-flight is a no-op (stale event).
func TestReactor_Invariant_Backpressure(t *testing.T) {
	r := codexreactor.New()

	// TurnCompleted with no turn in-flight → no action, no panic.
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

	// Now start a turn, complete it, then receive a second spurious completion.
	r.Step(codexreactor.Event{Seq: 2, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u2"})
	r.Step(codexreactor.Event{Seq: 3, Type: codexreactor.EventTypeTurnCompleted, ThreadID: "t1", TurnID: "u2", Status: "completed"})

	spurious := r.Step(codexreactor.Event{Seq: 4, Type: codexreactor.EventTypeTurnCompleted, ThreadID: "t1", TurnID: "u2", Status: "completed"})
	if len(spurious) != 0 {
		t.Errorf("spurious second TurnCompleted should be no-op; got %s", formatActions(spurious))
	}

	// InFlight must be false after all of this.
	if r.State().InFlight {
		t.Error("InFlight should be false after completed turn")
	}
}

// TestReactor_Invariant_DedupBySeq verifies I2: events with Seq ≤ LastSeq are
// dropped; Seq=0 always passes.
func TestReactor_Invariant_DedupBySeq(t *testing.T) {
	r := codexreactor.New()

	// First event at seq=3 sets LastSeq=3.
	r.Step(codexreactor.Event{Seq: 3, Type: codexreactor.EventTypeTurnStarted, ThreadID: "t1", TurnID: "u1"})
	if r.State().LastSeq != 3 {
		t.Fatalf("LastSeq should be 3, got %d", r.State().LastSeq)
	}

	// seq=2 is below LastSeq=3 → dropped (no action, state unchanged).
	got := r.Step(codexreactor.Event{
		Seq:   2,
		Type:  codexreactor.EventTypeMessageDelta,
		Delta: "stale",
	})
	if len(got) != 0 {
		t.Errorf("out-of-order event (seq=2 < lastSeq=3) should be dropped; got %s", formatActions(got))
	}

	// seq=3 exact duplicate → dropped.
	got = r.Step(codexreactor.Event{
		Seq:   3,
		Type:  codexreactor.EventTypeMessageDelta,
		Delta: "dup",
	})
	if len(got) != 0 {
		t.Errorf("duplicate event (seq=3 == lastSeq=3) should be dropped; got %s", formatActions(got))
	}

	// seq=4 advances → processed.
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

	// seq=0 bypasses dedup even though lastSeq=4.
	got = r.Step(codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeConnected})
	if len(got) != 0 {
		t.Errorf("Connected (seq=0) should produce no action; got %s", formatActions(got))
	}
	// Connected resets LastSeq to 0.
	if r.State().LastSeq != 0 {
		t.Errorf("Connected should reset LastSeq to 0; got %d", r.State().LastSeq)
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

	// Run with a cancelled context. SyntheticSource returns an empty channel when
	// ctx is already done, so Run returns nil immediately.
	if err := r.Run(ctx, src, eff); err != nil {
		t.Fatalf("Run with cancelled ctx: %v", err)
	}
	// No actions should have been produced.
	if got := eff.Actions(); len(got) != 0 {
		t.Errorf("expected no actions with cancelled ctx; got %s", formatActions(got))
	}
}

// ─── FakeEffector unit tests ─────────────────────────────────────────────────

// TestFakeEffector_RecordAndReset verifies that FakeEffector records and resets correctly.
func TestFakeEffector_RecordAndReset(t *testing.T) {
	eff := &codexreactor.FakeEffector{}
	ctx := context.Background()

	a1 := codexreactor.Action{Type: codexreactor.ActionTypeEmitOutput, Delta: "a"}
	a2 := codexreactor.Action{Type: codexreactor.ActionTypeCompleteTurn, Status: "completed"}

	_ = eff.Execute(ctx, a1)
	_ = eff.Execute(ctx, a2)

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

// ─── SyntheticSource unit tests ──────────────────────────────────────────────

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

// ─── Helpers ─────────────────────────────────────────────────────────────────

// ensure formatActions is used (suppress unused-variable lint in non-table tests).
var _ = fmt.Sprintf
