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
	"context"
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
	code := runSubscribeFollowIO(context.Background(), subscribeFollowBaseReq, sockPath, "", os.Stdout, "")
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
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove stale socket: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove socket during cleanup: %v", err)
		}
	})

	// Two events with distinct IDs.
	ev1ID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new event 1 UUID: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	ev2ID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new event 2 UUID: %v", err)
	}

	// reconnectSince captures the since_event_id seen by the second connection.
	reconnectSince := make(chan string, 1)

	var connCount int32

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			n := atomic.AddInt32(&connCount, 1)
			go func(c net.Conn, num int32) {
				defer func() {
					if err := c.Close(); err != nil {
						t.Errorf("close connection: %v", err)
					}
				}()

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
					if err := json.NewEncoder(c).Encode(ev); err != nil {
						t.Errorf("encode event 1: %v", err)
					}
					// conn closed on return

				case 2:
					// Capture since_event_id from the reconnect request.
					since, ok := req["since_event_id"].(string)
					if !ok {
						t.Errorf("reconnect since_event_id has type %T, want string", req["since_event_id"])
						since = ""
					}
					reconnectSince <- since

					// Send event 2 so the follow loop has output to write.
					ev := map[string]any{
						"type":     "run_completed",
						"event_id": ev2ID.String(),
					}
					if err := json.NewEncoder(c).Encode(ev); err != nil {
						t.Errorf("encode event 2: %v", err)
					}
					// conn closed on return; follow loop will retry — fine for this test.
				}
			}(conn, n)
		}
	}()

	outFile, err := os.CreateTemp(t.TempDir(), "sub-follow-*.txt")
	if err != nil {
		t.Fatalf("create follow output file: %v", err)
	}
	t.Cleanup(func() {
		if err := outFile.Close(); err != nil {
			t.Errorf("close follow output file: %v", err)
		}
	})

	// Cancel + join in cleanup so the reconnect goroutine cannot outlive the
	// test and race a later os.Stderr swap (hk-me8ru).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSubscribeFollowIO(ctx, subscribeFollowBaseReq, sockPath, "" /*sinceEventID*/, outFile, "")
	}()
	t.Cleanup(func() { cancel(); <-done })

	// Wait for the second connection and capture its since_event_id.
	select {
	case since := <-reconnectSince:
		if since != ev1ID.String() {
			t.Errorf("reconnect since_event_id=%q, want %q (event 1 id)", since, ev1ID.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for reconnect; follow may not be retrying after EOF")
	}

	// Verify both events appeared in the output. The reconnectSince signal fires
	// when the daemon RECEIVES the reconnect (case 2), but event 2 is encoded and
	// written by the follow loop asynchronously AFTER that — so a single read here
	// races the write and false-fails under load. Poll the output file until both
	// events are present (or a generous deadline), then assert.
	var out string
	pollDeadline := time.After(15 * time.Second)
	for {
		if err := outFile.Sync(); err != nil {
			t.Fatalf("sync follow output: %v", err)
		}
		raw, err := os.ReadFile(outFile.Name())
		if err != nil {
			t.Fatalf("read follow output: %v", err)
		}
		out = string(raw)
		if strings.Contains(out, ev1ID.String()) && strings.Contains(out, ev2ID.String()) {
			break
		}
		select {
		case <-pollDeadline:
			// Fall through to the assertions below, which report which id is missing.
		case <-time.After(25 * time.Millisecond):
			continue
		}
		break
	}
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
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove stale socket: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove socket during cleanup: %v", err)
		}
	})

	hbID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new heartbeat UUID: %v", err)
	}
	heartbeatLastEventID := hbID.String()

	reconnectSince := make(chan string, 1)
	var connCount int32

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			n := atomic.AddInt32(&connCount, 1)
			go func(c net.Conn, num int32) {
				defer func() {
					if err := c.Close(); err != nil {
						t.Errorf("close connection: %v", err)
					}
				}()

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
					if err := json.NewEncoder(c).Encode(hb); err != nil {
						t.Errorf("encode heartbeat: %v", err)
					}

				case 2:
					// Capture since_event_id from the reconnect request.
					since, ok := req["since_event_id"].(string)
					if !ok {
						t.Errorf("reconnect since_event_id has type %T, want string", req["since_event_id"])
						since = ""
					}
					reconnectSince <- since

					// Send one event so the loop has something to process.
					ev2ID, err := uuid.NewV7()
					if err != nil {
						t.Errorf("new event 2 UUID: %v", err)
						return
					}
					ev := map[string]any{"type": "run_completed", "event_id": ev2ID.String()}
					if err := json.NewEncoder(c).Encode(ev); err != nil {
						t.Errorf("encode event 2: %v", err)
					}
				}
			}(conn, n)
		}
	}()

	outFile, err := os.CreateTemp(t.TempDir(), "sub-hb-*.txt")
	if err != nil {
		t.Fatalf("create heartbeat output file: %v", err)
	}
	t.Cleanup(func() {
		if err := outFile.Close(); err != nil {
			t.Errorf("close heartbeat output file: %v", err)
		}
	})

	// Cancel + join in cleanup so the reconnect goroutine cannot outlive the
	// test and race a later os.Stderr swap (hk-me8ru).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSubscribeFollowIO(ctx, subscribeFollowBaseReq, sockPath, "" /*sinceEventID*/, outFile, "")
	}()
	t.Cleanup(func() { cancel(); <-done })

	select {
	case since := <-reconnectSince:
		if since != heartbeatLastEventID {
			t.Errorf("reconnect since_event_id=%q, want %q (heartbeat.last_event_id — EV-037a)", since, heartbeatLastEventID)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for reconnect; watermark may not be advancing from heartbeat")
	}
}
