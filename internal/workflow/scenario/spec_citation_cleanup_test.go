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

func sccDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "spec-citation-cleanup.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("sccDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func sccRun(t *testing.T) *core.Run {
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

func sccOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestSCC_FullHappyPath exercises the fully successful path through both phases:
//
//	start → author → content_review(APPROVE) → citation_fixer(SUCCESS)
//	→ citation_verify(APPROVE) → close
func TestSCC_FullHappyPath(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("start→author: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "content_review" {
		t.Fatalf("author→content_review: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "citation_fixer" {
		t.Fatalf("content_review→citation_fixer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "citation_verify" {
		t.Fatalf("citation_fixer→citation_verify: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("citation_verify→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_ContentReviewRCLoopsToAuthor exercises the content review back-edge:
//
//	start → author → content_review(RC) → author → content_review(APPROVE)
//	→ citation_fixer(SUCCESS) → citation_verify(APPROVE) → close
func TestSCC_ContentReviewRCLoopsToAuthor(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("start→author: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "content_review" {
		t.Fatalf("author→content_review (1st): %+v", dec)
	}

	if _, err := cycles.Increment(run.RunID, "content_review", "author", nil); err != nil {
		t.Fatalf("pre-fill cycle counter content_review\u2192author: %v", err)
	}
	dec = workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "author" {
		t.Fatalf("content_review→author (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "content_review" {
		t.Fatalf("author→content_review (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "citation_fixer" {
		t.Fatalf("content_review→citation_fixer: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "citation_verify" {
		t.Fatalf("citation_fixer→citation_verify: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("citation_verify→close: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "close", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_ContentReviewBlock exercises the Phase 1 BLOCK path:
//
//	start → author → content_review(BLOCK) → close-needs-attention (terminal)
func TestSCC_ContentReviewBlock(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	dec := workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("content_review→close-needs-attention (BLOCK): Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_ContentReviewCapHit exercises WG-028/EM-043: when the
// content_review→author back-edge traversal_cap (3) is exhausted, the
// conditional edge is suppressed and the cascade reports a cap-hit failure.
func TestSCC_ContentReviewCapHit(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "content_review", "author", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter content_review\u2192author: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// TestSCC_ContentReviewUnrecognizedLabel exercises the WG-011 unconditional
// fallback at content_review: when the reviewer emits a label that matches no
// conditional edge, the cascade falls through to the unconditional fallback
// → close-needs-attention.
func TestSCC_ContentReviewUnrecognizedLabel(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	dec := workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_CitationFixerFailureFallback exercises the commit-gated handoff:
// when citation_fixer returns non-SUCCESS, the outcome.status == 'SUCCESS'
// condition is not met and the unconditional fallback routes to close-needs-attention.
func TestSCC_CitationFixerFailureFallback(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)

	dec := workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusFail, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("citation_fixer failure fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("citation_fixer failure fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_CitationVerifyRCLoopsToFixer exercises the tight fixer↔verifier
// sub-loop: citation_verify(RC) → citation_fixer(SUCCESS) → citation_verify(APPROVE)
// → close. Verifies the loop target is citation_fixer, NOT author.
func TestSCC_CitationVerifyRCLoopsToFixer(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	if _, err := cycles.Increment(run.RunID, "citation_verify", "citation_fixer", nil); err != nil {
		t.Fatalf("pre-fill cycle counter citation_verify\u2192citation_fixer: %v", err)
	}
	dec := workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "citation_fixer" {
		t.Fatalf("citation_verify→citation_fixer (RC): Advance=%v NextNodeID=%q, want citation_fixer",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "citation_verify" {
		t.Fatalf("citation_fixer→citation_verify (2nd): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("citation_verify→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_CitationVerifyBlock exercises the Phase 2 BLOCK path:
//
//	... → citation_verify(BLOCK) → close-needs-attention (terminal)
func TestSCC_CitationVerifyBlock(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	dec := workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("citation_verify→close-needs-attention (BLOCK): Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestSCC_CitationVerifyCapHit exercises WG-028/EM-043 on the citation sub-loop:
// when the citation_verify→citation_fixer back-edge traversal_cap (3) is
// exhausted, the conditional edge is suppressed and the cascade reports a
// cap-hit failure.
func TestSCC_CitationVerifyCapHit(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "citation_verify", "citation_fixer", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter citation_verify\u2192citation_fixer: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// TestSCC_CitationVerifyUnrecognizedLabel exercises the WG-011 unconditional
// fallback at citation_verify: an unrecognized label falls through to
// close-needs-attention.
func TestSCC_CitationVerifyUnrecognizedLabel(t *testing.T) {
	dotPath := sccDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := sccRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "author", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "content_review", sccOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	workflow.DecideNextNode(graph, "citation_fixer", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	dec := workflow.DecideNextNode(graph, "citation_verify", sccOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID=%q, want close-needs-attention", dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", sccOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
