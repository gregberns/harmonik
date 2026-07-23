package hookrelay_test

// hookrelay_coverage_hkj3hrn_test.go — targeted coverage uplift for
// extractFinalMessage, truncate4KiB, sendToSocket error paths, and envFromOS.
//
// Bead: hk-j3hrn (core coverage uplift EPIC, step a).
// Spec: specs/claude-hook-bridge.md §4.5 CHB-013, §4.6 CHB-015..017.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

// ─── extractFinalMessage / truncate4KiB ─────────────────────────────────────
// These are exercised indirectly via Run(Stop) with a specific message payload.

// TestHookRelay_Stop_MessageObjectContent exercises extractFinalMessage's
// structured-object path: {"content": "<text>"} in the Stop message field.
//
// Spec ref: claude-hook-bridge.md §4.5 CHB-013 — Stop synthesizes
// outcome_emitted{kind=WORK_COMPLETE,summary=<final_message_text>}.
func TestHookRelay_Stop_MessageObjectContent(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	// Build a Stop payload whose "message" field is a structured object with
	// a "content" key — the second unmarshal branch in extractFinalMessage.
	msgObj := map[string]string{"content": "structured content text"}
	msgBytes := hookRelayFixtureJSON(t, msgObj)

	rawMsg := map[string]interface{}{
		"session_id":      e.ClaudeSessionID,
		"hook_event_name": "Stop",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
		"message":         json.RawMessage(msgBytes),
	}
	stdinBytes := hookRelayFixtureJSON(t, rawMsg)

	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(stdinBytes), &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop message-object-content: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case recv := <-received:
		_, pl := hookRelayFixtureEnvelope(t, "Stop message-object-content", recv)
		if pl["summary"] != "structured content text" {
			t.Errorf("Stop message-object-content: summary=%v, want %q", pl["summary"], "structured content text")
		}
	default:
		t.Error("Stop message-object-content: no message received on socket")
	}
}

// TestHookRelay_Stop_MessageObjectNoContent exercises extractFinalMessage when
// the structured object has no "content" key — falls through to return "".
func TestHookRelay_Stop_MessageObjectNoContent(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	// Object with keys other than "content" — should return empty summary.
	msgObj := map[string]string{"role": "assistant", "text": "no content key"}
	msgBytes := hookRelayFixtureJSON(t, msgObj)

	rawMsg := map[string]interface{}{
		"session_id":      e.ClaudeSessionID,
		"hook_event_name": "Stop",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
		"message":         json.RawMessage(msgBytes),
	}
	stdinBytes := hookRelayFixtureJSON(t, rawMsg)

	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(stdinBytes), &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop message-object-no-content: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case recv := <-received:
		_, pl := hookRelayFixtureEnvelope(t, "Stop message-object-no-content", recv)
		// An object with no "content" key yields no extractable text, so the
		// relay must emit WORK_COMPLETE with an empty summary rather than
		// inventing one from another field.
		if pl["kind"] != "WORK_COMPLETE" {
			t.Errorf("Stop message-object-no-content: kind=%v, want WORK_COMPLETE", pl["kind"])
		}
		if summary := pl["summary"]; summary != "" {
			t.Errorf("Stop message-object-no-content: summary=%v, want empty", summary)
		}
	default:
		t.Error("Stop message-object-no-content: no message received on socket")
	}
}

// TestHookRelay_Stop_MessageLargeStringTruncated exercises truncate4KiB with a
// string longer than 4096 bytes — should produce a WORK_COMPLETE without error.
//
// Spec ref: claude-hook-bridge.md §4.5 CHB-013 — final message truncated to 4 KiB.
func TestHookRelay_Stop_MessageLargeStringTruncated(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	// Build a string well over 4 KiB.
	longText := strings.Repeat("x", 8192)
	longJSON := hookRelayFixtureJSON(t, longText)

	rawMsg := map[string]interface{}{
		"session_id":      e.ClaudeSessionID,
		"hook_event_name": "Stop",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
		"message":         json.RawMessage(longJSON),
	}
	stdinBytes := hookRelayFixtureJSON(t, rawMsg)

	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(stdinBytes), &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop large-message truncate: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case recv := <-received:
		_, pl := hookRelayFixtureEnvelope(t, "Stop large-message", recv)
		summary, ok := pl["summary"].(string)
		if !ok {
			t.Fatalf("Stop large-message: summary is %T, want string; payload=%v", pl["summary"], pl)
		}
		if len(summary) > 4096 {
			t.Errorf("Stop large-message: summary length %d exceeds 4 KiB limit 4096", len(summary))
		}
		if summary == "" {
			t.Error("Stop large-message: summary is empty; expected truncated text")
		}
	default:
		t.Error("Stop large-message: no message received on socket")
	}
}

// TestHookRelay_Stop_MessageNilRawEmpty exercises extractFinalMessage with a nil/
// empty raw message (no "message" field in the Stop payload).
func TestHookRelay_Stop_MessageNilRawEmpty(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	// No "message" key — should produce empty summary without error.
	rawMsg := map[string]interface{}{
		"session_id":      e.ClaudeSessionID,
		"hook_event_name": "Stop",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
		// message field intentionally absent
	}
	stdinBytes := hookRelayFixtureJSON(t, rawMsg)

	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(stdinBytes), &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop nil-message: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case recv := <-received:
		msg, _ := hookRelayFixtureEnvelope(t, "Stop nil-message", recv)
		if got := hookRelayFixtureString(t, "Stop nil-message", msg, "type"); got != "outcome_emitted" {
			t.Errorf("Stop nil-message: type=%q, want outcome_emitted", got)
		}
	default:
		t.Error("Stop nil-message: no message received on socket")
	}
}

// ─── sendToSocket error paths ────────────────────────────────────────────────

// TestHookRelay_SendToSocket_DaemonRejectsMessage exercises the non-ok,
// non-daemon_not_ready ack path in sendToSocket — triggers the
// "bridge_dial_failed: daemon rejected message" error.
//
// Spec ref: claude-hook-bridge.md §4.6 CHB-017.
func TestHookRelay_SendToSocket_DaemonRejectsMessage(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"

	// Respond with an unrecoverable non-ok status (not daemon_not_ready).
	sockPath, _ := hookRelayFixtureListenAndRespond(t, `{"status":"bad_envelope","reason":"unknown_run_id"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("daemon-rejected: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_dial_failed") {
		t.Errorf("daemon-rejected: stderr missing bridge_dial_failed, got %q", stderr.String())
	}
}

// TestHookRelay_SendToSocket_MalformedAckJSON exercises the malformed-ACK-JSON
// path in sendToSocket — daemon sends back non-JSON bytes.
func TestHookRelay_SendToSocket_MalformedAckJSON(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"

	// Respond with invalid JSON.
	sockPath, _ := hookRelayFixtureListenAndRespond(t, `not valid json at all`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("malformed-ack: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_dial_failed") {
		t.Errorf("malformed-ack: stderr missing bridge_dial_failed, got %q", stderr.String())
	}
}

// TestHookRelay_SendToSocket_MessageTooLarge exercises the 1-MiB NDJSON line
// limit in sendToSocket — should return exit 1 with bridge_malformed_hook_payload.
//
// The size guard checks the full JSON envelope (not just the message payload),
// so the reviewer verdict path is a good vehicle: we embed a >1 MiB notes field
// in the review.json verdict, which survives truncation and inflates the envelope.
//
// Spec ref: claude-hook-bridge.md §4.6 CHB-015 — message MUST NOT exceed 1 MiB.
func TestHookRelay_SendToSocket_MessageTooLarge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}

	// A >1 MiB notes field causes the JSON envelope to exceed the wire limit.
	bigNotes := strings.Repeat("x", 1<<20+1000)
	verdictJSON := fmt.Sprintf(`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":%q}`, bigNotes)
	//nolint:gosec // G306: test file with non-secret content
	if err := os.WriteFile(filepath.Join(harmonikDir, "review.json"), []byte(verdictJSON), 0o644); err != nil {
		t.Fatalf("write review.json: %v", err)
	}

	e := hookRelayFixtureEnv(dir)
	e.Phase = "reviewer"
	sockPath, _ := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("message-too-large: exit %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "bridge_malformed_hook_payload") {
		t.Errorf("message-too-large: stderr missing bridge_malformed_hook_payload, got %q", stderr.String())
	}
}

// TestHookRelay_SendToSocket_EmptyACKLine exercises the case where the daemon
// closes the connection without writing an ACK line — triggers the EOF/scan-fail path.
func TestHookRelay_SendToSocket_EmptyACKLine(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	e.Phase = "single"

	// Start a server that accepts the connection but closes without writing.
	sockPath := filepath.Join(hookRelayFixtureShortSockDir(t), "d.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	hookRelayFixtureWatch(t, "empty-ack", ln, func() error {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			if errors.Is(acceptErr, net.ErrClosed) {
				return nil // listener closed at teardown
			}
			return fmt.Errorf("accept: %w", acceptErr)
		}
		// Read the message bytes (drain connection) then close without ACK.
		buf := make([]byte, 1<<16)
		if _, readErr := conn.Read(buf); readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.Join(fmt.Errorf("drain connection: %w", readErr), conn.Close())
		}
		return conn.Close()
	})

	e.DaemonSocket = sockPath
	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 1 {
		t.Errorf("empty-ack: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_dial_failed") {
		t.Errorf("empty-ack: stderr missing bridge_dial_failed, got %q", stderr.String())
	}
}

// ─── envFromOS ───────────────────────────────────────────────────────────────

// TestHookRelay_EnvFromOS_MissingRequired exercises envFromOS via Run() with
// nil envOverride — when a required HARMONIK_* env var is absent, Run returns 1.
//
// Spec ref: claude-hook-bridge.md §4.1 CHB-006 — relay MUST read HARMONIK_* vars.
func TestHookRelay_EnvFromOS_MissingRequired(t *testing.T) {
	// Not t.Parallel() — manipulates process environment.

	// Clear all HARMONIK_* vars to ensure envFromOS fails on missing required.
	harmonikVars := []string{
		"HARMONIK_RUN_ID",
		"HARMONIK_DAEMON_SOCKET",
		"HARMONIK_WORKSPACE_PATH",
		"HARMONIK_HANDLER_SESSION_ID",
		"HARMONIK_CLAUDE_SESSION_ID",
		"HARMONIK_WORKFLOW_ID",
		"HARMONIK_NODE_ID",
		"HARMONIK_AGENT_TYPE",
		"HARMONIK_PHASE",
	}
	// envFromOS treats an empty value as absent (it only reads os.Getenv, which
	// cannot distinguish unset from set-to-empty), so blanking is equivalent to
	// unsetting here and t.Setenv restores the originals for us.
	for _, k := range harmonikVars {
		t.Setenv(k, "")
	}

	// nil envOverride forces envFromOS path.
	// When HARMONIK_RUN_ID is absent, hook-relay exits 0 silently (hk-f0xb6):
	// not a harmonik-managed session, so the hook is a no-op.
	stdin := `{"session_id":"","hook_event_name":"Stop"}`
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", strings.NewReader(stdin), &stderr, nil)
	if code != 0 {
		t.Errorf("envFromOS missing required: exit %d, want 0 (no-op outside harmonik); stderr=%q", code, stderr.String())
	}
}

// TestHookRelay_EnvFromOS_AllVarsPresent exercises envFromOS via Run() with
// nil envOverride when all required HARMONIK_* env vars are set.
// The test validates that run proceeds past envFromOS (session-id mismatch
// is the expected failure here since we don't match the env vars to stdin).
func TestHookRelay_EnvFromOS_AllVarsPresent(t *testing.T) {
	// Not t.Parallel() — manipulates process environment.

	const (
		testRunID      = "01HVTEST000000000000000099"
		testClauseSess = "test-claude-session-from-os"
		testHandlerSes = "test-handler-session-from-os"
		testWorkflowID = "test-workflow-from-os"
		testNodeID     = "node-os-1"
		testAgentType  = "claude-code"
	)

	tmpDir := t.TempDir()
	// t.Setenv restores every prior value at test end; HARMONIK_PHASE is blanked
	// rather than unset because envFromOS reads it through os.Getenv, for which
	// "absent" and "empty" are the same thing.
	for k, v := range map[string]string{
		"HARMONIK_RUN_ID":             testRunID,
		"HARMONIK_DAEMON_SOCKET":      filepath.Join(tmpDir, "d.sock"),
		"HARMONIK_WORKSPACE_PATH":     tmpDir,
		"HARMONIK_HANDLER_SESSION_ID": testHandlerSes,
		"HARMONIK_CLAUDE_SESSION_ID":  testClauseSess,
		"HARMONIK_WORKFLOW_ID":        testWorkflowID,
		"HARMONIK_NODE_ID":            testNodeID,
		"HARMONIK_AGENT_TYPE":         testAgentType,
		"HARMONIK_PHASE":              "",
	} {
		t.Setenv(k, v)
	}

	// Use wrong session_id to get a predictable session-mismatch exit 1 —
	// this proves envFromOS succeeded (otherwise we'd get a different error).
	// All CHB-012 required fields must be present so that the session_id mismatch
	// check (not the required-field check) is the first failure gate reached.
	stdin := strings.NewReader(`{"session_id":"wrong","hook_event_name":"Stop","transcript_path":"/tmp/t.jsonl"}`)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, nil)
	if code != 1 {
		t.Errorf("envFromOS all-present: exit %d, want 1 (session-mismatch); stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "bridge_session_id_mismatch") {
		t.Errorf("envFromOS all-present: expected bridge_session_id_mismatch (proving envFromOS succeeded), got %q", stderr.String())
	}
}

// TestHookRelay_ReviewerVerdictSchemaVersionNotOne exercises the schema_version!=1
// validation branch in buildStopMessage — produces malformed_review_file error.
//
// Spec ref: claude-hook-bridge.md §4.5 CHB-014.
func TestHookRelay_ReviewerVerdictSchemaVersionNotOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	// schema_version=2 is invalid per CHB-014.
	verdictJSON := `{"schema_version":2,"verdict":"APPROVE","flags":[],"notes":"bad version"}`
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
		t.Fatalf("reviewer bad schema_version: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case recv := <-received:
		_, pl := hookRelayFixtureEnvelope(t, "reviewer bad schema_version", recv)
		if pl["error"] != "malformed_review_file" {
			t.Errorf("reviewer bad schema_version: payload.error=%v, want malformed_review_file", pl["error"])
		}
	default:
		t.Error("reviewer bad schema_version: no message received")
	}
}

// TestHookRelay_ReviewerVerdictInvalidVerdictValue exercises the verdict-field
// validation in buildStopMessage — an unrecognized verdict string like "MAYBE".
//
// Spec ref: claude-hook-bridge.md §4.5 CHB-014 — verdict ∈ {APPROVE,REQUEST_CHANGES,BLOCK}.
func TestHookRelay_ReviewerVerdictInvalidVerdictValue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	verdictJSON := `{"schema_version":1,"verdict":"MAYBE","flags":[],"notes":"invalid verdict"}`
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
		t.Fatalf("reviewer invalid verdict value: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case recv := <-received:
		_, pl := hookRelayFixtureEnvelope(t, "reviewer invalid verdict value", recv)
		if pl["error"] != "malformed_review_file" {
			t.Errorf("reviewer invalid verdict value: payload.error=%v, want malformed_review_file", pl["error"])
		}
	default:
		t.Error("reviewer invalid verdict value: no message received")
	}
}
