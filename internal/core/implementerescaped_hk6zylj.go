package core

// implementerescaped_hk6zylj.go — ImplementerEscapedWorktreePayload (hk-6zylj).
//
// Emitted by the daemon's workloop after the implementer process exits if the
// MAIN repo's working tree contains dirty files outside the normal harmonik
// churn allowlist (.harmonik/, .claude/, .beads/issues.jsonl). This signals
// implementer cross-contamination: the implementer wrote files into the main
// repo via absolute MAIN-repo paths instead of staying inside its worktree.
//
// The run is failed on this event; the run branch will typically have no new
// commit, but main is dirty. Operator inspects via `git -C <main> status`.
//
// Durability class: F (terminal-state landmark — the run is failed on this
// event; loss would orphan a real cross-contamination incident).
//
// Bead ref: hk-6zylj.

import (
	"github.com/google/uuid"
)

// ImplementerEscapedWorktreePayload is the typed event payload for the
// implementer_escaped_worktree event (hk-6zylj).
//
// Fields:
//   - RunID:      the run whose implementer escaped (required).
//   - BeadID:     the bead correlation identifier (required, non-empty).
//   - MainPath:   absolute path to the main repo root checked (required).
//   - DirtyFiles: list of dirty path entries from `git status --porcelain`
//     filtered by the daemon allowlist. Required (non-empty); each entry is
//     the porcelain status line minus the leading XY-and-space prefix (just
//     the path).
type ImplementerEscapedWorktreePayload struct {
	// RunID identifies the run whose implementer escaped. Required.
	RunID RunID `json:"run_id"`

	// BeadID is the opaque bead correlation identifier. Required.
	BeadID string `json:"bead_id"`

	// MainPath is the absolute path to the main repo root that was checked.
	MainPath string `json:"main_path"`

	// DirtyFiles is the list of dirty paths in the main repo that triggered
	// the escape detection (after allowlist filtering). Non-empty by
	// construction: the event is only emitted when at least one file is dirty.
	DirtyFiles []string `json:"dirty_files"`
}

// Valid reports whether p is a well-formed ImplementerEscapedWorktreePayload.
func (p ImplementerEscapedWorktreePayload) Valid() bool {
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.MainPath == "" {
		return false
	}
	if len(p.DirtyFiles) == 0 {
		return false
	}
	return true
}
