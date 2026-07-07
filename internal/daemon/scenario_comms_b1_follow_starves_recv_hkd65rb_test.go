package daemon

// scenario_comms_b1_follow_starves_recv_hkd65rb_test.go — T2c L1:
// B1 follow-starves-recv pin + inline doc (bead hk-d65rb, codename:comms-test-harness).
//
// # Scenario 1: follow-starves-recv is CORRECT, not a bug
//
// When `recv --follow` is armed for agent A (SubscribeHub with shared CursorStore),
// messages directed to A are delivered to the follower's stream AND the shared
// durable cursor advances past each delivered event (hk-tafd4). A subsequent
// one-shot `comms recv --agent A` then drains 0 messages — the follower already
// consumed them and the cursor reflects that.
//
// # Why 0-drain is intentional (B1 pin)
//
// The shared cursor is the single source of truth for "what agent A has seen."
// The --follow subscriber and the one-shot recv share the SAME CursorStore:
// SetCommsCursorStore wires both to a single *CursorStore instance. When --follow
// advances the cursor past msg 3, recv starts scanning from after msg 3 and finds
// nothing new. This is at-least-once N3 semantics, not a missing-message gap:
//
//   - Exactly-once delivery is NOT guaranteed (N3). The follow path delivers, the
//     cursor advances, recv correctly skips already-delivered events.
//   - If --follow crashes between delivery and cursor-flush, recv will re-deliver
//     the same events. Recipients MUST deduplicate on event_id (N3 contract).
//   - The events are permanently stored in events.jsonl. `comms log --since <id>`
//     (cursor-independent scan of events.jsonl) always shows the full history.
//
// # Assertions (two)
//
//   1. 0-DRAIN: after the follower session delivers 3 messages and flushes the
//      shared cursor, HandleCommsRecv for the same agent returns 0 messages.
//   2. LOG-INTACT: eventbus.ScanAfter(eventsPath, anchorID) — the functional
//      equivalent of `comms log --since <anchorID>` — returns all 3 messages;
//      cursor advance does NOT delete events from events.jsonl (log is append-only).
//
// # Harness (L1 in-process)
//
// Shared CursorStore + SubscribeHub (SetCommsCursorStore) + net.Pipe fake client
// (hk7n6o7Never timer — suppresses heartbeats and cursor-flush ticks) +
// commsSendHandlerImpl. No socket, no real time, no daemon process.
//
// Bead: hk-d65rb. Design: comms-test-harness §3 scenario 1.
// Spec ref: agent-comms spec §N3.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hkd65rbReadAgentMessage reads one line from rdr and returns the decoded event_id.
// Skips heartbeat and subscription_gap lines (should not appear with hk7n6o7Never,
// but guard defensively). Fails the test if no agent_message arrives within 5s.
func hkd65rbReadAgentMessage(t *testing.T, conn net.Conn, rdr *bufio.Reader, label string) string {
	t.Helper()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, readErr := rdr.ReadBytes('\n')
		if len(line) == 0 {
			t.Fatalf("hkd65rbReadAgentMessage %s: read: %v", label, readErr)
		}
		var probe struct {
			Type    string `json:"type"`
			EventID string `json:"event_id"`
		}
		if jsonErr := json.Unmarshal(line, &probe); jsonErr != nil {
			t.Fatalf("hkd65rbReadAgentMessage %s: decode: %v (line=%q)", label, jsonErr, string(line))
		}
		if probe.Type == "heartbeat" || probe.Type == "subscription_gap" {
			continue
		}
		if probe.EventID == "" {
			t.Fatalf("hkd65rbReadAgentMessage %s: missing event_id (line=%q)", label, string(line))
		}
		return probe.EventID
	}
}

// TestScenario_HkD65rb_B1_FollowStarvesRecv asserts the two halves of scenario 1:
//
//  1. 0-drain: after --follow delivers 3 messages and advances the shared cursor,
//     HandleCommsRecv for the same agent returns 0 messages.
//  2. log-intact: eventbus.ScanAfter (comms-log-equivalent) still returns all 3
//     messages from events.jsonl regardless of the cursor position.
func TestScenario_HkD65rb_B1_FollowStarvesRecv(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hkd65rb-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")
	cursorDir := filepath.Join(dir, "cursors")

	// ── Fixture: anchor (noise) + 3 agent_message events directed to alice ───
	//
	// The anchor is a non-agent_message event establishing the replay boundary
	// (since_event_id). The follower will replay the 3 alice messages from the
	// log and advance the shared cursor past each one.
	anchorID := writeTestEvent(t, eventsPath, "run_started", map[string]string{"note": "anchor"})
	time.Sleep(2 * time.Millisecond)
	id1 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "captain", To: "alice", Body: "msg-1"})
	time.Sleep(2 * time.Millisecond)
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "captain", To: "alice", Body: "msg-2"})
	time.Sleep(2 * time.Millisecond)
	id3 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "captain", To: "alice", Body: "msg-3"})

	// ── Wire shared CursorStore; arm SubscribeHub with it ───────────────────
	//
	// SetCommsCursorStore binds the hub to the same *CursorStore used by the
	// one-shot recv handler. Both paths share one durable cursor per agent.
	cs := NewCursorStore(cursorDir)
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
		NewTimer:        hk7n6o7Never, // suppress heartbeats + cursor-flush ticks
	})
	hub.SetCommsCursorStore(cs)

	// ── Arm follower — HandleSubscribe via net.Pipe ──────────────────────────
	//
	// since_event_id=anchorID mirrors what runCommsRecvFollowIO does after a
	// successful one-shot drain: replay from the drain's cursor_after position
	// to cover messages already in the log before the follow session started.
	srv, cli := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	followDone := make(chan struct{})
	go func() {
		hub.HandleSubscribe(ctx, srv, SubscribeRequest{
			Types:            []string{"agent_message"},
			To:               "alice",
			SinceEventID:     anchorID,
			HeartbeatSeconds: 600,
		})
		close(followDone)
	}()

	// ── Read 3 replayed agent_message events ─────────────────────────────────
	//
	// HandleSubscribe replays events strictly after anchorID. After the replay
	// loop completes it calls flushCursor() which durably advances alice's
	// cursor to id3 before entering the live loop.
	rdr := bufio.NewReader(cli)
	got1 := hkd65rbReadAgentMessage(t, cli, rdr, "replay msg-1")
	got2 := hkd65rbReadAgentMessage(t, cli, rdr, "replay msg-2")
	got3 := hkd65rbReadAgentMessage(t, cli, rdr, "replay msg-3")

	if got1 != id1 || got2 != id2 || got3 != id3 {
		t.Errorf("follower replayed wrong events: got [%s %s %s], want [%s %s %s]",
			got1, got2, got3, id1, id2, id3)
	}

	// ── Close follower; wait for HandleSubscribe to return ───────────────────
	//
	// Cancelling ctx + closing both pipe ends causes the inner read-goroutine to
	// detect EOF and cancel streamCtx; the live select hits Done and returns.
	// defer flushCursor() in HandleSubscribe ensures any pending cursor write
	// completes (idempotent here: replay already flushed to id3).
	cancel()
	_ = cli.Close()
	_ = srv.Close()
	select {
	case <-followDone:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleSubscribe did not return within 3s of connection close")
	}

	// Confirm cursor advanced to id3 (post-replay flush or defer flush).
	got := waitForCursor(t, cs, "alice", id3, 3*time.Second)
	if got != id3 {
		t.Fatalf("cursor not at id3 after follow session: got %q, want %q", got, id3)
	}

	// ── Assertion 1: 0-drain ─────────────────────────────────────────────────
	//
	// HandleCommsRecv reads alice's cursor (now = id3) and scans events.jsonl
	// from id3 forward — finding no new events. This is the B1 semantics:
	// 0-drain is CORRECT because the follower already delivered id1..id3.
	impl := newTestCommsHandler(cs, eventsPath)
	recvPayload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	resBytes, recvErr := impl.HandleCommsRecv(context.Background(), recvPayload)
	if recvErr != nil {
		t.Fatalf("HandleCommsRecv: %v", recvErr)
	}
	var res CommsRecvResult
	if uErr := json.Unmarshal(resBytes, &res); uErr != nil {
		t.Fatalf("decode CommsRecvResult: %v", uErr)
	}
	if len(res.Messages) != 0 {
		bodies := make([]string, len(res.Messages))
		for i, m := range res.Messages {
			bodies[i] = m.Body
		}
		t.Errorf("B1 violation: HandleCommsRecv returned %d message(s) after follower consumed shared cursor; want 0 (follow-starves-recv is intentional at-least-once semantics). messages: %v",
			len(res.Messages), bodies)
	}

	// ── Assertion 2: log-intact (comms log --since equivalent) ───────────────
	//
	// eventbus.ScanAfter(eventsPath, sinceID) replicates `comms log --since
	// anchorID` — a cursor-independent scan of events.jsonl. The log is
	// append-only; cursor advances do NOT delete or modify events. All 3 alice
	// messages must be visible here regardless of the current cursor position.
	anchorUUID, parseErr := uuid.Parse(anchorID)
	if parseErr != nil {
		t.Fatalf("parse anchorID %q: %v", anchorID, parseErr)
	}
	sinceID := core.EventID(anchorUUID)

	var logCount int
	for evt := range eventbus.ScanAfter(eventsPath, sinceID) {
		if evt.Type != "agent_message" {
			continue
		}
		var p AgentMessagePayload
		if json.Unmarshal(evt.Payload, &p) == nil && MatchAgentMessage(p, "alice", "", "") {
			logCount++
		}
	}
	if logCount != 3 {
		t.Errorf("comms log --since anchor: got %d alice messages in events.jsonl, want 3 (cursor advance must not remove events from log)",
			logCount)
	}

	t.Logf("B1 PASS: follow delivered 3 + cursor=id3 → recv drained 0 (intentional); comms log still shows 3 (cursor-independent)")
}
