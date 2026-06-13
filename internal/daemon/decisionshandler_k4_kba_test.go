package daemon

// decisionshandler_k4_kba_test.go — unit tests for the operator-side decisions
// ops (hitl-decisions component K4, bead hk-kba):
//   - HandleDecisionsList   → returns the open set (≥2 distinct blocked agents in
//                            one result — S2); pure read, emits nothing.
//   - HandleDecisionsAnswer → valid option emits decision_resolved (assert the
//                            JSONL record); bad option on an OPEN decision is an
//                            error (N7); unknown/already-terminal id is a no-op
//                            with NO error and NO new event (N3, S8).
//
// The handler is constructed via NewCommsSendHandler over a REAL file-backed bus
// (NewBusImplWithWriter + OpenJSONLWriter), then SetRecvDeps wires the SAME
// events.jsonl path so HandleDecisionsList/Answer's decisionsProjection reads it.
// Emitting decision_resolved via the bus's EmitTyped (F-class fsync) writes a
// durable record we then re-read to assert.
//
// Helpers use the prefix "dk4" per the helper-prefix discipline.
//
// Bead ref: hk-kba (K4).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// dk4Handler builds a file-backed handler over a fresh events.jsonl in a temp
// dir. It returns the handler, the events.jsonl path, and a flush func that
// closes+reopens the writer so emitted lines are durably visible to a re-read.
// The writer stays open across the test; callers read the file directly via
// decisionsProjection (which opens its own read handle).
type dk4Setup struct {
	h          *commsSendHandlerImpl
	eventsPath string
	bus        eventbus.EventBus
}

func dk4NewHandler(t *testing.T) *dk4Setup {
	t.Helper()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")

	writer, err := eventbus.OpenJSONLWriter(eventsPath)
	if err != nil {
		t.Fatalf("dk4NewHandler: OpenJSONLWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)

	ch := NewCommsSendHandler(bus)
	if ch == nil {
		t.Fatal("dk4NewHandler: NewCommsSendHandler returned nil")
	}
	impl, ok := ch.(*commsSendHandlerImpl)
	if !ok {
		t.Fatal("dk4NewHandler: handler is not *commsSendHandlerImpl")
	}
	// Wire the events path so decisions-list / decisions-answer can re-project.
	impl.SetRecvDeps(NewCursorStore(filepath.Join(dir, "cursors")), eventsPath)

	return &dk4Setup{h: impl, eventsPath: eventsPath, bus: bus}
}

// dk4Raise emits a decision_needed via the real handler and returns its
// decision_id (= the event's own event_id). The F-class fsync makes it durable
// before return, so a subsequent decisionsProjection sees it.
func dk4Raise(t *testing.T, s *dk4Setup, question string, options []string, blockedAgent, contextLink string) string {
	t.Helper()
	req := DecisionsRaiseRequest{
		Question:     question,
		Options:      options,
		BlockedAgent: blockedAgent,
		ContextLink:  contextLink,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("dk4Raise: marshal: %v", err)
	}
	resultBytes, err := s.h.HandleDecisionsRaise(context.Background(), payload)
	if err != nil {
		t.Fatalf("dk4Raise(%q): HandleDecisionsRaise: %v", question, err)
	}
	var res DecisionsRaiseResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("dk4Raise: decode result: %v", err)
	}
	if res.DecisionID == "" {
		t.Fatal("dk4Raise: empty decision_id")
	}
	return res.DecisionID
}

// dk4List calls HandleDecisionsList with an optional single-id filter and decodes
// the result.
func dk4List(t *testing.T, s *dk4Setup, filterID string) DecisionsListResult {
	t.Helper()
	payload := json.RawMessage(`{}`)
	if filterID != "" {
		payload = json.RawMessage(`{"decision_id":"` + filterID + `"}`)
	}
	resultBytes, err := s.h.HandleDecisionsList(context.Background(), payload)
	if err != nil {
		t.Fatalf("dk4List: HandleDecisionsList: %v", err)
	}
	var res DecisionsListResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("dk4List: decode result: %v", err)
	}
	return res
}

// TestDecisionsList_CrossAgent: list returns open decisions from ≥2 distinct
// blocked agents in ONE result (S2), each with all five rendered fields.
func TestDecisionsList_CrossAgent(t *testing.T) {
	s := dk4NewHandler(t)

	d1 := dk4Raise(t, s, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa")
	d2 := dk4Raise(t, s, "Pick region", []string{"us", "eu"}, "bob", "hk-bbb")

	res := dk4List(t, s, "")
	if len(res.Decisions) != 2 {
		t.Fatalf("list: got %d open decisions, want 2", len(res.Decisions))
	}

	byID := make(map[string]DecisionsListItem, len(res.Decisions))
	agents := make(map[string]struct{})
	for _, it := range res.Decisions {
		byID[it.DecisionID] = it
		agents[it.BlockedAgent] = struct{}{}
	}
	if len(agents) < 2 {
		t.Fatalf("list: want ≥2 distinct blocked agents, got %v", agents)
	}

	a := byID[d1]
	if a.Question != "Ship to prod?" || len(a.Options) != 2 || a.BlockedAgent != "alice" || a.ContextLink != "hk-aaa" {
		t.Errorf("d1 item wrong: %+v", a)
	}
	b := byID[d2]
	if b.Question != "Pick region" || b.BlockedAgent != "bob" || b.ContextLink != "hk-bbb" {
		t.Errorf("d2 item wrong: %+v", b)
	}
}

// TestDecisionsList_Filter: a decision_id filter narrows the result to one (the
// `show <id>` server-side path).
func TestDecisionsList_Filter(t *testing.T) {
	s := dk4NewHandler(t)
	d1 := dk4Raise(t, s, "Q1", []string{"a", "b"}, "alice", "")
	_ = dk4Raise(t, s, "Q2", []string{"c", "d"}, "bob", "")

	res := dk4List(t, s, d1)
	if len(res.Decisions) != 1 {
		t.Fatalf("filtered list: got %d, want 1", len(res.Decisions))
	}
	if res.Decisions[0].DecisionID != d1 {
		t.Errorf("filtered list returned %q, want %q", res.Decisions[0].DecisionID, d1)
	}
}

// dk4CountResolved scans the durable log and returns the number of
// decision_resolved events for decisionID (asserts the JSONL record).
func dk4CountResolved(t *testing.T, eventsPath, decisionID string) int {
	t.Helper()
	var zeroID core.EventID
	n := 0
	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		if ev.Type != string(core.EventTypeDecisionResolved) {
			continue
		}
		var p core.DecisionResolvedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			continue
		}
		if p.DecisionID == decisionID {
			n++
		}
	}
	return n
}

// TestDecisionsAnswer_ValidEmitsResolved: a valid option emits decision_resolved
// (assert the JSONL record carries chosen_option/resolver) and the decision then
// leaves the open set.
func TestDecisionsAnswer_ValidEmitsResolved(t *testing.T) {
	s := dk4NewHandler(t)
	did := dk4Raise(t, s, "Ship v2?", []string{"ship", "hold"}, "alice", "hk-aaa")

	payload := json.RawMessage(`{"decision_id":"` + did + `","chosen_option":"ship","resolver":"operator"}`)
	resultBytes, err := s.h.HandleDecisionsAnswer(context.Background(), payload)
	if err != nil {
		t.Fatalf("answer valid option: unexpected error: %v", err)
	}
	var res DecisionsAnswerResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("answer: decode result: %v", err)
	}
	if res.NoOp {
		t.Fatal("answer on an open decision returned NoOp=true; want a real emit")
	}
	if res.EventID == "" {
		t.Fatal("answer: empty event_id on a successful resolve")
	}

	// Assert the durable decision_resolved record.
	if got := dk4CountResolved(t, s.eventsPath, did); got != 1 {
		t.Fatalf("want exactly 1 decision_resolved for %s, got %d", did, got)
	}
	// The decision is now closed → leaves the open set.
	if open := decisionsProjection(s.eventsPath); len(open) != 0 {
		t.Errorf("after answer, open set has %d entries, want 0", len(open))
	}
}

// TestDecisionsAnswer_BadOptionRejected: an option NOT in the open decision's
// options is rejected (N7) — an error, NO event emitted.
func TestDecisionsAnswer_BadOptionRejected(t *testing.T) {
	s := dk4NewHandler(t)
	did := dk4Raise(t, s, "Ship v2?", []string{"ship", "hold"}, "alice", "")

	payload := json.RawMessage(`{"decision_id":"` + did + `","chosen_option":"explode","resolver":"operator"}`)
	_, err := s.h.HandleDecisionsAnswer(context.Background(), payload)
	if err == nil {
		t.Fatal("answer with a bad option: want an error (N7), got nil")
	}
	// No decision_resolved emitted; the decision stays open.
	if got := dk4CountResolved(t, s.eventsPath, did); got != 0 {
		t.Errorf("bad option must emit no decision_resolved, got %d", got)
	}
	if open := decisionsProjection(s.eventsPath); len(open) != 1 {
		t.Errorf("after rejected bad option, open set = %d, want 1 (still open)", len(open))
	}
}

// TestDecisionsAnswer_UnknownIDNoOp: answering an UNKNOWN decision_id is a no-op
// — NO error, NoOp=true, NO event written (N3, S8). Distinct from the bad-option
// error above.
func TestDecisionsAnswer_UnknownIDNoOp(t *testing.T) {
	s := dk4NewHandler(t)
	// No decision raised; this id is unknown.
	const unknown = "01965b00-0000-7000-8000-0000deadbeef"
	payload := json.RawMessage(`{"decision_id":"` + unknown + `","chosen_option":"anything"}`)
	resultBytes, err := s.h.HandleDecisionsAnswer(context.Background(), payload)
	if err != nil {
		t.Fatalf("answer unknown id: want NO error (N3), got %v", err)
	}
	var res DecisionsAnswerResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("answer unknown id: decode result: %v", err)
	}
	if !res.NoOp {
		t.Error("answer unknown id: want NoOp=true")
	}
	if res.EventID != "" {
		t.Errorf("answer unknown id: want empty event_id, got %q", res.EventID)
	}
	if got := dk4CountResolved(t, s.eventsPath, unknown); got != 0 {
		t.Errorf("no-op must write no decision_resolved, got %d", got)
	}
}

// TestDecisionsAnswer_AlreadyTerminalNoOp: a SECOND answer for the same
// decision_id (already resolved) is a no-op — first-writer-wins (N3): no second
// emit, no error. Guards S8.
func TestDecisionsAnswer_AlreadyTerminalNoOp(t *testing.T) {
	s := dk4NewHandler(t)
	did := dk4Raise(t, s, "Ship v2?", []string{"ship", "hold"}, "alice", "")

	// First answer → resolves.
	first := json.RawMessage(`{"decision_id":"` + did + `","chosen_option":"ship","resolver":"operator"}`)
	if _, err := s.h.HandleDecisionsAnswer(context.Background(), first); err != nil {
		t.Fatalf("first answer: %v", err)
	}

	// Second answer for the SAME id → already terminal → no-op (N3), even with a
	// DIFFERENT option (first-writer-wins: the second never applies).
	second := json.RawMessage(`{"decision_id":"` + did + `","chosen_option":"hold","resolver":"someone-else"}`)
	resultBytes, err := s.h.HandleDecisionsAnswer(context.Background(), second)
	if err != nil {
		t.Fatalf("second answer (already terminal): want NO error (N3), got %v", err)
	}
	var res DecisionsAnswerResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("second answer: decode result: %v", err)
	}
	if !res.NoOp {
		t.Error("second answer on a resolved decision: want NoOp=true (first-writer-wins)")
	}
	// Exactly ONE decision_resolved total for did (the first writer).
	if got := dk4CountResolved(t, s.eventsPath, did); got != 1 {
		t.Errorf("first-writer-wins: want exactly 1 decision_resolved for %s, got %d", did, got)
	}
}

// TestDecisionsList_NoEmit: a list call writes NO event to the durable log
// (N9 read-pure / S6 — the orphaned-pending flag is computed client-side, the
// daemon op stays a pure read). Asserts the event count is unchanged by a list.
func TestDecisionsList_NoEmit(t *testing.T) {
	s := dk4NewHandler(t)
	_ = dk4Raise(t, s, "Q1", []string{"a", "b"}, "alice", "")
	_ = dk4Raise(t, s, "Q2", []string{"c", "d"}, "bob", "")

	before := dk4TotalEvents(t, s.eventsPath)
	// Several list calls (and a filtered show) — none may emit.
	_ = dk4List(t, s, "")
	_ = dk4List(t, s, "")
	res := dk4List(t, s, "")
	if len(res.Decisions) > 0 {
		_ = dk4List(t, s, res.Decisions[0].DecisionID) // a "show"
	}
	after := dk4TotalEvents(t, s.eventsPath)
	if after != before {
		t.Errorf("decisions-list emitted events: before=%d after=%d (must be read-pure, N9/S6)", before, after)
	}
}

// dk4TotalEvents counts all events in the durable log.
func dk4TotalEvents(t *testing.T, eventsPath string) int {
	t.Helper()
	var zeroID core.EventID
	n := 0
	for range eventbus.ScanAfter(eventsPath, zeroID) {
		n++
	}
	return n
}
