package workflow_test

// scenario_quality_gate_policy_hko52fm21_test.go — gate-node policy decision
// end-to-end scenario test exercising specs/examples/quality-gate-policy.dot
// through the real parser → validator → loader → cascade dispatcher pipeline.
//
// Nine named scenarios covering the S2 path obligations for the gate topology:
//   1. gate-allow-happy-path       → APPROVE → gate(allow) → close
//   2. gate-deny-loop              → APPROVE → gate(deny) → implementer → APPROVE → gate(allow) → close
//   3. gate-escalate-to-human      → APPROVE → gate(escalate-to-human) → close-needs-attention
//   4. gate-fallback-eval-failure  → APPROVE → gate(FAIL/eval-failure) → close-needs-attention (fallback)
//   5. reviewer-request-changes    → REQUEST_CHANGES → implementer → APPROVE → gate(allow) → close
//   6. reviewer-block              → BLOCK → close-needs-attention
//   7. reviewer-fallback           → unrecognized label → close-needs-attention (fallback)
//   8. reviewer-cap-hit            → 3× REQUEST_CHANGES → cap_hit → Failed
//   9. gate-deny-cap-hit           → APPROVE → 3× gate(deny) → cap_hit → Failed
//
// Key: gate decisions (allow/deny/escalate-to-human) are ALL status=SUCCESS per
// specs/control-points.md §6.1.8 CP-058. The gate's outgoing edges route on
// outcome.preferred_label, NOT on outcome.status. A gate eval-failure returns
// status=FAIL and reaches the unconditional fallback.
//
// Spec refs:
//   - specs/workflow-graph.md WG-005  — gate node attribute set
//   - specs/workflow-graph.md WG-007  — gate legal outcome statuses: SUCCESS, FAIL
//   - specs/workflow-graph.md WG-010  — five-step cascade
//   - specs/workflow-graph.md WG-011  — unconditional-edge fallback invariant
//   - specs/workflow-graph.md WG-028  — cycle bounding / traversal_cap
//   - specs/control-points.md CP-058  — gate is SUCCESS regardless of decision
//   - specs/execution-model.md EM-005b — gate_decision Outcome
//   - specs/execution-model.md EM-043  — traversal-cap enforcement
//
// Bead ref: hk-o52fm.21.
// Helper prefix: qgp (per implementer-protocol.md §Helper-prefix discipline).

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

func qgpDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "quality-gate-policy.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("qgpDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func qgpRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-qgp-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

// qgpOutcome returns a reviewer-style Outcome (status=SUCCESS, optional label).
func qgpOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// qgpGateOutcome returns a gate-decision Outcome. Per CP-058, evaluated gate
// decisions are always status=SUCCESS; the decision is in preferred_label.
// An eval-failure passes status=FAIL and an empty label (fallback path).
func qgpGateOutcome(allow bool, label string) core.Outcome {
	status := core.OutcomeStatusSuccess
	if !allow {
		status = core.OutcomeStatusFail
	}
	o := core.Outcome{Status: status, Kind: core.OutcomeKindGateDecision}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: gate-allow happy path ───────────────────────────────────────

// TestQGP_GateAllowHappyPath exercises the full happy path:
// start → implementer → reviewer(APPROVE) → quality_gate(allow) → close.
func TestQGP_GateAllowHappyPath(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	// start → implementer
	dec := workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implementer → reviewer
	dec = workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reviewer(APPROVE) → quality_gate
	dec = workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "quality_gate" {
		t.Fatalf("reviewer(APPROVE)→quality_gate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// quality_gate(allow) → close. Gate decisions are status=SUCCESS per CP-058.
	dec = workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(true, "allow"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("quality_gate(allow)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: gate-deny loop ───────────────────────────────────────────────

// TestQGP_GateDenyLoopThenAllow exercises the gate deny back-edge:
// implementer → reviewer(APPROVE) → quality_gate(deny) → implementer →
// reviewer(APPROVE) → quality_gate(allow) → close.
func TestQGP_GateDenyLoopThenAllow(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to quality_gate (first pass).
	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// Simulate one gate deny traversal; increment the cycle counter for quality_gate→implementer.
	cycles.Increment(run.RunID, "quality_gate", "implementer", nil)

	// quality_gate(deny) → implementer.
	dec := workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(true, "deny"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("quality_gate(deny)→implementer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implementer → reviewer (second pass)
	dec = workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer (2nd pass): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reviewer(APPROVE) → quality_gate (second pass)
	dec = workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "quality_gate" {
		t.Fatalf("reviewer(APPROVE)→quality_gate (2nd pass): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// quality_gate(allow) → close (second pass)
	dec = workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(true, "allow"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("quality_gate(allow)→close (2nd pass): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: gate-escalate-to-human ──────────────────────────────────────

// TestQGP_GateEscalateToHuman exercises:
// reviewer(APPROVE) → quality_gate(escalate-to-human) → close-needs-attention.
func TestQGP_GateEscalateToHuman(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// quality_gate(escalate-to-human) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(true, "escalate-to-human"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("quality_gate(escalate-to-human)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: gate fallback / eval-failure ─────────────────────────────────

// TestQGP_GateFallbackEvalFailure exercises the unconditional fallback when the
// gate returns status=FAIL (eval-failure: registry nil or structural error).
// No conditional edge matches FAIL, so the unconditional fallback fires.
func TestQGP_GateFallbackEvalFailure(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// quality_gate returns FAIL (eval-failure) → unconditional fallback → close-needs-attention.
	dec := workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(false, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("quality_gate(FAIL)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 5: reviewer REQUEST_CHANGES loop then APPROVE + gate allow ──────

// TestQGP_ReviewerRequestChangesThenApprove exercises the reviewer back-edge loop:
// reviewer(REQUEST_CHANGES) → implementer → reviewer(APPROVE) → quality_gate(allow) → close.
func TestQGP_ReviewerRequestChangesThenApprove(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → implementer → reviewer.
	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Increment reviewer→implementer counter (simulates one prior traversal).
	cycles.Increment(run.RunID, "reviewer", "implementer", nil)

	// reviewer(REQUEST_CHANGES) → implementer.
	dec := workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("reviewer(REQUEST_CHANGES)→implementer: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// implementer → reviewer (second pass)
	dec = workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reviewer(APPROVE) → quality_gate
	dec = workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "quality_gate" {
		t.Fatalf("reviewer(APPROVE)→quality_gate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// quality_gate(allow) → close
	dec = workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(true, "allow"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("quality_gate(allow)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 6: reviewer BLOCK → close-needs-attention ───────────────────────

// TestQGP_ReviewerBlock exercises:
// reviewer(BLOCK) → close-needs-attention (terminal).
func TestQGP_ReviewerBlock(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// reviewer(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("reviewer(BLOCK)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 7: reviewer fallback (unrecognized label) ───────────────────────

// TestQGP_ReviewerFallback exercises WG-011 on the reviewer node: an unrecognized
// label (e.g., "UNKNOWN") does not match any conditional edge; the unconditional
// fallback fires → close-needs-attention.
func TestQGP_ReviewerFallback(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Unrecognized label → no conditional edge matches → unconditional fallback.
	dec := workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "UNKNOWN"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("reviewer(UNKNOWN)→fallback: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 8: reviewer cap-hit ─────────────────────────────────────────────

// TestQGP_ReviewerCapHit exercises WG-028 / EM-043 on the reviewer→implementer
// back-edge (traversal_cap="3"): after 3 REQUEST_CHANGES traversals, the cap is
// exhausted and SelectNextEdge returns Failed=true, CompletionReason="cap_hit".
func TestQGP_ReviewerCapHit(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to reviewer.
	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: 3 traversals of reviewer→implementer.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "reviewer", "implementer", &cap)
	}

	// With cap exhausted, REQUEST_CHANGES back-edge is suppressed → cap_hit.
	dec := workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Failed {
		t.Fatalf("reviewer cap-hit: expected Failed=true, got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("reviewer cap-hit: expected CompletionReason=cap_hit, got %q", dec.CompletionReason)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("reviewer cap-hit: expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}

// ── Scenario 9: gate deny cap-hit ────────────────────────────────────────────

// TestQGP_GateDenyCapHit exercises WG-028 / EM-043 on the quality_gate→implementer
// back-edge (traversal_cap="3"): after 3 deny traversals the cap is exhausted.
// The deny cap is independent of the reviewer→implementer cap.
func TestQGP_GateDenyCapHit(t *testing.T) {
	graph, err := workflow.LoadDotWorkflow(qgpDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := qgpRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to quality_gate.
	workflow.DecideNextNode(graph, "start", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", qgpOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "reviewer", qgpOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// Pre-fill cycle counter: 3 traversals of quality_gate→implementer (deny).
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "quality_gate", "implementer", &cap)
	}

	// With cap exhausted, deny back-edge is suppressed → cap_hit.
	dec := workflow.DecideNextNode(graph, "quality_gate", qgpGateOutcome(true, "deny"), run, cycles)
	if !dec.Failed {
		t.Fatalf("gate deny cap-hit: expected Failed=true, got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("gate deny cap-hit: expected CompletionReason=cap_hit, got %q", dec.CompletionReason)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("gate deny cap-hit: expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}
