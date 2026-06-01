package workspace

// conflictescalation_wm023.go — WM-023 escalation execution path.
//
// Implements the deterministic (mechanism-tagged) escalation path for
// unresolvable merge conflicts per workspace-model.md §4.6.WM-023:
//
//   - ErrEscalationNotInConflictResolving — state-guard: workspace MUST be in
//     conflict-resolving before escalation can proceed.
//   - BuildConflictEscalationPayload — constructs the core.MergeConflictEscalationPayload
//     required by [event-model.md §8.5.6] from workspace state and conflict inputs.
//
// Caller discipline per WM-023 ordering contract:
//  1. Call BuildConflictEscalationPayload to obtain the typed payload.
//  2. Emit merge_conflict_escalation on the event bus (emitter owns the bus call).
//  3. Call Transition(ws, core.WorkspaceStateDiscarded) to advance lifecycle state.
//
// The ordering of steps 2 and 3 is the caller's responsibility; this package
// provides the primitive, not the sequencing. The "single-entry" invariant
// (conflict-resolving is entered at most once per merge-pending cycle) is enforced
// by the state guard in BuildConflictEscalationPayload: escalation can only execute
// from conflict-resolving, so the workspace MUST have already entered that state
// once (at initial conflict detection) before any escalation call is valid.
//
// Spec refs:
//   - specs/workspace-model.md §4.6 WM-023 — escalation trigger conditions and
//     ordering contract (event BEFORE state transition per §7.1 table row).
//   - specs/workspace-model.md §7.1 — conflict-resolving → discarded transition.
//   - specs/event-model.md §8.5.6 — merge_conflict_escalation payload schema
//     (EV-owned; payload type: core.MergeConflictEscalationPayload).
//   - specs/workspace-model.md §4.6 WM-022a — all-mechanical escalation (null ref
//     routes here without a re-dispatch attempt).
//   - specs/workspace-model.md §4.6 WM-024 — cap-exhausted routing to this path.
//
// Bead ref: hk-8mwo.35.

import (
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrEscalationNotInConflictResolving is returned by BuildConflictEscalationPayload
// when the workspace is not in the conflict-resolving state.
//
// Per WM-023: "the run MUST transition to conflict-resolving per §7.1 on initial
// conflict detection (not again on escalation — the transition is single-entry)."
// Escalation executes from conflict-resolving; calling BuildConflictEscalationPayload
// from any other state is a programming error that must be caught early.
//
// Callers SHOULD use errors.Is to test for this sentinel.
var ErrEscalationNotInConflictResolving = errors.New(
	"workspace: escalation requires workspace in conflict-resolving state",
)

// BuildConflictEscalationPayload constructs the core.MergeConflictEscalationPayload
// for the merge_conflict_escalation event per workspace-model.md §4.6.WM-023 and
// event-model.md §8.5.6.
//
// Single-entry guard: ws.State MUST be core.WorkspaceStateConflictResolving. Any
// other value returns ErrEscalationNotInConflictResolving. This enforces the
// invariant declared in WM-023: the workspace enters conflict-resolving exactly once
// per merge-pending cycle (at initial conflict detection via merge-pending →
// conflict-resolving per §7.1); escalation advances the workspace OUT of
// conflict-resolving to discarded, not back through it.
//
// workspace_id derivation: The payload workspace_id is derived from ws.RunID per
// workspace-model.md §4.1.WM-004 ("workspace_id = 'ws-' + run_id"). The UUID part
// of the workspace_id equals the run_id UUID, so core.WorkspaceID(ws.RunID) is the
// correct payload value.
//
// Caller discipline per WM-023 ordering contract:
//  1. Call BuildConflictEscalationPayload → obtain the payload.
//  2. Emit merge_conflict_escalation on the event bus.
//  3. Call Transition(ws, core.WorkspaceStateDiscarded).
//
// Parameters:
//   - ws: workspace record; must be in conflict-resolving state and carry a non-zero RunID.
//   - conflictPaths: list of file paths with merge conflicts per §8.5.6; must be non-empty.
//   - escalatedAt: RFC 3339 wall-clock timestamp of the escalation; must be non-empty.
//
// Returns a non-nil error when any precondition is violated. On success, the
// returned payload passes core.MergeConflictEscalationPayload.Valid().
func BuildConflictEscalationPayload(
	ws *Workspace,
	conflictPaths []string,
	escalatedAt string,
) (core.MergeConflictEscalationPayload, error) {
	if ws == nil {
		return core.MergeConflictEscalationPayload{}, fmt.Errorf(
			"workspace: BuildConflictEscalationPayload: ws must not be nil",
		)
	}

	// Single-entry guard per WM-023: escalation can only execute from
	// conflict-resolving. This enforces that the workspace already entered
	// conflict-resolving at initial conflict detection and is now exhausting
	// the resolution path.
	if ws.State != core.WorkspaceStateConflictResolving {
		return core.MergeConflictEscalationPayload{}, fmt.Errorf(
			"%w: current state is %q",
			ErrEscalationNotInConflictResolving, ws.State,
		)
	}

	if len(conflictPaths) == 0 {
		return core.MergeConflictEscalationPayload{}, fmt.Errorf(
			"workspace: BuildConflictEscalationPayload: conflictPaths must be non-empty",
		)
	}

	if escalatedAt == "" {
		return core.MergeConflictEscalationPayload{}, fmt.Errorf(
			"workspace: BuildConflictEscalationPayload: escalatedAt must be non-empty",
		)
	}

	// workspace_id UUID = run_id UUID per WM-004 derivation rule. The string
	// workspace_id "ws-<run_id>" carries the same UUID value as run_id; the
	// payload field uses the UUID type directly.
	payload := core.MergeConflictEscalationPayload{
		WorkspaceID:   core.WorkspaceID(ws.RunID),
		RunID:         ws.RunID,
		ConflictPaths: conflictPaths,
		EscalatedAt:   escalatedAt,
	}

	if !payload.Valid() {
		return core.MergeConflictEscalationPayload{}, fmt.Errorf(
			"workspace: BuildConflictEscalationPayload: produced invalid payload (workspace_id or run_id may be zero — ensure ws.RunID is non-zero)",
		)
	}

	return payload, nil
}
