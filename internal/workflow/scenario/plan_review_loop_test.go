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

func prlDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "plan-review-loop.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("prlDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func prlRun(t *testing.T) *core.Run {
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

func prlOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestPRL_ApproveOnFirstPass exercises the happy path:
// start → draft_plan → plan_review(APPROVE) → plan-approved (terminal).
func TestPRL_ApproveOnFirstPass(t *testing.T) {
	dotPath := prlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prlOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("plan_review→plan-approved: Advance=%v NextNodeID=%q, want plan-approved", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-approved", prlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRL_TwoRequestChangesThenApprove exercises the bounded loop:
// start → draft_plan → plan_review(RC) → draft_plan → plan_review(RC) →
// draft_plan → plan_review(APPROVE) → plan-approved.
func TestPRL_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := prlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}

	for i := 1; i <= 2; i++ {
		dec = workflow.DecideNextNode(graph, "draft_plan", prlOutcome(""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "plan_review" {
			t.Fatalf("iteration %d draft_plan→plan_review: %+v", i, dec)
		}

		if _, err := cycles.Increment(run.RunID, "plan_review", "draft_plan", nil); err != nil {
			t.Fatalf("pre-fill cycle counter plan_review\u2192draft_plan: %v", err)
		}

		dec = workflow.DecideNextNode(graph, "plan_review", prlOutcome("REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "draft_plan" {
			t.Fatalf("iteration %d plan_review→draft_plan: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("final draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prlOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("final plan_review→plan-approved: Advance=%v NextNodeID=%q, want plan-approved", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-approved", prlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRL_BlockOnFirst exercises:
// start → draft_plan → plan_review(BLOCK) → plan-needs-attention (terminal).
func TestPRL_BlockOnFirst(t *testing.T) {
	dotPath := prlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prlOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-needs-attention" {
		t.Fatalf("plan_review→plan-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-needs-attention", prlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRL_CapHitFallback exercises WG-028/EM-043: when the plan_review→draft_plan
// back-edge's traversal_cap (3) is exhausted, the conditional edge is
// suppressed and the cascade reports a cap-hit failure.
func TestPRL_CapHitFallback(t *testing.T) {
	dotPath := prlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prlRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", prlOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "draft_plan", prlOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "plan_review", "draft_plan", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter plan_review\u2192draft_plan: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "plan_review", prlOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestPRL_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the reviewer emits a label that matches no conditional edge, the cascade
// falls through to the unconditional fallback → plan-needs-attention.
func TestPRL_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := prlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "draft_plan", prlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prlOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "plan-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "plan-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "plan-needs-attention", prlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
