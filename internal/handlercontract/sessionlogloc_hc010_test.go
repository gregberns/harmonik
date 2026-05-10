package handlercontract_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// sessionLogLocFixture — per-bead helper prefix for test helpers in this file
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.11).

// sessionLogLocFixturePtr returns a pointer to s (helper for *string fields).
func sessionLogLocFixturePtr(s string) *string { return &s }

// ─────────────────────────────────────────────────────────────────────────────
// HC-010 — SessionLogLocationTimeout value
// ─────────────────────────────────────────────────────────────────────────────

// TestSessionLogLoc_TimeoutValue verifies that SessionLogLocationTimeout equals
// 10 seconds as required by specs/handler-contract.md §7.2.
func TestSessionLogLoc_TimeoutValue(t *testing.T) {
	t.Parallel()

	const want = 10 * time.Second
	if handlercontract.SessionLogLocationTimeout != want {
		t.Errorf("SessionLogLocationTimeout = %v, want %v (HC-010 §7.2)", handlercontract.SessionLogLocationTimeout, want)
	}
}

// TestSessionLogLoc_TimeoutPositive verifies the timeout is strictly positive.
func TestSessionLogLoc_TimeoutPositive(t *testing.T) {
	t.Parallel()

	if handlercontract.SessionLogLocationTimeout <= 0 {
		t.Errorf("SessionLogLocationTimeout = %v; must be > 0", handlercontract.SessionLogLocationTimeout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-010 — SessionLogLocationMsg type field
// ─────────────────────────────────────────────────────────────────────────────

// TestSessionLogLoc_MsgTypeField verifies that Type matches
// ProgressMsgTypeSessionLogLocation per §4.2.HC-010.
func TestSessionLogLoc_MsgTypeField(t *testing.T) {
	t.Parallel()

	msg := handlercontract.SessionLogLocationMsg{
		Type:      handlercontract.ProgressMsgTypeSessionLogLocation,
		SessionID: "s",
		RunID:     "r",
		NodeID:    "n",
		AgentType: "claude",
		LogPath:   "/tmp/log",
		LogFormat: "jsonl",
	}

	if msg.Type != handlercontract.ProgressMsgTypeSessionLogLocation {
		t.Errorf("SessionLogLocationMsg.Type = %q, want ProgressMsgTypeSessionLogLocation (%q)",
			msg.Type, handlercontract.ProgressMsgTypeSessionLogLocation)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-010 — SessionLogLocationMsg JSON round-trip
// ─────────────────────────────────────────────────────────────────────────────

// TestSessionLogLoc_MsgRoundTripMinimal verifies round-trip with no optional
// fields (bead_id absent).
func TestSessionLogLoc_MsgRoundTripMinimal(t *testing.T) {
	t.Parallel()

	orig := handlercontract.SessionLogLocationMsg{
		Type:      "session_log_location",
		SessionID: "sess-1",
		RunID:     "run-1",
		NodeID:    "node-1",
		AgentType: "claude",
		LogPath:   "/var/harmonik/sessions/sess-1/session.log",
		LogFormat: "jsonl",
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got handlercontract.SessionLogLocationMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Type != orig.Type {
		t.Errorf("Type: got %q, want %q", got.Type, orig.Type)
	}
	if got.SessionID != orig.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, orig.SessionID)
	}
	if got.RunID != orig.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, orig.RunID)
	}
	if got.NodeID != orig.NodeID {
		t.Errorf("NodeID: got %q, want %q", got.NodeID, orig.NodeID)
	}
	if got.AgentType != orig.AgentType {
		t.Errorf("AgentType: got %q, want %q", got.AgentType, orig.AgentType)
	}
	if got.LogPath != orig.LogPath {
		t.Errorf("LogPath: got %q, want %q", got.LogPath, orig.LogPath)
	}
	if got.LogFormat != orig.LogFormat {
		t.Errorf("LogFormat: got %q, want %q", got.LogFormat, orig.LogFormat)
	}
	if got.BeadID != nil {
		t.Errorf("BeadID: got %v, want nil (omitempty)", got.BeadID)
	}
}

// TestSessionLogLoc_MsgRoundTripWithBeadID verifies round-trip when the optional
// bead_id field is present.
func TestSessionLogLoc_MsgRoundTripWithBeadID(t *testing.T) {
	t.Parallel()

	orig := handlercontract.SessionLogLocationMsg{
		Type:      "session_log_location",
		SessionID: "sess-2",
		RunID:     "run-2",
		NodeID:    "node-2",
		AgentType: "claude",
		LogPath:   "/var/harmonik/sessions/sess-2/session.log",
		LogFormat: "text",
		BeadID:    sessionLogLocFixturePtr("hk-8i31.11"),
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got handlercontract.SessionLogLocationMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BeadID == nil || *got.BeadID != "hk-8i31.11" {
		t.Errorf("BeadID: got %v, want %q", got.BeadID, "hk-8i31.11")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-010 — SessionLogLocationMsg wire field names
// ─────────────────────────────────────────────────────────────────────────────

// TestSessionLogLoc_MsgWireFieldNames verifies that all 7 required fields (plus
// the optional bead_id when set) use the on-wire names mandated by
// specs/handler-contract.md §4.2.HC-010.
func TestSessionLogLoc_MsgWireFieldNames(t *testing.T) {
	t.Parallel()

	msg := handlercontract.SessionLogLocationMsg{
		Type:      "session_log_location",
		SessionID: "s",
		RunID:     "r",
		NodeID:    "n",
		AgentType: "claude",
		LogPath:   "/tmp/l",
		LogFormat: "jsonl",
		BeadID:    sessionLogLocFixturePtr("b"),
	}

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	required := []string{
		"type", "session_id", "run_id", "node_id",
		"agent_type", "log_path", "log_format", "bead_id",
	}
	for _, k := range required {
		if _, ok := raw[k]; !ok {
			t.Errorf("expected JSON key %q in SessionLogLocationMsg", k)
		}
	}
}

// TestSessionLogLoc_MsgBeadIDOmittedWhenNil verifies that bead_id is omitted
// from the JSON when nil (omitempty) per §4.2.HC-010's "bead_id?" notation.
func TestSessionLogLoc_MsgBeadIDOmittedWhenNil(t *testing.T) {
	t.Parallel()

	msg := handlercontract.SessionLogLocationMsg{
		Type:      "session_log_location",
		SessionID: "s",
		RunID:     "r",
		NodeID:    "n",
		AgentType: "claude",
		LogPath:   "/tmp/l",
		LogFormat: "jsonl",
		// BeadID intentionally nil
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["bead_id"]; ok {
		t.Error("bead_id should be omitted when nil (omitempty)")
	}
}
