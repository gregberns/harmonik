package core

import "github.com/google/uuid"

// StateID is a UUIDv7 run-state identifier (execution-model.md §6.1; unique per run-entry).
//
// StateID identifies a run-state: a Run's position at a node plus its accumulated context.
// It is carried as the Harmonik-State-ID trailer on checkpoint commits per EM-018/§6.2.
//
// StateID is a named type (not an alias) so the compiler rejects accidental mixing with
// RunID, TransitionID, or other UUID-backed identifiers.
type StateID uuid.UUID

// String returns the canonical UUID string representation (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func (s StateID) String() string {
	return uuid.UUID(s).String()
}

// MarshalText implements encoding.TextMarshaler.
// The text form is the canonical UUID string.
func (s StateID) MarshalText() ([]byte, error) {
	return uuid.UUID(s).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts any UUID string form that github.com/google/uuid accepts.
func (s *StateID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*s = StateID(u)
	return nil
}
