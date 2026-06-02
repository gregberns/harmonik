package core

import (
	"encoding/json"
	"testing"
)

// TestAgentMessagePayloadValid covers the Valid() contract for AgentMessagePayload.
func TestAgentMessagePayloadValid(t *testing.T) {
	t.Parallel()

	inReplyTo := "01900000-0000-7000-0000-000000000001"

	tests := []struct {
		name  string
		p     AgentMessagePayload
		valid bool
	}{
		{
			name:  "minimal valid directed",
			p:     AgentMessagePayload{From: "agent-a", To: "agent-b", Body: "hello"},
			valid: true,
		},
		{
			name:  "valid broadcast",
			p:     AgentMessagePayload{From: "agent-a", To: "*", Body: "hello all"},
			valid: true,
		},
		{
			name:  "valid with optional fields",
			p:     AgentMessagePayload{From: "agent-a", To: "agent-b", Topic: "status", Body: "pong", InReplyTo: &inReplyTo},
			valid: true,
		},
		{
			name:  "missing from",
			p:     AgentMessagePayload{From: "", To: "agent-b", Body: "hello"},
			valid: false,
		},
		{
			name:  "missing to",
			p:     AgentMessagePayload{From: "agent-a", To: "", Body: "hello"},
			valid: false,
		},
		{
			name:  "missing body",
			p:     AgentMessagePayload{From: "agent-a", To: "agent-b", Body: ""},
			valid: false,
		},
		{
			name: "in_reply_to non-nil but empty",
			p: func() AgentMessagePayload {
				empty := ""
				return AgentMessagePayload{From: "a", To: "b", Body: "x", InReplyTo: &empty}
			}(),
			valid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v; want %v", got, tc.valid)
			}
		})
	}
}

// TestAgentPresencePayloadValid covers the Valid() contract for AgentPresencePayload.
func TestAgentPresencePayloadValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		p     AgentPresencePayload
		valid bool
	}{
		{
			name:  "valid online join",
			p:     AgentPresencePayload{Agent: "agent-a", Status: AgentPresenceStatusOnline, LastSeen: "2026-06-01T00:00:00Z", Reason: AgentPresenceReasonJoin},
			valid: true,
		},
		{
			name:  "valid offline leave",
			p:     AgentPresencePayload{Agent: "agent-a", Status: AgentPresenceStatusOffline, LastSeen: "2026-06-01T00:00:00Z", Reason: AgentPresenceReasonLeave},
			valid: true,
		},
		{
			name:  "valid refresh no reason",
			p:     AgentPresencePayload{Agent: "agent-a", Status: AgentPresenceStatusOnline, LastSeen: "2026-06-01T00:00:00Z"},
			valid: true,
		},
		{
			name:  "missing agent",
			p:     AgentPresencePayload{Agent: "", Status: AgentPresenceStatusOnline, LastSeen: "2026-06-01T00:00:00Z"},
			valid: false,
		},
		{
			name:  "invalid status",
			p:     AgentPresencePayload{Agent: "agent-a", Status: "unknown", LastSeen: "2026-06-01T00:00:00Z"},
			valid: false,
		},
		{
			name:  "missing last_seen",
			p:     AgentPresencePayload{Agent: "agent-a", Status: AgentPresenceStatusOnline, LastSeen: ""},
			valid: false,
		},
		{
			name:  "invalid reason",
			p:     AgentPresencePayload{Agent: "agent-a", Status: AgentPresenceStatusOnline, LastSeen: "2026-06-01T00:00:00Z", Reason: "bogus"},
			valid: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v; want %v", got, tc.valid)
			}
		})
	}
}

// TestAgentCommsPayloads_RegistryRoundTrip verifies that agent_message and
// agent_presence are registered in the global event registry and that a
// marshal → DecodePayload round-trip restores the original payload.
func TestAgentCommsPayloads_RegistryRoundTrip(t *testing.T) {
	t.Parallel()

	inReplyTo := "01900000-0000-7000-8000-000000000001"

	t.Run("agent_message", func(t *testing.T) {
		t.Parallel()

		want := AgentMessagePayload{
			From:      "agent-a",
			To:        "agent-b",
			Topic:     "heartbeat",
			Body:      "ping",
			InReplyTo: &inReplyTo,
		}

		payloadJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		evt := Event{Type: "agent_message", Payload: payloadJSON}
		decoded, err := evt.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}

		got, ok := decoded.(*AgentMessagePayload)
		if !ok {
			t.Fatalf("DecodePayload returned %T; want *AgentMessagePayload", decoded)
		}
		if got.From != want.From || got.To != want.To || got.Body != want.Body || got.Topic != want.Topic {
			t.Errorf("round-trip mismatch: got %+v; want %+v", got, want)
		}
		if got.InReplyTo == nil || *got.InReplyTo != inReplyTo {
			t.Errorf("in_reply_to round-trip: got %v; want %q", got.InReplyTo, inReplyTo)
		}
	})

	t.Run("agent_presence", func(t *testing.T) {
		t.Parallel()

		want := AgentPresencePayload{
			Agent:    "agent-a",
			Status:   AgentPresenceStatusOnline,
			LastSeen: "2026-06-01T00:00:00Z",
			Reason:   AgentPresenceReasonJoin,
		}

		payloadJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		evt := Event{Type: "agent_presence", Payload: payloadJSON}
		decoded, err := evt.DecodePayload()
		if err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}

		got, ok := decoded.(*AgentPresencePayload)
		if !ok {
			t.Fatalf("DecodePayload returned %T; want *AgentPresencePayload", decoded)
		}
		if got.Agent != want.Agent || got.Status != want.Status || got.LastSeen != want.LastSeen || got.Reason != want.Reason {
			t.Errorf("round-trip mismatch: got %+v; want %+v", got, want)
		}
	})
}

// TestAgentPresenceStatus_Valid covers the status discriminator.
func TestAgentPresenceStatus_Valid(t *testing.T) {
	t.Parallel()

	if !AgentPresenceStatusOnline.Valid() {
		t.Error("online should be valid")
	}
	if !AgentPresenceStatusOffline.Valid() {
		t.Error("offline should be valid")
	}
	if AgentPresenceStatus("unknown").Valid() {
		t.Error("unknown should not be valid")
	}
}
