package workflow_test

// scenario_standard_bead_hkp0kum_test.go — golden tests for
// specs/examples/standard-bead.dot through the real parser → validator →
// loader → cascade dispatcher pipeline.
//
// Two invariant classes asserted:
//
//   A. Single-inbound-edge-to-close: the ONLY path that reaches the "close"
//      terminal node is review→close[APPROVE]. Every other terminal path
//      leads to "close-needs-attention". Verified structurally (graph
//      inspection) and behaviorally (cascade routing).
//
//   B. Cascade routes — seven named scenarios:
//      1. happy-path              start→implement→commit_gate(SUCCESS)→review(APPROVE)→close
//      2. gate-deterministic-fix  commit_gate(FAIL/deterministic)→implement (fix-loop)
//      3. gate-transient-retry    commit_gate(FAIL/transient)→commit_gate (self-loop)
//      4. gate-fallback           commit_gate(fallback)→close-needs-attention
//      5. review-request-changes  review(REQUEST_CHANGES)→implement
//      6. review-block            review(BLOCK)→close-needs-attention
//      7. review-fallback         review(unknown label)→close-needs-attention (unconditional fallback)
//
// Spec refs:
//   - specs/workflow-graph.md WG-010 (5-step cascade)
//   - specs/workflow-graph.md WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md WG-028 (traversal_cap)
//   - specs/execution-model.md EM-015d (verdict routing)
//   - specs/execution-model.md EM-015e (iteration cap)
//   - specs/execution-model.md EM-057 (exit-code → outcome)
//
// Bead ref: hk-p0kum.
// Helper prefix: sb (per implementer-protocol.md §Helper-prefix discipline).

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

// ── fixtures ────────────────────────────────────────────────────────────────

func sbDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "standard-bead.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("sbDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func sbLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(sbDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

func sbRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-sb-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func sbOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

func sbOutcomeWithFailureClass(status core.OutcomeStatus, failureClass core.FailureClass) core.Outcome {
	fc := failureClass
	return core.Outcome{
		Status:       status,
		Kind:         core.OutcomeKindDefault,
		FailureClass: &fc,
	}
}

// ── Invariant A: single-inbound-edge-to-close ───────────────────────────────

// TestSB_SingleInboundEdgeToClose asserts that the "close" terminal node has
// exactly one inbound edge in the parsed graph, and that edge originates from
// "review" with the raw condition "outcome.preferred_label == 'APPROVE'".
// This is the structural half of the sole-inbound-edge invariant.
func TestSB_SingleInboundEdgeToClose(t *testing.T) {
	graph := sbLoadGraph(t)

	// Collect all edges whose destination is "close".
	var inbound []*dot.Edge
	for _, e := range graph.Edges {
		if e.ToNodeID == "close" {
			inbound = append(inbound, e)
		}
	}

	if len(inbound) != 1 {
		t.Fatalf("close node has %d inbound edge(s), want exactly 1; edges: %+v",
			len(inbound), inbound)
	}

	e := inbound[0]
	if e.FromNodeID != "review" {
		t.Errorf("sole inbound edge to close: FromNodeID = %q, want %q", e.FromNodeID, "review")
	}
	if e.Condition == nil {
		t.Fatalf("sole inbound edge to close has no condition; want outcome.preferred_label == 'APPROVE'")
	}
	const wantCond = "outcome.preferred_label == 'APPROVE'"
	if e.ConditionRaw != wantCond {
		t.Errorf("sole inbound edge condition = %q, want %q", e.ConditionRaw, wantCond)
	}
}

// ── Invariant B.1: happy path ───────────────────────────────────────────────

// TestSB_HappyPath exercises the end-to-end success path:
// start → implement → commit_gate(SUCCESS) → review(APPROVE) → close (terminal).
func TestSB_HappyPath(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// start → implement
	dec := workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("start→implement: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// implement → commit_gate
	dec = workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "commit_gate" {
		t.Fatalf("implement→commit_gate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// commit_gate(SUCCESS) → review
	dec = workflow.DecideNextNode(graph, "commit_gate", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review" {
		t.Fatalf("commit_gate(SUCCESS)→review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// review(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "review", sbOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review(APPROVE)→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Invariant B.2: commit_gate deterministic fix-loop ───────────────────────

// TestSB_GateDeterministicFixLoop exercises the build/test failure fix-loop:
// implement → commit_gate(FAIL/deterministic) → implement (loop-back).
func TestSB_GateDeterministicFixLoop(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to commit_gate via the normal path.
	workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// commit_gate(FAIL/deterministic) → implement (fix-loop)
	det := sbOutcomeWithFailureClass(core.OutcomeStatusFail, core.FailureClassDeterministic)
	dec := workflow.DecideNextNode(graph, "commit_gate", det, run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("commit_gate(FAIL/deterministic)→implement: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Invariant B.3: commit_gate transient self-loop ──────────────────────────

// TestSB_GateTransientSelfLoop exercises the transient infra-glitch retry:
// commit_gate(FAIL/transient) → commit_gate (self-loop).
func TestSB_GateTransientSelfLoop(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to commit_gate.
	workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// commit_gate(FAIL/transient) → commit_gate (self-loop)
	tr := sbOutcomeWithFailureClass(core.OutcomeStatusFail, core.FailureClassTransient)
	dec := workflow.DecideNextNode(graph, "commit_gate", tr, run, cycles)
	if !dec.Advance || dec.NextNodeID != "commit_gate" {
		t.Fatalf("commit_gate(FAIL/transient)→commit_gate: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Invariant B.4: commit_gate fallback ─────────────────────────────────────

// TestSB_GateFallback exercises the unconditional fallback from commit_gate:
// commit_gate(canceled / structural / unknown) → close-needs-attention.
func TestSB_GateFallback(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to commit_gate.
	workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// An outcome that matches neither SUCCESS nor FAIL+deterministic nor FAIL+transient
	// falls through to the unconditional fallback. RETRY is a valid OutcomeStatus
	// that matches none of the commit_gate conditional edges.
	retry := core.Outcome{Status: core.OutcomeStatusRetry, Kind: core.OutcomeKindDefault}
	dec := workflow.DecideNextNode(graph, "commit_gate", retry, run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("commit_gate(canceled)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Invariant B.5: review REQUEST_CHANGES fix-loop ──────────────────────────

// TestSB_ReviewRequestChanges exercises the reviewer REQUEST_CHANGES loop-back:
// review(REQUEST_CHANGES) → implement (fix-loop).
func TestSB_ReviewRequestChanges(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to review via the happy path through commit_gate.
	workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "commit_gate", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// review(REQUEST_CHANGES) → implement
	dec := workflow.DecideNextNode(graph, "review", sbOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implement" {
		t.Fatalf("review(REQUEST_CHANGES)→implement: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Invariant B.6: review BLOCK ─────────────────────────────────────────────

// TestSB_ReviewBlock exercises the BLOCK path:
// review(BLOCK) → close-needs-attention (terminal).
func TestSB_ReviewBlock(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to review.
	workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "commit_gate", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// review(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "review", sbOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("review(BLOCK)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Invariant B.7: review unconditional fallback ────────────────────────────

// TestSB_ReviewFallback exercises the unconditional fallback from review:
// an unrecognized label falls through to close-needs-attention.
func TestSB_ReviewFallback(t *testing.T) {
	graph := sbLoadGraph(t)
	run := sbRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to review.
	workflow.DecideNextNode(graph, "start", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implement", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "commit_gate", sbOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Unrecognized label → unconditional fallback → close-needs-attention.
	dec := workflow.DecideNextNode(graph, "review", sbOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("review(fallback)→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}
