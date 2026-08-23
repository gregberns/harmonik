package daemon_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

const dotReviewerGraph = `digraph "dot-reviewer-fixture" {
    schema_version="1";
    version="1.0";
    workflow_id="dot-reviewer-fixture";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="entry"
    ];

    implement [
        type="agentic",
        agent_type="implementer",
        handler_ref="claude-implementer",
        idempotency_class="non-idempotent",
        role="produce the change; commit required"
    ];

    review [
        type="agentic",
        agent_type="reviewer",
        handler_ref="claude-reviewer",
        idempotency_class="idempotent",
        role="render APPROVE / REQUEST_CHANGES / BLOCK verdict on the change"
    ];

    close [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="success terminal"
    ];

    "close-needs-attention" [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="failure terminal"
    ];

    start -> implement;

    implement -> review;

    review -> close [
        condition="outcome.preferred_label == 'APPROVE'"
    ];

    review -> "close-needs-attention";
}
`

const dotReviewerApproveJSON = `{"schema_version":1,"verdict":"APPROVE","flags":[],` +
	`"notes":"fixture reviewer approves the committed work."}`

const dotReviewerBudgetSentinelJSON = `{"budget_ms":600000,"changed_lines":42,` +
	`"elapsed_ms":611000,"reason":"budget-exceeded"}`

type dotReviewerHandlerOpts struct {
	// ExitCode is what the reviewer process exits with after writing its verdict.
	ExitCode int
	// WriteBudgetSentinel makes the reviewer leave the budget-kill marker behind,
	// which is what a real budget kill does before it sends /quit.
	WriteBudgetSentinel bool
	// GarbageProgressLine makes the reviewer print a line the NDJSON
	// progress-stream reader cannot parse, which puts the watcher into error.
	GarbageProgressLine bool

	// StaleBudgetSentinelFromImplementer makes the IMPLEMENTER leave a budget
	// marker in the worktree. It stands in for a marker a PRIOR iteration's
	// budget-killed reviewer left behind, which is what the scrub before each
	// reviewer launch has to clear.
	StaleBudgetSentinelFromImplementer bool
}

func dotReviewerHandler(t *testing.T, bead core.BeadID, opts dotReviewerHandlerOpts) string {
	t.Helper()

	reviewerBody := "  mkdir -p .harmonik\n" +
		"  printf '%s' '" + dotReviewerApproveJSON + "' > .harmonik/review.json\n"
	if opts.WriteBudgetSentinel {
		reviewerBody += "  printf '%s' '" + dotReviewerBudgetSentinelJSON +
			"' > .harmonik/reviewer-budget-exceeded.json\n"
	}
	if opts.GarbageProgressLine {
		reviewerBody += "  printf 'this line is not NDJSON\\n'\n"
	}
	reviewerBody += "  exit " + strconv.Itoa(opts.ExitCode) + "\n"

	implementerBody := dotFixtureCommitLines(bead)
	if opts.StaleBudgetSentinelFromImplementer {
		implementerBody += "mkdir -p .harmonik\n" +
			"printf '%s' '" + dotReviewerBudgetSentinelJSON +
			"' > .harmonik/reviewer-budget-exceeded.json\n"
	}

	body := "if [ -f .harmonik/review-target.md ]; then\n" +
		reviewerBody +
		"fi\n" +
		implementerBody +
		"exit 0\n"
	return dotFixtureHandlerScript(t, "dot-reviewer-fixture.sh", body)
}

type dotReviewerHookStore struct {
	Outcome json.RawMessage

	mu    sync.Mutex
	seen  []string
	first string
}

func (s *dotReviewerHookStore) outcomeFor(claudeSessionID string) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := false
	for _, id := range s.seen {
		if id == claudeSessionID {
			known = true
			break
		}
	}
	if !known {
		s.seen = append(s.seen, claudeSessionID)
	}
	if s.first == "" {
		s.first = claudeSessionID
	}
	if claudeSessionID == s.first {
		return nil
	}
	return s.Outcome
}

// SessionCount is the number of distinct claude sessions the store was asked
// about.
func (s *dotReviewerHookStore) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func (*dotReviewerHookStore) RegisterHookSession(string, string) {}
func (*dotReviewerHookStore) CloseHookSession(string, string)    {}

func (s *dotReviewerHookStore) LatestOutcome(_, claudeSessionID string) *json.RawMessage {
	out := s.outcomeFor(claudeSessionID)
	if len(out) == 0 {
		return nil
	}
	return &out
}

func (s *dotReviewerHookStore) WaitForOutcome(_ context.Context, _, claudeSessionID string) (json.RawMessage, error) {
	return s.outcomeFor(claudeSessionID), nil
}

func (*dotReviewerHookStore) SetAgentReadyCallback(_, _ string, cb func()) {
	if cb != nil {
		cb()
	}
}

func runDotReviewerBead(t *testing.T, beadID core.BeadID, handlerOpts dotReviewerHandlerOpts, opts dotFixtureOpts) dotFixtureResult {
	t.Helper()
	opts.Graph = dotReviewerGraph
	opts.HandlerScript = dotReviewerHandler(t, beadID, handlerOpts)
	return runDotFixtureBead(t, beadID, opts)
}

// TestDotReviewer_ApproveThenCleanExitClosesTheBead is the control, and it comes
// first because every assertion below is worthless without it. Same graph, same
// handler, same APPROVE verdict — the reviewer just exits 0.
//
// If this cannot reach a green close, the failure tests are satisfied by a
// fixture in which nothing ever closes a bead.
func TestDotReviewer_ApproveThenCleanExitClosesTheBead(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sb8jy-approve-clean-exit")
	res := runDotReviewerBead(t, beadID, dotReviewerHandlerOpts{ExitCode: 0}, dotFixtureOpts{})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Fatalf("bead %s was not closed after its reviewer wrote APPROVE and exited 0 (reopened=%v, events=%v).\n"+
			"The happy path must reach a green close, or the failure tests in this file prove nothing.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
}

// TestDotReviewer_ApproveThenNonZeroExitFailsTheNode is the headline claim: a
// reviewer that writes APPROVE and then dies must not merge the work.
//
// The APPROVE is what makes this bite. The verdict file is well-formed and the
// node's own verdict read succeeds, so the ONLY thing between this run and a
// green close is the reviewer's reported exit.
func TestDotReviewer_ApproveThenNonZeroExitFailsTheNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sb8jy-approve-then-crash")
	res := runDotReviewerBead(t, beadID, dotReviewerHandlerOpts{ExitCode: 3}, dotFixtureOpts{})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its reviewer exited non-zero after writing APPROVE (closed=%v).\n"+
			"The reviewer branch returned SUCCESS on the verdict file alone and never read the exit, so the APPROVE edge reached the success terminal and the work merged.",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened; want a crashed reviewer to reopen it. events=%v",
			beadID, res.Bus.eventTypes())
	}
}

// TestDotReviewer_ApproveThenFailureSignalFailsTheNode covers CHB-020 branch 2:
// the reviewer's Stop hook reported FAILURE_SIGNAL through the socket after the
// APPROVE landed. The exit code is 0, so the socket outcome is the only signal
// there is.
func TestDotReviewer_ApproveThenFailureSignalFailsTheNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sb8jy-approve-then-failure-signal")
	store := &dotReviewerHookStore{Outcome: json.RawMessage(dotFixtureFailureSignal)}
	res := runDotReviewerBead(t, beadID, dotReviewerHandlerOpts{ExitCode: 0}, dotFixtureOpts{
		HookStore: store,
	})

	if n := store.SessionCount(); n < 2 {
		t.Fatalf("the hook store saw %d distinct claude session(s); want at least 2 (implementer then reviewer).\n"+
			"With one session the store answers nil for the reviewer too and this test asserts nothing.", n)
	}
	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its reviewer signalled FAILURE_SIGNAL after writing APPROVE (closed=%v)",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after a reviewer reported failure; events=%v",
			beadID, res.Bus.eventTypes())
	}
}

// TestDotReviewer_ApproveThenWatcherErrorFailsTheNode covers the watcher leg. The
// reviewer writes APPROVE, then writes a line the NDJSON progress-stream reader
// cannot parse, then exits 0. Nothing about the verdict or the exit code is
// wrong; only the watcher knows.
func TestDotReviewer_ApproveThenWatcherErrorFailsTheNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sb8jy-approve-then-watcher-error")
	res := runDotReviewerBead(t, beadID, dotReviewerHandlerOpts{
		ExitCode:            0,
		GarbageProgressLine: true,
	}, dotFixtureOpts{})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its reviewer's progress-stream watcher failed after the APPROVE (closed=%v)",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after a reviewer watcher failure; events=%v",
			beadID, res.Bus.eventTypes())
	}
	if !strings.Contains(strings.Join(res.Bus.eventTypes(), ","), string(core.EventTypeRunFailed)) {
		t.Errorf("no run_failed event for the reviewer watcher failure; events=%v", res.Bus.eventTypes())
	}
}

// TestDotReviewer_BudgetKillAfterApproveStillMerges is the guard on the fix, and
// it is the reason the classification could not simply be moved.
//
// A reviewer that runs past its verdict budget is killed BY THE DAEMON:
// pasteInjectQuitOnReviewFile writes reviewer-budget-exceeded.json, sends /quit,
// then kills the pane. The non-zero exit that follows describes the daemon's
// kill, not a reviewer that crashed. When the verdict landed before the kill it
// is complete and it stands.
//
// If this goes red, every long review that still delivered a verdict now reads
// as a crash and good work is blocked — which is worse than the hole the other
// tests here close.
func TestDotReviewer_BudgetKillAfterApproveStillMerges(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sb8jy-budget-kill-after-approve")
	res := runDotReviewerBead(t, beadID, dotReviewerHandlerOpts{
		ExitCode:            3,
		WriteBudgetSentinel: true,
	}, dotFixtureOpts{})

	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed although its reviewer wrote APPROVE and was then killed for exceeding its verdict budget (reopened=%v, events=%v).\n"+
			"A budget kill is the daemon's own kill, not a reviewer crash; reading it as a crash false-fails a legitimate review and blocks a good merge.",
			beadID, res.Ledger.reopenedIDs(), res.Bus.eventTypes())
	}
	if !strings.Contains(strings.Join(res.Bus.eventTypes(), ","), string(core.EventTypeReviewerBudgetExceeded)) {
		t.Errorf("no reviewer_budget_exceeded event for a budget-killed reviewer; events=%v", res.Bus.eventTypes())
	}
}

// TestDotReviewer_StaleBudgetMarkerDoesNotExcuseALaterReviewer closes the hole
// the budget exemption opens if the marker is never cleared.
//
// The marker lives in the run worktree, which outlives a single reviewer visit.
// Nothing removed it, so ONE legitimate budget kill left the file there for good
// and excused every later reviewer in that worktree from the terminal check —
// including one that wrote APPROVE and then crashed. The hk-bqf1q reviewer retry
// reaches that state with nothing going wrong and no bad actor.
//
// Here the implementer leaves the stale marker, standing in for the prior
// iteration. This reviewer is NOT budget-killed: it writes APPROVE and exits
// non-zero on its own account, so it must be refused.
func TestDotReviewer_StaleBudgetMarkerDoesNotExcuseALaterReviewer(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sb8jy-stale-budget-marker")
	res := runDotReviewerBead(t, beadID, dotReviewerHandlerOpts{
		ExitCode:                           3,
		StaleBudgetSentinelFromImplementer: true,
	}, dotFixtureOpts{})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was CLOSED although its reviewer wrote APPROVE and then exited non-zero (closed=%v).\n"+
			"A budget marker left by an EARLIER visit excused this reviewer from the terminal check. The marker must be scrubbed before each reviewer launch, the way review.json already is.",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened; events=%v", beadID, res.Bus.eventTypes())
	}
}
