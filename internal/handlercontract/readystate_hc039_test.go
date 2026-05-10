package handlercontract_test

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// readyStateFixture — per-bead helper prefix for test helpers in this file
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.46).

// ─────────────────────────────────────────────────────────────────────────────
// HC-039 — AgentReadyMsg type field
// ─────────────────────────────────────────────────────────────────────────────

// TestReadyState_AgentReadyMsgTypeField verifies that Type matches
// ProgressMsgTypeAgentReady per §4.9.HC-039.
func TestReadyState_AgentReadyMsgTypeField(t *testing.T) {
	t.Parallel()

	msg := handlercontract.AgentReadyMsg{
		Type:         handlercontract.ProgressMsgTypeAgentReady,
		SessionID:    "sess-1",
		Capabilities: []string{},
	}

	if msg.Type != handlercontract.ProgressMsgTypeAgentReady {
		t.Errorf("AgentReadyMsg.Type = %q, want ProgressMsgTypeAgentReady (%q)",
			msg.Type, handlercontract.ProgressMsgTypeAgentReady)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-039 — AgentReadyMsg JSON round-trip
// ─────────────────────────────────────────────────────────────────────────────

// TestReadyState_AgentReadyMsgRoundTripEmptyCapabilities verifies round-trip
// when capabilities is empty (handler declares no optional capabilities).
func TestReadyState_AgentReadyMsgRoundTripEmptyCapabilities(t *testing.T) {
	t.Parallel()

	orig := handlercontract.AgentReadyMsg{
		Type:         "agent_ready",
		SessionID:    "sess-a",
		Capabilities: []string{},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got handlercontract.AgentReadyMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != orig.Type {
		t.Errorf("Type: got %q, want %q", got.Type, orig.Type)
	}
	if got.SessionID != orig.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, orig.SessionID)
	}
	if got.Capabilities == nil {
		t.Error("Capabilities: got nil after round-trip; empty slice should survive")
	}
	if len(got.Capabilities) != 0 {
		t.Errorf("Capabilities: got len %d, want 0", len(got.Capabilities))
	}
}

// TestReadyState_AgentReadyMsgRoundTripWithCapabilities verifies round-trip
// when capabilities is non-empty.
func TestReadyState_AgentReadyMsgRoundTripWithCapabilities(t *testing.T) {
	t.Parallel()

	orig := handlercontract.AgentReadyMsg{
		Type:         "agent_ready",
		SessionID:    "sess-b",
		Capabilities: []string{"tool_use", "streaming"},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got handlercontract.AgentReadyMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Capabilities) != 2 {
		t.Fatalf("Capabilities: got len %d, want 2", len(got.Capabilities))
	}
	if got.Capabilities[0] != "tool_use" || got.Capabilities[1] != "streaming" {
		t.Errorf("Capabilities: got %v, want [tool_use streaming]", got.Capabilities)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-039 — AgentReadyMsg wire field names
// ─────────────────────────────────────────────────────────────────────────────

// TestReadyState_AgentReadyMsgWireFieldNames verifies that the JSON
// serialization uses the spec-mandated wire field names per §4.9.HC-039.
func TestReadyState_AgentReadyMsgWireFieldNames(t *testing.T) {
	t.Parallel()

	msg := handlercontract.AgentReadyMsg{
		Type:         "agent_ready",
		SessionID:    "s",
		Capabilities: []string{"tool_use"},
	}

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	for _, wantKey := range []string{"type", "session_id", "capabilities"} {
		if _, ok := raw[wantKey]; !ok {
			t.Errorf("expected JSON key %q in AgentReadyMsg", wantKey)
		}
	}
}

// TestReadyState_AgentReadyMsgCapabilitiesIsSliceNotNull verifies that the
// capabilities field encodes as a JSON array (not null) when non-nil, even
// when empty.  The spec requires capabilities[] at minimum; a null capabilities
// field on the wire would fail to satisfy the "at minimum session_id + capabilities[]"
// requirement of HC-039.
func TestReadyState_AgentReadyMsgCapabilitiesIsSliceNotNull(t *testing.T) {
	t.Parallel()

	msg := handlercontract.AgentReadyMsg{
		Type:         "agent_ready",
		SessionID:    "s",
		Capabilities: []string{}, // empty but non-nil
	}

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	capRaw, ok := raw["capabilities"]
	if !ok {
		t.Fatal("capabilities key missing from JSON")
	}
	// An empty Go slice marshals as "[]" (JSON array), not "null".
	if capRaw == nil {
		t.Error("capabilities marshalled as null; want JSON array (even when empty)")
	}
}
