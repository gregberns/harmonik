package daemon_test

// dot_node_terminal_test.go — the graph node must read what the agent REPORTED,
// not only whether the worktree HEAD moved.
//
// The graph path decided node success on HEAD advance alone. It never read the
// Stop-hook outcome and never ran the terminal classifier the single-mode tail
// runs, so an agent that committed and then signalled failure was recorded as a
// SUCCESS node and its work was merged. The same blindness covered the process
// exit code and the progress-stream watcher.
//
// Bead: hk-v4wer.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestDotNode_FailureSignalAfterACommitFailsTheNode is the headline claim: a
// graph implementer that COMMITS and then reports FAILURE_SIGNAL must not be
// merged and closed green.
//
// The commit is what makes this test bite. Without it the run fails on the
// no-commit guard for an unrelated reason and the assertion is satisfied for
// free, so the fixture's implementer commits real work and the ONLY thing
// standing between the run and a green close is the reported failure.
func TestDotNode_FailureSignalAfterACommitFailsTheNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-v4wer-failure-signal")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HookOutcome: dotFixtureFailureSignal,
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED after the agent reported FAILURE_SIGNAL (closed=%v).\n"+
			"The graph node decided success on HEAD advance alone, so an agent that committed and then declared failure had its work merged and its bead closed green.",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened; want the reported failure to reopen it. events=%v",
			beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_CleanReportAfterACommitStillClosesTheBead is the control. It
// proves the fixture can reach a green close at all — without it the test above
// is satisfied by any fixture in which nothing ever closes a bead, which is
// exactly the shape of a test that cannot fail.
//
// Same inputs, same commit, one field different: the agent reports
// WORK_COMPLETE instead of FAILURE_SIGNAL.
func TestDotNode_CleanReportAfterACommitStillClosesTheBead(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-v4wer-work-complete")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HookOutcome: dotFixtureWorkComplete,
	})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed after a committing agent reported WORK_COMPLETE (reopened=%v, events=%v).\n"+
			"The happy path must still reach a green close, or the FAILURE_SIGNAL test above proves nothing.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
}

// TestDotNode_NonZeroExitWithNoReportFailsTheNode covers CHB-020 branch 3 with a
// crashed agent: no Stop-hook outcome arrived and the process exited non-zero.
// The single-mode tail treats that as a failure; the graph treated it as a
// success whenever the agent had happened to commit before it died.
func TestDotNode_NonZeroExitWithNoReportFailsTheNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-v4wer-crash-after-commit")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureCommitThenCrashHandler(t, beadID),
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED after its agent committed and then exited non-zero with no reported outcome (closed=%v)",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after a crashed agent; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_CleanExitWithNoReportStillClosesTheBead pins the auto-close
// heuristic the single-mode tail keeps for twin-blind runs: no Stop-hook outcome
// AND exit 0 AND no watcher error is a PASS, not a failure.
//
// It is the second control, and it is what keeps the fix from over-reaching.
// Every shell-handler fixture in this suite reports nothing through the socket,
// so a classifier that failed branch 3 unconditionally would break them all.
func TestDotNode_CleanExitWithNoReportStillClosesTheBead(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-v4wer-silent-clean-exit")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed after a committing agent exited 0 with no reported outcome (reopened=%v, events=%v).\n"+
			"Exit 0 with no outcome is the documented auto-close heuristic; failing it would fail every twin-blind run.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
}

// TestDotNode_MalformedProgressStreamFailsTheNode covers the watcher leg. A
// handler that writes a line the NDJSON progress-stream reader cannot parse
// leaves the watcher in error. The single-mode tail reads watcher.Err() and
// fails the run; the graph had no equivalent read at all, so a watcher failure
// left it with no signal.
func TestDotNode_MalformedProgressStreamFailsTheNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-v4wer-watcher-error")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureCommitThenGarbageHandler(t, beadID),
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its progress-stream watcher failed (closed=%v)", beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after a watcher failure; events=%v", beadID, res.Bus.eventTypes())
	}
	if !strings.Contains(strings.Join(res.Bus.eventTypes(), ","), string(core.EventTypeRunFailed)) {
		t.Errorf("no run_failed event for the watcher failure; events=%v", res.Bus.eventTypes())
	}
}
