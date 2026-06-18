package main

// comms_recv_follow_hk5xuvc_test.go — tests for `comms recv --follow`
// reconnect behaviour (F12 fix, bead hk-5xuvc), EV-037a watermark invariant,
// and park-signal self-quiesce (hk-s8qi, codename:sleep-wake M2).
//
// The tests verify:
//   1. First-dial failure (daemon absent) → exit 17 (old behaviour preserved).
//   2. Connection-drop after first messages → reconnect, deliver messages from
//      the second server without gaps or duplicates (F12 fix).
//   3. Heartbeat last_event_id advances lastSeen watermark so reconnects use
//      max(prior, heartbeat.last_event_id) as since_event_id (EV-037a).
//   4. Park signal (topic="park", from="daemon") causes --follow to deliver the
//      message then exit cleanly (code 0) WITHOUT reconnecting (hk-s8qi M2).

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// followTestSeedMessage appends one agent_message event to eventsPath and
// returns its UUIDv7 event_id string.
func followTestSeedMessage(t *testing.T, eventsPath, to, from, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(eventsPath), 0o755); err != nil {
		t.Fatalf("followTestSeedMessage: mkdir: %v", err)
	}
	mid, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("followTestSeedMessage: uuid: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"from": from, "to": to, "body": body})
	ev := core.Event{
		EventID:         core.EventID(mid),
		SchemaVersion:   1,
		Type:            "agent_message",
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(payload),
	}
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("followTestSeedMessage: open: %v", err)
	}
	if encErr := json.NewEncoder(f).Encode(ev); encErr != nil {
		t.Fatalf("followTestSeedMessage: encode: %v", encErr)
	}
	_ = f.Close()
	return mid.String()
}

// followTestStartHub starts a SubscribeHub on sockPath and returns a cancel
// function that tears the hub down. The caller must ensure sockPath does not
// already exist before calling (or that RunSocketListenerWithSubscribe handles it).
func followTestStartHub(t *testing.T, sockPath, eventsPath string) (cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()
	hub := daemon.NewSubscribeHub(daemon.SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
	})
	ctx, cancelFn := context.WithCancel(context.Background())
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		_ = daemon.RunSocketListenerWithSubscribe(ctx, sockPath, nil, nil, hub)
	}()
	// Wait for socket to appear.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(sockPath); err != nil {
		cancelFn()
		t.Fatalf("followTestStartHub: socket %s did not appear", sockPath)
	}
	return cancelFn, ch
}

// TestCommsRecvFollow_Exit17WhenDaemonAbsentOnFirstDial verifies that the
// first-dial-fail case (daemon not running) still returns exit 17 — the
// reconnect loop must NOT silently retry on the very first attempt.
func TestCommsRecvFollow_Exit17WhenDaemonAbsentOnFirstDial(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "missing.sock")
	// No daemon running — socket does not exist.
	code := runCommsRecvFollow(sockPath, "alice", "", "", "", false)
	if code != 17 {
		t.Fatalf("runCommsRecvFollow with missing socket: exit %d, want 17", code)
	}
}

// TestCommsFollowReconnect verifies F12 fix:
// when the subscribe connection drops (daemon restart), --follow reconnects,
// re-anchors at the last seen event_id, and delivers subsequent messages.
//
// Scenario:
//   - Seed message A into events.jsonl.
//   - Start hub-1; runCommsRecvFollow connects and receives A.
//   - Shut hub-1 down (simulates daemon restart).
//   - Seed message B into events.jsonl.
//   - Start hub-2 on the same socket path; runCommsRecvFollow reconnects and
//     receives B anchored past A (no duplicate delivery of A).
//   - Close the write end to stop the follow loop; verify both A and B appear.
//
// Output is captured to a temp file so there is no pipe-redirection race
// (os.Stdout is a global; pipe-close and goroutine scheduling interact).
// Note: uses a short sockPath under /tmp to stay within the 104-byte macOS
// Unix socket path limit (struct sockaddr_un sun_path).
func TestCommsFollowReconnect(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	// Short path to stay under the 104-byte macOS sun_path limit.
	sockPath := "/tmp/hk5xuvc.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	// Redirect os.Stdout to a temp file so output survives across os.Stdout
	// reassignments in the cleanup/defer chain.
	outFile, err := os.CreateTemp(dir, "follow-out-*.txt")
	if err != nil {
		t.Fatalf("create capture file: %v", err)
	}
	oldOut := os.Stdout
	os.Stdout = outFile
	t.Cleanup(func() {
		os.Stdout = oldOut
		_ = outFile.Close()
	})

	// Create an anchor UUID before message A so ScanAfter(anchor) yields A.
	// The subscribe hub only replays when since_event_id != ""; we pass the
	// anchor as the initial since_event_id so the replay path runs.
	anchorID, anchorErr := uuid.NewV7()
	if anchorErr != nil {
		t.Fatalf("uuid anchor: %v", anchorErr)
	}
	time.Sleep(2 * time.Millisecond) // ensure A's UUID > anchor

	// Seed message A and start hub-1.
	msgAID := followTestSeedMessage(t, eventsPath, "alice", "bob", "reconnect-msg-A")
	cancelHub1, hub1Done := followTestStartHub(t, sockPath, eventsPath)

	// Start --follow anchored at anchorID (so message A is replayed from events.jsonl).
	followDone := make(chan int, 1)
	go func() {
		followDone <- runCommsRecvFollow(sockPath, "alice", "", "", anchorID.String(), true /*jsonOut*/)
	}()

	// Give the follow goroutine time to connect and receive message A.
	time.Sleep(300 * time.Millisecond)

	// Shut down hub-1 (daemon restart simulation).
	cancelHub1()
	<-hub1Done
	_ = os.Remove(sockPath)
	time.Sleep(20 * time.Millisecond)

	// Seed message B and start hub-2.
	msgBID := followTestSeedMessage(t, eventsPath, "alice", "bob", "reconnect-msg-B")
	cancelHub2, hub2Done := followTestStartHub(t, sockPath, eventsPath)
	defer func() {
		cancelHub2()
		<-hub2Done
	}()

	// Poll outFile for message B for up to 15 s (max reconnect backoff = 10 s).
	// Use os.ReadFile (opens the file fresh) — do NOT seek outFile, as seeking
	// would reset the write-end cursor and cause message B to overwrite message A.
	deadline := time.Now().Add(15 * time.Second)
	var combined string
	for time.Now().Before(deadline) {
		raw, readErr := os.ReadFile(outFile.Name())
		if readErr == nil {
			combined = string(raw)
			if strings.Contains(combined, "reconnect-msg-B") {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Stop the follow goroutine by restoring os.Stdout (causes next fmt.Println
	// to not write to the captured file, but the goroutine may also exit on the
	// next pipe/write error or just keep running until the test exits).
	os.Stdout = oldOut
	_ = outFile.Close()
	select {
	case <-followDone:
	case <-time.After(3 * time.Second):
		// still running — acceptable, signal-stop requires SIGTERM
	}

	if !strings.Contains(combined, "reconnect-msg-B") {
		t.Errorf("--follow did not deliver message B after reconnect\noutput=%q\nmsgAID=%s msgBID=%s",
			combined, msgAID, msgBID)
	}
	if !strings.Contains(combined, "reconnect-msg-A") {
		t.Errorf("--follow did not deliver message A (initial backlog)\noutput=%q", combined)
	}
	// No duplicate: message A event_id should appear at most once.
	if n := strings.Count(combined, msgAID); n > 1 {
		t.Errorf("message A event_id appeared %d times (want ≤1, reconnect caused duplicate)\noutput=%q", n, combined)
	}
}

// TestCommsFollowReconnect_WatermarkAdvancesOnHeartbeat verifies EV-037a:
// a heartbeat carrying last_event_id must advance lastSeen so that the
// subsequent reconnect supplies since_event_id=max(prior, heartbeat.last_event_id).
// Without this fix, a quiet period with no agent_message events would leave
// the watermark anchored at its initial value, forcing the daemon to replay
// all events from the beginning on every reconnect.
//
// Note: uses a short sockPath under /tmp to stay within the 104-byte macOS
// Unix socket path limit (struct sockaddr_un sun_path).
func TestCommsFollowReconnect_WatermarkAdvancesOnHeartbeat(t *testing.T) {
	dir := t.TempDir()
	sockPath := "/tmp/hku2ko5.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	// A UUIDv7 that the heartbeat will report as last_event_id.
	heartbeatID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	heartbeatLastEventID := heartbeatID.String()

	// reconnectSinceID captures the since_event_id from the second subscribe request.
	reconnectSinceID := make(chan string, 1)

	// Raw Unix socket listener — gives us control over the exact JSON sent.
	ln, lnErr := net.Listen("unix", sockPath)
	if lnErr != nil {
		t.Fatalf("listen: %v", lnErr)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var connCount int32
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return // listener closed
			}
			n := atomic.AddInt32(&connCount, 1)
			go func(c net.Conn, num int32) {
				defer func() { _ = c.Close() }()

				// Read the subscribe request the client sends.
				var req map[string]any
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					return
				}

				switch num {
				case 1:
					// First connection: emit heartbeat with last_event_id, then close.
					// The follow loop must advance lastSeen to heartbeatLastEventID.
					hb := map[string]any{
						"type":          "heartbeat",
						"ts":            time.Now().UTC().Format(time.RFC3339),
						"last_event_id": heartbeatLastEventID,
						"active_runs":   []any{},
					}
					_ = json.NewEncoder(c).Encode(hb)
					// c closed on return → triggers reconnect.

				case 2:
					// Second connection: capture since_event_id from the reconnect request.
					sinceID, _ := req["since_event_id"].(string)
					reconnectSinceID <- sinceID

					// Send one agent_message so the follow loop has output to write
					// (avoids a silent-exit race before the test reads the channel).
					mid, _ := uuid.NewV7()
					payload, _ := json.Marshal(map[string]any{"from": "srv", "to": "alice", "body": "ok"})
					ev := map[string]any{
						"type":     "agent_message",
						"event_id": mid.String(),
						"payload":  json.RawMessage(payload),
					}
					_ = json.NewEncoder(c).Encode(ev)
					// c closed on return; follow loop will retry — that's fine.
				}
			}(conn, n)
		}
	}()

	// Capture follow-loop output in a temp file rather than redirecting os.Stdout
	// (redirecting the global os.Stdout races with the goroutine reading it — hk-uh6x).
	outFile, _ := os.CreateTemp(dir, "watermark-hb-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	// Run the follow loop; no initial since_event_id (cold start).
	go func() {
		runCommsRecvFollowIO(sockPath, "alice", "", "", "" /* sinceEventID */, true /*jsonOut*/, outFile)
	}()

	// Wait for the second connection's captured since_event_id.
	// Allow up to 15 s to cover the 1 s initial reconnect backoff.
	select {
	case captured := <-reconnectSinceID:
		if captured != heartbeatLastEventID {
			t.Errorf("reconnect since_event_id=%q, want %q (heartbeat.last_event_id — EV-037a)", captured, heartbeatLastEventID)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for second reconnect; watermark may not be advancing from heartbeat")
	}
}

// TestCommsRecvFollow_ParkMessageExitsWithoutReconnect verifies that when the
// daemon emits a park agent_message (topic="park", from="daemon"), comms recv
// --follow delivers the message and exits cleanly (code 0) WITHOUT reconnecting.
//
// This is the Go contract for the park/resume protocol (hk-s8qi M2,
// codename:sleep-wake): the session self-quiesces by having --follow exit on
// the park signal. The session MUST NOT re-arm --follow until after a pane-nudge
// WAKE (defined in the crew-launch and captain skill files).
func TestCommsRecvFollow_ParkMessageExitsWithoutReconnect(t *testing.T) {
	sockPath := "/tmp/hks8qi-park.sock"
	_ = os.Remove(sockPath)
	t.Cleanup(func() { _ = os.Remove(sockPath) })

	// exitCode receives the return value of runCommsRecvFollowIO.
	exitCode := make(chan int, 1)

	// connCount lets the test detect reconnect attempts.
	var connCount int32

	ln, lnErr := net.Listen("unix", sockPath)
	if lnErr != nil {
		t.Fatalf("listen: %v", lnErr)
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
				// Read (and discard) the subscribe request.
				var req map[string]any
				if decErr := json.NewDecoder(c).Decode(&req); decErr != nil {
					return
				}
				if num > 1 {
					// Any second connection means the follow loop reconnected — fail fast.
					return
				}
				// Send a park agent_message directed at "captain" from "daemon".
				mid, _ := uuid.NewV7()
				parkPayload, _ := json.Marshal(map[string]any{
					"from":  "daemon",
					"to":    "captain",
					"topic": "park",
					"body":  `{"type":"park","reason":"drain_detected"}`,
				})
				ev := map[string]any{
					"type":             "agent_message",
					"event_id":         mid.String(),
					"schema_version":   1,
					"timestamp_wall":   time.Now().UTC().Format(time.RFC3339Nano),
					"source_subsystem": "quiesce-arbiter",
					"payload":          json.RawMessage(parkPayload),
				}
				_ = json.NewEncoder(c).Encode(ev)
				// Leave connection open briefly so the client can read the event.
				time.Sleep(200 * time.Millisecond)
				// c closed on return — if follow loop were to reconnect it would
				// hit connCount == 2 and return immediately.
			}(conn, n)
		}
	}()

	outFile, _ := os.CreateTemp(t.TempDir(), "park-test-*.txt")
	t.Cleanup(func() { _ = outFile.Close() })

	go func() {
		exitCode <- runCommsRecvFollowIO(sockPath, "captain", "", "", "", true /*jsonOut*/, outFile)
	}()

	// Expect exit within 5 s; a reconnect path would loop indefinitely.
	select {
	case code := <-exitCode:
		if code != 0 {
			t.Errorf("runCommsRecvFollowIO: exit %d after park signal, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runCommsRecvFollowIO did not exit after park signal (reconnect loop?)")
	}

	// Verify no reconnect attempt was made after the park message.
	conns := atomic.LoadInt32(&connCount)
	if conns > 1 {
		t.Errorf("got %d subscribe connections; park signal should not trigger reconnect (want 1)", conns)
	}

	// Verify the park message was delivered to the output.
	_ = outFile.Sync()
	raw, _ := os.ReadFile(outFile.Name())
	if !strings.Contains(string(raw), `"topic":"park"`) {
		t.Errorf("park message not found in follow output; got:\n%s", raw)
	}
}
