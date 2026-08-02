package daemon_test

// single_nowork_detector_test.go — no-work detection must cover every
// process-exit harness in the no-review DOT graph, not just codex.
//
// The detector answers the question an operator asks when a run fails with no
// explanation: did the agent do nothing at all? It fires when the commit
// fallback found nothing to commit AND the phase finished faster than the floor.
// It sat inside the codex leg of the fallback's harness branch, so a no-review
// DOT Pi run — same shape, same clean worktree, same seconds-long phase — produced
// no such record.
//
// The two harness legs are the only difference between these tests. Same
// implementer, same fixture, same timing.
//
// Bead: hk-3ywqv.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// singleFixtureProcessExitOpts builds a legacy single input that resolves to
// no-review DOT on a process-exit harness. The launch-spec port stamps the
// resolved agent type, which is what the commit fallback branches on.
func singleFixtureProcessExitOpts(t *testing.T, agent core.AgentType, script string) dotFixtureOpts {
	t.Helper()

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	if h, hErr := reg.ForAgent(agent); hErr != nil {
		t.Fatalf("harness registry has no %s harness: %v", agent, hErr)
	} else if h.Completion() != handlercontract.CompletionProcessExit {
		t.Fatalf("%s harness completion = %v; this test only means something for a process-exit harness", agent, h.Completion())
	}

	build := daemon.ExportedPiProcessExitLaunchSpecBuilder(script)
	if agent == core.AgentTypeCodex {
		build = daemon.ExportedCodexProcessExitLaunchSpecBuilder(script)
	}
	return dotFixtureOpts{
		WorkflowMode:      core.WorkflowModeSingle,
		HandlerScript:     script,
		HarnessRegistry:   reg,
		LaunchSpecBuilder: build,
	}
}

// TestLegacySingleInput_NoReviewDOTPiNoWorkIsFlagged is the claim. A Pi implementer that
// commits nothing and leaves a clean worktree in seconds MUST be recorded as a
// suspected no-work run.
func TestLegacySingleInput_NoReviewDOTPiNoWorkIsFlagged(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-3ywqv-pi-no-work")
	res := runDotFixtureBead(t, beadID,
		singleFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureNoCommitHandler(t)))

	if !singleFixtureHasEvent(res, core.EventTypeImplementerNoWorkSuspected) {
		t.Errorf("Pi bead %s produced no commit and a clean worktree in seconds, and emitted no implementer_no_work_suspected; events=%v.\n"+
			"The detector sits inside the codex leg of the commit fallback's harness branch, so a no-review DOT Pi run never reaches it.",
			beadID, res.Bus.eventTypes())
	}
}

// TestLegacySingleInput_NoReviewDOTCodexNoWorkIsFlagged is the reference the claim is measured
// against. It is the SAME implementer and the SAME fixture; only the harness
// differs. Codex has always been detected. If this one goes red the detector or
// the fixture broke, and the Pi test's verdict means nothing.
func TestLegacySingleInput_NoReviewDOTCodexNoWorkIsFlagged(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-3ywqv-codex-no-work")
	res := runDotFixtureBead(t, beadID,
		singleFixtureProcessExitOpts(t, core.AgentTypeCodex, dotFixtureNoCommitHandler(t)))

	if !singleFixtureHasEvent(res, core.EventTypeImplementerNoWorkSuspected) {
		t.Errorf("codex bead %s produced no commit and a clean worktree in seconds, and emitted no implementer_no_work_suspected; events=%v",
			beadID, res.Bus.eventTypes())
	}
}

// TestLegacySingleInput_NoReviewDOTPiCommitIsNotFlagged keeps the claim from being
// satisfied by a detector that fires on every Pi run. The implementer does real
// work, so the fallback finds a commit and the run must NOT be flagged.
func TestLegacySingleInput_NoReviewDOTPiCommitIsNotFlagged(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-3ywqv-pi-real-work")
	res := runDotFixtureBead(t, beadID,
		singleFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureCommittingHandler(t, beadID)))

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Fatalf("Pi bead %s was not closed by a committing implementer (reopened=%v, events=%v)",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
	if singleFixtureHasEvent(res, core.EventTypeImplementerNoWorkSuspected) {
		t.Errorf("Pi bead %s committed real work and was still flagged as a suspected no-work run; events=%v",
			beadID, res.Bus.eventTypes())
	}
}
