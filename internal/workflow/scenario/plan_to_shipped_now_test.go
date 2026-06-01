package scenario_test

// plan_to_shipped_now_test.go — scenario tests for specs/examples/plan-to-shipped-now.dot.
//
// DEMO D1: all-NOW topology — idea → plan → spec → tasking → implement →
// multi-review-consolidate → docs → close.
//
// Scenarios (S2 path obligations from sdlc-workflows SPEC.md):
//   1. happy-path-full-arc       → all APPROVE + load_beads SUCCESS → close (terminal)
//   2. plan-review-block         → plan_review(BLOCK) → close-needs-attention (early exit)
//   3. spec-review-rc-then-approve → spec_review(RC) loop → APPROVE → full arc → close
//   4. load-beads-non-success    → load_beads FAIL → unconditional fallback → close-needs-attention
//   5. consolidate-block         → consolidate(BLOCK) → close-needs-attention (incl. red-build path)
//   6. consolidate-cap-hit       → 3× RC at consolidate → cap-hit failure (cap=3)
//   7. docs-review-approve       → full arc to docs_review(APPROVE) → close
//   8. docs-review-unrecognized  → unrecognized label at docs_review → unconditional fallback
//
// S2 obligations exercised:
//   - Verdict-loop: APPROVE→success; REQUEST_CHANGES→loop→APPROVE; BLOCK→needs-attention; cap-hit→Failed
//   - Consolidation: reviewer spine (rev_correct→rev_design) advances unconditionally; consolidate routes
//   - Commit-gated handoff: outcome.status=='SUCCESS' advances from load_beads; non-SUCCESS→needs-attention
//   - Unrecognized-label fallback via docs_review unconditional edge
//   - Terminal-by-identity: close→SUCCESS, close-needs-attention→needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §D1 (plan-to-shipped-now topology)
//   - docs/sdlc-workflow-corpus.md §Marquee brief discipline (consolidate reviewer spine)
//   - docs/sdlc-workflow-corpus.md §"Changes from _consolidated.md" fix #2 (in-session build gate)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: ptsn (plan-to-shipped-now).

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

func ptsnDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "plan-to-shipped-now.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("ptsnDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func ptsnRun(t *testing.T) *core.Run {
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

func ptsnOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ptsnWalkPlanPhase walks start → draft_plan → plan_review returning after
// plan_review so the caller can branch on the plan verdict.
func ptsnWalkPlanPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("start→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "draft_plan", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsnWalkSpecPhase walks draft_spec → spec_review after plan_review has
// produced APPROVE.
func ptsnWalkSpecPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "draft_spec", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "spec_review" {
		t.Fatalf("draft_spec→spec_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsnWalkTaskingPhase walks decompose → load_beads after spec_review has
// produced APPROVE.
func ptsnWalkTaskingPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "decompose", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("decompose→load_beads: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsnWalkReviewSpine walks the unconditional reviewer spine:
//
//	implement → rev_correct → rev_design → consolidate
//
// It leaves the caller at consolidate so they can exercise the branch.
func ptsnWalkReviewSpine(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "implement", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "rev_correct" {
		t.Fatalf("implement→rev_correct: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "rev_correct", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "rev_design" {
		t.Fatalf("rev_correct→rev_design: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "rev_design", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("rev_design→consolidate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsnWalkToConsolidate walks from start all the way through plan→spec→tasking
// and the review spine, arriving at consolidate ready to branch.
func ptsnWalkToConsolidate(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	// PLAN phase
	ptsnWalkPlanPhase(t, graph, run, cycles)

	// plan_review(APPROVE) → draft_spec
	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// SPEC phase
	ptsnWalkSpecPhase(t, graph, run, cycles)

	// spec_review(APPROVE) → decompose
	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// TASKING phase
	ptsnWalkTaskingPhase(t, graph, run, cycles)

	// load_beads(SUCCESS) → implement
	dec = workflow.DecideNextNode(graph, "load_beads", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("load_beads→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Review spine: implement → rev_correct → rev_design → consolidate
	ptsnWalkReviewSpine(t, graph, run, cycles)
}

// ── Scenario 1: happy-path-full-arc ──────────────────────────────────────────

// TestPTSN_HappyPathFullArc exercises the end-to-end happy path:
// all APPROVE verdicts, load_beads SUCCESS → close (terminal, success).
func TestPTSN_HappyPathFullArc(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	// Walk to consolidate.
	ptsnWalkToConsolidate(t, graph, run, cycles)

	// consolidate(APPROVE) → update_docs
	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// update_docs → docs_review
	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// docs_review(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("docs_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal (SUCCESS classification per terminal-by-identity).
	dec = workflow.DecideNextNode(graph, "close", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: plan-review-block (early exit) ───────────────────────────────

// TestPTSN_PlanReviewBlock exercises early escalation:
// start → draft_plan → plan_review(BLOCK) → close-needs-attention (terminal).
func TestPTSN_PlanReviewBlock(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	ptsnWalkPlanPhase(t, graph, run, cycles)

	// plan_review(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("plan_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: spec-review REQUEST_CHANGES then approve ─────────────────────

// TestPTSN_SpecReviewRCThenApprove exercises the spec revision loop:
// spec_review(RC) → draft_spec → spec_review(APPROVE) → full arc → close.
func TestPTSN_SpecReviewRCThenApprove(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	// PLAN phase → APPROVE → SPEC phase
	ptsnWalkPlanPhase(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: %+v", dec)
	}
	ptsnWalkSpecPhase(t, graph, run, cycles)

	// Increment the cycle counter for the spec_review→draft_spec back-edge.
	cycles.Increment(run.RunID, "spec_review", "draft_spec", nil)

	// spec_review(REQUEST_CHANGES) → draft_spec
	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("spec_review(RC)→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Revise: draft_spec → spec_review (second pass)
	dec = workflow.DecideNextNode(graph, "draft_spec", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "spec_review" {
		t.Fatalf("draft_spec→spec_review (2nd): %+v", dec)
	}

	// spec_review(APPROVE) → decompose
	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review(APPROVE)→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Finish the rest of the arc: tasking → review spine → consolidate(APPROVE) → docs → close.
	ptsnWalkTaskingPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "load_beads", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("load_beads→implement: %+v", dec)
	}

	ptsnWalkReviewSpine(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("docs_review→close: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "close", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: load_beads non-SUCCESS → unconditional fallback ───────────────

// TestPTSN_LoadBeadsNonSuccess exercises the commit-gated handoff:
// load_beads FAIL → outcome.status != 'SUCCESS' → unconditional fallback →
// close-needs-attention.
func TestPTSN_LoadBeadsNonSuccess(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	// Walk to load_beads.
	ptsnWalkPlanPhase(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: %+v", dec)
	}
	ptsnWalkSpecPhase(t, graph, run, cycles)
	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: %+v", dec)
	}
	ptsnWalkTaskingPhase(t, graph, run, cycles)

	// load_beads returns FAIL: the SUCCESS condition does not match;
	// unconditional fallback fires → close-needs-attention.
	dec = workflow.DecideNextNode(graph, "load_beads", ptsnOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("load_beads FAIL fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("load_beads FAIL fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 5: consolidate BLOCK → close-needs-attention ────────────────────

// TestPTSN_ConsolidateBlock exercises:
// full arc to consolidate → consolidate(BLOCK) → close-needs-attention.
// This also covers the in-session red-build-gate path (review fix #2): a red
// build causes the consolidate reviewer to emit BLOCK, which routes here.
func TestPTSN_ConsolidateBlock(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	ptsnWalkToConsolidate(t, graph, run, cycles)

	// consolidate(BLOCK) → close-needs-attention (incl. red-build case).
	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 6: consolidate cap-hit (cap=3) ───────────────────────────────────

// TestPTSN_ConsolidateCapHit exercises WG-028/EM-043:
// when the consolidate→implement back-edge's traversal_cap (3) is exhausted,
// the conditional edge is suppressed and the cascade reports a cap-hit failure.
func TestPTSN_ConsolidateCapHit(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	ptsnWalkToConsolidate(t, graph, run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of consolidate→implement
	// (the cap declared in the DOT is 3).
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "consolidate", "implement", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is suppressed;
	// cascade reports cap-hit failure.
	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// ── Scenario 7: docs_review APPROVE → close ───────────────────────────────────

// TestPTSN_DocsReviewApprove exercises the docs phase happy path in isolation:
// update_docs → docs_review(APPROVE) → close (terminal, success).
func TestPTSN_DocsReviewApprove(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	// Walk to consolidate, then APPROVE to enter the docs phase.
	ptsnWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: %+v", dec)
	}

	// docs_review(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("docs_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal.
	dec = workflow.DecideNextNode(graph, "close", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 8: docs_review unrecognized label → unconditional fallback ────────

// TestPTSN_DocsReviewUnrecognizedLabel exercises the WG-011 unconditional fallback
// at docs_review: an unrecognized label (e.g. from no-progress detection) falls
// through to close-needs-attention.
func TestPTSN_DocsReviewUnrecognizedLabel(t *testing.T) {
	dotPath := ptsnDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := ptsnRun(t)
	cycles := core.NewCycleCounter()

	// Walk to the docs phase.
	ptsnWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: %+v", dec)
	}

	// docs_review emits an unrecognized label: no conditional edge matches;
	// unconditional fallback fires → close-needs-attention.
	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
