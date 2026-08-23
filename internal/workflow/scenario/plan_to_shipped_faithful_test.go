package scenario_test

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
		WorkflowID:      mustWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
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

func ptsfOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

func ptsfLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	dotPath := ptsfDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

func ptsfWalkEntry(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "frame_problem" {
		t.Fatalf("start→frame_problem: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "frame_problem", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("frame_problem→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

func ptsfWalkPlanPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "draft_plan", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "plan_review" {
		t.Fatalf("draft_plan→plan_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

func ptsfWalkSpecPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "draft_spec", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "spec_review" {
		t.Fatalf("draft_spec→spec_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

func ptsfWalkTaskingPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "decompose", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("decompose→load_beads: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

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

func ptsfWalkToConsolidate(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	ptsfWalkEntry(t, graph, run, cycles)

	ptsfWalkPlanPhase(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsfWalkSpecPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "spec_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsfWalkTaskingPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "load_beads", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("load_beads→cycle_check: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "cycle_check", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("cycle_check→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsfWalkReviewSpine(t, graph, run, cycles)
}

// TestPTSF_HappyPathFullArc exercises the complete happy path:
// all APPROVE verdicts, tool-node successes → green_build(SUCCESS) → close.
func TestPTSF_HappyPathFullArc(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("consolidate→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "green_build", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", ptsfOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestPTSF_PlanReviewBlock exercises early escalation:
// start → frame_problem → draft_plan → plan_review(BLOCK) → close-needs-attention.
func TestPTSF_PlanReviewBlock(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkEntry(t, graph, run, cycles)
	ptsfWalkPlanPhase(t, graph, run, cycles)

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

// TestPTSF_PlanReviewRCThenApprove exercises the plan revision loop:
// plan_review(RC) → draft_plan → plan_review(APPROVE) → full arc → close.
func TestPTSF_PlanReviewRCThenApprove(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkEntry(t, graph, run, cycles)
	ptsfWalkPlanPhase(t, graph, run, cycles)

	if _, err := cycles.Increment(run.RunID, "plan_review", "draft_plan", nil); err != nil {
		t.Fatalf("pre-fill cycle counter plan_review\u2192draft_plan: %v", err)
	}

	dec := workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_plan" {
		t.Fatalf("plan_review(RC)→draft_plan: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsfWalkPlanPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "plan_review", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review(APPROVE)→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
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

// TestPTSF_LoadBeadsNonSuccess exercises the tool-node commit-gate:
// load_beads FAIL → outcome.status != 'SUCCESS' → unconditional fallback →
// close-needs-attention.
func TestPTSF_LoadBeadsNonSuccess(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

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

// TestPTSF_CycleCheckNonSuccess exercises the cycle-detection gate:
// load_beads SUCCESS → cycle_check FAIL (CYCLE detected) → unconditional fallback →
// close-needs-attention. This is the hk-l8rpd tool-node gate for dependency cycles.
func TestPTSF_CycleCheckNonSuccess(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

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

// TestPTSF_ConsolidateCapHit exercises WG-028/EM-043:
// when the consolidate→implement back-edge's traversal_cap (3) is exhausted,
// the conditional edge is suppressed and the cascade reports a cap-hit failure.
func TestPTSF_ConsolidateCapHit(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "consolidate", "implement", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter consolidate\u2192implement: %v", err)
		}
	}

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

// TestPTSF_GreenBuildDeterministicFail exercises the compound-condition edge
// (D5 v1 dialect: outcome.status=='FAIL' && outcome.failure_class=='deterministic'):
// consolidate(APPROVE) → green_build(FAIL+deterministic) → implement (back-edge, cap=3).
func TestPTSF_GreenBuildDeterministicFail(t *testing.T) {
	graph := ptsfLoadGraph(t)
	run := ptsfRun(t)
	cycles := core.NewCycleCounter()

	ptsfWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsfOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("consolidate→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "green_build",
		ptsfOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("green_build(FAIL+deterministic)→implement: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

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
