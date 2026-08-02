package daemon_test

// single_nocommit_headprobe_test.go — a legacy single input selects no-review
// DOT, whose implementer node must fail closed when it cannot read HEAD.
//
// The guard asked two questions in one condition: "did the probe succeed" AND
// "should the run reopen". A probe that errored answered the first question
// `false`, so the whole condition was false and the guard was skipped. A run
// that exited 0 and produced nothing then fell through to the clean-exit
// classification, merged as no-change, and CLOSED the bead green.
//
// specs/execution-model.md EM-058 component C says a worktree whose HEAD cannot
// be resolved is a daemon-side error. The input below is deliberately legacy
// `single`; run planning maps it to the registered no-review DOT graph.
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
// run whose probe crosses SSH, so the worktree is broken directly instead.
func singleFixtureBreakWorktreeHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "single-fixture-broken-worktree.sh",
		"printf 'gitdir: /nonexistent/harmonik-hk-fmere\\n' > .git\nexit 0\n")
}

// TestLegacySingleInput_NoReviewDOTUnreadableHeadDoesNotClose is the claim.
// The legacy input selects no-review DOT. Its implementer commits nothing and
// leaves a worktree whose HEAD cannot be read. The run must not close the bead.
func TestLegacySingleInput_NoReviewDOTUnreadableHeadDoesNotClose(t *testing.T) {
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
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "resolve HEAD after node") {
		t.Errorf("run_failed summary = %q; want it to name the unreadable worktree HEAD", summary)
	}
}

// TestLegacySingleInput_NoReviewDOTNoCommitReopens is the control that makes
// the test above mean something: the same do-nothing implementer, with its
// worktree left intact, must reopen through the no-review DOT node.
func TestLegacySingleInput_NoReviewDOTNoCommitReopens(t *testing.T) {
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
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "exited without advancing HEAD") {
		t.Errorf("run_failed summary = %q; want the no-review DOT no-commit reason", summary)
	}
}

// TestLegacySingleInput_NoReviewDOTHealthyRunCloses proves the compatibility
// input reaches a green no-review DOT close. Without it, both tests above are
// satisfied for free.
func TestLegacySingleInput_NoReviewDOTHealthyRunCloses(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-fmere-healthy-single")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode: core.WorkflowModeSingle,
	})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed by a committing no-review DOT implementer (reopened=%v, events=%v).\n"+
			"Fix this before believing the two tests above — otherwise they pass for free.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
}
