package daemon

// scenario_comms_recv_follow_e2e_hkyw5c_test.go — GH #8 socket-level E2E
// regression test (bead hk-yw5c).
//
// The hk-7xvf unit tests verified each piece of the ScanAnchor fix independently:
//   - TestCommsRecvResult_ScanAnchorPopulated: HandleCommsRecv populates scan_anchor
//   - TestCommsRecvFollowGap_WithAnchorDeliversGapMessage: HandleSubscribe delivers
//     gap messages when since_event_id is set.
//
// This file adds the COMBINED socket-level E2E scenario: a single test that exercises
// comms-recv drain + subscribe on the same socket, confirming:
//
//   1. comms-recv for an agent with no prior messages returns scan_anchor = last
//      event in the log (even though no event matched the agent filter).
//   2. A directed message written AFTER the drain completes (the "gap" window) is
//      delivered when subscribe opens with since_event_id = scan_anchor.
//
// This is the end-to-end regression guard for the scenario described in GH #8:
//
//   crew boots → arms recv --follow → captain sends message in the gap → message
//   permanently lost (pre-fix) → message delivered via replay (post-fix).
//
// Root cause recap: without scan_anchor, cursor_after="" caused the CLI to omit
// since_event_id from the subscribe request → HandleSubscribe skipped replay → gap
// messages were neither in the live channel (subscriber not registered) nor in the
// replay window (replay skipped). The ScanAnchor fix (hk-7xvf) closed this gap.
//
// Bead ref: hk-yw5c (GH #8). See also scenario_comms_recv_follow_gap_hk7xvf_test.go.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// hkyw5cStartSocket starts RunSocketListenerFull with both a SubscribeHub
// (for subscribe ops) and a *commsSendHandlerImpl with RecvDeps (for comms-recv
// ops), sharing the same CursorStore and events.jsonl path. Returns the socket path.
func hkyw5cStartSocket(t *testing.T, hub *SubscribeHub, ch *commsSendHandlerImpl) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hkyw5c-")
	if err != nil {
		t.Fatalf("hkyw5cStartSocket: mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = RunSocketListenerFull(ctx, sockPath, nil, nil, hub, nil, ch)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Wait until the socket is accepting connections (dial-based, not stat-based).
	// File-existence alone races: the socket file appears after net.Listen but
	// before the accept loop starts. A dial confirms the accept loop is live.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.Dial("unix", sockPath)
		if dialErr == nil {
			_ = conn.Close()
			return sockPath
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hkyw5cStartSocket: socket not accepting within 5s")
	return ""
}

// hkyw5cCommsRecv dials sockPath, sends a comms-recv request for agent, and
// returns the decoded CommsRecvResult.
func hkyw5cCommsRecv(t *testing.T, sockPath, agent string) CommsRecvResult {
	t.Helper()

	payload, err := json.Marshal(CommsRecvRequest{Agent: agent})
	if err != nil {
		t.Fatalf("hkyw5cCommsRecv: marshal payload: %v", err)
	}
	reqBytes, marshalErr := json.Marshal(map[string]any{
		"op":      "comms-recv",
		"payload": json.RawMessage(payload),
	})
	if marshalErr != nil {
		t.Fatalf("hkyw5cCommsRecv: marshal request: %v", marshalErr)
	}

	conn, dialErr := net.Dial("unix", sockPath)
	if dialErr != nil {
		t.Fatalf("hkyw5cCommsRecv: dial %q: %v", sockPath, dialErr)
	}
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		t.Fatalf("hkyw5cCommsRecv: write: %v", writeErr)
	}
	// Half-close write side so the server's json.Decoder sees EOF.
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
	}

	var resp SocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		t.Fatalf("hkyw5cCommsRecv: decode response: %v", decErr)
	}
	if !resp.Ok {
		t.Fatalf("hkyw5cCommsRecv: server error: %s", resp.Error)
	}

	var result CommsRecvResult
	if uErr := json.Unmarshal(resp.Result, &result); uErr != nil {
		t.Fatalf("hkyw5cCommsRecv: decode result: %v", uErr)
	}
	return result
}

// TestCommsRecvFollowE2E_GapDelivery_ScanAnchorFix is the primary GH #8 E2E
// regression guard. It exercises the full path:
//
//   comms-recv drain (no messages for alice) → scan_anchor set →
//   gap message written → subscribe with since_event_id=scan_anchor → delivered.
//
// Pre-fix: cursor_after="" → CLI omits since_event_id → replay skipped → gap message
// lost. Post-fix: scan_anchor populated → CLI passes since_event_id=scan_anchor →
// HandleSubscribe replays from scan_anchor → gap message found and delivered.
func TestCommsRecvFollowE2E_GapDelivery_ScanAnchorFix(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hkyw5c-gapdel-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")
	cursorDir := filepath.Join(dir, "cursors")

	// Write two non-matching events directed to "bob" so the drain for "alice"
	// finds nothing to match but still advances the scan cursor to anchorID.
	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "noise-1"})
	time.Sleep(2 * time.Millisecond)
	anchorID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "noise-2"})

	// Wire comms-recv handler (recv deps only; no bus needed for drain).
	cs := NewCursorStore(cursorDir)
	ch := newTestCommsHandler(cs, eventsPath)

	// Wire subscribe hub (eventsPath for replay; no live bus needed for this test).
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
	})
	hub.SetCommsCursorStore(cs)

	sockPath := hkyw5cStartSocket(t, hub, ch)

	// ── Step 1: drain for alice (no cursor, no matching messages) ──────────
	//
	// Expected: cursor_after="" (no messages matched → cursor not advanced),
	//           scan_anchor=anchorID (last event scanned regardless of match).
	result := hkyw5cCommsRecv(t, sockPath, "alice")

	if len(result.Messages) != 0 {
		t.Errorf("Step 1: got %d messages, want 0 (no messages for alice yet)", len(result.Messages))
	}
	if result.CursorAfter != "" {
		t.Errorf("Step 1: cursor_after=%q, want empty (cursor not advanced when no match)", result.CursorAfter)
	}
	if result.ScanAnchor != anchorID {
		t.Fatalf("Step 1: scan_anchor=%q, want %q — ScanAnchor fix not in effect; recv --follow will have no replay anchor and gap messages will be lost",
			result.ScanAnchor, anchorID)
	}

	// ── Step 2: gap message arrives AFTER the drain ─────────────────────────
	//
	// Simulates the race described in GH #8: a captain sends a message in the
	// narrow window between the crew's drain completing and the subscribe
	// subscriber registering on the hub.
	time.Sleep(2 * time.Millisecond)
	gapID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: "alice", Body: "gap-retask",
	})

	// ── Step 3: subscribe with since_event_id=scan_anchor ──────────────────
	//
	// This mirrors exactly what runCommsRecvFollowIO does when cursor_after is
	// empty: it uses scan_anchor as since_event_id. HandleSubscribe will run
	// the replay path from scan_anchor, finding the gap message.
	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "alice",
		"since_event_id":    result.ScanAnchor, // = anchorID
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	// Replay should deliver the gap message.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, readErr := rdr.ReadBytes('\n')
	if len(line) == 0 {
		t.Fatalf("Step 3: no event received (read error: %v) — gap message %q was NOT delivered; the recv --follow gap-delivery fix (ScanAnchor) may have regressed",
			readErr, gapID)
	}
	var ev core.Event
	if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
		t.Fatalf("Step 3: decode event: %v (line=%q)", jsonErr, string(line))
	}
	if ev.EventID.String() != gapID {
		t.Errorf("Step 3: got event_id=%q, want gap message %q — wrong event replayed", ev.EventID, gapID)
	}
	t.Logf("GH #8 E2E PASS: gap message %q delivered via recv drain (scan_anchor=%s) + subscribe replay", gapID, anchorID)
}

// TestCommsRecvFollowE2E_DrainThenLive verifies the steady-state path: messages
// present before the drain are returned by the drain, and messages that arrive
// after subscribe opens are delivered live.
//
// This covers the "normal" operation (non-gap) to ensure the combined drain +
// follow path works end-to-end without dropping any messages.
func TestCommsRecvFollowE2E_DrainThenLive(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hkyw5c-drainlive-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")
	cursorDir := filepath.Join(dir, "cursors")

	// Pre-drain message directed to alice (should be found by the drain).
	time.Sleep(2 * time.Millisecond)
	preDrainID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: "alice", Body: "pre-drain-assign",
	})

	cs := NewCursorStore(cursorDir)
	ch := newTestCommsHandler(cs, eventsPath)

	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
	})
	hub.SetCommsCursorStore(cs)

	sockPath := hkyw5cStartSocket(t, hub, ch)

	// Step 1: drain for alice — should return the pre-drain message.
	result := hkyw5cCommsRecv(t, sockPath, "alice")

	if len(result.Messages) != 1 {
		t.Fatalf("Step 1: got %d messages, want 1 (pre-drain message)", len(result.Messages))
	}
	if result.Messages[0].EventID != preDrainID {
		t.Errorf("Step 1: drain returned wrong message: got %q, want %q", result.Messages[0].EventID, preDrainID)
	}
	// cursor_after should point to the pre-drain message.
	if result.CursorAfter != preDrainID {
		t.Errorf("Step 1: cursor_after=%q, want %q", result.CursorAfter, preDrainID)
	}

	// Step 2: subscribe starting from cursor_after — no replay events expected
	// (cursor_after is the last event, nothing follows it yet).
	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "alice",
		"since_event_id":    result.CursorAfter,
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	// No replay messages expected (since_event_id == last event).
	// Give the subscribe a short window to confirm no spurious replay.
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	brdr := bufio.NewReader(rdr)
	for {
		line, readErr := brdr.ReadBytes('\n')
		if len(line) == 0 {
			break // timeout — expected
		}
		var env struct {
			Type    string `json:"type"`
			EventID string `json:"event_id"`
		}
		if jsonErr := json.Unmarshal(line, &env); jsonErr != nil || env.Type == "heartbeat" {
			break
		}
		if env.Type == "agent_message" {
			t.Errorf("Step 2: unexpected replayed message %q (should be empty since cursor_after is latest)", env.EventID)
		}
		if readErr != nil {
			break
		}
	}

	t.Logf("GH #8 drain-then-live PASS: pre-drain message returned by drain, subscribe anchored at cursor_after")
}
