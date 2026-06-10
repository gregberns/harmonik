package scenario_test

// docs_sync_test.go — scenario tests for specs/examples/docs-sync.dot.
//
// Seven named scenarios:
//   1. approve-on-first-pass         → spine runs once; review_sync(APPROVE) → close (terminal)
//   2. REQUEST_CHANGES-then-approve  → 1× loop-back to update_docs, then APPROVE → close
//   3. CODE_CHANGE-then-approve      → 1× loop-back to change_code (full re-spine), then APPROVE → close
//   4. BLOCK-on-first               → review_sync(BLOCK) → close-needs-attention (terminal)
//   5. cap-hit-REQUEST_CHANGES       → 3× REQUEST_CHANGES → cap-hit failure (cap=3)
//   6. cap-hit-CODE_CHANGE           → 2× CODE_CHANGE     → cap-hit failure (cap=2)
//   7. unrecognized-label-fallback   → unknown label → unconditional fallback → close-needs-attention
//
// Spec refs:
//   - docs/sdlc-workflow-corpus.md §11 (docs-sync topology)
//   - specs/workflow-graph.md  WG-010 (5-step cascade)
//   - specs/workflow-graph.md  WG-011 (unconditional-edge fallback invariant)
//   - specs/workflow-graph.md  WG-019 (arbitrary preferred_label strings)
//   - specs/workflow-graph.md  WG-028 (cycle bounding / traversal_cap)
//   - specs/execution-model.md EM-015e (no-progress / cap-hit vocabulary)
//   - specs/execution-model.md EM-043  (traversal-cap enforcement)
//
// Helper prefix: ds (per implementer-protocol.md §Helper-prefix discipline).

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
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func dsOutcome(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── Scenario 1: approve-on-first-pass ────────────────────────────────────────

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

	// start → change_code
	dec := workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// change_code → update_docs
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// update_docs → review_sync
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// review_sync(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review_sync→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: REQUEST_CHANGES then approve ─────────────────────────────────

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

	// Forward spine: start → change_code → update_docs → review_sync
	dec := workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	// Increment cycle counter for the review_sync→update_docs back-edge.
	cycles.Increment(run.RunID, "review_sync", "update_docs", nil)

	// review_sync(REQUEST_CHANGES) → update_docs
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("review_sync→update_docs (RC): Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// update_docs → review_sync (second visit)
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync (2nd): %+v", dec)
	}

	// review_sync(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review_sync→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: CODE_CHANGE then approve ─────────────────────────────────────

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

	// Forward spine: start → change_code → update_docs → review_sync
	dec := workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	// Increment cycle counter for the review_sync→change_code back-edge.
	cycles.Increment(run.RunID, "review_sync", "change_code", nil)

	// review_sync(CODE_CHANGE) → change_code
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "CODE_CHANGE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("review_sync→change_code: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// Re-run the forward spine from change_code
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs (2nd): %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync (2nd): %+v", dec)
	}

	// review_sync(APPROVE) → close
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("review_sync→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// close is terminal
	dec = workflow.DecideNextNode(graph, "close", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: BLOCK on first ───────────────────────────────────────────────

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

	// Forward spine
	dec := workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	// review_sync(BLOCK) → close-needs-attention
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("review_sync→close-needs-attention: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal
	dec = workflow.DecideNextNode(graph, "close-needs-attention", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 5: cap-hit REQUEST_CHANGES ──────────────────────────────────────

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

	// Navigate to review_sync via the forward spine.
	workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill: simulate 3 prior traversals of review_sync→update_docs (cap=3 exhausted).
	cap := 3
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "review_sync", "update_docs", &cap)
	}

	// With the traversal cap exhausted, the REQUEST_CHANGES back-edge is suppressed;
	// the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
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

// ── Scenario 6: cap-hit CODE_CHANGE ──────────────────────────────────────────

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

	// Navigate to review_sync via the forward spine.
	workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)

	// Pre-fill: simulate 2 prior traversals of review_sync→change_code (cap=2 exhausted).
	cap := 2
	for i := 0; i < cap; i++ {
		cycles.Increment(run.RunID, "review_sync", "change_code", &cap)
	}

	// With the traversal cap exhausted, the CODE_CHANGE back-edge is suppressed;
	// the cascade reports a cap-hit failure.
	dec := workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "CODE_CHANGE"), run, cycles)
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

// ── Scenario 7: unrecognized label → unconditional fallback ──────────────────

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

	// Forward spine: start → change_code → update_docs → review_sync
	dec := workflow.DecideNextNode(graph, "start", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "change_code" {
		t.Fatalf("start→change_code: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "change_code", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "update_docs" {
		t.Fatalf("change_code→update_docs: %+v", dec)
	}
	dec = workflow.DecideNextNode(graph, "update_docs", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "review_sync" {
		t.Fatalf("update_docs→review_sync: %+v", dec)
	}

	// Unrecognized label: no conditional edge matches; unconditional fallback fires.
	dec = workflow.DecideNextNode(graph, "review_sync", dsOutcome(core.OutcomeStatusSuccess, "UNKNOWN_LABEL"), run, cycles)
	if !dec.Advance {
		t.Fatalf("unrecognized-label fallback: Advance=%v Failed=%v FailureReason=%q",
			dec.Advance, dec.Failed, dec.FailureReason)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("unrecognized-label fallback: NextNodeID = %q, want %q",
			dec.NextNodeID, "close-needs-attention")
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", dsOutcome(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention: IsTerminal=%v, want true", dec.IsTerminal)
	}
}
