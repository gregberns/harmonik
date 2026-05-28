package scenario_test

// two_reviewer_consensus_test.go — scenario tests for specs/examples/two-reviewer-consensus.dot.
//
// Five named scenarios:
//   1. approve-on-first-pass         → spine runs once; consolidate(APPROVE) → close (terminal, success)
//   2. one-REQUEST_CHANGES-then-approve → 1× loop-back; second pass APPROVE → close
//   3. BLOCK-on-first                → consolidate(BLOCK) → close-needs-attention (terminal)
//   4. cap-hit-fallback              → 3× REQUEST_CHANGES → cap-hit failure (cap=3)
//   5. unrecognized-label-fallback   → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §4 (two-reviewer-consensus topology)
//   - docs/sdlc-workflow-corpus.md §Marquee brief discipline (reviewer-commit channel)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: trc (per implementer-protocol.md §Helper-prefix discipline).

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func trcOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// trcWalkSpine walks the unconditional spine of the two-reviewer-consensus graph:
//   start → implement → reviewer_a → reviewer_b
// returning after reviewer_b so the caller can exercise the consolidate branch.
func trcWalkSpine(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "implement", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_a" {
		t.Fatalf("implement→reviewer_a: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reviewer_a", trcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_b" {
		t.Fatalf("reviewer_a→reviewer_b: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "reviewer_b", trcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("reviewer_b→consolidate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

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

	// consolidate(APPROVE) → close
	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: one REQUEST_CHANGES then approve ──────────────────────────────

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

	// First pass through the spine.
	trcWalkSpine(t, graph, run, cycles)

	// Increment the cycle counter for the consolidate→implement back-edge.
	cycles.Increment(run.RunID, "consolidate", "implement", nil)

	// consolidate(REQUEST_CHANGES) → implement
	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("consolidate→implement (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Second pass through the spine.
	dec = workflow.DecideNextNode(graph, "implement", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_a" {
		t.Fatalf("implement→reviewer_a (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "reviewer_a", trcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer_b" {
		t.Fatalf("reviewer_a→reviewer_b (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
	dec = workflow.DecideNextNode(graph, "reviewer_b", trcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("reviewer_b→consolidate (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// consolidate(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "consolidate", trcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("consolidate→close (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ───────────────────────────────────────────────

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

	// consolidate(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

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

	// Pre-fill cycle counter: simulate 3 prior traversals of consolidate→implement
	// (the cap declared in the DOT is 3).
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "consolidate", "implement", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is suppressed;
	// the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec := workflow.DecideNextNode(graph, "consolidate", trcOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", trcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
