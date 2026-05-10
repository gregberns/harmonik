package workspace

import (
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// Workspace is the production record for a harmonik-managed git worktree.
// It is defined normatively in workspace-model.md §6.1 (RECORD Workspace).
//
// Lifecycle states traverse the graph declared in §7.1 and enforced by
// [Transition]. Terminal states are [core.WorkspaceStateMerged] and
// [core.WorkspaceStateDiscarded]; all others are in-flight.
//
// InterruptState is orthogonal to State for in-flight states only; terminal
// states MUST carry [core.InterruptStateNone] per WM-037a.
type Workspace struct {
	// WorkspaceID is the stable workspace identifier, derived deterministically
	// from RunID as "ws-"+RunID per WM-004.
	WorkspaceID string

	// RunID is the run this workspace is leased to (workspace-model.md §6.1).
	RunID core.RunID

	// Repository is the absolute path to the local clone of the backing
	// repository (WM-001, WM-002).
	Repository string

	// ParentCommit is the commit SHA this worktree was branched from
	// (workspace-model.md §6.1). Type alias CommitSHA is defined in
	// execution-model.md §6.1; represented here as string per the
	// typed-alias-deferral pattern.
	ParentCommit string

	// BranchName is the task branch name per §4.2 (e.g., "run/<run_id>").
	BranchName string

	// Path is the absolute filesystem path to the worktree (WM-002).
	Path string

	// State is the lifecycle state per §4.4 and the §7.1 transition table.
	// Mutation is only via [Transition].
	State core.WorkspaceState

	// InterruptState is the orthogonal interrupt field per §4.10 / WM-037.
	// Applies only to in-flight states; terminal states MUST carry
	// [core.InterruptStateNone] per WM-037a.
	InterruptState core.InterruptState

	// BeadID is the correlation field; present iff the run is bead-tied.
	// Nil when the run is not bead-tied.
	BeadID *core.BeadID

	// ImplementerHandlerRef is set at merge-pending entry per WM-022; nil iff
	// the task branch has no agentic commits. Carries the handler_ref of the
	// most-recent agentic session sidecar (identified by the sidecar walk per
	// WM-022) and serves as the re-dispatch key for merge-conflict resolution
	// per §4.6.WM-024.
	//
	// The type is *core.HandlerRef (defined per handler-contract.md §6.1 and
	// workspace-model.md §6.1). A nil pointer represents null (no agentic
	// commits on the task branch per WM-022a).
	ImplementerHandlerRef *core.HandlerRef

	// Metadata is a closed map of string metadata fields.
	// Declared keys: "created_at" (RFC 3339) and "operator_fingerprint".
	// Additional keys are not permitted per §6.1.
	Metadata map[string]string

	// SchemaVersion is the record schema version (N-1 readable per §6.4).
	// Set by S06 (the workspace manager) on create.
	SchemaVersion int
}

// ErrInvalidTransition is returned by [Transition] when the requested
// (from, to) state pair is not declared in the §7.1 transition table.
var ErrInvalidTransition = errors.New("workspace: invalid lifecycle state transition")

// Transition advances ws.State from its current value to next, enforcing
// the §7.1 lifecycle state machine.
//
// Guard rules per §7.1:
//
//   - Only the (from, to) pairs declared in the §7.1 table are accepted;
//     all others return [ErrInvalidTransition].
//   - On entry to a terminal state ([core.WorkspaceStateMerged] or
//     [core.WorkspaceStateDiscarded]), ws.InterruptState MUST be cleared
//     to [core.InterruptStateNone] per WM-037a.
//   - The `setup` value has been retired in v0.3.0 and MUST NOT be
//     re-introduced (workspace-model.md §12).
//
// Transition does NOT emit events. The caller (workspace manager S06) is
// responsible for event emission at the times declared in §7.1 and WM-015.
//
// Transition does NOT acquire locks. Callers must serialise access to ws.
func Transition(ws *Workspace, next core.WorkspaceState) error {
	if !next.Valid() {
		return fmt.Errorf("%w: target state %q is not a valid WorkspaceState", ErrInvalidTransition, next)
	}

	from := ws.State

	// §7.1 transition table: allowed (from → to) pairs.
	//
	// Legend (from §7.1):
	//   (initial)              → created              : orchestrator issues create
	//   created                → ready                : git worktree add + sessions_dir succeed
	//   ready                  → leased               : first session sidecar + lease-lock fsynced
	//   leased                 → merge-pending        : merge node dispatched
	//   merge-pending          → merged               : merge succeeds
	//   merge-pending          → conflict-resolving   : merge conflicts detected
	//   conflict-resolving     → merge-pending        : implementer resolves
	//   conflict-resolving     → discarded            : re-dispatch exhausted OR all-mechanical
	//   leased                 → discarded            : run reaches terminal failure
	//
	// The zero value ("") of WorkspaceState models the "initial" origin per §7.1
	// (the Workspace record does not exist before create; this path covers the
	// first assignment into a freshly allocated Workspace).
	allowed := false
	switch from {
	case "": // initial (zero-value WorkspaceState before first assignment)
		allowed = (next == core.WorkspaceStateCreated)

	case core.WorkspaceStateCreated:
		allowed = (next == core.WorkspaceStateReady)

	case core.WorkspaceStateReady:
		allowed = (next == core.WorkspaceStateLeased)

	case core.WorkspaceStateLeased:
		allowed = (next == core.WorkspaceStateMergePending ||
			next == core.WorkspaceStateDiscarded)

	case core.WorkspaceStateMergePending:
		allowed = (next == core.WorkspaceStateMerged ||
			next == core.WorkspaceStateConflictResolving)

	case core.WorkspaceStateConflictResolving:
		allowed = (next == core.WorkspaceStateMergePending ||
			next == core.WorkspaceStateDiscarded)

	case core.WorkspaceStateMerged, core.WorkspaceStateDiscarded:
		// Terminal states are absorbing; no further transitions are permitted.
		allowed = false
	}

	if !allowed {
		return fmt.Errorf("%w: %q → %q", ErrInvalidTransition, from, next)
	}

	ws.State = next

	// WM-037a: terminal states MUST carry interrupt_state = none.
	if next == core.WorkspaceStateMerged || next == core.WorkspaceStateDiscarded {
		ws.InterruptState = core.InterruptStateNone
	}

	return nil
}

// IsTerminal reports whether s is a terminal lifecycle state per §7.1.
// Terminal states are [core.WorkspaceStateMerged] and [core.WorkspaceStateDiscarded].
// No further transitions are permitted from a terminal state.
func IsTerminal(s core.WorkspaceState) bool {
	return s == core.WorkspaceStateMerged || s == core.WorkspaceStateDiscarded
}

// IsInFlight reports whether s is an in-flight lifecycle state per §7.1.
// In-flight states are all non-terminal valid states:
// created, ready, leased, merge-pending, conflict-resolving.
// The [core.InterruptState] orthogonality rule (WM-037) applies only to
// in-flight states.
func IsInFlight(s core.WorkspaceState) bool {
	return s.Valid() && !IsTerminal(s)
}
