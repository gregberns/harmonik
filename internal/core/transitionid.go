// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import (
	"github.com/google/uuid"
)

// TransitionID is a UUIDv7 transition identifier (execution-model.md §6.1; daemon-local generation per §4.4.EM-018a).
type TransitionID uuid.UUID

// String returns the canonical UUID string representation of the TransitionID.
func (t TransitionID) String() string {
	return uuid.UUID(t).String()
}

// MarshalText implements encoding.TextMarshaler.
// It encodes the TransitionID as a UUID string.
func (t TransitionID) MarshalText() ([]byte, error) {
	return uuid.UUID(t).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It parses a UUID string into the TransitionID.
func (t *TransitionID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*t = TransitionID(u)
	return nil
}

// IsUUIDv7 reports whether the underlying UUID is version 7 (time-ordered per RFC 9562).
//
// This is the EM-018a enforcement point (execution-model.md §4.4 EM-018a): callers
// that accept a TransitionID from an external source MUST call IsUUIDv7 and reject
// the value if it returns false.
func (t TransitionID) IsUUIDv7() bool {
	return uuid.UUID(t).Version() == 7
}
