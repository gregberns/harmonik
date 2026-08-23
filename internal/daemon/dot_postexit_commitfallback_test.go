package daemon_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func dotFixtureProcessExitOpts(t *testing.T, agent core.AgentType, script string) dotFixtureOpts {
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
		WorkflowMode:      core.WorkflowModeDot,
		HandlerScript:     script,
		HarnessRegistry:   reg,
		LaunchSpecBuilder: build,
	}
}

func dotFixtureEditNoCommitHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-edit-no-commit.sh",
		"set -e\necho \"uncommitted work for "+string(bead)+" $$\" > fixture-work.txt\nexit 0\n")
}

func fixtureCommitSubjects(t *testing.T, projectDir string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "-C", projectDir, "log", "--all", "--format=%s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log in %s: %v\n%s", projectDir, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// TestDotMode_PiCommitFallbackUsesThePiWrapper is the claim. A Pi node whose
// agent edited files and committed nothing gets its commit from the daemon, and
// that commit must come from the Pi harness's own fallback.
func TestDotMode_PiCommitFallbackUsesThePiWrapper(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-step7-dot-pi-wrapper")
	res := runDotFixtureBead(t, beadID,
		dotFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureEditNoCommitHandler(t, beadID)))

	subjects := fixtureCommitSubjects(t, res.ProjectDir)
	var got string
	for _, s := range subjects {
		if strings.HasPrefix(s, "feat(pi)") || strings.HasPrefix(s, "feat(codex)") {
			got = s
			break
		}
	}
	if !strings.HasPrefix(got, "feat(pi)") {
		t.Errorf("Pi node on bead %s got its fallback commit from the wrong harness wrapper.\n"+
			"  fallback commit subject = %q, want a feat(pi) subject\n"+
			"  all subjects = %v\n"+
			"The graph path calls codex.EnsureRefsTrailer for every process-exit harness, so a Pi\n"+
			"node's daemon-side commit is written by the codex wrapper (specs/pi-harness.md PI-030).",
			beadID, got, subjects)
	}
}

// TestDotMode_CodexCommitFallbackUsesTheCodexWrapper is the reference the claim
// is measured against. Same fixture, same implementer, only the harness differs.
// If this one goes red the fallback or the fixture broke, and the Pi test's
// verdict means nothing.
func TestDotMode_CodexCommitFallbackUsesTheCodexWrapper(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-step7-dot-codex-wrapper")
	res := runDotFixtureBead(t, beadID,
		dotFixtureProcessExitOpts(t, core.AgentTypeCodex, dotFixtureEditNoCommitHandler(t, beadID)))

	subjects := fixtureCommitSubjects(t, res.ProjectDir)
	found := false
	for _, s := range subjects {
		if strings.HasPrefix(s, "feat(codex)") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("codex node on bead %s produced no feat(codex) fallback commit; subjects = %v",
			beadID, subjects)
	}
}

// TestDotMode_PiNoWorkRunIsFlagged holds the coverage the wrapper fix is easy to
// trade away. A Pi node that commits nothing and leaves a clean worktree in
// seconds MUST still be recorded as a suspected no-work run.
//
// It passes before the wrapper fix and after it, and that is the point: the
// detector reads the shared outcome enum, so it must sit OUTSIDE the harness
// branch. Move it inside the codex leg and this test is the one that goes red.
func TestDotMode_PiNoWorkRunIsFlagged(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-step7-dot-pi-no-work")
	res := runDotFixtureBead(t, beadID,
		dotFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureNoCommitHandler(t)))

	if !singleFixtureHasEvent(res, core.EventTypeImplementerNoWorkSuspected) {
		t.Errorf("Pi node on bead %s produced no commit and a clean worktree in seconds, and emitted no implementer_no_work_suspected; events=%v.\n"+
			"The detector must read the commit fallback's outcome enum, not the codex leg of its harness branch.",
			beadID, res.Bus.eventTypes())
	}
}

// TestDotMode_PiRunThatCommittedIsNotFlagged keeps the claim above from being
// satisfied by a detector that fires on every Pi node. The implementer does real
// work, so the fallback finds a commit and the node must NOT be flagged.
func TestDotMode_PiRunThatCommittedIsNotFlagged(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-step7-dot-pi-real-work")
	res := runDotFixtureBead(t, beadID,
		dotFixtureProcessExitOpts(t, core.AgentTypePi, dotFixtureCommittingHandler(t, beadID)))

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Fatalf("Pi node on bead %s was not closed by a committing implementer (reopened=%v, events=%v)",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
	if singleFixtureHasEvent(res, core.EventTypeImplementerNoWorkSuspected) {
		t.Errorf("Pi node on bead %s committed real work and was still flagged as a suspected no-work run; events=%v",
			beadID, res.Bus.eventTypes())
	}
}
