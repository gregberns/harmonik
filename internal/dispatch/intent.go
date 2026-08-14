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

const intentSchemaVersion = 2

// Phase is the last dispatch boundary known to be durable.
type Phase string

const (
	// PhasePrepared binds the queue item before the reservation write.
	PhasePrepared Phase = "prepared"
	// PhaseClaimDurable records that Beads accepted the claim.
	PhaseClaimDurable Phase = "claim_durable"
	// PhaseClaimRefused records one actionable definite claim refusal.
	PhaseClaimRefused Phase = "claim_refused"
	// PhaseRunDurable records that the durable run record exists.
	PhaseRunDurable Phase = "run_durable"
	// PhaseHandoffDurable records that the session handoff is durable.
	PhaseHandoffDurable Phase = "handoff_durable"
)

func (p Phase) valid() bool {
	switch p {
	case PhasePrepared, PhaseClaimDurable, PhaseClaimRefused, PhaseRunDurable, PhaseHandoffDurable:
		return true
	default:
		return false
	}
}

// ClaimRefusalCause is the durable class of one actionable claim refusal.
type ClaimRefusalCause string

const (
	// ClaimRefusalDependency means Beads refused an open dependency.
	ClaimRefusalDependency ClaimRefusalCause = "dependency_refusal"
	// ClaimRefusalSupportedNonOpen means the bead has a supported non-open state.
	ClaimRefusalSupportedNonOpen ClaimRefusalCause = "supported_non_open"
)

// ClaimRefusalBinding is the typed refusal stored before queue compensation.
type ClaimRefusalBinding struct {
	Cause ClaimRefusalCause `json:"cause"`
}

func (b ClaimRefusalBinding) validate() error {
	switch b.Cause {
	case ClaimRefusalDependency, ClaimRefusalSupportedNonOpen:
		return nil
	default:
		return fmt.Errorf("dispatch: invalid claim refusal cause %q", b.Cause)
	}
}

func (p Phase) atLeast(want Phase) bool {
	switch want {
	case PhaseRunDurable:
		return p == PhaseRunDurable || p == PhaseHandoffDurable
	case PhaseHandoffDurable:
		return p == PhaseHandoffDurable
	default:
		return p == want
	}
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
	ParentCommit      string            `json:"parent_commit"`
}

// RunBinding proves that the durable run record names this dispatch.
type RunBinding struct {
	RecordRunID core.RunID `json:"record_run_id"`
}

// HandoffBinding proves that the session and worktree lease name this run.
type HandoffBinding struct {
	SessionName        string     `json:"session_name"`
	WindowName         string     `json:"window_name"`
	WorktreeLeaseRunID core.RunID `json:"worktree_lease_run_id"`
}

// Intent records the durable progress of one dispatch.
type Intent struct {
	SchemaVersion int                  `json:"schema_version"`
	Phase         Phase                `json:"phase"`
	Binding       Binding              `json:"binding"`
	Refusal       *ClaimRefusalBinding `json:"refusal,omitempty"`
	Run           *RunBinding          `json:"run,omitempty"`
	Handoff       *HandoffBinding      `json:"handoff,omitempty"`
}

// WithClaimRefused returns the terminal claim branch for one definite refusal.
func (i Intent) WithClaimRefused(cause ClaimRefusalCause) (Intent, error) {
	if err := i.Validate(); err != nil {
		return Intent{}, fmt.Errorf("dispatch: invalid prepared predecessor: %w", err)
	}
	if i.Phase != PhasePrepared {
		return Intent{}, fmt.Errorf("dispatch: refusal advance requires prepared phase, got %q", i.Phase)
	}
	i.Phase = PhaseClaimRefused
	i.Refusal = &ClaimRefusalBinding{Cause: cause}
	if err := i.Validate(); err != nil {
		return Intent{}, err
	}
	return i, nil
}

// NewPrepared returns the first durable intent for one queue reservation.
func NewPrepared(binding Binding) (Intent, error) {
	intent := Intent{SchemaVersion: intentSchemaVersion, Phase: PhasePrepared, Binding: binding}
	if err := intent.Validate(); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

// WithClaimDurable returns the next intent after the exact claim is durable.
func (i Intent) WithClaimDurable() (Intent, error) {
	if err := i.Validate(); err != nil {
		return Intent{}, fmt.Errorf("dispatch: invalid prepared predecessor: %w", err)
	}
	if i.Phase != PhasePrepared {
		return Intent{}, fmt.Errorf("dispatch: claim advance requires prepared phase, got %q", i.Phase)
	}
	i.Phase = PhaseClaimDurable
	if err := i.Validate(); err != nil {
		return Intent{}, err
	}
	return i, nil
}

// WithRunDurable returns the next intent after the universal run record exists.
func (i Intent) WithRunDurable() (Intent, error) {
	if err := i.Validate(); err != nil {
		return Intent{}, fmt.Errorf("dispatch: invalid claim predecessor: %w", err)
	}
	if i.Phase != PhaseClaimDurable {
		return Intent{}, fmt.Errorf("dispatch: run advance requires claim_durable phase, got %q", i.Phase)
	}
	i.Phase = PhaseRunDurable
	i.Run = &RunBinding{RecordRunID: i.Binding.RunID}
	if err := i.Validate(); err != nil {
		return Intent{}, err
	}
	return i, nil
}

// WithHandoffDurable returns the next intent after handoff identity is durable.
func (i Intent) WithHandoffDurable(sessionName, windowName string) (Intent, error) {
	if err := i.Validate(); err != nil {
		return Intent{}, fmt.Errorf("dispatch: invalid run predecessor: %w", err)
	}
	if i.Phase != PhaseRunDurable {
		return Intent{}, fmt.Errorf("dispatch: handoff advance requires run_durable phase, got %q", i.Phase)
	}
	i.Phase = PhaseHandoffDurable
	runBinding := *i.Run
	i.Run = &runBinding
	i.Handoff = &HandoffBinding{
		SessionName: sessionName, WindowName: windowName, WorktreeLeaseRunID: i.Binding.RunID,
	}
	if err := i.Validate(); err != nil {
		return Intent{}, err
	}
	return i, nil
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
	if err := i.validateRefusalBinding(); err != nil {
		return err
	}
	if err := i.validateRunBinding(); err != nil {
		return err
	}
	return i.validateHandoffBinding()
}

func (i Intent) validateRefusalBinding() error {
	if i.Phase == PhaseClaimRefused {
		if i.Refusal == nil {
			return errors.New("dispatch: refusal binding is required at claim_refused")
		}
		return i.Refusal.validate()
	}
	if i.Refusal != nil {
		return errors.New("dispatch: refusal binding is allowed only at claim_refused")
	}
	return nil
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
		if i.Handoff.WindowName == "" {
			return errors.New("dispatch: window_name is required at handoff_durable")
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
	if err := ValidateParentCommit(b.ParentCommit); err != nil {
		return err
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
			ParentCommit:      i.Binding.ParentCommit,
		},
	}
	if i.Run != nil {
		w.Run = &runBindingWire{RecordRunID: i.Run.RecordRunID.String()}
	}
	if i.Refusal != nil {
		w.Refusal = &claimRefusalWire{Cause: i.Refusal.Cause}
	}
	if i.Handoff != nil {
		w.Handoff = &handoffBindingWire{
			SessionName:        i.Handoff.SessionName,
			WindowName:         i.Handoff.WindowName,
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
	Refusal       *claimRefusalWire   `json:"refusal,omitempty"`
	Run           *runBindingWire     `json:"run,omitempty"`
	Handoff       *handoffBindingWire `json:"handoff,omitempty"`
}

type claimRefusalWire struct {
	Cause ClaimRefusalCause `json:"cause"`
}

type bindingWire struct {
	QueueID           string `json:"queue_id"`
	QueueName         string `json:"queue_name"`
	GroupIndex        *int   `json:"group_index"`
	ItemIndex         *int   `json:"item_index"`
	BeadID            string `json:"bead_id"`
	RunID             string `json:"run_id"`
	ClaimTransitionID string `json:"claim_transition_id"`
	ParentCommit      string `json:"parent_commit"`
}

type runBindingWire struct {
	RecordRunID string `json:"record_run_id"`
}

type handoffBindingWire struct {
	SessionName        string `json:"session_name"`
	WindowName         string `json:"window_name"`
	WorktreeLeaseRunID string `json:"worktree_lease_run_id"`
}

func (w intentWire) intent() (Intent, error) {
	binding, err := w.Binding.binding()
	if err != nil {
		return Intent{}, err
	}
	value := Intent{SchemaVersion: w.SchemaVersion, Phase: w.Phase, Binding: binding}
	if w.Run != nil {
		if err := validateUUIDv7(w.Run.RecordRunID); err != nil {
			return Intent{}, fmt.Errorf("dispatch: record_run_id: %w", err)
		}
		value.Run = &RunBinding{
			RecordRunID: core.RunID(uuid.MustParse(w.Run.RecordRunID)),
		}
	}
	if w.Refusal != nil {
		value.Refusal = &ClaimRefusalBinding{Cause: w.Refusal.Cause}
	}
	if w.Handoff != nil {
		if err := validateUUIDv7(w.Handoff.WorktreeLeaseRunID); err != nil {
			return Intent{}, fmt.Errorf("dispatch: worktree_lease_run_id: %w", err)
		}
		value.Handoff = &HandoffBinding{
			SessionName:        w.Handoff.SessionName,
			WindowName:         w.Handoff.WindowName,
			WorktreeLeaseRunID: core.RunID(uuid.MustParse(w.Handoff.WorktreeLeaseRunID)),
		}
	}
	return value, nil
}

func (w bindingWire) binding() (Binding, error) {
	if w.GroupIndex == nil || w.ItemIndex == nil {
		return Binding{}, errors.New("dispatch: group_index and item_index are required")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "queue_id", value: w.QueueID},
		{name: "run_id", value: w.RunID},
		{name: "claim_transition_id", value: w.ClaimTransitionID},
	} {
		if err := validateUUIDv7(field.value); err != nil {
			return Binding{}, fmt.Errorf("dispatch: %s: %w", field.name, err)
		}
	}
	return Binding{
		QueueID: w.QueueID, QueueName: w.QueueName, GroupIndex: *w.GroupIndex, ItemIndex: *w.ItemIndex,
		BeadID: core.BeadID(w.BeadID), RunID: core.RunID(uuid.MustParse(w.RunID)),
		ClaimTransitionID: core.TransitionID(uuid.MustParse(w.ClaimTransitionID)),
		ParentCommit:      w.ParentCommit,
	}, nil
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
