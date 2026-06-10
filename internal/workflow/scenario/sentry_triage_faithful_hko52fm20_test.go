package scenario_test

// sentry_triage_faithful_hko52fm20_test.go — scenario tests for
// specs/examples/sentry-triage-faithful.dot.
//
// SOON workflow #18: Sentry issue triage via investigate→confidence→dedup→create-issue.
// The defining characteristic: all three agentic nodes are non-committing (hk-69asi);
// the confidence and dedup decisions are deterministic shell gates (hk-l8rpd).
//
// Four named scenarios (S2 path obligations):
//   1. happy-path            → full arc → close (issue created)
//   2. low-confidence        → assess_confidence(FAIL) → close-skip (fallback)
//   3. duplicate-issue       → check_dedup(FAIL) → close-skip (fallback)
//   4. init-unconditional    → init(FAIL) → investigate (unconditional advance)
//
// S2 obligations exercised:
//   - Non-committing spine: init→investigate→assess_confidence always advances
//     unconditionally (no branching, no fallback on these single-edge nodes).
//   - Tool-node status gate: assess_confidence SUCCESS advances to dedup_check;
//     FAIL falls through to close-skip via the WG-011 unconditional fallback.
//   - Tool-node status gate: check_dedup SUCCESS advances to create_issue;
//     FAIL falls through to close-skip via the WG-011 unconditional fallback.
//   - Non-committing closure: create_issue→close is unconditional.
//   - Terminal-by-identity: close and close-skip are both declared terminal nodes.
//   - Unconditional advance under failure: init→investigate fires regardless of
//     init's outcome (the only edge is unconditional).
//
// Spec refs:
//   - /tmp/sdlc-corpus/_final.md §18 (sentry-triage-faithful topology)
//   - specs/workflow-graph.md WG-001/WG-002 — node-type closed enum
//   - specs/workflow-graph.md WG-010 — five-step cascade
//   - specs/workflow-graph.md WG-011 — unconditional-edge fallback invariant
//   - specs/workflow-graph.md WG-041 — non_committing attribute (hk-69asi)
//   - specs/examples/authoring-notes.md §1 — non_committing
//
// Bead ref: hk-o52fm.20.
// Helper prefix: stf (per implementer-protocol.md §Helper-prefix discipline).

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

// ── fixtures ─────────────────────────────────────────────────────────────────

func stfDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "sentry-triage-faithful.dot")
	//nolint:gosec // G304: path is test-internal, constructed from a fixed relative anchor
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("stfDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func stfRun(t *testing.T) *core.Run {
	t.Helper()
	return &core.Run{
		RunID:           core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      core.WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: core.WorkflowVersion("1.0"),
		Input:           core.WorkspaceRef("ws-stf-test"),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.Must(uuid.NewV7())),
		Context:         make(map[string]any),
		StartTime:       time.Now(),
	}
}

func stfOutcome(status core.OutcomeStatus) core.Outcome {
	return core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
}

func stfLoadGraph(t *testing.T) *dot.Graph {
	t.Helper()
	graph, err := workflow.LoadDotWorkflow(stfDotPath(t))
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return graph
}

// stfWalkToAssessConfidence walks start → init → investigate → assess_confidence.
// All three edges are unconditional; the outcome passed for each node does not
// affect which edge fires.
func stfWalkToAssessConfidence(t *testing.T, graph *dot.Graph, run *core.Run, cycles *core.CycleCounter) {
	t.Helper()

	// start → init
	dec := workflow.DecideNextNode(graph, "start", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "init" {
		t.Fatalf("start→init: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// init → investigate (unconditional — advances regardless of init outcome)
	dec = workflow.DecideNextNode(graph, "init", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "investigate" {
		t.Fatalf("init→investigate: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// investigate → assess_confidence (unconditional)
	dec = workflow.DecideNextNode(graph, "investigate", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "assess_confidence" {
		t.Fatalf("investigate→assess_confidence: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}
}

// ── Scenario 1: happy-path ────────────────────────────────────────────────────

// TestSTF_HappyPath exercises the full success arc:
// start → init → investigate → assess_confidence(SUCCESS) →
// dedup_check → check_dedup(SUCCESS) → create_issue → close.
func TestSTF_HappyPath(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	// Walk to assess_confidence (unconditional spine).
	stfWalkToAssessConfidence(t, graph, run, cycles)

	// assess_confidence(SUCCESS, HIGH/MEDIUM) → dedup_check.
	dec := workflow.DecideNextNode(graph, "assess_confidence", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "dedup_check" {
		t.Fatalf("assess_confidence(SUCCESS)→dedup_check: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// dedup_check → check_dedup (unconditional).
	dec = workflow.DecideNextNode(graph, "dedup_check", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "check_dedup" {
		t.Fatalf("dedup_check→check_dedup: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// check_dedup(SUCCESS, NOVEL) → create_issue.
	dec = workflow.DecideNextNode(graph, "check_dedup", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "create_issue" {
		t.Fatalf("check_dedup(SUCCESS)→create_issue: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// create_issue → close (unconditional).
	dec = workflow.DecideNextNode(graph, "create_issue", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("create_issue→close: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// close is terminal.
	dec = workflow.DecideNextNode(graph, "close", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 2: low-confidence ────────────────────────────────────────────────

// TestSTF_LowConfidence exercises the WG-011 unconditional fallback on
// assess_confidence: a FAIL outcome (LOW confidence, exit 1) has no matching
// conditional edge, so the unconditional fallback fires → close-skip.
func TestSTF_LowConfidence(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	// Walk to assess_confidence.
	stfWalkToAssessConfidence(t, graph, run, cycles)

	// assess_confidence(FAIL, LOW confidence) → unconditional fallback → close-skip.
	dec := workflow.DecideNextNode(graph, "assess_confidence", stfOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-skip" {
		t.Fatalf("assess_confidence(FAIL)→close-skip: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-skip is terminal.
	dec = workflow.DecideNextNode(graph, "close-skip", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-skip: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 3: duplicate-issue ──────────────────────────────────────────────

// TestSTF_DuplicateIssue exercises the WG-011 unconditional fallback on
// check_dedup: a FAIL outcome (DUPLICATE, exit 1) has no matching conditional
// edge, so the unconditional fallback fires → close-skip.
func TestSTF_DuplicateIssue(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	// Walk to assess_confidence and advance with SUCCESS (HIGH/MEDIUM).
	stfWalkToAssessConfidence(t, graph, run, cycles)
	dec := workflow.DecideNextNode(graph, "assess_confidence", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "dedup_check" {
		t.Fatalf("assess_confidence(SUCCESS)→dedup_check: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// dedup_check → check_dedup (unconditional).
	dec = workflow.DecideNextNode(graph, "dedup_check", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "check_dedup" {
		t.Fatalf("dedup_check→check_dedup: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// check_dedup(FAIL, DUPLICATE) → unconditional fallback → close-skip.
	dec = workflow.DecideNextNode(graph, "check_dedup", stfOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-skip" {
		t.Fatalf("check_dedup(FAIL)→close-skip: Advance=%v NextNodeID=%q",
			dec.Advance, dec.NextNodeID)
	}

	// close-skip is terminal.
	dec = workflow.DecideNextNode(graph, "close-skip", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-skip: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── Scenario 4: init-unconditional ───────────────────────────────────────────

// TestSTF_InitUnconditional demonstrates that the init→investigate edge is
// unconditional: a FAIL outcome from init (e.g. Sentry CLI unavailable) still
// advances to investigate. The topology delegates failure detection to
// investigate (which will find no .ai/issue.json) and then to assess_confidence's
// fallback.
func TestSTF_InitUnconditional(t *testing.T) {
	graph := stfLoadGraph(t)
	run := stfRun(t)
	cycles := core.NewCycleCounter()

	// start → init.
	dec := workflow.DecideNextNode(graph, "start", stfOutcome(core.OutcomeStatusSuccess), run, cycles)
	if !dec.Advance || dec.NextNodeID != "init" {
		t.Fatalf("start→init: Advance=%v NextNodeID=%q", dec.Advance, dec.NextNodeID)
	}

	// init(FAIL) → investigate: unconditional edge fires even on FAIL.
	dec = workflow.DecideNextNode(graph, "init", stfOutcome(core.OutcomeStatusFail), run, cycles)
	if !dec.Advance || dec.NextNodeID != "investigate" {
		t.Fatalf("init(FAIL)→investigate: Advance=%v NextNodeID=%q (want unconditional advance)",
			dec.Advance, dec.NextNodeID)
	}
}
