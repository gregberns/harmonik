package queue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

// PreclaimTerminalCause is the durable reason a dispatch ended before claim.
type PreclaimTerminalCause string

const (
	// PreclaimTerminalMaxAttempts means the item reached its attempt limit.
	PreclaimTerminalMaxAttempts PreclaimTerminalCause = "max_attempts"
	// PreclaimTerminalCrossQueue means another queue owns the bead.
	PreclaimTerminalCrossQueue PreclaimTerminalCause = "cross_queue_duplicate"
	// PreclaimTerminalDependencyRefusal means Beads refused an open dependency.
	PreclaimTerminalDependencyRefusal PreclaimTerminalCause = "dependency_refusal"
)

// PreclaimTerminalBinding binds a failed item to the exact dispatch attempt.
type PreclaimTerminalBinding struct {
	RunID             string                `json:"run_id"`
	ClaimTransitionID string                `json:"claim_transition_id"`
	Cause             PreclaimTerminalCause `json:"cause"`
}

// MarshalJSON refuses an invalid durable binding.
func (b PreclaimTerminalBinding) MarshalJSON() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	type wire PreclaimTerminalBinding
	return json.Marshal(wire(b))
}

// UnmarshalJSON rejects unknown fields and invalid durable binding values.
func (b *PreclaimTerminalBinding) UnmarshalJSON(data []byte) error {
	if b == nil {
		return errors.New("queue: unmarshal nil preclaim terminal binding")
	}
	type wire PreclaimTerminalBinding
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("queue: decode preclaim terminal binding: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("queue: preclaim terminal binding must contain one JSON value")
	}
	value := PreclaimTerminalBinding(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*b = value
	return nil
}

func validatePreclaimTerminalItems(q *Queue) error {
	if q == nil {
		return errors.New("queue: nil queue")
	}
	for groupIndex := range q.Groups {
		for itemIndex := range q.Groups[groupIndex].Items {
			item := &q.Groups[groupIndex].Items[itemIndex]
			if err := validatePreclaimTerminalItem(*item); err != nil {
				return fmt.Errorf("queue: group %d item %d: %w", groupIndex, itemIndex, err)
			}
		}
	}
	return nil
}

func validatePreclaimTerminalItem(item Item) error {
	if item.PreclaimTerminal == nil {
		return nil
	}
	if err := item.PreclaimTerminal.Validate(); err != nil {
		return err
	}
	if item.Status != ItemStatusFailed {
		return errors.New("preclaim terminal binding requires failed item")
	}
	switch item.PreclaimTerminal.Cause {
	case PreclaimTerminalDependencyRefusal:
		if item.RunID == nil || *item.RunID != item.PreclaimTerminal.RunID {
			return errors.New("dependency refusal requires matching item run_id")
		}
	case PreclaimTerminalMaxAttempts, PreclaimTerminalCrossQueue:
		if item.RunID != nil {
			return errors.New("reservation failure requires nil item run_id")
		}
	}
	return nil
}

// Validate rejects non-canonical identity and unknown causes.
func (b PreclaimTerminalBinding) Validate() error {
	if err := validatePreclaimUUIDv7(b.RunID); err != nil {
		return fmt.Errorf("queue: preclaim terminal run_id: %w", err)
	}
	if err := validatePreclaimUUIDv7(b.ClaimTransitionID); err != nil {
		return fmt.Errorf("queue: preclaim terminal claim_transition_id: %w", err)
	}
	switch b.Cause {
	case PreclaimTerminalMaxAttempts, PreclaimTerminalCrossQueue, PreclaimTerminalDependencyRefusal:
		return nil
	default:
		return fmt.Errorf("queue: invalid preclaim terminal cause %q", b.Cause)
	}
}

func validatePreclaimUUIDv7(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 7 || id.String() != value {
		return errors.New("must be canonical lowercase UUIDv7")
	}
	return nil
}
