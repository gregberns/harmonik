package daemon_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

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
