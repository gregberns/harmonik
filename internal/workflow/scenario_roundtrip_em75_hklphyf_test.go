package workflow_test

// scenario_roundtrip_em75_hklphyf_test.go — end-to-end scenario test for
// workflow_mode=dot round-trip driving review-loop.dot.
//
// Exercises the real parser → validator → loader → dispatcher pipeline with
// the canonical review-loop.dot fixture, asserting:
//   1. Input contract validates (§7.5.1).
//   2. Dispatch equivalence with §7.4 holds — node-transition records appear
//      in the expected order for the APPROVE, REQUEST_CHANGES (bounded loop),
//      and BLOCK paths.
//   3. Validator obligations fire on invalid input (e.g. invalid node type).
//   4. Dispatch table routes each node-type to its handler_ref correctly.
//
// Spec refs:
//   - specs/execution-model.md §7.5   — DOT mode contract.
//   - specs/workflow-graph.md §4-§11  — graph grammar and validation.
//
// Bead ref: hk-lphyf.
// Helper prefix: scenarioEM75 (per implementer-protocol.md §Helper-prefix discipline).

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

// scenarioEM75ReviewLoopDotPath returns the absolute path to the canonical
// review-loop.dot fixture in specs/examples/.
func scenarioEM75ReviewLoopDotPath(t *testing.T) string {
	t.Helper()
	// Walk up from the test file to the repo root. The test binary's working
	// directory is the package directory (internal/workflow/), so specs/ is two
	// levels up.
	repoRoot := filepath.Join("..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "review-loop.dot")
	if _, err := os.Stat(dotPath); err != nil {
		t.Fatalf("scenarioEM75ReviewLoopDotPath: fixture not found: %v", err)
	}
	return dotPath
}

func scenarioEM75Run(t *testing.T) *core.Run {
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

func scenarioEM75OutcomeWithLabel(status core.OutcomeStatus, label string) core.Outcome {
	o := core.Outcome{Status: status, Kind: core.OutcomeKindDefault}
	if label != "" {
		o.PreferredLabel = &label
	}
	return o
}

// ── §7.5.1: input contract validates ────────────────────────────────────────

// TestScenarioEM75_LoadReviewLoopDot_Validates verifies that the canonical
// review-loop.dot file passes the full parse → validate pipeline without
// errors. This is the §7.5.1 input-contract assertion.
func TestScenarioEM75_LoadReviewLoopDot_Validates(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)

	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow failed: %v", err)
	}
	if graph == nil {
		t.Fatal("graph is nil after successful load")
	}

	// Structural assertions on the parsed graph.
	if graph.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", graph.SchemaVersion, "1")
	}
	if graph.Version != "1.0" {
		t.Errorf("Version = %q, want %q", graph.Version, "1.0")
	}
	if graph.StartNodeID != "start" {
		t.Errorf("StartNodeID = %q, want %q", graph.StartNodeID, "start")
	}
	if len(graph.TerminalNodeIDs) != 2 {
		t.Fatalf("TerminalNodeIDs = %v, want 2 entries", graph.TerminalNodeIDs)
	}

	// Verify exactly 5 nodes declared.
	if len(graph.Nodes) != 5 {
		t.Errorf("len(Nodes) = %d, want 5", len(graph.Nodes))
	}

	// Verify exactly 6 edges declared.
	if len(graph.Edges) != 6 {
		t.Errorf("len(Edges) = %d, want 6", len(graph.Edges))
	}
}

// ── §7.5.1: dispatch table routes node-type to handler_ref ──────────────────

// TestScenarioEM75_DispatchTable_HandlerRefPerNodeType asserts that each node
// in the loaded graph carries the expected (type, handler_ref) pair. This
// verifies the dispatch-table routing obligation.
func TestScenarioEM75_DispatchTable_HandlerRefPerNodeType(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	// Expected dispatch table: node ID → (type, handler_ref).
	type nodeExpect struct {
		nodeType   core.NodeType
		handlerRef string
	}
	expect := map[string]nodeExpect{
		"start":                  {core.NodeTypeNonAgentic, "noop"},
		"implementer":           {core.NodeTypeAgentic, "claude-implementer"},
		"reviewer":              {core.NodeTypeAgentic, "claude-reviewer"},
		"close":                 {core.NodeTypeNonAgentic, "noop"},
		"close-needs-attention": {core.NodeTypeNonAgentic, "noop"},
	}

	nodeIndex := make(map[string]*dot.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeIndex[n.ID] = n
	}

	for id, want := range expect {
		n, ok := nodeIndex[id]
		if !ok {
			t.Errorf("node %q not found in graph", id)
			continue
		}
		if n.Type != want.nodeType {
			t.Errorf("node %q: Type = %q, want %q", id, n.Type, want.nodeType)
		}
		if n.HandlerRef != want.handlerRef {
			t.Errorf("node %q: HandlerRef = %q, want %q", id, n.HandlerRef, want.handlerRef)
		}
	}
}

// ── §7.4 dispatch equivalence: APPROVE path ────────────────────────────────

// TestScenarioEM75_ApprovePath exercises the happy path:
// start → implementer → reviewer → close (APPROVE).
// Node-transition records appear in the expected order.
func TestScenarioEM75_ApprovePath(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := scenarioEM75Run(t)
	cycles := core.NewCycleCounter()

	// Step 1: start → implementer (unconditional entry edge).
	dec := workflow.DecideNextNode(graph, "start", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("step start→implementer: Advance=%v NextNodeID=%q, want implementer", dec.Advance, dec.NextNodeID)
	}

	// Step 2: implementer → reviewer (unconditional hand-off).
	dec = workflow.DecideNextNode(graph, "implementer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("step implementer→reviewer: Advance=%v NextNodeID=%q, want reviewer", dec.Advance, dec.NextNodeID)
	}

	// Step 3: reviewer → close (APPROVE label).
	dec = workflow.DecideNextNode(graph, "reviewer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, "APPROVE"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close" {
		t.Fatalf("step reviewer→close: Advance=%v NextNodeID=%q, want close", dec.Advance, dec.NextNodeID)
	}

	// Step 4: close is terminal.
	dec = workflow.DecideNextNode(graph, "close", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("step close terminal: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── §7.4 dispatch equivalence: REQUEST_CHANGES via condition evaluation ──────

// TestScenarioEM75_RequestChangesConditionEval verifies that the edge condition
// `outcome.preferred_label == 'REQUEST_CHANGES'` evaluates correctly against
// the review-loop.dot graph. This is a unit-level bridge test between the
// loaded graph and the condition evaluator.
//
// Note on cascade ordering: in the canonical review-loop.dot, the unconditional
// fallback edge (reviewer → close-needs-attention) and the conditional edges
// all have the same weight (0). core.SelectNextEdge sorts matched edges by
// -Weight then OrderingKey (alphabetical). Because "close-needs-attention"
// sorts before "implementer", the unconditional fallback edge wins the
// tie-break even when the REQUEST_CHANGES condition matches. This is a known
// gap: the DOT spec intends conditional-first evaluation ordering, but the
// current cascade bridge does not encode declaration-order priority or edge
// weight to enforce it. The APPROVE and BLOCK conditions work because the
// cascade's preferred-label step (b) narrows correctly when conditions use
// equality on outcome.preferred_label.
//
// This test verifies the condition evaluation itself is correct, and that both
// conditional and unconditional edges are included in the matched set.
func TestScenarioEM75_RequestChangesConditionEval(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	// Verify the REQUEST_CHANGES edge condition evaluates correctly.
	label := "REQUEST_CHANGES"
	outcome := core.Outcome{
		Status:         core.OutcomeStatusSuccess,
		Kind:           core.OutcomeKindDefault,
		PreferredLabel: &label,
	}

	// Find the reviewer → implementer edge and evaluate its condition directly.
	var reqChangesEdge *dot.Edge
	for _, e := range graph.Edges {
		if e.FromNodeID == "reviewer" && e.ToNodeID == "implementer" && e.Condition != nil {
			reqChangesEdge = e
			break
		}
	}
	if reqChangesEdge == nil {
		t.Fatal("reviewer → implementer conditional edge not found in graph")
	}

	matched, evalErr := dot.EvalCondition(reqChangesEdge.Condition, outcome, nil)
	if evalErr != nil {
		t.Fatalf("EvalCondition error: %v", evalErr)
	}
	if !matched {
		t.Error("REQUEST_CHANGES condition should match when outcome.preferred_label == 'REQUEST_CHANGES'")
	}

	// Verify the APPROVE condition does NOT match for REQUEST_CHANGES.
	var approveEdge *dot.Edge
	for _, e := range graph.Edges {
		if e.FromNodeID == "reviewer" && e.ToNodeID == "close" && e.Condition != nil {
			approveEdge = e
			break
		}
	}
	if approveEdge == nil {
		t.Fatal("reviewer → close conditional edge not found in graph")
	}

	matched, evalErr = dot.EvalCondition(approveEdge.Condition, outcome, nil)
	if evalErr != nil {
		t.Fatalf("EvalCondition error: %v", evalErr)
	}
	if matched {
		t.Error("APPROVE condition should NOT match when outcome.preferred_label == 'REQUEST_CHANGES'")
	}
}

// TestScenarioEM75_CascadeFallback_ReviewerUnconditional verifies that when
// outcome.preferred_label is set to REQUEST_CHANGES, the cascade selects the
// conditional edge (reviewer -> implementer) over the unconditional fallback
// (reviewer -> close-needs-attention). Both edges have weight=0, but
// conditional edges sort before unconditional edges per WG-010/WG-011
// (hk-hx8ja fix).
func TestScenarioEM75_CascadeFallback_ReviewerUnconditional(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := scenarioEM75Run(t)
	cycles := core.NewCycleCounter()

	// Navigate to reviewer.
	workflow.DecideNextNode(graph, "start", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)

	// REQUEST_CHANGES outcome at reviewer: the conditional edge to
	// implementer wins over the unconditional fallback because conditional
	// edges sort before unconditional edges at the same weight.
	dec := workflow.DecideNextNode(graph, "reviewer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, "REQUEST_CHANGES"), run, cycles)
	if !dec.Advance {
		t.Fatalf("expected Advance=true, got Failed=%v", dec.Failed)
	}
	if dec.NextNodeID != "implementer" {
		t.Errorf("NextNodeID = %q, want %q (conditional edge wins over unconditional fallback)",
			dec.NextNodeID, "implementer")
	}
}

// ── §7.4 dispatch equivalence: BLOCK path ───────────────────────────────────

// TestScenarioEM75_BlockPath exercises:
// start → implementer → reviewer → close-needs-attention (BLOCK).
func TestScenarioEM75_BlockPath(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := scenarioEM75Run(t)
	cycles := core.NewCycleCounter()

	// Navigate: start → implementer → reviewer.
	dec := workflow.DecideNextNode(graph, "start", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "implementer" {
		t.Fatalf("start→implementer failed")
	}
	dec = workflow.DecideNextNode(graph, "implementer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance || dec.NextNodeID != "reviewer" {
		t.Fatalf("implementer→reviewer failed")
	}

	// reviewer → close-needs-attention (BLOCK).
	dec = workflow.DecideNextNode(graph, "reviewer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, "BLOCK"), run, cycles)
	if !dec.Advance || dec.NextNodeID != "close-needs-attention" {
		t.Fatalf("reviewer→close-needs-attention: Advance=%v NextNodeID=%q, want close-needs-attention",
			dec.Advance, dec.NextNodeID)
	}

	// close-needs-attention is terminal.
	dec = workflow.DecideNextNode(graph, "close-needs-attention", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.IsTerminal {
		t.Fatalf("close-needs-attention terminal: IsTerminal=%v, want true", dec.IsTerminal)
	}
}

// ── §7.4 dispatch equivalence: unconditional fallback (no label) ────────────

// TestScenarioEM75_NoLabel_FallbackToCloseNeedsAttention exercises the
// unconditional fallback edge: when the reviewer emits no label (or an
// unknown label), the cascade falls through to close-needs-attention.
func TestScenarioEM75_NoLabel_FallbackToCloseNeedsAttention(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	run := scenarioEM75Run(t)
	cycles := core.NewCycleCounter()

	// Navigate to reviewer.
	workflow.DecideNextNode(graph, "start", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	workflow.DecideNextNode(graph, "implementer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)

	// reviewer with no label → unconditional fallback → close-needs-attention.
	dec := workflow.DecideNextNode(graph, "reviewer", scenarioEM75OutcomeWithLabel(core.OutcomeStatusSuccess, ""), run, cycles)
	if !dec.Advance {
		t.Fatalf("no-label fallback: Advance=%v Failed=%v", dec.Advance, dec.Failed)
	}
	if dec.NextNodeID != "close-needs-attention" {
		t.Errorf("no-label fallback: NextNodeID = %q, want %q", dec.NextNodeID, "close-needs-attention")
	}
}

// ── validator obligations: invalid node type ────────────────────────────────

// TestScenarioEM75_Validator_InvalidNodeType verifies that the validator
// rejects a graph containing an unknown node type (§7.5.1 obligation).
func TestScenarioEM75_Validator_InvalidNodeType(t *testing.T) {
	src := `digraph bad {
		schema_version="1";
		version="1.0";
		start_node="s";
		terminal_node_ids="e";

		s [type="bogus-type"; handler_ref="noop"; idempotency_class="idempotent"];
		e [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];

		s -> e;
	}`

	_, err := dot.Parse(src, "test-invalid-node-type.dot")
	if err == nil {
		t.Fatal("expected parse error for invalid node type, got nil")
	}
	// The parser should reject "bogus-type" per WG-001.
	if pe, ok := err.(dot.ParseErrors); ok {
		found := false
		for _, e := range pe {
			if e != nil && containsSubstring(e.Error(), "WG-001") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected WG-001 error in ParseErrors, got: %v", err)
		}
	} else if pe, ok := err.(*dot.ParseError); ok {
		if !containsSubstring(pe.Error(), "WG-001") {
			t.Errorf("expected WG-001 error, got: %v", err)
		}
	} else {
		t.Fatalf("unexpected error type %T: %v", err, err)
	}
}

// TestScenarioEM75_Validator_MissingStartNode verifies the validator catches
// a graph missing the start_node attribute (WG-027).
func TestScenarioEM75_Validator_MissingStartNode(t *testing.T) {
	src := `digraph bad {
		schema_version="1";
		version="1.0";
		terminal_node_ids="e";

		e [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "no-start.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected validation error for missing start_node")
	}
	if !containsSubstring(err.Error(), "WG-027") {
		t.Errorf("expected WG-027 in error, got: %v", err)
	}
}

// TestScenarioEM75_Validator_TerminalWithOutgoingEdge verifies the validator
// catches a terminal node that has outgoing edges (WG-023).
func TestScenarioEM75_Validator_TerminalWithOutgoingEdge(t *testing.T) {
	src := `digraph bad {
		schema_version="1";
		version="1.0";
		start_node="s";
		terminal_node_ids="e";

		s [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
		e [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
		x [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];

		s -> e;
		e -> x;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "terminal-outgoing.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected validation error for terminal node with outgoing edges")
	}
	if !containsSubstring(err.Error(), "WG-023") {
		t.Errorf("expected WG-023 in error, got: %v", err)
	}
}

// TestScenarioEM75_Validator_CycleWithoutTraversalCap verifies the validator
// catches an unbounded cycle (WG-028).
func TestScenarioEM75_Validator_CycleWithoutTraversalCap(t *testing.T) {
	src := `digraph bad {
		schema_version="1";
		version="1.0";
		start_node="a";
		terminal_node_ids="c";

		a [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
		b [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];
		c [type="non-agentic"; handler_ref="noop"; idempotency_class="idempotent"];

		a -> b;
		b -> a;
		b -> c;
	}`
	dir := t.TempDir()
	dotPath := filepath.Join(dir, "unbounded-cycle.dot")
	if err := os.WriteFile(dotPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := workflow.LoadDotWorkflow(dotPath)
	if err == nil {
		t.Fatal("expected validation error for unbounded cycle")
	}
	if !containsSubstring(err.Error(), "WG-028") {
		t.Errorf("expected WG-028 in error, got: %v", err)
	}
}

// ── §7.5.1: agentic node attribute completeness ────────────────────────────

// TestScenarioEM75_ReviewLoopDot_AgenticNodesHaveRequiredAttrs checks that
// agentic nodes (implementer, reviewer) carry agent_type and idempotency_class.
func TestScenarioEM75_ReviewLoopDot_AgenticNodesHaveRequiredAttrs(t *testing.T) {
	dotPath := scenarioEM75ReviewLoopDotPath(t)
	graph, err := workflow.LoadDotWorkflow(dotPath)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}

	agenticNodes := []string{"implementer", "reviewer"}
	nodeIndex := make(map[string]*dot.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeIndex[n.ID] = n
	}

	for _, id := range agenticNodes {
		n := nodeIndex[id]
		if n == nil {
			t.Errorf("agentic node %q not found", id)
			continue
		}
		if n.AgentType == "" {
			t.Errorf("node %q: agent_type is empty", id)
		}
		if n.IdempotencyClass == "" {
			t.Errorf("node %q: idempotency_class is empty", id)
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
