package scenario_test

// spec_r1_r2_cycle_test.go — scenario tests for specs/examples/spec-R1-R2-cycle.dot.
//
// Ten named scenarios:
//   1. full-happy-path              → all nodes approve/succeed on first pass → close (terminal)
//   2. r1-build-rc-loops-to-author  → r1_build(RC) → author → r1_build(APPROVE) → ... → close
//   3. r1-critic-rc-loops-to-author → r1_critic(RC) → author → r1_critic(APPROVE) → ... → close
//   4. r2-skeptic-rc-loops-to-integrate-r1  → r2_skeptic(RC) → integrate_r1 (NOT author) → ... → close
//   5. r2-adversary-rc-loops-to-integrate-r1 → r2_adversary(RC) → integrate_r1 → ... → close
//   6. r1-build-block               → r1_build(BLOCK) → close-needs-attention (terminal)
//   7. r2-adversary-block           → r2_adversary(BLOCK) → close-needs-attention (terminal)
//   8. r1-build-cap-hit             → 3× r1_build→author traversals → cap-hit failure
//   9. integrate-r1-failure-fallback → integrate_r1 non-SUCCESS → close-needs-attention
//  10. r1-build-unrecognized-label-fallback → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §7 (spec-R1-R2-cycle topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: src (per implementer-protocol.md §Helper-prefix discipline).

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

func srcDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "spec-R1-R2-cycle.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("srcDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func srcRun(t *testing.T) *core.Run {
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

func srcOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: full-happy-path ───────────────────────────────────────────────

// TestSRC_FullHappyPath exercises the fully successful path through both review
// rounds:
//
//	start → author → r1_build(APPROVE) → r1_critic(APPROVE) → integrate_r1(SUCCESS)
//	→ r2_skeptic(APPROVE) → r2_adversary(APPROVE) → integrate_r2(SUCCESS) → close
func TestSRC_FullHappyPath(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// start → author
	dec := workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("start→author: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// author → r1_build
	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// r1_build(APPROVE) → r1_critic
	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_critic" {
		t.Fatalf("r1_build→r1_critic: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// r1_critic(APPROVE) → integrate_r1
	dec = workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r1_critic→integrate_r1: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// integrate_r1(SUCCESS) → r2_skeptic
	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// r2_skeptic(APPROVE) → r2_adversary
	dec = workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_adversary" {
		t.Fatalf("r2_skeptic→r2_adversary: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// r2_adversary(APPROVE) → integrate_r2
	dec = workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r2" {
		t.Fatalf("r2_adversary→integrate_r2: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// integrate_r2(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "integrate_r2", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("integrate_r2→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: r1-build-rc-loops-to-author ──────────────────────────────────

// TestSRC_R1BuildRCLoopsToAuthor exercises the R1 build back-edge:
//
//	start → author → r1_build(RC) → author → r1_build(APPROVE) → r1_critic(APPROVE)
//	→ integrate_r1(SUCCESS) → r2_skeptic(APPROVE) → r2_adversary(APPROVE)
//	→ integrate_r2(SUCCESS) → close
func TestSRC_R1BuildRCLoopsToAuthor(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// start → author
	dec := workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("start→author: %+v", dec)
	}

	// author → r1_build (first pass)
	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build (1st): %+v", dec)
	}

	// r1_build(REQUEST_CHANGES) → author
	cycles.Increment(run.RunID, "r1_build", "author", nil)
	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("r1_build→author (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// author → r1_build (second pass)
	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build (2nd): %+v", dec)
	}

	// r1_build(APPROVE) → r1_critic
	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_critic" {
		t.Fatalf("r1_build→r1_critic: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// r1_critic(APPROVE) → integrate_r1
	dec = workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r1_critic→integrate_r1: %+v", dec)
	}

	// integrate_r1(SUCCESS) → r2_skeptic
	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic: %+v", dec)
	}

	// r2_skeptic(APPROVE) → r2_adversary
	dec = workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_adversary" {
		t.Fatalf("r2_skeptic→r2_adversary: %+v", dec)
	}

	// r2_adversary(APPROVE) → integrate_r2
	dec = workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r2" {
		t.Fatalf("r2_adversary→integrate_r2: %+v", dec)
	}

	// integrate_r2(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "integrate_r2", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("integrate_r2→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: r1-critic-rc-loops-to-author ─────────────────────────────────

// TestSRC_R1CriticRCLoopsToAuthor exercises the R1 critic back-edge:
//
//	... → r1_build(APPROVE) → r1_critic(RC) → author → r1_build(APPROVE) → r1_critic(APPROVE)
//	→ integrate_r1 → ... → close
func TestSRC_R1CriticRCLoopsToAuthor(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate spine to r1_critic.
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// r1_critic(REQUEST_CHANGES) → author (not r1_build directly)
	cycles.Increment(run.RunID, "r1_critic", "author", nil)
	dec := workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("r1_critic→author (RC): Advance=%v NextNodeID=%q, want author", dec.Advance, dec.NextNodeID)
	}

	// author → r1_build (second pass)
	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build (2nd): %+v", dec)
	}

	// r1_build(APPROVE) → r1_critic (second pass)
	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_critic" {
		t.Fatalf("r1_build→r1_critic (2nd): %+v", dec)
	}

	// r1_critic(APPROVE) → integrate_r1
	dec = workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r1_critic→integrate_r1: Advance=%v NextNodeID=%q, want integrate_r1", dec.Advance, dec.NextNodeID)
	}

	// integrate_r1(SUCCESS) → r2_skeptic
	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic: %+v", dec)
	}
}

// ── Scenario 4: r2-skeptic-rc-loops-to-integrate-r1 ──────────────────────────

// TestSRC_R2SkepticRCLoopsToIntegrateR1 verifies that R2 REQUEST_CHANGES loops
// back to integrate_r1 (the nearest author surface) — NOT to author, which would
// needlessly re-run the R1 reviewer pair.
func TestSRC_R2SkepticRCLoopsToIntegrateR1(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to r2_skeptic (past R1 round).
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// r2_skeptic(REQUEST_CHANGES) → integrate_r1 (NOT author)
	cycles.Increment(run.RunID, "r2_skeptic", "integrate_r1", nil)
	dec := workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r2_skeptic→integrate_r1 (RC): Advance=%v NextNodeID=%q, want integrate_r1",
			dec.Advance, dec.NextNodeID)
	}

	// integrate_r1(SUCCESS) → r2_skeptic again
	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// r2_skeptic(APPROVE) → r2_adversary
	dec = workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_adversary" {
		t.Fatalf("r2_skeptic→r2_adversary: %+v", dec)
	}

	// r2_adversary(APPROVE) → integrate_r2
	dec = workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r2" {
		t.Fatalf("r2_adversary→integrate_r2: %+v", dec)
	}

	// integrate_r2(SUCCESS) → close
	dec = workflow.DecideNextNode(graph, "integrate_r2", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("integrate_r2→close: %+v", dec)
	}
}

// ── Scenario 5: r2-adversary-rc-loops-to-integrate-r1 ────────────────────────

// TestSRC_R2AdversaryRCLoopsToIntegrateR1 verifies that r2_adversary REQUEST_CHANGES
// also loops to integrate_r1 (not author), bypassing R1.
func TestSRC_R2AdversaryRCLoopsToIntegrateR1(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to r2_adversary.
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// r2_adversary(REQUEST_CHANGES) → integrate_r1 (NOT author)
	cycles.Increment(run.RunID, "r2_adversary", "integrate_r1", nil)
	dec := workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r2_adversary→integrate_r1 (RC): Advance=%v NextNodeID=%q, want integrate_r1",
			dec.Advance, dec.NextNodeID)
	}

	// integrate_r1(SUCCESS) → r2_skeptic (re-enters R2 from the top)
	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic (after R2 RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 6: r1-build-block ────────────────────────────────────────────────

// TestSRC_R1BuildBlock exercises the R1 BLOCK path:
//
//	start → author → r1_build(BLOCK) → close-needs-attention (terminal)
func TestSRC_R1BuildBlock(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// r1_build(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("r1_build→close-needs-attention (BLOCK): Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 7: r2-adversary-block ───────────────────────────────────────────

// TestSRC_R2AdversaryBlock exercises the R2 adversary BLOCK path:
//
//	... → r2_adversary(BLOCK) → close-needs-attention (terminal)
func TestSRC_R2AdversaryBlock(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to r2_adversary.
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// r2_adversary(BLOCK) → close-needs-attention
	dec := workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("r2_adversary→close-needs-attention (BLOCK): Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 8: r1-build-cap-hit ─────────────────────────────────────────────

// TestSRC_R1BuildCapHit exercises WG-028/EM-043: when the r1_build→author
// back-edge traversal_cap (3) is exhausted, the conditional edge is suppressed
// and the cascade reports a cap-hit failure.
func TestSRC_R1BuildCapHit(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → author → r1_build.
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill cycle counter: simulate 3 prior traversals of r1_build→author.
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "r1_build", "author", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is suppressed;
	// the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// ── Scenario 9: integrate-r1-failure-fallback ─────────────────────────────────

// TestSRC_IntegrateR1FailureFallback exercises the integrate_r1 non-SUCCESS path:
// when integrate_r1 returns a non-SUCCESS status, no conditional edge matches
// and the unconditional fallback routes to close-needs-attention.
func TestSRC_IntegrateR1FailureFallback(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate to integrate_r1.
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	// integrate_r1 returns FAIL: SUCCESS condition not met; unconditional fallback fires.
	dec := workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("integrate_r1 failure fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("integrate_r1 failure fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 10: r1-build-unrecognized-label-fallback ────────────────────────

// TestSRC_R1BuildUnrecognizedLabelFallback exercises the WG-011 unconditional
// fallback: when r1_build emits a label that matches no conditional edge, the
// cascade falls through to the unconditional fallback → close-needs-attention.
func TestSRC_R1BuildUnrecognizedLabelFallback(t *testing.T) {
	dotPath := srcDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := srcRun(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → author → r1_build.
	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec := workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
