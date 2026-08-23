package runmerge_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runmerge"
)

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

	dirtyLedgerGit(t, wtPath, "rm", "code.txt")

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, "code.txt") {
		t.Fatalf("precondition: expected staged deletion of code.txt; got status:\n%s", status)
	}
	runmerge.DiscardDirtyChurn(context.Background(), wtPath)
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, "code.txt") {
		t.Fatalf("discardDirtyChurn must NOT discard the real deletion (hk-i1n7j); got:\n%s", status)
	}

	runID := newResidualRunID(t)
	runmerge.CommitResidualDelta(context.Background(), wtPath, runID)

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after commitResidualDelta: expected clean worktree; got:\n%s", status)
	}

	subject := dirtyLedgerGit(t, wtPath, "log", "-1", "--format=%s")
	if !strings.Contains(subject, "residual iteration delta") || !strings.Contains(subject, runID.String()) {
		t.Fatalf("expected a run-scoped residual-delta commit at HEAD; got subject:\n%s", subject)
	}

	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
		t.Fatalf("git rebase main after commitResidualDelta: %v\n%s", rebaseErr, out)
	}

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

	runmerge.CommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))

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

	writeFile(t, wtPath+"/.gitignore", "junk.log\n")
	dirtyLedgerGit(t, wtPath, "add", ".gitignore")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "add gitignore")

	writeFile(t, wtPath+"/code.txt", "code\nagent work\nreview-loop iteration edit\n")
	writeFile(t, wtPath+"/junk.log", "i am ignored runtime junk\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath)

	runmerge.CommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))

	committed := dirtyLedgerGit(t, wtPath, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(committed, "code.txt") {
		t.Errorf("tracked code.txt delta must be in the residual commit; HEAD changed files:\n%s", committed)
	}
	if strings.Contains(committed, "junk.log") {
		t.Errorf("gitignored junk.log must NOT be in the residual commit (git add -A honors .gitignore); HEAD changed files:\n%s", committed)
	}
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "junk.log"); files != "" {
		t.Errorf("gitignored junk.log must NOT be tracked after commit; ls-files lists:\n%s", files)
	}
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Errorf("after commit, only ignored junk.log remains on disk; status should be clean, got:\n%s", status)
	}
}

// TestCommitResidualDelta_UntrackedClaudeNotSwept guards against the hk-igq3
// latent hazard: a worktree that has a legitimate non-churn residual delta AND
// an untracked .claude/ file (e.g. settings.local.json, todos/, ide/ — classes
// that are NOT gitignored). With the original `git add -A`, BOTH the
// legitimate file and the .claude/ file would be staged and committed — the
// latter would then be pushed to origin (credential-adjacent leak). With the
// fix (explicit :(exclude).claude pathspec), the .claude/ file must NOT appear
// in the residual commit; the legitimate change MUST still be captured.
func TestCommitResidualDelta_UntrackedClaudeNotSwept(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	writeFile(t, wtPath+"/new_feature.go", "package work\n// authored feature\n")

	if err := os.MkdirAll(wtPath+"/.claude", 0o750); err != nil {
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	writeFile(t, wtPath+"/.claude/settings.local.json", `{"localOverride":true}`+"\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath)

	runID := newResidualRunID(t)
	runmerge.CommitResidualDelta(context.Background(), wtPath, runID)

	committed := dirtyLedgerGit(t, wtPath, "show", "--name-only", "--format=", "HEAD")

	if !strings.Contains(committed, "new_feature.go") {
		t.Errorf("hk-igq3: authored new_feature.go must be captured in the residual commit; HEAD changed files:\n%s", committed)
	}

	if strings.Contains(committed, ".claude") {
		t.Errorf("hk-igq3: .claude/ file must NOT be swept into the residual commit (credential-adjacent leak); HEAD changed files:\n%s", committed)
	}

	if files := dirtyLedgerGit(t, wtPath, "ls-files", ".claude/settings.local.json"); files != "" {
		t.Errorf("hk-igq3: .claude/settings.local.json must remain untracked; ls-files shows it as tracked:\n%s", files)
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

	writeFile(t, wtPath+"/code.txt", "code\nagent work\nRED test added\n")
	writeFile(t, wtPath+"/new_source.go", "package green\n\n// authored GREEN, never committed\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath)

	runID := newResidualRunID(t)
	runmerge.CommitResidualDelta(context.Background(), wtPath, runID)

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after commitResidualDelta: expected clean worktree (new file captured); got:\n%s", status)
	}

	committed := dirtyLedgerGit(t, wtPath, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(committed, "code.txt") {
		t.Errorf("tracked code.txt modification must be in the residual commit; HEAD changed files:\n%s", committed)
	}
	if !strings.Contains(committed, "new_source.go") {
		t.Errorf("hk-cmry defect #3: authored NEW file new_source.go was DROPPED from the residual commit (git add -u bug); it must be captured by git add -A. HEAD changed files:\n%s", committed)
	}
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "new_source.go"); files == "" {
		t.Errorf("new_source.go must be tracked after commitResidualDelta; ls-files is empty")
	}

	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
		t.Fatalf("git rebase main after commitResidualDelta: %v\n%s", rebaseErr, out)
	}
	if files := dirtyLedgerGit(t, wtPath, "ls-files", "new_source.go"); files == "" {
		t.Errorf("new_source.go not preserved through rebase; ls-files is empty")
	}
}
