package core

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RunStartedReadPayload is the small, writer-independent form used by
// historical readers. It carries only fields that replay and restart
// reconciliation need from a run_started event.
//
// It is a read model. New event writers must use RunStartedPayload.
type RunStartedReadPayload struct {
	RunID           RunID
	BeadID          string
	QueueID         *string
	QueueGroupIndex *int
}

// DecodeRunStartedForRead decodes the current run_started payload or its one
// supported prior version. Current-version data always passes through the
// strict RunStartedPayload decoder. The version-1 branch is private to this
// read boundary and never supplies a value to a writer.
func DecodeRunStartedForRead(e Event) (RunStartedReadPayload, error) {
	entry, ok := LookupPayloadCompatEntry(EventTypeRunStarted)
	if !ok {
		return RunStartedReadPayload{}, fmt.Errorf("run_started compatibility is not registered")
	}
	switch e.SchemaVersion {
	case entry.CurrentVersion:
		var current RunStartedPayload
		if err := json.Unmarshal(e.Payload, &current); err != nil {
			return RunStartedReadPayload{}, fmt.Errorf("decode version-%d run_started: %w", entry.CurrentVersion, err)
		}
		return RunStartedReadPayload{
			RunID:           current.RunID,
			BeadID:          beadIDString(current.BeadID),
			QueueID:         current.QueueID,
			QueueGroupIndex: current.QueueGroupIndex,
		}, nil
	case entry.PreviousVersion:
		if !entry.CompatWindowHolds {
			return RunStartedReadPayload{}, fmt.Errorf("run_started version-%d compatibility window is closed", entry.PreviousVersion)
		}
		return decodeRunStartedV1(e.Payload)
	default:
		return RunStartedReadPayload{}, fmt.Errorf(
			"unsupported run_started schema version %d (supported %d and %d)",
			e.SchemaVersion, entry.CurrentVersion, entry.PreviousVersion,
		)
	}
}

func beadIDString(id *BeadID) string {
	if id == nil {
		return ""
	}
	return string(*id)
}

type runStartedPayloadV1 struct {
	RunID           RunID   `json:"run_id"`
	BeadID          string  `json:"bead_id"`
	WorkspacePath   string  `json:"workspace_path"`
	StartedAt       string  `json:"started_at"`
	QueueID         *string `json:"queue_id,omitempty"`
	QueueGroupIndex *int    `json:"queue_group_index,omitempty"`
}

func decodeRunStartedV1(raw json.RawMessage) (RunStartedReadPayload, error) {
	var legacy runStartedPayloadV1
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return RunStartedReadPayload{}, fmt.Errorf("decode version-1 run_started: %w", err)
	}
	if uuid.UUID(legacy.RunID) == uuid.Nil || legacy.BeadID == "" || legacy.WorkspacePath == "" {
		return RunStartedReadPayload{}, fmt.Errorf("invalid version-1 run_started payload")
	}
	if _, err := time.Parse(time.RFC3339, legacy.StartedAt); err != nil {
		return RunStartedReadPayload{}, fmt.Errorf("invalid version-1 run_started started_at: %w", err)
	}
	if (legacy.QueueID == nil) != (legacy.QueueGroupIndex == nil) ||
		(legacy.QueueGroupIndex != nil && *legacy.QueueGroupIndex < 0) {
		return RunStartedReadPayload{}, fmt.Errorf("invalid version-1 run_started queue routing")
	}
	return RunStartedReadPayload{
		RunID:           legacy.RunID,
		BeadID:          legacy.BeadID,
		QueueID:         legacy.QueueID,
		QueueGroupIndex: legacy.QueueGroupIndex,
	}, nil
}
