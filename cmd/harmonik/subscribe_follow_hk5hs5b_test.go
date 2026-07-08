package main

// subscribe_follow_hk5hs5b_test.go — tests for `harmonik subscribe --follow`
// auto-reconnect behaviour (hk-5hs5b).
//
// The tests verify:
//   1. First-dial failure (daemon absent) → exit 17 (backwards-compat preserved).
//   2. Connection-drop after first messages → reconnect, deliver events from the
//      second server without gaps or duplicates.
//   3. Heartbeat last_event_id advances lastSeen watermark so reconnects in quiet
//      periods use max(prior, heartbeat.last_event_id) as since_event_id (EV-037a).

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// subscribeFollowBaseReq is a minimal base request used across follow tests.
var subscribeFollowBaseReq = map[string]any{
	"op":                "subscribe",
	"heartbeat_seconds": 60,
}

// TestSubscribeFollow_Exit17WhenDaemonAbsentOnFirstDial verifies that the
// first-dial-fail path (daemon not running) still returns exit 17 — the
// reconnect loop must NOT silently retry on the very first attempt.
func TestSubscribeFollow_Exit17WhenDaemonAbsentOnFirstDial(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/missing.sock"
	code := runSubscribeFollowIO(subscribeFollowBaseReq, sockPath, "", os.Stdout, "")
	if code != 17 {
		t.Fatalf("runSubscribeFollowIO with missing socket: exit %d, want 17", code)
	}
}

// TestSubscribeFollow_Reconnect verifies that when the subscribe connection
// drops (daemon restart simulation), --follow reconnects, re-anchors at the
// last seen event_id, and delivers subsequent events.
//
// Scenario:
//   - Start a raw Unix listener (server-1). It sends one event then closes the conn.
//   - runSubscribeFollowIO connects, receives the event, updates lastSeen.
//   - Server-1 closes → follow detects EOF and reconnects after backoff.
//   - Start server-2 on the same socket. It captures the since_event_id from the
//     reconnect request and sends a second event.
//   - Verify both events appear in the output and that the reconnect request
//     carried the first event's ID as since_event_id (no gap, no duplicate).
//
// Uses /tmp for the socket to stay within the 104-byte macOS sun_path limit.
func TestSubscribeFollow_Reconnect(t *testing.T) {
	sockPath := "/tmp/hk5hs5b-sub.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	// Two events with distinct IDs.
	ev1ID, _ := uuid.NewV7()
	time.Sleep(2 * time.Millisecond)
	ev2ID, _ := uuid.NewV7()

	// reconnectSince captures the since_event_id seen by the second connection.
	reconnectSince := make(chan string, 1)

	var connCount int32

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			n := atomic.AddInt32(&connCount, 1)
			go func(c net.Conn, num int32) {
				defer func() { _ = c.Close() }()

				// Read and discard the subscribe request.
				var req map[string]any
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					return
				}

				switch num {
				case 1:
					// Send event 1, then close → triggers reconnect.
					ev := map[string]any{
						"type":     "run_completed",
						"event_id": ev1ID.String(),
					}
					_ = json.NewEncoder(c).Encode(ev)
					// conn closed on return

				case 2:
					// Capture since_event_id from the reconnect request.
					since, _ := req["since_event_id"].(string)
					reconnectSince <- since

					// Send event 2 so the follow loop has output to write.
					ev := map[string]any{
						"type":     "run_completed",
						"event_id": ev2ID.String(),
					}
					_ = json.NewEncoder(c).Encode(ev)
					// conn closed on return; follow loop will retry — fine for this test.
				}
			}(conn, n)
		}
	}()

	outFile, _ := os.CreateTemp(t.TempDir(), "sub-follow-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	go func() {
		runSubscribeFollowIO(subscribeFollowBaseReq, sockPath, "" /*sinceEventID*/, outFile, "")
	}()

	// Wait for the second connection and capture its since_event_id.
	select {
	case since := <-reconnectSince:
		if since != ev1ID.String() {
			t.Errorf("reconnect since_event_id=%q, want %q (event 1 id)", since, ev1ID.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for reconnect; follow may not be retrying after EOF")
	}

	// Verify both events appeared in the output.
	_ = outFile.Sync()
	raw, _ := os.ReadFile(outFile.Name())
	out := string(raw)
	if !strings.Contains(out, ev1ID.String()) {
		t.Errorf("event 1 (%s) not found in follow output:\n%s", ev1ID, out)
	}
	if !strings.Contains(out, ev2ID.String()) {
		t.Errorf("event 2 (%s) not found in follow output:\n%s", ev2ID, out)
	}
	// No duplicate: event 1 should appear exactly once.
	if n := strings.Count(out, ev1ID.String()); n > 1 {
		t.Errorf("event 1 appeared %d times (want ≤1); reconnect caused duplicate delivery", n)
	}
}

// TestSubscribeFollow_WatermarkAdvancesOnHeartbeat verifies EV-037a for
// `harmonik subscribe --follow`: a heartbeat carrying last_event_id must advance
// lastSeen so that the subsequent reconnect supplies
// since_event_id=max(prior, heartbeat.last_event_id). Without this, a quiet
// period leaves the watermark stale and the daemon re-replays all events.
func TestSubscribeFollow_WatermarkAdvancesOnHeartbeat(t *testing.T) {
	sockPath := "/tmp/hk5hs5b-hb.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	hbID, _ := uuid.NewV7()
	heartbeatLastEventID := hbID.String()

	reconnectSince := make(chan string, 1)
	var connCount int32

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			n := atomic.AddInt32(&connCount, 1)
			go func(c net.Conn, num int32) {
				defer func() { _ = c.Close() }()

				var req map[string]any
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					return
				}

				switch num {
				case 1:
					// Send heartbeat with last_event_id, then close.
					hb := map[string]any{
						"type":          "heartbeat",
						"last_event_id": heartbeatLastEventID,
					}
					_ = json.NewEncoder(c).Encode(hb)

				case 2:
					// Capture since_event_id from the reconnect request.
					since, _ := req["since_event_id"].(string)
					reconnectSince <- since

					// Send one event so the loop has something to process.
					ev2ID, _ := uuid.NewV7()
					ev := map[string]any{"type": "run_completed", "event_id": ev2ID.String()}
					_ = json.NewEncoder(c).Encode(ev)
				}
			}(conn, n)
		}
	}()

	outFile, _ := os.CreateTemp(t.TempDir(), "sub-hb-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	go func() {
		runSubscribeFollowIO(subscribeFollowBaseReq, sockPath, "" /*sinceEventID*/, outFile, "")
	}()

	select {
	case since := <-reconnectSince:
		if since != heartbeatLastEventID {
			t.Errorf("reconnect since_event_id=%q, want %q (heartbeat.last_event_id — EV-037a)", since, heartbeatLastEventID)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for reconnect; watermark may not be advancing from heartbeat")
	}
}
