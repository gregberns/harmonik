package main

// comms_recv_follow_hk5xuvc_test.go — tests for `comms recv --follow`
// reconnect behaviour (F12 fix, bead hk-5xuvc).
//
// The tests verify:
//   1. First-dial failure (daemon absent) → exit 17 (old behaviour preserved).
//   2. Connection-drop after first messages → reconnect, deliver messages from
//      the second server without gaps or duplicates (F12 fix).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
