package core

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RunStartedPayload is the version-2 durable payload for run_started. The
// resolver creates its descriptor, mode, policy, and source before emission.
type RunStartedPayload struct {
	RunID                   RunID                   `json:"run_id"`
	WorkflowID              WorkflowID              `json:"workflow_id"`
	WorkflowVersion         WorkflowVersion         `json:"workflow_version"`
	WorkflowMode            WorkflowMode            `json:"workflow_mode"`
	ReviewPolicy            ReviewPolicy            `json:"review_policy"`
	WorkflowSelectionSource WorkflowSelectionSource `json:"workflow_selection_source"`
	BeadID                  *BeadID                 `json:"bead_id,omitempty"`
	WorkspacePath           string                  `json:"workspace_path"`
	InputRef                string                  `json:"input_ref"`
	StartedAt               time.Time               `json:"started_at"`
	WorkerName              *string                 `json:"worker_name"`
	WorkerOS                *string                 `json:"worker_os"`
	QueueID                 *string                 `json:"queue_id,omitempty"`
	QueueGroupIndex         *int                    `json:"queue_group_index,omitempty"`
}

// Descriptor returns the immutable graph identity carried by p.
func (p RunStartedPayload) Descriptor() WorkflowDescriptor {
	return WorkflowDescriptor{WorkflowID: p.WorkflowID, WorkflowVersion: p.WorkflowVersion}
}

// UnmarshalJSON accepts only a complete version-2 start record. Historical
// version-1 reads belong to the replay compatibility boundary, not this type.
func (p *RunStartedPayload) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"worker_name", "worker_os"} {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("invalid version-2 run_started payload: missing %s", name)
		}
	}
	type wire RunStartedPayload
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	next := RunStartedPayload(decoded)
	if !next.Valid() {
		return fmt.Errorf("invalid version-2 run_started payload")
	}
	*p = next
	return nil
}

// Valid reports whether p is a complete version-2 start record.
func (p RunStartedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil || !p.Descriptor().Valid() {
		return false
	}
	if p.WorkflowMode != WorkflowModeDot || !p.ReviewPolicy.Valid() || !p.WorkflowSelectionSource.Valid() {
		return false
	}
	if !validRunStartedPolicyBinding(p.Descriptor(), p.ReviewPolicy, p.WorkflowSelectionSource) {
		return false
	}
	if p.BeadID != nil && *p.BeadID == "" {
		return false
	}
	if p.WorkspacePath == "" || p.InputRef == "" || p.StartedAt.IsZero() {
		return false
	}
	if (p.QueueID == nil) != (p.QueueGroupIndex == nil) || (p.QueueGroupIndex != nil && *p.QueueGroupIndex < 0) {
		return false
	}
	return true
}

func validRunStartedPolicyBinding(d WorkflowDescriptor, policy ReviewPolicy, source WorkflowSelectionSource) bool {
	noReviewDescriptor := d.WorkflowID == WorkflowID("no-review-bead") && d.WorkflowVersion == WorkflowVersion("1.0")
	legacySource := source == WorkflowSelectionLegacySingleLabel || source == WorkflowSelectionQueueItemSingleMode
	if policy == ReviewPolicyNoReview {
		return noReviewDescriptor && legacySource
	}
	return policy == ReviewPolicyReviewed && !(noReviewDescriptor && legacySource)
}
