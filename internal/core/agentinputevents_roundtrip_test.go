package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

// agentinputevents_roundtrip_test.go — registry round-trip tests for the two
// §8.21 agent-input acceptance event types (codename:agent-input-substrate, M2-1
// T3).
//
// Both agent_input_acked and agent_input_stale are registered in
// registerAgentInputEvents() (eventreg_hqwn59.go). These tests assert that the
// global registry maps each type name to the correct payload constructor and
// that a JSON round-trip decodes to the right concrete type — mirroring the
// §8.16/§8.20 keeper round-trip idiom (keeperevents_roundtrip_test.go).

func TestAgentInputEvents_Acked_RoundTrip(t *testing.T) {
	want := &AgentInputAckedPayload{
		RunID:         "run-abc",
		InputSeq:      7,
		AcceptanceRef: "turn-42",
		SessionID:     "550e8400-e29b-41d4-a716-446655440000",
		AckedAt:       "2026-07-14T12:00:00Z",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	ev := minimalEvent(t, "agent_input_acked", raw)
	got, err := ev.DecodePayload()
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		t.Errorf("DecodePayload type mismatch: got %T, want %T", got, want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodePayload value mismatch: got %+v, want %+v", got, want)
	}
	if !want.Valid() {
		t.Errorf("Valid() = false for a well-formed acked payload: %+v", want)
	}
}

func TestAgentInputEvents_Stale_RoundTrip(t *testing.T) {
	want := &AgentInputStalePayload{
		RunID:      "run-abc",
		InputSeq:   7,
		SessionID:  "550e8400-e29b-41d4-a716-446655440000",
		TimedOutAt: "2026-07-14T12:00:30Z",
		Window:     "30s",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	ev := minimalEvent(t, "agent_input_stale", raw)
	got, err := ev.DecodePayload()
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		t.Errorf("DecodePayload type mismatch: got %T, want %T", got, want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodePayload value mismatch: got %+v, want %+v", got, want)
	}
	if !want.Valid() {
		t.Errorf("Valid() = false for a well-formed stale payload: %+v", want)
	}
}

// TestAgentInputEvents_Valid exercises the Valid() preconditions per §8.21.1/2:
// acked requires non-empty run_id + input_seq >= 0; stale additionally requires a
// non-empty timed_out_at.
func TestAgentInputEvents_Valid(t *testing.T) {
	if (AgentInputAckedPayload{RunID: "", InputSeq: 0, AckedAt: "t"}).Valid() {
		t.Error("acked with empty run_id must be invalid")
	}
	if (AgentInputAckedPayload{RunID: "r", InputSeq: -1}).Valid() {
		t.Error("acked with negative input_seq must be invalid")
	}
	if !(AgentInputAckedPayload{RunID: "r", InputSeq: 0}).Valid() {
		t.Error("acked with run_id + input_seq 0 must be valid")
	}
	if (AgentInputStalePayload{RunID: "r", InputSeq: 0, TimedOutAt: ""}).Valid() {
		t.Error("stale with empty timed_out_at must be invalid")
	}
	if !(AgentInputStalePayload{RunID: "r", InputSeq: 0, TimedOutAt: "t"}).Valid() {
		t.Error("stale with run_id + input_seq + timed_out_at must be valid")
	}
}
