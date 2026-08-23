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

func gbmgDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "green-build-merge-gate.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("gbmgDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func gbmgRun(t *testing.T) *core.Run {
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

func gbmgOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

func gbmgOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

func gbmgLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(gbmgDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

// TestGBMG_HappyPath exercises the full success arc:
// start → make_change(SUCCESS) → green_build(SUCCESS) → close.
// The agent commits the change and the build gate passes on the first attempt.
func TestGBMG_HappyPath(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestGBMG_DeterministicFixLoop exercises the build-failure back-edge:
// start → make_change → green_build(FAIL+deterministic) → make_change →
// green_build(SUCCESS) → close.
// A deterministic failure (e.g. compilation error or test assertion) routes back
// to make_change for a second attempt; the second attempt passes.
func TestGBMG_DeterministicFixLoop(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: %+v", dec)
	}

	traversalCap := 3
	if _, err := cycles.Increment(run.RunID, "green_build", "make_change", &traversalCap); err != nil {
		t.Fatalf("pre-fill cycle counter green_build\u2192make_change: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("green_build(FAIL+deterministic)→make_change: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change (2nd)→green_build: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestGBMG_DeterministicCapHit exercises traversal_cap enforcement on the
// green_build→make_change back-edge (cap=3):
// green_build(FAIL+deterministic) ×3 → cap-hit failure.
func TestGBMG_DeterministicCapHit(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "green_build", "make_change", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter green_build\u2192make_change: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "green_build", gbmgOutcomeFC(core.FailureClassDeterministic), run, cycles)
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

// TestGBMG_TransientSelfRetry exercises the transient self-loop on green_build:
// start → make_change → green_build(FAIL+transient) → green_build(SUCCESS) → close.
// A transient infra glitch retries the build step (self-loop). The second attempt passes.
func TestGBMG_TransientSelfRetry(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: %+v", dec)
	}

	traversalCap := 2
	if _, err := cycles.Increment(run.RunID, "green_build", "green_build", &traversalCap); err != nil {
		t.Fatalf("pre-fill cycle counter green_build\u2192green_build: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("green_build(FAIL+transient)→green_build: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestGBMG_TransientCapHit exercises traversal_cap enforcement on the
// green_build→green_build self-loop (cap=2):
// green_build(FAIL+transient) ×2 → cap-hit failure.
func TestGBMG_TransientCapHit(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)

	traversalCap := 2
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "green_build", "green_build", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter green_build\u2192green_build: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "green_build", gbmgOutcomeFC(core.FailureClassTransient), run, cycles)
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

// TestGBMG_StructuralFallback exercises the unconditional fallback on green_build:
// start → make_change → green_build(RETRY, no condition match) → close-needs-attention.
// A RETRY outcome does not match SUCCESS, FAIL+deterministic, or FAIL+transient, so
// the unconditional fallback (WG-011) fires → close-needs-attention.
func TestGBMG_StructuralFallback(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusRetry), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("green_build(RETRY) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", gbmgOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
