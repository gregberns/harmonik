package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCompleteAndUnlinkPersistsCompletedBeforeUnlink proves the completed
// queue reaches the canonical file before cleanup starts.
func TestCompleteAndUnlinkPersistsCompletedBeforeUnlink(t *testing.T) {
	projectDir := t.TempDir()
	q := Queue{
		Name:   QueueNameMain,
		Status: QueueStatusActive,
		Groups: []Group{{
			GroupIndex: 0,
			Status:     GroupStatusCompleteSuccess,
		}},
	}

	stopAfterObservation := errors.New("stop after persistence observation")
	result := completeAndUnlinkResult(context.Background(), projectDir, &q, func(_ context.Context, dir, name string) error {
		//nolint:gosec // G304: dir and name are supplied by completeAndUnlinkResult from the test fixture.
		data, err := os.ReadFile(filepath.Join(dir, ".harmonik", queuesSubDir, name+".json"))
		if err != nil {
			t.Fatalf("read canonical queue before unlink: %v", err)
		}
		var persisted Queue
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatalf("decode canonical queue before unlink: %v", err)
		}
		if persisted.Status != QueueStatusCompleted {
			t.Fatalf("canonical queue status before unlink = %q, want %q", persisted.Status, QueueStatusCompleted)
		}
		return stopAfterObservation
	})

	if !result.Committed || result.CommitErr != nil || !errors.Is(result.CleanupErr, stopAfterObservation) {
		t.Fatalf("result = %+v, want committed cleanup observation failure", result)
	}
}
