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

// EvidenceExternalDir returns the canonical relative directory path for
// externalized evidence files belonging to a single transition, within a git
// repository tree.
//
// The directory has the form:
//
//	.harmonik/transitions/<run_id>/<transition_id>/evidence
//
// Per execution-model.md §4.4.EM-021, large evidence or verifier_metrics
// payloads MUST be externalized as sibling files under this directory and
// referenced from the primary <transition_id>.json record by relative path.
// Externalized files are part of the commit's tree and inherit the atomicity
// boundary of §4.4.EM-016 (writing them outside the tree is non-conforming).
//
// Individual externalized-payload filenames are caller-chosen; this function
// returns only the containing directory. The primary <transition_id>.json
// SHOULD remain single-digit KB per EM-021 (advisory; enables cheap
// parseability without loading large payloads).
//
// path.Join (not filepath.Join) is used intentionally: this is a git-tree
// path and must use forward slashes on all platforms.
func EvidenceExternalDir(runID RunID, transitionID TransitionID) string {
	return path.Join(".harmonik", "transitions", runID.String(), transitionID.String(), "evidence")
}
