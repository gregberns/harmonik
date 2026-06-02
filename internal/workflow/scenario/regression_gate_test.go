package scenario_test

// regression_gate_test.go — scenario tests for specs/examples/regression-gate.dot.
//
// Six named scenarios (S2 path obligations):
//   1. happy-path-bug-reproduced   → reproduce(FAIL)→fix_bug(SUCCESS)→regression_suite(SUCCESS)→close
//   2. cannot-reproduce            → reproduce(SUCCESS)→cannot_reproduce→close-needs-attention (terminal)
//   3. reproduce-infra-fallback    → reproduce(RETRY, no condition match)→close-needs-attention (fallback)
//   4. regression-fix-loop         → reproduce(FAIL)→fix_bug→suite(FAIL+deterministic)→fix_bug→suite(SUCCESS)→close
//   5. regression-cap-hit          → reproduce(FAIL)→fix_bug→suite(FAIL+deterministic)×3→cap-hit failure
//   6. regression-transient-fallback → reproduce(FAIL)→fix_bug→suite(FAIL+transient)→close-needs-attention
//
// Key S2 obligations exercised:
//   - FAIL as forward path: reproduce FAIL → fix_bug (the expected success path)
//   - Tool-node commit-gate: outcome.status=='SUCCESS' advances from regression_suite
//   - Compound condition: outcome.status=='FAIL' && outcome.failure_class=='deterministic'
//     on regression_suite→fix_bug back-edge
//   - Traversal-cap enforcement: 3× FAIL+deterministic → cap-hit (compilation_loop)
//   - Unconditional fallback: reproduce→close-needs-attention fires on non-SUCCESS/non-FAIL
//   - Transient failure fallback: regression_suite FAIL+transient hits unconditional fallback
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §16 (regression-gate topology)
//   - specs/workflow-graph.md WG-010 (5-step cascade)
//   - specs/workflow-graph.md WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-043 (traversal-cap enforcement)
//   - specs/execution-model.md EM-057 item 7 (exit-code → outcome)
//
// Helper prefix: rg (per implementer-protocol.md §Helper-prefix discipline).

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

func rgDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "regression-gate.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("rgDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func rgRun(t *testing.T) *core.Run {
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

func rgOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// rgOutcomeFC builds a FAIL outcome carrying failure_class.
// Used for the compound condition on regression_suite→fix_bug.
func rgOutcomeFC(fc core.FailureClass) core.Outcome {
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		FailureClass: &fc,
		Kind:         core.OutcomeKindDefault,
	}
}

func rgLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(rgDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

// ── Scenario 1: happy-path-bug-reproduced ─────────────────────────────────────

// TestRG_HappyPathBugReproduced exercises the full success arc:
// start → reproduce(FAIL) → fix_bug(SUCCESS) → regression_suite(SUCCESS) → close.
// FAIL on reproduce is the *forward* path (bug confirmed present → fix it).
func TestRG_HappyPathBugReproduced(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	// start → reproduce
	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// reproduce(FAIL) → fix_bug  (FAIL is the forward path: bug reproduces)
	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_bug" {
		t.Fatalf("reproduce(FAIL)→fix_bug: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// fix_bug(SUCCESS) → regression_suite
	dec = workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "regression_suite" {
		t.Fatalf("fix_bug→regression_suite: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// regression_suite(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "regression_suite", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("regression_suite(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: cannot-reproduce ──────────────────────────────────────────────

// TestRG_CannotReproduce exercises the "bug absent" path:
// start → reproduce(SUCCESS) → cannot_reproduce → close-needs-attention.
// SUCCESS on reproduce means the test passed — bug is not present.
func TestRG_CannotReproduce(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	// start → reproduce
	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: %+v", dec)
	}

	// reproduce(SUCCESS) → cannot_reproduce  (zero exit = bug absent)
	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "cannot_reproduce" {
		t.Fatalf("reproduce(SUCCESS)→cannot_reproduce: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// cannot_reproduce → close-needs-attention
	dec = workflow.DecideNextNode(graph, "cannot_reproduce", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("cannot_reproduce→close-needs-attention: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: reproduce infra fallback ──────────────────────────────────────

// TestRG_ReproduceInfraFallback exercises the unconditional fallback on reproduce:
// start → reproduce(RETRY, neither SUCCESS nor FAIL) → close-needs-attention.
// A RETRY outcome (e.g. transient infra failure during the test probe) does not
// match either conditional edge, so the unconditional fallback fires.
func TestRG_ReproduceInfraFallback(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	// start → reproduce
	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: %+v", dec)
	}

	// reproduce(RETRY) → close-needs-attention via unconditional fallback
	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusRetry, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("reproduce(RETRY) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 4: regression suite fix-loop ─────────────────────────────────────

// TestRG_RegressionFixLoop exercises the back-edge capped loop:
// reproduce(FAIL) → fix_bug → regression_suite(FAIL+deterministic) →
// fix_bug → regression_suite(SUCCESS) → close.
// A deterministic failure on the first regression run means the fix didn't hold;
// the capped back-edge sends control back to fix_bug for a second attempt.
func TestRG_RegressionFixLoop(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	// start → reproduce
	dec := workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reproduce" {
		t.Fatalf("start→reproduce: %+v", dec)
	}

	// reproduce(FAIL) → fix_bug
	dec = workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_bug" {
		t.Fatalf("reproduce(FAIL)→fix_bug: %+v", dec)
	}

	// fix_bug → regression_suite
	dec = workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "regression_suite" {
		t.Fatalf("fix_bug→regression_suite: %+v", dec)
	}

	// regression_suite(FAIL+deterministic): fix didn't hold → back to fix_bug.
	// Increment cycle counter to model the traversal.
	cap := 3
	cycles.Increment(run.RunID, "regression_suite", "fix_bug", &cap)

	dec = workflow.DecideNextNode(graph, "regression_suite", rgOutcomeFC(core.FailureClassDeterministic), run, cycles)
	if !dec.Advance || dec.NextNodeID != "fix_bug" {
		t.Fatalf("regression_suite(FAIL+deterministic)→fix_bug: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// Second fix_bug attempt → regression_suite
	dec = workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "regression_suite" {
		t.Fatalf("fix_bug (2nd)→regression_suite: %+v", dec)
	}

	// regression_suite(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "regression_suite", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("regression_suite(SUCCESS)→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 5: regression suite cap-hit ──────────────────────────────────────

// TestRG_RegressionCapHit exercises traversal_cap enforcement on the
// regression_suite→fix_bug back-edge (cap=3):
// reproduce(FAIL) → fix_bug → regression_suite × 3 FAIL+deterministic → cap-hit failure.
func TestRG_RegressionCapHit(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to regression_suite via reproduce(FAIL) → fix_bug.
	workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail, ""), run, cycles)
	workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill the cycle counter: 3 traversals of regression_suite→fix_bug at cap=3.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "regression_suite", "fix_bug", &cap)
	}

	// With the cap exhausted, FAIL+deterministic can no longer take the back-edge.
	dec := workflow.DecideNextNode(graph, "regression_suite", rgOutcomeFC(core.FailureClassDeterministic), run, cycles)
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

// ── Scenario 6: regression suite transient fallback ───────────────────────────

// TestRG_RegressionTransientFallback exercises the unconditional fallback on
// regression_suite: a FAIL+transient outcome does NOT match the deterministic
// compound condition, so the fallback fires → close-needs-attention.
func TestRG_RegressionTransientFallback(t *testing.T) {
	graph := rgLoadGraph(t)
	run := rgRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to regression_suite.
	workflow.DecideNextNode(graph, "start", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "reproduce", rgOutcome(core.OutcomeStatusFail, ""), run, cycles)
	workflow.DecideNextNode(graph, "fix_bug", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// regression_suite(FAIL+transient): does NOT match FAIL+deterministic condition.
	// The unconditional fallback fires → close-needs-attention.
	dec := workflow.DecideNextNode(graph, "regression_suite", rgOutcomeFC(core.FailureClassTransient), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("regression_suite(FAIL+transient) fallback→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", rgOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
