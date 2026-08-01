package daemon_test

// single_nocommit_headprobe_test.go — the single-mode no-commit guard must not
// fail open when it cannot read the worktree HEAD.
//
// The guard asked two questions in one condition: "did the probe succeed" AND
// "should the run reopen". A probe that errored answered the first question
// `false`, so the whole condition was false and the guard was skipped. A run
// that exited 0 and produced nothing then fell through to the clean-exit
// classification, merged as no-change, and CLOSED the bead green.
//
// specs/execution-model.md EM-058 component C already said what must happen
// instead: "A worktree whose HEAD cannot be resolved at all is a daemon-side
// error in BOTH modes". The graph node has always refused at this probe. Single
// mode passed.
//
// Bead: hk-fmere.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// singleFixtureBreakWorktreeHandler writes an implementer that produces NO work
// and then makes its own worktree unreadable to git, so the post-exit HEAD probe
// errors while everything outside the worktree stays healthy.
//
// A `.git` file that points at a directory which does not exist is what a git
// worktree looks like after its administrative directory is gone. git refuses
// with "not a git repository" rather than walking up to the parent repo, which
// is what makes this a probe ERROR and not a wrong answer.
//
// The real-world shape is a transient probe failure — most plausibly a remote
// run whose probe crosses SSH — but single mode routes its local probes through
// no injectable runner, so the worktree is broken directly instead.
func singleFixtureBreakWorktreeHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "single-fixture-broken-worktree.sh",
		"printf 'gitdir: /nonexistent/harmonik-hk-fmere\\n' > .git\nexit 0\n")
}

// TestSingleMode_UnreadableHeadDoesNotCloseARunThatDidNoWork is the claim. The
// implementer commits nothing and leaves a worktree whose HEAD cannot be read.
// The run MUST NOT close the bead.
func TestSingleMode_UnreadableHeadDoesNotCloseARunThatDidNoWork(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-fmere-unreadable-head")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerScript: singleFixtureBreakWorktreeHandler(t),
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its implementer produced no commit (closed=%v).\n"+
			"The no-commit guard is conditioned on its own HEAD probe succeeding, so a probe error skips the guard entirely and an exit-0 run auto-closes as success.",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s reached no reopen; events=%v", beadID, res.Bus.eventTypes())
	}
	// The run must fail for THIS reason. A broken worktree also breaks the merge
	// and the scenario gate, so a later regression could reopen the bead for the
	// wrong reason and satisfy the two assertions above for free.
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "worktree_head_unreadable") {
		t.Errorf("run_failed summary = %q; want it to name the unreadable worktree HEAD", summary)
	}
}

// TestSingleMode_ReadableHeadFailsARunThatDidNoWork is the control that makes
// the test above mean something: the same do-nothing implementer, with its
// worktree left intact, must already reopen. Without it, "the bead was not
// closed" could be true because nothing in single mode ever closes a bead.
func TestSingleMode_ReadableHeadFailsARunThatDidNoWork(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-fmere-readable-head")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerScript: dotFixtureNoCommitHandler(t),
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was closed although its implementer produced no commit (closed=%v)", beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s reached no reopen with a healthy worktree; events=%v", beadID, res.Bus.eventTypes())
	}
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "no_commit_during_implementer") {
		t.Errorf("run_failed summary = %q; want the no-commit guard's own reason", summary)
	}
}

// TestSingleMode_HealthyRunCloses proves single mode can reach a green close in
// this fixture at all. Without it, both tests above are satisfied for free.
func TestSingleMode_HealthyRunCloses(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-fmere-healthy-single")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode: core.WorkflowModeSingle,
	})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed by a committing single-mode implementer (reopened=%v, events=%v).\n"+
			"Fix this before believing the two tests above — while a single-mode run can never close, they pass for free.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
}
