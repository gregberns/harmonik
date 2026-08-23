package scenario_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func rgDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "regression-gate.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("rgDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func rgRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      mustWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func rgOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

func rgOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

func rgLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(rgDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

// TestRG_HappyPathBugReproduced exercises the full success arc:
// start → reproduce(FAIL) → fix_bug(SUCCESS) → regression_suite(SUCCESS) → close.
// FAIL on reproduce is the *forward* path (bug confirmed present → fix it).
func TestRG_HappyPathBugReproduced(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_bug" {
		t.Fatalf("reproduce(FAIL)→fix_bug: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "regression_suite" {
		t.Fatalf("fix_bug→regression_suite: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "regression_suite", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("regression_suite(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRG_CannotReproduce exercises the "bug absent" path:
// start → reproduce(SUCCESS) → cannot_reproduce → close-needs-attention.
// SUCCESS on reproduce means the test passed — bug is not present.
func TestRG_CannotReproduce(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cannot_reproduce" {
		t.Fatalf("reproduce(SUCCESS)→cannot_reproduce: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "cannot_reproduce", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("cannot_reproduce→close-needs-attention: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRG_ReproduceInfraFallback exercises the unconditional fallback on reproduce:
// start → reproduce(RETRY, neither SUCCESS nor FAIL) → close-needs-attention.
// A RETRY outcome (e.g. transient infra failure during the test probe) does not
// match either conditional edge, so the unconditional fallback fires.
func TestRG_ReproduceInfraFallback(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusRetry), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("reproduce(RETRY) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// TestRG_RegressionFixLoop exercises the back-edge capped loop:
// reproduce(FAIL) → fix_bug → regression_suite(FAIL+deterministic) →
// fix_bug → regression_suite(SUCCESS) → close.
// A deterministic failure on the first regression run means the fix didn't hold;
// the capped back-edge sends control back to fix_bug for a second attempt.
func TestRG_RegressionFixLoop(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_bug" {
		t.Fatalf("reproduce(FAIL)→fix_bug: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "regression_suite" {
		t.Fatalf("fix_bug→regression_suite: %+v", dec)
	}

	traversalCap := 3
	if _, err := cycles.Increment(run.RunID, "regression_suite", "fix_bug", &traversalCap); err != nil {
		t.Fatalf("pre-fill cycle counter regression_suite\u2192fix_bug: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "regression_suite", rgOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_bug" {
		t.Fatalf("regression_suite(FAIL+deterministic)→fix_bug: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "regression_suite" {
		t.Fatalf("fix_bug (2nd)→regression_suite: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "regression_suite", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("regression_suite(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRG_RegressionCapHit exercises traversal_cap enforcement on the
// regression_suite→fix_bug back-edge (cap=3):
// reproduce(FAIL) → fix_bug → regression_suite × 3 FAIL+deterministic → cap-hit failure.
func TestRG_RegressionCapHit(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail), run, cycles)
	workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "regression_suite", "fix_bug", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter regression_suite\u2192fix_bug: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "regression_suite", rgOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Failed {
		t.Fatalf("expected Failed=true on cap-hit, got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("expected CompletionReason=cap_hit, got %q (%+v)", dec.CompletionReason, dec)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}

// TestRG_RegressionTransientFallback exercises the unconditional fallback on
// regression_suite: a FAIL+transient outcome does NOT match the deterministic
// compound condition, so the fallback fires → close-needs-attention.
func TestRG_RegressionTransientFallback(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail), run, cycles)
	workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess), run, cycles)

	dec := workflow.DecideNextNode(graph, "regression_suite", rgOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("regression_suite(FAIL+transient) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
