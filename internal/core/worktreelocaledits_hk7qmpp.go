package core

import (
	"github.com/google/uuid"
)

// WorkingTreeLocalEditsOverwrittenPayload is the typed event payload for the
// working_tree_local_edits_overwritten event (hk-7qmpp).
//
// The EM-054 post-merge refresh re-syncs the main working tree to the paths the
// merged commit itself changed. When one of those paths also carried an
// uncommitted local edit, the refresh overwrites it: the merged commit is
// authoritative for its own paths. That overwrite MUST NOT be silent — the
// 2026-07-22 data-loss incident was not that a refresh happened, but that it
// happened without naming what it destroyed.
//
// Fields:
//   - RunID:         the run whose merge triggered the refresh (required).
//   - BeadID:        the bead correlation identifier (required, non-empty).
//   - MainPath:      absolute path to the main repo root refreshed (required).
//   - Paths:         the overwritten paths — merged paths that carried an
//     uncommitted local edit at refresh time. Required (non-empty): the event
//     is only emitted when at least one path was overwritten.
//   - RecoveryPatch: path to a diff of the overwritten edits, written before
//     the refresh so the work is recoverable ("park it before you delete it").
//     Empty when the patch could not be written — the event still fires, since
//     naming the loss matters more than the recovery file.
type WorkingTreeLocalEditsOverwrittenPayload struct {
	// RunID identifies the run whose merge refreshed the tree. Required.
	RunID RunID `json:"run_id"`

	// BeadID is the opaque bead correlation identifier. Required.
	BeadID string `json:"bead_id"`

	// MainPath is the absolute path to the main repo root that was refreshed.
	MainPath string `json:"main_path"`

	// Paths lists the merged paths whose uncommitted local edits the refresh
	// overwrote. Non-empty by construction.
	Paths []string `json:"paths"`

	// RecoveryPatch is the path to a `git diff` of the overwritten edits, or
	// "" when the patch could not be written.
	RecoveryPatch string `json:"recovery_patch,omitempty"`
}

// Valid reports whether p is a well-formed
// WorkingTreeLocalEditsOverwrittenPayload.
//
// RecoveryPatch is deliberately NOT required: a refresh that overwrote work but
// failed to write the recovery patch is exactly the case an operator most needs
// to hear about, so it must remain emittable.
func (p WorkingTreeLocalEditsOverwrittenPayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.MainPath == "" {
		return false
	}
	if len(p.Paths) == 0 {
		return false
	}
	return true
}
