package daemon_test

// dot_node_baseline_test.go — a graph node with no readable baseline HEAD must
// fail, not guess.
//
// The pre-launch probe dropped its error. An unreadable worktree therefore gave
// the node an EMPTY baseline, and an empty baseline is not a harmless zero:
//
//   - the no-advance guard cannot fire, because the post-exit HEAD can never
//     equal an empty string, so a node that did no work returns SUCCESS;
//   - the trailer amend leg is disabled, because it gates on a non-empty parent,
//     so a commit lands untrailered and is then mislabelled as no-change; and
//   - the quit-on-commit watchdog reads "HEAD moved" on its FIRST poll, sends
//     /quit and force kills the agent seconds after the brief is delivered.
//
// The bad state needs the PRE-probe to fail while the POST-probe succeeds — a
// transient, most plausibly on a remote run whose probe crosses SSH. That is
// what dotFixtureFlakyBaselineRunner reproduces.
//
// Bead: hk-o4sgg.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// dotFixtureFlakyBaselineRunner passes every command through to a local exec,
// except that it fails ONE `rev-parse HEAD` — the first one after the node's
// agent-task.md is written.
//
// That command is keyed on rather than counted because it is the one the node
// makes for its own baseline: the launch-spec build writes agent-task.md, and
// the very next probe is the baseline read. Counting would pin the test to a
// probe order that is free to change.
//
// failBaseline false leaves every probe intact, which is the control's runner.
type dotFixtureFlakyBaselineRunner struct {
	mu           sync.Mutex
	failBaseline bool
	armed        bool
	failed       bool
}

func (r *dotFixtureFlakyBaselineRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	joined := strings.Join(args, " ")
	r.mu.Lock()
	switch {
	case strings.Contains(joined, "agent-task.md"):
		r.armed = true
	case r.armed && r.failBaseline && !r.failed && strings.Contains(joined, "rev-parse HEAD"):
		r.armed = false
		r.failed = true
		r.mu.Unlock()
		// `false` exits 1 with no output, which is what a probe that could not
		// reach the worktree looks like to the caller.
		return exec.CommandContext(ctx, "false")
	}
	r.mu.Unlock()
	return exec.CommandContext(ctx, name, args...)
}

func (r *dotFixtureFlakyBaselineRunner) baselineProbeFailed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed
}

// TestDotNode_UnreadableBaselineDoesNotPassANodeThatDidNoWork is the claim. The
// implementer does nothing at all. Its baseline probe fails, its post-exit probe
// succeeds, and the node MUST NOT report SUCCESS off the difference between a
// real SHA and an empty string.
func TestDotNode_UnreadableBaselineDoesNotPassANodeThatDidNoWork(t *testing.T) {
	t.Parallel()

	runner := &dotFixtureFlakyBaselineRunner{failBaseline: true}
	const beadID = core.BeadID("hk-o4sgg-unreadable-baseline")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureNoCommitHandler(t),
		Runner:        runner,
	})

	if !runner.baselineProbeFailed() {
		t.Fatal("the fixture never failed a baseline probe, so this test asserts nothing — fix the fixture before trusting the result")
	}
	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its implementer produced no commit (closed=%v).\n"+
			"The baseline probe error was dropped, so the node compared a real post-exit SHA against an empty baseline, read that as HEAD advancing, and reported SUCCESS.",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s reached no reopen; events=%v", beadID, res.Bus.eventTypes())
	}
	// The run must fail for THIS reason. An empty baseline also poisons the
	// quit-on-commit watchdog, so a later regression could reopen the bead for
	// the wrong reason and satisfy the assertions above for free.
	if summary := dotFixtureRunFailedSummary(res); !strings.Contains(summary, "resolve HEAD before node") {
		t.Errorf("run_failed summary = %q; want it to name the unreadable baseline", summary)
	}
}

// dotFixtureRunFailedSummary returns the summary of the first run_failed event,
// or "" when the run did not fail.
func dotFixtureRunFailedSummary(res dotFixtureResult) string {
	for _, ev := range res.Bus.allEvents() {
		if ev.EventType != string(core.EventTypeRunFailed) {
			continue
		}
		var pl struct {
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			return ""
		}
		return pl.Summary
	}
	return ""
}

// TestDotNode_ReadableBaselineFailsANodeThatDidNoWork is the control, and it is
// what makes the test above mean something. Same runner type, same do-nothing
// implementer, one field different: the baseline probe is left intact.
//
// It also proves the no-advance guard is alive at all. Without it, "the bead was
// not closed" could be true because nothing in this fixture ever closes a bead.
func TestDotNode_ReadableBaselineFailsANodeThatDidNoWork(t *testing.T) {
	t.Parallel()

	runner := &dotFixtureFlakyBaselineRunner{failBaseline: false}
	const beadID = core.BeadID("hk-o4sgg-readable-baseline")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureNoCommitHandler(t),
		Runner:        runner,
	})

	if runner.baselineProbeFailed() {
		t.Fatal("the control runner failed a probe; it must leave every probe intact")
	}
	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was closed although its implementer produced no commit (closed=%v)", beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s reached no reopen with a healthy baseline; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_HealthyRunWithARunnerStillCloses proves the injected runner is not
// itself what fails the two runs above. A committing implementer under the same
// passthrough runner must still reach a green close.
func TestDotNode_HealthyRunWithARunnerStillCloses(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-o4sgg-healthy-runner")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		Runner: &dotFixtureFlakyBaselineRunner{failBaseline: false},
	})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed under a passthrough runner (reopened=%v, events=%v).\n"+
			"Fix this before believing the two tests above — while a run with a runner can never close, they are satisfied for free.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
}

var _ tmuxPkg.CommandRunner = (*dotFixtureFlakyBaselineRunner)(nil)
