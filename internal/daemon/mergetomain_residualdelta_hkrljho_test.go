package daemon_test

// mergetomain_residualdelta_hkrljho_test.go — regression test for the
// review-loop residual-delta merge fix.
//
// Bug (hk-rljho class): a review-loop iteration can leave a TRACKED but
// UNCOMMITTED change in the run worktree — e.g. an iteration deleted a tracked
// test file and `git rm`'d it, but the daemon's commit-detection had already
// fired, so the deletion never got a commit of its own. discardDirtyChurn
// (hk-3yz2d/hk-aiw63) deliberately restores ONLY the isHarmonikChurn allowlist
// and leaves genuine work untouched (hk-i1n7j: don't silently reset real work).
// So the real iteration delta survives to the pre-merge `git rebase main`,
// which aborts with:
//
//	rebase_conflict: exit status 1
//	error: cannot rebase: You have unstaged changes.
//
// and the merge-to-main fails even though the bead's work is complete.
//
// The fix (commitResidualDelta) PRESERVES hk-i1n7j: it does NOT discard the
// residual work — it COMMITS the delta onto the run-branch (it IS the bead's
// own work — a review-loop edit that never got committed) so the rebase
// proceeds with the work intact. It stages with `git add -u` (tracked-only),
// NOT `git add -A`, so a stray untracked file is never swept into the
// run-branch and pushed to origin.
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 2.
// Bead: review-loop residual-delta merge fix (hk-rljho class).

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// newResidualRunID mints a v7 run-id for the residual-delta tests.
func newResidualRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// TestCommitResidualDelta_CommitsTrackedDeletionAndAllowsRebase reproduces the
// hk-rljho scenario: a run worktree with an UNCOMMITTED tracked deletion (the
// kind a review-loop iteration leaves behind) reaches the pre-rebase step.
// discardDirtyChurn leaves the deletion in place (it is NOT churn — hk-i1n7j),
// so a bare `git rebase main` would abort with "unstaged changes". With the
// fix, commitResidualDelta commits the deletion onto the run-branch and the
// rebase then succeeds — with the deletion preserved.
func TestCommitResidualDelta_CommitsTrackedDeletionAndAllowsRebase(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Simulate a review-loop iteration that deleted a TRACKED file but whose
	// deletion never got its own commit. code.txt is the tracked file committed
	// by dirtyLedgerSetup.
	dirtyLedgerGit(t, wtPath, "rm", "code.txt")

	// Sanity: the worktree has an uncommitted tracked deletion — exactly the
	// state that makes `git rebase main` refuse.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, "code.txt") {
		t.Fatalf("precondition: expected staged deletion of code.txt; got status:\n%s", status)
	}
	// This deletion is NOT churn, so discardDirtyChurn must leave it alone
	// (hk-i1n7j) — meaning a bare rebase would still fail.
	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, "code.txt") {
		t.Fatalf("discardDirtyChurn must NOT discard the real deletion (hk-i1n7j); got:\n%s", status)
	}

	// Apply the fix: commit the residual delta onto the run-branch.
	runID := newResidualRunID(t)
	daemon.ExportedCommitResidualDelta(context.Background(), wtPath, runID)

	// The worktree must now be clean (the delta is committed, not discarded).
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after commitResidualDelta: expected clean worktree; got:\n%s", status)
	}

	// The residual delta must be COMMITTED (preserved), not lost. The HEAD
	// commit subject carries the run-scoped message.
	subject := dirtyLedgerGit(t, wtPath, "log", "-1", "--format=%s")
	if !strings.Contains(subject, "residual iteration delta") || !strings.Contains(subject, runID.String()) {
		t.Fatalf("expected a run-scoped residual-delta commit at HEAD; got subject:\n%s", subject)
	}

	// And the rebase — the step that previously failed — must now succeed.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
		t.Fatalf("git rebase main after commitResidualDelta: %v\n%s", rebaseErr, out)
	}

	// The deletion is preserved through the rebase: code.txt must be gone.
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "code.txt"); files != "" {
		t.Errorf("code.txt deletion not preserved through rebase; ls-files still lists:\n%s", files)
	}
}

// TestCommitResidualDelta_NoOpOnCleanWorktree verifies the helper makes no
// commit when there is no residual tracked delta after churn cleanup (so it
// does not manufacture empty commits on the run-branch).
func TestCommitResidualDelta_NoOpOnCleanWorktree(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	headBefore := dirtyLedgerGit(t, wtPath, "rev-parse", "HEAD")

	daemon.ExportedCommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))

	headAfter := dirtyLedgerGit(t, wtPath, "rev-parse", "HEAD")
	if headBefore != headAfter {
		t.Errorf("no-op expected on clean worktree; HEAD moved %s -> %s", headBefore, headAfter)
	}
}

// TestCommitResidualDelta_TrackedDeltaWithStrayUntracked is the review's gap:
// a worktree with BOTH a tracked delta AND a stray UNTRACKED file. The fix uses
// `git add -u` (tracked-only), so the residual commit must contain the tracked
// delta but MUST NOT contain the untracked stray — otherwise the stray would
// FF-merge to main and push junk to origin (the `git add -A` defect the review
// reproduced). The stray must remain untracked after the commit.
func TestCommitResidualDelta_TrackedDeltaWithStrayUntracked(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Tracked delta: modify the tracked code.txt (a genuine review-loop edit).
	writeFile(t, wtPath+"/code.txt", "code\nagent work\nreview-loop iteration edit\n")
	// Stray untracked file the iteration left lying around.
	writeFile(t, wtPath+"/stray.txt", "i am an untracked stray\n")

	// Churn cleanup leaves the tracked delta (hk-i1n7j) and the untracked stray.
	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)

	daemon.ExportedCommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))

	// The tracked delta MUST be in the residual commit.
	committed := dirtyLedgerGit(t, wtPath, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(committed, "code.txt") {
		t.Errorf("tracked code.txt delta must be in the residual commit; HEAD changed files:\n%s", committed)
	}
	// The stray untracked file MUST NOT be in the residual commit (proves
	// `git add -u`, not `git add -A`).
	if strings.Contains(committed, "stray.txt") {
		t.Errorf("untracked stray.txt must NOT be in the residual commit (git add -u, not -A); HEAD changed files:\n%s", committed)
	}
	// And it must still be untracked (never added to the index).
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "stray.txt"); files != "" {
		t.Errorf("untracked stray.txt must NOT be tracked after commit; ls-files lists:\n%s", files)
	}
	// The stray remains on disk as an untracked file.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, "?? stray.txt") {
		t.Errorf("stray.txt should remain an untracked file after commit; got status:\n%s", status)
	}
}
