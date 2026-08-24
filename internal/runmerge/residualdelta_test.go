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

// commitResidualDeltaOK runs the residual-delta commit on a path that must
// succeed, and stops the test when it does not. The commit reports a failure
// now (hk-33u5r) because the step after it deletes untracked files, so a test
// that dropped the error would go green on a worktree whose work was never
// saved. The failing paths have their own tests below.
func commitResidualDeltaOK(t *testing.T, wtPath string, runID core.RunID) {
	t.Helper()
	if err := runmerge.CommitResidualDelta(context.Background(), wtPath, runID); err != nil {
		t.Fatalf("CommitResidualDelta(%s): %v", wtPath, err)
	}
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
	commitResidualDeltaOK(t, wtPath, runID)

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

	commitResidualDeltaOK(t, wtPath, newResidualRunID(t))

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

	commitResidualDeltaOK(t, wtPath, newResidualRunID(t))

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
	commitResidualDeltaOK(t, wtPath, runID)

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
	commitResidualDeltaOK(t, wtPath, runID)

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

// residualGit runs git under the CALLER's context. The merge hands its own
// context to the queue callback, so a fixture that reached for t.Context()
// there would run git outside the merge it is meant to be inside.
func residualGit(ctx context.Context, t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// lockGitIndex makes the next `git add` in wtPath fail the way a real one does:
// index.lock already exists, so git refuses to write the index. It is the
// cheapest honest way to fail the staging step — no stub, no fake git, the real
// program reporting a real error.
func lockGitIndex(ctx context.Context, t *testing.T, wtPath string) {
	t.Helper()
	gitDir := residualGit(ctx, t, wtPath, "rev-parse", "--absolute-git-dir")
	writeFile(t, gitDir+"/index.lock", "")
}

// TestCommitResidualDelta_StagingFailureReportsAnError is the hk-33u5r
// regression at the level of the helper itself.
//
// The step exists to save authored files that never got their own commit, and
// the step that runs next is `git clean -fd`. When staging fails, the files are
// not saved — so the helper HAS to say so, or the caller cleans away exactly
// what this step failed to keep. Before the fix it wrote one line to stderr and
// returned nothing, which the caller could not read.
//
// RED→GREEN: this test does not compile against the old void signature, and
// fails against any version that swallows the staging failure.
func TestCommitResidualDelta_StagingFailureReportsAnError(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	writeFile(t, wtPath+"/new_source.go", "package green\n\n// authored, never committed\n")

	lockGitIndex(t.Context(), t, wtPath)

	err := runmerge.CommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))
	if err == nil {
		t.Fatalf("staging failed and the authored file is unsaved; CommitResidualDelta returned no error")
	}
	if !strings.Contains(err.Error(), "git add") {
		t.Errorf("the error must name the step that failed; got: %v", err)
	}

	if _, statErr := os.Stat(wtPath + "/new_source.go"); statErr != nil {
		t.Errorf("the authored file must still be on disk after a failed save: %v", statErr)
	}
}

// TestCommitResidualDelta_UnreadableStatusReportsAnError covers the half that
// is easiest to get wrong. A failed `git status` reads here exactly like a
// clean worktree: both produce "nothing to save". Treat them the same and a
// worktree that could not say what it holds is cleaned as if it held nothing.
func TestCommitResidualDelta_UnreadableStatusReportsAnError(t *testing.T) {
	t.Parallel()

	notARepo := t.TempDir()
	writeFile(t, notARepo+"/new_source.go", "package green\n")

	err := runmerge.CommitResidualDelta(context.Background(), notARepo, newResidualRunID(t))
	if err == nil {
		t.Fatalf("a worktree that cannot report its state must not be read as an empty one")
	}
	if !strings.Contains(err.Error(), "git status") {
		t.Errorf("the error must name the step that failed; got: %v", err)
	}
}

// TestCommitResidualDelta_FailedCommitReportsAnError closes the last of the
// three exits. Staging can succeed and the commit still fail — a hook that
// refuses it, a full disk, a broken object store. The staged work survives the
// clean in that case, because a staged file is not untracked, but the merge
// must still stop: the rebase that follows would report a confusing
// "unstaged changes" for a cause that has a name.
func TestCommitResidualDelta_FailedCommitReportsAnError(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	hooks := t.TempDir()
	writeFile(t, hooks+"/pre-commit", "#!/bin/sh\nexit 1\n")
	//nolint:gosec // G302: a git hook that is not executable never runs, so this test would measure nothing.
	if err := os.Chmod(hooks+"/pre-commit", 0o700); err != nil {
		t.Fatalf("chmod pre-commit: %v", err)
	}
	dirtyLedgerGit(t, wtPath, "config", "core.hooksPath", hooks)

	writeFile(t, wtPath+"/new_source.go", "package green\n\n// authored, never committed\n")

	err := runmerge.CommitResidualDelta(context.Background(), wtPath, newResidualRunID(t))
	if err == nil {
		t.Fatalf("the commit was refused and nothing was saved; CommitResidualDelta returned no error")
	}
	if !strings.Contains(err.Error(), "git commit") {
		t.Errorf("the error must name the step that failed; got: %v", err)
	}

	if _, statErr := os.Stat(wtPath + "/new_source.go"); statErr != nil {
		t.Errorf("the authored file must still be on disk after a failed commit: %v", statErr)
	}
}
