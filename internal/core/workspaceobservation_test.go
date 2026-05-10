package core

import (
	"testing"
)

// workspaceObsFixture returns a fully-populated WorkspaceObservation with
// all required fields set to valid values. Tests mutate individual fields to
// probe Valid().
func workspaceObsFixture(t *testing.T) WorkspaceObservation {
	t.Helper()
	hash := "abc123def456"
	return WorkspaceObservation{
		Path:            "/home/runner/worktrees/task-branch",
		PathExists:      true,
		BranchTipHash:   &hash,
		WIPPresent:      false,
		GitInProgressOp: GitInProgressOpNone,
	}
}

func TestWorkspaceObservationValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	w := workspaceObsFixture(t)
	if !w.Valid() {
		t.Error("Valid() = false for fully-populated WorkspaceObservation, want true")
	}
}

func TestWorkspaceObservationValid_ZeroValue(t *testing.T) {
	t.Parallel()

	w := WorkspaceObservation{}
	if w.Valid() {
		t.Error("Valid() = true for zero-value WorkspaceObservation, want false")
	}
}

func TestWorkspaceObservationValid_EmptyPath(t *testing.T) {
	t.Parallel()

	w := workspaceObsFixture(t)
	w.Path = ""
	if w.Valid() {
		t.Error("Valid() = true with empty Path, want false")
	}
}

func TestWorkspaceObservationValid_MissingWorktreeNilHash(t *testing.T) {
	t.Parallel()

	w := workspaceObsFixture(t)
	w.PathExists = false
	w.BranchTipHash = nil
	if !w.Valid() {
		t.Error("Valid() = false when PathExists=false and BranchTipHash=nil, want true")
	}
}

func TestWorkspaceObservationValid_MissingWorktreeNonNilHash(t *testing.T) {
	t.Parallel()

	w := workspaceObsFixture(t)
	w.PathExists = false
	// BranchTipHash still set from fixture — invariant violation
	if w.Valid() {
		t.Error("Valid() = true when PathExists=false but BranchTipHash is non-nil, want false")
	}
}

func TestWorkspaceObservationValid_AllGitInProgressOps(t *testing.T) {
	t.Parallel()

	ops := []GitInProgressOp{
		GitInProgressOpNone,
		GitInProgressOpRebase,
		GitInProgressOpMerge,
		GitInProgressOpCherryPick,
		GitInProgressOpBisect,
	}
	for _, op := range ops {
		op := op
		t.Run(string(op), func(t *testing.T) {
			t.Parallel()
			w := workspaceObsFixture(t)
			w.GitInProgressOp = op
			if !w.Valid() {
				t.Errorf("Valid() = false for git_in_progress_op=%q, want true", op)
			}
		})
	}
}

func TestWorkspaceObservationValid_UnknownGitInProgressOp(t *testing.T) {
	t.Parallel()

	w := workspaceObsFixture(t)
	w.GitInProgressOp = GitInProgressOp("not-an-op")
	if w.Valid() {
		t.Error("Valid() = true with unknown GitInProgressOp, want false")
	}
}

func TestWorkspaceObservationValid_WIPPresentTrue(t *testing.T) {
	t.Parallel()

	w := workspaceObsFixture(t)
	w.WIPPresent = true
	if !w.Valid() {
		t.Error("Valid() = false when WIPPresent=true, want true")
	}
}
