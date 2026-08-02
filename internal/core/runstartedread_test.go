package core

import (
	"encoding/json"
	"testing"
)

func TestDecodeRunStartedForReadUsesStrictV2Decoder(t *testing.T) {
	current := runStartedV2Fixture()
	raw, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("marshal current payload: %v", err)
	}

	got, err := DecodeRunStartedForRead(Event{SchemaVersion: 2, Payload: raw})
	if err != nil {
		t.Fatalf("decode current payload: %v", err)
	}
	if got.RunID != current.RunID || got.BeadID != "" {
		t.Fatalf("current read payload = %#v, want run_id %s and empty bead_id", got, current.RunID)
	}

	legacy, err := json.Marshal(map[string]any{
		"run_id":         current.RunID.String(),
		"bead_id":        "hk-old",
		"workspace_path": "/tmp/workspace",
		"started_at":     "2026-08-02T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal legacy payload: %v", err)
	}
	if _, err := DecodeRunStartedForRead(Event{SchemaVersion: 2, Payload: legacy}); err == nil {
		t.Fatal("version-2 read accepted the legacy payload through a fallback")
	}
}

func TestDecodeRunStartedForReadConvertsVersion1(t *testing.T) {
	raw := json.RawMessage(`{"run_id":"01942b3c-0000-7000-8000-000000000020","bead_id":"hk-old","workspace_path":"/tmp/workspace","started_at":"2026-08-02T12:00:00Z","queue_id":"queue-a","queue_group_index":1}`)

	got, err := DecodeRunStartedForRead(Event{SchemaVersion: 1, Payload: raw})
	if err != nil {
		t.Fatalf("decode version-1 payload: %v", err)
	}
	if got.BeadID != "hk-old" || got.QueueID == nil || *got.QueueID != "queue-a" || got.QueueGroupIndex == nil || *got.QueueGroupIndex != 1 {
		t.Fatalf("legacy read payload = %#v, want converted queue metadata", got)
	}
}
