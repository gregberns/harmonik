package core

// WorkspaceObservation is a read-only point-in-time observation of the
// workspace state visible to a reconciliation investigator, as defined in
// specs/reconciliation/schemas.md §6.1 RECORD WorkspaceObservation.
//
// The type was renamed from WorkspaceState on 2026-05-09 (hk-63oh.80) to
// avoid a cross-spec name collision with the WorkspaceState ENUM defined in
// workspace-model.md §6.1, which is already implemented at
// internal/core/workspacestate.go.
//
// # Structural invariants (enforced by Valid)
//
//   - Path is non-empty.
//   - If PathExists is false, BranchTipHash must be nil (worktree absent).
//   - GitInProgressOp is a declared GitInProgressOp constant.
type WorkspaceObservation struct {
	// Path is the absolute filesystem path to the run's worktree.
	// Non-empty required.
	Path string

	// PathExists reports the result of the filesystem probe: true if the
	// worktree directory exists on disk, false otherwise.
	PathExists bool

	// BranchTipHash is the current task-branch tip SHA, or nil when the
	// worktree is missing (PathExists == false). Nullable per spec
	// ("null if worktree missing").
	BranchTipHash *string

	// WIPPresent is true when `git status --porcelain` returns non-empty
	// output or untracked files exist within the worktree.
	WIPPresent bool

	// GitInProgressOp describes any Git operation currently in progress
	// (none | rebase | merge | cherry-pick | bisect). A non-none value is a
	// Cat 6a trigger per specs/reconciliation/spec.md §8.11.
	GitInProgressOp GitInProgressOp
}

// Valid reports whether all structural invariants of the WorkspaceObservation
// are satisfied.
//
// Rules per specs/reconciliation/schemas.md §6.1:
//   - Path is non-empty.
//   - If PathExists is false, BranchTipHash must be nil.
//   - GitInProgressOp is a declared constant (GitInProgressOp.Valid() true).
func (w WorkspaceObservation) Valid() bool {
	if w.Path == "" {
		return false
	}
	if !w.PathExists && w.BranchTipHash != nil {
		return false
	}
	if !w.GitInProgressOp.Valid() {
		return false
	}
	return true
}
