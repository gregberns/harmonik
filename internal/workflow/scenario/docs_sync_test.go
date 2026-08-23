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

func dsDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "docs-sync.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("dsDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func dsRun(t *testing.T) *core.Run {
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

func dsOutcome(label string) core.Outcome {
	o := core.Outcome{Status: core.OutcomeStatusSuccess, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// TestDS_ApproveOnFirstPass exercises the happy path:
// start → change_code → update_docs → review_sync(APPROVE) → close (terminal).
func TestDS_ApproveOnFirstPass(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review_sync→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", dsOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestDS_RequestChangesThenApprove exercises the docs-only loop:
// start → change_code → update_docs → review_sync(REQUEST_CHANGES) →
// update_docs → review_sync(APPROVE) → close.
func TestDS_RequestChangesThenApprove(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	if _, err := cycles.Increment(run.RunID, "review_sync", "update_docs", nil); err != nil {
		t.Fatalf("pre-fill cycle counter review_sync\u2192update_docs: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("review_sync→update_docs (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review_sync→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", dsOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestDS_CodeChangeThenApprove exercises the full re-spine loop (WG-019):
// start → change_code → update_docs → review_sync(CODE_CHANGE) →
// change_code → update_docs → review_sync(APPROVE) → close.
func TestDS_CodeChangeThenApprove(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	if _, err := cycles.Increment(run.RunID, "review_sync", "change_code", nil); err != nil {
		t.Fatalf("pre-fill cycle counter review_sync\u2192change_code: %v", err)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("CODE_CHANGE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("review_sync→change_code: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs (2nd): %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync (2nd): %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review_sync→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close", dsOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestDS_BlockOnFirst exercises:
// start → change_code → update_docs → review_sync(BLOCK) → close-needs-attention (terminal).
func TestDS_BlockOnFirst(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("review_sync→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", dsOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// TestDS_CapHitRequestChanges exercises WG-028/EM-043 on the review_sync→update_docs
// back-edge (cap=3): when exhausted, emitting REQUEST_CHANGES reports a cap-hit failure.
func TestDS_CapHitRequestChanges(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)

	traversalCap := 3
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "review_sync", "update_docs", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter review_sync\u2192update_docs: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "review_sync", dsOutcome("REQUEST_CHANGES"), run, cycles)
	if !dec.Failed {
		t.Fatalf("expected Failed=true on cap-hit (REQUEST_CHANGES), got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("expected CompletionReason=cap_hit, got %q (%+v)", dec.CompletionReason, dec)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}

// TestDS_CapHitCodeChange exercises WG-028/EM-043 on the review_sync→change_code
// back-edge (cap=2): when exhausted, emitting CODE_CHANGE reports a cap-hit failure.
func TestDS_CapHitCodeChange(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)

	traversalCap := 2
	for i := 0; i < traversalCap; i++ {
		if _, err := cycles.Increment(run.RunID, "review_sync", "change_code", &traversalCap); err != nil {
			t.Fatalf("pre-fill cycle counter review_sync\u2192change_code: %v", err)
		}
	}

	dec := workflow.DecideNextNode(graph, "review_sync", dsOutcome("CODE_CHANGE"), run, cycles)
	if !dec.Failed {
		t.Fatalf("expected Failed=true on cap-hit (CODE_CHANGE), got: %+v", dec)
	}
	if dec.CompletionReason != "cap_hit" {
		t.Fatalf("expected CompletionReason=cap_hit, got %q (%+v)", dec.CompletionReason, dec)
	}
	if dec.FailureClass != core.FailureClassCompilationLoop {
		t.Fatalf("expected FailureClass=compilation_loop, got %q", dec.FailureClass)
	}
}

// TestDS_UnrecognizedLabelFallback exercises the WG-011 unconditional fallback:
// when review_sync emits a label that matches no conditional edge, the cascade
// falls through to the unconditional fallback → close-needs-attention.
func TestDS_UnrecognizedLabelFallback(t *testing.T) {
	dotPath := dsDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := dsRun(t)
	cycles := core.NewCycleCounter()

	dec := workflow.DecideNextNode(graph, "start", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome("UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	dec = workflow.DecideNextNode(graph, "close-needs-attention", dsOutcome(""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
