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

func crvDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "characterize-refactor-verify.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("crvDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func crvRun(t *testing.T) *core.Run {
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

func crvOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestCRV_ApproveOnFirstPass exercises the happy path:
// start → characterize → refactor → verify_review(APPROVE) → close (terminal).
func TestCRV_ApproveOnFirstPass(t *testing.T) {
	dotPath := crvDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := crvRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("refactor→verify_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("verify_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", crvOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestCRV_TwoRequestChangesThenApprove exercises the bounded loop:
// start → characterize → refactor → verify_review(RC) → refactor →
// verify_review(RC) → refactor → verify_review(APPROVE) → close.
//
// The back-edge re-enters refactor (NOT characterize) — the oracle commit
// from characterize is never revisited in the loop.
func TestCRV_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := crvDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := crvRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: %+v", dec)
	}

	for i := 1; i <= 2; i++ {
		dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "verify_review" {
			t.Fatalf("iteration %d refactor→verify_review: %+v", i, dec)
		}

		if _, err := cycles.Increment(run.RunID, "verify_review", "refactor", nil); err != nil {
			t.Fatalf("pre-fill cycle counter verify_review\u2192refactor: %v", err)
		}

		dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome("REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "refactor" {
			t.Fatalf("iteration %d verify_review→refactor: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("final refactor→verify_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final verify_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", crvOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestCRV_BlockOnFirst exercises:
// start → characterize → refactor → verify_review(BLOCK) → close-needs-attention (terminal).
func TestCRV_BlockOnFirst(t *testing.T) {
	dotPath := crvDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := crvRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("refactor→verify_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("verify_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", crvOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestCRV_CapHitFallback exercises WG-028/EM-043: when the verify_review→refactor
// back-edge's traversal_cap (3) is exhausted, the conditional edge is
// suppressed and the cascade reports a cap-hit failure.
func TestCRV_CapHitFallback(t *testing.T) {
	dotPath := crvDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := crvRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", crvOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "characterize", crvOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "refactor", crvOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "verify_review", "refactor", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter verify_review\u2192refactor: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "verify_review", crvOutcome("REQUEST_CHANGES"), run, cycles)
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

// TestCRV_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when verify_review emits a label matching no conditional edge, the cascade
// falls through to the unconditional fallback → close-needs-attention.
func TestCRV_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := crvDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := crvRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("refactor→verify_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", crvOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
