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

func irfDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "implement-review-fix.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("irfDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func irfRun(t *testing.T) *core.Run {
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

func irfOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestIRF_ApproveOnFirstPass exercises the happy path:
// start → implement → review(APPROVE) → close (terminal).
func TestIRF_ApproveOnFirstPass(t *testing.T) {
	dotPath := irfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := irfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("implement→review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review", irfOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", irfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestIRF_TwoRequestChangesThenApprove exercises the bounded loop:
// start → implement → review(RC) → implement → review(RC) →
// implement → review(APPROVE) → close.
func TestIRF_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := irfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := irfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	for i := 1; i <= 2; i++ {
		dec = workflow.DecideNextNode(graph, "implement", irfOutcome(""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "review" {
			t.Fatalf("iteration %d implement→review: %+v", i, dec)
		}

		if _, err := cycles.Increment(run.RunID, "review", "implement", nil); err != nil {
			t.Fatalf("pre-fill cycle counter review\u2192implement: %v", err)
		}

		dec = workflow.DecideNextNode(graph, "review", irfOutcome("REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "implement" {
			t.Fatalf("iteration %d review→implement: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("final implement→review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review", irfOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", irfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestIRF_BlockOnFirst exercises:
// start → implement → review(BLOCK) → close-needs-attention (terminal).
func TestIRF_BlockOnFirst(t *testing.T) {
	dotPath := irfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := irfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("implement→review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review", irfOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", irfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestIRF_CapHitFallback exercises WG-028/EM-043: when the review→implement
// back-edge's traversal_cap (3) is exhausted, the conditional edge is
// suppressed and the cascade reports a cap-hit failure.
func TestIRF_CapHitFallback(t *testing.T) {
	dotPath := irfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := irfRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", irfOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "implement", irfOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "review", "implement", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter review\u2192implement: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "review", irfOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestIRF_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the reviewer emits a label that matches no conditional edge, the cascade
// falls through to the unconditional fallback → close-needs-attention.
func TestIRF_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := irfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := irfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("implement→review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review", irfOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", irfOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
