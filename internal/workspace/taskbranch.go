package workspace

// TaskBranchPrefix is the normative prefix for task branch names per
// workspace-model.md §4.2 WM-005.
//
// Any change to this constant constitutes a breaking branch-naming change and
// requires a migration release per WM-009 and [operator-nfr.md §4.5 ON-018].
const TaskBranchPrefix = "run/"

// TaskBranchName returns the task branch name for the given run ID.
//
// The returned name has the form "run/<runID>" per workspace-model.md §4.2
// WM-005: "Every run's task branch MUST be named `run/<run_id>`."
//
// The caller is responsible for ensuring runID satisfies the filesystem-safe
// regex [A-Za-z0-9-]+ per WM-002. UUIDv7, the canonical run_id scheme,
// satisfies this constraint by construction. TaskBranchName does not validate
// the run ID — validation is the caller's responsibility at run-create time.
//
// Spec refs:
//   - workspace-model.md §4.2 WM-005 — task branch naming convention.
//   - workspace-model.md §4.2 WM-009 — naming is stable across a harmonik
//     minor version; a breaking change requires a migration release.
func TaskBranchName(runID string) string {
	return TaskBranchPrefix + runID
}
