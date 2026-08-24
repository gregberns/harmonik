package core

import (
	"github.com/google/uuid"
)

// This file holds the two payloads for the pre-rebase steps that destroy
// uncommitted work in a run worktree.
//
// Both steps are correct to run: `git rebase` refuses to start on a dirty
// worktree. What was wrong is that each one destroyed in silence. EM-054
// already sets the rule for the main-root refresh, and it applies here
// unchanged: park the work in a recovery patch, warn on standard error, and
// name the loss in an event.
//
// The two payloads stay separate types because a reader who has the event name
// goes looking for the payload of that name.
//
// Path convention, the same on both: WorktreePath and RecoveryPatch are
// absolute, and every entry in Paths is relative to WorktreePath.
//
// The two RecoveryPatch fields have the same name and the same type and they do
// NOT take the same apply command. Each type says which one below. The stderr
// warning says it too, but the event is the durable record and stderr is not.
//
// Refs: hk-nqvqr (churn edits), hk-4q6ah (untracked files).

// RunWorktreeChurnEditsDiscardedPayload is the typed event payload for the
// run_worktree_churn_edits_discarded event (hk-nqvqr).
//
// DiscardDirtyChurn restores a tracked churn path with `git checkout -- <path>`,
// which puts the INDEX content back. The content it destroys is therefore the
// UNSTAGED delta, and that is what RecoveryPatch holds.
//
// Apply it with `git apply -3`, on the machine that ran the merge. The patch is
// cut against the run worktree INDEX rather than against a commit, so its
// preimage can be a blob that exists in that machine's object store and in no
// clone of it. When the discarded path also carried a STAGED edit, `-3` applies
// with conflicts rather than cleanly: the destroyed content is in the file,
// between markers, and a person has to finish the job. A plain `git apply`
// restores the ordinary case and refuses that one.
type RunWorktreeChurnEditsDiscardedPayload struct {
	RunID        RunID  `json:"run_id"`
	BeadID       string `json:"bead_id"`
	WorktreePath string `json:"worktree_path"`

	// Paths lists the churn paths whose uncommitted edit the revert discarded.
	Paths []string `json:"paths"`

	// RecoveryPatch is the path to a `git diff` of the discarded edits, or ""
	// when the patch could not be written.
	RecoveryPatch string `json:"recovery_patch,omitempty"`
}

// Valid reports whether p is a well-formed
// RunWorktreeChurnEditsDiscardedPayload.
//
// RecoveryPatch is deliberately NOT required: a revert that destroyed work and
// then failed to write the patch is exactly the case an operator most needs to
// hear about, so it must remain emittable.
func (p RunWorktreeChurnEditsDiscardedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.WorktreePath == "" {
		return false
	}
	if len(p.Paths) == 0 {
		return false
	}
	return true
}

// RunWorktreeUntrackedFilesRemovedPayload is the typed event payload for the
// run_worktree_untracked_files_removed event (hk-4q6ah).
//
// CleanUntrackedFiles runs `git clean -fd` before the pre-merge rebase. By that
// point CommitResidualDelta has already committed every untracked non-ignored
// file it was allowed to stage, so the files that reach the clean are the ones
// its pathspec excludes. They were deleted with no record.
//
// Apply it with plain `git apply`. RecoveryPatch holds one add-this-file patch
// per rescued file, so it needs no base to apply against.
type RunWorktreeUntrackedFilesRemovedPayload struct {
	RunID        RunID  `json:"run_id"`
	BeadID       string `json:"bead_id"`
	WorktreePath string `json:"worktree_path"`

	// Paths lists the untracked non-ignored files the clean was about to delete.
	Paths []string `json:"paths"`

	// UnsavedPaths lists the entries of Paths that RecoveryPatch does NOT hold,
	// because the rescue could not read them. Usually empty.
	//
	// Without this a reader cannot tell a full rescue from a partial one: the
	// event would name every deleted file next to a non-empty RecoveryPatch and
	// say "saved" about content that is gone. Naming the loss is the whole
	// purpose of this event, so a partial save has to be legible as one.
	UnsavedPaths []string `json:"unsaved_paths,omitempty"`

	// RecoveryPatch is the path to a patch that recreates the deleted files, or
	// "" when the patch could not be written. It holds the files in Paths that
	// are not in UnsavedPaths.
	RecoveryPatch string `json:"recovery_patch,omitempty"`
}

// Valid reports whether p is a well-formed
// RunWorktreeUntrackedFilesRemovedPayload.
//
// RecoveryPatch is optional for the same reason it is optional on the churn
// payload: a delete that saved nothing still has to be announceable.
func (p RunWorktreeUntrackedFilesRemovedPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.WorktreePath == "" {
		return false
	}
	if len(p.Paths) == 0 {
		return false
	}
	return true
}
