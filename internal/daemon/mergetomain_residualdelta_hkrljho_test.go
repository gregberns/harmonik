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
// own work — a review-loop edit or a newly authored source file that never got
// committed) so the rebase proceeds with the work intact.
//
// hk-cmry defect #3: the staging was switched from `git add -u` (tracked-only)
// to `git add -A` because -u SILENTLY DROPPED genuinely new (untracked) source
// files the implementer authored — that is how the daemon dropped a reviewed
// GREEN and broke main fleet-wide. `git add -A` honors .gitignore, so daemon/
// runtime/build junk stays excluded while authored new files are captured.
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 2.
// Bead: review-loop residual-delta merge fix (hk-rljho class); untracked-capture
// fix (hk-cmry defect #3).

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

// TestCommitResidualDelta_GitignoredUntrackedNotSwept guards the original
// design intent (hk-rljho's `git add -u` rationale) under the hk-cmry defect-#3
// fix that switched to `git add -A`: a worktree with a tracked delta AND a
// GITIGNORED untracked file. The residual commit must contain the tracked delta
// but MUST NOT contain the gitignored file — `git add -A` honors .gitignore, so
// daemon/runtime junk (.harmonik/, build outputs, etc.) is never swept to main
// and pushed to origin. The gitignored file must remain on disk, ignored.
func TestCommitResidualDelta_GitignoredUntrackedNotSwept(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Commit a .gitignore on the run branch so the worktree treats junk.log as
	// ignored (mirrors the real repo .gitignore covering daemon/runtime junk).
	writeFile(t, wtPath+"/.gitignore", "junk.log\n")
	dirtyLedgerGit(t, wtPath, "add", ".gitignore")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "add gitignore")

	// Tracked delta: modify the tracked code.txt (a genuine review-loop edit).
	writeFile(t, wtPath+"/code.txt", "code\nagent work\nreview-loop iteration edit\n")
	// A gitignored junk file the daemon/build left lying around.
	writeFile(t, wtPath+"/junk.log", "i am ignored runtime junk\n")

	// Churn cleanup leaves the tracked delta (hk-i1n7j); junk.log is ignored.
	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)

	daemon.ExportedCommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))

	// The tracked delta MUST be in the residual commit.
	committed := dirtyLedgerGit(t, wtPath, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(committed, "code.txt") {
		t.Errorf("tracked code.txt delta must be in the residual commit; HEAD changed files:\n%s", committed)
	}
	// The gitignored junk file MUST NOT be in the residual commit (proves
	// `git add -A` honors .gitignore — no junk pushed to origin).
	if strings.Contains(committed, "junk.log") {
		t.Errorf("gitignored junk.log must NOT be in the residual commit (git add -A honors .gitignore); HEAD changed files:\n%s", committed)
	}
	// And it must never be tracked.
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "junk.log"); files != "" {
		t.Errorf("gitignored junk.log must NOT be tracked after commit; ls-files lists:\n%s", files)
	}
	// The junk remains on disk but git still ignores it (status is clean).
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Errorf("after commit, only ignored junk.log remains on disk; status should be clean, got:\n%s", status)
	}
}

// TestCommitResidualDelta_CapturesUntrackedNewFile is the hk-cmry defect-#3
// regression: a run worktree where the implementer / a review-loop iteration
// authored a genuinely NEW (untracked) source file ALONGSIDE a tracked
// modification, and neither got committed. This mirrors hk-8prq's GREEN, which
// added internal/keeper/sessionid.go and a new hook script as brand-new files.
//
// Under the old `git add -u` (tracked-only), the residual commit silently
// DROPPED the new file — carrying only the tracked change (often the RED test)
// — and the new-file GREEN was lost when the worktree was cleaned, breaking
// main fleet-wide. The fix stages with `git add -A`, so BOTH the tracked
// modification AND the new file are committed and survive the rebase.
//
// RED→GREEN: this test FAILS on `git add -u` (new_source.go absent from the
// commit) and PASSES after the switch to `git add -A`.
func TestCommitResidualDelta_CapturesUntrackedNewFile(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Tracked modification: the review/iteration edited an existing tracked file
	// (e.g. the RED test that was added first).
	writeFile(t, wtPath+"/code.txt", "code\nagent work\nRED test added\n")
	// NEW authored source file the implementer added but never committed — the
	// GREEN. This is the file `git add -u` silently drops.
	writeFile(t, wtPath+"/new_source.go", "package green\n\n// authored GREEN, never committed\n")

	// Churn cleanup leaves BOTH (neither is isHarmonikChurn; new_source.go is a
	// genuine untracked authored file, not gitignored).
	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)

	runID := newResidualRunID(t)
	daemon.ExportedCommitResidualDelta(context.Background(), wtPath, runID)

	// The worktree must be clean (both the tracked delta and the new file are
	// committed, not left dangling).
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after commitResidualDelta: expected clean worktree (new file captured); got:\n%s", status)
	}

	committed := dirtyLedgerGit(t, wtPath, "show", "--name-only", "--format=", "HEAD")
	// The tracked modification must be in the residual commit.
	if !strings.Contains(committed, "code.txt") {
		t.Errorf("tracked code.txt modification must be in the residual commit; HEAD changed files:\n%s", committed)
	}
	// The NEW authored file MUST be in the residual commit — this is the defect
	// #3 assertion that FAILS on `git add -u` and PASSES on `git add -A`.
	if !strings.Contains(committed, "new_source.go") {
		t.Errorf("hk-cmry defect #3: authored NEW file new_source.go was DROPPED from the residual commit (git add -u bug); it must be captured by git add -A. HEAD changed files:\n%s", committed)
	}
	// The new file must now be tracked.
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "new_source.go"); files == "" {
		t.Errorf("new_source.go must be tracked after commitResidualDelta; ls-files is empty")
	}

	// And the rebase — the step the residual commit unblocks — must succeed with
	// the new file preserved through it.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
		t.Fatalf("git rebase main after commitResidualDelta: %v\n%s", rebaseErr, out)
	}
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "new_source.go"); files == "" {
		t.Errorf("new_source.go not preserved through rebase; ls-files is empty")
	}
}
