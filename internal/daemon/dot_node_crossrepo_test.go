package daemon_test

// dot_node_crossrepo_test.go — a graph node asks about the repo the bead's work
// lands in, not about the harmonik project root.
//
// The subsumption check probed env.ProjectDir unconditionally, because
// driveDotWorkflow never received activeRepo. On a cross-repo bead that is the
// wrong repository, so "is this bead's work already on main?" could never be
// answered yes and a subsumed cross-repo bead hard-failed at iteration 1 and was
// re-dispatched forever.
//
// The same run merged into the RIGHT repo the whole time — activeRepo already
// reached the merge through the run bridge — so it landed work in one repo and
// asked its questions of another.
//
// Bead: hk-pq3ex.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// dotFixtureCrossRepoBody is the bead body that declares a cross-repo target.
func dotFixtureCrossRepoBody(targetRepo string) string {
	return "## Summary\n\nWork that lands in another repository.\n\n" +
		"## Branching\n\n```yaml\ntarget_repo: " + targetRepo + "\ntarget_branch: main\n```\n"
}

// dotFixtureRepoWithSubsumedWork builds a git repo whose main branch already
// carries a commit with this bead's `Refs:` trailer — the state a prior run
// leaves behind when it landed the work.
func dotFixtureRepoWithSubsumedWork(t *testing.T, bead core.BeadID) string {
	t.Helper()
	dir := t.TempDir()
	workloopFixtureGitRepo(t, dir)
	dotFixtureLandSubsumedCommit(t, dir, bead)
	return dir
}

// dotFixtureLandSubsumedCommit adds a trailered commit to dir's main branch.
func dotFixtureLandSubsumedCommit(t *testing.T, dir string, bead core.BeadID) {
	t.Helper()
	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(filepath.Join(dir, "landed.txt"), []byte("landed by a prior run\n"), 0o644); err != nil {
		t.Fatalf("dotFixtureLandSubsumedCommit: write: %v", err)
	}
	dotFixtureGit(t, dir, "add", "landed.txt")
	dotFixtureGit(t, dir, "commit", "-m", "feat: work landed by a prior run\n\nRefs: "+string(bead))
}

func dotFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dotFixtureGit: git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// TestDotNode_CrossRepoSubsumedBeadClosesInsteadOfFailing is the claim. The
// bead's work is already on the TARGET repo's main. Its implementer therefore
// finds nothing to do and makes no commit. The node must recognise that as
// subsumed, not as an iteration-1 hard failure.
func TestDotNode_CrossRepoSubsumedBeadClosesInsteadOfFailing(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-pq3ex-crossrepo-subsumed")
	targetRepo := dotFixtureRepoWithSubsumedWork(t, beadID)

	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript:   dotFixtureNoCommitHandler(t),
		BeadDescription: dotFixtureCrossRepoBody(targetRepo),
		AllowedRepos:    []string{targetRepo},
	})

	if reopened := res.Ledger.reopenedIDs(); len(reopened) > 0 {
		t.Errorf("bead %s was REOPENED although its work is already on the target repo's main (reopened=%v).\n"+
			"The node asked the harmonik project root whether this bead had landed, which is not the repo the work goes to, so subsumption could never fire.",
			beadID, reopened)
	}
	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed subsumed; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_LocalSubsumedBeadStillCloses is the control. A local bead whose
// work is on the PROJECT repo's main must still close subsumed, which is the
// behaviour the cross-repo fix must not cost.
//
// It also proves the subsumption path is reachable at all in this fixture.
// Without it, "the cross-repo bead was not reopened" could hold for reasons that
// have nothing to do with which repo was probed.
func TestDotNode_LocalSubsumedBeadStillCloses(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-pq3ex-local-subsumed")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureNoCommitHandler(t),
		BeforeRun: func(t *testing.T, projectDir string) {
			dotFixtureLandSubsumedCommit(t, projectDir, beadID)
		},
	})

	if reopened := res.Ledger.reopenedIDs(); len(reopened) > 0 {
		t.Errorf("bead %s was reopened although its work is already on the project repo's main (reopened=%v)", beadID, reopened)
	}
	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed subsumed; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_CrossRepoBeadWithNoLandedWorkStillFails is the second control, and
// it is the one that keeps the claim from being "cross-repo beads always pass".
// Same cross-repo setup, same do-nothing implementer, one thing missing: the
// target repo carries no trailered commit. The node must still hard-fail.
func TestDotNode_CrossRepoBeadWithNoLandedWorkStillFails(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-pq3ex-crossrepo-no-work")
	targetRepo := t.TempDir()
	workloopFixtureGitRepo(t, targetRepo)

	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript:   dotFixtureNoCommitHandler(t),
		BeadDescription: dotFixtureCrossRepoBody(targetRepo),
		AllowedRepos:    []string{targetRepo},
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was closed although nothing landed anywhere (closed=%v)", beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after an implementer that did nothing; events=%v", beadID, res.Bus.eventTypes())
	}
}
