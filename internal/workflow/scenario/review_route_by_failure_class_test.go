package scenario_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
)

func rfcDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "review-route-by-failure-class.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("rfcDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func rfcRun(t *testing.T) *core.Run {
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

func rfcOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

func rfcOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

// TestRFC_SuccessPathApprove exercises the happy path:
// start → implementer(SUCCESS, no failure_class) → reviewer(APPROVE) → close (terminal).
// SUCCESS carries no failure_class — all failure-class conditions miss and the cascade
// falls through to the unconditional implementer→reviewer handoff.
func TestRFC_SuccessPathApprove(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reviewer", rfcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRFC_TransientRetryThenSuccess exercises the transient self-loop:
// start → implementer(transient) → implementer (retry) → implementer(SUCCESS) →
// reviewer(APPROVE) → close (terminal).
func TestRFC_TransientRetryThenSuccess(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("implementer(transient)→implementer: Advance=%v NextNodeID=%q, want implementer",
			dec.Advance, dec.NextNodeID)
	}
	if _, err := cycles.Increment(run.RunID, "implementer", "implementer", nil); err != nil {
		t.Fatalf("pre-fill cycle counter implementer\u2192implementer: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer(SUCCESS)→reviewer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reviewer", rfcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRFC_TransientCapHit exercises WG-028/EM-043 on the implementer self-loop:
// after 3 transient traversals the self-loop's traversal_cap is exhausted and the
// cascade reports cap_hit (FailureClass=compilation_loop).
func TestRFC_TransientCapHit(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "implementer", "implementer", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter implementer\u2192implementer: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Failed {
		t.Fatalf("expected Failed=true on transient cap-hit, got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("expected CompletionReason=cap_hit, got %q (%+v)", dec.CompletionReason, dec)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}

// TestRFC_StructuralNeedsAttention exercises:
// start → implementer(structural) → close-needs-attention (terminal).
func TestRFC_StructuralNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassStructural), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(structural)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRFC_DeterministicNeedsAttention exercises:
// start → implementer(deterministic) → close-needs-attention (terminal).
func TestRFC_DeterministicNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(deterministic)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRFC_CanceledNeedsAttention exercises:
// start → implementer(canceled) → close-needs-attention (terminal).
func TestRFC_CanceledNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassCanceled), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(canceled)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRFC_BudgetExhaustedNeedsAttention exercises:
// start → implementer(budget_exhausted) → close-needs-attention (terminal).
func TestRFC_BudgetExhaustedNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassBudgetExhausted), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(budget_exhausted)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRFC_CompilationLoopNeedsAttention exercises the handler-emitted
// compilation_loop failure class:
// start → implementer(compilation_loop) → close-needs-attention (terminal).
// This is distinct from the cap-hit path (scenario 3): here the handler itself
// emits the compilation_loop failure class; the cascade routes it via the explicit
// condition edge rather than the traversal-cap mechanism.
func TestRFC_CompilationLoopNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassCompilationLoop), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(compilation_loop)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
