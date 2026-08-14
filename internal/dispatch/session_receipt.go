package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const sessionStartReceiptSchemaVersion = 2

// SessionStartReceipt proves that the exact dispatch target acknowledged start.
type SessionStartReceipt struct {
	SchemaVersion int
	Binding       Binding
	SessionName   string
	WindowName    string
}

// NewSessionStartReceipt returns the receipt for one durable handoff.
func NewSessionStartReceipt(intent Intent) (SessionStartReceipt, error) {
	if err := intent.Validate(); err != nil {
		return SessionStartReceipt{}, err
	}
	if intent.Phase != PhaseHandoffDurable {
		return SessionStartReceipt{}, errors.New("dispatch: session receipt requires handoff_durable intent")
	}
	receipt := SessionStartReceipt{
		SchemaVersion: sessionStartReceiptSchemaVersion,
		Binding:       intent.Binding,
		SessionName:   intent.Handoff.SessionName,
		WindowName:    intent.Handoff.WindowName,
	}
	if err := receipt.Validate(); err != nil {
		return SessionStartReceipt{}, err
	}
	return receipt, nil
}

// Validate rejects partial receipt identity.
func (r SessionStartReceipt) Validate() error {
	if r.SchemaVersion != sessionStartReceiptSchemaVersion {
		return fmt.Errorf("dispatch: session receipt schema_version must be %d", sessionStartReceiptSchemaVersion)
	}
	if err := r.Binding.validate(); err != nil {
		return err
	}
	for _, target := range []struct{ name, value string }{
		{name: "session_name", value: r.SessionName},
		{name: "window_name", value: r.WindowName},
	} {
		if target.value == "" || strings.TrimSpace(target.value) != target.value ||
			strings.ContainsAny(target.value, "\x00\r\n") {
			return fmt.Errorf("dispatch: session receipt %s is invalid", target.name)
		}
	}
	return nil
}

// MarshalJSON writes the strict receipt wire shape.
func (r SessionStartReceipt) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	groupIndex, itemIndex := r.Binding.GroupIndex, r.Binding.ItemIndex
	return json.Marshal(sessionStartReceiptWire{
		SchemaVersion: r.SchemaVersion,
		Binding: bindingWire{
			QueueID: r.Binding.QueueID, QueueName: r.Binding.QueueName,
			GroupIndex: &groupIndex, ItemIndex: &itemIndex, BeadID: string(r.Binding.BeadID),
			RunID: r.Binding.RunID.String(), ClaimTransitionID: r.Binding.ClaimTransitionID.String(),
			ParentCommit: r.Binding.ParentCommit,
		},
		SessionName: r.SessionName,
		WindowName:  r.WindowName,
	})
}

// UnmarshalJSON rejects unknown fields and non-canonical binding values.
func (r *SessionStartReceipt) UnmarshalJSON(data []byte) error {
	if r == nil {
		return errors.New("dispatch: unmarshal into nil session receipt")
	}
	var wire sessionStartReceiptWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return fmt.Errorf("dispatch: decode session receipt: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("dispatch: session receipt must contain one JSON value")
	}
	binding, err := wire.Binding.binding()
	if err != nil {
		return err
	}
	value := SessionStartReceipt{
		SchemaVersion: wire.SchemaVersion,
		Binding:       binding,
		SessionName:   wire.SessionName,
		WindowName:    wire.WindowName,
	}
	if err := value.Validate(); err != nil {
		return err
	}
	*r = value
	return nil
}

type sessionStartReceiptWire struct {
	SchemaVersion int         `json:"schema_version"`
	Binding       bindingWire `json:"binding"`
	SessionName   string      `json:"session_name"`
	WindowName    string      `json:"window_name"`
}
