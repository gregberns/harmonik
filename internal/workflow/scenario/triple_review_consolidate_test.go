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

func triplercDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "triple-review-consolidate.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("triplercDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func triplercRun(t *testing.T) *core.Run {
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

func triplercOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

func triplercWalkSpine(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", triplercOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", triplercOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_correctness" {
		t.Fatalf("implement→review_correctness: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review_correctness", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_design" {
		t.Fatalf("review_correctness→review_design: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review_design", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_tests" {
		t.Fatalf("review_design→review_tests: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review_tests", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("review_tests→consolidate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// TestTripleRC_ApproveOnFirstPass exercises the happy path:
// start → implement → review_correctness → review_design → review_tests → consolidate(APPROVE) → close (terminal).
func TestTripleRC_ApproveOnFirstPass(t *testing.T) {
	dotPath := triplercDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := triplercRun(t)
	cycles := core.NewCycleCounter()

	triplercWalkSpine(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", triplercOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestTripleRC_OneRequestChangesThenApprove exercises the bounded loop:
// spine → consolidate(RC) → implement → spine → consolidate(APPROVE) → close.
func TestTripleRC_OneRequestChangesThenApprove(t *testing.T) {
	dotPath := triplercDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := triplercRun(t)
	cycles := core.NewCycleCounter()

	triplercWalkSpine(t, graph, run, cycles)

	if _, err := cycles.Increment(run.RunID, "consolidate", "implement", nil); err != nil {
		t.Fatalf("pre-fill cycle counter consolidate\u2192implement: %v", err)
	}

	dec := workflow.DecideNextNode(graph, "consolidate", triplercOutcome("REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("consolidate→implement (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", triplercOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_correctness" {
		t.Fatalf("implement→review_correctness (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "review_correctness", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_design" {
		t.Fatalf("review_correctness→review_design (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "review_design", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_tests" {
		t.Fatalf("review_design→review_tests (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "review_tests", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("review_tests→consolidate (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "consolidate", triplercOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", triplercOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestTripleRC_BlockOnFirst exercises:
// spine → consolidate(BLOCK) → close-needs-attention (terminal).
func TestTripleRC_BlockOnFirst(t *testing.T) {
	dotPath := triplercDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := triplercRun(t)
	cycles := core.NewCycleCounter()

	triplercWalkSpine(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", triplercOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", triplercOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestTripleRC_CapHitFallback exercises WG-028/EM-043: when the consolidate→implement
// back-edge's traversal_cap (3) is exhausted, the conditional edge is suppressed
// and the cascade reports a cap-hit failure.
func TestTripleRC_CapHitFallback(t *testing.T) {
	dotPath := triplercDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := triplercRun(t)
	cycles := core.NewCycleCounter()

	triplercWalkSpine(t, graph, run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "consolidate", "implement", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter consolidate\u2192implement: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "consolidate", triplercOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestTripleRC_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the consolidate node emits a label that matches no conditional edge, the
// cascade falls through to the unconditional fallback → close-needs-attention.
func TestTripleRC_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := triplercDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := triplercRun(t)
	cycles := core.NewCycleCounter()

	triplercWalkSpine(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", triplercOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", triplercOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
