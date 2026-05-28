package scenario_test

// review_route_by_failure_class_test.go — scenario tests for
// specs/examples/review-route-by-failure-class.dot.
//
// Eight named scenarios:
//   1. success-path-approve          → start→implementer(SUCCESS)→reviewer(APPROVE)→close (terminal)
//   2. transient-retry-then-success  → implementer(transient) self-loops once, then SUCCESS→reviewer(APPROVE)→close
//   3. transient-cap-hit             → 3× transient → cap-hit failure (compilation_loop)
//   4. structural-needs-attention    → implementer(structural) → close-needs-attention (terminal)
//   5. deterministic-needs-attention → implementer(deterministic) → close-needs-attention
//   6. canceled-needs-attention      → implementer(canceled) → close-needs-attention
//   7. budget-exhausted-needs-attention → implementer(budget_exhausted) → close-needs-attention
//   8. compilation-loop-needs-attention → implementer(compilation_loop, handler-emitted) → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §12 (review-route-by-failure-class topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Live-run note: agents cannot be reliably forced to emit a given failure_class on
// demand. Scenarios 3–8 drive the non-transient branches with synthetic outcomes;
// live-branch coverage is gated on hk-1xsyu (stub handler).
//
// Helper prefix: rfc (per implementer-protocol.md §Helper-prefix discipline).

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

func rfcDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "review-route-by-failure-class.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("rfcDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func rfcRun(t *testing.T) *core.Run {
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

// rfcOutcome builds a SUCCESS/non-FAIL outcome with an optional preferred_label.
func rfcOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// rfcOutcomeFC builds a FAIL outcome carrying the given failure_class.
// Status must be FAIL for FailureClass to be present per EM-005c.
func rfcOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

// ── Scenario 1: success-path-approve ─────────────────────────────────────────

// TestRFC_SuccessPathApprove exercises the happy path:
// start → implementer(SUCCESS, no failure_class) → reviewer(APPROVE) → close (terminal).
// SUCCESS carries no failure_class — all failure-class conditions miss and the cascade
// falls through to the unconditional implementer→reviewer handoff.
func TestRFC_SuccessPathApprove(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implementer(SUCCESS) → reviewer (failure-class conditions all miss; falls through)
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reviewer(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "reviewer", rfcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: transient-retry-then-success ─────────────────────────────────

// TestRFC_TransientRetryThenSuccess exercises the transient self-loop:
// start → implementer(transient) → implementer (retry) → implementer(SUCCESS) →
// reviewer(APPROVE) → close (terminal).
func TestRFC_TransientRetryThenSuccess(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implementer(transient) → implementer (self-loop, first retry; cap=3, count=0 → OK)
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("implementer(transient)→implementer: Advance=%v NextNodeID=%q, want implementer",
			dec.Advance, dec.NextNodeID)
	}
	// Record the self-loop traversal so the cycle counter reflects the retry.
	cycles.Increment(run.RunID, "implementer", "implementer", nil) //nolint:errcheck

	// implementer(SUCCESS) → reviewer (failure-class conditions miss; falls through)
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer(SUCCESS)→reviewer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reviewer(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "reviewer", rfcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: transient-cap-hit ────────────────────────────────────────────

// TestRFC_TransientCapHit exercises WG-028/EM-043 on the implementer self-loop:
// after 3 transient traversals the self-loop's traversal_cap is exhausted and the
// cascade reports cap_hit (FailureClass=compilation_loop).
func TestRFC_TransientCapHit(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → implementer.
	workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of the implementer self-loop.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "implementer", "implementer", &cap) //nolint:errcheck
	}

	// With the traversal cap exhausted, the transient self-loop is suppressed;
	// the cascade reports a cap-hit failure (EM-043).
	dec := workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Failed {
		t.Fatalf("expected Failed=true on transient cap-hit, got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("expected CompletionReason=cap_hit, got %q (%+v)", dec.CompletionReason, dec)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}

// ── Scenario 4: structural → close-needs-attention ───────────────────────────

// TestRFC_StructuralNeedsAttention exercises:
// start → implementer(structural) → close-needs-attention (terminal).
func TestRFC_StructuralNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	// implementer(structural) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassStructural), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(structural)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 5: deterministic → close-needs-attention ────────────────────────

// TestRFC_DeterministicNeedsAttention exercises:
// start → implementer(deterministic) → close-needs-attention (terminal).
func TestRFC_DeterministicNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	// implementer(deterministic) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(deterministic)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 6: canceled → close-needs-attention ─────────────────────────────

// TestRFC_CanceledNeedsAttention exercises:
// start → implementer(canceled) → close-needs-attention (terminal).
func TestRFC_CanceledNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	// implementer(canceled) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassCanceled), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(canceled)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 7: budget_exhausted → close-needs-attention ─────────────────────

// TestRFC_BudgetExhaustedNeedsAttention exercises:
// start → implementer(budget_exhausted) → close-needs-attention (terminal).
func TestRFC_BudgetExhaustedNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	// implementer(budget_exhausted) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassBudgetExhausted), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(budget_exhausted)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 8: compilation_loop (handler-emitted) → close-needs-attention ───

// TestRFC_CompilationLoopNeedsAttention exercises the handler-emitted
// compilation_loop failure class:
// start → implementer(compilation_loop) → close-needs-attention (terminal).
// This is distinct from the cap-hit path (scenario 3): here the handler itself
// emits the compilation_loop failure class; the cascade routes it via the explicit
// condition edge rather than the traversal-cap mechanism.
func TestRFC_CompilationLoopNeedsAttention(t *testing.T) {
	dotPath := rfcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rfcRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	// implementer(compilation_loop) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "implementer", rfcOutcomeFC(core.FailureClassCompilationLoop), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("implementer(compilation_loop)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rfcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
