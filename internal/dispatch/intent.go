// Package dispatch owns the durable value contract for one queue-to-run handoff.
package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

const intentSchemaVersion = 1

// Phase is the last dispatch boundary known to be durable.
type Phase string

const (
	// PhasePrepared binds the queue item before the reservation write.
	PhasePrepared Phase = "prepared"
	// PhaseClaimDurable records that Beads accepted the claim.
	PhaseClaimDurable Phase = "claim_durable"
	// PhaseRunDurable records that the durable run record exists.
	PhaseRunDurable Phase = "run_durable"
	// PhaseHandoffDurable records that the session handoff is durable.
	PhaseHandoffDurable Phase = "handoff_durable"
)

func (p Phase) valid() bool {
	switch p {
	case PhasePrepared, PhaseClaimDurable, PhaseRunDurable, PhaseHandoffDurable:
		return true
	default:
		return false
	}
}

func (p Phase) atLeast(want Phase) bool {
	order := map[Phase]int{
		PhasePrepared:       0,
		PhaseClaimDurable:   1,
		PhaseRunDurable:     2,
		PhaseHandoffDurable: 3,
	}
	return order[p] >= order[want]
}

// Binding identifies one queue item and the run that owns its claim.
type Binding struct {
	QueueID           string            `json:"queue_id"`
	QueueName         string            `json:"queue_name"`
	GroupIndex        int               `json:"group_index"`
	ItemIndex         int               `json:"item_index"`
	BeadID            core.BeadID       `json:"bead_id"`
	RunID             core.RunID        `json:"run_id"`
	ClaimTransitionID core.TransitionID `json:"claim_transition_id"`
}

// RunBinding proves that the durable run record names this dispatch.
type RunBinding struct {
	RecordRunID core.RunID `json:"record_run_id"`
}

// HandoffBinding proves that the session and worktree lease name this run.
type HandoffBinding struct {
	SessionName        string     `json:"session_name"`
	WorktreeLeaseRunID core.RunID `json:"worktree_lease_run_id"`
}

// Intent records the durable progress of one dispatch.
type Intent struct {
	SchemaVersion int             `json:"schema_version"`
	Phase         Phase           `json:"phase"`
	Binding       Binding         `json:"binding"`
	Run           *RunBinding     `json:"run,omitempty"`
	Handoff       *HandoffBinding `json:"handoff,omitempty"`
}

// Validate rejects incomplete phases and identity conflicts.
func (i Intent) Validate() error {
	if i.SchemaVersion != intentSchemaVersion {
		return fmt.Errorf("dispatch: schema_version must be %d", intentSchemaVersion)
	}
	if !i.Phase.valid() {
		return fmt.Errorf("dispatch: invalid phase %q", i.Phase)
	}
	if err := i.Binding.validate(); err != nil {
		return err
	}
	if err := i.validateRunBinding(); err != nil {
		return err
	}
	return i.validateHandoffBinding()
}

func (i Intent) validateRunBinding() error {
	if i.Phase.atLeast(PhaseRunDurable) {
		if i.Run == nil {
			return errors.New("dispatch: run binding is required at run_durable")
		}
		if i.Run.RecordRunID != i.Binding.RunID {
			return errors.New("dispatch: run record identity does not match run_id")
		}
	} else if i.Run != nil {
		return errors.New("dispatch: run binding is not allowed before run_durable")
	}
	return nil
}

func (i Intent) validateHandoffBinding() error {
	if i.Phase.atLeast(PhaseHandoffDurable) {
		if i.Handoff == nil {
			return errors.New("dispatch: handoff binding is required at handoff_durable")
		}
		if i.Handoff.SessionName == "" {
			return errors.New("dispatch: session_name is required at handoff_durable")
		}
		if i.Handoff.WorktreeLeaseRunID != i.Binding.RunID {
			return errors.New("dispatch: worktree lease identity does not match run_id")
		}
	} else if i.Handoff != nil {
		return errors.New("dispatch: handoff binding is not allowed before handoff_durable")
	}
	return nil
}

func (b Binding) validate() error {
	if err := validateUUIDv7(b.QueueID); err != nil {
		return fmt.Errorf("dispatch: queue_id: %w", err)
	}
	if ok, detail := queue.ValidateQueueName(b.QueueName); !ok || queue.NormaliseQueueName(b.QueueName) != b.QueueName {
		return fmt.Errorf("dispatch: queue_name is not normalized: %s", detail)
	}
	if b.GroupIndex < 0 || b.ItemIndex < 0 {
		return errors.New("dispatch: queue location indexes must be non-negative")
	}
	if b.BeadID == "" {
		return errors.New("dispatch: bead_id is required")
	}
	if uuid.UUID(b.RunID).Version() != 7 {
		return errors.New("dispatch: run_id must be UUIDv7")
	}
	if !b.ClaimTransitionID.IsUUIDv7() {
		return errors.New("dispatch: claim_transition_id must be UUIDv7")
	}
	return nil
}

func validateUUIDv7(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 7 || id.String() != value {
		return errors.New("must be canonical lowercase UUIDv7")
	}
	return nil
}

// MarshalJSON validates the intent before it creates durable bytes.
func (i Intent) MarshalJSON() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	groupIndex := i.Binding.GroupIndex
	itemIndex := i.Binding.ItemIndex
	w := intentWire{
		SchemaVersion: i.SchemaVersion,
		Phase:         i.Phase,
		Binding: bindingWire{
			QueueID:           i.Binding.QueueID,
			QueueName:         i.Binding.QueueName,
			GroupIndex:        &groupIndex,
			ItemIndex:         &itemIndex,
			BeadID:            string(i.Binding.BeadID),
			RunID:             i.Binding.RunID.String(),
			ClaimTransitionID: i.Binding.ClaimTransitionID.String(),
		},
	}
	if i.Run != nil {
		w.Run = &runBindingWire{RecordRunID: i.Run.RecordRunID.String()}
	}
	if i.Handoff != nil {
		w.Handoff = &handoffBindingWire{
			SessionName:        i.Handoff.SessionName,
			WorktreeLeaseRunID: i.Handoff.WorktreeLeaseRunID.String(),
		}
	}
	return json.Marshal(w)
}

// UnmarshalJSON rejects unknown fields and every invalid phase shape.
func (i *Intent) UnmarshalJSON(data []byte) error {
	if i == nil {
		return errors.New("dispatch: unmarshal into nil intent")
	}
	var decoded intentWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("dispatch: decode intent: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	value, err := decoded.intent()
	if err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	*i = value
	return nil
}

type intentWire struct {
	SchemaVersion int                 `json:"schema_version"`
	Phase         Phase               `json:"phase"`
	Binding       bindingWire         `json:"binding"`
	Run           *runBindingWire     `json:"run,omitempty"`
	Handoff       *handoffBindingWire `json:"handoff,omitempty"`
}

type bindingWire struct {
	QueueID           string `json:"queue_id"`
	QueueName         string `json:"queue_name"`
	GroupIndex        *int   `json:"group_index"`
	ItemIndex         *int   `json:"item_index"`
	BeadID            string `json:"bead_id"`
	RunID             string `json:"run_id"`
	ClaimTransitionID string `json:"claim_transition_id"`
}

type runBindingWire struct {
	RecordRunID string `json:"record_run_id"`
}

type handoffBindingWire struct {
	SessionName        string `json:"session_name"`
	WorktreeLeaseRunID string `json:"worktree_lease_run_id"`
}

func (w intentWire) intent() (Intent, error) {
	if w.Binding.GroupIndex == nil || w.Binding.ItemIndex == nil {
		return Intent{}, errors.New("dispatch: group_index and item_index are required")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "queue_id", value: w.Binding.QueueID},
		{name: "run_id", value: w.Binding.RunID},
		{name: "claim_transition_id", value: w.Binding.ClaimTransitionID},
	} {
		if err := validateUUIDv7(field.value); err != nil {
			return Intent{}, fmt.Errorf("dispatch: %s: %w", field.name, err)
		}
	}
	runUUID := uuid.MustParse(w.Binding.RunID)
	claimUUID := uuid.MustParse(w.Binding.ClaimTransitionID)
	value := Intent{
		SchemaVersion: w.SchemaVersion,
		Phase:         w.Phase,
		Binding: Binding{
			QueueID:           w.Binding.QueueID,
			QueueName:         w.Binding.QueueName,
			GroupIndex:        *w.Binding.GroupIndex,
			ItemIndex:         *w.Binding.ItemIndex,
			BeadID:            core.BeadID(w.Binding.BeadID),
			RunID:             core.RunID(runUUID),
			ClaimTransitionID: core.TransitionID(claimUUID),
		},
	}
	if w.Run != nil {
		if err := validateUUIDv7(w.Run.RecordRunID); err != nil {
			return Intent{}, fmt.Errorf("dispatch: record_run_id: %w", err)
		}
		value.Run = &RunBinding{
			RecordRunID: core.RunID(uuid.MustParse(w.Run.RecordRunID)),
		}
	}
	if w.Handoff != nil {
		if err := validateUUIDv7(w.Handoff.WorktreeLeaseRunID); err != nil {
			return Intent{}, fmt.Errorf("dispatch: worktree_lease_run_id: %w", err)
		}
		value.Handoff = &HandoffBinding{
			SessionName:        w.Handoff.SessionName,
			WorktreeLeaseRunID: core.RunID(uuid.MustParse(w.Handoff.WorktreeLeaseRunID)),
		}
	}
	return value, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("dispatch: multiple JSON values")
		}
		return fmt.Errorf("dispatch: trailing JSON: %w", err)
	}
	return nil
}
