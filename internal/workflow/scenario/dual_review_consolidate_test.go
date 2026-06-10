package scenario_test

// dual_review_consolidate_test.go — scenario tests for specs/examples/dual-review-consolidate.dot.
//
// Five named scenarios:
//   1. approve-on-first-pass         → spine runs once; consolidate(APPROVE) → close (terminal, success)
//   2. one-REQUEST_CHANGES-then-approve → 1× loop-back; second pass APPROVE → close
//   3. BLOCK-on-first                → consolidate(BLOCK) → close-needs-attention (terminal)
//   4. cap-hit-fallback              → 2× REQUEST_CHANGES → cap-hit failure (cap=2)
//   5. unrecognized-label-fallback   → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §3 (dual-review-consolidate topology)
//   - docs/sdlc-workflow-corpus.md §Marquee brief discipline (reviewer-commit channel)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: drc (per implementer-protocol.md §Helper-prefix discipline).

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

// ── fixtures ─────────────────────────────────────────────────────────────────

func drcDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "dual-review-consolidate.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("drcDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func drcRun(t *testing.T) *core.Run {
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

func drcOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// drcWalkSpine walks the unconditional spine of the dual-review-consolidate graph:
//
//	start → implement → review_correctness → review_design
//
// returning after review_design so the caller can exercise the consolidate branch.
func drcWalkSpine(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_correctness" {
		t.Fatalf("implement→review_correctness: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review_correctness", drcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_design" {
		t.Fatalf("review_correctness→review_design: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review_design", drcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("review_design→consolidate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

// TestDRC_ApproveOnFirstPass exercises the happy path:
// start → implement → review_correctness → review_design → consolidate(APPROVE) → close (terminal).
func TestDRC_ApproveOnFirstPass(t *testing.T) {
	dotPath := drcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drcRun(t)
	cycles := core.NewCycleCounter()

	drcWalkSpine(t, graph, run, cycles)

	// consolidate(APPROVE) → close
	dec := workflow.DecideNextNode(graph, "consolidate", drcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: one REQUEST_CHANGES then approve ──────────────────────────────

// TestDRC_OneRequestChangesThenApprove exercises the bounded loop:
// spine → consolidate(RC) → implement → spine → consolidate(APPROVE) → close.
func TestDRC_OneRequestChangesThenApprove(t *testing.T) {
	dotPath := drcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drcRun(t)
	cycles := core.NewCycleCounter()

	// First pass through the spine.
	drcWalkSpine(t, graph, run, cycles)

	// Increment the cycle counter for the consolidate→implement back-edge.
	cycles.Increment(run.RunID, "consolidate", "implement", nil)

	// consolidate(REQUEST_CHANGES) → implement
	dec := workflow.DecideNextNode(graph, "consolidate", drcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("consolidate→implement (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Second pass through the spine (implement → review_correctness → review_design → consolidate).
	dec = workflow.DecideNextNode(graph, "implement", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_correctness" {
		t.Fatalf("implement→review_correctness (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "review_correctness", drcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_design" {
		t.Fatalf("review_correctness→review_design (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "review_design", drcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("review_design→consolidate (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// consolidate(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "consolidate", drcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

// TestDRC_BlockOnFirst exercises:
// spine → consolidate(BLOCK) → close-needs-attention (terminal).
func TestDRC_BlockOnFirst(t *testing.T) {
	dotPath := drcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drcRun(t)
	cycles := core.NewCycleCounter()

	drcWalkSpine(t, graph, run, cycles)

	// consolidate(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "consolidate", drcOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

// TestDRC_CapHitFallback exercises WG-028/EM-043: when the consolidate→implement
// back-edge's traversal_cap (2) is exhausted, the conditional edge is suppressed
// and the cascade reports a cap-hit failure.
func TestDRC_CapHitFallback(t *testing.T) {
	dotPath := drcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drcRun(t)
	cycles := core.NewCycleCounter()

	drcWalkSpine(t, graph, run, cycles)

	// Pre-fill cycle counter: simulate 2 prior traversals of consolidate→implement
	// (the cap declared in the DOT is 2).
	cap := 2
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "consolidate", "implement", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is suppressed;
	// the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "consolidate", drcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// TestDRC_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when the consolidate node emits a label that matches no conditional edge, the
// cascade falls through to the unconditional fallback → close-needs-attention.
func TestDRC_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := drcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := drcRun(t)
	cycles := core.NewCycleCounter()

	drcWalkSpine(t, graph, run, cycles)

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec := workflow.DecideNextNode(graph, "consolidate", drcOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", drcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
