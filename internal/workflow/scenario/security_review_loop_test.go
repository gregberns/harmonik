package scenario_test

// security_review_loop_test.go — scenario tests for specs/examples/security-review-loop.dot.
//
// Five named scenarios:
//   1. approve-on-first-pass          → start→implement→security_review(APPROVE)→close (terminal, success)
//   2. two-REQUEST_CHANGES-then-approve → 2× loop-back then APPROVE → close
//   3. BLOCK-on-first                 → security_review(BLOCK) → close-needs-attention (terminal)
//   4. cap-hit-fallback               → 3× REQUEST_CHANGES → cap-hit failure
//   5. unrecognized-label-fallback    → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §14 (security-review-loop topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: srl (per implementer-protocol.md §Helper-prefix discipline).

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func srlOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

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

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implement → security_review
	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("implement→security_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// security_review(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("security_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: two REQUEST_CHANGES then approve ─────────────────────────────

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

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	// Loop twice: security_review(REQUEST_CHANGES) → implement
	for i := 1; i <= 2; i++ {
		// implement → security_review
		dec = workflow.DecideNextNode(graph, "implement", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "security_review" {
			t.Fatalf("iteration %d implement→security_review: %+v", i, dec)
		}

		// Increment cycle counter for the security_review→implement back-edge.
		cycles.Increment(run.RunID, "security_review", "implement", nil)

		// security_review(REQUEST_CHANGES) → implement
		dec = workflow.DecideNextNode(graph, "security_review", srlOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "implement" {
			t.Fatalf("iteration %d security_review→implement: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	// Third pass: implement → security_review → APPROVE → close
	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("final implement→security_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final security_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

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

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}

	// implement → security_review
	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("implement→security_review: %+v", dec)
	}

	// security_review(BLOCK) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("security_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

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

	// Navigate: start → implement → security_review.
	workflow.DecideNextNode(graph, "start", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of security_review→implement.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "security_review", "implement", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is
	// suppressed; the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "security_review", srlOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

	// Navigate: start → implement → security_review.
	dec := workflow.DecideNextNode(graph, "start", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "implement", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "security_review" {
		t.Fatalf("implement→security_review: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "security_review", srlOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", srlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
