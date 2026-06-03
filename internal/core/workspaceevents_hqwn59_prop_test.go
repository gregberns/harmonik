package core

// workspaceevents_hqwn59_prop_test.go — property tests for the Valid() methods
// declared in workspaceevents_hqwn59.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// Approach: rapid generator builds a valid payload, flips exactly one required
// field to its zero/invalid value, asserts Valid()==false; all-valid -> true.
//
// Refs: hk-qgzso (property-test coverage uplift for hk-j3hrn core uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ============================================================
// WorkspaceCreatedPayload
// ============================================================

func TestProp_WorkspaceCreatedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceCreatedPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			Path:         drawNonEmptyString(rt, "path"),
			BranchName:   drawNonEmptyString(rt, "branch"),
			ParentCommit: drawNonEmptyString(rt, "parent_commit"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed WorkspaceCreatedPayload")
		}
	})
}

func TestProp_WorkspaceCreatedPayload_NilWorkspaceIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceCreatedPayload{
			WorkspaceID:  WorkspaceID(uuid.Nil),
			Path:         drawNonEmptyString(rt, "path"),
			BranchName:   drawNonEmptyString(rt, "branch"),
			ParentCommit: drawNonEmptyString(rt, "parent_commit"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil WorkspaceID")
		}
	})
}

func TestProp_WorkspaceCreatedPayload_EmptyPathRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceCreatedPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			Path:         "",
			BranchName:   drawNonEmptyString(rt, "branch"),
			ParentCommit: drawNonEmptyString(rt, "parent_commit"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty Path")
		}
	})
}

func TestProp_WorkspaceCreatedPayload_EmptyBranchNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceCreatedPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			Path:         drawNonEmptyString(rt, "path"),
			BranchName:   "",
			ParentCommit: drawNonEmptyString(rt, "parent_commit"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty BranchName")
		}
	})
}

func TestProp_WorkspaceCreatedPayload_EmptyParentCommitRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceCreatedPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			Path:         drawNonEmptyString(rt, "path"),
			BranchName:   drawNonEmptyString(rt, "branch"),
			ParentCommit: "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ParentCommit")
		}
	})
}

// ============================================================
// WorkspaceLeasedPayload
// ============================================================

func TestProp_WorkspaceLeasedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceLeasedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			LeasedAt:    drawNonEmptyString(rt, "leased_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed WorkspaceLeasedPayload")
		}
	})
}

func TestProp_WorkspaceLeasedPayload_NilWorkspaceIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceLeasedPayload{
			WorkspaceID: WorkspaceID(uuid.Nil),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			LeasedAt:    drawNonEmptyString(rt, "leased_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil WorkspaceID")
		}
	})
}

func TestProp_WorkspaceLeasedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceLeasedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(uuid.Nil),
			LeasedAt:    drawNonEmptyString(rt, "leased_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_WorkspaceLeasedPayload_EmptyLeasedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceLeasedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			LeasedAt:    "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty LeasedAt")
		}
	})
}

// ============================================================
// WorkspaceMergeStatus
// ============================================================

func TestProp_WorkspaceMergeStatus_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allWorkspaceMergeStatuses {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if WorkspaceMergeStatus(s).Valid() {
			rt.Errorf("Valid() == true for unknown WorkspaceMergeStatus %q", s)
		}
	})
}

func TestProp_WorkspaceMergeStatus_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allWorkspaceMergeStatuses).Draw(rt, "status")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared WorkspaceMergeStatus %q", v)
		}
	})
}

// ============================================================
// WorkspaceMergeStatusPayload
// ============================================================

func TestProp_WorkspaceMergeStatusPayload_AllValidPendingAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			Status:       WorkspaceMergeStatusPending,
			SourceBranch: drawNonEmptyString(rt, "source"),
			TargetBranch: drawNonEmptyString(rt, "target"),
			ChangedAt:    drawNonEmptyString(rt, "changed_at"),
			// MergeCommitHash must be nil for pending
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed WorkspaceMergeStatusPayload (pending)")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_AllValidMergedAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		hash := drawNonEmptyString(rt, "merge_hash")
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:     WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			Status:          WorkspaceMergeStatusMerged,
			SourceBranch:    drawNonEmptyString(rt, "source"),
			TargetBranch:    drawNonEmptyString(rt, "target"),
			MergeCommitHash: &hash,
			ChangedAt:       drawNonEmptyString(rt, "changed_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed WorkspaceMergeStatusPayload (merged)")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_NilWorkspaceIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(uuid.Nil),
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			Status:       WorkspaceMergeStatusPending,
			SourceBranch: drawNonEmptyString(rt, "source"),
			TargetBranch: drawNonEmptyString(rt, "target"),
			ChangedAt:    drawNonEmptyString(rt, "changed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil WorkspaceID")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:        RunID(uuid.Nil),
			Status:       WorkspaceMergeStatusPending,
			SourceBranch: drawNonEmptyString(rt, "source"),
			TargetBranch: drawNonEmptyString(rt, "target"),
			ChangedAt:    drawNonEmptyString(rt, "changed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_EmptySourceBranchRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			Status:       WorkspaceMergeStatusPending,
			SourceBranch: "",
			TargetBranch: drawNonEmptyString(rt, "target"),
			ChangedAt:    drawNonEmptyString(rt, "changed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SourceBranch")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_EmptyTargetBranchRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			Status:       WorkspaceMergeStatusPending,
			SourceBranch: drawNonEmptyString(rt, "source"),
			TargetBranch: "",
			ChangedAt:    drawNonEmptyString(rt, "changed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty TargetBranch")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_PendingWithCommitHashRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		hash := drawNonEmptyString(rt, "hash")
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:     WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			Status:          WorkspaceMergeStatusPending,
			SourceBranch:    drawNonEmptyString(rt, "source"),
			TargetBranch:    drawNonEmptyString(rt, "target"),
			MergeCommitHash: &hash, // must be nil when pending
			ChangedAt:       drawNonEmptyString(rt, "changed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false for status=pending with non-nil MergeCommitHash")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_MergedWithoutCommitHashRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			Status:       WorkspaceMergeStatusMerged,
			SourceBranch: drawNonEmptyString(rt, "source"),
			TargetBranch: drawNonEmptyString(rt, "target"),
			// MergeCommitHash is nil — required when merged
			ChangedAt: drawNonEmptyString(rt, "changed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false for status=merged with nil MergeCommitHash")
		}
	})
}

func TestProp_WorkspaceMergeStatusPayload_EmptyChangedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceMergeStatusPayload{
			WorkspaceID:  WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			Status:       WorkspaceMergeStatusPending,
			SourceBranch: drawNonEmptyString(rt, "source"),
			TargetBranch: drawNonEmptyString(rt, "target"),
			ChangedAt:    "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ChangedAt")
		}
	})
}

// ============================================================
// WorkspaceDiscardedPayload
// ============================================================

func TestProp_WorkspaceDiscardedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceDiscardedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			Reason:      drawNonEmptyString(rt, "reason"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed WorkspaceDiscardedPayload")
		}
	})
}

func TestProp_WorkspaceDiscardedPayload_NilWorkspaceIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceDiscardedPayload{
			WorkspaceID: WorkspaceID(uuid.Nil),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			Reason:      drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil WorkspaceID")
		}
	})
}

func TestProp_WorkspaceDiscardedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceDiscardedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(uuid.Nil),
			Reason:      drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_WorkspaceDiscardedPayload_EmptyReasonRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceDiscardedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			Reason:      "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty Reason")
		}
	})
}

// ============================================================
// WorkspaceInterruptedPayload
// ============================================================

func TestProp_WorkspaceInterruptedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceInterruptedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			DetectedAt:  drawNonEmptyString(rt, "detected_at"),
			Category:    rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed WorkspaceInterruptedPayload")
		}
	})
}

func TestProp_WorkspaceInterruptedPayload_NilWorkspaceIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceInterruptedPayload{
			WorkspaceID: WorkspaceID(uuid.Nil),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			DetectedAt:  drawNonEmptyString(rt, "detected_at"),
			Category:    rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil WorkspaceID")
		}
	})
}

func TestProp_WorkspaceInterruptedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceInterruptedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(uuid.Nil),
			DetectedAt:  drawNonEmptyString(rt, "detected_at"),
			Category:    rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_WorkspaceInterruptedPayload_EmptyDetectedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkspaceInterruptedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			DetectedAt:  "",
			Category:    rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty DetectedAt")
		}
	})
}

func TestProp_WorkspaceInterruptedPayload_InvalidCategoryRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allReconciliationCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "cat_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := WorkspaceInterruptedPayload{
			WorkspaceID: WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:       RunID(drawNonNilUUID(rt, "run_id")),
			DetectedAt:  drawNonEmptyString(rt, "detected_at"),
			Category:    ReconciliationCategory(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown category %q", s)
		}
	})
}

// ============================================================
// MergeConflictEscalationPayload
// ============================================================

func TestProp_MergeConflictEscalationPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MergeConflictEscalationPayload{
			WorkspaceID:   WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			ConflictPaths: []string{drawNonEmptyString(rt, "path")},
			EscalatedAt:   drawNonEmptyString(rt, "escalated_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed MergeConflictEscalationPayload")
		}
	})
}

func TestProp_MergeConflictEscalationPayload_NilWorkspaceIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MergeConflictEscalationPayload{
			WorkspaceID:   WorkspaceID(uuid.Nil),
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			ConflictPaths: []string{"a.go"},
			EscalatedAt:   drawNonEmptyString(rt, "escalated_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil WorkspaceID")
		}
	})
}

func TestProp_MergeConflictEscalationPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MergeConflictEscalationPayload{
			WorkspaceID:   WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:         RunID(uuid.Nil),
			ConflictPaths: []string{"a.go"},
			EscalatedAt:   drawNonEmptyString(rt, "escalated_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_MergeConflictEscalationPayload_EmptyConflictPathsRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MergeConflictEscalationPayload{
			WorkspaceID:   WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			ConflictPaths: []string{},
			EscalatedAt:   drawNonEmptyString(rt, "escalated_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ConflictPaths slice")
		}
	})
}

func TestProp_MergeConflictEscalationPayload_EmptyEscalatedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MergeConflictEscalationPayload{
			WorkspaceID:   WorkspaceID(drawNonNilUUID(rt, "ws_id")),
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			ConflictPaths: []string{"a.go"},
			EscalatedAt:   "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EscalatedAt")
		}
	})
}
