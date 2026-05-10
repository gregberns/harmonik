package core

import "github.com/google/uuid"

// WorkspaceID is a stable UUID identifier for a harmonik workspace (git worktree)
// (event-model.md §8.5.1–§8.5.6).
//
// WorkspaceID is a named type (not a Go alias) over uuid.UUID so that it is
// not interchangeable with RunID, EventID, or other UUID-backed ID types at
// compile time. The underlying UUID MUST be UUIDv7 per the workspace-manager
// (S06) allocation policy (workspace-model.md §4.4).
//
// WorkspaceID appears in all six workspace event payloads:
//   - WorkspaceCreatedPayload    (§8.5.1)
//   - WorkspaceLeasedPayload     (§8.5.2)
//   - WorkspaceMergeStatusPayload (§8.5.3)
//   - WorkspaceDiscardedPayload  (§8.5.4)
//   - WorkspaceInterruptedPayload (§8.5.5)
//   - MergeConflictEscalationPayload (§8.5.6)
//
// Bead: hk-hqwn.74.
type WorkspaceID uuid.UUID

// String returns the canonical hyphenated UUID string representation.
func (w WorkspaceID) String() string {
	return uuid.UUID(w).String()
}

// MarshalText implements encoding.TextMarshaler.
// The output is the canonical hyphenated UUID string (36 bytes).
func (w WorkspaceID) MarshalText() ([]byte, error) {
	return uuid.UUID(w).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts the canonical hyphenated UUID string form.
func (w *WorkspaceID) UnmarshalText(data []byte) error {
	var u uuid.UUID
	if err := u.UnmarshalText(data); err != nil {
		return err
	}
	*w = WorkspaceID(u)
	return nil
}
