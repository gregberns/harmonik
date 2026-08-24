package runmerge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workspace"
)

// A forked git can die before it ever runs, and it can die right after it did
// its work. These tests hold both halves for the merge commands that are not
// safe to run again blind: the rebase, the worktree removal and the review
// trailer amend.
//
// They also hold the two commands with the widest blast radius — the push that
// publishes the target branch to origin, and the update-ref that moves the
// target branch. Both run again when their child gives no answer, so what a
// SECOND run does is the claim the whole change rests on.
//
// Bead: hk-7neu1.

const fixtureRunBranch = "task/run-1"

// mergeRebaseFixture builds a repository whose run branch is behind the target,
// plus the worktree the merge rebases in. conflict decides whether the rebase
// can finish: a conflicting rebase is how a worktree ends up holding rebase
// state, which is the case a second rebase must not walk into.
func mergeRebaseFixture(t *testing.T, conflict bool) (repoRoot, wtPath string) {
	t.Helper()
	repoRoot = t.TempDir()
	gitInDir(t, repoRoot, "init", "-q", "--initial-branch=main")
	gitInDir(t, repoRoot, "config", "user.email", "fixture@example.invalid")
	gitInDir(t, repoRoot, "config", "user.name", "Fixture")
	writeFixtureFile(t, repoRoot, "README", "base\n")
	gitInDir(t, repoRoot, "add", "README")
	gitInDir(t, repoRoot, "commit", "-q", "-m", "base")

	gitInDir(t, repoRoot, "checkout", "-q", "-b", fixtureRunBranch)
	if conflict {
		writeFixtureFile(t, repoRoot, "README", "run branch\n")
		gitInDir(t, repoRoot, "add", "README")
	} else {
		writeFixtureFile(t, repoRoot, "FEATURE", "feature\n")
		gitInDir(t, repoRoot, "add", "FEATURE")
	}
	gitInDir(t, repoRoot, "commit", "-q", "-m", "the run's work")

	gitInDir(t, repoRoot, "checkout", "-q", "main")
	if conflict {
		writeFixtureFile(t, repoRoot, "README", "main branch\n")
	} else {
		writeFixtureFile(t, repoRoot, "DIVERGE", "diverge\n")
		gitInDir(t, repoRoot, "add", "DIVERGE")
	}
	gitInDir(t, repoRoot, "add", ".")
	gitInDir(t, repoRoot, "commit", "-q", "-m", "the target moved on")

	wtPath = filepath.Join(t.TempDir(), "wt")
	gitInDir(t, repoRoot, "worktree", "add", "-q", wtPath, fixtureRunBranch)
	return repoRoot, wtPath
}

// TestPrepareInitialMerge_RebaseStoppedBeforeItStartedRunsAgain: a child that
// died before git ran gave no answer, so the rebase has to be asked again.
// Reporting a rebase conflict there charges the bead for a fault of the machine.
func TestPrepareInitialMerge_RebaseStoppedBeforeItStartedRunsAgain(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, false)
	mainTip := gitInDir(t, repoRoot, "rev-parse", "refs/heads/main")
	runTip := gitInDir(t, repoRoot, "rev-parse", "refs/heads/"+fixtureRunBranch)
	calls := stopGitOnceOn(t, "rebase", gitStubNonFlagArg, "before")

	out := prepareInitialMerge(context.Background(), wtPath, repoRoot, fixtureRunID(t),
		fixtureRunBranch, "main", &runTip, &mainTip)
	if out != nil {
		t.Fatalf("prepareInitialMerge = %+v; want the merge to survive one stopped git", *out)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the rebase ran %d times; want 2 — the stopped run gave no answer", got)
	}
	if runTip == mainTip {
		t.Error("the run tip equals the target tip; the rebase did not produce the run's commit")
	}
	if merged := gitInDir(t, repoRoot, "merge-base", mainTip, runTip); merged != mainTip {
		t.Errorf("merge-base = %s; want %s — the run branch is not rebased onto the target", merged, mainTip)
	}
}

// TestPrepareInitialMerge_RebaseStoppedMidRebaseDoesNotRunAgain: the signal
// landed on a git that was already rebasing, so the worktree holds rebase state.
// A second rebase there refuses to start and says so, and the merge would report
// that refusal as the bead's conflict.
func TestPrepareInitialMerge_RebaseStoppedMidRebaseDoesNotRunAgain(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, true)
	mainTip := gitInDir(t, repoRoot, "rev-parse", "refs/heads/main")
	runTip := gitInDir(t, repoRoot, "rev-parse", "refs/heads/"+fixtureRunBranch)
	calls := stopGitOnceOn(t, "rebase", gitStubNonFlagArg, "after")

	out := prepareInitialMerge(context.Background(), wtPath, repoRoot, fixtureRunID(t),
		fixtureRunBranch, "main", &runTip, &mainTip)
	if out == nil {
		t.Fatal("prepareInitialMerge reported success though the rebase never finished")
	}
	if !strings.HasPrefix(out.Reason, "rebase_conflict") {
		t.Errorf("reason = %q; want the rebase_conflict the abort-and-classify path gives", out.Reason)
	}
	if got := stubCalls(t, calls); got != 1 {
		t.Errorf("the rebase ran %d times; want 1 — a worktree in a rebase must not be rebased again", got)
	}
}

// TestRebaseInProgress_SeesTheStateAGitLeftBehind pins the probe itself: it has
// to find rebase state that lives under the repository, not under the worktree.
func TestRebaseInProgress_SeesTheStateAGitLeftBehind(t *testing.T) {
	_, wtPath := mergeRebaseFixture(t, true)

	inProgress, err := rebaseInProgress(context.Background(), wtPath)
	if err != nil {
		t.Fatalf("rebaseInProgress = %v; want an answer for a clean worktree", err)
	}
	if inProgress {
		t.Error("rebaseInProgress = true for a worktree with no rebase")
	}

	cmd := exec.CommandContext(context.Background(), "git", "rebase", "main")
	cmd.Dir = wtPath
	if _, rebaseErr := cmd.CombinedOutput(); rebaseErr == nil {
		t.Fatal("the fixture rebase finished; it has to conflict to leave rebase state")
	}

	inProgress, err = rebaseInProgress(context.Background(), wtPath)
	if err != nil {
		t.Fatalf("rebaseInProgress = %v; want an answer for a conflicted worktree", err)
	}
	if !inProgress {
		t.Error("rebaseInProgress = false though the worktree is in the middle of a rebase")
	}
}

// TestRemoveWorktree_StoppedAfterTheRemovalLandedReportsSuccess: the worktree is
// gone, so the reclaim worked. Reporting a failed reclaim there sends the run's
// caller after a directory that does not exist.
func TestRemoveWorktree_StoppedAfterTheRemovalLandedReportsSuccess(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, false)
	calls := stopGitOnceOn(t, "worktree", "remove", "after")

	if err := RemoveWorktree(context.Background(), repoRoot, wtPath); err != nil {
		t.Fatalf("RemoveWorktree = %v; want success for a removal that landed", err)
	}
	if got := stubCalls(t, calls); got != 1 {
		t.Errorf("the removal ran %d times; want 1 — a worktree already gone must not be removed again", got)
	}
	if _, statErr := os.Stat(wtPath); statErr == nil {
		t.Error("the worktree is still there though RemoveWorktree reported success")
	}
}

// TestRemoveWorktree_StoppedBeforeTheRemovalRunsAgain is the other half: nothing
// was removed, so the command has to run again.
func TestRemoveWorktree_StoppedBeforeTheRemovalRunsAgain(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, false)
	calls := stopGitOnceOn(t, "worktree", "remove", "before")

	if err := RemoveWorktree(context.Background(), repoRoot, wtPath); err != nil {
		t.Fatalf("RemoveWorktree = %v; want the reclaim to survive one stopped git", err)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the removal ran %d times; want 2 — the stopped run removed nothing", got)
	}
	if _, statErr := os.Stat(wtPath); statErr == nil {
		t.Error("the worktree is still there though RemoveWorktree reported success")
	}
}

// TestAddMergeWorktree_StoppedAfterTheAddLandedReportsSuccess: the worktree is
// there, so the add worked. Reporting a failure there makes the caller skip the
// deferred cleanup, and the directory is left behind for good.
func TestAddMergeWorktree_StoppedAfterTheAddLandedReportsSuccess(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, false)
	gitInDir(t, repoRoot, "worktree", "remove", "--force", wtPath)
	calls := stopGitOnceOn(t, "worktree", "add", "after")

	if err := addMergeWorktree(context.Background(), repoRoot, wtPath, fixtureRunBranch); err != nil {
		t.Fatalf("addMergeWorktree = %v; want success for an add that landed", err)
	}
	if got := stubCalls(t, calls); got != 1 {
		t.Errorf("the add ran %d times; want 1 — a path that already exists must not be added again", got)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Errorf("the worktree is not there though addMergeWorktree reported success: %v", statErr)
	}
}

// TestAddMergeWorktree_StoppedBeforeTheAddRunsAgain is the other half: nothing
// was created, so the command has to run again.
func TestAddMergeWorktree_StoppedBeforeTheAddRunsAgain(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, false)
	gitInDir(t, repoRoot, "worktree", "remove", "--force", wtPath)
	calls := stopGitOnceOn(t, "worktree", "add", "before")

	if err := addMergeWorktree(context.Background(), repoRoot, wtPath, fixtureRunBranch); err != nil {
		t.Fatalf("addMergeWorktree = %v; want the add to survive one stopped git", err)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the add ran %d times; want 2 — the stopped run created nothing", got)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Errorf("the worktree is not there though addMergeWorktree reported success: %v", statErr)
	}
}

// TestAddMergeWorktree_EmptyDirectoryIsNotALandedAdd separates the two probes
// the add could use. git makes the worktree directory first and checks the files
// out last, so a directory on its own proves nothing. git writes the `.git` file
// to say the worktree is real, and that is what the code looks for.
//
// The directory is made up front, which git accepts, so a probe that only stats
// the path sees it there and reports a landed add that never ran.
func TestAddMergeWorktree_EmptyDirectoryIsNotALandedAdd(t *testing.T) {
	repoRoot, wtPath := mergeRebaseFixture(t, false)
	gitInDir(t, repoRoot, "worktree", "remove", "--force", wtPath)
	if err := os.MkdirAll(wtPath, 0o750); err != nil {
		t.Fatalf("make the empty worktree directory: %v", err)
	}
	calls := stopGitOnceOn(t, "worktree", "add", "before")

	if err := addMergeWorktree(context.Background(), repoRoot, wtPath, fixtureRunBranch); err != nil {
		t.Fatalf("addMergeWorktree = %v; want the add to survive one stopped git", err)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the add ran %d times; want 2 — an empty directory is not a worktree", got)
	}
	if _, statErr := os.Stat(filepath.Join(wtPath, ".git")); statErr != nil {
		t.Errorf("the worktree has no .git though addMergeWorktree reported success: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(wtPath, "FEATURE")); statErr != nil {
		t.Errorf("the run branch's file is not checked out: %v", statErr)
	}
}

func approveVerdictFixture() *workspace.ReviewVerdict {
	return &workspace.ReviewVerdict{
		SchemaVersion: 1,
		Verdict:       workspace.ReviewVerdictApprove,
		Flags:         []string{},
		Notes:         "The child died after the amend landed.",
	}
}

// TestAppendReviewTrailers_AmendStoppedAfterItLandedReportsSuccess: the amend is
// commit-class, so a stopped child is decided from HEAD. HEAD moved, so the
// amend landed and the failure is not real.
func TestAppendReviewTrailers_AmendStoppedAfterItLandedReportsSuccess(t *testing.T) {
	dir := t.TempDir()
	initTestRepoDyim(t, dir)
	calls := stopGitOnceOn(t, "commit", "--amend", "after")

	if err := AppendReviewTrailersToHEAD(context.Background(), dir, approveVerdictFixture()); err != nil {
		t.Fatalf("AppendReviewTrailersToHEAD = %v; want success for an amend that landed", err)
	}
	if got := stubCalls(t, calls); got != 1 {
		t.Errorf("the amend ran %d times; want 1 — a commit already rewritten must not be rewritten again", got)
	}
	if got := gitInDir(t, dir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("the repository holds %s commits; want 1 — the amend must not add one", got)
	}
	msg := headCommitMsgDyim(t, dir)
	if got := strings.Count(msg, "Reviewed-By: "+reviewedByTrailerValue); got != 1 {
		t.Errorf("the message carries %d Reviewed-By trailers; want exactly 1:\n%s", got, msg)
	}
}

// TestAppendReviewTrailers_AmendStoppedBeforeItRanRunsAgain is the other half:
// HEAD did not move, so nothing was rewritten and the amend runs again.
func TestAppendReviewTrailers_AmendStoppedBeforeItRanRunsAgain(t *testing.T) {
	dir := t.TempDir()
	initTestRepoDyim(t, dir)
	calls := stopGitOnceOn(t, "commit", "--amend", "before")

	if err := AppendReviewTrailersToHEAD(context.Background(), dir, approveVerdictFixture()); err != nil {
		t.Fatalf("AppendReviewTrailersToHEAD = %v; want the amend to survive one stopped git", err)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the amend ran %d times; want 2 — the stopped run rewrote nothing", got)
	}
	msg := headCommitMsgDyim(t, dir)
	if got := strings.Count(msg, "Reviewed-By: "+reviewedByTrailerValue); got != 1 {
		t.Errorf("the message carries %d Reviewed-By trailers; want exactly 1:\n%s", got, msg)
	}
}

// pushOriginFixture builds a repository with a real bare remote. The push in
// these tests reaches a repository a later git reads back, so the assertion
// measures what origin holds rather than what a stub agreed to say.
func pushOriginFixture(t *testing.T) (repoRoot, originDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	gitInDir(t, repoRoot, "init", "-q", "--initial-branch=main")
	gitInDir(t, repoRoot, "config", "user.email", "fixture@example.invalid")
	gitInDir(t, repoRoot, "config", "user.name", "Fixture")
	writeFixtureFile(t, repoRoot, "README", "base\n")
	gitInDir(t, repoRoot, "add", "README")
	gitInDir(t, repoRoot, "commit", "-q", "-m", "base")

	originDir = t.TempDir()
	gitInDir(t, originDir, "init", "-q", "--bare", "--initial-branch=main")
	gitInDir(t, repoRoot, "remote", "add", "origin", originDir)
	gitInDir(t, repoRoot, "push", "-q", "origin", "main")
	return repoRoot, originDir
}

// pushFixtureCommit puts one more commit on main and returns the new tip. It is
// the commit the push has to publish.
func pushFixtureCommit(t *testing.T, repoRoot, name string) string {
	t.Helper()
	writeFixtureFile(t, repoRoot, name, name+"\n")
	gitInDir(t, repoRoot, "add", name)
	gitInDir(t, repoRoot, "commit", "-q", "-m", "publish "+name)
	return gitInDir(t, repoRoot, "rev-parse", "refs/heads/main")
}

// TestGitPushOrigin_StoppedBeforeItRanPublishesOnTheSecondAttempt: the child
// died with git never having run, so origin was never told anything. A merge
// that reports push_failed here strands work that is already on the local
// target branch.
func TestGitPushOrigin_StoppedBeforeItRanPublishesOnTheSecondAttempt(t *testing.T) {
	repoRoot, originDir := pushOriginFixture(t)
	want := pushFixtureCommit(t, repoRoot, "FEATURE")
	calls := stopGitOnceOn(t, "push", "origin", "before")

	out, err := gitPushOrigin(context.Background(), repoRoot, "main")
	if err != nil {
		t.Fatalf("gitPushOrigin = %v; want the push to survive one stopped git\n%s", err, out)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the push ran %d times; want 2 — the stopped run published nothing", got)
	}
	if got := gitInDir(t, originDir, "rev-parse", "refs/heads/main"); got != want {
		t.Errorf("origin main = %s; want %s — the commit never reached origin", got, want)
	}
}

// TestGitPushOrigin_StoppedAfterItLandedIsSafeToRunAgain is the claim the change
// rests on. The push landed and the signal arrived after it, so the second run
// pushes the same refspec to a remote that already holds it. git answers that
// everything is up to date and exits zero, so the merge is not failed for work
// that succeeded — and origin keeps exactly the commits it already had.
func TestGitPushOrigin_StoppedAfterItLandedIsSafeToRunAgain(t *testing.T) {
	repoRoot, originDir := pushOriginFixture(t)
	want := pushFixtureCommit(t, repoRoot, "FEATURE")
	calls := stopGitOnceOn(t, "push", "origin", "after")

	out, err := gitPushOrigin(context.Background(), repoRoot, "main")
	if err != nil {
		t.Fatalf("gitPushOrigin = %v; want a landed push to be recognised, not to fail the merge\n%s", err, out)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("the push ran %d times; want 2 — a child stopped by a signal gave no answer of its own", got)
	}
	if got := gitInDir(t, originDir, "rev-parse", "refs/heads/main"); got != want {
		t.Errorf("origin main = %s; want %s", got, want)
	}
	if got := gitInDir(t, originDir, "rev-list", "--count", "refs/heads/main"); got != "2" {
		t.Errorf("origin main holds %s commits; want 2 — the second push must not add one", got)
	}
}

// advanceRefFixture builds a target branch and a run branch one commit ahead of
// it, which is the state commitAdvanceRef fast-forwards.
func advanceRefFixture(t *testing.T) (repoRoot, priorTip, runTip string) {
	t.Helper()
	repoRoot = t.TempDir()
	gitInDir(t, repoRoot, "init", "-q", "--initial-branch=main")
	gitInDir(t, repoRoot, "config", "user.email", "fixture@example.invalid")
	gitInDir(t, repoRoot, "config", "user.name", "Fixture")
	writeFixtureFile(t, repoRoot, "README", "base\n")
	gitInDir(t, repoRoot, "add", "README")
	gitInDir(t, repoRoot, "commit", "-q", "-m", "base")
	priorTip = gitInDir(t, repoRoot, "rev-parse", "refs/heads/main")

	gitInDir(t, repoRoot, "checkout", "-q", "-b", fixtureRunBranch)
	writeFixtureFile(t, repoRoot, "FEATURE", "feature\n")
	gitInDir(t, repoRoot, "add", "FEATURE")
	gitInDir(t, repoRoot, "commit", "-q", "-m", "the run's work")
	runTip = gitInDir(t, repoRoot, "rev-parse", "refs/heads/"+fixtureRunBranch)

	gitInDir(t, repoRoot, "checkout", "-q", "main")
	return repoRoot, priorTip, runTip
}

// TestCommitAdvanceRef_StoppedAfterTheRefMovedSetsTheSameValueTwice: update-ref
// sets an exact value, so the second run writes what the first one wrote. That
// is why this command is allowed to run again at all, and it is what makes the
// stopped child recoverable instead of a failed merge.
func TestCommitAdvanceRef_StoppedAfterTheRefMovedSetsTheSameValueTwice(t *testing.T) {
	repoRoot, priorTip, runTip := advanceRefFixture(t)
	calls := stopGitOnceOn(t, "update-ref", gitStubNonFlagArg, "after")

	adv := commitAdvanceRef(context.Background(), repoRoot, runTip, "main", 1, 3)

	if adv.done != nil {
		t.Fatalf("commitAdvanceRef = %+v; want the ref move to survive one stopped git", *adv.done)
	}
	if !adv.advanced {
		t.Error("advanced = false though the ref was moved")
	}
	if adv.priorMainTip != priorTip {
		t.Errorf("priorMainTip = %s; want %s — the rollback would put main back to the wrong commit", adv.priorMainTip, priorTip)
	}
	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("update-ref ran %d times; want 2 — a child stopped by a signal gave no answer of its own", got)
	}
	if got := gitInDir(t, repoRoot, "rev-parse", "refs/heads/main"); got != runTip {
		t.Errorf("main = %s; want %s — the second update-ref must set the value the first one set", got, runTip)
	}
}

// TestGitUpdateRefBestEffort_StoppedAfterTheRollbackLandedSetsTheSameValueTwice
// is the same property on the rollback site: the branch goes back to the commit
// origin holds, and a second run puts it at the same place.
func TestGitUpdateRefBestEffort_StoppedAfterTheRollbackLandedSetsTheSameValueTwice(t *testing.T) {
	repoRoot, priorTip, runTip := advanceRefFixture(t)
	gitInDir(t, repoRoot, "update-ref", "refs/heads/main", runTip)
	calls := stopGitOnceOn(t, "update-ref", gitStubNonFlagArg, "after")

	gitUpdateRefBestEffort(context.Background(), repoRoot, "main", priorTip)

	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("update-ref ran %d times; want 2 — a child stopped by a signal gave no answer of its own", got)
	}
	if got := gitInDir(t, repoRoot, "rev-parse", "refs/heads/main"); got != priorTip {
		t.Errorf("main = %s; want %s", got, priorTip)
	}
}

// TestGitUpdateRefBestEffort_StoppedBeforeItRanStillRollsBack is the other half,
// and the one with the consequence: a rollback that never happens leaves the
// target branch on a commit origin refused to take.
func TestGitUpdateRefBestEffort_StoppedBeforeItRanStillRollsBack(t *testing.T) {
	repoRoot, priorTip, runTip := advanceRefFixture(t)
	gitInDir(t, repoRoot, "update-ref", "refs/heads/main", runTip)
	calls := stopGitOnceOn(t, "update-ref", gitStubNonFlagArg, "before")

	gitUpdateRefBestEffort(context.Background(), repoRoot, "main", priorTip)

	if got := stubCalls(t, calls); got != 2 {
		t.Errorf("update-ref ran %d times; want 2 — the stopped run moved nothing", got)
	}
	if got := gitInDir(t, repoRoot, "rev-parse", "refs/heads/main"); got != priorTip {
		t.Errorf("main = %s; want %s — main is left on a commit origin never took", got, priorTip)
	}
}
