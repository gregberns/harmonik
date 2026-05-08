package core

import (
	"errors"
	"fmt"
)

// ErrSkipUnknown is returned by DispatchObservational when the event's type has
// no registered constructor. It signals "skipped" — the caller may count or log
// the skip but MUST NOT treat it as a fatal error.
//
// Spec ref: event-model.md §4.9 EV-033 — "skip the event (observational
// consumers)".
var ErrSkipUnknown = errors.New("core: event skipped — unknown type")

// DispatchUnknownEventError is the structured error returned by
// DispatchSynchronous when the event's type has no registered constructor.
//
// It wraps ErrUnknownEventType so callers can use errors.Is, and it carries
// EventType and EventID for diagnostics.
//
// Spec ref: event-model.md §4.9 EV-033 — "fail with a structured error
// (synchronous consumers)".
type DispatchUnknownEventError struct {
	// EventType is the unrecognised type string from Event.Type.
	EventType string
	// EventID is the event's identifier, for log correlation.
	EventID EventID
}

// Error implements the error interface.
func (e *DispatchUnknownEventError) Error() string {
	return fmt.Sprintf("core: unknown event type %q (event_id=%s)", e.EventType, e.EventID)
}

// Unwrap returns ErrUnknownEventType so errors.Is(err, ErrUnknownEventType)
// works on a *DispatchUnknownEventError.
func (e *DispatchUnknownEventError) Unwrap() error {
	return ErrUnknownEventType
}

// DispatchObservational decodes the payload for an observational consumer.
//
// On success it returns (payload, nil).
// When Event.Type has no registered constructor it returns (nil, ErrSkipUnknown).
// On any other decode error (e.g., json.SyntaxError) it returns (nil, err).
//
// Observational consumers MUST skip ErrSkipUnknown events — they SHOULD NOT
// propagate the error. Any other non-nil error indicates a malformed payload
// and the consumer may log and skip or propagate as appropriate.
//
// Spec ref: event-model.md §4.9 EV-033.
func DispatchObservational(e Event) (EventPayload, error) {
	payload, err := e.DecodePayload()
	if err == nil {
		return payload, nil
	}
	if errors.Is(err, ErrUnknownEventType) {
		return nil, ErrSkipUnknown
	}
	return nil, err
}

// DispatchSynchronous decodes the payload for a synchronous consumer.
//
// On success it returns (payload, nil).
// When Event.Type has no registered constructor it returns
// (nil, *DispatchUnknownEventError), which wraps ErrUnknownEventType.
// On any other decode error (e.g., json.SyntaxError) it returns (nil, err).
//
// Synchronous consumers SHOULD treat any non-nil error as fatal — i.e., they
// MAY stop processing the stream.
//
// Spec ref: event-model.md §4.9 EV-033.
func DispatchSynchronous(e Event) (EventPayload, error) {
	payload, err := e.DecodePayload()
	if err == nil {
		return payload, nil
	}
	if errors.Is(err, ErrUnknownEventType) {
		return nil, &DispatchUnknownEventError{
			EventType: e.Type,
			EventID:   e.EventID,
		}
	}
	return nil, err
}
