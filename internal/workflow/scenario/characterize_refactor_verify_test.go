package scenario_test

// characterize_refactor_verify_test.go — scenario tests for specs/examples/characterize-refactor-verify.dot.
//
// Five named scenarios:
//   1. approve-on-first-pass             → start→characterize→refactor→verify_review(APPROVE)→close (terminal)
//   2. two-REQUEST_CHANGES-then-approve  → 2× loop-back to refactor (not characterize) then APPROVE → close
//   3. BLOCK-on-first                    → verify_review(BLOCK) → close-needs-attention (terminal)
//   4. cap-hit-fallback                  → 3× REQUEST_CHANGES → cap-hit failure
//   5. unrecognized-label-fallback       → unknown label → unconditional fallback → close-needs-attention
//
// Key invariant: the REQUEST_CHANGES back-edge re-enters refactor (NOT characterize).
// The oracle tests committed by characterize are never revisited or overwritten.
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §13 (characterize-refactor-verify topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: crv (per implementer-protocol.md §Helper-prefix discipline).

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func crvOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

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

	// start → characterize
	dec := workflow.DecideNextNode(graph, "start", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// characterize → refactor
	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// refactor → verify_review
	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("refactor→verify_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// verify_review(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("verify_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: two REQUEST_CHANGES then approve ─────────────────────────────

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

	// start → characterize
	dec := workflow.DecideNextNode(graph, "start", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: %+v", dec)
	}

	// characterize → refactor (runs once; oracle committed)
	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: %+v", dec)
	}

	// Loop twice: verify_review(REQUEST_CHANGES) → refactor
	for i := 1; i <= 2; i++ {
		// refactor → verify_review
		dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "verify_review" {
			t.Fatalf("iteration %d refactor→verify_review: %+v", i, dec)
		}

		// Increment cycle counter for the verify_review→refactor back-edge.
		cycles.Increment(run.RunID, "verify_review", "refactor", nil)

		// verify_review(REQUEST_CHANGES) → refactor (back-edge, not characterize)
		dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "refactor" {
			t.Fatalf("iteration %d verify_review→refactor: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	// Third pass: refactor → verify_review → APPROVE → close
	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("final refactor→verify_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final verify_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

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

	// start → characterize
	dec := workflow.DecideNextNode(graph, "start", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: %+v", dec)
	}

	// characterize → refactor
	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: %+v", dec)
	}

	// refactor → verify_review
	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("refactor→verify_review: %+v", dec)
	}

	// verify_review(BLOCK) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("verify_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

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

	// Navigate: start → characterize → refactor → verify_review.
	workflow.DecideNextNode(graph, "start", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "characterize", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "refactor", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of verify_review→refactor.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "verify_review", "refactor", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is
	// suppressed; the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "verify_review", crvOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

	// Navigate: start → characterize → refactor → verify_review.
	dec := workflow.DecideNextNode(graph, "start", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "characterize" {
		t.Fatalf("start→characterize: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "characterize", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "refactor" {
		t.Fatalf("characterize→refactor: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "refactor", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "verify_review" {
		t.Fatalf("refactor→verify_review: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "verify_review", crvOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", crvOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
