package daemon

// scenario_comms_b1_follow_starves_recv_hkd65rb_test.go — T2c L1:
// B1 decoupled poll/live cursor pin + inline doc.
//
// Originally pinned hk-d65rb's "follow-starves-recv is correct" behavior (a
// shared cursor between `recv --follow` and one-shot `comms recv`). That
// behavior was operator-RATIFIED as a BUG via T4 (hk-tnyb7) and fixed by
// hk-8xspi: `comms recv --agent` now owns an independent POLL cursor, fully
// decoupled from the LIVE cursor `--follow`/`--wait` share with SubscribeHub.
// This file now pins the DECOUPLED behavior instead.
//
// # Scenario 1: follow no longer starves recv
//
// When `recv --follow` is armed for agent A (SubscribeHub with a LIVE
// CursorStore), messages directed to A are delivered to the follower's stream
// AND the LIVE cursor advances past each delivered event (hk-tafd4). A
// subsequent one-shot `comms recv --agent A` reads its own, separate POLL
// cursor — untouched by the follower — and drains the SAME 3 messages again.
//
// # Why duplicate delivery across the two cursors is safe
//
// N3 at-least-once + mandatory dedupe-on-event_id (agent-comms spec §5 Q1/Q3)
// makes this safe: recipients already tolerate redelivery, so two independent
// cursors each seeing the same backlog once is not a correctness problem — it
// removes the far worse failure mode where a --follow watcher silently drained
// messages a plain poller was still waiting to see (the original recv-drains-0
// -under-follow bug, B1).
//
//   - The events are permanently stored in events.jsonl regardless of either
//     cursor's position. `comms log --since <id>` (cursor-independent scan of
//     events.jsonl) always shows the full history.
//
// # Assertions (two)
//
//   1. FULL-DRAIN: after the follower session delivers 3 messages and flushes
//      the LIVE cursor, HandleCommsRecv against the agent's POLL cursor (a
//      request with Live=false) still returns all 3 messages — the poll cursor
//      was never touched by the follower.
//   2. LOG-INTACT: eventbus.ScanAfter(eventsPath, anchorID) — the functional
//      equivalent of `comms log --since <anchorID>` — returns all 3 messages;
//      cursor advance does NOT delete events from events.jsonl (log is append-only).
//
// # Harness (L1 in-process)
//
// Two independent CursorStore instances (poll, live) + SubscribeHub wired to
// the live store (SetCommsCursorStore) + net.Pipe fake client (hk7n6o7Never
// timer — suppresses heartbeats and cursor-flush ticks) + commsSendHandlerImpl
// wired to both stores. No socket, no real time, no daemon process.
//
// Bead: hk-d65rb (original pin), hk-8xspi (B1 decoupling, supersedes the pin).
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

// TestScenario_HkD65rb_B1_DecoupledCursors asserts the two halves of scenario 1:
//
//  1. full-drain: after --follow delivers 3 messages and advances the LIVE cursor,
//     HandleCommsRecv against the agent's independent POLL cursor still returns
//     all 3 messages.
//  2. log-intact: eventbus.ScanAfter (comms-log-equivalent) still returns all 3
//     messages from events.jsonl regardless of either cursor's position.
func TestScenario_HkD65rb_B1_DecoupledCursors(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hkd65rb-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")
	liveCursorDir := filepath.Join(dir, "cursors-live")
	pollCursorDir := filepath.Join(dir, "cursors-poll")

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

	// ── Wire two independent CursorStores: live (follower) and poll (recv) ──
	//
	// SetCommsCursorStore binds the hub to the LIVE store only. The POLL store
	// is separate and is never touched by the follower (hk-8xspi, B1).
	liveCS := NewCursorStore(liveCursorDir)
	pollCS := NewCursorStore(pollCursorDir)
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
		NewTimer:        hk7n6o7Never, // suppress heartbeats + cursor-flush ticks
	})
	hub.SetCommsCursorStore(liveCS)

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

	// Confirm the LIVE cursor advanced to id3 (post-replay flush or defer flush).
	got := waitForCursor(t, liveCS, "alice", id3, 3*time.Second)
	if got != id3 {
		t.Fatalf("live cursor not at id3 after follow session: got %q, want %q", got, id3)
	}

	// ── Assertion 1: full-drain via the independent POLL cursor ─────────────
	//
	// HandleCommsRecv with Live=false (the default) reads alice's POLL cursor,
	// which the follower never touched (still ""), and scans events.jsonl from
	// the beginning — finding all 3 messages. This is the hk-8xspi B1 fix: the
	// poll cursor is no longer starved by a --follow session's consumption.
	impl := &commsSendHandlerImpl{}
	impl.SetRecvDeps(pollCS, liveCS, eventsPath)
	recvPayload, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
	resBytes, recvErr := impl.HandleCommsRecv(context.Background(), recvPayload)
	if recvErr != nil {
		t.Fatalf("HandleCommsRecv: %v", recvErr)
	}
	var res CommsRecvResult
	if uErr := json.Unmarshal(resBytes, &res); uErr != nil {
		t.Fatalf("decode CommsRecvResult: %v", uErr)
	}
	if len(res.Messages) != 3 {
		bodies := make([]string, len(res.Messages))
		for i, m := range res.Messages {
			bodies[i] = m.Body
		}
		t.Errorf("B1 violation: HandleCommsRecv (poll cursor) returned %d message(s) after follower consumed the live cursor; want 3 (poll and live cursors must be independent). messages: %v",
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

	t.Logf("B1 PASS: follow delivered 3 + live cursor=id3 → poll recv independently drained 3; comms log still shows 3 (cursor-independent)")
}
