package scenario_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow"
)

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
		WorkflowID:      mustWorkflowID(t, uuid.Must(uuid.NewV7()).String()),
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

	dec := workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("start→author: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_critic" {
		t.Fatalf("r1_build→r1_critic: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r1_critic→integrate_r1: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_adversary" {
		t.Fatalf("r2_skeptic→r2_adversary: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r2" {
		t.Fatalf("r2_adversary→integrate_r2: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r2", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("integrate_r2→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	dec := workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("start→author: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build (1st): %+v", dec)
	}

	if _, err := cycles.Increment(run.RunID, "r1_build", "author", nil); err != nil {
		t.Fatalf("pre-fill cycle counter r1_build\u2192author: %v", err)
	}
	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("r1_build→author (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_critic" {
		t.Fatalf("r1_build→r1_critic: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r1_critic→integrate_r1: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_adversary" {
		t.Fatalf("r2_skeptic→r2_adversary: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r2" {
		t.Fatalf("r2_adversary→integrate_r2: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r2", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("integrate_r2→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	if _, err := cycles.Increment(run.RunID, "r1_critic", "author", nil); err != nil {
		t.Fatalf("pre-fill cycle counter r1_critic\u2192author: %v", err)
	}
	dec := workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("r1_critic→author (RC): Advance=%v NextNodeID=%q, want author", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_build" {
		t.Fatalf("author→r1_build (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r1_critic" {
		t.Fatalf("r1_build→r1_critic (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r1_critic→integrate_r1: Advance=%v NextNodeID=%q, want integrate_r1", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic: %+v", dec)
	}
}

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	if _, err := cycles.Increment(run.RunID, "r2_skeptic", "integrate_r1", nil); err != nil {
		t.Fatalf("pre-fill cycle counter r2_skeptic\u2192integrate_r1: %v", err)
	}
	dec := workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r2_skeptic→integrate_r1 (RC): Advance=%v NextNodeID=%q, want integrate_r1",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_adversary" {
		t.Fatalf("r2_skeptic→r2_adversary: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r2" {
		t.Fatalf("r2_adversary→integrate_r2: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r2", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("integrate_r2→close: %+v", dec)
	}
}

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	if _, err := cycles.Increment(run.RunID, "r2_adversary", "integrate_r1", nil); err != nil {
		t.Fatalf("pre-fill cycle counter r2_adversary\u2192integrate_r1: %v", err)
	}
	dec := workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "integrate_r1" {
		t.Fatalf("r2_adversary→integrate_r1 (RC): Advance=%v NextNodeID=%q, want integrate_r1",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "r2_skeptic" {
		t.Fatalf("integrate_r1→r2_skeptic (after R2 RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

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

	dec := workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("r1_build→close-needs-attention (BLOCK): Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "integrate_r1", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r2_skeptic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	dec := workflow.DecideNextNode(graph, "r2_adversary", srcOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("r2_adversary→close-needs-attention (BLOCK): Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "r1_build", "author", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter r1_build\u2192author: %v", err)
		}
	}

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "r1_build", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "r1_critic", srcOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

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

	workflow.DecideNextNode(graph, "start", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", srcOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

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
