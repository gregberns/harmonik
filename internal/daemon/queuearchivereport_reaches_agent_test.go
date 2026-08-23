package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

// TestOrphanSweepPayload_CarriesTheArchiveReport asserts the archive counts
// survive OrphanSweepResult.ToPayload and appear under the JSON names an agent
// reads from events.jsonl.
func TestOrphanSweepPayload_CarriesTheArchiveReport(t *testing.T) {
	result := OrphanSweepResult{
		QueueArchivesObserved:      68,
		QueueArchiveBytes:          4096,
		QueueArchivesOverRetention: 3,
		SweptAt:                    time.Date(2026, 5, 19, 16, 14, 37, 0, time.UTC),
	}

	payload := result.ToPayload()
	if payload.QueueArchivesObserved != 68 {
		t.Errorf("payload.QueueArchivesObserved = %d; want 68", payload.QueueArchivesObserved)
	}
	if payload.QueueArchiveBytes != 4096 {
		t.Errorf("payload.QueueArchiveBytes = %d; want 4096", payload.QueueArchiveBytes)
	}
	if payload.QueueArchivesOverRetention != 3 {
		t.Errorf("payload.QueueArchivesOverRetention = %d; want 3", payload.QueueArchivesOverRetention)
	}
	if !payload.Valid() {
		t.Error("payload.Valid() = false; a well-formed archive report must validate")
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded map[string]any
	if unmarshalErr := json.Unmarshal(encoded, &decoded); unmarshalErr != nil {
		t.Fatalf("unmarshal payload: %v", unmarshalErr)
	}
	for field, want := range map[string]float64{
		"queue_archives_observed":       68,
		"queue_archive_bytes":           4096,
		"queue_archives_over_retention": 3,
	} {
		got, present := decoded[field]
		if !present {
			t.Fatalf("field %q is absent from the emitted event; the finding never reaches the agent", field)
		}
		if got != want {
			t.Errorf("field %q = %v; want %v", field, got, want)
		}
	}
}
