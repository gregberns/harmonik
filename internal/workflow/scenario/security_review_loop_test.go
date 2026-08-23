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

func srlDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "security-review-loop.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("srlDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func srlRun(t *testing.T) *core.Run {
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

func srlOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestSRL_ApproveOnFirstPass exercises the happy path:
// start → implement → security_review(APPROVE) → close (terminal).
func TestSRL_ApproveOnFirstPass(t *testing.T) {
	dotPath := srlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("implement→security_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("security_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", srlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSRL_TwoRequestChangesThenApprove exercises the bounded loop:
// start → implement → security_review(RC) → implement → security_review(RC) →
// implement → security_review(APPROVE) → close.
func TestSRL_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := srlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	for i := 1; i <= 2; i++ {
		dec = workflow.DecideNextNode(graph, "implement", srlOutcome(""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "security_review" {
			t.Fatalf("iteration %d implement→security_review: %+v", i, dec)
		}

		if _, err := cycles.Increment(run.RunID, "security_review", "implement", nil); err != nil {
			t.Fatalf("pre-fill cycle counter security_review\u2192implement: %v", err)
		}

		dec = workflow.DecideNextNode(graph, "security_review", srlOutcome("REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "implement" {
			t.Fatalf("iteration %d security_review→implement: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("final implement→security_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final security_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", srlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSRL_BlockOnFirst exercises the ship-blocking security defect path:
// start → implement → security_review(BLOCK) → close-needs-attention (terminal).
func TestSRL_BlockOnFirst(t *testing.T) {
	dotPath := srlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("implement→security_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("security_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", srlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSRL_CapHitFallback exercises WG-028/EM-043: when the security_review→implement
// back-edge's traversal_cap (3) is exhausted, the conditional edge is
// suppressed and the cascade reports a cap-hit failure.
func TestSRL_CapHitFallback(t *testing.T) {
	dotPath := srlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srlRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", srlOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "implement", srlOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "security_review", "implement", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter security_review\u2192implement: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "security_review", srlOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestSRL_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the security reviewer emits a label that matches no conditional edge, the
// cascade falls through to the unconditional fallback → close-needs-attention.
func TestSRL_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := srlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srlRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("implement→security_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", srlOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
