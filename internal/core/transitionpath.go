// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import "path"

// TransitionRecordPath returns the canonical relative path for a transition record
// within a git repository tree.
//
// The path has the form:
//
//	.harmonik/transitions/<run_id>/<transition_id>.json
//
// This satisfies execution-model.md §4.4 EM-019: given a run_id, a transition_id,
// and a commit on the run's task branch with matching trailers, the transition
// record MUST be retrievable via:
//
//	git show <commit>:.harmonik/transitions/<run_id>/<transition_id>.json
//
// No cross-commit index may be required to resolve this path.
//
// path.Join (not filepath.Join) is used intentionally: this path is a git-tree
// path and must use forward slashes on all platforms.
func TransitionRecordPath(runID RunID, transitionID TransitionID) string {
	return path.Join(".harmonik", "transitions", runID.String(), transitionID.String()+".json")
}
