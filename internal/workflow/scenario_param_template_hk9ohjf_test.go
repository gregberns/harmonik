package workflow_test

// scenario_param_template_hk9ohjf_test.go — conformance gate: operator runs a
// parameterized .dot template via --param (T6e).
//
// Operator-facing surface: harmonik run --param KEY=VALUE (repeatable) over a
// reusable .dot template.  Substrate: LoadDotWorkflowWithParams.
//
// Acceptance scenarios (bead hk-9ohjf, T6e — refs WG-044, WG-045, WG-046):
//
//  (a) DifferentParamMaps: the same on-disk .dot template produces a different
//      substituted graph when called with different param maps.  The file is
//      unchanged; only the operator-supplied params differ.
//
//  (b) ResidualTokenRefusesLoad: an unsubstituted __UPPER_SNAKE__ token after
//      the substitution pass is a launch-time error (*ErrWorkflowLoad wrapping
//      *ErrResidualToken) that names the offending token.  The run refuses to
//      start.
//
//  (c) TokenFreeNoOp: a token-free .dot template is a byte-identical no-op
//      pass; no error, graph is parsed normally.
//
//  (d) TrustBoundaryNeutralized: a --param value containing DOT syntax (e.g.
//      a closing quote) is now SAFE. WG-045/WG-046 were reversed for security:
//      the template is parsed FIRST (tokens intact), then the value is
//      substituted into the already-parsed, typed attribute. A value that used
//      to break the DOT tokenizer (`172"`) is now an inert literal inside
//      Graph.Goal — the run loads successfully and the value cannot alter graph
//      shape (DOT-structure injection closed). Pre-fix this raised a parse error.
//
//  (e) GoalNoTokensThreaded: goal="..." with no template tokens parses into
//      Graph.Goal and is non-empty (threads into agentic briefs via the
//      ExtraContext channel per WG-044).
//
// Depends on: hk-55zv2 (T5 — SubstituteTemplateParams + LoadDotWorkflowWithParams).
// Bead ref: hk-9ohjf (T6e).

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow"
)

// paramTemplateDot is a reusable .dot template with a __ISSUE_NUMBER__ token
// in the graph-level goal.  It is a complete, valid workflow once substituted.
const paramTemplateDot = `digraph param_template {
  schema_version="1";
  version="1.0";
  start_node="impl";
  terminal_node_ids="impl";
  goal="Fix #__ISSUE_NUMBER__";

  impl [type="agentic", agent_type="implementer", handler_ref="builtin:claude-code", idempotency_class="non-idempotent"];
}`

// paramTokenFreeDot is a valid .dot with no __TOKEN__ placeholders, used
// to verify the byte-identical no-op path (scenario c).
const paramTokenFreeDot = `digraph token_free {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="work";
  goal="No templates here";

  work [type="agentic", agent_type="implementer", handler_ref="builtin:claude-code", idempotency_class="non-idempotent"];
}`

// paramGoalOnlyDot is a valid .dot that has a goal but no template tokens,
// used to verify scenario (e).
const paramGoalOnlyDot = `digraph goal_only {
  schema_version="1";
  version="1.0";
  start_node="step";
  terminal_node_ids="step";
  goal="Migrate the payments API to v2";

  step [type="agentic", agent_type="implementer", handler_ref="builtin:claude-code", idempotency_class="non-idempotent"];
}`

// ── (a) DifferentParamMaps ────────────────────────────────────────────────────

// TestParamTemplate_DifferentParamMaps verifies that the same .dot template
// on disk produces a different substituted graph when called with different
// param maps (WG-045).
func TestParamTemplate_DifferentParamMaps(t *testing.T) {
	dotPath := writeTempDot(t, paramTemplateDot)

	// First run: ISSUE_NUMBER=42
	g1, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"ISSUE_NUMBER": "42"})
	if err != nil {
		t.Fatalf("run-1: unexpected error: %v", err)
	}
	if g1.Goal != "Fix #42" {
		t.Errorf("run-1: Goal = %q, want %q", g1.Goal, "Fix #42")
	}

	// Second run: ISSUE_NUMBER=99 — same file, different param map.
	g2, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"ISSUE_NUMBER": "99"})
	if err != nil {
		t.Fatalf("run-2: unexpected error: %v", err)
	}
	if g2.Goal != "Fix #99" {
		t.Errorf("run-2: Goal = %q, want %q", g2.Goal, "Fix #99")
	}

	// The two results must differ (same file, different params → different graph).
	if g1.Goal == g2.Goal {
		t.Errorf("expected different goals for different param maps, both returned %q", g1.Goal)
	}
}

// ── (b) ResidualTokenRefusesLoad ─────────────────────────────────────────────

// TestParamTemplate_ResidualTokenRefusesLoad verifies that an unresolved
// __UPPER_SNAKE__ token after substitution is a launch-time load error
// (*ErrWorkflowLoad) that names the offending token (WG-045 / WG-046).
func TestParamTemplate_ResidualTokenRefusesLoad(t *testing.T) {
	dotPath := writeTempDot(t, paramTemplateDot)

	// No params supplied → __ISSUE_NUMBER__ is unresolved.
	_, err := workflow.LoadDotWorkflowWithParams(dotPath, nil)
	if err == nil {
		t.Fatal("expected load error for unresolved token, got nil")
	}

	var loadErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}

	// The error must name the offending token.
	if !strings.Contains(loadErr.Error(), "ISSUE_NUMBER") {
		t.Errorf("load error %q does not mention ISSUE_NUMBER", loadErr.Error())
	}

	// The ErrWorkflowLoad reason must reference the residual-token detail.
	// (ErrResidualToken is embedded in the Reason string, not %w-wrapped.)
	if !strings.Contains(loadErr.Reason, "ISSUE_NUMBER") {
		t.Errorf("ErrWorkflowLoad.Reason %q does not mention ISSUE_NUMBER", loadErr.Reason)
	}
	if !strings.Contains(loadErr.Reason, "template substitution failed") {
		t.Errorf("ErrWorkflowLoad.Reason %q does not cite template substitution failure", loadErr.Reason)
	}
}

// ── (c) TokenFreeNoOp ────────────────────────────────────────────────────────

// TestParamTemplate_TokenFreeNoOp verifies that a token-free .dot template is
// a byte-identical no-op pass: no substitution error, graph loads normally
// regardless of params supplied (WG-045 fast-path).
func TestParamTemplate_TokenFreeNoOp(t *testing.T) {
	dotPath := writeTempDot(t, paramTokenFreeDot)

	// Supply params whose keys don't appear in the source — should be silently ignored.
	params := map[string]string{"UNUSED_KEY": "some_value", "ANOTHER": "42"}

	graph, err := workflow.LoadDotWorkflowWithParams(dotPath, params)
	if err != nil {
		t.Fatalf("unexpected error on token-free source: %v", err)
	}

	// Graph must be well-formed.
	if graph.StartNodeID != "work" {
		t.Errorf("StartNodeID = %q, want %q", graph.StartNodeID, "work")
	}
	if graph.Goal != "No templates here" {
		t.Errorf("Goal = %q, want %q", graph.Goal, "No templates here")
	}
}

// ── (d) TrustBoundaryMalformedDot ────────────────────────────────────────────

// TestParamTemplate_TrustBoundaryNeutralized verifies the REVERSED trust boundary
// (WG-045/WG-046 security fix): a --param value containing DOT syntax (a quote
// character that previously broke the attribute string) is now neutralized.
// Because the template is parsed BEFORE substitution, the value lands in the
// already-parsed, typed Graph.Goal field as an inert literal — it cannot terminate
// an attribute string or alter graph shape. The run loads successfully; pre-fix
// this raised a "parse failed" error.
func TestParamTemplate_TrustBoundaryNeutralized(t *testing.T) {
	dotPath := writeTempDot(t, paramTemplateDot)

	// A param value carrying a bare double-quote. Under the OLD pre-parse splice
	// this broke the DOT tokenizer (parse error). Under the new post-parse
	// substitution it is harmless: Graph.Goal becomes the literal `Fix #172"`.
	injectedValue := `172"`
	graph, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"ISSUE_NUMBER": injectedValue})
	if err != nil {
		t.Fatalf("expected the value to be neutralized and the run to load, got error: %v", err)
	}

	// The value is substituted verbatim into the typed goal field — no parse error,
	// no graph-shape change, just an inert literal containing the quote character.
	wantGoal := `Fix #172"`
	if graph.Goal != wantGoal {
		t.Errorf("Graph.Goal = %q, want %q (value neutralized as an inert literal)", graph.Goal, wantGoal)
	}

	// Exactly one node survives — the injected `"` did NOT spawn or alter a node
	// (DOT-structure injection closed by construction).
	if len(graph.Nodes) != 1 {
		t.Errorf("graph has %d nodes, want 1 — injected DOT syntax must not alter graph shape", len(graph.Nodes))
	}
}

// ── (e) GoalNoTokensThreaded ─────────────────────────────────────────────────

// TestParamTemplate_GoalNoTokensThreaded verifies that goal="..." with no
// template tokens is parsed into Graph.Goal (WG-044) and is non-empty.  The
// daemon will thread this into agentic node briefs via the ExtraContext channel.
func TestParamTemplate_GoalNoTokensThreaded(t *testing.T) {
	dotPath := writeTempDot(t, paramGoalOnlyDot)

	graph, err := workflow.LoadDotWorkflowWithParams(dotPath, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Goal == "" {
		t.Error("Graph.Goal is empty; expected a non-empty string from the goal= attribute (WG-044)")
	}
	if graph.Goal != "Migrate the payments API to v2" {
		t.Errorf("Graph.Goal = %q, want %q", graph.Goal, "Migrate the payments API to v2")
	}
}
