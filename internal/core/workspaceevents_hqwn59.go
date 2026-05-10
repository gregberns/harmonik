package core

import "github.com/google/uuid"

// workspaceevents_hqwn59.go — event-bus payload types for §8.5 workspace
// lifecycle events covered by this implementer wave (hqwn59b):
//   - workspace_created          (§8.5.1)
//   - workspace_leased           (§8.5.2)
//   - workspace_merge_status     (§8.5.3)
//   - workspace_discarded        (§8.5.4)
//   - workspace_interrupted      (§8.5.5)
//   - merge_conflict_escalation  (§8.5.6)
//
// Spec ref: specs/event-model.md §8.5.
// Bead refs: hk-hqwn.59.37, hk-hqwn.59.38, hk-hqwn.59.39, hk-hqwn.59.40,
//            hk-hqwn.59.41, hk-hqwn.59.42.

// WorkspaceCreatedPayload is the typed event payload for the workspace_created
// event (event-model.md §8.5.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — workspace lifecycle observability per
// workspace-model.md §4.4).
//
// Emitted by the workspace-manager (S06) when a new workspace (git worktree)
// is created for a run.
//
// # Payload fields (event-model.md §8.5.1)
//
//   - workspace_id   — stable UUID identifier for this workspace
//   - path           — absolute filesystem path to the worktree
//   - branch_name    — task branch name created for this workspace
//   - parent_commit  — git commit SHA of the parent commit at creation time
type WorkspaceCreatedPayload struct {
	// WorkspaceID is the stable UUID identifier for this workspace.
	// Required (must not be zero). See workspaceid.go (hk-hqwn.74).
	WorkspaceID WorkspaceID `json:"workspace_id"`

	// Path is the absolute filesystem path to the git worktree.
	// Required (non-empty).
	Path string `json:"path"`

	// BranchName is the task branch name created for this workspace.
	// Required (non-empty).
	BranchName string `json:"branch_name"`

	// ParentCommit is the git commit SHA of the parent commit at creation time.
	// Required (non-empty).
	ParentCommit string `json:"parent_commit"`
}

// Valid reports whether p is a well-formed WorkspaceCreatedPayload.
//
// Rules per event-model.md §8.5.1:
//   - WorkspaceID must not be uuid.Nil.
//   - Path must be non-empty.
//   - BranchName must be non-empty.
//   - ParentCommit must be non-empty.
func (p WorkspaceCreatedPayload) Valid() bool {
	if uuid.UUID(p.WorkspaceID) == uuid.Nil {
		return false
	}
	if p.Path == "" {
		return false
	}
	if p.BranchName == "" {
		return false
	}
	if p.ParentCommit == "" {
		return false
	}
	return true
}

// WorkspaceLeasedPayload is the typed event payload for the workspace_leased
// event (event-model.md §8.5.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — workspace lifecycle observability; the
// orchestrator uses this to track run-to-workspace assignment per
// workspace-model.md §4.4).
//
// Emitted by the workspace-manager (S06) when a workspace is leased to a run.
//
// # Payload fields (event-model.md §8.5.2)
//
//   - workspace_id — stable UUID identifier for this workspace
//   - run_id       — the run that was granted the lease
//   - leased_at    — RFC 3339 wall-clock timestamp at lease grant
type WorkspaceLeasedPayload struct {
	// WorkspaceID is the stable UUID identifier for this workspace.
	// Required (must not be zero). See workspaceid.go (hk-hqwn.74).
	WorkspaceID WorkspaceID `json:"workspace_id"`

	// RunID is the run that was granted the workspace lease.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// LeasedAt is the RFC 3339 wall-clock timestamp at lease grant.
	// Required (non-empty).
	LeasedAt string `json:"leased_at"`
}

// Valid reports whether p is a well-formed WorkspaceLeasedPayload.
//
// Rules per event-model.md §8.5.2:
//   - WorkspaceID must not be uuid.Nil.
//   - RunID must not be uuid.Nil.
//   - LeasedAt must be non-empty.
func (p WorkspaceLeasedPayload) Valid() bool {
	if uuid.UUID(p.WorkspaceID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.LeasedAt == "" {
		return false
	}
	return true
}

// WorkspaceMergeStatus is the typed discriminator for the status field of a
// workspace_merge_status event (event-model.md §8.5.3 §6.3).
//
// This is a paired-phase lifecycle type per §8.9(h): emitters MUST emit only
// on status transitions; successive emissions with identical status for the
// same workspace are forbidden. The changed_at field MUST carry millisecond
// resolution per §8.9(h).
type WorkspaceMergeStatus string

const (
	// WorkspaceMergeStatusPending indicates the merge is pending (not yet completed).
	WorkspaceMergeStatusPending WorkspaceMergeStatus = "pending"

	// WorkspaceMergeStatusMerged indicates the merge has completed successfully.
	WorkspaceMergeStatusMerged WorkspaceMergeStatus = "merged"
)

// Valid reports whether s is one of the two declared WorkspaceMergeStatus constants.
func (s WorkspaceMergeStatus) Valid() bool {
	switch s {
	case WorkspaceMergeStatusPending, WorkspaceMergeStatusMerged:
		return true
	default:
		return false
	}
}

// WorkspaceMergeStatusPayload is the typed event payload for the
// workspace_merge_status event (event-model.md §8.5.3 §6.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
// Durability class: F (fsync-boundary — losing a merge event would force git-DAG
// reconstruction per A.3; beads-integration consumers use this event as the merge
// authority per workspace-model.md §4.5).
//
// Emitted by the workspace-manager (S06) on workspace merge-phase transitions.
// This is a paired-phase lifecycle event (§8.9(h)); emitters MUST emit only on
// transitions. merge_commit_hash is null when status=pending.
//
// # Payload fields (event-model.md §8.5.3 §6.3)
//
//   - workspace_id      — stable UUID identifier for this workspace
//   - run_id            — the run whose task branch is being merged
//   - status            — merge phase (pending / merged)
//   - source_branch     — the task branch being merged
//   - target_branch     — the integration branch being merged into
//   - merge_commit_hash — git SHA of the merge commit (null when status=pending)
//   - changed_at        — RFC 3339 wall-clock timestamp at this phase transition (millisecond resolution)
type WorkspaceMergeStatusPayload struct {
	// WorkspaceID is the stable UUID identifier for this workspace.
	// Required (must not be zero). See workspaceid.go (hk-hqwn.74).
	WorkspaceID WorkspaceID `json:"workspace_id"`

	// RunID is the run whose task branch is being merged.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// Status is the merge phase at this transition. Required; must be a valid
	// WorkspaceMergeStatus constant. Emitters MUST emit only on transitions per §8.9(h).
	Status WorkspaceMergeStatus `json:"status"`

	// SourceBranch is the task branch being merged. Required (non-empty).
	SourceBranch string `json:"source_branch"`

	// TargetBranch is the integration branch being merged into. Required (non-empty).
	TargetBranch string `json:"target_branch"`

	// MergeCommitHash is the git SHA of the merge commit. Nil when Status=pending;
	// required (non-empty) when Status=merged.
	MergeCommitHash *string `json:"merge_commit_hash,omitempty"`

	// ChangedAt is the RFC 3339 wall-clock timestamp at this phase transition.
	// Required (non-empty). MUST carry millisecond resolution per §8.9(h).
	ChangedAt string `json:"changed_at"`
}

// Valid reports whether p is a well-formed WorkspaceMergeStatusPayload.
//
// Rules per event-model.md §8.5.3 §6.3:
//   - WorkspaceID must not be uuid.Nil.
//   - RunID must not be uuid.Nil.
//   - Status must be a valid WorkspaceMergeStatus constant.
//   - SourceBranch must be non-empty.
//   - TargetBranch must be non-empty.
//   - MergeCommitHash must be nil when Status=pending; non-nil and non-empty when Status=merged.
//   - ChangedAt must be non-empty.
func (p WorkspaceMergeStatusPayload) Valid() bool {
	if uuid.UUID(p.WorkspaceID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if !p.Status.Valid() {
		return false
	}
	if p.SourceBranch == "" {
		return false
	}
	if p.TargetBranch == "" {
		return false
	}
	// merge_commit_hash: null when pending, required when merged
	switch p.Status {
	case WorkspaceMergeStatusPending:
		if p.MergeCommitHash != nil {
			return false
		}
	case WorkspaceMergeStatusMerged:
		if p.MergeCommitHash == nil || *p.MergeCommitHash == "" {
			return false
		}
	}
	if p.ChangedAt == "" {
		return false
	}
	return true
}

// WorkspaceDiscardedPayload is the typed event payload for the
// workspace_discarded event (event-model.md §8.5.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — workspace lifecycle observability per
// workspace-model.md §4.4).
//
// Emitted by the workspace-manager (S06) when a workspace is discarded
// (worktree deleted without merge).
//
// # Payload fields (event-model.md §8.5.4)
//
//   - workspace_id — stable UUID identifier for this workspace
//   - run_id       — the run associated with the discarded workspace
//   - reason       — human-readable reason for the discard
type WorkspaceDiscardedPayload struct {
	// WorkspaceID is the stable UUID identifier for this workspace.
	// Required (must not be zero). See workspaceid.go (hk-hqwn.74).
	WorkspaceID WorkspaceID `json:"workspace_id"`

	// RunID is the run associated with the discarded workspace.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// Reason is a human-readable reason for the workspace being discarded.
	// Required (non-empty).
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed WorkspaceDiscardedPayload.
//
// Rules per event-model.md §8.5.4:
//   - WorkspaceID must not be zero.
//   - RunID must not be uuid.Nil.
//   - Reason must be non-empty.
func (p WorkspaceDiscardedPayload) Valid() bool {
	if uuid.UUID(p.WorkspaceID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.Reason == "" {
		return false
	}
	return true
}

// WorkspaceInterruptedPayload is the typed event payload for the
// workspace_interrupted event (event-model.md §8.5.5).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — reconciliation and audit input; the category
// field carries a Cat 6 classification per workspace-model.md §4.4 and
// reconciliation/spec.md §8).
//
// Emitted by the reconciliation detector when it detects an interrupted workspace
// (a worktree in an in-progress git state or other Cat 6 condition).
//
// # Payload fields (event-model.md §8.5.5)
//
//   - workspace_id — stable UUID identifier for the interrupted workspace
//   - run_id       — the run associated with the interrupted workspace
//   - detected_at  — RFC 3339 wall-clock timestamp at detection
//   - category     — reconciliation category (Cat 6) per reconciliation/spec.md §8
type WorkspaceInterruptedPayload struct {
	// WorkspaceID is the stable UUID identifier for the interrupted workspace.
	// Required (must not be zero). See workspaceid.go (hk-hqwn.74).
	WorkspaceID WorkspaceID `json:"workspace_id"`

	// RunID is the run associated with the interrupted workspace.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// DetectedAt is the RFC 3339 wall-clock timestamp at detection.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`

	// Category is the reconciliation category (Cat 6) at detection time,
	// per reconciliation/spec.md §8. Required; must be a valid ReconciliationCategory.
	Category ReconciliationCategory `json:"category"`
}

// Valid reports whether p is a well-formed WorkspaceInterruptedPayload.
//
// Rules per event-model.md §8.5.5:
//   - WorkspaceID must not be zero.
//   - RunID must not be uuid.Nil.
//   - DetectedAt must be non-empty.
//   - Category must be a valid ReconciliationCategory constant.
func (p WorkspaceInterruptedPayload) Valid() bool {
	if uuid.UUID(p.WorkspaceID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if p.DetectedAt == "" {
		return false
	}
	if !p.Category.Valid() {
		return false
	}
	return true
}

// MergeConflictEscalationPayload is the typed event payload for the
// merge_conflict_escalation event (event-model.md §8.5.6).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — operator-observability and audit; the
// operator must manually resolve the conflict per workspace-model.md §4.5).
//
// Emitted by the workspace-manager (S06) when a merge conflict cannot be
// auto-resolved and requires operator escalation.
//
// # Payload fields (event-model.md §8.5.6)
//
//   - workspace_id   — stable UUID identifier for this workspace
//   - run_id         — the run whose merge produced the conflict
//   - conflict_paths — list of paths with merge conflicts
//   - escalated_at   — RFC 3339 wall-clock timestamp at escalation
type MergeConflictEscalationPayload struct {
	// WorkspaceID is the stable UUID identifier for this workspace.
	// Required (must not be zero). See workspaceid.go (hk-hqwn.74).
	WorkspaceID WorkspaceID `json:"workspace_id"`

	// RunID is the run whose merge produced the conflict.
	// Required (must not be uuid.Nil).
	RunID RunID `json:"run_id"`

	// ConflictPaths is the list of file paths with merge conflicts.
	// Required (non-nil; must be non-empty to be a valid escalation).
	ConflictPaths []string `json:"conflict_paths"`

	// EscalatedAt is the RFC 3339 wall-clock timestamp at escalation.
	// Required (non-empty).
	EscalatedAt string `json:"escalated_at"`
}

// Valid reports whether p is a well-formed MergeConflictEscalationPayload.
//
// Rules per event-model.md §8.5.6:
//   - WorkspaceID must not be zero.
//   - RunID must not be uuid.Nil.
//   - ConflictPaths must be non-nil and non-empty.
//   - EscalatedAt must be non-empty.
func (p MergeConflictEscalationPayload) Valid() bool {
	if uuid.UUID(p.WorkspaceID) == uuid.Nil {
		return false
	}
	if uuid.UUID(p.RunID) == uuid.Nil {
		return false
	}
	if len(p.ConflictPaths) == 0 {
		return false
	}
	if p.EscalatedAt == "" {
		return false
	}
	return true
}
