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

func trcDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "two-reviewer-consensus.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("trcDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func trcRun(t *testing.T) *core.Run {
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

func trcOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

func trcWalkSpine(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", trcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", trcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_a" {
		t.Fatalf("implement→reviewer_a: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reviewer_a", trcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_b" {
		t.Fatalf("reviewer_a→reviewer_b: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reviewer_b", trcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("reviewer_b→consolidate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// TestTRC_ApproveOnFirstPass exercises the happy path:
// start → implement → reviewer_a → reviewer_b → consolidate(APPROVE) → close (terminal).
func TestTRC_ApproveOnFirstPass(t *testing.T) {
	dotPath := trcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := trcRun(t)
	cycles := core.NewCycleCounter()

	trcWalkSpine(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", trcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestTRC_OneRequestChangesThenApprove exercises the bounded loop:
// spine → consolidate(RC) → implement → spine → consolidate(APPROVE) → close.
func TestTRC_OneRequestChangesThenApprove(t *testing.T) {
	dotPath := trcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := trcRun(t)
	cycles := core.NewCycleCounter()

	trcWalkSpine(t, graph, run, cycles)

	if _, err := cycles.Increment(run.RunID, "consolidate", "implement", nil); err != nil {
		t.Fatalf("pre-fill cycle counter consolidate\u2192implement: %v", err)
	}

	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome("REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("consolidate→implement (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", trcOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_a" {
		t.Fatalf("implement→reviewer_a (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "reviewer_a", trcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_b" {
		t.Fatalf("reviewer_a→reviewer_b (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "reviewer_b", trcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("reviewer_b→consolidate (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "consolidate", trcOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", trcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestTRC_BlockOnFirst exercises:
// spine → consolidate(BLOCK) → close-needs-attention (terminal).
func TestTRC_BlockOnFirst(t *testing.T) {
	dotPath := trcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := trcRun(t)
	cycles := core.NewCycleCounter()

	trcWalkSpine(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", trcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestTRC_CapHitFallback exercises WG-028/EM-043: when the consolidate→implement
// back-edge's traversal_cap (3) is exhausted, the conditional edge is suppressed
// and the cascade reports a cap-hit failure.
func TestTRC_CapHitFallback(t *testing.T) {
	dotPath := trcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := trcRun(t)
	cycles := core.NewCycleCounter()

	trcWalkSpine(t, graph, run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "consolidate", "implement", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter consolidate\u2192implement: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestTRC_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the consolidate node emits a label that matches no conditional edge, the
// cascade falls through to the unconditional fallback → close-needs-attention.
func TestTRC_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := trcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := trcRun(t)
	cycles := core.NewCycleCounter()

	trcWalkSpine(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", trcOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
