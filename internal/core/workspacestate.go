package core

import "fmt"

// WorkspaceState is the lifecycle state of a harmonik-managed worktree, defined
// in workspace-model.md §6.1.
//
// The seven values are:
//
//	created, ready, leased, merge-pending, conflict-resolving, merged, discarded
//
// Terminal states: merged and discarded. All other values are in-flight states
// through which a worktree transitions during its lifecycle.
//
// NOTE (v0.3.0): the value "setup" has been retired and MUST NOT be
// reintroduced. See workspace-model.md §12 for migration impact.
//
// The enum is harmonik-owned and closed: unlike Beads-owned enums (e.g.
// CoarseStatus), unknown values are never tolerated.
type WorkspaceState string

// WorkspaceState constants per workspace-model.md §6.1.
const (
	// WorkspaceStateCreated is the initial state when a worktree has been created
	// but is not yet ready for use.
	WorkspaceStateCreated WorkspaceState = "created"

	// WorkspaceStateReady means the worktree has been set up and is available for
	// a session to lease.
	WorkspaceStateReady WorkspaceState = "ready"

	// WorkspaceStateLeased means a session currently holds a lease on the worktree.
	WorkspaceStateLeased WorkspaceState = "leased"

	// WorkspaceStateMergePending means the session work is complete and the worktree
	// is queued for merging back into the integration branch.
	WorkspaceStateMergePending WorkspaceState = "merge-pending"

	// WorkspaceStateConflictResolving means a merge conflict was detected and
	// requires resolution before the worktree can advance.
	WorkspaceStateConflictResolving WorkspaceState = "conflict-resolving"

	// WorkspaceStateMerged is a terminal state: the worktree's changes have been
	// successfully merged.
	WorkspaceStateMerged WorkspaceState = "merged"

	// WorkspaceStateDiscarded is a terminal state: the worktree has been discarded
	// without merging.
	WorkspaceStateDiscarded WorkspaceState = "discarded"
)

// Valid reports whether s is one of the seven declared WorkspaceState constants.
// The workspace-state enum is harmonik-owned and closed; unknown values are
// never valid.
func (s WorkspaceState) Valid() bool {
	switch s {
	case WorkspaceStateCreated, WorkspaceStateReady, WorkspaceStateLeased,
		WorkspaceStateMergePending, WorkspaceStateConflictResolving,
		WorkspaceStateMerged, WorkspaceStateDiscarded:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so WorkspaceState serialises
// correctly in JSON and YAML.
func (s WorkspaceState) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("workspacestate: unknown value %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the seven declared constants.
func (s *WorkspaceState) UnmarshalText(text []byte) error {
	v := WorkspaceState(text)
	if !v.Valid() {
		return fmt.Errorf(
			"workspacestate: unknown value %q; must be one of created, ready, leased, merge-pending, conflict-resolving, merged, discarded",
			string(text),
		)
	}
	*s = v
	return nil
}
