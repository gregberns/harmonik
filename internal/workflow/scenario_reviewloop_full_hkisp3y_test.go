package workflow_test

// scenario_reviewloop_full_hkisp3y_test.go — full review-loop cascade
// end-to-end scenario test exercising specs/examples/review-loop.dot through
// the real parser → validator → loader → cascade dispatcher pipeline.
//
// Five named scenarios from the spec:
//   1. approve-on-first-pass      → APPROVE → close (terminal, success)
//   2. two-REQUEST_CHANGES-approve → 2× loop-back then APPROVE → close
//   3. BLOCK-on-first             → BLOCK → close-needs-attention (terminal)
//   4. cap-hit-fallback           → 3× REQUEST_CHANGES → cap-hit → fallback
//   5. no-progress                → identical outcome → EM-015e early exit
//
// Spec refs:
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Bead ref: hk-isp3y.
// Helper prefix: rlFull (per implementer-protocol.md §Helper-prefix discipline).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
)

// ── fixtures ────────────────────────────────────────────────────────────────

func rlFullDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "review-loop.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("rlFullDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func rlFullRun(t *testing.T) *core.Run {
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

func rlFullOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ───────────────────────────────────────

// TestRLFull_ApproveOnFirstPass exercises the happy path:
// start → implementer → reviewer(APPROVE) → close (terminal).
func TestRLFull_ApproveOnFirstPass(t *testing.T) {
	dotPath := rlFullDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implementer → reviewer
	dec = workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reviewer(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: two REQUEST_CHANGES then approve ────────────────────────────

// TestRLFull_TwoRequestChangesThenApprove exercises the bounded loop:
// start → implementer → reviewer(RC) → implementer → reviewer(RC) →
// implementer → reviewer(APPROVE) → close.
func TestRLFull_TwoRequestChangesThenApprove(t *testing.T) {
	dotPath := rlFullDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("step 1 start→implementer failed: %+v", dec)
	}

	// Loop twice: reviewer(REQUEST_CHANGES) → implementer
	for i := 1; i <= 2; i++ {
		// implementer → reviewer
		dec = workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
		if !dec.Advance || dec.NextNodeID != "reviewer" {
			t.Fatalf("iteration %d implementer→reviewer failed: %+v", i, dec)
		}

		// Increment cycle counter for the reviewer→implementer edge (the caller
		// is responsible for incrementing after committing the transition).
		cycles.Increment(run.RunID, "reviewer", "implementer", nil)

		// reviewer(REQUEST_CHANGES) → implementer
		dec = workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
		if !dec.Advance || dec.NextNodeID != "implementer" {
			t.Fatalf("iteration %d reviewer→implementer failed: Advance=%v NextNodeID=%q",
				i, dec.Advance, dec.NextNodeID)
		}
	}

	// Third pass: implementer → reviewer → APPROVE → close
	dec = workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("final implementer→reviewer failed: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("final reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: BLOCK on first ─────────────────────────────────────────────

// TestRLFull_BlockOnFirst exercises:
// start → implementer → reviewer(BLOCK) → close-needs-attention (terminal).
func TestRLFull_BlockOnFirst(t *testing.T) {
	dotPath := rlFullDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}

	// implementer → reviewer
	dec = workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer: %+v", dec)
	}

	// reviewer(BLOCK) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("reviewer→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ────────────────────────────────────────────

// TestRLFull_CapHitFallback exercises the WG-010 cascade invariant through the
// REAL DOT pipeline: when the reviewer→implementer back-edge's traversal_cap (3)
// is exhausted, the conditional edge fails the traversal-cap check and the
// cascade reports a cap-hit failure (completion_reason=cap_hit). In the daemon
// driver (driveDotWorkflow) this routes the run to the needs-attention terminal.
//
// Previously dotEdgeToCoreEdge dropped dot.Edge.UnknownAttrs["traversal_cap"]
// (the hk-i7yq8 gap), so the cap was invisible to the cascade and the back-edge
// matched unboundedly. That bridge is now implemented: dotEdgeToCoreEdge parses
// traversal_cap into core.Edge.TraversalCap, and core.SelectNextEdge enforces it
// against the CycleCounter. This test asserts the now-correct cap-hit behavior.
func TestRLFull_CapHitFallback(t *testing.T) {
	dotPath := rlFullDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to reviewer.
	workflow.DecideNextNode(graph, "start", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of reviewer→implementer
	// (the back-edge's traversal_cap is 3 in review-loop.dot).
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "reviewer", "implementer", &cap)
	}

	// With the traversal_cap now bridged into core.Edge, the cascade enforces the
	// cap: the REQUEST_CHANGES back-edge is suppressed at cap-hit and the cascade
	// reports a compilation_loop failure with completion_reason=cap_hit.
	dec := workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// TestRLFull_CapHitFallback_DirectCascade proves the cascade engine correctly
// enforces traversal_cap when core.Edge.TraversalCap is set. This exercises
// the cap-hit → unconditional-fallback path that WILL work end-to-end once
// the dotEdgeToCoreEdge bridge is completed.
func TestRLFull_CapHitFallback_DirectCascade(t *testing.T) {
	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// Simulate 3 prior traversals of reviewer→implementer.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "reviewer", "implementer", &cap)
	}

	// Build edges matching review-loop.dot's reviewer outgoing edges, but with
	// TraversalCap properly set on the back-edge.
	approveLabel := "APPROVE"
	requestChangesLabel := "REQUEST_CHANGES"
	blockLabel := "BLOCK"

	approveCondition := core.PolicyExpression("outcome.preferred_label == 'APPROVE'")
	rcCondition := core.PolicyExpression("outcome.preferred_label == 'REQUEST_CHANGES'")
	blockCondition := core.PolicyExpression("outcome.preferred_label == 'BLOCK'")

	candidates := []core.Edge{
		{
			FromNode:    "reviewer",
			ToNode:      "close",
			Condition:   &approveCondition,
			Label:       &approveLabel,
			OrderingKey: "close",
		},
		{
			FromNode:     "reviewer",
			ToNode:       "implementer",
			Condition:    &rcCondition,
			Label:        &requestChangesLabel,
			TraversalCap: &cap,
			OrderingKey:  "implementer",
		},
		{
			FromNode:    "reviewer",
			ToNode:      "close-needs-attention",
			Condition:   &blockCondition,
			Label:       &blockLabel,
			OrderingKey: "close-needs-attention",
		},
		{
			// Unconditional fallback edge — no condition, no label.
			FromNode:    "reviewer",
			ToNode:      "close-needs-attention",
			OrderingKey: "close-needs-attention-fallback",
		},
	}

	// A simple evaluator that matches preferred_label equality conditions.
	eval := func(expr core.PolicyExpression, ctx map[string]any, o core.Outcome) bool {
		// Match "outcome.preferred_label == 'VALUE'" style conditions.
		if o.PreferredLabel == nil {
			return false
		}
		label := *o.PreferredLabel
		switch string(expr) {
		case "outcome.preferred_label == 'APPROVE'":
			return label == "APPROVE"
		case "outcome.preferred_label == 'REQUEST_CHANGES'":
			return label == "REQUEST_CHANGES"
		case "outcome.preferred_label == 'BLOCK'":
			return label == "BLOCK"
		}
		return false
	}

	// REQUEST_CHANGES with cap exhausted: the conditional REQUEST_CHANGES edge
	// is the highest-priority match (conditional-before-unconditional), but its
	// traversal cap is hit. SelectNextEdge should return FailureClassCompilationLoop.
	result := core.SelectNextEdge(run, candidates, rlFullOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), eval, cycles)

	if !result.Failed {
		t.Fatalf("expected Failed=true (cap hit), got Matched=%v Edge.ToNode=%v",
			result.Matched, result.Edge.ToNode)
	}
	if result.FailureClass != core.FailureClassCompilationLoop {
		t.Errorf("FailureClass = %q, want %q", result.FailureClass, core.FailureClassCompilationLoop)
	}
}

// TestRLFull_CapHitFallback_DirectCascade_FallbackFires extends the direct
// cascade test to demonstrate the full cap-hit → fallback chain that the
// daemon workloop would execute: when SelectNextEdge returns compilation_loop,
// the daemon re-runs the cascade WITHOUT the capped edge, and the
// unconditional fallback fires. This test simulates that two-phase dispatch.
func TestRLFull_CapHitFallback_DirectCascade_FallbackFires(t *testing.T) {
	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "reviewer", "implementer", &cap)
	}

	// Unconditional fallback edge only (simulating the daemon's retry without
	// the capped edge).
	approveCondition := core.PolicyExpression("outcome.preferred_label == 'APPROVE'")
	blockCondition := core.PolicyExpression("outcome.preferred_label == 'BLOCK'")
	approveLabel := "APPROVE"
	blockLabel := "BLOCK"

	candidatesWithoutCapped := []core.Edge{
		{
			FromNode:    "reviewer",
			ToNode:      "close",
			Condition:   &approveCondition,
			Label:       &approveLabel,
			OrderingKey: "close",
		},
		{
			FromNode:    "reviewer",
			ToNode:      "close-needs-attention",
			Condition:   &blockCondition,
			Label:       &blockLabel,
			OrderingKey: "close-needs-attention",
		},
		{
			// Unconditional fallback.
			FromNode:    "reviewer",
			ToNode:      "close-needs-attention",
			OrderingKey: "close-needs-attention-fallback",
		},
	}

	eval := func(expr core.PolicyExpression, _ map[string]any, o core.Outcome) bool {
		if o.PreferredLabel == nil {
			return false
		}
		label := *o.PreferredLabel
		switch string(expr) {
		case "outcome.preferred_label == 'APPROVE'":
			return label == "APPROVE"
		case "outcome.preferred_label == 'REQUEST_CHANGES'":
			return label == "REQUEST_CHANGES"
		case "outcome.preferred_label == 'BLOCK'":
			return label == "BLOCK"
		}
		return false
	}

	// With REQUEST_CHANGES label but capped edge removed: the APPROVE and BLOCK
	// conditions don't match, so the unconditional fallback fires.
	result := core.SelectNextEdge(run, candidatesWithoutCapped, rlFullOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), eval, cycles)

	if !result.Matched {
		t.Fatalf("expected Matched=true (unconditional fallback), got Failed=%v FailureReason=%q",
			result.Failed, result.FailureReason)
	}
	if string(result.Edge.ToNode) != "close-needs-attention" {
		t.Errorf("Edge.ToNode = %q, want %q", result.Edge.ToNode, "close-needs-attention")
	}
}

// ── Scenario 5: no-progress (EM-015e early exit) ────────────────────────────

// TestRLFull_NoProgress exercises the no-progress path per EM-015e: when the
// reviewer emits an outcome that indicates no progress was made (modeled here
// as an unknown/empty label that doesn't match any conditional edge), the
// cascade falls through to the unconditional fallback → close-needs-attention.
//
// In the production daemon, no-progress detection uses a bit-identical diff
// hash comparison (EM-015e). The cascade layer sees this as the daemon NOT
// routing to the REQUEST_CHANGES edge (the daemon suppresses the loop-back
// when it detects no progress). The cascade then has no matching conditional
// edge and falls through to the unconditional fallback.
//
// This test exercises the cascade-level behavior: an outcome with no matching
// label (simulating the daemon's suppressed routing) correctly reaches
// close-needs-attention via the unconditional fallback.
func TestRLFull_NoProgress(t *testing.T) {
	dotPath := rlFullDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → implementer → reviewer.
	dec := workflow.DecideNextNode(graph, "start", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer: %+v", dec)
	}

	// Simulate no-progress: the daemon would suppress the REQUEST_CHANGES
	// routing, so the reviewer emits an outcome with no label (or an
	// unrecognized label like "NO_PROGRESS"). No conditional edge matches;
	// the unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, "NO_PROGRESS"), run, cycles)
	if !dec.Advance {
		t.Fatalf("no-progress fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("no-progress fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestRLFull_NoProgress_EmptyLabel verifies the same fallback with a
// completely empty label (nil PreferredLabel), which is the simplest
// no-progress signal.
func TestRLFull_NoProgress_EmptyLabel(t *testing.T) {
	dotPath := rlFullDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := rlFullRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to reviewer.
	workflow.DecideNextNode(graph, "start", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Empty label → no conditional match → unconditional fallback.
	dec := workflow.DecideNextNode(graph, "reviewer", rlFullOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("empty-label fallback: Advance=%v Failed=%v", dec.Advance, dec.Failed)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("empty-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}
}
