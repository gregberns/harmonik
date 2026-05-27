package dot

// scenario_roundtrip_wg_test.go — DOT round-trip scenario: parse + validate +
// reserved-attribute rejection + unknown-attribute retention.
//
// Bead: hk-fiq55
//
// Coverage:
//   1. All 4 node types parse correctly (agentic, non-agentic, gate, sub-workflow).
//   2. Edge cascade ordering is preserved (declaration order).
//   3. Reserved-attribute strict policy rejects schema_version on a node (WG-031/WG-033).
//   4. Mixed unknown-attribute policy emits warning but retains AST (D9 / WG-032).
//
// The canonical fixture specs/examples/review-loop.dot only uses agentic and
// non-agentic nodes.  Assertion (1) therefore uses a synthetic fixture that
// exercises all four types.  Assertions (2)–(4) use both the canonical fixture
// and targeted synthetic inputs.
//
// Tags: mechanism

import (
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ── (1) All 4 node types parse correctly ─────────────────────────────────────

// dotFixtureAllFourNodeTypes returns a synthetic graph exercising every WG-001
// node type: agentic, non-agentic, gate, sub-workflow.
func dotFixtureAllFourNodeTypes() string {
	return `digraph all_four {
  schema_version="1";
  version="1.0";
  start_node="impl";
  terminal_node_ids="done";

  impl [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-code",
    idempotency_class="non-idempotent"
  ];

  check_gate [
    type="gate",
    gate_ref="review-gate-policy",
    handler_ref="gate-evaluator"
  ];

  inner [
    type="sub-workflow",
    sub_workflow_ref="inner-wf",
    workflow_version="1.0"
  ];

  done [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent"
  ];

  impl -> check_gate [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
  check_gate -> inner [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
  inner -> done [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
}`
}

func TestScenarioAllFourNodeTypesParse(t *testing.T) {
	g, err := Parse(dotFixtureAllFourNodeTypes(), "all_four.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if len(g.Nodes) != 4 {
		t.Fatalf("len(Nodes) = %d, want 4", len(g.Nodes))
	}

	// Build a type→node map.
	typeMap := make(map[core.NodeType]*Node, 4)
	for _, n := range g.Nodes {
		typeMap[n.Type] = n
	}

	// Agentic.
	if n, ok := typeMap[core.NodeTypeAgentic]; !ok {
		t.Error("no agentic node found")
	} else {
		if n.ID != "impl" {
			t.Errorf("agentic node ID = %q, want %q", n.ID, "impl")
		}
		if n.AgentType != "implementer" {
			t.Errorf("agentic AgentType = %q, want %q", n.AgentType, "implementer")
		}
		if n.HandlerRef != "claude-code" {
			t.Errorf("agentic HandlerRef = %q, want %q", n.HandlerRef, "claude-code")
		}
		if n.IdempotencyClass != "non-idempotent" {
			t.Errorf("agentic IdempotencyClass = %q, want %q", n.IdempotencyClass, "non-idempotent")
		}
	}

	// Gate.
	if n, ok := typeMap[core.NodeTypeGate]; !ok {
		t.Error("no gate node found")
	} else {
		if n.ID != "check_gate" {
			t.Errorf("gate node ID = %q, want %q", n.ID, "check_gate")
		}
		if n.GateRef != "review-gate-policy" {
			t.Errorf("gate GateRef = %q, want %q", n.GateRef, "review-gate-policy")
		}
		if n.HandlerRef != "gate-evaluator" {
			t.Errorf("gate HandlerRef = %q, want %q", n.HandlerRef, "gate-evaluator")
		}
	}

	// Sub-workflow.
	if n, ok := typeMap[core.NodeTypeSubWorkflow]; !ok {
		t.Error("no sub-workflow node found")
	} else {
		if n.ID != "inner" {
			t.Errorf("sub-workflow node ID = %q, want %q", n.ID, "inner")
		}
		if n.SubWorkflowRef != "inner-wf" {
			t.Errorf("sub-workflow SubWorkflowRef = %q, want %q", n.SubWorkflowRef, "inner-wf")
		}
		if n.WorkflowVersion != "1.0" {
			t.Errorf("sub-workflow WorkflowVersion = %q, want %q", n.WorkflowVersion, "1.0")
		}
	}

	// Non-agentic.
	if n, ok := typeMap[core.NodeTypeNonAgentic]; !ok {
		t.Error("no non-agentic node found")
	} else {
		if n.ID != "done" {
			t.Errorf("non-agentic node ID = %q, want %q", n.ID, "done")
		}
		if n.HandlerRef != "noop" {
			t.Errorf("non-agentic HandlerRef = %q, want %q", n.HandlerRef, "noop")
		}
		if n.IdempotencyClass != "idempotent" {
			t.Errorf("non-agentic IdempotencyClass = %q, want %q", n.IdempotencyClass, "idempotent")
		}
	}

	// Validate: no errors expected.
	diags := Validate(g)
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected validation error: %s", d)
		}
	}
}

// ── (2) Edge cascade ordering preserved ──────────────────────────────────────

// TestScenarioEdgeCascadeOrdering verifies that edges are returned in
// declaration order, which is load-bearing for the edge-cascade evaluation
// semantics (unconditional fallback must be last).
func TestScenarioEdgeCascadeOrdering(t *testing.T) {
	// Use the canonical specs/examples/review-loop.dot fixture.
	//nolint:gosec // G304: test-local constant path.
	src, err := os.ReadFile("../../../specs/examples/review-loop.dot")
	if err != nil {
		t.Skipf("specs/examples/review-loop.dot not found: %v", err)
	}

	g, parseErr := Parse(string(src), "specs/examples/review-loop.dot")
	if parseErr != nil {
		t.Fatalf("Parse: %v", parseErr)
	}

	// The canonical file declares 6 edges in this order:
	//   (1) start -> implementer          (unconditional)
	//   (2) implementer -> reviewer       (unconditional)
	//   (3) reviewer -> close             (APPROVE)
	//   (4) reviewer -> implementer       (REQUEST_CHANGES, traversal_cap=3)
	//   (5) reviewer -> close-needs-attn  (BLOCK)
	//   (6) reviewer -> close-needs-attn  (unconditional fallback)
	if len(g.Edges) != 6 {
		t.Fatalf("len(Edges) = %d, want 6", len(g.Edges))
	}

	type edgeExpect struct {
		from string
		to   string
	}
	expected := []edgeExpect{
		{"start", "implementer"},
		{"implementer", "reviewer"},
		{"reviewer", "close"},
		{"reviewer", "implementer"},
		{"reviewer", "close-needs-attention"},
		{"reviewer", "close-needs-attention"},
	}

	for i, want := range expected {
		got := g.Edges[i]
		if got.FromNodeID != want.from || got.ToNodeID != want.to {
			t.Errorf("Edges[%d] = %s->%s, want %s->%s",
				i, got.FromNodeID, got.ToNodeID, want.from, want.to)
		}
	}

	// The last reviewer edge must be the unconditional fallback (no condition).
	lastReviewerEdge := g.Edges[5]
	if lastReviewerEdge.Condition != nil {
		t.Errorf("Edges[5] (unconditional fallback) has non-nil Condition: %q",
			lastReviewerEdge.ConditionRaw)
	}

	// Edge 3 (reviewer -> implementer, REQUEST_CHANGES) must carry traversal_cap.
	rcEdge := g.Edges[3]
	if cap, ok := rcEdge.UnknownAttrs["traversal_cap"]; !ok || cap != "3" {
		t.Errorf("Edges[3] traversal_cap = %q (present=%v), want %q",
			rcEdge.UnknownAttrs["traversal_cap"], ok, "3")
	}
}

// ── (3) Reserved-attribute strict policy: schema_version on a node ───────────

// TestScenarioReservedAttrSchemaVersionOnNode verifies WG-031/WG-033:
// schema_version on a node is a strict parse error.
func TestScenarioReservedAttrSchemaVersionOnNode(t *testing.T) {
	src := `digraph reserved_test {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="noop",
     idempotency_class="idempotent", schema_version="1"];
}`
	_, err := Parse(src, "reserved_test.dot")
	if err == nil {
		t.Fatal("expected strict error for schema_version on node, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "schema_version") {
		t.Errorf("error %q does not mention schema_version", errStr)
	}
	if !strings.Contains(errStr, "WG-033") {
		t.Errorf("error %q does not cite WG-033", errStr)
	}
}

// TestScenarioReservedAttrSchemaVersionOnEdge verifies WG-033 on edges too.
func TestScenarioReservedAttrSchemaVersionOnEdge(t *testing.T) {
	src := `digraph reserved_edge_test {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="outcome.status == 'SUCCESS'", schema_version="1"];
}`
	_, err := Parse(src, "reserved_edge_test.dot")
	if err == nil {
		t.Fatal("expected strict error for schema_version on edge, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "schema_version") {
		t.Errorf("error %q does not mention schema_version", errStr)
	}
	if !strings.Contains(errStr, "WG-033") {
		t.Errorf("error %q does not cite WG-033", errStr)
	}
}

// ── (4) Mixed unknown-attribute policy: warning + AST retention (WG-032) ─────

// TestScenarioUnknownAttrWarningAndRetention verifies the D9 / WG-032 policy:
// unknown permissive attributes emit warnings but are retained in the AST,
// and the graph still parses successfully (no strict error).
func TestScenarioUnknownAttrWarningAndRetention(t *testing.T) {
	src := `digraph unknown_test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="done";
  custom_graph_meta="experiment-42";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent",
        team="alpha", priority="P0"];
  done [type="non-agentic", handler_ref="noop",
        idempotency_class="idempotent"];

  work -> done [condition="outcome.status == 'SUCCESS'",
                routing_hint="fast-lane", debug_tag="test-123"];
}`
	g, err := Parse(src, "unknown_test.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error (unknown attrs should not be strict errors): %v", err)
	}

	// Graph-level unknown attr retained.
	if got := g.UnknownAttrs["custom_graph_meta"]; got != "experiment-42" {
		t.Errorf("graph UnknownAttrs[custom_graph_meta] = %q, want %q", got, "experiment-42")
	}

	// Node-level unknown attrs retained.
	var workNode *Node
	for _, n := range g.Nodes {
		if n.ID == "work" {
			workNode = n
			break
		}
	}
	if workNode == nil {
		t.Fatal("node \"work\" not found")
	}
	if got := workNode.UnknownAttrs["team"]; got != "alpha" {
		t.Errorf("node work UnknownAttrs[team] = %q, want %q", got, "alpha")
	}
	if got := workNode.UnknownAttrs["priority"]; got != "P0" {
		t.Errorf("node work UnknownAttrs[priority] = %q, want %q", got, "P0")
	}

	// Edge-level unknown attrs retained.
	if len(g.Edges) == 0 {
		t.Fatal("no edges parsed")
	}
	edge := g.Edges[0]
	if got := edge.UnknownAttrs["routing_hint"]; got != "fast-lane" {
		t.Errorf("edge UnknownAttrs[routing_hint] = %q, want %q", got, "fast-lane")
	}
	if got := edge.UnknownAttrs["debug_tag"]; got != "test-123" {
		t.Errorf("edge UnknownAttrs[debug_tag] = %q, want %q", got, "test-123")
	}

	// Warnings emitted for all unknowns.
	if len(g.Warnings) == 0 {
		t.Error("expected warnings for unknown permissive attributes, got none")
	}
	// Count expected unknown-attr warnings:
	//   graph: custom_graph_meta (1)
	//   node work: team, priority (2)
	//   edge: routing_hint, debug_tag (2)
	//   total: 5
	if len(g.Warnings) < 5 {
		t.Errorf("len(Warnings) = %d, want >= 5 (one per unknown attr)", len(g.Warnings))
	}

	// All warnings mention WG-031 or WG-032.
	for i, w := range g.Warnings {
		if !strings.Contains(w.Message, "WG-031") && !strings.Contains(w.Message, "WG-032") {
			t.Errorf("Warning[%d] %q does not cite WG-031 or WG-032", i, w.Message)
		}
	}

	// Validate passes (unknown attrs are not validation errors).
	diags := Validate(g)
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected validation error: %s", d)
		}
	}
}

// ── Canonical fixture round-trip with validation ─────────────────────────────

// TestScenarioCanonicalReviewLoopRoundTrip loads specs/examples/review-loop.dot,
// parses it, runs Validate, and asserts the full pipeline produces no errors.
// Also verifies the "role" attribute (used on every node in the canonical file)
// is retained as an unknown permissive attribute per WG-032.
func TestScenarioCanonicalReviewLoopRoundTrip(t *testing.T) {
	//nolint:gosec // G304: test-local constant path.
	src, err := os.ReadFile("../../../specs/examples/review-loop.dot")
	if err != nil {
		t.Skipf("specs/examples/review-loop.dot not found: %v", err)
	}

	g, parseErr := Parse(string(src), "specs/examples/review-loop.dot")
	if parseErr != nil {
		t.Fatalf("Parse: %v", parseErr)
	}

	// Basic structural assertions.
	if g.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", g.SchemaVersion, "1")
	}
	if g.Version != "1.0" {
		t.Errorf("Version = %q, want %q", g.Version, "1.0")
	}
	if g.StartNodeID != "start" {
		t.Errorf("StartNodeID = %q, want %q", g.StartNodeID, "start")
	}
	if len(g.TerminalNodeIDs) != 2 {
		t.Errorf("len(TerminalNodeIDs) = %d, want 2", len(g.TerminalNodeIDs))
	}
	// 5 nodes: start, implementer, reviewer, close, close-needs-attention.
	if len(g.Nodes) != 5 {
		t.Errorf("len(Nodes) = %d, want 5", len(g.Nodes))
	}
	if len(g.Edges) != 6 {
		t.Errorf("len(Edges) = %d, want 6", len(g.Edges))
	}

	// "role" is an unknown permissive attribute on every node in the canonical
	// fixture — verify it is retained (WG-032) and warned about (WG-031).
	for _, n := range g.Nodes {
		if role, ok := n.UnknownAttrs["role"]; !ok || role == "" {
			t.Errorf("node %q: expected unknown attr \"role\" to be retained (WG-032), got empty/absent", n.ID)
		}
	}

	// Warnings should include mentions of "role" attribute.
	roleWarnCount := 0
	for _, w := range g.Warnings {
		if strings.Contains(w.Message, "role") {
			roleWarnCount++
		}
	}
	if roleWarnCount < 5 {
		t.Errorf("expected >= 5 warnings for \"role\" unknown attr (one per node), got %d", roleWarnCount)
	}

	// Validate the parsed graph — no errors expected from the canonical fixture.
	diags := Validate(g)
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected validation error on canonical fixture: %s", d)
		}
	}
}
