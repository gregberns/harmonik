package scenario_test

// dependency_cycle_fix_loop_test.go — scenario tests for
// specs/examples/dependency-cycle-fix-loop.dot.
//
// Five named scenarios:
//   1. acyclic-on-first-check       → start→cycle_check(ACYCLIC)→close (terminal)
//   2. cycle-once-then-acyclic      → cycle_check(CYCLE)→fix_cycle→cycle_check(ACYCLIC)→close
//   3. structural-failure           → cycle_check(FAIL,structural)→close-needs-attention
//   4. cap-hit-fallback             → 3× CYCLE traversals → cap-hit failure
//   5. unrecognized-label-fallback  → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §10 (dependency-cycle-fix-loop topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-019 (author-minted preferred_label values)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: dcfl (per implementer-protocol.md §Helper-prefix discipline).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

func dcflDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "dependency-cycle-fix-loop.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("dcflDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func dcflRun(t *testing.T) *core.Run {
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

func dcflOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

func dcflFailOutcome(fc core.FailureClass) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusFail, Kind: core.OutcomeKindDefault}
	o.FailureClass = &fc
	return o
}

// ── Scenario 1: acyclic-on-first-check ───────────────────────────────────────

// TestDCFL_AcyclicOnFirstCheck exercises the happy path:
// start → cycle_check(ACYCLIC) → close (terminal).
func TestDCFL_AcyclicOnFirstCheck(t *testing.T) {
	dotPath := dcflDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dcflRun(t)
	cycles := core.NewCycleCounter()

	// start → cycle_check
	dec := workflow.DecideNextNode(graph, "start", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("start→cycle_check: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// cycle_check(ACYCLIC) → close
	dec = workflow.DecideNextNode(graph, "cycle_check", dcflOutcome(core.OutcomeStatusSuccess, "ACYCLIC"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("cycle_check→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: cycle-once-then-acyclic ──────────────────────────────────────

// TestDCFL_CycleOnceThenAcyclic exercises one detect-fix-recheck iteration:
// start → cycle_check(CYCLE) → fix_cycle → cycle_check(ACYCLIC) → close.
func TestDCFL_CycleOnceThenAcyclic(t *testing.T) {
	dotPath := dcflDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dcflRun(t)
	cycles := core.NewCycleCounter()

	// start → cycle_check
	dec := workflow.DecideNextNode(graph, "start", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("start→cycle_check: %+v", dec)
	}

	// cycle_check(CYCLE) → fix_cycle
	dec = workflow.DecideNextNode(graph, "cycle_check", dcflOutcome(core.OutcomeStatusSuccess, "CYCLE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_cycle" {
		t.Fatalf("cycle_check→fix_cycle: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Increment cycle counter for the fix_cycle→cycle_check back-edge.
	cycles.Increment(run.RunID, "fix_cycle", "cycle_check", nil)

	// fix_cycle → cycle_check (unconditional, traversal_cap=3)
	dec = workflow.DecideNextNode(graph, "fix_cycle", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("fix_cycle→cycle_check: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// cycle_check(ACYCLIC) → close
	dec = workflow.DecideNextNode(graph, "cycle_check", dcflOutcome(core.OutcomeStatusSuccess, "ACYCLIC"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("cycle_check→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: structural failure ───────────────────────────────────────────

// TestDCFL_StructuralFailure exercises the failure_class routing:
// cycle_check(FAIL, failure_class=structural) → close-needs-attention (terminal).
func TestDCFL_StructuralFailure(t *testing.T) {
	dotPath := dcflDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dcflRun(t)
	cycles := core.NewCycleCounter()

	// start → cycle_check
	dec := workflow.DecideNextNode(graph, "start", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("start→cycle_check: %+v", dec)
	}

	// cycle_check(FAIL, structural) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "cycle_check", dcflFailOutcome(core.FailureClassStructural), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("cycle_check→close-needs-attention: Advance=%v NextNodeID=%q, want close-needs-attention",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: cap-hit fallback ─────────────────────────────────────────────

// TestDCFL_CapHitFallback exercises WG-028/EM-043: when the fix_cycle→cycle_check
// back-edge's traversal_cap (3) is exhausted, the edge is suppressed and the
// cascade reports a cap-hit failure.
func TestDCFL_CapHitFallback(t *testing.T) {
	dotPath := dcflDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dcflRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → cycle_check → fix_cycle.
	workflow.DecideNextNode(graph, "start", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "cycle_check", dcflOutcome(core.OutcomeStatusSuccess, "CYCLE"), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of fix_cycle→cycle_check.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "fix_cycle", "cycle_check", &cap)
	}

	// With the traversal cap exhausted, the back-edge is suppressed; the cascade
	// reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "fix_cycle", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
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

// ── Scenario 5: unrecognized label → unconditional fallback ──────────────────

// TestDCFL_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when cycle_check emits a label that matches no conditional edge, the cascade
// falls through to the unconditional fallback → close-needs-attention.
func TestDCFL_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := dcflDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dcflRun(t)
	cycles := core.NewCycleCounter()

	// start → cycle_check
	dec := workflow.DecideNextNode(graph, "start", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cycle_check" {
		t.Fatalf("start→cycle_check: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "cycle_check", dcflOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", dcflOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
