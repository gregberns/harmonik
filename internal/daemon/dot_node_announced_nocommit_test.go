package daemon_test

// dot_node_announced_nocommit_test.go — when an agent ends its own turn
// and commits nothing, the run must say THAT.
//
// The run still fails, and it must: the HEAD-advance guard is what stopped two
// bad runs on 2026-08-15 from merging anything. What those runs did not say is
// WHY. Both were reported as `node "implement" (implementer) exited without
// advancing HEAD past <sha>` — a sentence about the process exit, which sends
// the next reader to the worktree and the merge path. One of them had a model
// endpoint that refused three times. The other produced a final turn with no
// tool call in it: the agent typed the literal characters of a JSON tool call
// into its message. Recovering the second fact took a read of a 188 MB log.
//
// The daemon holds neither of those as a value it could put in a sentence (see
// dotNoHeadAdvanceReason). It holds two other things it was throwing away: the
// agent announced the end of its own turn, and the agent's own output was
// captured to a directory whose path the run knows. Together they point at the
// last turn instead of at the merge, and they say where to read it.
//
// The pair is what makes the claim mean something. Both runs commit nothing and
// both fail; only the first announces. A "fix" that stamped the new sentence on
// every no-commit failure would say something false about the second, and the
// control is what catches it.
//
// Bead: hk-c6v0m.

import (
	"slices"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// dotFixtureAgentEndNoCommitHandler writes an implementer that does no work at
// all, announces the end of its turn, and then lingers.
//
// It is dotFixtureAgentEndThenLingerHandler with the commit removed, and the
// removal is the whole scenario: this is the shape of a turn that reasoned and
// then emitted no tool call, and the shape of a turn whose model endpoint
// refused every attempt. Both reach the daemon as one announcement and an
// unmoved HEAD.
//
// The linger matters for the same reason it does there. An implementer that
// announced and then exited 0 would be judged on an exit code it produced
// itself; lingering makes the daemon's own kill end the process every time, so
// the wait reports the manufactured signal death a real Pi turn produces.
func dotFixtureAgentEndNoCommitHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-agent-end-no-commit.sh",
		`printf '{"type":"session","version":3,"id":"pi-fixture-session"}\n'`+"\n"+
			`printf '{"type":"agent_end","messages":[]}\n'`+"\n"+
			"while :; do sleep 1; done\n")
}

// dotFixtureSilentNoCommitHandler writes an implementer that does no work and
// exits 0 without announcing anything — a harness whose process simply ended.
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
	// Pi reports nothing on the hook socket, so the exit is all the classifier
	// has — the real condition this bead is about.
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

	// The path is what turns the reason into something a reader can act on. The
	// missing tool call is IN that file — pi's stdout log keeps toolCall,
	// agent_end and turn_end — so naming the directory deletes the hunt the old
	// sentence forced.
	if !strings.Contains(summary, "/.harmonik/pi-agent/") {
		t.Errorf("run_failed summary = %q; want it to name the directory holding the agent's captured output.\n"+
			"The daemon tees this agent's stdout to <worktree>/.harmonik/pi-agent/pi-stdout.log and carries the\n"+
			"path back on the launch result, so a reason that omits it makes the reader go and find it.",
			summary)
	}

	// The guard must still be the thing that failed the run. This bead is about
	// the reason, never about which runs fail.
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
