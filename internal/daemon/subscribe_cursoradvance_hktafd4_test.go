package daemon

// subscribe_cursoradvance_hktafd4_test.go — regression guards for hk-tafd4.
//
// FIX (a): a `comms recv --follow` subscribe session MUST advance the agent's
// durable comms cursor as agent_message events are delivered, so a watcher
// restart does NOT replay already-delivered messages. Before this fix the
// live-tail streamed events without touching the cursor, causing duplicate
// delivery on every --follow restart.
//
// These tests exercise the server-side SubscribeHub directly (the CLI is a thin
// transport over this handler). They use the same socket + JSONL harness as
// subscribe_test.go.

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

// tafd4MakeAgentMessage builds a strictly-ordered agent_message core.Event
// addressed to `to` from `from`.
func tafd4MakeAgentMessage(t *testing.T, to, from, body string) core.Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	time.Sleep(2 * time.Millisecond) // ensure strictly ordered UUIDv7
	payload, mErr := json.Marshal(AgentMessagePayload{To: to, From: from, Body: body})
	if mErr != nil {
		t.Fatalf("marshal agent_message payload: %v", mErr)
	}
	return core.Event{
		EventID:         core.EventID(id),
		SchemaVersion:   1,
		Type:            "agent_message",
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(payload),
	}
}

// waitForCursor polls the cursor store until Get(agent) == want or the deadline
// elapses. Returns the final value read.
func waitForCursor(t *testing.T, cs *CursorStore, agent, want string, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	var last string
	for time.Now().Before(deadline) {
		got, err := cs.Get(agent)
		if err != nil {
			t.Fatalf("cursor Get: %v", err)
		}
		last = got
		if got == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return last
}

// TestSubscribe_FollowAdvancesCursor_Replay is the core regression guard for the
// replay bug: after a --follow subscribe delivers agent_message events, the
// agent's durable cursor must have advanced past them so a subsequent recv does
// NOT re-deliver. We seed two agent_message events in JSONL, subscribe with a
// since_event_id before both (the --follow replay path), read both, then close
// the connection. On connection close the daemon flushes the cursor; we assert
// it advanced to the last delivered event_id and that a one-shot recv via the
// SAME store then returns nothing.
func TestSubscribe_FollowAdvancesCursor_Replay(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk-tafd4-replay-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	jsonlPath := filepath.Join(dir, "events.jsonl")

	// e0 is the cursor anchor (before both messages); e1, e2 are directed to alice.
	e0 := tafd4MakeAgentMessage(t, "alice", "bob", "anchor")
	e1 := tafd4MakeAgentMessage(t, "alice", "bob", "hello-1")
	e2 := tafd4MakeAgentMessage(t, "alice", "bob", "hello-2")

	f, openErr := os.Create(jsonlPath)
	if openErr != nil {
		t.Fatalf("create jsonl: %v", openErr)
	}
	enc := json.NewEncoder(f)
	if encErr := enc.Encode(e1); encErr != nil {
		t.Fatalf("encode e1: %v", encErr)
	}
	if encErr := enc.Encode(e2); encErr != nil {
		t.Fatalf("encode e2: %v", encErr)
	}
	_ = f.Close()

	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: jsonlPath,
	})
	hub.SetCommsCursorStore(cs)
	sockPath := subscribeTestStartSocketHub(t, hub)

	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "alice",
		"since_event_id":    uuid.UUID(e0.EventID).String(),
		"heartbeat_seconds": 600,
	})

	readEvent := func(label string) core.Event {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, readErr := rdr.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			t.Fatalf("%s: read: %v", label, readErr)
		}
		var ev core.Event
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
			t.Fatalf("%s: decode: %v (line=%q)", label, jsonErr, string(line))
		}
		return ev
	}

	got1 := readEvent("replayed e1")
	if got1.EventID != e1.EventID {
		t.Errorf("replayed[0] event_id mismatch")
	}
	got2 := readEvent("replayed e2")
	if got2.EventID != e2.EventID {
		t.Errorf("replayed[1] event_id mismatch")
	}

	// Close the connection — the daemon's HandleSubscribe defer must flush the
	// cursor to the last delivered agent_message (e2).
	_ = conn.Close()

	wantCursor := uuid.UUID(e2.EventID).String()
	got := waitForCursor(t, cs, "alice", wantCursor, 3*time.Second)
	if got != wantCursor {
		t.Fatalf("cursor did not advance after --follow delivery: got %q, want %q (replay bug regressed)", got, wantCursor)
	}

	// A one-shot recv via the SAME store must now re-deliver NOTHING: the
	// follow stream already consumed e1 and e2.
	impl := newTestCommsHandler(cs, jsonlPath)
	recvReq, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	resBytes, recvErr := impl.HandleCommsRecv(context.Background(), recvReq)
	if recvErr != nil {
		t.Fatalf("HandleCommsRecv: %v", recvErr)
	}
	var res CommsRecvResult
	if uErr := json.Unmarshal(resBytes, &res); uErr != nil {
		t.Fatalf("decode recv result: %v", uErr)
	}
	if len(res.Messages) != 0 {
		t.Fatalf("one-shot recv after --follow re-delivered %d message(s); want 0 (cursor not durably advanced)", len(res.Messages))
	}
}

// TestSubscribe_FollowAdvancesCursor_LiveEvents verifies the live-tail path (not
// just replay) advances the cursor: dispatch agent_message events through the
// hub after the subscriber registers, then close and assert the cursor advanced.
func TestSubscribe_FollowAdvancesCursor_LiveEvents(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk-tafd4-live-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	cs := NewCursorStore(filepath.Join(dir, "cursors"))
	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})
	hub.SetCommsCursorStore(cs)
	sockPath := subscribeTestStartSocketHub(t, hub)

	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "carol",
		"heartbeat_seconds": 600,
	})

	// Give HandleSubscribe time to register on the hub.
	time.Sleep(50 * time.Millisecond)

	live1 := tafd4MakeAgentMessage(t, "carol", "dave", "live-1")
	live2 := tafd4MakeAgentMessage(t, "carol", "dave", "live-2")
	hub.dispatch(context.Background(), live1) //nolint:errcheck
	hub.dispatch(context.Background(), live2) //nolint:errcheck

	readEvent := func(label string) core.Event {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, readErr := rdr.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			t.Fatalf("%s: read: %v", label, readErr)
		}
		var ev core.Event
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
			t.Fatalf("%s: decode: %v (line=%q)", label, jsonErr, string(line))
		}
		return ev
	}
	if got := readEvent("live e1"); got.EventID != live1.EventID {
		t.Errorf("live[0] event_id mismatch")
	}
	if got := readEvent("live e2"); got.EventID != live2.EventID {
		t.Errorf("live[1] event_id mismatch")
	}

	_ = conn.Close()

	wantCursor := uuid.UUID(live2.EventID).String()
	got := waitForCursor(t, cs, "carol", wantCursor, 5*time.Second)
	if got != wantCursor {
		t.Fatalf("cursor did not advance after live --follow delivery: got %q, want %q", got, wantCursor)
	}
}

// TestSubscribe_FollowCursor_NilStoreNoop verifies the cursor-advance code is a
// strict no-op when no cursor store is wired (subscribe behaves exactly as
// before for non-comms subscribers).
func TestSubscribe_FollowCursor_NilStoreNoop(t *testing.T) {
	t.Parallel()

	hub := NewSubscribeHub(SubscribeHubConfig{Bus: nil})
	// Intentionally do NOT call SetCommsCursorStore.
	sockPath := subscribeTestStartSocketHub(t, hub)

	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "erin",
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	time.Sleep(50 * time.Millisecond)
	live := tafd4MakeAgentMessage(t, "erin", "frank", "no-store")
	hub.dispatch(context.Background(), live) //nolint:errcheck

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, readErr := rdr.ReadBytes('\n')
	if len(line) == 0 && readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	var ev core.Event
	if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
		t.Fatalf("decode: %v (line=%q)", jsonErr, string(line))
	}
	if ev.EventID != live.EventID {
		t.Errorf("nil-store subscribe: event_id mismatch")
	}
	// No panic, no cursor file: the absence of a store must not break delivery.
}
