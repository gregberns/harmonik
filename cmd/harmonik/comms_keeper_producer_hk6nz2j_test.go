package main

// comms_keeper_producer_hk6nz2j_test.go — T1 (hk-keeper-delivery-agent-input-6nz2j)
// AIS-019 acceptance: the session-keeper is a recognized comms producer. A
// keeper-sent agent_message (--from keeper, --topic keeper) is durable and
// observable via `harmonik comms log`, carrying producer=keeper and topic=keeper.
//
// --from / --topic are unrestricted free-text on the send surface (no allowlist —
// comms.go resolves `from` from the flag/$HARMONIK_AGENT and passes topic through),
// so "keeper" is accepted and routable with no plumbing change. This test pins the
// observability half of the acceptance (durable keeper agent_message surfaces via
// `comms log` with producer=keeper/topic=keeper) — it does NOT drive `comms send`
// end-to-end (that needs a live daemon socket, integration tier). Reuses the
// comms_log fixture helpers (commsLogFixture / commsLogEvent / captureCommsLog).
//
// Spec ref: agent-input.md §4.10 AIS-019 (draft 05-spec-drafts/agent-input-amendment.md).

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommsLogSurfacesKeeperProducer_hk6nz2j(t *testing.T) {
	ts := "2026-07-18T23:00:00Z"
	lines := []string{
		commsLogEvent("01986000-0000-7000-8000-000000000001", ts, "keeper", "delta", "keeper", "leader warn: restart soon"),
		commsLogEvent("01986000-0000-7000-8000-000000000002", ts, "alice", "bob", "status", "unrelated"),
	}
	dir := commsLogFixture(t, lines)

	// Filter by the keeper producer identity AND the keeper topic: the keeper nudge
	// surfaces, the unrelated message does not.
	out, code := captureCommsLog(t, []string{"--project", dir, "--from", "keeper", "--topic", "keeper", "--json"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.Contains(out, "unrelated") {
		t.Errorf("non-keeper message leaked through --from keeper --topic keeper: %q", out)
	}

	jsonLines := strings.Split(strings.TrimSpace(out), "\n")
	if len(jsonLines) != 1 {
		t.Fatalf("expected exactly 1 keeper message, got %d: %q", len(jsonLines), out)
	}

	var ev struct {
		Type    string `json:"type"`
		Payload struct {
			From  string `json:"from"`
			Topic string `json:"topic"`
			Body  string `json:"body"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(jsonLines[0]), &ev); err != nil {
		t.Fatalf("keeper log line is not valid JSON: %v — %q", err, jsonLines[0])
	}
	if ev.Type != "agent_message" {
		t.Errorf("type = %q, want agent_message", ev.Type)
	}
	if ev.Payload.From != "keeper" {
		t.Errorf("producer = %q, want keeper", ev.Payload.From)
	}
	if ev.Payload.Topic != "keeper" {
		t.Errorf("topic = %q, want keeper", ev.Payload.Topic)
	}
	if ev.Payload.Body != "leader warn: restart soon" {
		t.Errorf("body = %q, want the keeper nudge body", ev.Payload.Body)
	}
}
