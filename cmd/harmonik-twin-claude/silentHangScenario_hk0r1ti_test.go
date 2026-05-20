package main

// silentHangScenario_hk0r1ti_test.go — tests for the silent-hang canned scenario
// (hk-0r1ti).
//
// SC-3 (hk-xfhva) requires a twin scenario that emits the preamble
// (handler_capabilities → agent_ready → agent_started) and then returns
// without heartbeats or outcome_emitted, forcing HC-056 silent-hang detection
// to fire.
//
// Helper prefix: silentHangScenarioFixture (bead hk-0r1ti, concept: silent-hang
// scenario).
//
// Cite: specs/handler-contract.md §4.6.HC-056, §7.1; hk-0r1ti; hk-xfhva.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

// silentHangScenarioFixtureDecodeAll splits buf into NDJSON lines and decodes
// all into a []map[string]any.  Calls t.Fatalf on any JSON decode error.
func silentHangScenarioFixtureDecodeAll(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	raw := buf.Bytes()
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	out := make([]map[string]any, 0, len(parts))
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(part, &m); err != nil {
			t.Fatalf("silentHangScenarioFixtureDecodeAll: line %d unmarshal: %v — raw: %q", i, err, string(part))
		}
		out = append(out, m)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenarioSilentHang_TruncatedSequence
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioSilentHang_TruncatedSequence verifies that the silent-hang canned
// scenario (SC-3 / hk-xfhva) emits exactly the truncated preamble sequence:
//
//	handler_capabilities → agent_ready → agent_started
//
// and nothing further — no heartbeats, no outcome_emitted, no agent_completed.
//
// This is the normative shape required by HC-056: the daemon watcher must
// observe agent_started followed by process exit (no heartbeats, no outcome)
// and fire silent-hang detection.
//
// Spec: specs/handler-contract.md §4.6.HC-056; hk-0r1ti.
func TestScenarioSilentHang_TruncatedSequence(t *testing.T) {
	t.Parallel()

	sf := scenarioSilentHang()

	var buf bytes.Buffer
	e := newWireEmitter(&buf)

	if err := runScript(context.Background(), e, sf, scriptRunConfig{}); err != nil {
		t.Fatalf("runScript: %v", err)
	}

	msgs := silentHangScenarioFixtureDecodeAll(t, &buf)

	wantTypes := []string{
		"handler_capabilities",
		"agent_ready",
		"agent_started",
	}

	if len(msgs) != len(wantTypes) {
		var gotTypes []string
		for _, m := range msgs {
			gotTypes = append(gotTypes, m["type"].(string))
		}
		t.Fatalf("silent-hang scenario: got %d messages %v, want %d %v (HC-056 truncated preamble only)",
			len(msgs), gotTypes, len(wantTypes), wantTypes)
	}

	for i, want := range wantTypes {
		got, _ := msgs[i]["type"].(string)
		if got != want {
			t.Errorf("msgs[%d].type = %q, want %q", i, got, want)
		}
	}
}

// TestScenarioSilentHang_NoHeartbeat verifies that the silent-hang scenario
// contains no agent_heartbeat messages.  The absence of heartbeats is the
// trigger condition for HC-056 silent-hang detection: once agent_started is
// observed, the watcher's silence timer must not be reset.
//
// Spec: specs/handler-contract.md §4.6.HC-056 (silence timer reset on heartbeat).
func TestScenarioSilentHang_NoHeartbeat(t *testing.T) {
	t.Parallel()

	sf := scenarioSilentHang()

	for i, msg := range sf.Messages {
		if msg.Type == "agent_heartbeat" {
			t.Errorf("silent-hang scenario: messages[%d] is agent_heartbeat; scenario must emit no heartbeats (HC-056 trigger)", i)
		}
	}
}

// TestScenarioSilentHang_NoOutcomeEmitted verifies that the silent-hang
// scenario contains no outcome_emitted message.  Outcome absence is required:
// if outcome_emitted were present the watcher would transition to the
// post-outcome shutdown-window regime (HC-008a) rather than the silent-hang
// FSM.
//
// Spec: specs/handler-contract.md §7.1 (silent-hang suspended after
// outcome_emitted); HC-008a.
func TestScenarioSilentHang_NoOutcomeEmitted(t *testing.T) {
	t.Parallel()

	sf := scenarioSilentHang()

	for i, msg := range sf.Messages {
		if msg.Type == "outcome_emitted" {
			t.Errorf("silent-hang scenario: messages[%d] is outcome_emitted; scenario must not emit outcome (would suppress HC-056 FSM)", i)
		}
	}
}

// TestScenarioSilentHang_HeartbeatModeScripted verifies that the silent-hang
// scenario uses heartbeat_mode: scripted, ensuring byte-reproducible emission
// in scenario tests (HC-026a scripted-mode carve-out).
//
// Spec: specs/handler-contract.md §4.6.HC-026a scripted-mode carve-out.
func TestScenarioSilentHang_HeartbeatModeScripted(t *testing.T) {
	t.Parallel()

	sf := scenarioSilentHang()

	if sf.HeartbeatMode != heartbeatModeScripted {
		t.Errorf("silent-hang scenario: heartbeat_mode = %q, want %q (HC-026a scripted-mode carve-out)",
			sf.HeartbeatMode, heartbeatModeScripted)
	}
}

// TestScenarioSilentHang_CannedRoutes verifies that cannedScenario("silent-hang")
// returns a non-nil ScriptFile without error.
func TestScenarioSilentHang_CannedRoutes(t *testing.T) {
	t.Parallel()

	sf, err := cannedScenario("silent-hang")
	if err != nil {
		t.Fatalf("cannedScenario(\"silent-hang\"): unexpected error: %v", err)
	}
	if sf == nil {
		t.Fatal("cannedScenario(\"silent-hang\"): returned nil ScriptFile")
	}
}
