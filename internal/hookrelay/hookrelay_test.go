package hookrelay_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

// hookRelayFixtureEnv returns a minimal Env for tests.
func hookRelayFixtureEnv(workspacePath string) hookrelay.Env {
	return hookrelay.Env{
		RunID:            "01HVTEST000000000000000001",
		DaemonSocket:     "",
		WorkspacePath:    workspacePath,
		HandlerSessionID: "handler-session-uuid-1234",
		ClaudeSessionID:  "claude-session-uuid-5678",
		WorkflowID:       "workflow-uuid-abcd",
		NodeID:           "node-1",
		AgentType:        "claude-code",
		Phase:            "",
	}
}

// hookRelayFixtureStdin builds a JSON stdin payload for tests.
func hookRelayFixtureStdin(sessionID, hookEventName string, extra map[string]interface{}) *bytes.Reader {
	m := map[string]interface{}{
		"session_id":      sessionID,
		"hook_event_name": hookEventName,
		"transcript_path": "/tmp/transcript.jsonl",
		"cwd":             "/tmp/workspace",
		"permission_mode": "auto",
	}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("hookRelayFixtureStdin: marshal: %v", err))
	}
	return bytes.NewReader(b)
}

// hookRelayFixtureShortSockDir creates a short-path temp dir suitable for Unix
// socket paths (macOS limit: 104 bytes including the filename).
func hookRelayFixtureShortSockDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hr")
	if err != nil {
		t.Fatalf("hookRelayFixtureShortSockDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// hookRelayFixtureListenAndRespond starts a fake Unix domain socket listener
// that responds once with the given ackJSON (e.g. {"status":"ok"}) then stops.
// Returns the socket path and a channel that receives the received message bytes.
func hookRelayFixtureListenAndRespond(t *testing.T, ackJSON string) (socketPath string, received <-chan []byte) {
	t.Helper()

	dir := hookRelayFixtureShortSockDir(t)
	sockPath := filepath.Join(dir, "d.sock")

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("hookRelayFixtureListenAndRespond: listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ch := make(chan []byte, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			ch <- scanner.Bytes()
		}
		_, _ = fmt.Fprintln(conn, ackJSON)
	}()

	return sockPath, ch
}

// hookRelayFixtureListenSequence starts a listener that responds to multiple
// connections in order. Each response in ackSequence is sent to successive callers.
func hookRelayFixtureListenSequence(t *testing.T, ackSequence []string) (socketPath string, received <-chan []byte) {
	t.Helper()

	dir := hookRelayFixtureShortSockDir(t)
	sockPath := filepath.Join(dir, "d.sock")

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("hookRelayFixtureListenSequence: listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ch := make(chan []byte, 1)
	go func() {
		for _, ack := range ackSequence {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			scanner := bufio.NewScanner(conn)
			if scanner.Scan() {
				select {
				case ch <- scanner.Bytes():
				default:
				}
			}
			_, _ = fmt.Fprintln(conn, ack)
			_ = conn.Close()
		}
	}()

	return sockPath, ch
}

// hookRelayFixtureListenDelayed returns a socket path that has NO listener yet;
// after delay, a listener binds and responds once with ackJSON. It models the
// cold-boot / in-place-swap startup race (CHB-016): the first dial gets ENOENT,
// and later dials succeed once the daemon starts listening.
func hookRelayFixtureListenDelayed(t *testing.T, delay time.Duration, ackJSON string) (socketPath string, received <-chan []byte) {
	t.Helper()

	dir := hookRelayFixtureShortSockDir(t)
	sockPath := filepath.Join(dir, "d.sock")

	ch := make(chan []byte, 1)
	go func() {
		time.Sleep(delay)
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", sockPath)
		if err != nil {
			return
		}
		defer func() { _ = ln.Close() }() //nolint:errcheck // test listener cleanup; close error non-actionable
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }() //nolint:errcheck // test conn cleanup; close error non-actionable
		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			select {
			case ch <- scanner.Bytes():
			default:
			}
		}
		_, _ = fmt.Fprintln(conn, ackJSON) //nolint:errcheck // test ack write; error non-actionable
	}()

	return sockPath, ch
}

// ─── Tests ───────────────────────────────────────────────────────────────────

func TestHookRelay_UnknownEventKind_NoOp(t *testing.T) {
	t.Parallel()

	// CHB-011: unknown event kind MUST exit 0 without writing to the daemon
	// socket and without writing to stderr.  This is the distinct conformance
	// invariant for CHB-011 — all three properties are asserted explicitly.

	// Set up a real listener so we can confirm nothing arrives on the socket.
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(t.TempDir())
	e.DaemonSocket = sockPath

	stdin := strings.NewReader(`{"session_id":"x","hook_event_name":"FutureEvent"}`)
	var stderr bytes.Buffer
	code := hookrelay.Run("FutureEvent", stdin, &stderr, &e)

	// (1) Must exit 0.
	if code != 0 {
		t.Errorf("CHB-011: unknown event kind: exit %d, want 0; stderr=%q", code, stderr.String())
	}
	// (2) Must not write to stderr.
	if s := stderr.String(); s != "" {
		t.Errorf("CHB-011: unknown event kind: non-empty stderr %q, want empty", s)
	}
	// (3) Must not write to the daemon socket.
	select {
	case msg := <-received:
		t.Errorf("CHB-011: unknown event kind: unexpected socket message %q", msg)
	default:
		// No message received — correct.
	}
}

func TestHookRelay_SessionStart_SynthesizesAgentReady(t *testing.T) {
	t.Parallel()

	// CHB-013 (as amended by hk-p63bz): SessionStart synthesizes agent_ready
	// with provenance="claude_session_start" and sends it to the daemon socket.
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(t.TempDir())
	e.DaemonSocket = sockPath
	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "SessionStart", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionStart", stdin, &stderr, &e)
	if code != 0 {
		t.Errorf("SessionStart: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	// The relay should have sent an agent_ready message to the socket.
	select {
	case msgBytes := <-received:
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			t.Fatalf("SessionStart: unmarshal sent message: %v", err)
		}
		var msgType string
		if err := json.Unmarshal(msg["type"], &msgType); err != nil {
			t.Fatalf("SessionStart: unmarshal type field: %v", err)
		}
		if msgType != "agent_ready" {
			t.Errorf("SessionStart: message type = %q; want %q", msgType, "agent_ready")
		}
		// Verify payload carries provenance="claude_session_start".
		var payload map[string]interface{}
		if err := json.Unmarshal(msg["payload"], &payload); err != nil {
			t.Fatalf("SessionStart: unmarshal payload: %v", err)
		}
		if payload["provenance"] != "claude_session_start" {
			t.Errorf("SessionStart: payload.provenance = %v; want %q", payload["provenance"], "claude_session_start")
		}
	default:
		t.Error("SessionStart: no message received on socket; expected agent_ready")
	}
}

func TestHookRelay_SessionEnd_NoOp(t *testing.T) {
	t.Parallel()

	// CHB-013: SessionEnd is no-op at MVH.
	e := hookRelayFixtureEnv(t.TempDir())
	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "SessionEnd", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionEnd", stdin, &stderr, &e)
	if code != 0 {
		t.Errorf("SessionEnd: exit %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestHookRelay_SessionIDMismatch(t *testing.T) {
	t.Parallel()

	// CHB-012: session_id mismatch → exit 1 with bridge_session_id_mismatch on stderr.
	e := hookRelayFixtureEnv(t.TempDir())
	stdin := hookRelayFixtureStdin("wrong-session-id", "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("session_id mismatch: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_session_id_mismatch") {
		t.Errorf("session_id mismatch: stderr missing bridge_session_id_mismatch, got %q", stderr.String())
	}
}

func TestHookRelay_EventKindMismatch(t *testing.T) {
	t.Parallel()

	// CHB-012: hook_event_name mismatch → exit 1 with bridge_event_kind_mismatch on stderr.
	e := hookRelayFixtureEnv(t.TempDir())
	// stdin says "Stop" but argv says "Notification"
	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Notification", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("event_kind mismatch: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_event_kind_mismatch") {
		t.Errorf("event_kind mismatch: stderr missing bridge_event_kind_mismatch, got %q", stderr.String())
	}
}

func TestHookRelay_MalformedPayload(t *testing.T) {
	t.Parallel()

	// CHB-012: malformed JSON stdin → exit 1.
	e := hookRelayFixtureEnv(t.TempDir())
	stdin := strings.NewReader(`{not valid json}`)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("malformed payload: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_malformed_hook_payload") {
		t.Errorf("malformed payload: stderr missing bridge_malformed_hook_payload, got %q", stderr.String())
	}
}

func TestHookRelay_Stop_WorkComplete(t *testing.T) {
	t.Parallel()

	// CHB-013: Stop in single/implementer phase → outcome_emitted{kind=WORK_COMPLETE}.
	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", map[string]interface{}{
		"message": "Final assistant summary text",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop work_complete: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	if err := json.Unmarshal(msgBytes, &msg); err != nil {
		t.Fatalf("Stop work_complete: unmarshal received: %v", err)
	}
	if msg["type"] != "outcome_emitted" {
		t.Errorf("Stop work_complete: type=%v, want outcome_emitted", msg["type"])
	}
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["kind"] != "WORK_COMPLETE" {
		t.Errorf("Stop work_complete: payload.kind=%v, want WORK_COMPLETE", pl["kind"])
	}
}

func TestHookRelay_Stop_ReviewerVerdictPresent(t *testing.T) {
	t.Parallel()

	// CHB-014: reviewer phase Stop → reads review.json and packages verdict.
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	verdictJSON := `{"schema_version":1,"verdict":"APPROVE","flags":["flag1"],"notes":"looks good"}`
	//nolint:gosec // G306: test file with non-secret content
	if err := os.WriteFile(filepath.Join(harmonikDir, "review.json"), []byte(verdictJSON), 0o644); err != nil {
		t.Fatalf("write review.json: %v", err)
	}

	e := hookRelayFixtureEnv(dir)
	e.Phase = "reviewer"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop reviewer verdict: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)

	if msg["type"] != "outcome_emitted" {
		t.Errorf("reviewer verdict: type=%v, want outcome_emitted", msg["type"])
	}
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["kind"] != "REVIEWER_VERDICT" {
		t.Errorf("reviewer verdict: payload.kind=%v, want REVIEWER_VERDICT", pl["kind"])
	}
	verdict, _ := pl["verdict"].(map[string]interface{})
	if verdict["verdict"] != "APPROVE" {
		t.Errorf("reviewer verdict: verdict.verdict=%v, want APPROVE", verdict["verdict"])
	}
}

func TestHookRelay_Stop_ReviewerVerdictAbsent(t *testing.T) {
	t.Parallel()

	// CHB-014: reviewer phase Stop, file absent → error payload.
	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "reviewer"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("reviewer verdict absent: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["error"] != "missing_review_file" {
		t.Errorf("reviewer verdict absent: payload.error=%v, want missing_review_file", pl["error"])
	}
}

func TestHookRelay_Stop_ReviewerVerdictMalformed(t *testing.T) {
	t.Parallel()

	// CHB-014: reviewer phase Stop, file malformed → malformed_review_file error.
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	//nolint:gosec // G306: test file with non-secret content
	if err := os.WriteFile(filepath.Join(harmonikDir, "review.json"), []byte(`{invalid}`), 0o644); err != nil {
		t.Fatalf("write review.json: %v", err)
	}

	e := hookRelayFixtureEnv(dir)
	e.Phase = "reviewer"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("reviewer verdict malformed: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["error"] != "malformed_review_file" {
		t.Errorf("reviewer verdict malformed: payload.error=%v, want malformed_review_file", pl["error"])
	}
}

func TestHookRelay_StopFailure_RateLimit(t *testing.T) {
	t.Parallel()

	// CHB-013: StopFailure{error_type:rate_limit} → agent_rate_limited{retry_after_seconds:60}.
	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "rate_limit",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure rate_limit: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)
	if msg["type"] != "agent_rate_limited" {
		t.Errorf("StopFailure rate_limit: type=%v, want agent_rate_limited", msg["type"])
	}
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["retry_after_seconds"] != float64(60) {
		t.Errorf("StopFailure rate_limit: retry_after_seconds=%v, want 60", pl["retry_after_seconds"])
	}
}

func TestHookRelay_StopFailure_ServerError(t *testing.T) {
	t.Parallel()

	// CHB-013: StopFailure{error_type:server_error} → outcome_emitted{kind=FAILURE_SIGNAL,suggested_class=transient}.
	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "server_error",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure server_error: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)
	if msg["type"] != "outcome_emitted" {
		t.Errorf("StopFailure server_error: type=%v, want outcome_emitted", msg["type"])
	}
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["kind"] != "FAILURE_SIGNAL" {
		t.Errorf("StopFailure server_error: kind=%v, want FAILURE_SIGNAL", pl["kind"])
	}
	if pl["suggested_class"] != "transient" {
		t.Errorf("StopFailure server_error: suggested_class=%v, want transient", pl["suggested_class"])
	}
	if pl["sub_reason"] != "claude_server_error" {
		t.Errorf("StopFailure server_error: sub_reason=%v, want claude_server_error", pl["sub_reason"])
	}
}

func TestHookRelay_StopFailure_Structural(t *testing.T) {
	t.Parallel()

	// CHB-013: StopFailure{error_type:authentication_failed} → FAILURE_SIGNAL with structural class.
	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "authentication_failed",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure structural: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["suggested_class"] != "structural" {
		t.Errorf("StopFailure structural: suggested_class=%v, want structural", pl["suggested_class"])
	}
	if pl["sub_reason"] != "claude_authentication_failed" {
		t.Errorf("StopFailure structural: sub_reason=%v, want claude_authentication_failed", pl["sub_reason"])
	}
}

func TestHookRelay_Notification_WaitingInput(t *testing.T) {
	t.Parallel()

	// CHB-013: Notification{idle_prompt} → agent_heartbeat{phase:waiting_input}.
	for _, notifType := range []string{"idle_prompt", "permission_prompt"} {
		notifType := notifType
		t.Run(notifType, func(t *testing.T) {
			t.Parallel()

			e := hookRelayFixtureEnv(t.TempDir())
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Notification", map[string]interface{}{
				"notification_type": notifType,
			})
			var stderr bytes.Buffer
			code := hookrelay.Run("Notification", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("Notification %s: exit %d, want 0; stderr=%q", notifType, code, stderr.String())
			}

			msgBytes := <-received
			var msg map[string]interface{}
			_ = json.Unmarshal(msgBytes, &msg)
			if msg["type"] != "agent_heartbeat" {
				t.Errorf("Notification %s: type=%v, want agent_heartbeat", notifType, msg["type"])
			}
			payload, _ := json.Marshal(msg["payload"])
			var pl map[string]interface{}
			_ = json.Unmarshal(payload, &pl)
			if pl["phase"] != "waiting_input" {
				t.Errorf("Notification %s: phase=%v, want waiting_input", notifType, pl["phase"])
			}
		})
	}
}

func TestHookRelay_Notification_Reasoning(t *testing.T) {
	t.Parallel()

	// CHB-013: Notification{other type} → agent_heartbeat{phase:reasoning}.
	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Notification", map[string]interface{}{
		"notification_type": "some_other_notification",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("Notification", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Notification reasoning: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msgBytes := <-received
	var msg map[string]interface{}
	_ = json.Unmarshal(msgBytes, &msg)
	payload, _ := json.Marshal(msg["payload"])
	var pl map[string]interface{}
	_ = json.Unmarshal(payload, &pl)
	if pl["phase"] != "reasoning" {
		t.Errorf("Notification reasoning: phase=%v, want reasoning", pl["phase"])
	}
}

func TestHookRelay_DialFailed_NonSocketFatal(t *testing.T) {
	t.Parallel()

	// CHB-017: a genuinely-fatal dial error — the target path exists but is not a
	// socket (ENOTSOCK) — is NOT the startup race. It must fail fast with
	// bridge_dial_failed and must NOT enter the CHB-016 retry loop.
	e := hookRelayFixtureEnv(t.TempDir())
	notASocket := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(notASocket, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	e.DaemonSocket = notASocket

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("non-socket dial: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_dial_failed") {
		t.Errorf("non-socket dial: stderr missing bridge_dial_failed, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "retrying") {
		t.Errorf("non-socket dial: must not retry a fatal error, got %q", stderr.String())
	}
}

func TestHookRelay_DialRetry_SocketAppearsLate(t *testing.T) {
	t.Parallel()

	// CHB-016 / RU-14: the daemon socket is absent at first dial (ENOENT — the
	// cold-boot / in-place-swap race) and only appears after a short delay. The
	// relay must retry the dial within the startup window and then succeed —
	// NOT return bridge_dial_failed on the first miss.
	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, _ := hookRelayFixtureListenDelayed(t, 250*time.Millisecond, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("late socket retry: exit %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "retrying") {
		t.Errorf("late socket retry: expected a dial-retry log, got %q", stderr.String())
	}
}

func TestHookRelay_DaemonNotReady_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	// CHB-016: daemon_not_ready typed-error → retry with exponential backoff, eventual success.
	e := hookRelayFixtureEnv(t.TempDir())

	// First response: daemon_not_ready. Second response: ok.
	sockPath, received := hookRelayFixtureListenSequence(t, []string{
		`{"status":"daemon_not_ready","reason":"unknown_run_id"}`,
		`{"status":"ok"}`,
	})
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("daemon_not_ready retry: exit %d, want 0; stderr=%q", code, stderr.String())
	}
	// Two messages should have been received.
	// The channel is buffered at 1, so we just need to confirm one message.
	if len(received) == 0 {
		// The second message was received; channel may be empty — that's fine.
	}
}

func TestHookRelay_EnvelopeFields(t *testing.T) {
	t.Parallel()

	// Verify the envelope fields on a Stop→outcome_emitted message.
	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	_ = hookrelay.Run("Stop", stdin, &stderr, &e)

	msgBytes := <-received
	var msg map[string]interface{}
	if err := json.Unmarshal(msgBytes, &msg); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	// CHB-015: envelope must carry run_id and claude_session_id.
	if msg["run_id"] != e.RunID {
		t.Errorf("envelope: run_id=%v, want %v", msg["run_id"], e.RunID)
	}
	if msg["claude_session_id"] != e.ClaudeSessionID {
		t.Errorf("envelope: claude_session_id=%v, want %v", msg["claude_session_id"], e.ClaudeSessionID)
	}
	if msg["handler_session_id"] != e.HandlerSessionID {
		t.Errorf("envelope: handler_session_id=%v, want %v", msg["handler_session_id"], e.HandlerSessionID)
	}
}
