package core

import "github.com/google/uuid"

// EventID is a UUIDv7 event identifier (event-model.md §4.1 EV-002).
//
// EventID is a named type (not a Go alias) so EventID is not interchangeable with
// other UUID-backed identifiers (RunID, StateID, ...) at compile time. The
// underlying UUID MUST be UUIDv7 per event-model.md §4.1 EV-002 (UUIDv4/UUIDv1 forbidden).
// Carried as the event_id field on every event envelope.
type EventID uuid.UUID

// String returns the canonical hyphenated UUID string representation.
func (e EventID) String() string {
	return uuid.UUID(e).String()
}

// MarshalText implements encoding.TextMarshaler.
// The output is the canonical hyphenated UUID string (36 bytes).
func (e EventID) MarshalText() ([]byte, error) {
	return uuid.UUID(e).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts the canonical hyphenated UUID string form.
func (e *EventID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*e = EventID(u)
	return nil
}
