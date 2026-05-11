package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// rateLimitFixtureRunID returns a non-nil RunID for AgentRateLimitStatusPayload tests.
func rateLimitFixtureRunID(t *testing.T) RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return RunID(id)
}

// rateLimitFixtureSource returns a pointer to a valid RateLimitSource for use in tests.
func rateLimitFixtureSource(s RateLimitSource) *RateLimitSource {
	return &s
}

func TestAgentRateLimitStatusPayloadValid(t *testing.T) {
	t.Parallel()

	runID := rateLimitFixtureRunID(t)

	tests := []struct {
		name  string
		p     AgentRateLimitStatusPayload
		valid bool
	}{
		{
			name: "minimal valid active with source",
			p: AgentRateLimitStatusPayload{
				RunID:           runID,
				SessionID:       "sess-1",
				Status:          AgentRateLimitStatusActive,
				RateLimitSource: rateLimitFixtureSource(RateLimitSourceAnthropic),
				ChangedAt:       "2026-05-10T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "minimal valid active no source",
			p: AgentRateLimitStatusPayload{
				RunID:     runID,
				SessionID: "sess-1",
				Status:    AgentRateLimitStatusActive,
				ChangedAt: "2026-05-10T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "minimal valid cleared",
			p: AgentRateLimitStatusPayload{
				RunID:     runID,
				SessionID: "sess-1",
				Status:    AgentRateLimitStatusCleared,
				ChangedAt: "2026-05-10T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "open vocabulary source valid",
			p: AgentRateLimitStatusPayload{
				RunID:           runID,
				SessionID:       "sess-1",
				Status:          AgentRateLimitStatusActive,
				RateLimitSource: rateLimitFixtureSource("vertex-ai"),
				ChangedAt:       "2026-05-10T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: AgentRateLimitStatusPayload{
				RunID:     RunID(uuid.Nil),
				SessionID: "sess-1",
				Status:    AgentRateLimitStatusActive,
				ChangedAt: "2026-05-10T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty session_id rejected",
			p: AgentRateLimitStatusPayload{
				RunID:     runID,
				SessionID: "",
				Status:    AgentRateLimitStatusActive,
				ChangedAt: "2026-05-10T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "invalid status rejected",
			p: AgentRateLimitStatusPayload{
				RunID:     runID,
				SessionID: "sess-1",
				Status:    AgentRateLimitStatus("unknown"),
				ChangedAt: "2026-05-10T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "invalid rate_limit_source (uppercase) rejected",
			p: AgentRateLimitStatusPayload{
				RunID:           runID,
				SessionID:       "sess-1",
				Status:          AgentRateLimitStatusActive,
				RateLimitSource: rateLimitFixtureSource("Anthropic"),
				ChangedAt:       "2026-05-10T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "invalid rate_limit_source (underscore) rejected",
			p: AgentRateLimitStatusPayload{
				RunID:           runID,
				SessionID:       "sess-1",
				Status:          AgentRateLimitStatusActive,
				RateLimitSource: rateLimitFixtureSource("anthropic_api"),
				ChangedAt:       "2026-05-10T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "negative retry_after_seconds rejected",
			p: AgentRateLimitStatusPayload{
				RunID:             runID,
				SessionID:         "sess-1",
				Status:            AgentRateLimitStatusActive,
				RetryAfterSeconds: func() *int { v := -1; return &v }(),
				ChangedAt:         "2026-05-10T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty changed_at rejected",
			p: AgentRateLimitStatusPayload{
				RunID:     runID,
				SessionID: "sess-1",
				Status:    AgentRateLimitStatusActive,
				ChangedAt: "",
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("AgentRateLimitStatusPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestAgentRateLimitStatusPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := rateLimitFixtureRunID(t)
	retrySeconds := 60

	original := AgentRateLimitStatusPayload{
		RunID:             runID,
		SessionID:         "sess-abc",
		Status:            AgentRateLimitStatusActive,
		RateLimitSource:   rateLimitFixtureSource(RateLimitSourceAnthropic),
		RetryAfterSeconds: &retrySeconds,
		ChangedAt:         "2026-05-10T12:34:56.789Z",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded AgentRateLimitStatusPayload
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
	if decoded.Status != original.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, original.Status)
	}
	if decoded.RateLimitSource == nil || *decoded.RateLimitSource != *original.RateLimitSource {
		t.Errorf("RateLimitSource: got %v, want %v", decoded.RateLimitSource, original.RateLimitSource)
	}
	if decoded.RetryAfterSeconds == nil || *decoded.RetryAfterSeconds != *original.RetryAfterSeconds {
		t.Errorf("RetryAfterSeconds: got %v, want %v", decoded.RetryAfterSeconds, original.RetryAfterSeconds)
	}
	if decoded.ChangedAt != original.ChangedAt {
		t.Errorf("ChangedAt: got %q, want %q", decoded.ChangedAt, original.ChangedAt)
	}
}

func TestAgentRateLimitStatusPayloadSourceOmittedWhenNil(t *testing.T) {
	t.Parallel()

	runID := rateLimitFixtureRunID(t)

	p := AgentRateLimitStatusPayload{
		RunID:     runID,
		SessionID: "sess-1",
		Status:    AgentRateLimitStatusCleared,
		ChangedAt: "2026-05-10T00:00:00.000Z",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, exists := m["rate_limit_source"]; exists {
		t.Error("rate_limit_source key present when RateLimitSource is nil; want omitted")
	}
}

func TestAgentRateLimitStatusPayloadInvalidSourceRejectedOnDecode(t *testing.T) {
	t.Parallel()

	// Encoding that carries an invalid rate_limit_source value (uppercase)
	// must be rejected during JSON decode because RateLimitSource.UnmarshalText
	// rejects it.
	raw := `{
		"run_id": "018f4a1d-0000-7000-8000-000000000000",
		"session_id": "sess-x",
		"status": "active",
		"rate_limit_source": "Anthropic",
		"changed_at": "2026-05-10T00:00:00.000Z"
	}`

	var p AgentRateLimitStatusPayload
	if err := json.Unmarshal([]byte(raw), &p); err == nil {
		t.Error("expected error decoding invalid rate_limit_source, got nil")
	}
}
