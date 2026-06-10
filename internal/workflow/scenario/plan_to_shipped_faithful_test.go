package scenario_test

// plan_to_shipped_faithful_test.go — scenario tests for
// specs/examples/plan-to-shipped-faithful.dot.
//
// DEMO D2: the north-star post-parity fixture.
// idea → frame_problem → plan → spec → tasking (load_beads+cycle_check) →
// implement → multi-review-consolidate → green_build → close.
//
// Key differences from D1 (plan-to-shipped-now):
//   - NEW frame_problem node (non-committing analysis before draft_plan — hk-69asi)
//   - load_beads is a non-agentic shell node (tool proxy for hk-l8rpd)
//   - NEW cycle_check shell node between load_beads and implement
//   - green_build shell node replaces the in-session build gate + docs phase
//   - Reviewer nodes carry non_committing="true" (v1 warning, not dispatched)
//   - Per-node prompt= on implementer nodes (hk-sdnzj, live for implementers)
//
// Scenarios (S2 path obligations):
//   1. happy-path-full-arc         → all green → close (terminal)
//   2. plan-review-block           → plan_review(BLOCK) → close-needs-attention
//   3. plan-review-rc-then-approve → plan_review(RC) loop → APPROVE → arc → close
//   4. load-beads-non-success      → load_beads FAIL → close-needs-attention
//   5. cycle-check-non-success     → load_beads OK → cycle_check FAIL → close-needs-attention
//   6. consolidate-block           → consolidate(BLOCK) → close-needs-attention
//   7. consolidate-cap-hit         → 3× RC at consolidate → cap-hit failure (cap=3)
//   8. green-build-deterministic   → green_build FAIL+deterministic → implement loop
//   9. green-build-other-failure   → green_build FAIL+structural → close-needs-attention
//
// S2 obligations exercised:
//   - Non-committing entry: frame_problem always advances (unconditional exit)
//   - Verdict-loop: APPROVE→success; REQUEST_CHANGES→loop→APPROVE; BLOCK→needs-attention; cap-hit→Failed
//   - Tool-node commit-gate: outcome.status=='SUCCESS' advances from load_beads/cycle_check
//   - Compound condition: outcome.status=='FAIL' && outcome.failure_class=='deterministic' on green_build
//   - Consolidation: reviewer spine (rev_correct→rev_tests) advances unconditionally
//   - Terminal-by-identity: close→SUCCESS; close-needs-attention→needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §D2 (plan-to-shipped-faithful topology)
//   - docs/sdlc-workflow-corpus.md §Marquee brief discipline
//   - specs/workflow-graph.md WG-010 (5-step cascade)
//   - specs/workflow-graph.md WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-043 (traversal-cap enforcement)
//   - specs/examples/authoring-notes.md §1 (non_committing)
//
// Helper prefix: ptsf (plan-to-shipped-faithful).

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

func ptsfDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "plan-to-shipped-faithful.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("ptsfDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func ptsfRun(t *testing.T) *core.Run {
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

func ptsfOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ptsfOutcomeFC builds a FAIL outcome carrying failure_class.
// Used for compound-condition edges (outcome.status=='FAIL' && outcome.failure_class==...).
func ptsfOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

// ptsfLoadGraph loads and validates the D2 fixture. It also verifies the DOT
// parses with no errors (permissive class= warnings are allowed).
func ptsfLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	dotPath := ptsfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

// ── phase helpers ─────────────────────────────────────────────────────────────

// ptsfWalkEntry walks start → frame_problem → draft_plan.
// frame_problem is non-committing: unconditional advance after its exit.
func ptsfWalkEntry(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	// start → frame_problem
	dec := workflow.DecideNextNode(graph, "start", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "frame_problem" {
		t.Fatalf("start→frame_problem: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// frame_problem → draft_plan (unconditional — non-committing node always advances)
	dec = workflow.DecideNextNode(graph, "frame_problem", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("frame_problem→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsfWalkPlanPhase walks draft_plan → plan_review (leaving caller at plan_review branch).
func ptsfWalkPlanPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "draft_plan", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsfWalkSpecPhase walks draft_spec → spec_review (leaving caller at spec_review branch).
func ptsfWalkSpecPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "draft_spec", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "spec_review" {
		t.Fatalf("draft_spec→spec_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsfWalkTaskingPhase walks decompose → load_beads (leaving caller at load_beads branch).
func ptsfWalkTaskingPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "decompose", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("decompose→load_beads: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsfWalkReviewSpine walks the unconditional reviewer spine:
// implement → rev_correct → rev_tests → consolidate.
// Leaves caller at consolidate so they can exercise the branch.
func ptsfWalkReviewSpine(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "implement", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "rev_correct" {
		t.Fatalf("implement→rev_correct: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "rev_correct", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "rev_tests" {
		t.Fatalf("rev_correct→rev_tests: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "rev_tests", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "consolidate" {
		t.Fatalf("rev_tests→consolidate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ptsfWalkToConsolidate walks start → frame_problem → plan → spec → tasking
// (load_beads SUCCESS → cycle_check SUCCESS → implement) → review spine → consolidate.
func ptsfWalkToConsolidate(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	// Entry: start → frame_problem → draft_plan
	ptsfWalkEntry(t, graph, run, cycles)

	// PLAN phase
	ptsfWalkPlanPhase(t, graph, run, cycles)

	// plan_review(APPROVE) → draft_spec
	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// SPEC phase
	ptsfWalkSpecPhase(t, graph, run, cycles)

	// spec_review(APPROVE) → decompose
	dec = workflow.DecideNextNode(graph, "spec_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// TASKING phase: decompose → load_beads
	ptsfWalkTaskingPhase(t, graph, run, cycles)

	// load_beads(SUCCESS) → cycle_check
	dec = workflow.DecideNextNode(graph, "load_beads", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("load_beads→cycle_check: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// cycle_check(SUCCESS, ACYCLIC) → implement
	dec = workflow.DecideNextNode(graph, "cycle_check", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("cycle_check→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Review spine: implement → rev_correct → rev_tests → consolidate
	ptsfWalkReviewSpine(t, graph, run, cycles)
}

// ── Scenario 1: happy-path-full-arc ──────────────────────────────────────────

// TestPTSF_HappyPathFullArc exercises the complete happy path:
// all APPROVE verdicts, tool-node successes → green_build(SUCCESS) → close.
func TestPTSF_HappyPathFullArc(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	// Walk to consolidate.
	ptsfWalkToConsolidate(t, graph, run, cycles)

	// consolidate(APPROVE) → green_build
	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("consolidate→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// green_build(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "green_build", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal (SUCCESS per terminal-by-identity).
	dec = workflow.DecideNextNode(graph, "close", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: plan-review BLOCK (early exit) ───────────────────────────────

// TestPTSF_PlanReviewBlock exercises early escalation:
// start → frame_problem → draft_plan → plan_review(BLOCK) → close-needs-attention.
func TestPTSF_PlanReviewBlock(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkEntry(t, graph, run, cycles)
	ptsfWalkPlanPhase(t, graph, run, cycles)

	// plan_review(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("plan_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: plan-review REQUEST_CHANGES then approve ─────────────────────

// TestPTSF_PlanReviewRCThenApprove exercises the plan revision loop:
// plan_review(RC) → draft_plan → plan_review(APPROVE) → full arc → close.
func TestPTSF_PlanReviewRCThenApprove(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkEntry(t, graph, run, cycles)
	ptsfWalkPlanPhase(t, graph, run, cycles)

	// Increment cycle counter for the plan_review→draft_plan back-edge.
	cycles.Increment(run.RunID, "plan_review", "draft_plan", nil)

	// plan_review(REQUEST_CHANGES) → draft_plan
	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("plan_review(RC)→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Revise: draft_plan → plan_review (second pass)
	ptsfWalkPlanPhase(t, graph, run, cycles)

	// plan_review(APPROVE) → draft_spec
	dec = workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review(APPROVE)→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Continue: spec → tasking → review spine → consolidate(APPROVE) → green_build → close.
	ptsfWalkSpecPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "spec_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: %+v", dec)
	}

	ptsfWalkTaskingPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "load_beads", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("load_beads→cycle_check: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "cycle_check", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("cycle_check→implement: %+v", dec)
	}

	ptsfWalkReviewSpine(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("consolidate→green_build: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "green_build", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build→close: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "close", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: load_beads non-SUCCESS → unconditional fallback ──────────────

// TestPTSF_LoadBeadsNonSuccess exercises the tool-node commit-gate:
// load_beads FAIL → outcome.status != 'SUCCESS' → unconditional fallback →
// close-needs-attention.
func TestPTSF_LoadBeadsNonSuccess(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	// Walk to load_beads.
	ptsfWalkEntry(t, graph, run, cycles)
	ptsfWalkPlanPhase(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: %+v", dec)
	}
	ptsfWalkSpecPhase(t, graph, run, cycles)
	dec = workflow.DecideNextNode(graph, "spec_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: %+v", dec)
	}
	ptsfWalkTaskingPhase(t, graph, run, cycles)

	// load_beads FAIL → unconditional fallback → close-needs-attention.
	dec = workflow.DecideNextNode(graph, "load_beads", ptsfOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("load_beads FAIL fallback: Advance=%v Failed=%v", dec.Advance, dec.Failed)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("load_beads FAIL fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 5: cycle_check non-SUCCESS → unconditional fallback ──────────────

// TestPTSF_CycleCheckNonSuccess exercises the cycle-detection gate:
// load_beads SUCCESS → cycle_check FAIL (CYCLE detected) → unconditional fallback →
// close-needs-attention. This is the hk-l8rpd tool-node gate for dependency cycles.
func TestPTSF_CycleCheckNonSuccess(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	// Walk to cycle_check.
	ptsfWalkEntry(t, graph, run, cycles)
	ptsfWalkPlanPhase(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: %+v", dec)
	}
	ptsfWalkSpecPhase(t, graph, run, cycles)
	dec = workflow.DecideNextNode(graph, "spec_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: %+v", dec)
	}
	ptsfWalkTaskingPhase(t, graph, run, cycles)
	dec = workflow.DecideNextNode(graph, "load_beads", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("load_beads→cycle_check: %+v", dec)
	}

	// cycle_check FAIL (dependency cycle detected) → unconditional fallback →
	// close-needs-attention. The SUCCESS condition does not match, so the cascade
	// falls through to the unconditional fallback edge.
	dec = workflow.DecideNextNode(graph, "cycle_check", ptsfOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("cycle_check FAIL fallback: Advance=%v Failed=%v", dec.Advance, dec.Failed)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("cycle_check FAIL: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 6: consolidate BLOCK → close-needs-attention ────────────────────

// TestPTSF_ConsolidateBlock exercises:
// full arc to consolidate → consolidate(BLOCK) → close-needs-attention.
func TestPTSF_ConsolidateBlock(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 7: consolidate cap-hit (cap=3) ───────────────────────────────────

// TestPTSF_ConsolidateCapHit exercises WG-028/EM-043:
// when the consolidate→implement back-edge's traversal_cap (3) is exhausted,
// the conditional edge is suppressed and the cascade reports a cap-hit failure.
func TestPTSF_ConsolidateCapHit(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of consolidate→implement.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "consolidate", "implement", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is suppressed.
	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// ── Scenario 8: green_build deterministic fail → implement loop ───────────────

// TestPTSF_GreenBuildDeterministicFail exercises the compound-condition edge
// (D5 v1 dialect: outcome.status=='FAIL' && outcome.failure_class=='deterministic'):
// consolidate(APPROVE) → green_build(FAIL+deterministic) → implement (back-edge, cap=3).
func TestPTSF_GreenBuildDeterministicFail(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	// consolidate(APPROVE) → green_build
	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("consolidate→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// green_build(FAIL+deterministic) → implement (compound condition match).
	// This exercises the && conjunction in the D5 v1 dialect:
	// outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'.
	dec = workflow.DecideNextNode(graph, "green_build",
		ptsfOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("green_build(FAIL+deterministic)→implement: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 9: green_build non-deterministic fail → unconditional fallback ───

// TestPTSF_GreenBuildOtherFailure exercises the unconditional fallback on green_build:
// a non-deterministic failure (e.g. structural infra failure) does NOT match the
// compound condition, so the cascade falls through to close-needs-attention.
func TestPTSF_GreenBuildOtherFailure(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("consolidate→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// green_build(FAIL+structural) — the compound condition requires failure_class=deterministic;
	// structural does NOT match, so the conditional edge is skipped and the unconditional
	// fallback fires → close-needs-attention.
	dec = workflow.DecideNextNode(graph, "green_build",
		ptsfOutcomeFC(core.FailureClassStructural), run, cycles)
	if !dec.Advance {
		t.Fatalf("green_build(FAIL+structural) fallback: Advance=%v Failed=%v", dec.Advance, dec.Failed)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("green_build(FAIL+structural): NextNodeID=%q, want close-needs-attention",
			dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
