package main

// comms_recv_wait_hktafd4_test.go — tests for `comms recv --wait [--timeout]`
// (hk-tafd4 FIX b) and the --follow/--wait mutual-exclusion guard.
//
// runCommsRecvWait opens a subscribe stream over the daemon socket, blocks until
// exactly one matching agent_message arrives (via the replay path here), prints
// it, and exits 0. The daemon advances the agent's durable cursor as it delivers
// the message, so the cursor is consumed exactly as a one-shot `comms recv` would
// consume it. With --timeout it exits 3 if no message arrives in time.

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

// waitTestStartHub stands up a SubscribeHub over a unix socket with the given
// events.jsonl path and (optional) cursor store. Returns the socket path.
func waitTestStartHub(t *testing.T, eventsPath string, cs *daemon.CursorStore) string {
	t.Helper()
	dir := socketSafeTempDir(t)
	sockPath := filepath.Join(dir, "daemon.sock")

	hub := daemon.NewSubscribeHub(daemon.SubscribeHubConfig{
		Bus:             nil,
		EventsJSONLPath: eventsPath,
	})
	if cs != nil {
		hub.SetCommsCursorStore(cs)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.RunSocketListenerWithSubscribe(ctx, sockPath, nil, nil, hub)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sockPath
}

// waitTestSeedAgentMessage writes one agent_message event to eventsPath and
// returns its event_id string. anchor (returned separately) is an earlier id
// suitable as the since_event_id cursor.
func waitTestSeedAgentMessage(t *testing.T, eventsPath, to, from, body string) (anchorID, msgID string) {
	t.Helper()
	anchor, _ := uuid.NewV7()
	time.Sleep(2 * time.Millisecond)
	mid, _ := uuid.NewV7()
	payload, _ := json.Marshal(map[string]any{"from": from, "to": to, "body": body})
	ev := core.Event{
		EventID:         core.EventID(mid),
		SchemaVersion:   1,
		Type:            "agent_message",
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(payload),
	}
	if err := os.MkdirAll(filepath.Dir(eventsPath), 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	f, err := os.Create(eventsPath)
	if err != nil {
		t.Fatalf("create events.jsonl: %v", err)
	}
	if encErr := json.NewEncoder(f).Encode(ev); encErr != nil {
		t.Fatalf("encode event: %v", encErr)
	}
	_ = f.Close()
	return anchor.String(), mid.String()
}

// captureStdoutDuring runs fn with os.Stdout redirected to a pipe and returns the
// captured output.
func captureStdoutDuring(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	_ = r.Close()
	return b.String()
}

// TestCommsRecvWait_DeliversOneAndAdvancesCursor verifies --wait returns exactly
// one message (exit 0), prints it, and advances the durable cursor past it.
func TestCommsRecvWait_DeliversOneAndAdvancesCursor(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")
	cs := daemon.NewCursorStore(filepath.Join(dir, "cursors"))

	anchorID, msgID := waitTestSeedAgentMessage(t, eventsPath, "alice", "bob", "hello-wait")
	sockPath := waitTestStartHub(t, eventsPath, cs)

	var code int
	out := captureStdoutDuring(t, func() {
		code = runCommsRecvWait(sockPath, "alice", "", "", anchorID, true /*jsonOut*/, 5*time.Second)
	})

	if code != 0 {
		t.Fatalf("runCommsRecvWait: exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "hello-wait") {
		t.Fatalf("runCommsRecvWait stdout = %q, want it to contain the message body", out)
	}
	var got struct {
		EventID string `json:"event_id"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("decode wait output %q: %v", out, err)
	}
	if got.EventID != msgID {
		t.Errorf("delivered event_id = %q, want %q", got.EventID, msgID)
	}

	// Cursor must have advanced to the delivered message (flushed on conn close).
	deadline := time.Now().Add(3 * time.Second)
	var cursor string
	for time.Now().Before(deadline) {
		cursor, _ = cs.Get("alice")
		if cursor == msgID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cursor != msgID {
		t.Fatalf("durable cursor after --wait = %q, want %q (cursor not advanced)", cursor, msgID)
	}
}

// TestCommsRecvWait_TimeoutExit verifies --wait --timeout exits 3 when no
// matching message arrives in the window.
func TestCommsRecvWait_TimeoutExit(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")
	// Empty events file → no message will ever be delivered.
	if err := os.MkdirAll(filepath.Dir(eventsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(eventsPath, nil, 0o644); err != nil {
		t.Fatalf("write empty events: %v", err)
	}
	sockPath := waitTestStartHub(t, eventsPath, nil)

	anchor, _ := uuid.NewV7()
	start := time.Now()
	code := runCommsRecvWait(sockPath, "ghost", "", "", anchor.String(), false, 300*time.Millisecond)
	elapsed := time.Since(start)

	if code != commsRecvWaitTimeoutExit {
		t.Fatalf("runCommsRecvWait timeout: exit code = %d, want %d", code, commsRecvWaitTimeoutExit)
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("timeout returned too early (%s); expected ~300ms wait", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took too long (%s)", elapsed)
	}
}

// TestCommsRecvSubcommand_FollowWaitMutualExclusion verifies the CLI rejects
// passing both --follow and --wait, and that --timeout requires --wait.
func TestCommsRecvSubcommand_FollowWaitMutualExclusion(t *testing.T) {
	if got := runCommsRecvSubcommand([]string{"--agent", "x", "--follow", "--wait"}); got != 1 {
		t.Errorf("--follow --wait: exit = %d, want 1", got)
	}
	if got := runCommsRecvSubcommand([]string{"--agent", "x", "--timeout", "5s"}); got != 1 {
		t.Errorf("--timeout without --wait: exit = %d, want 1", got)
	}
	if got := runCommsRecvSubcommand([]string{"--agent", "x", "--timeout", "not-a-dur", "--wait"}); got != 1 {
		t.Errorf("--timeout bad duration: exit = %d, want 1", got)
	}
}
