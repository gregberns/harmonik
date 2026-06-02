package daemon

// commsrecvhandler_nnwaa_test.go — unit tests for comms-recv handler (T8, hk-nnwaa).
//
// Tests verify:
//   - Returns empty messages when no events match (no cursor, empty log).
//   - Returns matched messages from beginning when no cursor set (sinceID == zero).
//   - Filters by agent (to==agent OR broadcast "*").
//   - Advances cursor after delivery (N3).
//   - Subsequent call with advanced cursor returns only new messages.
//   - From and topic filters are applied via MatchAgentMessage (R1).
//   - Returns error when agent is missing from request.
//   - Returns error when CursorStore not configured.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

// writeTestEvent appends a core.Event to a JSONL file and returns the EventID string.
// Uses UUIDv7 so that byte-ordering matches chronological order (EV-002), which is
// required for ScanAfter to work correctly in tests.
func writeTestEvent(t *testing.T, path string, evType string, payload any) string {
	t.Helper()
	raw, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("writeTestEvent: uuid.NewV7: %v", err)
	}
	id := core.EventID(raw)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("writeTestEvent: marshal payload: %v", err)
	}
	ev := core.Event{
		EventID:       id,
		Type:          evType,
		Payload:       payloadBytes,
		TimestampWall: time.Now().UTC(),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("writeTestEvent: marshal event: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("writeTestEvent: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	line = append(line, '\n')
	if _, err := f.Write(line); err != nil {
		t.Fatalf("writeTestEvent: write: %v", err)
	}
	return id.String()
}

// newTestCommsHandler builds a *commsSendHandlerImpl with a nil emitter (send/presence
// not needed) but with recv deps wired so comms-recv works.
func newTestCommsHandler(cursorStore *CursorStore, eventsPath string) *commsSendHandlerImpl {
	h := &commsSendHandlerImpl{}
	h.SetRecvDeps(cursorStore, eventsPath)
	return h
}

// TestCommsRecv_EmptyLog verifies that comms-recv returns an empty messages list
// when the events JSONL file does not exist (no events ever written).
func TestCommsRecv_EmptyLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl") // does not exist
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: unexpected error: %v", err)
	}
	var got CommsRecvResult
	if unmarshalErr := json.Unmarshal(result, &got); unmarshalErr != nil {
		t.Fatalf("unmarshal result: %v", unmarshalErr)
	}
	if len(got.Messages) != 0 {
		t.Fatalf("want 0 messages, got %d", len(got.Messages))
	}
}

// TestCommsRecv_DirectedMessage verifies that comms-recv returns messages directed
// to the agent (to==agent) and not messages directed to others.
func TestCommsRecv_DirectedMessage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	// Write a message directed to "alice" (should be delivered).
	id1 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "hello alice",
	})
	// Write a message directed to "carol" (should NOT be delivered to alice).
	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "carol", Body: "hello carol",
	})

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(got.Messages))
	}
	if got.Messages[0].EventID != id1 {
		t.Errorf("event_id: want %q, got %q", id1, got.Messages[0].EventID)
	}
	if got.Messages[0].Body != "hello alice" {
		t.Errorf("body: want %q, got %q", "hello alice", got.Messages[0].Body)
	}
}

// TestCommsRecv_BroadcastDelivered verifies that broadcast messages (to=="*") are
// delivered to all agents.
func TestCommsRecv_BroadcastDelivered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	id1 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "orchestrator", To: "*", Body: "broadcast msg",
	})

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("want 1 broadcast message, got %d", len(got.Messages))
	}
	if got.Messages[0].EventID != id1 {
		t.Errorf("event_id mismatch")
	}
}

// TestCommsRecv_CursorAdvancedAfterRead verifies N3: cursor is advanced after the
// scan, and a second call returns only new messages.
func TestCommsRecv_CursorAdvancedAfterRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "first",
	})

	// First recv: should return 1 message and advance cursor.
	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result1, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("first HandleCommsRecv: %v", err)
	}
	var got1 CommsRecvResult
	if err := json.Unmarshal(result1, &got1); err != nil {
		t.Fatalf("unmarshal result1: %v", err)
	}
	if len(got1.Messages) != 1 {
		t.Fatalf("first call: want 1 message, got %d", len(got1.Messages))
	}

	// Write a second message.
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "carol", To: "alice", Body: "second",
	})

	// Second recv: should return only the second message (cursor advanced past first).
	result2, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("second HandleCommsRecv: %v", err)
	}
	var got2 CommsRecvResult
	if err := json.Unmarshal(result2, &got2); err != nil {
		t.Fatalf("unmarshal result2: %v", err)
	}
	if len(got2.Messages) != 1 {
		t.Fatalf("second call: want 1 message, got %d", len(got2.Messages))
	}
	if got2.Messages[0].EventID != id2 {
		t.Errorf("second call: event_id want %q, got %q", id2, got2.Messages[0].EventID)
	}
	if got2.Messages[0].Body != "second" {
		t.Errorf("second call: body want %q, got %q", "second", got2.Messages[0].Body)
	}
}

// TestCommsRecv_FromFilter verifies that the --from filter is applied via
// MatchAgentMessage (R1/N1): messages from other senders are excluded.
func TestCommsRecv_FromFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "from bob",
	})
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "carol", To: "alice", Body: "from carol",
	})

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice", From: "carol"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(got.Messages))
	}
	if got.Messages[0].EventID != id2 {
		t.Errorf("event_id: want %q, got %q", id2, got.Messages[0].EventID)
	}
}

// TestCommsRecv_TopicFilter verifies that the --topic filter is applied via
// MatchAgentMessage (R1/N1).
func TestCommsRecv_TopicFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "no topic", Topic: "",
	})
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "status msg", Topic: "status",
	})

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice", Topic: "status"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(got.Messages))
	}
	if got.Messages[0].EventID != id2 {
		t.Errorf("event_id mismatch")
	}
}

// TestCommsRecv_NonAgentMessageEventsSkipped verifies that non-agent_message events
// in the JSONL log are silently skipped.
func TestCommsRecv_NonAgentMessageEventsSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	// Write a non-agent_message event (run_completed, agent_presence, etc.).
	writeTestEvent(t, eventsPath, "run_completed", map[string]string{"status": "ok"})
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "hello",
	})

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(got.Messages))
	}
	if got.Messages[0].EventID != id2 {
		t.Errorf("event_id mismatch")
	}
}

// TestCommsRecv_MissingAgent verifies that an empty agent field returns an error.
func TestCommsRecv_MissingAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, filepath.Join(dir, "events.jsonl"))

	payload, _ := json.Marshal(CommsRecvRequest{Agent: ""})
	_, err := h.HandleCommsRecv(context.Background(), payload)
	if err == nil {
		t.Fatal("want error for missing agent, got nil")
	}
}

// TestCommsRecv_NoCursorStore verifies that a nil CursorStore returns an error.
func TestCommsRecv_NoCursorStore(t *testing.T) {
	t.Parallel()
	h := &commsSendHandlerImpl{} // no SetRecvDeps

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	_, err := h.HandleCommsRecv(context.Background(), payload)
	if err == nil {
		t.Fatal("want error when CursorStore not configured, got nil")
	}
}

// TestCommsRecv_CursorSurvivesDaemonRestart simulates a daemon restart by creating
// a new handler instance pointing at the same cursor directory and events JSONL.
// The new instance should resume from where the previous one left off.
func TestCommsRecv_CursorSurvivesDaemonRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cursorDir := filepath.Join(dir, "cursors")

	// Write two messages.
	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "before restart",
	})
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "bob", To: "alice", Body: "after restart",
	})

	// Recv with first handler instance (simulates daemon-1).
	h1 := newTestCommsHandler(NewCursorStore(cursorDir), eventsPath)
	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result1, err := h1.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("handler1 HandleCommsRecv: %v", err)
	}
	var got1 CommsRecvResult
	if err := json.Unmarshal(result1, &got1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got1.Messages) != 2 {
		t.Fatalf("handler1: want 2 messages, got %d", len(got1.Messages))
	}

	// Simulate daemon restart: create a new handler that reads from the same cursor dir.
	h2 := newTestCommsHandler(NewCursorStore(cursorDir), eventsPath)
	result2, err := h2.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("handler2 HandleCommsRecv: %v", err)
	}
	var got2 CommsRecvResult
	if err := json.Unmarshal(result2, &got2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// After restart, cursor is advanced past both messages; no new messages since id2.
	// But id2 was the last cursor position after handler1's recv call,
	// so handler2 should return 0 messages.
	if len(got2.Messages) != 0 {
		t.Fatalf("handler2 (post-restart): want 0 messages, got %d; last event should be %s", len(got2.Messages), id2)
	}
}

// TestCommsRecv_SharedMatchPredicateParity verifies that comms-recv and
// MatchAgentMessage agree: for a given (agent, from, topic) filter combination,
// the same set of events is accepted by both. This catches divergence between
// the recv path and the shared predicate (R1 / N1).
func TestCommsRecv_SharedMatchPredicateParity(t *testing.T) {
	t.Parallel()
	type msgDef struct {
		from  string
		to    string
		topic string
	}
	messages := []msgDef{
		{from: "bob", to: "alice", topic: ""},
		{from: "carol", to: "alice", topic: "status"},
		{from: "bob", to: "*", topic: ""},
		{from: "carol", to: "dave", topic: "status"},
	}

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))

	// Write all messages to JSONL.
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
			From: m.from, To: m.to, Topic: m.topic, Body: "body",
		})
	}

	// For a specific filter (to="alice", from="", topic=""), check recv agrees with
	// MatchAgentMessage applied manually.
	h := newTestCommsHandler(cs, eventsPath)
	payload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Compute expected by manually applying MatchAgentMessage with to="alice".
	var wantIDs []string
	for i, m := range messages {
		p := AgentMessagePayload{From: m.from, To: m.to, Topic: m.topic, Body: "body"}
		if MatchAgentMessage(p, "alice", "", "") {
			wantIDs = append(wantIDs, ids[i])
		}
	}

	if len(got.Messages) != len(wantIDs) {
		t.Fatalf("parity: want %d messages, got %d", len(wantIDs), len(got.Messages))
	}
	for i, msg := range got.Messages {
		if msg.EventID != wantIDs[i] {
			t.Errorf("parity[%d]: want event_id %q, got %q", i, wantIDs[i], msg.EventID)
		}
	}
}

// TestCommsRecv_MessagesSliceNotNull verifies that the result always contains
// a non-null messages array, even when empty (JSON [] not null).
func TestCommsRecv_MessagesSliceNotNull(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	h := newTestCommsHandler(cs, eventsPath)

	payload, _ := json.Marshal(CommsRecvRequest{Agent: "nobody"})
	result, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("HandleCommsRecv: %v", err)
	}

	// Raw JSON check: must be `"messages":[]` not `"messages":null`.
	var got CommsRecvResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Messages == nil {
		t.Error("messages field is nil (want empty non-nil slice)")
	}
	if len(got.Messages) != 0 {
		t.Errorf("want empty messages, got %d", len(got.Messages))
	}
}
