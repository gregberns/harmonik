package scenario_test

// plan_review_finalize_test.go — scenario tests for specs/examples/plan-review-finalize.dot.
//
// Six named scenarios:
//   1. approve-on-first-pass           → start→draft_plan→plan_review(APPROVE)→finalize_plan→plan-approved (terminal)
//   2. two-REQUEST_CHANGES-then-approve → 2× loop-back then APPROVE → finalize_plan → plan-approved
//   3. BLOCK-on-first                  → plan_review(BLOCK) → plan-needs-attention (terminal)
//   4. cap-hit-fallback                → 3× REQUEST_CHANGES → cap-hit failure
//   5. unrecognized-label-fallback     → unknown label → unconditional fallback → plan-needs-attention
//   6. finalize-to-terminal-by-identity → finalize_plan→plan-approved is SUCCESS by terminal identity (WG-021/hk-z03e8)
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §6 (plan-review-finalize topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-021..WG-023 (terminal-by-identity classification)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: prf (per implementer-protocol.md §Helper-prefix discipline).

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func prfOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

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

	// start → draft_plan
	dec := workflow.DecideNextNode(graph, "start", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// draft_plan → plan_review
	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// plan_review(APPROVE) → finalize_plan
	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "finalize_plan" {
		t.Fatalf("plan_review→finalize_plan: Advance=%v NextNodeID=%q, want finalize_plan", dec.Advance, dec.NextNodeID)
	}

	// finalize_plan → plan-approved
	dec = workflow.DecideNextNode(graph, "finalize_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("finalize_plan→plan-approved: Advance=%v NextNodeID=%q, want plan-approved", dec.Advance, dec.NextNodeID)
	}

	// plan-approved is terminal
	dec = workflow.DecideNextNode(graph, "plan-approved", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: two REQUEST_CHANGES then approve ─────────────────────────────

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

	// start → draft_plan
	dec := workflow.DecideNextNode(graph, "start", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}

	// Loop twice: plan_review(REQUEST_CHANGES) → draft_plan
	for i := 1; i <= 2; i++ {
		// draft_plan → plan_review
		dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "plan_review" {
			t.Fatalf("iteration %d draft_plan→plan_review: %+v", i, dec)
		}

		// Increment cycle counter for the plan_review→draft_plan back-edge.
		cycles.Increment(run.RunID, "plan_review", "draft_plan", nil)

		// plan_review(REQUEST_CHANGES) → draft_plan
		dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "draft_plan" {
			t.Fatalf("iteration %d plan_review→draft_plan: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	// Third pass: draft_plan → plan_review → APPROVE → finalize_plan → plan-approved
	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("final draft_plan→plan_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "finalize_plan" {
		t.Fatalf("final plan_review→finalize_plan: Advance=%v NextNodeID=%q, want finalize_plan", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "finalize_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("finalize_plan→plan-approved: Advance=%v NextNodeID=%q, want plan-approved", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "plan-approved", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

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

	// start → draft_plan
	dec := workflow.DecideNextNode(graph, "start", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}

	// draft_plan → plan_review
	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: %+v", dec)
	}

	// plan_review(BLOCK) → plan-needs-attention
	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-needs-attention" {
		t.Fatalf("plan_review→plan-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// plan-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "plan-needs-attention", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

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

	// Navigate: start → draft_plan → plan_review.
	workflow.DecideNextNode(graph, "start", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "draft_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of plan_review→draft_plan.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "plan_review", "draft_plan", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is
	// suppressed; the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "plan_review", prfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

	// Navigate: start → draft_plan → plan_review.
	dec := workflow.DecideNextNode(graph, "start", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "draft_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "plan_review", prfOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "plan-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "plan-needs-attention")
	}

	// plan-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "plan-needs-attention", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 6: finalize-to-terminal-by-identity (WG-021 / hk-z03e8) ─────────

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

	// Navigate to finalize_plan directly (simulating APPROVE path already taken).
	// finalize_plan → plan-approved must advance (unconditional edge).
	dec := workflow.DecideNextNode(graph, "finalize_plan", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan-approved" {
		t.Fatalf("finalize_plan→plan-approved: Advance=%v NextNodeID=%q, want plan-approved",
			dec.Advance, dec.NextNodeID)
	}

	// plan-approved must be classified as terminal (and therefore SUCCESS).
	dec = workflow.DecideNextNode(graph, "plan-approved", prfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("plan-approved: IsTerminal=%v, want true (terminal-by-identity, WG-021)", dec.IsTerminal)
	}
	if dec.Failed {
		t.Fatalf("plan-approved: Failed=%v, want false — terminal-by-identity must be SUCCESS", dec.Failed)
	}
}
