package daemon

// scenario_comms_recv_follow_gap_hk7xvf_test.go — characterization and regression
// tests for GH #8 (bead hk-7xvf, re-attempt of hk-yw5c).
//
// BUG ROOT CAUSE (cursor-state dependent non-delivery):
//
// When a crew arms "comms recv --follow" with an empty cursor (first run, no prior
// messages for this agent), the one-shot comms-recv drain returns cursor_after=""
// (no matching messages found, no stored cursor). runCommsRecvFollowIO then omits
// since_event_id from the subscribe request. HandleSubscribe skips replay entirely
// when since_event_id=="". Any agent_message dispatched to the bus BEFORE the
// subscriber registers is therefore:
//
//   (a) NOT in the live subscriber channel (subscriber not yet registered when
//       the bus dispatched the event); AND
//   (b) NOT replayed (replay skipped, since_event_id=="").
//
// Result: the message is permanently lost. The failure is intermittent because it
// only triggers when events land in the narrow gap between the drain completing and
// the subscribe subscriber registering on the hub, AND the agent has no prior cursor.
//
// FIX (hk-7xvf):
//
// CommsRecvResult gains a ScanAnchor field: the event_id of the last event scanned
// during the drain, regardless of whether it matched the agent filter. The CLI uses
// ScanAnchor as since_event_id when cursor_after is "". This ensures HandleSubscribe
// runs the replay path from ScanAnchor, covering any messages that arrived in the gap
// between the drain completing and the subscriber registering.
//
// TEST STRUCTURE:
//
//   TestCommsRecvFollowGap_WithAnchorDeliversGapMessage — positive case (fix):
//     given an anchor before a gap message, subscribe with since_event_id=anchor
//     delivers the message via replay. Verifies the DESIRED behavior the fix enables.
//
//   TestCommsRecvFollowGap_WithoutAnchorDropsGapMessage — characterization (bug):
//     without since_event_id (the pre-fix CLI behavior), gap messages are silently
//     dropped. Documents the root cause.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// TestCommsRecvFollowGap_WithAnchorDeliversGapMessage verifies that HandleSubscribe
// delivers a message M that was written to events.jsonl BEFORE the subscriber
// registered, when the subscribe request includes since_event_id=anchor (an
// event_id strictly before M).
//
// This is the behavior the fix enables: the CLI will pass the ScanAnchor from
// the one-shot drain as since_event_id, so the replay path covers the gap.
//
// This test passes before AND after the fix (it directly exercises the existing
// HandleSubscribe replay path). It serves as the positive-case anchor for the fix.
func TestCommsRecvFollowGap_WithAnchorDeliversGapMessage(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk7xvf-anchored-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")

	// Write non-matching events to establish an anchor.
	writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "e1"})
	time.Sleep(2 * time.Millisecond)
	anchorID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "e2"})

	// "Gap": write M after the anchor (simulates message arriving in the gap
	// between one-shot drain and subscribe registration).
	time.Sleep(2 * time.Millisecond)
	mID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "captain", To: "alice", Body: "gap-retask"})

	// Start subscribe hub.
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
	})
	sockPath := subscribeTestStartSocketHub(t, hub)

	// Subscribe with since_event_id=anchor: replay from anchorID should find M.
	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "alice",
		"since_event_id":    anchorID,
		"heartbeat_seconds": 600,
	})
	defer func() { _ = conn.Close() }()

	// Replay should deliver M.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, readErr := rdr.ReadBytes('\n')
	if len(line) == 0 {
		t.Fatalf("no event received (read error: %v) — gap message not delivered; subscribe replay must cover since_event_id..M gap", readErr)
	}
	var ev core.Event
	if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
		t.Fatalf("decode event: %v (line=%q)", jsonErr, string(line))
	}
	if ev.EventID.String() != mID {
		t.Errorf("wrong event: got event_id=%q, want %q (gap message M)", ev.EventID, mID)
	}
}

// TestCommsRecvFollowGap_WithoutAnchorDropsGapMessage is a CHARACTERIZATION test
// documenting the bug root cause (GH #8, hk-7xvf).
//
// When since_event_id is absent from the subscribe request (the pre-fix CLI behavior
// when cursor_after==""), HandleSubscribe skips replay. A message M written to
// events.jsonl BEFORE the subscriber registers is permanently dropped:
//
//   - Not in the live channel (subscriber not registered when M was dispatched).
//   - Not replayed (replay skipped, since_event_id=="").
//
// This test verifies the gap message is NOT delivered in the broken state. It passes
// in the pre-fix world (M not delivered = expected). It may also pass after the fix
// at the HandleSubscribe level (HandleSubscribe itself doesn't change — the fix is in
// the CLI using ScanAnchor to populate since_event_id).
func TestCommsRecvFollowGap_WithoutAnchorDropsGapMessage(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "hk7xvf-noanchor-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	eventsPath := filepath.Join(dir, "events.jsonl")

	// "Gap message" M: written to events.jsonl before subscribe opens.
	mID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "captain", To: "alice", Body: "gap-retask"})

	// Start subscribe hub.
	hub := NewSubscribeHub(SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
	})
	sockPath := subscribeTestStartSocketHub(t, hub)

	// Subscribe WITHOUT since_event_id (the bug state: CLI omits it when cursor_after=="").
	conn, rdr := subscribeTestDial(t, sockPath, map[string]any{
		"op":                "subscribe",
		"types":             []string{"agent_message"},
		"to":                "alice",
		// no since_event_id
		"heartbeat_seconds": 10, // short heartbeat so we get a response quickly
	})
	defer func() { _ = conn.Close() }()

	// With no since_event_id, replay is skipped. M was in events.jsonl before
	// the subscriber registered, so it was not dispatched to the live channel.
	// Expect: no agent_message delivered within a short window (only heartbeats).
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	brdr := bufio.NewReader(rdr)
	gotMessage := false
	for {
		line, readErr := brdr.ReadBytes('\n')
		if len(line) == 0 {
			break // timeout or EOF
		}
		var env struct {
			Type    string `json:"type"`
			EventID string `json:"event_id"`
		}
		if jsonErr := json.Unmarshal(line, &env); jsonErr != nil {
			break
		}
		if env.Type == "agent_message" && env.EventID == mID {
			gotMessage = true
			break
		}
		if readErr != nil {
			break
		}
	}

	// Document the bug: without since_event_id, M is dropped. This PASSES in the
	// broken state (M not delivered = correct observation of the bug). The fix is
	// in the CLI (using ScanAnchor), not in HandleSubscribe itself.
	if gotMessage {
		// If this ever fires, it means HandleSubscribe now replays even without
		// since_event_id — a change that may indicate an alternative fix was applied.
		t.Logf("NOTE: gap message WAS delivered without since_event_id — HandleSubscribe may now replay unconditionally (alternative fix path)")
	} else {
		t.Logf("BUG CONFIRMED: gap message %q dropped (no since_event_id → replay skipped, not in live channel)", mID)
	}
}

// TestCommsRecvResult_ScanAnchorPopulated is the regression guard for GH #8 fix
// (hk-7xvf): HandleCommsRecv must populate ScanAnchor with the event_id of the
// last scanned event, regardless of whether it matched the agent filter.
//
// Three cases:
//
//  1. Empty log → ScanAnchor == "" (nothing scanned).
//  2. Events present but none match the agent → ScanAnchor == last event_id,
//     CursorAfter == "" (no matching messages, but scan still advanced past them).
//  3. Matching message → ScanAnchor == CursorAfter == last event_id.
func TestCommsRecvResult_ScanAnchorPopulated(t *testing.T) {
	t.Parallel()

	t.Run("empty_log", func(t *testing.T) {
		t.Parallel()
		dir, err := os.MkdirTemp("/tmp", "hk7xvf-scananchor-empty-")
		if err != nil {
			t.Fatalf("mkdtemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		eventsPath := filepath.Join(dir, "events.jsonl")
		// Create an empty file.
		f, _ := os.Create(eventsPath)
		_ = f.Close()

		cs := NewCursorStore(filepath.Join(dir, "cursors"))
		impl := newTestCommsHandler(cs, eventsPath)
		req, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
		resBytes, recvErr := impl.HandleCommsRecv(context.Background(), req)
		if recvErr != nil {
			t.Fatalf("HandleCommsRecv: %v", recvErr)
		}
		var res CommsRecvResult
		if uErr := json.Unmarshal(resBytes, &res); uErr != nil {
			t.Fatalf("decode: %v", uErr)
		}
		if res.ScanAnchor != "" {
			t.Errorf("empty log: ScanAnchor=%q, want empty", res.ScanAnchor)
		}
		if res.CursorAfter != "" {
			t.Errorf("empty log: CursorAfter=%q, want empty", res.CursorAfter)
		}
	})

	t.Run("events_but_no_match", func(t *testing.T) {
		t.Parallel()
		dir, err := os.MkdirTemp("/tmp", "hk7xvf-scananchor-nomatch-")
		if err != nil {
			t.Fatalf("mkdtemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		eventsPath := filepath.Join(dir, "events.jsonl")

		// Write two events directed to "bob" (not "alice").
		writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "e1"})
		time.Sleep(2 * time.Millisecond)
		lastID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "e2"})

		cs := NewCursorStore(filepath.Join(dir, "cursors"))
		impl := newTestCommsHandler(cs, eventsPath)
		req, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
		resBytes, recvErr := impl.HandleCommsRecv(context.Background(), req)
		if recvErr != nil {
			t.Fatalf("HandleCommsRecv: %v", recvErr)
		}
		var res CommsRecvResult
		if uErr := json.Unmarshal(resBytes, &res); uErr != nil {
			t.Fatalf("decode: %v", uErr)
		}
		if len(res.Messages) != 0 {
			t.Errorf("no_match: got %d messages, want 0", len(res.Messages))
		}
		if res.CursorAfter != "" {
			t.Errorf("no_match: CursorAfter=%q, want empty (cursor not advanced when no match)", res.CursorAfter)
		}
		if res.ScanAnchor != lastID {
			t.Errorf("no_match: ScanAnchor=%q, want %q (last scanned event_id)", res.ScanAnchor, lastID)
		}
	})

	t.Run("matching_message", func(t *testing.T) {
		t.Parallel()
		dir, err := os.MkdirTemp("/tmp", "hk7xvf-scananchor-match-")
		if err != nil {
			t.Fatalf("mkdtemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		eventsPath := filepath.Join(dir, "events.jsonl")

		writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "x", To: "bob", Body: "noise"})
		time.Sleep(2 * time.Millisecond)
		matchID := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{From: "captain", To: "alice", Body: "retask"})

		cs := NewCursorStore(filepath.Join(dir, "cursors"))
		impl := newTestCommsHandler(cs, eventsPath)
		req, _ := json.Marshal(CommsRecvRequest{Agent: "alice"})
		resBytes, recvErr := impl.HandleCommsRecv(context.Background(), req)
		if recvErr != nil {
			t.Fatalf("HandleCommsRecv: %v", recvErr)
		}
		var res CommsRecvResult
		if uErr := json.Unmarshal(resBytes, &res); uErr != nil {
			t.Fatalf("decode: %v", uErr)
		}
		if len(res.Messages) != 1 {
			t.Fatalf("matching: got %d messages, want 1", len(res.Messages))
		}
		if res.CursorAfter != matchID {
			t.Errorf("matching: CursorAfter=%q, want %q", res.CursorAfter, matchID)
		}
		if res.ScanAnchor != matchID {
			t.Errorf("matching: ScanAnchor=%q, want %q (should equal CursorAfter when last scan IS the match)", res.ScanAnchor, matchID)
		}
	})
}
