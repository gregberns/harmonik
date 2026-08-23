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

func prfDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "plan-review-finalize.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("prfDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func prfRun(t *testing.T) *core.Run {
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

func prfOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestPRF_ApproveOnFirstPass exercises the happy path with finalize seam:
// start → draft_plan → plan_review(APPROVE) → finalize_plan → plan-approved (terminal).
func TestPRF_ApproveOnFirstPass(t *testing.T) {
	dotPath := prfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "finalize_plan" {
		t.Fatalf("plan_review→finalize_plan: Advance=%v NextNodeID=%q, want finalize_plan", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "finalize_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("finalize_plan→plan-approved: Advance=%v NextNodeID=%q, want plan-approved", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-approved", prfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRF_TwoRequestChangesThenApprove exercises the bounded loop with finalize seam:
// start → draft_plan → plan_review(RC) → draft_plan → plan_review(RC) →
// draft_plan → plan_review(APPROVE) → finalize_plan → plan-approved.
func TestPRF_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := prfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}

	for i := 1; i <= 2; i++ {
		dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "plan_review" {
			t.Fatalf("iteration %d draft_plan→plan_review: %+v", i, dec)
		}

		if _, err := cycles.Increment(run.RunID, "plan_review", "draft_plan", nil); err != nil {
			t.Fatalf("pre-fill cycle counter plan_review\u2192draft_plan: %v", err)
		}

		dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome("REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "draft_plan" {
			t.Fatalf("iteration %d plan_review→draft_plan: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("final draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "finalize_plan" {
		t.Fatalf("final plan_review→finalize_plan: Advance=%v NextNodeID=%q, want finalize_plan", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "finalize_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("finalize_plan→plan-approved: Advance=%v NextNodeID=%q, want plan-approved", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-approved", prfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRF_BlockOnFirst exercises:
// start → draft_plan → plan_review(BLOCK) → plan-needs-attention (terminal).
func TestPRF_BlockOnFirst(t *testing.T) {
	dotPath := prfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-needs-attention" {
		t.Fatalf("plan_review→plan-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-needs-attention", prfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRF_CapHitFallback exercises WG-028/EM-043: when the plan_review→draft_plan
// back-edge's traversal_cap (3) is exhausted, the conditional edge is
// suppressed and the cascade reports a cap-hit failure.
func TestPRF_CapHitFallback(t *testing.T) {
	dotPath := prfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prfRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", prfOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "draft_plan", prfOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "plan_review", "draft_plan", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter plan_review\u2192draft_plan: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "plan_review", prfOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestPRF_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the reviewer emits a label that matches no conditional edge, the cascade
// falls through to the unconditional fallback → plan-needs-attention.
func TestPRF_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := prfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "plan-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "plan-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "plan-needs-attention", prfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPRF_FinalizeToTerminalByIdentity exercises the key structural difference
// from plan-review-loop: plan-approved is reached via an unconditional edge from
// finalize_plan (a non-agentic intermediate node), NOT directly via a conditional
// APPROVE edge from plan_review. Under the WG-021 fix (hk-z03e8), the terminal
// is classified SUCCESS by terminal identity alone — not by inbound-edge topology.
//
// This is the same invariant exercised by review-loop-finalize.dot but applied
// to the planning workflow variant.
func TestPRF_FinalizeToTerminalByIdentity(t *testing.T) {
	dotPath := prfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := prfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "finalize_plan", prfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("finalize_plan→plan-approved: Advance=%v NextNodeID=%q, want plan-approved",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-approved", prfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true (terminal-by-identity, WG-021)", dec.IsTerminal)
	}
	if dec.Failed {
		t.Fatalf("plan-approved: Failed=%v, want false — terminal-by-identity must be SUCCESS", dec.Failed)
	}
}
