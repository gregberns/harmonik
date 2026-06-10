package scenario_test

// implement_review_fix_test.go — scenario tests for specs/examples/implement-review-fix.dot.
//
// Five named scenarios:
//   1. approve-on-first-pass         → start→implement→review(APPROVE)→close (terminal, success)
//   2. two-REQUEST_CHANGES-then-approve → 2× loop-back then APPROVE → close
//   3. BLOCK-on-first                → review(BLOCK) → close-needs-attention (terminal)
//   4. cap-hit-fallback              → 3× REQUEST_CHANGES → cap-hit failure
//   5. unrecognized-label-fallback   → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §1 (implement-review-fix topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: irf (per implementer-protocol.md §Helper-prefix discipline).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func irfOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

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

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implement → review
	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("implement→review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// review(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "review", irfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: two REQUEST_CHANGES then approve ─────────────────────────────

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

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	// Loop twice: review(REQUEST_CHANGES) → implement
	for i := 1; i <= 2; i++ {
		// implement → review
		dec = workflow.DecideNextNode(graph, "implement", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "review" {
			t.Fatalf("iteration %d implement→review: %+v", i, dec)
		}

		// Increment cycle counter for the review→implement back-edge.
		cycles.Increment(run.RunID, "review", "implement", nil)

		// review(REQUEST_CHANGES) → implement
		dec = workflow.DecideNextNode(graph, "review", irfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "implement" {
			t.Fatalf("iteration %d review→implement: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	// Third pass: implement → review → APPROVE → close
	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("final implement→review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review", irfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

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

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	// implement → review
	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("implement→review: %+v", dec)
	}

	// review(BLOCK) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "review", irfOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

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

	// Navigate: start → implement → review.
	workflow.DecideNextNode(graph, "start", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of review→implement.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "review", "implement", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is
	// suppressed; the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "review", irfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// ── Scenario 5: unrecognized label → unconditional fallback ──────────────────

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

	// Navigate: start → implement → review.
	dec := workflow.DecideNextNode(graph, "start", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "implement", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("implement→review: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "review", irfOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", irfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
