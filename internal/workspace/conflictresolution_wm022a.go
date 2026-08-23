package workspace

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
