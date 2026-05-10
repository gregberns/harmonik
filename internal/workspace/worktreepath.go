package workspace

import "path/filepath"

// DefaultWorktreeRoot is the default worktree-root directory relative to the
// repo root, per workspace-model.md §4.1 WM-002 and §6.2.
//
// The operator-configurable override is tracked in specs/control-points.md
// §4.7 CP-037; a typed config surface is deferred to a follow-up bead.
const DefaultWorktreeRoot = ".harmonik/worktrees"

// WorktreeRootPath returns the absolute path to the worktree root directory.
//
// Per workspace-model.md §4.1 WM-002 and §6.2:
//
//	<repo>/.harmonik/worktrees/
//
// The worktree root MAY be overridden by operator configuration per
// [control-points.md §4.7 CP-037]; pass the operator-supplied override as
// worktreeRootOverride. When worktreeRootOverride is empty, the default
// [DefaultWorktreeRoot] is used. The override MUST be an absolute path or a
// path relative to repoRoot when non-empty; callers sourcing it from
// operator config MUST resolve it against repoRoot before passing.
//
// TODO(hk-ml3rw): replace the *string placeholder with a typed config
// surface once the CP-037 control-point schema is defined in
// internal/operatornfr/ or wherever the control-points config typed surface
// lands. Bead hk-ml3rw tracks "CP-037 worktree-root override config typed
// surface".
func WorktreeRootPath(repoRoot string, worktreeRootOverride *string) string {
	if worktreeRootOverride != nil && *worktreeRootOverride != "" {
		if filepath.IsAbs(*worktreeRootOverride) {
			return *worktreeRootOverride
		}
		return filepath.Join(repoRoot, *worktreeRootOverride)
	}
	return filepath.Join(repoRoot, DefaultWorktreeRoot)
}

// WorktreePath returns the canonical worktree path for the given run ID.
//
// Per workspace-model.md §4.1 WM-002 and §6.2:
//
//	<repo>/.harmonik/worktrees/<run_id>/
//
// where `<repo>` is the absolute path to the local clone of the backing
// repository and `<run_id>` is the run's stable identifier.
//
// The worktree root MAY be overridden by operator configuration per
// [control-points.md §4.7 CP-037]. Pass the operator-supplied override as
// worktreeRootOverride (a *string placeholder; see
// [WorktreeRootPath] godoc for the typed-alias deferral note). When
// worktreeRootOverride is nil or empty, the default
// `<repo>/.harmonik/worktrees/` is used.
//
// WorktreePath does NOT validate runID against the [A-Za-z0-9-]+ regex
// mandated by WM-002 — validation is the caller's responsibility at
// run-create time. UUIDv7, the canonical run_id scheme, satisfies this
// constraint by construction.
//
// Spec refs:
//   - workspace-model.md §4.1 WM-002 — canonical worktree path convention.
//   - workspace-model.md §6.2 — on-disk path table.
//   - control-points.md §4.7 CP-037 — worktree-root operator override.
func WorktreePath(repoRoot, runID string, worktreeRootOverride *string) string {
	return filepath.Join(WorktreeRootPath(repoRoot, worktreeRootOverride), runID)
}
