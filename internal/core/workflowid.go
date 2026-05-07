package core

import "github.com/google/uuid"

// WorkflowID is a UUID-backed stable workflow identifier (execution-model.md §6.1).
//
// WorkflowID is a named type (not a Go alias) so that WorkflowID and other ID types
// (RunID, StateID, TransitionID, etc.) are not interchangeable at compile time.
// The underlying UUID identifies the workflow definition and is pinned on the Run at
// dispatch time per execution-model.md §6.1 Run.workflow_id.
type WorkflowID uuid.UUID

// String returns the canonical hyphenated UUID string representation.
func (w WorkflowID) String() string {
	return uuid.UUID(w).String()
}

// MarshalText implements encoding.TextMarshaler.
// The output is the canonical hyphenated UUID string (36 bytes).
func (w WorkflowID) MarshalText() ([]byte, error) {
	return uuid.UUID(w).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts the canonical hyphenated UUID string form.
func (w *WorkflowID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*w = WorkflowID(u)
	return nil
}
