package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// sessionLogFixtureRunID returns a non-nil RunID for SessionLogLocationPayload tests.
func sessionLogFixtureRunID(t *testing.T) RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return RunID(id)
}

// sessionLogFixtureBeadID returns a pointer to a BeadID for use in optional-field tests.
func sessionLogFixtureBeadID(s BeadID) *BeadID {
	return &s
}

func TestSessionLogLocationPayloadValid(t *testing.T) {
	t.Parallel()

	runID := sessionLogFixtureRunID(t)

	tests := []struct {
		name  string
		p     SessionLogLocationPayload
		valid bool
	}{
		{
			name: "minimal valid without bead_id",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
			},
			valid: true,
		},
		{
			name: "valid with bead_id",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-2",
				NodeID:    "node-xyz",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-2",
				LogFormat: "jsonl",
				BeadID:    sessionLogFixtureBeadID("hk-abc.123"),
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: SessionLogLocationPayload{
				RunID:     RunID(uuid.Nil),
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
			},
			valid: false,
		},
		{
			name: "empty session_id rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
			},
			valid: false,
		},
		{
			name: "empty node_id rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
			},
			valid: false,
		},
		{
			name: "invalid agent_type rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentType("INVALID"),
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
			},
			valid: false,
		},
		{
			name: "empty agent_type rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentType(""),
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
			},
			valid: false,
		},
		{
			name: "empty log_path rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "",
				LogFormat: "jsonl",
			},
			valid: false,
		},
		{
			name: "empty log_format rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "",
			},
			valid: false,
		},
		{
			name: "empty bead_id pointer rejected",
			p: SessionLogLocationPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				LogPath:   "/workspace/.harmonik/sessions/sess-1",
				LogFormat: "jsonl",
				BeadID:    sessionLogFixtureBeadID(""),
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("SessionLogLocationPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestSessionLogLocationPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := sessionLogFixtureRunID(t)

	original := SessionLogLocationPayload{
		RunID:     runID,
		SessionID: "sess-roundtrip",
		NodeID:    "node-roundtrip",
		AgentType: AgentTypeClaudeCode,
		LogPath:   "/workspace/.harmonik/sessions/sess-roundtrip",
		LogFormat: "jsonl",
		BeadID:    sessionLogFixtureBeadID("hk-round.trip"),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded SessionLogLocationPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.RunID != runID {
		t.Errorf("RunID: got %v, want %v", decoded.RunID, runID)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID: got %q, want %q", decoded.SessionID, original.SessionID)
	}
	if decoded.NodeID != original.NodeID {
		t.Errorf("NodeID: got %q, want %q", decoded.NodeID, original.NodeID)
	}
	if decoded.AgentType != original.AgentType {
		t.Errorf("AgentType: got %q, want %q", decoded.AgentType, original.AgentType)
	}
	if decoded.LogPath != original.LogPath {
		t.Errorf("LogPath: got %q, want %q", decoded.LogPath, original.LogPath)
	}
	if decoded.LogFormat != original.LogFormat {
		t.Errorf("LogFormat: got %q, want %q", decoded.LogFormat, original.LogFormat)
	}
	if decoded.BeadID == nil || *decoded.BeadID != *original.BeadID {
		t.Errorf("BeadID: got %v, want %v", decoded.BeadID, original.BeadID)
	}
}

func TestSessionLogLocationPayloadBeadIDOmittedWhenNil(t *testing.T) {
	t.Parallel()

	runID := sessionLogFixtureRunID(t)

	p := SessionLogLocationPayload{
		RunID:     runID,
		SessionID: "sess-nobead",
		NodeID:    "node-nobead",
		AgentType: AgentTypeClaudeCode,
		LogPath:   "/workspace/.harmonik/sessions/sess-nobead",
		LogFormat: "jsonl",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, exists := m["bead_id"]; exists {
		t.Error("bead_id key present when BeadID is nil; want omitted")
	}
}

func TestSessionLogLocationPayloadConstructorShape(t *testing.T) {
	t.Parallel()

	// Verify the constructor shape as used by registerAgentEvents() without
	// touching the global registry.
	ctor := func() EventPayload { return &SessionLogLocationPayload{} }
	got := ctor()
	if _, ok := got.(*SessionLogLocationPayload); !ok {
		t.Fatalf("constructor returned %T, want *SessionLogLocationPayload", got)
	}
}
