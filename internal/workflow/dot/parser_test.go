package dot

// parser_test.go — tests for the DOT parser and typed AST.
//
// Test prefix: dotFixture (per implementer-protocol.md helper-prefix discipline).
//
// Spec coverage:
//   WG-001  — node type closed enum
//   WG-002  — per-type attribute catalog
//   WG-031  — mixed strict/permissive policy
//   WG-032  — AST retention of unknown permissive attributes
//   WG-033  — schema_version graph-level
//   WG-035  — version distinct from schema_version
//   WG-013  — edge-condition dialect
//   WG-014  — LHS whitelist
//   WG-015  — RHS literal types
//
// Tags: mechanism

import (
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

// dotFixtureMinimal returns the minimal three-node illustrative graph from
// specs/workflow-graph.md §15.1.
func dotFixtureMinimal() string {
	return `digraph minimal {
  schema_version="1";
  version="1.0";
  start_node="start";
  terminal_node_ids="close,close-needs-attention";

  start [type="agentic", agent_type="implementer",
         handler_ref="claude-code", idempotency_class="non-idempotent"];
  close [type="agentic", agent_type="reviewer",
         handler_ref="claude-code", idempotency_class="idempotent"];
  "close-needs-attention" [type="agentic", agent_type="reviewer",
         handler_ref="claude-code", idempotency_class="idempotent"];

  start -> close [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
  start -> "close-needs-attention" [condition="outcome.status == 'FAIL'", weight="10", ordering_key="b"];
}`
}

// dotFixtureWithUnknownAttrs returns a graph with unknown permissive attributes
// on nodes and edges (WG-031/032 round-trip test).
func dotFixtureWithUnknownAttrs() string {
	return `digraph test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";
  custom_meta="team-alpha";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent",
        priority="P1", owner="squad-2"];
  close [type="non-agentic", handler_ref="merge-handler",
         idempotency_class="idempotent", sla_class="standard"];

  work -> close [condition="outcome.status == 'SUCCESS'", weight="10",
                 ordering_key="a", routing_hint="fast-path"];
}`
}

// dotFixtureConditionConjunction returns a graph with a &&-conjunction condition.
func dotFixtureConditionConjunction() string {
	return `digraph conditions {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close,close-needs-attention";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent"];
  close [type="non-agentic", handler_ref="merge-handler",
         idempotency_class="idempotent"];
  "close-needs-attention" [type="non-agentic", handler_ref="alert-handler",
                            idempotency_class="idempotent"];

  work -> work [condition="outcome.status == RETRY", weight="10", ordering_key="a",
                traversal_cap="5"];
  work -> close [condition="outcome.status == 'SUCCESS'", weight="5", ordering_key="b"];
  work -> "close-needs-attention" [
    condition="outcome.status == FAIL && outcome.failure_class == structural",
    weight="5", ordering_key="c"];
}`
}

// dotFixtureGateNode returns a graph with a gate node per WG-005.
func dotFixtureGateNode() string {
	return `digraph gate_test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close,close-needs-attention";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent"];
  review_gate [type="gate", gate_ref="review-gate-policy",
               handler_ref="gate-evaluator"];
  close [type="non-agentic", handler_ref="merge-handler",
         idempotency_class="idempotent"];
  "close-needs-attention" [type="non-agentic", handler_ref="alert-handler",
                            idempotency_class="idempotent"];

  work -> review_gate [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
  review_gate -> close [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
  review_gate -> "close-needs-attention" [condition="outcome.status == FAIL", weight="10", ordering_key="b"];
}`
}

// ── parse success tests ───────────────────────────────────────────────────────

func TestDotFixtureParseMinimal(t *testing.T) {
	g, err := Parse(dotFixtureMinimal(), "minimal.dot")
	if err != nil {
		t.Fatalf("Parse(minimal): unexpected error: %v", err)
	}
	if g.Name != "minimal" {
		t.Errorf("Name = %q, want %q", g.Name, "minimal")
	}
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
	if len(g.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(g.Nodes))
	}
	if len(g.Edges) != 2 {
		t.Errorf("len(Edges) = %d, want 2", len(g.Edges))
	}
}

func TestDotFixtureNodeTypes(t *testing.T) {
	g, err := Parse(dotFixtureMinimal(), "minimal.dot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range g.Nodes {
		if n.Type != core.NodeTypeAgentic {
			t.Errorf("node %q: Type = %q, want %q", n.ID, n.Type, core.NodeTypeAgentic)
		}
		if n.HandlerRef != "claude-code" {
			t.Errorf("node %q: HandlerRef = %q, want %q", n.ID, n.HandlerRef, "claude-code")
		}
	}
}

func TestDotFixtureEdgeConditions(t *testing.T) {
	g, err := Parse(dotFixtureMinimal(), "minimal.dot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("len(Edges) = %d, want 2", len(g.Edges))
	}
	e0 := g.Edges[0]
	if e0.Condition == nil {
		t.Fatal("Edges[0].Condition is nil")
	}
	if len(e0.Condition.Clauses) != 1 {
		t.Fatalf("Edges[0].Condition.Clauses len = %d, want 1", len(e0.Condition.Clauses))
	}
	cl := e0.Condition.Clauses[0]
	if cl.LHS != "outcome.status" {
		t.Errorf("clause LHS = %q, want %q", cl.LHS, "outcome.status")
	}
	if cl.Op != "==" {
		t.Errorf("clause Op = %q, want %q", cl.Op, "==")
	}
	if cl.RHS != "SUCCESS" {
		t.Errorf("clause RHS = %q, want %q", cl.RHS, "SUCCESS")
	}
}

// TestDotFixtureWG032UnknownAttrsRetained verifies WG-032: unknown permissive
// attributes are retained in the AST and NOT silently dropped.
func TestDotFixtureWG032UnknownAttrsRetained(t *testing.T) {
	g, err := Parse(dotFixtureWithUnknownAttrs(), "unknown.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	// Graph-level unknown attr.
	if got := g.UnknownAttrs["custom_meta"]; got != "team-alpha" {
		t.Errorf("graph UnknownAttrs[custom_meta] = %q, want %q", got, "team-alpha")
	}
	// Graph-level warnings emitted.
	if len(g.Warnings) == 0 {
		t.Error("expected Warnings for unknown permissive attributes, got none")
	}
	// Node-level unknown attrs.
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
	if got := workNode.UnknownAttrs["priority"]; got != "P1" {
		t.Errorf("node work UnknownAttrs[priority] = %q, want %q", got, "P1")
	}
	if got := workNode.UnknownAttrs["owner"]; got != "squad-2" {
		t.Errorf("node work UnknownAttrs[owner] = %q, want %q", got, "squad-2")
	}
	// Edge-level unknown attr.
	if len(g.Edges) == 0 {
		t.Fatal("no edges")
	}
	e := g.Edges[0]
	if got := e.UnknownAttrs["routing_hint"]; got != "fast-path" {
		t.Errorf("edge UnknownAttrs[routing_hint] = %q, want %q", got, "fast-path")
	}
}

// TestDotFixtureWG033SchemaVersionGraphLevel verifies WG-033: schema_version
// is a graph-level attribute; on a node it must be a strict error.
func TestDotFixtureWG033SchemaVersionOnNode(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="idempotent", schema_version="1"];
}`
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error for schema_version on node, got nil")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("error %q does not mention schema_version", err.Error())
	}
}

// TestDotFixtureWG031PolicyRefRejected verifies WG-031 (CP-056): policy_ref is
// reserved-and-rejected on any node.
func TestDotFixtureWG031PolicyRefRejected(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="idempotent", policy_ref="some-policy"];
}`
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error for policy_ref on node, got nil")
	}
	if !strings.Contains(err.Error(), "policy_ref") {
		t.Errorf("error %q does not mention policy_ref", err.Error())
	}
}

// TestDotFixtureWG001BadNodeType verifies WG-001: unknown node type is a strict error.
func TestDotFixtureWG001BadNodeType(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="control-point", handler_ref="h"];
}`
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error for unknown node type, got nil")
	}
	if !strings.Contains(err.Error(), "control-point") {
		t.Errorf("error %q does not mention control-point", err.Error())
	}
}

// TestDotFixtureConditionConjunction verifies WG-013 (&&-conjunction) and
// WG-017 (failure-class RHS validation).
func TestDotFixtureConditionConjunctionParsed(t *testing.T) {
	g, err := Parse(dotFixtureConditionConjunction(), "conj.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	// Find the conjunction edge.
	var conjEdge *Edge
	for _, e := range g.Edges {
		if e.Condition != nil && len(e.Condition.Clauses) == 2 {
			conjEdge = e
			break
		}
	}
	if conjEdge == nil {
		t.Fatal("no edge with 2-clause conjunction found")
	}
	cl0 := conjEdge.Condition.Clauses[0]
	if cl0.LHS != "outcome.status" || cl0.RHS != "FAIL" {
		t.Errorf("clause[0]: LHS=%q RHS=%q, want LHS=outcome.status RHS=FAIL", cl0.LHS, cl0.RHS)
	}
	cl1 := conjEdge.Condition.Clauses[1]
	if cl1.LHS != "outcome.failure_class" || cl1.RHS != "structural" {
		t.Errorf("clause[1]: LHS=%q RHS=%q, want LHS=outcome.failure_class RHS=structural", cl1.LHS, cl1.RHS)
	}
}

// TestDotFixtureTraversalCapRetained verifies that traversal_cap is retained in
// edge.UnknownAttrs for validator-layer consumption (EM-043).
func TestDotFixtureTraversalCapRetained(t *testing.T) {
	g, err := Parse(dotFixtureConditionConjunction(), "conj.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	var capEdge *Edge
	for _, e := range g.Edges {
		if v, ok := e.UnknownAttrs["traversal_cap"]; ok && v != "" {
			capEdge = e
			break
		}
	}
	if capEdge == nil {
		t.Fatal("no edge with traversal_cap found in UnknownAttrs")
	}
	if capEdge.UnknownAttrs["traversal_cap"] != "5" {
		t.Errorf("traversal_cap = %q, want %q", capEdge.UnknownAttrs["traversal_cap"], "5")
	}
}

// TestDotFixtureGateNode verifies gate node parsing per WG-005.
func TestDotFixtureGateNodeParsed(t *testing.T) {
	g, err := Parse(dotFixtureGateNode(), "gate.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	var gateNode *Node
	for _, n := range g.Nodes {
		if n.Type == core.NodeTypeGate {
			gateNode = n
			break
		}
	}
	if gateNode == nil {
		t.Fatal("no gate node found")
	}
	if gateNode.GateRef != "review-gate-policy" {
		t.Errorf("GateRef = %q, want %q", gateNode.GateRef, "review-gate-policy")
	}
	if gateNode.HandlerRef != "gate-evaluator" {
		t.Errorf("HandlerRef = %q, want %q", gateNode.HandlerRef, "gate-evaluator")
	}
}

// ── condition LHS whitelist tests (WG-014) ────────────────────────────────────

func TestDotFixtureConditionBadLHS(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="node.status == 'SUCCESS'", weight="1", ordering_key="a"];
}`
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error for bad LHS, got nil")
	}
	if !strings.Contains(err.Error(), "WG-014") {
		t.Errorf("error %q does not mention WG-014", err.Error())
	}
}

func TestDotFixtureConditionContextLHS(t *testing.T) {
	// context.<key> is a valid LHS per WG-014.
	src := `digraph ctx {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  context_keys="pr_url";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="context.pr_url == 'yes'", weight="1", ordering_key="a"];
}`
	g, err := Parse(src, "ctx.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error for context.* LHS: %v", err)
	}
	if len(g.ContextKeys) != 1 || g.ContextKeys[0] != "pr_url" {
		t.Errorf("ContextKeys = %v, want [pr_url]", g.ContextKeys)
	}
}

// TestDotFixtureConditionBadStatusRHS verifies WG-015: unknown status enum on RHS.
func TestDotFixtureConditionBadStatusRHS(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="outcome.status == UNKNOWN_STATUS", weight="1", ordering_key="a"];
}`
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error for unknown status RHS, got nil")
	}
}

// TestDotFixtureConditionBadFailureClassRHS verifies WG-015: unknown failure class.
func TestDotFixtureConditionBadFailureClassRHS(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="outcome.failure_class == not_a_class", weight="1", ordering_key="a"];
}`
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error for unknown failure_class RHS, got nil")
	}
}

// TestDotFixtureInequalityOp verifies != operator is parsed.
func TestDotFixtureInequalityOp(t *testing.T) {
	src := `digraph ineq {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="outcome.status != FAIL", weight="1", ordering_key="a"];
}`
	g, err := Parse(src, "ineq.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(g.Edges) == 0 || g.Edges[0].Condition == nil {
		t.Fatal("no parsed condition")
	}
	if g.Edges[0].Condition.Clauses[0].Op != "!=" {
		t.Errorf("Op = %q, want %q", g.Edges[0].Condition.Clauses[0].Op, "!=")
	}
}

// ── error message contains file:line tests ────────────────────────────────────

func TestDotFixtureParseErrorHasLine(t *testing.T) {
	src := "digraph bad {\n  n [type=\"unknown-type\"];\n}"
	_, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Error should include a line number.
	if !strings.Contains(err.Error(), "dot:") {
		t.Errorf("error %q does not include dot: prefix with line number", err.Error())
	}
}

// ── tokenizer comment stripping ───────────────────────────────────────────────

func TestDotFixtureBlockComment(t *testing.T) {
	src := `digraph test {
  /* This is a block comment */
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g, err := Parse(src, "comment.dot")
	if err != nil {
		t.Fatalf("Parse with block comment: unexpected error: %v", err)
	}
	if g.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", g.SchemaVersion, "1")
	}
}

func TestDotFixtureLineComment(t *testing.T) {
	src := `digraph test {
  // schema version
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g, err := Parse(src, "lcomment.dot")
	if err != nil {
		t.Fatalf("Parse with line comment: unexpected error: %v", err)
	}
	if g.StartNodeID != "n" {
		t.Errorf("StartNodeID = %q, want %q", g.StartNodeID, "n")
	}
}

// ── sub-workflow node ─────────────────────────────────────────────────────────

func TestDotFixtureSubWorkflowNode(t *testing.T) {
	src := `digraph sw {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";
  work [type="sub-workflow", sub_workflow_ref="inner-wf", workflow_version="1.0"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  work -> close [condition="outcome.status == 'SUCCESS'", weight="1", ordering_key="a"];
}`
	g, err := Parse(src, "sw.dot")
	if err != nil {
		t.Fatalf("Parse sub-workflow: unexpected error: %v", err)
	}
	var swNode *Node
	for _, n := range g.Nodes {
		if n.Type == core.NodeTypeSubWorkflow {
			swNode = n
		}
	}
	if swNode == nil {
		t.Fatal("no sub-workflow node found")
	}
	if swNode.SubWorkflowRef != "inner-wf" {
		t.Errorf("SubWorkflowRef = %q, want %q", swNode.SubWorkflowRef, "inner-wf")
	}
	if swNode.WorkflowVersion != "1.0" {
		t.Errorf("WorkflowVersion = %q, want %q", swNode.WorkflowVersion, "1.0")
	}
}

// ── multiple strict errors collected ─────────────────────────────────────────

func TestDotFixtureMultipleStrictErrors(t *testing.T) {
	src := `digraph multi {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="bad-type", policy_ref="x", schema_version="1"];
  b [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  a -> b [weight="1", ordering_key="a"];
}`
	_, err := Parse(src, "multi.dot")
	if err == nil {
		t.Fatal("expected multiple errors, got nil")
	}
	pe, ok := err.(ParseErrors)
	if !ok {
		t.Fatalf("expected ParseErrors, got %T: %v", err, err)
	}
	if len(pe) < 3 {
		t.Errorf("expected ≥3 strict errors, got %d: %v", len(pe), pe)
	}
}

// ── testdata/review-loop.dot round-trip ──────────────────────────────────────

// TestDotFixtureReviewLoopFile parses the testdata/review-loop.dot fixture that
// mirrors the future specs/examples/review-loop.dot (C5 of phase-3-dot).
// This satisfies the acceptance criterion "round-trips specs/examples/review-loop.dot
// (if missing, use a test fixture)" from bead hk-nvzur.
func TestDotFixtureReviewLoopFile(t *testing.T) {
	//nolint:gosec // G304: path is a test-local constant, not user-supplied.
	src, err := os.ReadFile("testdata/review-loop.dot")
	if err != nil {
		t.Fatalf("read testdata/review-loop.dot: %v", err)
	}
	g, parseErr := Parse(string(src), "testdata/review-loop.dot")
	if parseErr != nil {
		t.Fatalf("Parse(review-loop.dot): %v", parseErr)
	}
	if g.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", g.SchemaVersion, "1")
	}
	if g.StartNodeID != "implement" {
		t.Errorf("StartNodeID = %q, want %q", g.StartNodeID, "implement")
	}
	if len(g.TerminalNodeIDs) != 2 {
		t.Errorf("len(TerminalNodeIDs) = %d, want 2", len(g.TerminalNodeIDs))
	}
	if len(g.ContextKeys) != 2 {
		t.Errorf("len(ContextKeys) = %d, want 2 (bead_id, pr_url)", len(g.ContextKeys))
	}
	if len(g.Nodes) != 4 {
		t.Errorf("len(Nodes) = %d, want 4", len(g.Nodes))
	}
	if len(g.Edges) != 6 {
		t.Errorf("len(Edges) = %d, want 6", len(g.Edges))
	}
	// Verify preferred_label conditions are parsed (WG-019).
	var approveEdge *Edge
	for _, e := range g.Edges {
		if e.Condition != nil {
			for _, cl := range e.Condition.Clauses {
				if cl.LHS == "outcome.preferred_label" && cl.RHS == "APPROVE" {
					approveEdge = e
				}
			}
		}
	}
	if approveEdge == nil {
		t.Error("no edge with outcome.preferred_label == 'APPROVE' found")
	}
	// Verify traversal_cap is retained in UnknownAttrs.
	var capCount int
	for _, e := range g.Edges {
		if _, ok := e.UnknownAttrs["traversal_cap"]; ok {
			capCount++
		}
	}
	if capCount == 0 {
		t.Error("no edge with traversal_cap found in UnknownAttrs")
	}
}

// ── specs/examples/review-loop.dot round-trip ────────────────────────────────

func TestDotFixtureSpecsExamplesReviewLoop(t *testing.T) {
	//nolint:gosec // G304: path is a test-local constant, not user-supplied.
	src, err := os.ReadFile("../../../specs/examples/review-loop.dot")
	if err != nil {
		t.Skipf("specs/examples/review-loop.dot not found (C5 not yet landed): %v", err)
	}
	g, parseErr := Parse(string(src), "specs/examples/review-loop.dot")
	if parseErr != nil {
		t.Fatalf("Parse(specs/examples/review-loop.dot): %v", parseErr)
	}
	if g.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q, want %q", g.SchemaVersion, "1")
	}
	if g.StartNodeID != "start" {
		t.Errorf("StartNodeID = %q, want %q", g.StartNodeID, "start")
	}
	if len(g.TerminalNodeIDs) != 2 {
		t.Errorf("TerminalNodeIDs = %v, want 2 entries", g.TerminalNodeIDs)
	}
	if len(g.Nodes) < 4 {
		t.Errorf("Nodes = %d, want ≥4", len(g.Nodes))
	}
}

// ── round-trip: ConditionRaw retained ────────────────────────────────────────

// TestDotFixtureWG044GoalGraphLevel verifies that the graph-level "goal" attribute
// is parsed into Graph.Goal (WG-044).
func TestDotFixtureWG044GoalGraphLevel(t *testing.T) {
	src := `digraph W {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  goal="Fix #172";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g, err := Parse(src, "goal.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if g.Goal != "Fix #172" {
		t.Errorf("Graph.Goal = %q, want %q", g.Goal, "Fix #172")
	}
}

// TestDotFixtureWG044GoalOnNodeError verifies that "goal" on a node is a strict
// reserved-out-of-position error (WG-044).
func TestDotFixtureWG044GoalOnNodeError(t *testing.T) {
	src := `digraph W {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent", goal="bad"];
}`
	_, err := Parse(src, "goal_node.dot")
	if err == nil {
		t.Fatal("expected strict error for goal on node, got nil")
	}
	if !containsString(err.Error(), "goal") {
		t.Errorf("error %q does not mention 'goal'", err.Error())
	}
}

// TestDotFixtureWG044GoalOnEdgeError verifies that "goal" on an edge is a strict
// reserved-out-of-position error (WG-044).
func TestDotFixtureWG044GoalOnEdgeError(t *testing.T) {
	src := `digraph W {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [goal="bad"];
}`
	_, err := Parse(src, "goal_edge.dot")
	if err == nil {
		t.Fatal("expected strict error for goal on edge, got nil")
	}
	if !containsString(err.Error(), "goal") {
		t.Errorf("error %q does not mention 'goal'", err.Error())
	}
}

// containsString is a local helper (avoid importing strings in package dot test).
func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestDotFixtureConditionRawRetained(t *testing.T) {
	rawCond := "outcome.status == 'SUCCESS'"
	src := `digraph rt {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b [condition="outcome.status == 'SUCCESS'", weight="1", ordering_key="a"];
}`
	g, err := Parse(src, "rt.dot")
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if g.Edges[0].ConditionRaw != rawCond {
		t.Errorf("ConditionRaw = %q, want %q", g.Edges[0].ConditionRaw, rawCond)
	}
}

// ── no_progress_guard attribute parsing (hk-nvd3) ────────────────────────────

// noProgressGuardFixture returns a minimal two-node graph with the given
// no_progress_guard attribute value embedded at the graph level.
func noProgressGuardFixture(val string) string {
	attr := ""
	if val != "" {
		attr = "\n  no_progress_guard=\"" + val + "\";"
	}
	return `digraph npg {
  schema_version="1";
  version="1.0";` + attr + `
  start_node="a";
  terminal_node_ids="b";
  a [type="agentic", agent_type="impl", handler_ref="h", idempotency_class="non-idempotent"];
  b [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
  a -> b;
}`
}

// TestNoProgressGuardAttr_ValidValues verifies that "", "strict", "off", and
// "capped:N" are accepted and round-trip through the Graph.NoProgressGuard field.
// hk-nvd3.
func TestNoProgressGuardAttr_ValidValues(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"strict", "strict"},
		{"off", "off"},
		{"capped:1", "capped:1"},
		{"capped:10", "capped:10"},
		{"capped:100", "capped:100"},
	}
	for _, tc := range cases {
		g, err := Parse(noProgressGuardFixture(tc.input), "npg.dot")
		if err != nil {
			t.Errorf("no_progress_guard=%q: unexpected parse error: %v", tc.input, err)
			continue
		}
		if g.NoProgressGuard != tc.want {
			t.Errorf("no_progress_guard=%q: got Graph.NoProgressGuard=%q, want %q",
				tc.input, g.NoProgressGuard, tc.want)
		}
	}
}

// TestNoProgressGuardAttr_InvalidValues verifies that invalid values produce a
// strict parse error. hk-nvd3.
func TestNoProgressGuardAttr_InvalidValues(t *testing.T) {
	invalid := []string{
		"STRICT",     // wrong case
		"Cap:1",      // wrong case prefix
		"capped:0",   // N must be >= 1
		"capped:-1",  // negative N
		"capped:abc", // non-integer N
		"capped:",    // empty N
		"disabled",   // not a valid value
		"none",       // not a valid value
	}
	for _, val := range invalid {
		_, err := Parse(noProgressGuardFixture(val), "npg.dot")
		if err == nil {
			t.Errorf("no_progress_guard=%q: expected strict parse error, got nil", val)
		}
	}
}
