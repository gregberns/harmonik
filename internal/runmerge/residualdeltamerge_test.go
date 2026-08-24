package runmerge_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runmerge"
	"github.com/gregberns/harmonik/internal/workspace"
)

// residualMergeSetup builds a project, a bare origin and a run branch, then
// leaves the run worktree in place at the path the merge looks for.
//
// Leaving it in place is the point. RunBranchToTarget adds the worktree itself
// when it is absent, and then removes it on the way out — which would delete
// the authored file this test is about to ask after and prove nothing.
func residualMergeSetup(t *testing.T) (projectDir, wtPath string, runID core.RunID) {
	t.Helper()

	projectDir = t.TempDir()
	dirtyLedgerGit(t, projectDir, "init", "--initial-branch=main")
	dirtyLedgerGit(t, projectDir, "config", "user.email", "daemon@harmonik.local")
	dirtyLedgerGit(t, projectDir, "config", "user.name", "Harmonik Test")
	writeFile(t, filepath.Join(projectDir, "README"), "init\n")
	dirtyLedgerGit(t, projectDir, "add", ".")
	dirtyLedgerGit(t, projectDir, "commit", "-m", "init")

	originDir := t.TempDir()
	dirtyLedgerGit(t, originDir, "init", "--bare", "--initial-branch=main")
	dirtyLedgerGit(t, projectDir, "remote", "add", "origin", originDir)
	dirtyLedgerGit(t, projectDir, "push", "origin", "main")

	runID = core.RunID(uuid.MustParse("0190a000-0000-7000-8000-0000000033a5"))

	runBranch := workspace.TaskBranchName(runID.String())
	dirtyLedgerGit(t, projectDir, "branch", runBranch, "main")

	wtPath = workspace.WorktreePath(projectDir, runID.String(), workspace.NoWorktreeRootOverride())
	dirtyLedgerGit(t, projectDir, "worktree", "add", wtPath, runBranch)

	writeFile(t, filepath.Join(wtPath, "work.txt"), "agent work\n")
	dirtyLedgerGit(t, wtPath, "add", "work.txt")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "agent commit")

	return projectDir, wtPath, runID
}

// TestRunBranchToTarget_FailedResidualSaveDoesNotCleanAwayTheWork is the
// hk-33u5r regression, and it is a data-loss one.
//
// The merge saves an implementer's uncommitted work, and then it runs
// `git clean -fd`. The save could not report a failure, so a save that failed
// was followed immediately by deleting exactly the files it failed to save. The
// rebase succeeded, the merge succeeded, the run was recorded as landed, and
// the authored file was gone. Nothing was red anywhere.
//
// The failure injected here is a real one: an index.lock left behind by an
// earlier git, which is the same thing a crashed or killed git leaves. Nothing
// is stubbed — the real git refuses the staging step for the real reason.
//
// RED→GREEN: with the save unable to report its failure, authored.go is deleted
// and the merge does not stop. With the error return and the caller acting on
// it, the merge stops with a reason that names the cause and the file survives
// for a person to salvage.
func TestRunBranchToTarget_FailedResidualSaveDoesNotCleanAwayTheWork(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, runID := residualMergeSetup(t)

	authored := filepath.Join(wtPath, "authored.go")
	writeFile(t, authored, "package x\n\n// authored work that never got its own commit\n")

	// An index.lock the previous git left behind. `git status` still answers, so
	// the save sees the file and tries to keep it; `git add` is what refuses.
	lockGitIndex(t.Context(), t, wtPath)

	out := runmerge.RunBranchToTarget(
		context.Background(),
		nil, // nil Submit → runmerge.InlineSubmit
		projectDir,
		runID,
		discardingEmitter{},
		core.BeadID("hk-33u5r"),
		"",     // headSHA — unset, so the no-change guard does not fire
		"main", // targetBranch
		nil,    // protectBranches
		"",     // brPath — disables the bead-ledger sync step
	)

	if _, statErr := os.Stat(authored); statErr != nil {
		t.Errorf("the authored file was deleted by the merge after its save failed —"+
			" this is the data loss hk-33u5r is about: %v", statErr)
	}

	if out.Success {
		t.Fatalf("the merge reported success over work it did not save")
	}
	if !strings.Contains(out.Reason, "residual_delta_commit_failed") {
		t.Errorf("the failure reason must name the save that failed, so the work can be found;"+
			" got: %q", out.Reason)
	}
	// Assert on wording only the outer reason carries. The inner git error names
	// the worktree by itself, so an assertion on the path alone stays green when
	// the reason around it is deleted — it measures the union of the two, not the
	// thing the merge is adding.
	if !strings.Contains(out.Reason, "the bead reopens for another run") {
		t.Errorf("the failure reason must say what happens next, or a person reading it"+
			" cannot tell a stopped merge from a lost one; got: %q", out.Reason)
	}
	if strings.Contains(out.Reason, "salvage") {
		t.Errorf("the reason must not promise a salvage: the run's teardown removes the worktree"+
			" for most harnesses before anyone reads this; got: %q", out.Reason)
	}
}

// TestRunBranchToTarget_FailedResidualSaveOnRetryStopsTheMerge covers the
// second place the save runs: the rebase the merge does when the target branch
// moved under it and the first push has to be redone.
//
// What this measures is attribution, and the difference from the test above is
// worth stating. Nothing deletes files on this path, and a save that fails here
// leaves the worktree dirty, so the rebase right after it fails too. The merge
// already stopped. What it did not do was say why: the reason named a rebase
// conflict and printed git's lock complaint, and the unsaved authored work was
// not mentioned at all. A person reading that reason looks for a conflict and
// does not go looking for work to salvage.
//
// The target branch is moved from inside the merge's own queue callback, which
// is the seam a concurrent merge would go through, so the retry is reached the
// way a real one is.
func TestRunBranchToTarget_FailedResidualSaveOnRetryStopsTheMerge(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, runID := residualMergeSetup(t)

	authored := filepath.Join(wtPath, "authored.go")

	firstCall := true
	submit := func(ctx context.Context, label string, critical func(context.Context) error) error {
		if label == "commit-merge" && firstCall {
			firstCall = false
			// Another merge lands on the target while this one holds the queue.
			writeFile(t, filepath.Join(projectDir, "OTHER"), "another merge landed\n")
			residualGit(ctx, t, projectDir, "add", "OTHER")
			residualGit(ctx, t, projectDir, "commit", "-m", "another merge")
			// And the run worktree now holds authored work that cannot be saved.
			writeFile(t, authored, "package x\n\n// authored work that never got its own commit\n")
			lockGitIndex(ctx, t, wtPath)
		}
		return critical(ctx)
	}

	out := runmerge.RunBranchToTarget(
		context.Background(), submit, projectDir, runID, discardingEmitter{},
		core.BeadID("hk-33u5r"), "", "main", nil, "")

	if out.Success {
		t.Fatalf("the merge landed and reported success while authored work sat unsaved in %s", wtPath)
	}
	if !strings.Contains(out.Reason, "residual_delta_commit_failed") {
		t.Errorf("the failure reason must name the save that failed; got: %q", out.Reason)
	}
	// This reason makes the same promises as the one on the other path, so it
	// gets the same pins. Without them the whole message can be deleted down to
	// the bare tag and every test here stays green.
	if !strings.Contains(out.Reason, "the bead reopens for another run") {
		t.Errorf("the failure reason must say what happens next, or a person reading it"+
			" cannot tell a stopped merge from a lost one; got: %q", out.Reason)
	}
	if strings.Contains(out.Reason, "salvage") {
		t.Errorf("the reason must not promise a salvage: the run's teardown removes the worktree"+
			" for most harnesses before anyone reads this; got: %q", out.Reason)
	}
	if _, statErr := os.Stat(authored); statErr != nil {
		t.Errorf("the authored file must survive the merge on this path — nothing here deletes,"+
			" and the rebase is the backstop: %v", statErr)
	}
}
