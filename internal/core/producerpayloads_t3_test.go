package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProducerPayloadsDecodeThroughRegistry(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		payload   EventPayload
	}{
		{
			name:      "liveness halt",
			eventType: EventTypeLivenessHalt,
			payload: &LivenessHaltPayload{
				ConsecutiveZeroCycles: 3,
				LivenessNoProgressN:   3,
			},
		},
		{
			name:      "stale open bead detected",
			eventType: EventTypeStaleOpenBeadDetected,
			payload: &StaleOpenBeadDetectedPayload{
				BeadID:    BeadID("hk-test"),
				CommitSHA: "0123456789abcdef",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			decoded, err := (Event{Type: test.eventType, Payload: raw}).DecodePayload()
			if err != nil {
				t.Fatalf("DecodePayload: %v", err)
			}
			if reflect.TypeOf(decoded) != reflect.TypeOf(test.payload) {
				t.Fatalf("DecodePayload type = %T, want %T", decoded, test.payload)
			}
			if !reflect.DeepEqual(decoded, test.payload) {
				t.Errorf("DecodePayload = %#v, want %#v", decoded, test.payload)
			}
		})
	}
}
