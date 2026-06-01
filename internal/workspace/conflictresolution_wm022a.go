package workspace

// conflictresolution_wm022a.go — all-mechanical task branch escalation guard (WM-022a).
//
// Implements the deterministic (mechanism-tagged) guard for all-mechanical task branches
// per workspace-model.md §4.6.WM-022a:
//
//   - IsAllMechanicalBranch — predicate: returns true when implementer_handler_ref is
//     null (the task branch carries no agentic-node commits).
//
// When IsAllMechanicalBranch returns true, the workspace manager MUST skip WM-024
// re-dispatch and emit merge_conflict_escalation directly per WM-023 on conflict
// detection. The system MUST NOT silently remap the implementer role to an unrelated
// handler class.
//
// Relationship to ShouldDispatchConflictResolver (WM-024):
//   ShouldDispatchConflictResolver encodes the full routing decision (null ref,
//   retired handler, cap exhausted, or dispatch). IsAllMechanicalBranch is the
//   single-concern guard for the null-ref case only — it makes the WM-022a invariant
//   explicit and testable at the workspace record level, decoupled from the cap and
//   retirement checks of WM-024.
//
// Spec refs:
//   - specs/workspace-model.md §4.6 WM-022a — all-mechanical escalation rule.
//   - specs/workspace-model.md §4.6 WM-022  — null assignment at merge-pending entry.
//   - specs/workspace-model.md §4.6 WM-023  — escalation execution path.
//   - specs/workspace-model.md §4.6 WM-024  — WM-024 is SKIPPED for all-mechanical branches.
//
// Bead ref: hk-8mwo.34.

import "github.com/gregberns/harmonik/internal/core"

// IsAllMechanicalBranch reports whether ws represents an all-mechanical task branch
// per workspace-model.md §4.6 WM-022a.
//
// A task branch is all-mechanical when Workspace.ImplementerHandlerRef is nil,
// meaning the sidecar walk (WM-022) found no agentic session among the sessions
// recorded for this workspace. This occurs when the branch carries only non-agentic
// commits — mechanical refactors, generated-code landings, or merge-node commits
// with no agentic ancestry.
//
// When this returns true:
//   - The workspace manager MUST NOT attempt WM-024 re-dispatch on conflict.
//   - The workspace manager MUST emit merge_conflict_escalation per WM-023 directly.
//   - The workspace manager MUST NOT silently remap the implementer role to any
//     other handler class.
//
// A nil ws is treated as non-all-mechanical (returns false) to avoid panics in
// defensive callers; the workspace manager should not pass a nil workspace here.
func IsAllMechanicalBranch(ws *Workspace) bool {
	if ws == nil {
		return false
	}
	return ws.ImplementerHandlerRef == nil
}

// IsAllMechanicalRef reports whether the given implementer_handler_ref value signals
// an all-mechanical task branch per WM-022a.
//
// This is the ref-level variant of IsAllMechanicalBranch for callers that already
// hold the ref directly (e.g., ShouldDispatchConflictResolver). A nil ref means
// the workspace has no agentic ancestry and MUST escalate directly on conflict.
func IsAllMechanicalRef(ref *core.HandlerRef) bool {
	return ref == nil
}
