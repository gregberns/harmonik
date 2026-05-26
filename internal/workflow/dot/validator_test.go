package dot

// validator_test.go — tests for the workflow-graph validator.
//
// Test prefix: valFixture (per implementer-protocol.md helper-prefix discipline).
//
// Spec coverage:
//   WG-023  — terminal nodes have no outgoing edges
//   WG-024  — reserved-attribute strictness (per-type required/forbidden)
//   WG-027  — well-formedness: start_node, terminal_node_ids, reachability
//   WG-028  — cycle bounding via traversal_cap
//   WG-033  — schema_version graph-level (missing or bad value)
//   WG-034  — schema_version N-1 readability
//   WG-035  — workflow version present
//
// Tags: mechanism

import (
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// mustParse parses src and fatals if Parse returns an error.
func mustParse(t *testing.T, src, filename string) *Graph {
	t.Helper()
	g, err := Parse(src, filename)
	if err != nil {
		t.Fatalf("Parse(%s): unexpected parse error: %v", filename, err)
	}
	return g
}

// diagErrors returns only the error-severity diagnostics.
func diagErrors(diags []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Severity == SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// hasCode returns true if any diagnostic has Code == code.
func hasCode(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// ── fixtures ──────────────────────────────────────────────────────────────────

// valFixtureMinimal is the minimal well-formed three-node graph.
func valFixtureMinimal() string {
	return `digraph minimal {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close,close-needs-attention";

  work [type="agentic", agent_type="impl", handler_ref="claude-code",
        idempotency_class="non-idempotent"];
  close [type="non-agentic", handler_ref="noop",
         idempotency_class="idempotent"];
  "close-needs-attention" [type="non-agentic", handler_ref="noop",
                            idempotency_class="idempotent"];

  work -> close [condition="outcome.status == 'SUCCESS'"];
  work -> "close-needs-attention" [condition="outcome.status == FAIL"];
}`
}

// valFixtureGate is a well-formed graph with a gate node.
func valFixtureGate() string {
	return `digraph gate_wf {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close,close-needs-attention";

  work [type="agentic", agent_type="impl", handler_ref="claude-code",
        idempotency_class="non-idempotent"];
  gate [type="gate", gate_ref="review-gate", handler_ref="gate-eval"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
  "close-needs-attention" [type="non-agentic", handler_ref="noop",
                            idempotency_class="idempotent"];

  work -> gate [condition="outcome.status == 'SUCCESS'"];
  gate -> close [condition="outcome.status == 'SUCCESS'"];
  gate -> "close-needs-attention" [condition="outcome.status == FAIL"];
}`
}

// valFixtureSubWorkflow is a well-formed graph with a sub-workflow node.
func valFixtureSubWorkflow() string {
	return `digraph sw_wf {
  schema_version="1";
  version="1.0";
  start_node="sub";
  terminal_node_ids="close";

  sub [type="sub-workflow", sub_workflow_ref="inner-wf", workflow_version="1.0"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  sub -> close [condition="outcome.status == 'SUCCESS'"];
}`
}

// valFixtureCycleWithCap has a cycle whose back-edge carries traversal_cap.
func valFixtureCycleWithCap() string {
	return `digraph cycle_wf {
  schema_version="1";
  version="1.0";
  start_node="impl";
  terminal_node_ids="close";

  impl [type="agentic", agent_type="impl", handler_ref="claude-code",
        idempotency_class="non-idempotent"];
  review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer",
          idempotency_class="idempotent"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  impl -> review;
  review -> close [condition="outcome.preferred_label == 'APPROVE'"];
  review -> impl [condition="outcome.preferred_label == 'REQUEST_CHANGES'",
                  traversal_cap="3"];
  review -> close;
}`
}

// ── clean-graph tests ─────────────────────────────────────────────────────────

func TestValFixtureMinimalClean(t *testing.T) {
	g := mustParse(t, valFixtureMinimal(), "minimal.dot")
	diags := Validate(g)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestValFixtureGateClean(t *testing.T) {
	g := mustParse(t, valFixtureGate(), "gate.dot")
	diags := Validate(g)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestValFixtureSubWorkflowClean(t *testing.T) {
	g := mustParse(t, valFixtureSubWorkflow(), "sw.dot")
	diags := Validate(g)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diags), diags)
	}
}

func TestValFixtureCycleWithCapClean(t *testing.T) {
	g := mustParse(t, valFixtureCycleWithCap(), "cycle.dot")
	diags := Validate(g)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %d: %v", len(diags), diags)
	}
}

// ── specs/examples/review-loop.dot validates clean (acceptance criterion) ─────

func TestValFixtureSpecsExamplesReviewLoopClean(t *testing.T) {
	//nolint:gosec // G304: path is a test-local constant.
	src, err := os.ReadFile("../../../specs/examples/review-loop.dot")
	if err != nil {
		t.Fatalf("read specs/examples/review-loop.dot: %v", err)
	}
	g, parseErr := Parse(string(src), "specs/examples/review-loop.dot")
	if parseErr != nil {
		t.Fatalf("Parse(review-loop.dot): %v", parseErr)
	}
	diags := Validate(g)
	if len(diags) != 0 {
		t.Errorf("expected zero diagnostics, got %d:", len(diags))
		for _, d := range diags {
			t.Errorf("  %s", d)
		}
	}
}

// ── WG-035: version required ──────────────────────────────────────────────────

func TestValFixtureWG035MissingVersion(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diags, "WG-035") {
		t.Errorf("expected WG-035 diagnostic, got: %v", diags)
	}
}

// ── WG-033/034: schema_version checks ────────────────────────────────────────

func TestValFixtureWG033MissingSchemaVersion(t *testing.T) {
	src := `digraph bad {
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diags, "WG-033") {
		t.Errorf("expected WG-033 diagnostic for missing schema_version, got: %v", diags)
	}
}

func TestValFixtureWG034SchemaVersionTooOld(t *testing.T) {
	// schema_version=0 is older than N-1 (currentSchemaVersion-1 = 0 when current=1,
	// so 0 is exactly the minimum boundary; test with -1-style value using a string).
	// Actually current=1, N-1=0, so 0 is accepted. We use a future version to test:
	src := `digraph bad {
  schema_version="99";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diags, "WG-034") {
		t.Errorf("expected WG-034 diagnostic for future schema_version=99, got: %v", diags)
	}
}

// ── WG-024: missing required attributes ──────────────────────────────────────

func TestValFixtureWG024AgenticMissingAgentType(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="close";
  n [type="agentic", handler_ref="h", idempotency_class="non-idempotent"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  n -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	errs := diagErrors(diags)
	if !hasCode(errs, "WG-024") {
		t.Errorf("expected WG-024 for missing agent_type on agentic, got: %v", diags)
	}
}

func TestValFixtureWG024AgenticMissingHandlerRef(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="close";
  n [type="agentic", agent_type="impl", idempotency_class="non-idempotent"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  n -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for missing handler_ref on agentic, got: %v", diags)
	}
}

func TestValFixtureWG024AgenticMissingIdempotencyClass(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="close";
  n [type="agentic", agent_type="impl", handler_ref="h"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  n -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for missing idempotency_class on agentic, got: %v", diags)
	}
}

func TestValFixtureWG024GateMissingGateRef(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="g";
  terminal_node_ids="close";
  g [type="gate", handler_ref="gate-eval"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  g -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for missing gate_ref on gate, got: %v", diags)
	}
}

func TestValFixtureWG024GateMissingHandlerRef(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="g";
  terminal_node_ids="close";
  g [type="gate", gate_ref="my-gate"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  g -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for missing handler_ref on gate, got: %v", diags)
	}
}

func TestValFixtureWG024SubWorkflowMissingSubWorkflowRef(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="sw";
  terminal_node_ids="close";
  sw [type="sub-workflow", workflow_version="1.0"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  sw -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for missing sub_workflow_ref, got: %v", diags)
	}
}

func TestValFixtureWG024SubWorkflowMissingWorkflowVersion(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="sw";
  terminal_node_ids="close";
  sw [type="sub-workflow", sub_workflow_ref="inner-wf"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  sw -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for missing workflow_version, got: %v", diags)
	}
}

// ── WG-024: forbidden attributes ─────────────────────────────────────────────

func TestValFixtureWG024GateForbiddenIdempotencyClass(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="g";
  terminal_node_ids="close";
  g [type="gate", gate_ref="gref", handler_ref="heval",
     idempotency_class="idempotent"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  g -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for forbidden idempotency_class on gate, got: %v", diags)
	}
}

func TestValFixtureWG024NonAgenticForbiddenAgentType(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent",
     agent_type="impl"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-024") {
		t.Errorf("expected WG-024 for forbidden agent_type on non-agentic, got: %v", diags)
	}
}

// ── WG-027: well-formedness ───────────────────────────────────────────────────

func TestValFixtureWG027MissingStartNode(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-027") {
		t.Errorf("expected WG-027 for missing start_node, got: %v", diags)
	}
}

func TestValFixtureWG027MissingTerminalNodeIDs(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-027") {
		t.Errorf("expected WG-027 for missing terminal_node_ids, got: %v", diags)
	}
}

func TestValFixtureWG027StartNodeUndeclared(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="missing";
  terminal_node_ids="n";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-027") {
		t.Errorf("expected WG-027 for undeclared start_node, got: %v", diags)
	}
}

func TestValFixtureWG027TerminalNodeUndeclared(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="missing";
  n [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-027") {
		t.Errorf("expected WG-027 for undeclared terminal_node_id, got: %v", diags)
	}
}

func TestValFixtureWG027UnreachableNode(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  orphan [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-027") {
		t.Errorf("expected WG-027 for unreachable node, got: %v", diags)
	}
}

// ── WG-023: terminal nodes must not have outgoing edges ───────────────────────

func TestValFixtureWG023TerminalWithOutgoingEdge(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="b";
  a [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  b [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  c [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b;
  b -> c;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-023") {
		t.Errorf("expected WG-023 for terminal node with outgoing edge, got: %v", diags)
	}
}

// ── WG-028: cycle bounding ────────────────────────────────────────────────────

func TestValFixtureWG028CycleNoCap(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="close";
  a [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="non-idempotent"];
  b [type="agentic", agent_type="reviewer", handler_ref="h",
     idempotency_class="idempotent"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> b;
  b -> a [condition="outcome.preferred_label == 'REQUEST_CHANGES'"];
  b -> close [condition="outcome.preferred_label == 'APPROVE'"];
  b -> close;
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-028") {
		t.Errorf("expected WG-028 for cycle without traversal_cap, got: %v", diags)
	}
}

func TestValFixtureWG028SelfLoopNoCap(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="a";
  terminal_node_ids="close";
  a [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="non-idempotent"];
  close [type="non-agentic", handler_ref="h", idempotency_class="idempotent"];
  a -> a [condition="outcome.status == RETRY"];
  a -> close [condition="outcome.status == 'SUCCESS'"];
}`
	g := mustParse(t, src, "bad.dot")
	diags := Validate(g)
	if !hasCode(diagErrors(diags), "WG-028") {
		t.Errorf("expected WG-028 for self-loop without traversal_cap, got: %v", diags)
	}
}

// ── CP-056: policy_ref rejection ─────────────────────────────────────────────

// TestValFixtureCP056PolicyRefRejected verifies CP-056: any node with policy_ref
// in UnknownAttrs produces a Diagnostic with code CP-056 and SeverityError.
// The graph is constructed programmatically because the parser already rejects
// policy_ref at the ParseError layer; this test covers defense-in-depth
// (programmatic graph construction bypassing the parser).
func TestValFixtureCP056PolicyRefRejected(t *testing.T) {
	g := &Graph{
		SchemaVersion:   "1",
		Version:         "1.0",
		StartNodeID:     "n",
		TerminalNodeIDs: []string{"n"},
		Nodes: []*Node{
			{
				ID:               "n",
				Line:             6,
				Type:             core.NodeTypeNonAgentic,
				RawType:          "non-agentic",
				HandlerRef:       "h",
				IdempotencyClass: "idempotent",
				UnknownAttrs:     map[string]string{"policy_ref": "some-policy"},
			},
		},
	}
	diags := Validate(g)
	errs := diagErrors(diags)
	if !hasCode(errs, "CP-056") {
		t.Errorf("expected CP-056 diagnostic for policy_ref, got: %v", diags)
	}
}

// TestValFixtureCP056PolicyRefMessageSuggestions verifies that the CP-056
// diagnostic message names all three replacement attributes per CP-055.
func TestValFixtureCP056PolicyRefMessageSuggestions(t *testing.T) {
	g := &Graph{
		SchemaVersion:   "1",
		Version:         "1.0",
		StartNodeID:     "n",
		TerminalNodeIDs: []string{"n"},
		Nodes: []*Node{
			{
				ID:               "n",
				Line:             6,
				Type:             core.NodeTypeNonAgentic,
				RawType:          "non-agentic",
				HandlerRef:       "h",
				IdempotencyClass: "idempotent",
				UnknownAttrs:     map[string]string{"policy_ref": "some-policy"},
			},
		},
	}
	diags := Validate(g)
	var cp056 *Diagnostic
	for i := range diags {
		if diags[i].Code == "CP-056" {
			cp056 = &diags[i]
			break
		}
	}
	if cp056 == nil {
		t.Fatal("expected CP-056 diagnostic, got none")
	}
	for _, want := range []string{"gate_ref", "skills_ref", "freedom_profile_ref"} {
		if !strings.Contains(cp056.Message, want) {
			t.Errorf("CP-056 message %q does not mention %q", cp056.Message, want)
		}
	}
}

// ── Diagnostic.String() formatting ───────────────────────────────────────────

func TestValDiagnosticStringWithLine(t *testing.T) {
	d := Diagnostic{Severity: SeverityError, Line: 5, Code: "WG-027", Message: "test"}
	got := d.String()
	want := "error: dot:5 [WG-027]: test"
	if got != want {
		t.Errorf("Diagnostic.String() = %q, want %q", got, want)
	}
}

func TestValDiagnosticStringNoLine(t *testing.T) {
	d := Diagnostic{Severity: SeverityWarning, Line: 0, Code: "WG-034", Message: "test"}
	got := d.String()
	want := "warning: [WG-034]: test"
	if got != want {
		t.Errorf("Diagnostic.String() = %q, want %q", got, want)
	}
}
