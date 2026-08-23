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

func stfDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "sentry-triage-faithful.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("stfDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func stfRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      mustWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-stf-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func stfOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

func stfLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(stfDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

func stfWalkToAssessConfidence(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	dec := workflow.DecideNextNode(graph, "start", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "init" {
		t.Fatalf("start→init: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "init", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "investigate" {
		t.Fatalf("init→investigate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "investigate", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "assess_confidence" {
		t.Fatalf("investigate→assess_confidence: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// TestSTF_HappyPath exercises the full success arc:
// start → init → investigate → assess_confidence(SUCCESS) →
// dedup_check → check_dedup(SUCCESS) → create_issue → close.
func TestSTF_HappyPath(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	stfWalkToAssessConfidence(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "assess_confidence", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "dedup_check" {
		t.Fatalf("assess_confidence(SUCCESS)→dedup_check: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "dedup_check", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "check_dedup" {
		t.Fatalf("dedup_check→check_dedup: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "check_dedup", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "create_issue" {
		t.Fatalf("check_dedup(SUCCESS)→create_issue: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "create_issue", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("create_issue→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSTF_LowConfidence exercises the WG-011 unconditional fallback on
// assess_confidence: a FAIL outcome (LOW confidence, exit 1) has no matching
// conditional edge, so the unconditional fallback fires → close-skip.
func TestSTF_LowConfidence(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	stfWalkToAssessConfidence(t, graph, run, cycles)

	dec := workflow.DecideNextNode(graph, "assess_confidence", stfOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-skip" {
		t.Fatalf("assess_confidence(FAIL)→close-skip: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-skip", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-skip: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSTF_DuplicateIssue exercises the WG-011 unconditional fallback on
// check_dedup: a FAIL outcome (DUPLICATE, exit 1) has no matching conditional
// edge, so the unconditional fallback fires → close-skip.
func TestSTF_DuplicateIssue(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	stfWalkToAssessConfidence(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "assess_confidence", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "dedup_check" {
		t.Fatalf("assess_confidence(SUCCESS)→dedup_check: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "dedup_check", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "check_dedup" {
		t.Fatalf("dedup_check→check_dedup: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "check_dedup", stfOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-skip" {
		t.Fatalf("check_dedup(FAIL)→close-skip: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-skip", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-skip: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSTF_InitUnconditional demonstrates that the init→investigate edge is
// unconditional: a FAIL outcome from init (e.g. Sentry CLI unavailable) still
// advances to investigate. The topology delegates failure detection to
// investigate (which will find no .ai/issue.json) and then to assess_confidence's
// fallback.
func TestSTF_InitUnconditional(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "init" {
		t.Fatalf("start→init: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "init", stfOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "investigate" {
		t.Fatalf("init(FAIL)→investigate: Advance=%v NextNodeID=%q (want unconditional advance)",
			dec.Advance, dec.NextNodeID)
	}
}
