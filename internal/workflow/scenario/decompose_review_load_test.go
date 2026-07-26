package scenario_test

// decompose_review_load_test.go — scenario tests for specs/examples/decompose-review-load.dot.
//
// Six named scenarios:
//   1. approve-on-first-pass           → start→decompose→decomp_review(APPROVE)→load_beads(SUCCESS)→close (terminal)
//   2. two-REQUEST_CHANGES-then-approve → 2× loop-back then APPROVE → load_beads(SUCCESS) → close
//   3. BLOCK-on-first                  → decomp_review(BLOCK) → close-needs-attention (terminal)
//   4. cap-hit-fallback                → 3× REQUEST_CHANGES → cap-hit failure
//   5. unrecognized-label-fallback     → unknown label → unconditional fallback → close-needs-attention
//   6. load-beads-failure              → load_beads non-SUCCESS → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §9 (decompose-review-load topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: drl (per implementer-protocol.md §Helper-prefix discipline).

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

func drlDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "decompose-review-load.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("drlDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func drlRun(t *testing.T) *core.Run {
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

func drlOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

// TestDRL_ApproveOnFirstPass exercises the happy path:
// start → decompose → decomp_review(APPROVE) → load_beads(SUCCESS) → close (terminal).
func TestDRL_ApproveOnFirstPass(t *testing.T) {
	dotPath := drlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drlRun(t)
	cycles := core.NewCycleCounter()

	// start → decompose
	dec := workflow.DecideNextNode(graph, "start", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("start→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// decompose → decomp_review
	dec = workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decomp_review" {
		t.Fatalf("decompose→decomp_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// decomp_review(APPROVE) → load_beads
	dec = workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("decomp_review→load_beads: Advance=%v NextNodeID=%q, want load_beads", dec.Advance, dec.NextNodeID)
	}

	// load_beads(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "load_beads", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("load_beads→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: two REQUEST_CHANGES then approve ─────────────────────────────

// TestDRL_TwoRequestChangesThenApprove exercises the bounded loop:
// start → decompose → decomp_review(RC) → decompose → decomp_review(RC) →
// decompose → decomp_review(APPROVE) → load_beads(SUCCESS) → close.
func TestDRL_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := drlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drlRun(t)
	cycles := core.NewCycleCounter()

	// start → decompose
	dec := workflow.DecideNextNode(graph, "start", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("start→decompose: %+v", dec)
	}

	// Loop twice: decomp_review(REQUEST_CHANGES) → decompose
	for i := 1; i <= 2; i++ {
		// decompose → decomp_review
		dec = workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "decomp_review" {
			t.Fatalf("iteration %d decompose→decomp_review: %+v", i, dec)
		}

		// Increment cycle counter for the decomp_review→decompose back-edge.
		if _, err := cycles.Increment(run.RunID, "decomp_review", "decompose", nil); err != nil {
			t.Fatalf("pre-fill cycle counter decomp_review\u2192decompose: %v", err)
		}

		// decomp_review(REQUEST_CHANGES) → decompose
		dec = workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "decompose" {
			t.Fatalf("iteration %d decomp_review→decompose: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	// Third pass: decompose → decomp_review → APPROVE → load_beads → close
	dec = workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decomp_review" {
		t.Fatalf("final decompose→decomp_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("final decomp_review→load_beads: Advance=%v NextNodeID=%q, want load_beads", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "load_beads", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("load_beads→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

// TestDRL_BlockOnFirst exercises:
// start → decompose → decomp_review(BLOCK) → close-needs-attention (terminal).
func TestDRL_BlockOnFirst(t *testing.T) {
	dotPath := drlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drlRun(t)
	cycles := core.NewCycleCounter()

	// start → decompose
	dec := workflow.DecideNextNode(graph, "start", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("start→decompose: %+v", dec)
	}

	// decompose → decomp_review
	dec = workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decomp_review" {
		t.Fatalf("decompose→decomp_review: %+v", dec)
	}

	// decomp_review(BLOCK) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("decomp_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

// TestDRL_CapHitFallback exercises WG-028/EM-043: when the decomp_review→decompose
// back-edge's traversal_cap (3) is exhausted, the conditional edge is suppressed
// and the cascade reports a cap-hit failure.
func TestDRL_CapHitFallback(t *testing.T) {
	dotPath := drlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drlRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → decompose → decomp_review.
	workflow.DecideNextNode(graph, "start", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of decomp_review→decompose.
	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "decomp_review", "decompose", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter decomp_review\u2192decompose: %v", err)
		}
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is
	// suppressed; the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// TestDRL_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the reviewer emits a label that matches no conditional edge, the cascade
// falls through to the unconditional fallback → close-needs-attention.
func TestDRL_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := drlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drlRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → decompose → decomp_review.
	dec := workflow.DecideNextNode(graph, "start", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("start→decompose: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decomp_review" {
		t.Fatalf("decompose→decomp_review: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 6: load_beads failure → unconditional fallback ──────────────────

// TestDRL_LoadBeadsFailure exercises the load_beads unconditional fallback:
// when load_beads returns a non-SUCCESS status (commit absent, br error),
// the cascade falls through to the unconditional fallback → close-needs-attention.
func TestDRL_LoadBeadsFailure(t *testing.T) {
	dotPath := drlDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drlRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → decompose → decomp_review(APPROVE) → load_beads.
	dec := workflow.DecideNextNode(graph, "start", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("start→decompose: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "decompose", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decomp_review" {
		t.Fatalf("decompose→decomp_review: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "decomp_review", drlOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("decomp_review→load_beads: %+v", dec)
	}

	// load_beads returns FAILED: no SUCCESS edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "load_beads", drlOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("load_beads failure fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("load_beads failure fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", drlOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
