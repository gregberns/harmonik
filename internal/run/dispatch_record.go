package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
)

const dispatchRecordSchemaVersion = 4

// ExecutionKind identifies where one run executes.
type ExecutionKind string

const (
	ExecutionLocalIndependent ExecutionKind = "local_independent"
	ExecutionLocalShared      ExecutionKind = "local_shared"
	ExecutionRemote           ExecutionKind = "remote"
)

// ExecutionLocation binds a run to the selected local host or remote worker.
type ExecutionLocation struct {
	Kind           ExecutionKind `json:"kind"`
	WorkerName     string        `json:"worker_name,omitempty"`
	Transport      string        `json:"transport,omitempty"`
	Host           string        `json:"host,omitempty"`
	RepositoryPath string        `json:"repository_path"`
}

// DispatchRecord is the durable identity for every claimed queue run.
type DispatchRecord struct {
	SchemaVersion     int                `json:"schema_version"`
	RunID             core.RunID         `json:"run_id"`
	BeadID            core.BeadID        `json:"bead_id"`
	QueueName         string             `json:"queue_name"`
	QueueID           string             `json:"queue_id"`
	GroupIndex        int                `json:"group_index"`
	ItemIndex         int                `json:"item_index"`
	ClaimTransitionID core.TransitionID  `json:"claim_transition_id"`
	ParentCommit      string             `json:"parent_commit"`
	RepositoryPath    string             `json:"repository_path"`
	Location          *ExecutionLocation `json:"execution_location,omitempty"`
	SessionName       string             `json:"session_name,omitempty"`
	WindowName        string             `json:"window_name,omitempty"`
	StartedAt         time.Time          `json:"started_at"`
}

// NewDispatchRecord returns the base durable record for one claimed dispatch.
func NewDispatchRecord(binding dispatch.Binding, startedAt time.Time) (DispatchRecord, error) {
	record := DispatchRecord{
		SchemaVersion:     dispatchRecordSchemaVersion,
		RunID:             binding.RunID,
		BeadID:            binding.BeadID,
		QueueName:         binding.QueueName,
		QueueID:           binding.QueueID,
		GroupIndex:        binding.GroupIndex,
		ItemIndex:         binding.ItemIndex,
		ClaimTransitionID: binding.ClaimTransitionID,
		ParentCommit:      binding.ParentCommit,
		RepositoryPath:    binding.RepositoryPath,
		StartedAt:         startedAt.UTC().Truncate(time.Millisecond),
	}
	if err := record.Validate(); err != nil {
		return DispatchRecord{}, err
	}
	return record, nil
}

// Validate rejects partial records and non-canonical durable identity.
func (r DispatchRecord) Validate() error {
	if r.SchemaVersion != dispatchRecordSchemaVersion {
		return fmt.Errorf("run: dispatch record schema_version must be %d", dispatchRecordSchemaVersion)
	}
	if uuid.UUID(r.RunID).Version() != 7 || r.RunID.String() != uuid.UUID(r.RunID).String() {
		return errors.New("run: dispatch record run_id must be canonical UUIDv7")
	}
	if r.BeadID == "" {
		return errors.New("run: dispatch record bead_id is required")
	}
	if ok, detail := queue.ValidateQueueName(r.QueueName); !ok || queue.NormaliseQueueName(r.QueueName) != r.QueueName {
		return fmt.Errorf("run: dispatch record queue_name is not normalized: %s", detail)
	}
	queueID, err := uuid.Parse(r.QueueID)
	if err != nil || queueID.Version() != 7 || queueID.String() != r.QueueID {
		return errors.New("run: dispatch record queue_id must be canonical UUIDv7")
	}
	if r.GroupIndex < 0 || r.ItemIndex < 0 {
		return errors.New("run: dispatch record indexes must be non-negative")
	}
	if !r.ClaimTransitionID.IsUUIDv7() {
		return errors.New("run: dispatch record claim_transition_id must be UUIDv7")
	}
	if err := dispatch.ValidateParentCommit(r.ParentCommit); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if r.RepositoryPath == "" || !filepath.IsAbs(r.RepositoryPath) || filepath.Clean(r.RepositoryPath) != r.RepositoryPath {
		return errors.New("run: dispatch record repository_path must be a clean absolute path")
	}
	if r.Location != nil {
		if err := r.Location.validate(); err != nil {
			return err
		}
		if err := r.Location.validateRepository(r.RepositoryPath); err != nil {
			return err
		}
	}
	if (r.SessionName != "" || r.WindowName != "") && r.Location == nil {
		return errors.New("run: dispatch record cannot bind a target before execution location")
	}
	if (r.SessionName == "") != (r.WindowName == "") {
		return errors.New("run: dispatch record session_name and window_name must be bound together")
	}
	for _, target := range []struct{ field, value string }{
		{field: "session_name", value: r.SessionName},
		{field: "window_name", value: r.WindowName},
	} {
		if target.value != "" &&
			(strings.TrimSpace(target.value) != target.value || strings.ContainsAny(target.value, "\x00\r\n")) {
			return fmt.Errorf("run: dispatch record %s is invalid", target.field)
		}
	}
	return validateStartedAt(r.StartedAt)
}

// BindLocation returns the placement candidate without changing base facts.
func (r DispatchRecord) BindLocation(location ExecutionLocation) (DispatchRecord, error) {
	if err := r.Validate(); err != nil {
		return DispatchRecord{}, err
	}
	if err := location.validate(); err != nil {
		return DispatchRecord{}, err
	}
	if err := location.validateRepository(r.RepositoryPath); err != nil {
		return DispatchRecord{}, err
	}
	if r.Location != nil && *r.Location != location {
		return DispatchRecord{}, errors.New("run: durable execution location cannot change")
	}
	r.Location = &location
	return r, nil
}

// BindSession returns the exact handoff candidate without changing base facts.
func (r DispatchRecord) BindSession(sessionName, windowName string) (DispatchRecord, error) {
	if err := r.Validate(); err != nil {
		return DispatchRecord{}, err
	}
	if sessionName == "" {
		return DispatchRecord{}, errors.New("run: session_name is required for handoff")
	}
	if windowName == "" {
		return DispatchRecord{}, errors.New("run: window_name is required for handoff")
	}
	if r.Location == nil {
		return DispatchRecord{}, errors.New("run: execution location is required before handoff")
	}
	if r.SessionName != "" && (r.SessionName != sessionName || r.WindowName != windowName) {
		return DispatchRecord{}, errors.New("run: durable target identity cannot change")
	}
	location := *r.Location
	r.Location = &location
	r.SessionName = sessionName
	r.WindowName = windowName
	if err := r.Validate(); err != nil {
		return DispatchRecord{}, err
	}
	return r, nil
}

func (l ExecutionLocation) validate() error {
	switch l.Kind {
	case ExecutionLocalIndependent, ExecutionLocalShared:
		if l.WorkerName != "" || l.Transport != "" || l.Host != "" {
			return errors.New("run: local execution cannot name a worker")
		}
	case ExecutionRemote:
		if l.WorkerName == "" || l.Transport == "" || l.Host == "" {
			return errors.New("run: remote execution requires worker_name, transport, and host")
		}
	default:
		return fmt.Errorf("run: invalid execution kind %q", l.Kind)
	}
	if l.RepositoryPath == "" || !filepath.IsAbs(l.RepositoryPath) || filepath.Clean(l.RepositoryPath) != l.RepositoryPath {
		return errors.New("run: execution repository_path must be a clean absolute path")
	}
	return nil
}

func (l ExecutionLocation) validateRepository(activeRepositoryPath string) error {
	if (l.Kind == ExecutionLocalIndependent || l.Kind == ExecutionLocalShared) && l.RepositoryPath != activeRepositoryPath {
		return errors.New("run: local execution repository_path must match the dispatch record")
	}
	return nil
}

func validateStartedAt(value time.Time) error {
	if value.IsZero() || value.Location() != time.UTC || value.Nanosecond()%int(time.Millisecond) != 0 {
		return errors.New("run: started_at must be a nonzero UTC millisecond timestamp")
	}
	if value.Year() < 1 || value.Year() > 9999 {
		return errors.New("run: started_at is outside the RFC3339 range")
	}
	return nil
}

// MarshalJSON validates the record and writes one canonical timestamp shape.
func (r DispatchRecord) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	wire := dispatchRecordWire{
		SchemaVersion:     r.SchemaVersion,
		RunID:             r.RunID.String(),
		BeadID:            string(r.BeadID),
		QueueName:         r.QueueName,
		QueueID:           r.QueueID,
		GroupIndex:        &r.GroupIndex,
		ItemIndex:         &r.ItemIndex,
		ClaimTransitionID: r.ClaimTransitionID.String(),
		ParentCommit:      r.ParentCommit,
		RepositoryPath:    r.RepositoryPath,
		Location:          r.Location,
		SessionName:       r.SessionName,
		WindowName:        r.WindowName,
		StartedAt:         r.StartedAt.Format(time.RFC3339Nano),
	}
	return json.Marshal(wire)
}

// UnmarshalJSON rejects unknown fields and non-canonical wire values.
func (r *DispatchRecord) UnmarshalJSON(data []byte) error {
	if r == nil {
		return errors.New("run: unmarshal into nil dispatch record")
	}
	var wire dispatchRecordWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return fmt.Errorf("run: decode dispatch record: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("run: dispatch record must contain one JSON value")
	}
	value, err := wire.record()
	if err != nil {
		return err
	}
	*r = value
	return nil
}

type dispatchRecordWire struct {
	SchemaVersion     int                `json:"schema_version"`
	RunID             string             `json:"run_id"`
	BeadID            string             `json:"bead_id"`
	QueueName         string             `json:"queue_name"`
	QueueID           string             `json:"queue_id"`
	GroupIndex        *int               `json:"group_index"`
	ItemIndex         *int               `json:"item_index"`
	ClaimTransitionID string             `json:"claim_transition_id"`
	ParentCommit      string             `json:"parent_commit"`
	RepositoryPath    string             `json:"repository_path"`
	Location          *ExecutionLocation `json:"execution_location,omitempty"`
	SessionName       string             `json:"session_name,omitempty"`
	WindowName        string             `json:"window_name,omitempty"`
	StartedAt         string             `json:"started_at"`
}

func (w dispatchRecordWire) record() (DispatchRecord, error) {
	if w.GroupIndex == nil || w.ItemIndex == nil {
		return DispatchRecord{}, errors.New("run: dispatch record indexes are required")
	}
	runID, err := uuid.Parse(w.RunID)
	if err != nil || runID.Version() != 7 || runID.String() != w.RunID {
		return DispatchRecord{}, errors.New("run: dispatch record run_id must be canonical UUIDv7")
	}
	transitionID, err := uuid.Parse(w.ClaimTransitionID)
	if err != nil || transitionID.Version() != 7 || transitionID.String() != w.ClaimTransitionID {
		return DispatchRecord{}, errors.New("run: dispatch record claim_transition_id must be canonical UUIDv7")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, w.StartedAt)
	if err != nil || startedAt.Format(time.RFC3339Nano) != w.StartedAt {
		return DispatchRecord{}, errors.New("run: dispatch record started_at must be canonical RFC3339")
	}
	value := DispatchRecord{
		SchemaVersion:     w.SchemaVersion,
		RunID:             core.RunID(runID),
		BeadID:            core.BeadID(w.BeadID),
		QueueName:         w.QueueName,
		QueueID:           w.QueueID,
		GroupIndex:        *w.GroupIndex,
		ItemIndex:         *w.ItemIndex,
		ClaimTransitionID: core.TransitionID(transitionID),
		ParentCommit:      w.ParentCommit,
		RepositoryPath:    w.RepositoryPath,
		Location:          w.Location,
		SessionName:       w.SessionName,
		WindowName:        w.WindowName,
		StartedAt:         startedAt,
	}
	if err := value.Validate(); err != nil {
		return DispatchRecord{}, err
	}
	return value, nil
}
