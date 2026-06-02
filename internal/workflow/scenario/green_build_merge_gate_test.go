package scenario_test

// green_build_merge_gate_test.go — scenario tests for specs/examples/green-build-merge-gate.dot.
//
// Six named scenarios (S2 path obligations):
//   1. happy-path                → start→make_change(SUCCESS)→green_build(SUCCESS)→close
//   2. deterministic-fix-loop   → green_build(FAIL+deterministic)→make_change→green_build(SUCCESS)→close
//   3. deterministic-cap-hit    → green_build(FAIL+deterministic)×3 → cap-hit failure
//   4. transient-self-retry     → green_build(FAIL+transient)→green_build(SUCCESS)→close (self-loop)
//   5. transient-cap-hit        → green_build(FAIL+transient)×2 → cap-hit failure (self-loop exhausted)
//   6. structural-fallback      → green_build(RETRY, no condition match)→close-needs-attention (fallback)
//
// Key S2 obligations exercised:
//   - Tool-node commit-gate: outcome.status=='SUCCESS' on green_build advances to close
//   - Deterministic back-edge: FAIL+deterministic routes back to make_change (capped at 3)
//   - Transient self-loop: FAIL+transient retries green_build itself (capped at 2)
//   - Traversal-cap enforcement: 3× FAIL+deterministic → cap-hit; 2× FAIL+transient → cap-hit
//   - Unconditional fallback (WG-011): RETRY (no conditional match) → close-needs-attention
//   - Self-loop advancement: FAIL+transient → NextNodeID == "green_build" (same node)
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §15 (green-build-merge-gate topology)
//   - specs/workflow-graph.md WG-010 (5-step cascade)
//   - specs/workflow-graph.md WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-043 (traversal-cap enforcement)
//   - specs/execution-model.md EM-057 item 7 (exit-code → outcome)
//
// Helper prefix: gbmg (per implementer-protocol.md §Helper-prefix discipline).

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

// ── fixtures ──────────────────────────────────────────────────────────────────

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func gbmgOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// gbmgOutcomeFC builds a FAIL outcome carrying failure_class.
// Used for the compound conditions on green_build→make_change and green_build→green_build.
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

// ── Scenario 1: happy-path ────────────────────────────────────────────────────

// TestGBMG_HappyPath exercises the full success arc:
// start → make_change(SUCCESS) → green_build(SUCCESS) → close.
// The agent commits the change and the build gate passes on the first attempt.
func TestGBMG_HappyPath(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	// start → make_change
	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// make_change(SUCCESS) → green_build
	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// green_build(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: deterministic-fix-loop ───────────────────────────────────────

// TestGBMG_DeterministicFixLoop exercises the build-failure back-edge:
// start → make_change → green_build(FAIL+deterministic) → make_change →
// green_build(SUCCESS) → close.
// A deterministic failure (e.g. compilation error or test assertion) routes back
// to make_change for a second attempt; the second attempt passes.
func TestGBMG_DeterministicFixLoop(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	// start → make_change
	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: %+v", dec)
	}

	// make_change(SUCCESS) → green_build
	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: %+v", dec)
	}

	// green_build(FAIL+deterministic): build fails → back to make_change.
	// Increment cycle counter to model this traversal of the back-edge.
	cap := 3
	cycles.Increment(run.RunID, "green_build", "make_change", &cap)

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("green_build(FAIL+deterministic)→make_change: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// Second make_change attempt → green_build
	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change (2nd)→green_build: %+v", dec)
	}

	// green_build(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: deterministic-cap-hit ────────────────────────────────────────

// TestGBMG_DeterministicCapHit exercises traversal_cap enforcement on the
// green_build→make_change back-edge (cap=3):
// green_build(FAIL+deterministic) ×3 → cap-hit failure.
func TestGBMG_DeterministicCapHit(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to green_build via start → make_change.
	workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill the cycle counter: 3 traversals of green_build→make_change at cap=3.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "green_build", "make_change", &cap)
	}

	// With the cap exhausted, FAIL+deterministic can no longer take the back-edge.
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

// ── Scenario 4: transient-self-retry ─────────────────────────────────────────

// TestGBMG_TransientSelfRetry exercises the transient self-loop on green_build:
// start → make_change → green_build(FAIL+transient) → green_build(SUCCESS) → close.
// A transient infra glitch retries the build step (self-loop). The second attempt passes.
func TestGBMG_TransientSelfRetry(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	// start → make_change
	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: %+v", dec)
	}

	// make_change(SUCCESS) → green_build
	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: %+v", dec)
	}

	// green_build(FAIL+transient): infra glitch → self-loop back to green_build.
	// Increment cycle counter to model this self-loop traversal.
	cap := 2
	cycles.Increment(run.RunID, "green_build", "green_build", &cap)

	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("green_build(FAIL+transient)→green_build: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// green_build(SUCCESS) → close  (second attempt succeeds)
	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("green_build(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 5: transient-cap-hit ────────────────────────────────────────────

// TestGBMG_TransientCapHit exercises traversal_cap enforcement on the
// green_build→green_build self-loop (cap=2):
// green_build(FAIL+transient) ×2 → cap-hit failure.
func TestGBMG_TransientCapHit(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to green_build via start → make_change.
	workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill the cycle counter: 2 traversals of the green_build self-loop at cap=2.
	cap := 2
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "green_build", "green_build", &cap)
	}

	// With the cap exhausted, FAIL+transient can no longer take the self-loop edge.
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

// ── Scenario 6: structural-fallback ──────────────────────────────────────────

// TestGBMG_StructuralFallback exercises the unconditional fallback on green_build:
// start → make_change → green_build(RETRY, no condition match) → close-needs-attention.
// A RETRY outcome does not match SUCCESS, FAIL+deterministic, or FAIL+transient, so
// the unconditional fallback (WG-011) fires → close-needs-attention.
func TestGBMG_StructuralFallback(t *testing.T) {
	graph := gbmgLoadGraph(t)
	run := gbmgRun(t)
	cycles := core.NewCycleCounter()

	// start → make_change
	dec := workflow.DecideNextNode(graph, "start", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "make_change" {
		t.Fatalf("start→make_change: %+v", dec)
	}

	// make_change(SUCCESS) → green_build
	dec = workflow.DecideNextNode(graph, "make_change", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "green_build" {
		t.Fatalf("make_change→green_build: %+v", dec)
	}

	// green_build(RETRY): does not match any conditional edge.
	// The unconditional fallback fires → close-needs-attention.
	dec = workflow.DecideNextNode(graph, "green_build", gbmgOutcome(core.OutcomeStatusRetry, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("green_build(RETRY) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", gbmgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
