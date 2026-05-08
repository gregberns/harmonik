package core

import "github.com/google/uuid"

// SuiteID is a UUIDv7 suite identifier generated at suite invocation
// (specs/scenario-harness.md §6.1 RECORD SuiteResult — field suite_id UUID).
//
// SuiteID is a named type (not a Go alias) so that SuiteID and other ID types
// (RunID, SessionID, etc.) are not interchangeable at compile time.
// The underlying UUID MUST be UUIDv7 per the spec.
type SuiteID uuid.UUID

// String returns the canonical hyphenated UUID string representation.
func (s SuiteID) String() string {
	return uuid.UUID(s).String()
}

// MarshalText implements encoding.TextMarshaler.
// The output is the canonical hyphenated UUID string (36 bytes).
func (s SuiteID) MarshalText() ([]byte, error) {
	return uuid.UUID(s).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts the canonical hyphenated UUID string form.
func (s *SuiteID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*s = SuiteID(u)
	return nil
}
