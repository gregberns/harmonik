package main

// partialPreExecScenario_hk7amcv_test.go — tests for the partial-pre-exec canned
// scenario (hk-7amcv).
//
// SC-5 (hk-35mpj) requires a twin scenario that emits handler_capabilities +
// agent_started only — omitting agent_ready.  The watcher times out waiting for
// agent_ready per the §7.2 handshake window; daemon HC-024 closes the bead.
//
// Helper prefix: partialPreExecFixture (bead hk-7amcv, concept: partial-pre-exec
// scenario).
//
// Cite: specs/handler-contract.md §4.6.HC-024, §7.2; hk-7amcv; hk-35mpj.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

// partialPreExecFixtureDecodeAll splits buf into NDJSON lines and decodes all
// into a []map[string]any.  Calls t.Fatalf on any JSON decode error.
func partialPreExecFixtureDecodeAll(t *testing.T, buf *bytes.Buffer) []map[string]any {
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
			t.Fatalf("partialPreExecFixtureDecodeAll: line %d unmarshal: %v — raw: %q", i, err, string(part))
		}
		out = append(out, m)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenarioPartialPreExec_TruncatedSequence
// ─────────────────────────────────────────────────────────────────────────────

// TestScenarioPartialPreExec_TruncatedSequence verifies that the partial-pre-exec
// canned scenario (SC-5 / hk-35mpj) emits exactly the truncated sequence:
//
//	handler_capabilities → agent_started
//
// and nothing further — no agent_ready, no heartbeats, no outcome_emitted, no
// agent_completed.
//
// This is the normative shape required by HC-024 + §7.2: the watcher must observe
// the subprocess exit without ever receiving agent_ready, triggering the
// handshake-timeout path and forcing HC-024 bead closure.
//
// Spec: specs/handler-contract.md §4.6.HC-024, §7.2; hk-7amcv.
func TestScenarioPartialPreExec_TruncatedSequence(t *testing.T) {
	t.Parallel()

	sf := scenarioPartialPreExec()

	var buf bytes.Buffer
	e := newWireEmitter(&buf)

	if err := runScript(context.Background(), e, sf, scriptRunConfig{}); err != nil {
		t.Fatalf("runScript: %v", err)
	}

	msgs := partialPreExecFixtureDecodeAll(t, &buf)

	wantTypes := []string{
		"handler_capabilities",
		"agent_started",
	}

	if len(msgs) != len(wantTypes) {
		var gotTypes []string
		for _, m := range msgs {
			gotTypes = append(gotTypes, m["type"].(string))
		}
		t.Fatalf("partial-pre-exec scenario: got %d messages %v, want %d %v (HC-024 + §7.2 truncated preamble only)",
			len(msgs), gotTypes, len(wantTypes), wantTypes)
	}

	for i, want := range wantTypes {
		got, _ := msgs[i]["type"].(string)
		if got != want {
			t.Errorf("msgs[%d].type = %q, want %q", i, got, want)
		}
	}
}

// TestScenarioPartialPreExec_NoAgentReady verifies that the partial-pre-exec
// scenario contains no agent_ready message.  Absence of agent_ready is the
// defining characteristic of SC-5: the watcher's §7.2 handshake-timeout fires
// precisely because agent_ready never arrives.
//
// Spec: specs/handler-contract.md §7.2 (handshake window); HC-024.
func TestScenarioPartialPreExec_NoAgentReady(t *testing.T) {
	t.Parallel()

	sf := scenarioPartialPreExec()

	for i, msg := range sf.Messages {
		if msg.Type == "agent_ready" {
			t.Errorf("partial-pre-exec scenario: messages[%d] is agent_ready; scenario must omit agent_ready (§7.2 handshake-timeout trigger)", i)
		}
	}
}

// TestScenarioPartialPreExec_NoOutcomeEmitted verifies that the partial-pre-exec
// scenario contains no outcome_emitted message.  Outcome absence is required:
// the session never progresses past the pre-exec handshake window.
//
// Spec: specs/handler-contract.md §4.6.HC-024.
func TestScenarioPartialPreExec_NoOutcomeEmitted(t *testing.T) {
	t.Parallel()

	sf := scenarioPartialPreExec()

	for i, msg := range sf.Messages {
		if msg.Type == "outcome_emitted" {
			t.Errorf("partial-pre-exec scenario: messages[%d] is outcome_emitted; scenario must not emit outcome (pre-exec stall, HC-024)", i)
		}
	}
}

// TestScenarioPartialPreExec_HeartbeatModeScripted verifies that the
// partial-pre-exec scenario uses heartbeat_mode: scripted, ensuring
// byte-reproducible emission in scenario tests (HC-026a scripted-mode carve-out).
//
// Spec: specs/handler-contract.md §4.6.HC-026a scripted-mode carve-out.
func TestScenarioPartialPreExec_HeartbeatModeScripted(t *testing.T) {
	t.Parallel()

	sf := scenarioPartialPreExec()

	if sf.HeartbeatMode != heartbeatModeScripted {
		t.Errorf("partial-pre-exec scenario: heartbeat_mode = %q, want %q (HC-026a scripted-mode carve-out)",
			sf.HeartbeatMode, heartbeatModeScripted)
	}
}

// TestScenarioPartialPreExec_CannedRoutes verifies that
// cannedScenario("partial-pre-exec") returns a non-nil ScriptFile without error.
func TestScenarioPartialPreExec_CannedRoutes(t *testing.T) {
	t.Parallel()

	sf, err := cannedScenario("partial-pre-exec")
	if err != nil {
		t.Fatalf("cannedScenario(\"partial-pre-exec\"): unexpected error: %v", err)
	}
	if sf == nil {
		t.Fatal("cannedScenario(\"partial-pre-exec\"): returned nil ScriptFile")
	}
}

// TestScenarioPartialPreExec_StartsWithHandlerCapabilities verifies that the
// first message emitted is handler_capabilities per HC-009.  This confirms the
// scenario models a handler that successfully announced its protocol capabilities
// before the pre-exec stall occurred.
//
// Spec: specs/handler-contract.md §4.6.HC-009 (handler_capabilities first).
func TestScenarioPartialPreExec_StartsWithHandlerCapabilities(t *testing.T) {
	t.Parallel()

	sf := scenarioPartialPreExec()

	if len(sf.Messages) == 0 {
		t.Fatal("partial-pre-exec scenario: no messages; want at least handler_capabilities")
	}
	if sf.Messages[0].Type != "handler_capabilities" {
		t.Errorf("partial-pre-exec scenario: messages[0].type = %q, want handler_capabilities (HC-009 first-message rule)",
			sf.Messages[0].Type)
	}
}
