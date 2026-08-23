package core

// WIPCapture holds the work-in-progress snapshot captured from the outer
// run's worktree before the investigator emits a reopen-bead verdict.
//
// All fields are captured at a single point in time. Empty strings / nil
// slices are valid when the corresponding git query returned no output.
//
// # Structural invariants (enforced by Valid)
//
//   - WorktreePath is non-empty (identifies the outer run's worktree).
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019.
type WIPCapture struct {
	// WorktreePath is the absolute path of the outer run's worktree that was
	// inspected. Non-empty required.
	WorktreePath string

	// GitStatusPorcelain is the verbatim output of `git status --porcelain`
	// run inside the outer run's worktree. Empty when the worktree is clean.
	GitStatusPorcelain string

	// GitDiff is the verbatim output of `git diff HEAD` run inside the outer
	// run's worktree. Captures staged and unstaged modifications. Empty when
	// there are no tracked-file modifications.
	GitDiff string

	// UntrackedFiles is the list of untracked file paths reported by
	// `git status --porcelain` (lines beginning with "??"). Nil and empty
	// slices are both valid.
	UntrackedFiles []string
}

// Valid reports whether w satisfies the structural invariants of WIPCapture.
//
// Rule: WorktreePath must be non-empty.
func (w WIPCapture) Valid() bool {
	return w.WorktreePath != ""
}

// HasWIP reports whether there is any recoverable WIP in the captured snapshot.
// Returns true when GitStatusPorcelain is non-empty or UntrackedFiles is
// non-empty.
func (w WIPCapture) HasWIP() bool {
	return w.GitStatusPorcelain != "" || len(w.UntrackedFiles) > 0
}

// WIPCaptureFileNames declares the canonical file names written under the
// wip-capture/ directory by the investigator.
//
// Spec ref: reconciliation/spec.md §4.4 RC-019 — "annotated files under
// .harmonik/reconciliation/<investigator_run_id>/wip-capture/".
const (
	// WIPCaptureStatusFile is the file name for the `git status --porcelain`
	// output inside the wip-capture/ directory.
	WIPCaptureStatusFile = "git-status.txt"

	// WIPCaptureDiffFile is the file name for the `git diff HEAD` output
	// inside the wip-capture/ directory.
	WIPCaptureDiffFile = "git-diff.patch"

	// WIPCaptureUntrackedFile is the file name for the newline-separated list
	// of untracked file paths inside the wip-capture/ directory.
	WIPCaptureUntrackedFile = "untracked-files.txt"
)
