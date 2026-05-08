package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// Test helpers use the per-bead prefix declared in implementer-protocol.md:
// twinWireFixture (this bead: hk-ahvq.48.2).

// twinWireFixtureEmitter returns a wireEmitter writing to a bytes.Buffer plus
// the buffer itself, for round-trip message inspection.
func twinWireFixtureEmitter(t *testing.T) (*wireEmitter, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return newWireEmitter(&buf), &buf
}

// twinWireFixtureDecode decodes the nth NDJSON line (0-indexed) from buf into
// a map[string]any and returns it.  It calls t.Fatalf if the line does not
// exist or is not valid JSON.
func twinWireFixtureDecode(t *testing.T, buf *bytes.Buffer, n int) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if n >= len(lines) {
		t.Fatalf("twinWireFixtureDecode: want line %d, only %d lines in buffer", n, len(lines))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[n]), &m); err != nil {
		t.Fatalf("twinWireFixtureDecode: line %d unmarshal: %v — raw: %q", n, err, lines[n])
	}
	return m
}

// twinWireFixtureAssertType checks that a decoded message map has the expected
// "type" field value.
func twinWireFixtureAssertType(t *testing.T, m map[string]any, want string) {
	t.Helper()
	got, ok := m["type"].(string)
	if !ok {
		t.Fatalf("twinWireFixtureAssertType: no string 'type' field in message: %v", m)
	}
	if got != want {
		t.Errorf("message type = %q, want %q", got, want)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// NDJSON framing tests (HC-007a)
// ────────────────────────────────────────────────────────────────────────────

// TestWireEmitFraming verifies that each emitted message is exactly one JSON
// object terminated by a single newline (0x0A) with no extra whitespace
// between messages (HC-007a).
func TestWireEmitFraming(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	if err := e.emitAgentHeartbeat("sid-001", heartbeatPhaseStarting); err != nil {
		t.Fatalf("emitAgentHeartbeat: %v", err)
	}
	if err := e.emitAgentHeartbeat("sid-001", heartbeatPhaseReasoning); err != nil {
		t.Fatalf("emitAgentHeartbeat: %v", err)
	}

	raw := buf.String()

	// Each line must end with exactly one newline.
	lines := strings.Split(raw, "\n")
	// The last split element is "" because the string ends with \n.
	if lines[len(lines)-1] != "" {
		t.Errorf("buffer does not end with newline; last segment = %q", lines[len(lines)-1])
	}
	// Strip the trailing empty segment; should have exactly 2 messages.
	lines = lines[:len(lines)-1]
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d", len(lines))
	}

	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d is not valid JSON: %v — %q", i, err, line)
		}
		// No embedded unescaped newline inside a JSON object per HC-007a.
		if strings.Contains(line, "\n") {
			t.Errorf("line %d contains embedded newline", i)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// handler_capabilities (HC-009)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitHandlerCapabilities verifies the type field and required payload
// fields of the handler_capabilities message (HC-009, event-model §8.3.9).
func TestEmitHandlerCapabilities(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	if err := e.emitHandlerCapabilities("run-001", "sess-001", []int{1}); err != nil {
		t.Fatalf("emitHandlerCapabilities: %v", err)
	}

	m := twinWireFixtureDecode(t, buf, 0)
	twinWireFixtureAssertType(t, m, "handler_capabilities")

	if got := m["run_id"].(string); got != "run-001" {
		t.Errorf("run_id = %q, want %q", got, "run-001")
	}
	if got := m["session_id"].(string); got != "sess-001" {
		t.Errorf("session_id = %q, want %q", got, "sess-001")
	}
	vers, ok := m["protocol_versions_supported"].([]any)
	if !ok || len(vers) == 0 {
		t.Errorf("protocol_versions_supported missing or empty: %v", m["protocol_versions_supported"])
	}
}

// ────────────────────────────────────────────────────────────────────────────
// session_log_location (HC-010)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitSessionLogLocation verifies the type, required fields, and optional
// bead_id omission/inclusion for session_log_location (HC-010, event-model §8.3.7).
func TestEmitSessionLogLocation(t *testing.T) {
	t.Run("without_bead_id", func(t *testing.T) {
		e, buf := twinWireFixtureEmitter(t)
		if err := e.emitSessionLogLocation(
			"run-1", "sess-1", "node-1", "claude-twin",
			"/tmp/session.log", "ndjson", nil,
		); err != nil {
			t.Fatalf("emitSessionLogLocation: %v", err)
		}
		m := twinWireFixtureDecode(t, buf, 0)
		twinWireFixtureAssertType(t, m, "session_log_location")
		if _, exists := m["bead_id"]; exists {
			t.Error("bead_id present in message without bead_id arg; want omitted")
		}
	})

	t.Run("with_bead_id", func(t *testing.T) {
		e, buf := twinWireFixtureEmitter(t)
		bid := "hk-test.1"
		if err := e.emitSessionLogLocation(
			"run-1", "sess-1", "node-1", "claude-twin",
			"/tmp/session.log", "ndjson", &bid,
		); err != nil {
			t.Fatalf("emitSessionLogLocation: %v", err)
		}
		m := twinWireFixtureDecode(t, buf, 0)
		twinWireFixtureAssertType(t, m, "session_log_location")
		if got, ok := m["bead_id"].(string); !ok || got != bid {
			t.Errorf("bead_id = %v, want %q", m["bead_id"], bid)
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// skills_provisioned (HC-049)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitSkillsProvisioned verifies the type field and skills array encoding
// (event-model §8.3.8).
func TestEmitSkillsProvisioned(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	ver := "1.2.3"
	skills := []skillEntry{
		{Name: "git-apply", SourcePath: "/skills/git-apply"},
		{Name: "search", SourcePath: "/skills/search", Version: &ver},
	}
	if err := e.emitSkillsProvisioned("run-1", "sess-1", skills); err != nil {
		t.Fatalf("emitSkillsProvisioned: %v", err)
	}

	m := twinWireFixtureDecode(t, buf, 0)
	twinWireFixtureAssertType(t, m, "skills_provisioned")

	arr, ok := m["skills"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("skills field: want 2-element array, got %v", m["skills"])
	}
	// Second skill has version; first does not.
	s0 := arr[0].(map[string]any)
	if _, hasVer := s0["version"]; hasVer {
		t.Errorf("first skill should have no version field, got %v", s0)
	}
	s1 := arr[1].(map[string]any)
	if s1["version"] != "1.2.3" {
		t.Errorf("second skill version = %v, want 1.2.3", s1["version"])
	}
}

// ────────────────────────────────────────────────────────────────────────────
// agent_ready (HC-039, HC-040)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitAgentReady verifies the type and capabilities array (HC-039/HC-040,
// event-model §8.3.1).
func TestEmitAgentReady(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	if err := e.emitAgentReady("run-1", "sess-1", []string{"scripted", "heartbeat"}); err != nil {
		t.Fatalf("emitAgentReady: %v", err)
	}

	m := twinWireFixtureDecode(t, buf, 0)
	twinWireFixtureAssertType(t, m, "agent_ready")

	caps, ok := m["capabilities"].([]any)
	if !ok || len(caps) != 2 {
		t.Errorf("capabilities = %v, want 2-element array", m["capabilities"])
	}
}

// ────────────────────────────────────────────────────────────────────────────
// agent_started (HC-007, §6.4)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitAgentStarted verifies that the agent_started message does NOT
// include environment variables (HC-029) and carries the required fields
// (event-model §8.3.2).
func TestEmitAgentStarted(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := e.emitAgentStarted("run-1", "sess-1", "node-1", "claude-twin", ts); err != nil {
		t.Fatalf("emitAgentStarted: %v", err)
	}

	m := twinWireFixtureDecode(t, buf, 0)
	twinWireFixtureAssertType(t, m, "agent_started")

	// HC-029: no environment variables in the payload.
	for _, forbidden := range []string{"env", "environment", "environ"} {
		if _, exists := m[forbidden]; exists {
			t.Errorf("agent_started carries forbidden env field %q (HC-029)", forbidden)
		}
	}

	// started_at must be a parseable RFC3339 timestamp.
	sat, ok := m["started_at"].(string)
	if !ok {
		t.Fatalf("started_at missing or not string: %v", m["started_at"])
	}
	if _, err := time.Parse(time.RFC3339Nano, sat); err != nil {
		t.Errorf("started_at %q not RFC3339Nano: %v", sat, err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// agent_heartbeat (HC-026a)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitAgentHeartbeat verifies the heartbeat message type, required
// session_id and phase fields (HC-026a).
func TestEmitAgentHeartbeat(t *testing.T) {
	phases := []heartbeatPhase{
		heartbeatPhaseStarting,
		heartbeatPhaseReasoning,
		heartbeatPhaseToolCall,
		heartbeatPhaseWaitingInput,
		heartbeatPhaseRotating,
		heartbeatPhaseShuttingDown,
	}
	for _, phase := range phases {
		phase := phase
		t.Run(string(phase), func(t *testing.T) {
			e, buf := twinWireFixtureEmitter(t)
			if err := e.emitAgentHeartbeat("sess-001", phase); err != nil {
				t.Fatalf("emitAgentHeartbeat(%q): %v", phase, err)
			}
			m := twinWireFixtureDecode(t, buf, 0)
			twinWireFixtureAssertType(t, m, "agent_heartbeat")

			if got := m["session_id"].(string); got != "sess-001" {
				t.Errorf("session_id = %q, want sess-001", got)
			}
			if got := m["phase"].(string); got != string(phase) {
				t.Errorf("phase = %q, want %q", got, phase)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// outcome_emitted (HC-008)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitOutcomeEmitted verifies the outcome_emitted message type and
// required fields (HC-008).
func TestEmitOutcomeEmitted(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	if err := e.emitOutcomeEmitted("run-1", "sess-1", "node-1", "success"); err != nil {
		t.Fatalf("emitOutcomeEmitted: %v", err)
	}

	m := twinWireFixtureDecode(t, buf, 0)
	twinWireFixtureAssertType(t, m, "outcome_emitted")

	if got := m["outcome_status"].(string); got != "success" {
		t.Errorf("outcome_status = %q, want success", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// agent_completed (HC-024)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitAgentCompleted verifies the type and payload of agent_completed
// (HC-024, event-model §8.3.4).
func TestEmitAgentCompleted(t *testing.T) {
	e, buf := twinWireFixtureEmitter(t)

	endedAt := time.Date(2026, 1, 2, 4, 5, 6, 0, time.UTC)
	if err := e.emitAgentCompleted("run-1", "sess-1", endedAt, 0, "outcome-ref-001"); err != nil {
		t.Fatalf("emitAgentCompleted: %v", err)
	}

	m := twinWireFixtureDecode(t, buf, 0)
	twinWireFixtureAssertType(t, m, "agent_completed")

	if got := m["exit_code"].(float64); got != 0 {
		t.Errorf("exit_code = %v, want 0", got)
	}
	if got := m["outcome_ref"].(string); got != "outcome-ref-001" {
		t.Errorf("outcome_ref = %q, want outcome-ref-001", got)
	}

	// ended_at must parse as RFC3339Nano.
	eat := m["ended_at"].(string)
	if _, err := time.Parse(time.RFC3339Nano, eat); err != nil {
		t.Errorf("ended_at %q not RFC3339Nano: %v", eat, err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// agent_failed (HC-024)
// ────────────────────────────────────────────────────────────────────────────

// TestEmitAgentFailed verifies the type, error_category, reason, and
// optional sub_reason field (HC-024, event-model §8.3.5).
func TestEmitAgentFailed(t *testing.T) {
	t.Run("without_sub_reason", func(t *testing.T) {
		e, buf := twinWireFixtureEmitter(t)
		endedAt := time.Now().UTC()
		if err := e.emitAgentFailed("run-1", "sess-1", endedAt, "structural", "silent_hang", ""); err != nil {
			t.Fatalf("emitAgentFailed: %v", err)
		}
		m := twinWireFixtureDecode(t, buf, 0)
		twinWireFixtureAssertType(t, m, "agent_failed")
		if got := m["error_category"].(string); got != "structural" {
			t.Errorf("error_category = %q, want structural", got)
		}
		// sub_reason must be omitted when empty (omitempty).
		if _, exists := m["sub_reason"]; exists {
			t.Error("sub_reason present for empty value; want omitempty")
		}
	})

	t.Run("with_sub_reason", func(t *testing.T) {
		e, buf := twinWireFixtureEmitter(t)
		endedAt := time.Now().UTC()
		if err := e.emitAgentFailed("run-1", "sess-1", endedAt, "structural", "protocol_mismatch", "ndjson_line_too_long"); err != nil {
			t.Fatalf("emitAgentFailed: %v", err)
		}
		m := twinWireFixtureDecode(t, buf, 0)
		if got := m["sub_reason"].(string); got != "ndjson_line_too_long" {
			t.Errorf("sub_reason = %q, want ndjson_line_too_long", got)
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Control message reader (daemon-to-handler direction)
// ────────────────────────────────────────────────────────────────────────────

// TestWireReaderVersionSelected verifies that the wireReader correctly decodes
// the version_selected control message sent by the daemon after the handshake
// (§7.2).
func TestWireReaderVersionSelected(t *testing.T) {
	raw := `{"type":"version_selected","selected_version":1}` + "\n"
	r := newWireReader(strings.NewReader(raw))

	msg, err := r.readControlMsg()
	if err != nil {
		t.Fatalf("readControlMsg: %v", err)
	}
	if msg.Type != "version_selected" {
		t.Errorf("type = %q, want version_selected", msg.Type)
	}
	if msg.SelectedVersion == nil || *msg.SelectedVersion != 1 {
		t.Errorf("selected_version = %v, want 1", msg.SelectedVersion)
	}
}

// TestWireReaderUnknownTypeIgnored verifies that an unrecognised control
// message type is returned without error — forward-compatibility per §6.4.
func TestWireReaderUnknownTypeIgnored(t *testing.T) {
	raw := `{"type":"future_unknown_control","extra_field":"foo"}` + "\n"
	r := newWireReader(strings.NewReader(raw))

	msg, err := r.readControlMsg()
	if err != nil {
		t.Fatalf("readControlMsg: %v", err)
	}
	if msg.Type != "future_unknown_control" {
		t.Errorf("type = %q, want future_unknown_control", msg.Type)
	}
}

// TestWireReaderEOF verifies that readControlMsg returns io.EOF at end of
// stream.
func TestWireReaderEOF(t *testing.T) {
	r := newWireReader(strings.NewReader(""))
	_, err := r.readControlMsg()
	if err != io.EOF {
		t.Errorf("expected io.EOF at empty reader, got %v", err)
	}
}

// TestWireReaderMultipleMessages verifies sequential decoding across multiple
// NDJSON lines.
func TestWireReaderMultipleMessages(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"version_selected","selected_version":1}`,
		`{"type":"cancel"}`,
	}, "\n") + "\n"

	r := newWireReader(strings.NewReader(raw))

	m1, err := r.readControlMsg()
	if err != nil {
		t.Fatalf("first readControlMsg: %v", err)
	}
	if m1.Type != "version_selected" {
		t.Errorf("first message type = %q, want version_selected", m1.Type)
	}

	m2, err := r.readControlMsg()
	if err != nil {
		t.Fatalf("second readControlMsg: %v", err)
	}
	if m2.Type != "cancel" {
		t.Errorf("second message type = %q, want cancel", m2.Type)
	}

	_, err = r.readControlMsg()
	if err != io.EOF {
		t.Errorf("expected io.EOF after last message, got %v", err)
	}
}
