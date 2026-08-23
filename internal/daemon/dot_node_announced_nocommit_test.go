package daemon_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func dotFixtureAgentEndNoCommitHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-agent-end-no-commit.sh",
		`printf '{"type":"session","version":3,"id":"pi-fixture-session"}\n'`+"\n"+
			`printf '{"type":"agent_end","messages":[]}\n'`+"\n"+
			"while :; do sleep 1; done\n")
}

func dotFixtureSilentNoCommitHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-silent-no-commit.sh",
		`printf '{"type":"session","version":3,"id":"pi-fixture-session"}\n'`+"\n"+
			"exit 0\n")
}

// TestDotNode_AnnouncedEndWithNoCommitIsReportedAsTheAgentEndingItsOwnTurn is
// the claim.
func TestDotNode_AnnouncedEndWithNoCommitIsReportedAsTheAgentEndingItsOwnTurn(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-c6v0m-announced-no-commit")

	opts := dotFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureAgentEndNoCommitHandler(t))
	opts.HookOutcome = ""
	opts.WaitForRunTerminal = true

	res := runDotFixtureBead(t, beadID, opts)

	summary := dotFixtureRunFailedSummary(res)
	if !strings.Contains(summary, "announced the end of its turn") {
		t.Errorf("run_failed summary = %q.\n"+
			"The agent ended its OWN turn and left HEAD where it found it, and the run knows it did:\n"+
			"the announcement is recorded at the kill site and read by the terminal classifier.\n"+
			"Reporting only the unmoved HEAD blames the implementer's exit and sends the next reader\n"+
			"to the worktree and the merge path, which is where neither of the 2026-08-15 failures was.",
			summary)
	}

	if !strings.Contains(summary, "/.harmonik/pi-agent/") {
		t.Errorf("run_failed summary = %q; want it to name the directory holding the agent's captured output.\n"+
			"The daemon tees this agent's stdout to <worktree>/.harmonik/pi-agent/pi-stdout.log and carries the\n"+
			"path back on the launch result, so a reason that omits it makes the reader go and find it.",
			summary)
	}

	if closed := res.Ledger.closedIDs(); slices.Contains(closed, beadID) {
		t.Errorf("bead %s was CLOSED although its implementer committed nothing (closed=%v)", beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s reached no reopen; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_UnannouncedNoCommitIsStillReportedAsAnExit is the control. Same
// harness, same absent commit, no announcement — so the run must NOT claim the
// agent ended its own turn.
func TestDotNode_UnannouncedNoCommitIsStillReportedAsAnExit(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-c6v0m-silent-no-commit")

	opts := dotFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureSilentNoCommitHandler(t))
	opts.HookOutcome = ""
	opts.WaitForRunTerminal = true

	res := runDotFixtureBead(t, beadID, opts)

	summary := dotFixtureRunFailedSummary(res)
	if strings.Contains(summary, "announced the end of its turn") {
		t.Errorf("run_failed summary = %q; this agent announced NOTHING.\n"+
			"A reason that reads the same for an agent that ended its own turn and one that simply\n"+
			"stopped is the misattribution this bead removes, pointed the other way.",
			summary)
	}
	if !strings.Contains(summary, "exited without advancing HEAD") {
		t.Errorf("run_failed summary = %q; want the unannounced no-commit reason", summary)
	}
}

// TestDotNode_NoCommitWithNoCaptureNamesNoDirectory holds the reason correct
// when there is no captured output to point at.
//
// PiCaptureDir is empty for every harness that is not pi and for a launch that
// died before the capture directory existed. A sentence that appended the
// clause unconditionally would offer an empty path — worse than saying nothing,
// because it reads as a directory that ought to exist. This run uses the
// fixture's default harness, so no capture is ever opened.
func TestDotNode_NoCommitWithNoCaptureNamesNoDirectory(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-c6v0m-no-capture")

	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript:      dotFixtureNoCommitHandler(t),
		WaitForRunTerminal: true,
	})

	summary := dotFixtureRunFailedSummary(res)
	if !strings.Contains(summary, "exited without advancing HEAD") {
		t.Fatalf("run_failed summary = %q; want the no-commit reason", summary)
	}
	if strings.Contains(summary, "captured output") {
		t.Errorf("run_failed summary = %q; this run captured nothing.\n"+
			"An evidence pointer with no evidence behind it sends the reader to a path that does not exist.",
			summary)
	}
}
