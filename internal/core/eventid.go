package core

import "github.com/google/uuid"

// EventID is a UUIDv7 event identifier (event-model.md §4.1 EV-002).
//
// EventID is a named type (not a Go alias) so EventID is not interchangeable with
// other UUID-backed identifiers (RunID, StateID, ...) at compile time. The
// underlying UUID MUST be UUIDv7 per event-model.md §4.1 EV-002 (UUIDv4/UUIDv1 forbidden).
// Carried as the event_id field on every event envelope.
//
// Use [EventID.IsUUIDv7] to enforce EV-002 at validation time.
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

// IsUUIDv7 reports whether the underlying UUID is version 7 (time-ordered per RFC 9562).
//
// This method is the EV-002 enforcement point (event-model.md §4.1 EV-002): callers
// that accept an EventID from an external source MUST call IsUUIDv7 and reject the
// value if it returns false. UUIDv4 and UUIDv1 are explicitly forbidden by EV-002.
//
// The check reads the version nibble (upper nibble of byte index 6 in the
// 16-byte UUID layout, i.e. bits 76–79 of the 128-bit value) via
// [uuid.UUID.Version] from the github.com/google/uuid package.
func (e EventID) IsUUIDv7() bool {
	return uuid.UUID(e).Version() == 7
}
