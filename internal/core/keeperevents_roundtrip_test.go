package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

// keeperevents_roundtrip_test.go — registry round-trip tests for the two
// previously-unregistered keeper event types (hk-wqdc).
//
// Both session_keeper_live_pane_recover and session_keeper_ack_timeout are
// registered in registerKeeperEvents() (eventreg_hqwn59.go). These tests
// assert that the global registry maps each type name to the correct payload
// constructor and that a JSON round-trip decodes to the right concrete type.

func TestKeeperEvents_LivePaneRecover_RoundTrip(t *testing.T) {
	want := &SessionKeeperLivePaneRecoverPayload{
		AgentName:    "test-agent",
		SessionID:    "550e8400-e29b-41d4-a716-446655440000",
		StaleSeconds: 120,
		Outcome:      "ok",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	ev := minimalEvent(t, "session_keeper_live_pane_recover", raw)
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
}

func TestKeeperEvents_AckTimeout_RoundTrip(t *testing.T) {
	want := &SessionKeeperAckTimeoutPayload{
		AgentName:      "test-agent",
		Nonce:          "abc123",
		Kind:           "restart",
		TimeoutSeconds: 30.0,
		TmuxTarget:     "hk-captain:0",
		Reason:         "ack_not_observed",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	ev := minimalEvent(t, "session_keeper_ack_timeout", raw)
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
}
