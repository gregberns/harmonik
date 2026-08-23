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
		WorkflowID:      mustWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
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

func ptsnWalkSpecPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "draft_spec", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "spec_review" {
		t.Fatalf("draft_spec→spec_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

func ptsnWalkTaskingPhase(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "decompose", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "load_beads" {
		t.Fatalf("decompose→load_beads: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

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

func ptsnWalkToConsolidate(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	ptsnWalkPlanPhase(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsnWalkSpecPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsnWalkTaskingPhase(t, graph, run, cycles)

	dec = workflow.DecideNextNode(graph, "load_beads", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("load_beads→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	ptsnWalkReviewSpine(t, graph, run, cycles)
}

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

	ptsnWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("docs_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("plan_review→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	ptsnWalkPlanPhase(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "plan_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("plan_review→draft_spec: %+v", dec)
	}
	ptsnWalkSpecPhase(t, graph, run, cycles)

	if _, err := cycles.Increment(run.RunID, "spec_review", "draft_spec", nil); err != nil {
		t.Fatalf("pre-fill cycle counter spec_review\u2192draft_spec: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "draft_spec" {
		t.Fatalf("spec_review(RC)→draft_spec: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "draft_spec", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "spec_review" {
		t.Fatalf("draft_spec→spec_review (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "spec_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "decompose" {
		t.Fatalf("spec_review(APPROVE)→decompose: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

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

	dec = workflow.DecideNextNode(graph, "load_beads", ptsnOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("load_beads FAIL fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("load_beads FAIL fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("consolidate→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "consolidate", "implement", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter consolidate\u2192implement: %v", err)
		}
	}

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

	ptsnWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("docs_review→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	ptsnWalkToConsolidate(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "consolidate", ptsnOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("consolidate→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "docs_review" {
		t.Fatalf("update_docs→docs_review: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "docs_review", ptsnOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", ptsnOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
