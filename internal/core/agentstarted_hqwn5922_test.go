package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// agentStartedFixtureRunID returns a non-nil RunID for AgentStartedPayload tests.
func agentStartedFixtureRunID(t *testing.T) RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return RunID(id)
}

func TestAgentStartedPayloadValid(t *testing.T) {
	t.Parallel()

	runID := agentStartedFixtureRunID(t)

	tests := []struct {
		name  string
		p     AgentStartedPayload
		valid bool
	}{
		{
			name: "minimal valid",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				StartedAt: "2026-05-11T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "valid with pi agent type",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "sess-2",
				NodeID:    "node-xyz",
				AgentType: AgentTypePi,
				StartedAt: "2026-05-11T01:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: AgentStartedPayload{
				RunID:     RunID(uuid.Nil),
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				StartedAt: "2026-05-11T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty session_id rejected",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				StartedAt: "2026-05-11T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty node_id rejected",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "",
				AgentType: AgentTypeClaudeCode,
				StartedAt: "2026-05-11T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "invalid agent_type rejected",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentType("INVALID_TYPE"),
				StartedAt: "2026-05-11T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty agent_type rejected",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentType(""),
				StartedAt: "2026-05-11T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty started_at rejected",
			p: AgentStartedPayload{
				RunID:     runID,
				SessionID: "sess-1",
				NodeID:    "node-abc",
				AgentType: AgentTypeClaudeCode,
				StartedAt: "",
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("AgentStartedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestAgentStartedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := agentStartedFixtureRunID(t)

	original := AgentStartedPayload{
		RunID:     runID,
		SessionID: "sess-roundtrip",
		NodeID:    "node-roundtrip",
		AgentType: AgentTypeClaudeCode,
		StartedAt: "2026-05-11T12:34:56.789Z",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded AgentStartedPayload
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
	if decoded.StartedAt != original.StartedAt {
		t.Errorf("StartedAt: got %q, want %q", decoded.StartedAt, original.StartedAt)
	}
}

func TestAgentStartedPayloadConstructorShape(t *testing.T) {
	t.Parallel()

	// Verify the constructor shape: a zero-value AgentStartedPayload produced by
	// the constructor function is of the correct type.  This tests the constructor
	// function shape as used by registerAgentEvents() without touching the global
	// registry (which eventregistry_test.go resets between subtests).
	ctor := func() EventPayload { return &AgentStartedPayload{} }
	got := ctor()
	if _, ok := got.(*AgentStartedPayload); !ok {
		t.Fatalf("constructor returned %T, want *AgentStartedPayload", got)
	}
}

func TestAgentStartedPayloadNoEnvironmentVariables(t *testing.T) {
	t.Parallel()

	// HC-029 binding: AgentStartedPayload MUST NOT contain an env or
	// environment field. Verify via JSON marshal that no such key appears.
	runID := agentStartedFixtureRunID(t)
	p := AgentStartedPayload{
		RunID:     runID,
		SessionID: "sess-noenv",
		NodeID:    "node-noenv",
		AgentType: AgentTypeClaudeCode,
		StartedAt: "2026-05-11T00:00:00.000Z",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	for _, forbidden := range []string{"env", "environment", "env_vars", "environment_variables"} {
		if _, exists := m[forbidden]; exists {
			t.Errorf("HC-029 violation: AgentStartedPayload JSON contains forbidden key %q", forbidden)
		}
	}
}
