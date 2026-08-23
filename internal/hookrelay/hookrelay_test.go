package hookrelay_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

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

func hookRelayFixtureJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("hookRelayFixtureJSON: marshal %T: %v", v, err)
	}
	return b
}

func hookRelayFixtureStdin(t *testing.T, sessionID, hookEventName string, extra map[string]interface{}) *bytes.Reader {
	t.Helper()
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
	return bytes.NewReader(hookRelayFixtureJSON(t, m))
}

func hookRelayFixtureEnvelope(t *testing.T, what string, msgBytes []byte) (envelope map[string]json.RawMessage, payload map[string]interface{}) {
	t.Helper()
	if err := json.Unmarshal(msgBytes, &envelope); err != nil {
		t.Fatalf("%s: unmarshal envelope %q: %v", what, msgBytes, err)
	}
	raw, ok := envelope["payload"]
	if !ok {
		t.Fatalf("%s: envelope has no payload field: %q", what, msgBytes)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s: unmarshal payload %q: %v", what, raw, err)
	}
	return envelope, payload
}

func hookRelayFixtureString(t *testing.T, what string, envelope map[string]json.RawMessage, field string) string {
	t.Helper()
	raw, ok := envelope[field]
	if !ok {
		t.Fatalf("%s: envelope has no %s field", what, field)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s: unmarshal %s field %q: %v", what, field, raw, err)
	}
	return s
}

func hookRelayFixtureShortSockDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hr")
	if err != nil {
		t.Fatalf("hookRelayFixtureShortSockDir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("hookRelayFixtureShortSockDir: remove %s: %v", dir, err)
		}
	})
	return dir
}

func hookRelayFixtureExchange(conn net.Conn, ackJSON string, ch chan<- []byte) error {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	if scanner.Scan() {
		select {
		case ch <- bytes.Clone(scanner.Bytes()):
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read request line: %w", err)
	}
	if _, err := fmt.Fprintln(conn, ackJSON); err != nil {
		return fmt.Errorf("write ack %q: %w", ackJSON, err)
	}
	return nil
}

func hookRelayFixtureServe(ln net.Listener, ackSequence []string, ch chan<- []byte) error {
	errs := make([]error, 0, 2*len(ackSequence))
	for _, ack := range ackSequence {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			if !errors.Is(acceptErr, net.ErrClosed) {
				errs = append(errs, fmt.Errorf("accept: %w", acceptErr))
			}
			break
		}
		errs = append(errs, hookRelayFixtureExchange(conn, ack, ch), conn.Close())
	}
	return errors.Join(errs...)
}

func hookRelayFixtureWatch(t *testing.T, what string, ln net.Listener, serve func() error) {
	t.Helper()

	errCh := make(chan error, 1)
	go func() { errCh <- serve() }()

	t.Cleanup(func() {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("%s: close listener: %v", what, err)
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("%s: fixture server: %v", what, err)
			}
		case <-time.After(2 * time.Second):
		}
	})
}

func hookRelayFixtureListenAndRespond(t *testing.T, ackJSON string) (socketPath string, received <-chan []byte) {
	t.Helper()
	return hookRelayFixtureListenSequence(t, []string{ackJSON})
}

func hookRelayFixtureListenSequence(t *testing.T, ackSequence []string) (socketPath string, received <-chan []byte) {
	t.Helper()

	dir := hookRelayFixtureShortSockDir(t)
	sockPath := filepath.Join(dir, "d.sock")

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("hookRelayFixtureListenSequence: listen: %v", err)
	}

	ch := make(chan []byte, 1)
	hookRelayFixtureWatch(t, "hookRelayFixtureListenSequence", ln, func() error {
		return hookRelayFixtureServe(ln, ackSequence, ch)
	})

	return sockPath, ch
}

func hookRelayFixtureListenDelayed(t *testing.T, delay time.Duration, ackJSON string) (socketPath string, received <-chan []byte) {
	t.Helper()

	dir := hookRelayFixtureShortSockDir(t)
	sockPath := filepath.Join(dir, "d.sock")

	ch := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		time.Sleep(delay)
		ln, listenErr := (&net.ListenConfig{}).Listen(context.Background(), "unix", sockPath)
		if listenErr != nil {
			errCh <- fmt.Errorf("delayed listen on %s: %w", sockPath, listenErr)
			return
		}
		errCh <- errors.Join(hookRelayFixtureServe(ln, []string{ackJSON}, ch), ln.Close())
	}()

	t.Cleanup(func() {
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("hookRelayFixtureListenDelayed: fixture server: %v", err)
			}
		case <-time.After(2 * time.Second):
		}
	})

	return sockPath, ch
}

func TestHookRelay_UnknownEventKind_NoOp(t *testing.T) {
	t.Parallel()

	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(t.TempDir())
	e.DaemonSocket = sockPath

	stdin := strings.NewReader(`{"session_id":"x","hook_event_name":"FutureEvent"}`)
	var stderr bytes.Buffer
	code := hookrelay.Run("FutureEvent", stdin, &stderr, &e)

	if code != 0 {
		t.Errorf("CHB-011: unknown event kind: exit %d, want 0; stderr=%q", code, stderr.String())
	}
	if s := stderr.String(); s != "" {
		t.Errorf("CHB-011: unknown event kind: non-empty stderr %q, want empty", s)
	}
	select {
	case msg := <-received:
		t.Errorf("CHB-011: unknown event kind: unexpected socket message %q", msg)
	default:
	}
}

func TestHookRelay_SessionStart_SynthesizesAgentReady(t *testing.T) {
	t.Parallel()

	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(t.TempDir())
	e.DaemonSocket = sockPath
	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "SessionStart", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionStart", stdin, &stderr, &e)
	if code != 0 {
		t.Errorf("SessionStart: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		msg, payload := hookRelayFixtureEnvelope(t, "SessionStart", msgBytes)
		if msgType := hookRelayFixtureString(t, "SessionStart", msg, "type"); msgType != "agent_ready" {
			t.Errorf("SessionStart: message type = %q; want %q", msgType, "agent_ready")
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

	e := hookRelayFixtureEnv(t.TempDir())
	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "SessionEnd", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionEnd", stdin, &stderr, &e)
	if code != 0 {
		t.Errorf("SessionEnd: exit %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestHookRelay_SessionIDMismatch(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	stdin := hookRelayFixtureStdin(t, "wrong-session-id", "Stop", nil)
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

	e := hookRelayFixtureEnv(t.TempDir())
	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
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

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", map[string]interface{}{
		"message": "Final assistant summary text",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop work_complete: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	env, pl := hookRelayFixtureEnvelope(t, "Stop work_complete", <-received)
	if got := hookRelayFixtureString(t, "Stop work_complete", env, "type"); got != "outcome_emitted" {
		t.Errorf("Stop work_complete: type=%v, want outcome_emitted", got)
	}
	if pl["kind"] != "WORK_COMPLETE" {
		t.Errorf("Stop work_complete: payload.kind=%v, want WORK_COMPLETE", pl["kind"])
	}
}

func TestHookRelay_Stop_ReviewerVerdictPresent(t *testing.T) {
	t.Parallel()

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

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop reviewer verdict: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	env, pl := hookRelayFixtureEnvelope(t, "reviewer verdict", <-received)
	if got := hookRelayFixtureString(t, "reviewer verdict", env, "type"); got != "outcome_emitted" {
		t.Errorf("reviewer verdict: type=%v, want outcome_emitted", got)
	}
	if pl["kind"] != "REVIEWER_VERDICT" {
		t.Errorf("reviewer verdict: payload.kind=%v, want REVIEWER_VERDICT", pl["kind"])
	}
	verdict, ok := pl["verdict"].(map[string]interface{})
	if !ok {
		t.Fatalf("reviewer verdict: payload.verdict is %T, want an object; payload=%v", pl["verdict"], pl)
	}
	if verdict["verdict"] != "APPROVE" {
		t.Errorf("reviewer verdict: verdict.verdict=%v, want APPROVE", verdict["verdict"])
	}
}

func TestHookRelay_Stop_ReviewerVerdictAbsent(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "reviewer"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("reviewer verdict absent: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	_, pl := hookRelayFixtureEnvelope(t, "reviewer verdict absent", <-received)
	if pl["error"] != "missing_review_file" {
		t.Errorf("reviewer verdict absent: payload.error=%v, want missing_review_file", pl["error"])
	}
}

func TestHookRelay_Stop_ReviewerVerdictMalformed(t *testing.T) {
	t.Parallel()

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

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("reviewer verdict malformed: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	_, pl := hookRelayFixtureEnvelope(t, "reviewer verdict malformed", <-received)
	if pl["error"] != "malformed_review_file" {
		t.Errorf("reviewer verdict malformed: payload.error=%v, want malformed_review_file", pl["error"])
	}
}

func TestHookRelay_StopFailure_RateLimit(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "rate_limit",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure rate_limit: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	env, pl := hookRelayFixtureEnvelope(t, "StopFailure rate_limit", <-received)
	if got := hookRelayFixtureString(t, "StopFailure rate_limit", env, "type"); got != "agent_rate_limited" {
		t.Errorf("StopFailure rate_limit: type=%v, want agent_rate_limited", got)
	}
	if pl["retry_after_seconds"] != float64(60) {
		t.Errorf("StopFailure rate_limit: retry_after_seconds=%v, want 60", pl["retry_after_seconds"])
	}
}

func TestHookRelay_StopFailure_ServerError(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "server_error",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure server_error: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	env, pl := hookRelayFixtureEnvelope(t, "StopFailure server_error", <-received)
	if got := hookRelayFixtureString(t, "StopFailure server_error", env, "type"); got != "outcome_emitted" {
		t.Errorf("StopFailure server_error: type=%v, want outcome_emitted", got)
	}
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

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "authentication_failed",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure structural: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	_, pl := hookRelayFixtureEnvelope(t, "StopFailure structural", <-received)
	if pl["suggested_class"] != "structural" {
		t.Errorf("StopFailure structural: suggested_class=%v, want structural", pl["suggested_class"])
	}
	if pl["sub_reason"] != "claude_authentication_failed" {
		t.Errorf("StopFailure structural: sub_reason=%v, want claude_authentication_failed", pl["sub_reason"])
	}
}

func TestHookRelay_Notification_WaitingInput(t *testing.T) {
	t.Parallel()

	for _, notifType := range []string{"idle_prompt", "permission_prompt"} {
		t.Run(notifType, func(t *testing.T) {
			t.Parallel()

			e := hookRelayFixtureEnv(t.TempDir())
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Notification", map[string]interface{}{
				"notification_type": notifType,
			})
			var stderr bytes.Buffer
			code := hookrelay.Run("Notification", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("Notification %s: exit %d, want 0; stderr=%q", notifType, code, stderr.String())
			}

			what := "Notification " + notifType
			env, pl := hookRelayFixtureEnvelope(t, what, <-received)
			if got := hookRelayFixtureString(t, what, env, "type"); got != "agent_heartbeat" {
				t.Errorf("Notification %s: type=%v, want agent_heartbeat", notifType, got)
			}
			if pl["phase"] != "waiting_input" {
				t.Errorf("Notification %s: phase=%v, want waiting_input", notifType, pl["phase"])
			}
		})
	}
}

func TestHookRelay_Notification_Reasoning(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Notification", map[string]interface{}{
		"notification_type": "some_other_notification",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("Notification", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Notification reasoning: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	_, pl := hookRelayFixtureEnvelope(t, "Notification reasoning", <-received)
	if pl["phase"] != "reasoning" {
		t.Errorf("Notification reasoning: phase=%v, want reasoning", pl["phase"])
	}
}

func TestHookRelay_DialFailed_NonSocketFatal(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	notASocket := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(notASocket, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	e.DaemonSocket = notASocket

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
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

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, _ := hookRelayFixtureListenDelayed(t, 250*time.Millisecond, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
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

	e := hookRelayFixtureEnv(t.TempDir())

	sockPath, _ := hookRelayFixtureListenSequence(t, []string{
		`{"status":"daemon_not_ready","reason":"unknown_run_id"}`,
		`{"status":"ok"}`,
	})
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("daemon_not_ready retry: exit %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestHookRelay_EnvelopeFields(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	if code := hookrelay.Run("Stop", stdin, &stderr, &e); code != 0 {
		t.Fatalf("envelope: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	msg, _ := hookRelayFixtureEnvelope(t, "envelope", <-received)

	for _, tc := range []struct{ field, want string }{
		{"run_id", e.RunID},
		{"claude_session_id", e.ClaudeSessionID},
		{"handler_session_id", e.HandlerSessionID},
	} {
		if got := hookRelayFixtureString(t, "envelope", msg, tc.field); got != tc.want {
			t.Errorf("envelope: %s=%v, want %v", tc.field, got, tc.want)
		}
	}
}

func hookRelayFixtureListenRestart(t *testing.T, ackJSON string) (socketPath string, received <-chan []byte) {
	t.Helper()

	dir := hookRelayFixtureShortSockDir(t)
	sockPath := filepath.Join(dir, "d.sock")

	ch := make(chan []byte, 1)
	errCh := make(chan error, 1)

	first, listenErr := (&net.ListenConfig{}).Listen(context.Background(), "unix", sockPath)
	if listenErr != nil {
		t.Fatalf("hookRelayFixtureListenRestart: first listen on %s: %v", sockPath, listenErr)
	}

	go func() {
		errs := make([]error, 0, 4)
		conn, acceptErr := first.Accept()
		if acceptErr != nil {
			errCh <- fmt.Errorf("first accept: %w", acceptErr)
			return
		}
		if _, readErr := bufio.NewReader(conn).ReadString('\n'); readErr != nil {
			errs = append(errs, fmt.Errorf("first read: %w", readErr))
		}
		errs = append(errs, conn.Close(), first.Close())

		second, secondErr := (&net.ListenConfig{}).Listen(context.Background(), "unix", sockPath)
		if secondErr != nil {
			errCh <- errors.Join(append(errs, fmt.Errorf("restart listen: %w", secondErr))...)
			return
		}
		errs = append(errs, hookRelayFixtureServe(second, []string{ackJSON}, ch), second.Close())
		errCh <- errors.Join(errs...)
	}()

	t.Cleanup(func() {
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("hookRelayFixtureListenRestart: fixture server: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("hookRelayFixtureListenRestart: fixture server did not finish")
		}
	})

	return sockPath, ch
}

func TestHookRelay_Reconnects_WhenDaemonDiesBeforeAck(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenRestart(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("daemon died before ACK: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msg := <-received:
		if len(msg) == 0 {
			t.Fatal("daemon died before ACK: restarted daemon received an empty envelope")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon died before ACK: restarted daemon received no envelope")
	}

	if strings.Contains(stderr.String(), "bridge_dial_failed") {
		t.Errorf("daemon died before ACK: mid-exchange drop reported as a dial failure: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "reconnecting") {
		t.Errorf("daemon died before ACK: expected a reconnect diagnostic, got %q", stderr.String())
	}
}
