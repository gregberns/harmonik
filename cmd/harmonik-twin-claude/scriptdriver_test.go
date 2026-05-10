package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test helpers use the per-bead prefix declared in implementer-protocol.md:
// twinScriptFixture (this bead: hk-ahvq.48.3).

// twinScriptFixtureWriteFile writes content to a temp file under t.TempDir()
// with the given filename and returns the absolute path.
func twinScriptFixtureWriteFile(t *testing.T, filename, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("twinScriptFixtureWriteFile: %v", err)
	}
	return p
}

// twinScriptFixtureDecodeAll splits buf into NDJSON lines and decodes all into
// a []map[string]any.  It calls t.Fatalf on any JSON error.
func twinScriptFixtureDecodeAll(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	raw := buf.String()
	if raw == "" {
		return nil
	}
	// Split on newline; last element is "" due to trailing \n.
	parts := bytes.Split(bytes.TrimRight([]byte(raw), "\n"), []byte("\n"))
	out := make([]map[string]any, 0, len(parts))
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(part, &m); err != nil {
			t.Fatalf("twinScriptFixtureDecodeAll: line %d unmarshal: %v — raw: %q", i, err, string(part))
		}
		out = append(out, m)
	}
	return out
}

// twinScriptFixtureEmitter returns a wireEmitter writing to a fresh bytes.Buffer.
func twinScriptFixtureEmitter(t *testing.T) (*wireEmitter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return newWireEmitter(&buf), &buf
}

// ────────────────────────────────────────────────────────────────────────────
// heartbeatMode.Valid
// ────────────────────────────────────────────────────────────────────────────

// TestHeartbeatModeValid verifies that Valid() accepts the two declared
// constants and rejects unknown values.
func TestHeartbeatModeValid(t *testing.T) {
	cases := []struct {
		hm     heartbeatMode
		wantOK bool
	}{
		{heartbeatModeWallClock, true},
		{heartbeatModeScripted, true},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.hm), func(t *testing.T) {
			if got := tc.hm.Valid(); got != tc.wantOK {
				t.Errorf("heartbeatMode(%q).Valid() = %v, want %v", tc.hm, got, tc.wantOK)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// loadScriptFile
// ────────────────────────────────────────────────────────────────────────────

// TestLoadScriptFileDefaults verifies that absent heartbeat_mode defaults to
// "wall_clock" and an empty messages list is valid.
func TestLoadScriptFileDefaults(t *testing.T) {
	p := twinScriptFixtureWriteFile(t, "script.yaml", "messages: []\n")

	sf, err := loadScriptFile(p)
	if err != nil {
		t.Fatalf("loadScriptFile: %v", err)
	}
	if sf.HeartbeatMode != heartbeatModeWallClock {
		t.Errorf("default heartbeat_mode = %q, want %q", sf.HeartbeatMode, heartbeatModeWallClock)
	}
	if len(sf.Messages) != 0 {
		t.Errorf("messages length = %d, want 0", len(sf.Messages))
	}
}

// TestLoadScriptFileScriptedMode verifies that heartbeat_mode: scripted is
// correctly parsed.
func TestLoadScriptFileScriptedMode(t *testing.T) {
	content := `
heartbeat_mode: scripted
messages:
  - type: agent_heartbeat
    payload:
      session_id: sess-001
      phase: starting
    relative_timestamp_ms: 100
`
	p := twinScriptFixtureWriteFile(t, "script.yaml", content)

	sf, err := loadScriptFile(p)
	if err != nil {
		t.Fatalf("loadScriptFile: %v", err)
	}
	if sf.HeartbeatMode != heartbeatModeScripted {
		t.Errorf("heartbeat_mode = %q, want %q", sf.HeartbeatMode, heartbeatModeScripted)
	}
	if len(sf.Messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(sf.Messages))
	}
	msg := sf.Messages[0]
	if msg.Type != "agent_heartbeat" {
		t.Errorf("message type = %q, want agent_heartbeat", msg.Type)
	}
	if msg.RelativeTimestampMs != 100 {
		t.Errorf("relative_timestamp_ms = %d, want 100", msg.RelativeTimestampMs)
	}
	if msg.Payload["session_id"] != "sess-001" {
		t.Errorf("payload session_id = %v, want sess-001", msg.Payload["session_id"])
	}
}

// TestLoadScriptFileMissingMessageType verifies that loadScriptFile returns an
// error when any ScriptMessage has a missing (absent) type field, satisfying
// the §10.2 HC-036a obligation "rejection of missing or empty type in any
// ScriptMessage".
func TestLoadScriptFileMissingMessageType(t *testing.T) {
	// A message with no type key at all (YAML omission → empty string in Go).
	content := `
messages:
  - payload:
      session_id: sess-001
`
	p := twinScriptFixtureWriteFile(t, "script.yaml", content)

	_, err := loadScriptFile(p)
	if err == nil {
		t.Fatal("loadScriptFile: expected error for message with missing type field, got nil")
	}
}

// TestLoadScriptFileEmptyMessageType verifies that loadScriptFile returns an
// error when any ScriptMessage has an explicitly empty type field, satisfying
// the §10.2 HC-036a obligation "rejection of missing or empty type in any
// ScriptMessage".
func TestLoadScriptFileEmptyMessageType(t *testing.T) {
	content := `
messages:
  - type: ""
    payload:
      session_id: sess-001
`
	p := twinScriptFixtureWriteFile(t, "script.yaml", content)

	_, err := loadScriptFile(p)
	if err == nil {
		t.Fatal("loadScriptFile: expected error for message with empty type field, got nil")
	}
}

// TestLoadScriptFileMultipleMessagesSecondEmpty verifies that the empty-type
// check fires on a non-first message (index > 0), not only on message 0.
func TestLoadScriptFileMultipleMessagesSecondEmpty(t *testing.T) {
	content := `
messages:
  - type: agent_started
  - type: ""
`
	p := twinScriptFixtureWriteFile(t, "script.yaml", content)

	_, err := loadScriptFile(p)
	if err == nil {
		t.Fatal("loadScriptFile: expected error for second message with empty type, got nil")
	}
}

// TestLoadScriptFileValidMessages verifies that a script where all messages
// have non-empty type fields loads without error (positive control for the
// empty-type validation path).
func TestLoadScriptFileValidMessages(t *testing.T) {
	content := `
messages:
  - type: agent_started
  - type: agent_heartbeat
    payload:
      session_id: sess-1
      phase: reasoning
  - type: outcome_emitted
`
	p := twinScriptFixtureWriteFile(t, "script.yaml", content)

	sf, err := loadScriptFile(p)
	if err != nil {
		t.Fatalf("loadScriptFile: unexpected error for valid messages: %v", err)
	}
	if len(sf.Messages) != 3 {
		t.Errorf("messages length = %d, want 3", len(sf.Messages))
	}
}

// TestLoadScriptFileUnknownMode verifies that an unknown heartbeat_mode value
// returns an error.
func TestLoadScriptFileUnknownMode(t *testing.T) {
	p := twinScriptFixtureWriteFile(t, "script.yaml", "heartbeat_mode: turbo\nmessages: []\n")

	_, err := loadScriptFile(p)
	if err == nil {
		t.Fatal("loadScriptFile: expected error for unknown heartbeat_mode, got nil")
	}
}

// TestLoadScriptFileMissing verifies that a non-existent path returns an error.
func TestLoadScriptFileMissing(t *testing.T) {
	_, err := loadScriptFile("/nonexistent/path/does-not-exist.yaml")
	if err == nil {
		t.Fatal("loadScriptFile: expected error for missing file, got nil")
	}
}

// TestLoadScriptFileMalformedYAML verifies that a malformed YAML file returns
// a parse error.
func TestLoadScriptFileMalformedYAML(t *testing.T) {
	p := twinScriptFixtureWriteFile(t, "script.yaml", ":\n  bad: [unclosed\n")

	_, err := loadScriptFile(p)
	if err == nil {
		t.Fatal("loadScriptFile: expected error for malformed YAML, got nil")
	}
}

// TestLoadScriptFileMultipleMessages verifies that multiple messages are
// preserved in declaration order.
func TestLoadScriptFileMultipleMessages(t *testing.T) {
	content := `
heartbeat_mode: scripted
messages:
  - type: agent_started
    payload:
      run_id: run-1
  - type: agent_heartbeat
    payload:
      session_id: sess-1
      phase: reasoning
    relative_timestamp_ms: 200
  - type: outcome_emitted
    payload:
      run_id: run-1
      outcome_status: success
`
	p := twinScriptFixtureWriteFile(t, "script.yaml", content)

	sf, err := loadScriptFile(p)
	if err != nil {
		t.Fatalf("loadScriptFile: %v", err)
	}
	if len(sf.Messages) != 3 {
		t.Fatalf("messages length = %d, want 3", len(sf.Messages))
	}
	if sf.Messages[0].Type != "agent_started" {
		t.Errorf("messages[0].type = %q, want agent_started", sf.Messages[0].Type)
	}
	if sf.Messages[1].RelativeTimestampMs != 200 {
		t.Errorf("messages[1].relative_timestamp_ms = %d, want 200", sf.Messages[1].RelativeTimestampMs)
	}
	if sf.Messages[2].Type != "outcome_emitted" {
		t.Errorf("messages[2].type = %q, want outcome_emitted", sf.Messages[2].Type)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// emitScriptMessage
// ────────────────────────────────────────────────────────────────────────────

// TestEmitScriptMessageTypeField verifies that "type" is always present and
// equals the ScriptMessage.Type, even when Payload also contains a "type" key
// (the driver must overwrite the payload's type).
func TestEmitScriptMessageTypeField(t *testing.T) {
	e, buf := twinScriptFixtureEmitter(t)

	msg := ScriptMessage{
		Type: "agent_heartbeat",
		Payload: map[string]any{
			"type":       "should-be-overwritten",
			"session_id": "sess-xyz",
			"phase":      "reasoning",
		},
	}
	if err := emitScriptMessage(e, msg); err != nil {
		t.Fatalf("emitScriptMessage: %v", err)
	}

	msgs := twinScriptFixtureDecodeAll(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if got := m["type"].(string); got != "agent_heartbeat" {
		t.Errorf("type = %q, want agent_heartbeat (payload type must be overwritten)", got)
	}
	if got := m["session_id"].(string); got != "sess-xyz" {
		t.Errorf("session_id = %q, want sess-xyz", got)
	}
}

// TestEmitScriptMessageNoPayload verifies that a message with no payload emits
// only the "type" field.
func TestEmitScriptMessageNoPayload(t *testing.T) {
	e, buf := twinScriptFixtureEmitter(t)

	msg := ScriptMessage{Type: "cancel"}
	if err := emitScriptMessage(e, msg); err != nil {
		t.Fatalf("emitScriptMessage: %v", err)
	}

	msgs := twinScriptFixtureDecodeAll(t, buf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if len(m) != 1 {
		t.Errorf("expected exactly 1 key (type), got %d: %v", len(m), m)
	}
	if got := m["type"].(string); got != "cancel" {
		t.Errorf("type = %q, want cancel", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// runScript — wall_clock mode
// ────────────────────────────────────────────────────────────────────────────

// TestRunScriptWallClockIgnoresTimestamps verifies that wall_clock mode emits
// all messages in order without honouring relative_timestamp_ms delays.
func TestRunScriptWallClockIgnoresTimestamps(t *testing.T) {
	e, buf := twinScriptFixtureEmitter(t)

	sf := &ScriptFile{
		HeartbeatMode: heartbeatModeWallClock,
		Messages: []ScriptMessage{
			{Type: "agent_started", Payload: map[string]any{"run_id": "r1"}},
			// A 10-second delay that must NOT be honoured in wall_clock mode.
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "reasoning"}, RelativeTimestampMs: 10000},
			{Type: "outcome_emitted", Payload: map[string]any{"run_id": "r1", "outcome_status": "success"}},
		},
	}

	start := time.Now()
	ctx := context.Background()
	if err := runScript(ctx, e, sf); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	elapsed := time.Since(start)

	// Must complete in well under 1 second — the 10s delay must not have fired.
	if elapsed > time.Second {
		t.Errorf("wall_clock mode took %v; expected < 1s (timestamp delays must be ignored)", elapsed)
	}

	msgs := twinScriptFixtureDecodeAll(t, buf)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	wantTypes := []string{"agent_started", "agent_heartbeat", "outcome_emitted"}
	for i, want := range wantTypes {
		if got := msgs[i]["type"].(string); got != want {
			t.Errorf("messages[%d].type = %q, want %q", i, got, want)
		}
	}
}

// TestRunScriptEmptyMessages verifies that an empty messages list is a no-op.
func TestRunScriptEmptyMessages(t *testing.T) {
	e, buf := twinScriptFixtureEmitter(t)

	sf := &ScriptFile{HeartbeatMode: heartbeatModeWallClock, Messages: nil}
	if err := runScript(context.Background(), e, sf); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty buffer for empty message list, got %d bytes", buf.Len())
	}
}

// ────────────────────────────────────────────────────────────────────────────
// runScript — scripted mode
// ────────────────────────────────────────────────────────────────────────────

// TestRunScriptScriptedModeZeroDelay verifies that zero-delay scripted messages
// are emitted immediately (no measurable wall-clock delay).
func TestRunScriptScriptedModeZeroDelay(t *testing.T) {
	e, buf := twinScriptFixtureEmitter(t)

	sf := &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "starting"}, RelativeTimestampMs: 0},
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "reasoning"}, RelativeTimestampMs: 0},
		},
	}

	start := time.Now()
	if err := runScript(context.Background(), e, sf); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("zero-delay scripted run took %v; expected < 500ms", elapsed)
	}

	msgs := twinScriptFixtureDecodeAll(t, buf)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

// TestRunScriptScriptedModeDelay verifies that non-zero relative_timestamp_ms
// delays are honoured in scripted mode within a reasonable tolerance.
func TestRunScriptScriptedModeDelay(t *testing.T) {
	e, _ := twinScriptFixtureEmitter(t)

	const delayMs = 50
	sf := &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "starting"}, RelativeTimestampMs: delayMs},
		},
	}

	start := time.Now()
	if err := runScript(context.Background(), e, sf); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	elapsed := time.Since(start)

	const tolerance = 200 * time.Millisecond
	if elapsed < time.Duration(delayMs)*time.Millisecond {
		t.Errorf("scripted mode took %v; expected >= %dms", elapsed, delayMs)
	}
	if elapsed > time.Duration(delayMs)*time.Millisecond+tolerance {
		t.Errorf("scripted mode took %v; expected <= %dms+tolerance", elapsed, delayMs)
	}
}

// TestRunScriptContextCancellation verifies that runScript returns ctx.Err()
// promptly when the context is cancelled during a scripted delay.
func TestRunScriptContextCancellation(t *testing.T) {
	e, _ := twinScriptFixtureEmitter(t)

	// Use a 10-second scripted delay; cancel the context after 50ms.
	sf := &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "starting"}, RelativeTimestampMs: 10000},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := runScript(ctx, e, sf)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runScript: expected error on context cancellation, got nil")
	}
	// Must return well before the 10s scripted delay elapses.
	if elapsed > time.Second {
		t.Errorf("runScript cancelled after %v; expected < 1s", elapsed)
	}
}

// TestRunScriptWallClockContextCancellation verifies that runScript in
// wall_clock mode still respects context cancellation between messages.
func TestRunScriptWallClockContextCancellation(t *testing.T) {
	e, _ := twinScriptFixtureEmitter(t)

	// Many messages; cancel after the first one is emitted by using a context
	// that's already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sf := &ScriptFile{
		HeartbeatMode: heartbeatModeWallClock,
		Messages: []ScriptMessage{
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "starting"}},
			{Type: "agent_heartbeat", Payload: map[string]any{"session_id": "s1", "phase": "reasoning"}},
		},
	}

	err := runScript(ctx, e, sf)
	if err == nil {
		t.Fatal("runScript: expected error on pre-cancelled context, got nil")
	}
}
