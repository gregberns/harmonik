package queue_test

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestNewEventIntentDetachesPayloadBytes(t *testing.T) {
	t.Parallel()

	payload := &core.QueueAppendedPayload{
		QueueID:         "queue-1",
		GroupIndex:      2,
		AppendedBeadIDs: []string{"bead-1"},
		AppendedAt:      "2026-08-10T12:00:00Z",
	}
	intent, err := queue.NewEventIntent(core.EventTypeQueueAppended, payload)
	if err != nil {
		t.Fatalf("NewEventIntent: %v", err)
	}

	want := json.RawMessage(`{"queue_id":"queue-1","group_index":2,"appended_bead_ids":["bead-1"],"appended_at":"2026-08-10T12:00:00Z"}`)
	if string(intent.Payload) != string(want) {
		t.Fatalf("payload = %s, want %s", intent.Payload, want)
	}
	payload.AppendedBeadIDs[0] = "changed"
	payload.AppendedAt = "changed"

	if intent.Type != core.EventTypeQueueAppended {
		t.Fatalf("type = %q, want %q", intent.Type, core.EventTypeQueueAppended)
	}
	if string(intent.Payload) != string(want) {
		t.Fatalf("payload changed after source mutation: got %s want %s", intent.Payload, want)
	}
}
