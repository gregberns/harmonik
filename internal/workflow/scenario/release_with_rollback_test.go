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

func rwrDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "release-with-rollback.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("rwrDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func rwrRun(t *testing.T) *core.Run {
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

func rwrOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

func rwrLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(rwrDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

// TestRWR_HappyPath exercises the full success arc:
// start → cut_release(SUCCESS) → build_artifacts(SUCCESS) → publish(SUCCESS) → close.
// All tool nodes exit 0; the agent commits the release prep; the full pipeline completes.
func TestRWR_HappyPath(t *testing.T) {
	graph := rwrLoadGraph(t)
	run := rwrRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cut_release" {
		t.Fatalf("start→cut_release: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "cut_release", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "build_artifacts" {
		t.Fatalf("cut_release→build_artifacts: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "build_artifacts", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "publish" {
		t.Fatalf("build_artifacts(SUCCESS)→publish: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "publish", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("publish(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRWR_BuildFailure exercises the build_artifacts failure path:
// start → cut_release(SUCCESS) → build_artifacts(FAIL) → close-needs-attention.
// A build failure (non-zero exit) fires the unconditional fallback on build_artifacts.
// No rollback is needed — nothing was published yet.
func TestRWR_BuildFailure(t *testing.T) {
	graph := rwrLoadGraph(t)
	run := rwrRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cut_release" {
		t.Fatalf("start→cut_release: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "cut_release", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "build_artifacts" {
		t.Fatalf("cut_release→build_artifacts: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "build_artifacts", rwrOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("build_artifacts(FAIL) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRWR_BuildInfraFallback exercises the unconditional fallback on build_artifacts
// for a non-FAIL unexpected outcome (e.g. RETRY from a transient infra error):
// start → cut_release(SUCCESS) → build_artifacts(RETRY) → close-needs-attention.
// Any non-SUCCESS outcome from build_artifacts — including transient infra states —
// fires the fallback, since no conditional edge matches anything other than SUCCESS.
// Rollback is still not needed because nothing was published.
func TestRWR_BuildInfraFallback(t *testing.T) {
	graph := rwrLoadGraph(t)
	run := rwrRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cut_release" {
		t.Fatalf("start→cut_release: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "cut_release", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "build_artifacts" {
		t.Fatalf("cut_release→build_artifacts: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "build_artifacts", rwrOutcome(core.OutcomeStatusRetry), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("build_artifacts(RETRY) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRWR_PublishFailureRollback exercises the explicit publish FAIL condition:
// start → cut_release(SUCCESS) → build_artifacts(SUCCESS) → publish(FAIL) →
// rollback → close-needs-attention.
// A non-zero exit from publish matches the explicit FAIL condition → rollback fires
// as the compensating action. Rollback then always escalates to close-needs-attention.
func TestRWR_PublishFailureRollback(t *testing.T) {
	graph := rwrLoadGraph(t)
	run := rwrRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cut_release" {
		t.Fatalf("start→cut_release: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "cut_release", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "build_artifacts" {
		t.Fatalf("cut_release→build_artifacts: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "build_artifacts", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "publish" {
		t.Fatalf("build_artifacts(SUCCESS)→publish: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "publish", rwrOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "rollback" {
		t.Fatalf("publish(FAIL)→rollback: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "rollback", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("rollback→close-needs-attention: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRWR_PublishFallbackRollback exercises the unconditional fallback on publish:
// start → cut_release(SUCCESS) → build_artifacts(SUCCESS) → publish(RETRY) →
// rollback → close-needs-attention.
// A RETRY outcome from publish does not match the SUCCESS or FAIL conditions, so
// the unconditional fallback (WG-011) fires → rollback. This demonstrates that any
// non-SUCCESS publish state — not just explicit FAIL — triggers the compensating action.
func TestRWR_PublishFallbackRollback(t *testing.T) {
	graph := rwrLoadGraph(t)
	run := rwrRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cut_release" {
		t.Fatalf("start→cut_release: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "cut_release", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "build_artifacts" {
		t.Fatalf("cut_release→build_artifacts: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "build_artifacts", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "publish" {
		t.Fatalf("build_artifacts(SUCCESS)→publish: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "publish", rwrOutcome(core.OutcomeStatusRetry), run, cycles)
	if !dec.Advance || dec.NextNodeID != "rollback" {
		t.Fatalf("publish(RETRY) fallback→rollback: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "rollback", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("rollback→close-needs-attention: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", rwrOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
