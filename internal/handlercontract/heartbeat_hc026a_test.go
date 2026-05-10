package handlercontract_test

// heartbeat_hc026a_test.go — HC-026a heartbeat phase + message tests.
//
// Spec: specs/handler-contract.md §4.6.HC-026a.
// Bead: hk-8i31.32.

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Phase constant values
// ─────────────────────────────────────────────────────────────────────────────

// TestHC026a_PhaseConstants_StringValues verifies that all 6 required
// HeartbeatPhase constants have the normative wire-format string values.
//
// Spec: handler-contract.md §4.6.HC-026a.
func TestHC026a_PhaseConstants_StringValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase handlercontract.HeartbeatPhase
		want  string
	}{
		{handlercontract.HeartbeatPhaseStarting, "starting"},
		{handlercontract.HeartbeatPhaseReasoning, "reasoning"},
		{handlercontract.HeartbeatPhaseToolCall, "tool_call"},
		{handlercontract.HeartbeatPhaseWaitingInput, "waiting_input"},
		{handlercontract.HeartbeatPhaseRotating, "rotating"},
		{handlercontract.HeartbeatPhaseShuttingDown, "shutting_down"},
	}
	for _, tc := range cases {
		if string(tc.phase) != tc.want {
			t.Errorf("HeartbeatPhase constant: got %q, want %q", tc.phase, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IsRequiredPhase
// ─────────────────────────────────────────────────────────────────────────────

// TestHC026a_IsRequiredPhase_TrueForAllRequired verifies that IsRequiredPhase
// returns true for each of the 6 required phase values.
//
// Spec: handler-contract.md §4.6.HC-026a.
func TestHC026a_IsRequiredPhase_TrueForAllRequired(t *testing.T) {
	t.Parallel()

	required := []handlercontract.HeartbeatPhase{
		handlercontract.HeartbeatPhaseStarting,
		handlercontract.HeartbeatPhaseReasoning,
		handlercontract.HeartbeatPhaseToolCall,
		handlercontract.HeartbeatPhaseWaitingInput,
		handlercontract.HeartbeatPhaseRotating,
		handlercontract.HeartbeatPhaseShuttingDown,
	}
	for _, p := range required {
		if !p.IsRequiredPhase() {
			t.Errorf("IsRequiredPhase(%q) = false, want true", p)
		}
	}
}

// TestHC026a_IsRequiredPhase_FalseForUnknown verifies that IsRequiredPhase
// returns false for phase values not declared in HC-026a (extension phases).
//
// Spec: handler-contract.md §4.6.HC-026a — additive-evolution rule; watcher
// MUST NOT reject unknown phase values.
func TestHC026a_IsRequiredPhase_FalseForUnknown(t *testing.T) {
	t.Parallel()

	unknown := []handlercontract.HeartbeatPhase{
		"",
		"custom_phase",
		"idle",
		"unknown",
	}
	for _, p := range unknown {
		if p.IsRequiredPhase() {
			t.Errorf("IsRequiredPhase(%q) = true, want false (not a required phase)", p)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RequiredHeartbeatPhaseCount
// ─────────────────────────────────────────────────────────────────────────────

// TestHC026a_RequiredHeartbeatPhaseCount_Is6 verifies that
// RequiredHeartbeatPhaseCount equals 6 as declared in HC-026a.
//
// Spec: handler-contract.md §4.6.HC-026a.
func TestHC026a_RequiredHeartbeatPhaseCount_Is6(t *testing.T) {
	t.Parallel()

	const want = 6
	if handlercontract.RequiredHeartbeatPhaseCount != want {
		t.Errorf("RequiredHeartbeatPhaseCount = %d, want %d", handlercontract.RequiredHeartbeatPhaseCount, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeartbeatMsg JSON shape
// ─────────────────────────────────────────────────────────────────────────────

// TestHC026a_HeartbeatMsg_JSONFieldNames verifies that HeartbeatMsg marshals
// to the correct JSON field names: "type", "session_id", "phase".
//
// Spec: handler-contract.md §4.6.HC-026a.
func TestHC026a_HeartbeatMsg_JSONFieldNames(t *testing.T) {
	t.Parallel()

	msg := handlercontract.HeartbeatMsg{
		Type:      "agent_heartbeat",
		SessionID: "sess-abc123",
		Phase:     handlercontract.HeartbeatPhaseReasoning,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("HeartbeatMsg marshal: unexpected error: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("HeartbeatMsg unmarshal: unexpected error: %v", err)
	}

	if v, ok := got["type"]; !ok || v != "agent_heartbeat" {
		t.Errorf("JSON field \"type\": got %v, want \"agent_heartbeat\"", v)
	}
	if v, ok := got["session_id"]; !ok || v != "sess-abc123" {
		t.Errorf("JSON field \"session_id\": got %v, want \"sess-abc123\"", v)
	}
	if v, ok := got["phase"]; !ok || v != "reasoning" {
		t.Errorf("JSON field \"phase\": got %v, want \"reasoning\"", v)
	}
}

// TestHC026a_HeartbeatMsg_RoundTrip verifies that HeartbeatMsg survives a
// marshal/unmarshal round-trip with field values intact.
//
// Spec: handler-contract.md §4.6.HC-026a.
func TestHC026a_HeartbeatMsg_RoundTrip(t *testing.T) {
	t.Parallel()

	original := handlercontract.HeartbeatMsg{
		Type:      "agent_heartbeat",
		SessionID: "session-xyz-789",
		Phase:     handlercontract.HeartbeatPhaseToolCall,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded handlercontract.HeartbeatMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID: got %q, want %q", decoded.SessionID, original.SessionID)
	}
	if decoded.Phase != original.Phase {
		t.Errorf("Phase: got %q, want %q", decoded.Phase, original.Phase)
	}
}
