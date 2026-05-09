// Package core holds shared types that cross subsystem boundaries.
// It is a leaf package: it imports only stdlib and a narrow allowlist
// (github.com/google/uuid), and MUST NOT import any internal subsystem.
// See docs/foundation/project-level/subsystem-organization.md §Shared types.
package core

import "github.com/google/uuid"

// RunID is a UUIDv7 run identifier (execution-model.md §6.1; unique across project).
//
// RunID is a named type (not a Go alias) so that RunID and other ID types
// (StateID, TransitionID, etc.) are not interchangeable at compile time.
// The underlying UUID MUST be UUIDv7 per event-model.md §4.1 EV-002.
//
// EM-013 invariant (execution-model.md §4.3.EM-013): the run_id of a run MUST
// appear as the Harmonik-Run-ID trailer on every checkpoint commit for that run
// (per §4.4) and as the run_id field on every run-scoped event (per
// event-model.md §4.1). The run_id is the join key across git, Beads, and JSONL.
type RunID uuid.UUID

// String returns the canonical hyphenated UUID string representation.
func (r RunID) String() string {
	return uuid.UUID(r).String()
}

// MarshalText implements encoding.TextMarshaler.
// The output is the canonical hyphenated UUID string (36 bytes).
func (r RunID) MarshalText() ([]byte, error) {
	return uuid.UUID(r).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts the canonical hyphenated UUID string form.
func (r *RunID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*r = RunID(u)
	return nil
}
