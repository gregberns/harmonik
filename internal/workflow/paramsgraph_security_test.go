package workflow_test

// paramsgraph_security_test.go — WG-045/WG-046 command-injection close.
//
// Repro (must FAIL pre-fix, PASS post-fix): a template param value substituted into
// a node tool_command is now POSIX shell-quoted at LOAD time, so shell
// metacharacters in the value become an inert single shell word instead of
// executable shell. DOT-structure injection (a value with `"`/`]` trying to alter
// the parsed graph) is closed by construction because substitution happens AFTER
// parse. Author shell syntax (`&&`, `>`) and free-text non-shell params (goal) are
// unchanged.
//
// Spec refs: specs/workflow-graph.md §4 WG-039 / WG-045 / WG-046.

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// toolDot builds a minimal single-node non-agentic shell-tool .dot with the given
// tool_command literal.
func toolDot(toolCommand string) string {
	return `digraph tool_security {
  schema_version="1";
  version="1.0";
  start_node="run";
  terminal_node_ids="run";
  run [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="` + toolCommand + `"];
}`
}

// goalDot builds a minimal single-node graph carrying a templated goal.
func goalDot(goal string) string {
	return `digraph goal_security {
  schema_version="1";
  version="1.0";
  start_node="step";
  terminal_node_ids="step";
  goal="` + goal + `";
  step [type="agentic", agent_type="implementer", handler_ref="builtin:claude-code", idempotency_class="non-idempotent"];
}`
}

// toolNodeCommand returns the ToolCommand of the single tool node in g.
func toolNodeCommand(t *testing.T, g *dot.Graph) string {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ToolCommand != "" {
			return n.ToolCommand
		}
	}
	t.Fatal("no tool node found in graph")
	return ""
}

// TestParamTemplate_ToolCommandInjection_Neutralized is the headline repro: a
// shell-metacharacter-laden param value substituted into tool_command becomes a
// single shell-quoted word.
func TestParamTemplate_ToolCommandInjection_Neutralized(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("echo __SID__"))
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{
		"SID": "x; touch /tmp/pwned #",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := toolNodeCommand(t, g)
	// Post-fix: the value is one shell-quoted word. Pre-fix it was the raw splice
	// `echo x; touch /tmp/pwned #` (a command separator that fires `touch`).
	want := "echo 'x; touch /tmp/pwned #'"
	if got != want {
		t.Fatalf("tool_command = %q, want %q (param value must be shell-quoted)", got, want)
	}
	// The dangerous `;` and `#` must live ONLY inside the single-quoted span.
	if before, _, _ := strings.Cut(got, "'"); strings.ContainsAny(before, ";#") {
		t.Errorf("tool_command has an unquoted shell metacharacter before the quoted value: %q", got)
	}
}

// TestToolCommand_TokenPlusAuthorShell verifies the author's own shell syntax
// (flags + redirect) is preserved while only the value is quoted.
func TestToolCommand_TokenPlusAuthorShell(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("sentry issue view __SID__ --json > .ai/issue.json"))
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": "PROJECT-123"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := toolNodeCommand(t, g)
	want := "sentry issue view 'PROJECT-123' --json > .ai/issue.json"
	if got != want {
		t.Fatalf("tool_command = %q, want %q", got, want)
	}
}

// TestToolCommand_MidTokenValue verifies a token embedded mid-argument is quoted
// such that the shell concatenates it adjacently (safe) — e.g. a URL path segment.
func TestToolCommand_MidTokenValue(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("curl --url=http://example/__SID__"))
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": "a b; rm -rf x"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := toolNodeCommand(t, g)
	// Single-quoting yields shell-adjacent concatenation: --url=http://example/'a b; rm -rf x'
	want := "curl --url=http://example/'a b; rm -rf x'"
	if got != want {
		t.Fatalf("tool_command = %q, want %q (mid-token quoting must concatenate, not split)", got, want)
	}
}

// TestToolCommand_AuthorShellPreserved verifies a token-free tool_command carrying
// legitimate shell control flow is byte-identical (no spurious quoting).
func TestToolCommand_AuthorShellPreserved(t *testing.T) {
	// In DOT source the author escapes inner double-quotes as \" (the lexer
	// unescapes them); the command carries no template tokens.
	cmdSrc := `C=$(grep -oE 'HIGH|LOW' f) && [ \"$C\" = LOW ] && exit 1 || exit 0`
	cmdWant := `C=$(grep -oE 'HIGH|LOW' f) && [ "$C" = LOW ] && exit 1 || exit 0`
	dotPath := writeTempDot(t, toolDot(cmdSrc))
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"UNUSED": "x"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := toolNodeCommand(t, g)
	if got != cmdWant {
		t.Fatalf("tool_command = %q, want byte-identical author text %q (no spurious quoting)", got, cmdWant)
	}
}

// TestGoalParam_VerbatimWithSpaces verifies a non-shell attribute (goal) is
// substituted VERBATIM (no quoting) — free-text params keep working.
func TestGoalParam_VerbatimWithSpaces(t *testing.T) {
	dotPath := writeTempDot(t, goalDot("__HINT__"))
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"HINT": "fix the login bug"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if g.Goal != "fix the login bug" {
		t.Fatalf("Graph.Goal = %q, want %q (verbatim, no quoting in benign context)", g.Goal, "fix the login bug")
	}
}

// TestParamTemplate_DotStructureInjection_NoGraphChange verifies a value carrying
// DOT syntax (`"` to close the attribute, `]` to close the node, then a second
// node declaration) cannot alter the parsed graph — it lands as an inert quoted
// literal inside the original node's tool_command.
func TestParamTemplate_DotStructureInjection_NoGraphChange(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("echo __SID__"))
	inject := `x"] ; evil [type="non-agentic", handler_ref="shell", tool_command="touch pwned`
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": inject})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("graph has %d nodes, want 1 — DOT-structure injection altered the graph", len(g.Nodes))
	}
	got := toolNodeCommand(t, g)
	// The entire injected payload is one shell-quoted word; the `"`/`]` are literal.
	want := "echo " + workflow.ShellQuote(inject)
	if got != want {
		t.Fatalf("tool_command = %q, want %q (injection neutralized as inert literal)", got, want)
	}
}

// TestToolCommand_QuotedTokenSingle_Rejected verifies the load-time lint rejects a
// token wrapped in the author's own SINGLE quotes inside tool_command (the
// shell-quoting close would otherwise concatenate out of those quotes — injection).
func TestToolCommand_QuotedTokenSingle_Rejected(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("echo '__SID__'"))
	// SID is supplied, so the ONLY reason to fail is the quoted-token lint.
	_, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": "safe"})
	if err == nil {
		t.Fatal("expected load error for single-quoted token in tool_command, got nil")
	}
	var loadErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
	for _, want := range []string{"SID", "unquoted", "run"} {
		if !strings.Contains(loadErr.Reason, want) {
			t.Errorf("error %q does not mention %q", loadErr.Reason, want)
		}
	}
}

// TestToolCommand_QuotedTokenDouble_Rejected verifies the lint also rejects a token
// inside the author's DOUBLE quotes (inside "..." a substituted value is still
// re-expanded — $(...) would execute).
func TestToolCommand_QuotedTokenDouble_Rejected(t *testing.T) {
	// In DOT source the inner double-quotes are escaped \"; the lexer unescapes them
	// so node.ToolCommand becomes: echo "__SID__"
	dotPath := writeTempDot(t, toolDot(`echo \"__SID__\"`))
	_, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": "safe"})
	if err == nil {
		t.Fatal("expected load error for double-quoted token in tool_command, got nil")
	}
	var loadErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
	if !strings.Contains(loadErr.Reason, "SID") {
		t.Errorf("error %q does not name the offending token SID", loadErr.Reason)
	}
}

// TestToolCommand_UnquotedToken_Accepted verifies the legit unquoted form loads
// (the lint must not be over-eager).
func TestToolCommand_UnquotedToken_Accepted(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("sentry issue view __SID__ --json"))
	g, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": "PROJECT-9"})
	if err != nil {
		t.Fatalf("unquoted token must be accepted, got error: %v", err)
	}
	if got := toolNodeCommand(t, g); got != "sentry issue view 'PROJECT-9' --json" {
		t.Fatalf("tool_command = %q, want %q", got, "sentry issue view 'PROJECT-9' --json")
	}
}

// TestToolCommand_AuthorQuotesNoToken_Accepted verifies author-supplied quotes that
// contain NO template token are not rejected (the lint only fires on a token inside
// a quoted span).
func TestToolCommand_AuthorQuotesNoToken_Accepted(t *testing.T) {
	cases := []string{
		`[ \"$C\" = LOW ]`, // double-quoted shell var, no token
		`echo \"static text\"`,
		`grep -oE 'HIGH|LOW' f`, // single-quoted literal, no token
	}
	for _, src := range cases {
		dotPath := writeTempDot(t, toolDot(src))
		if _, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"UNUSED": "x"}); err != nil {
			t.Errorf("author quotes without a token must be accepted (%q), got error: %v", src, err)
		}
	}
}

// TestSubstituteBackstop_RejectsControlChar verifies the substitution-path backstop
// (covers the daemon-down local-persist path that bypasses the RPC) rejects a
// control-char value with a load error naming the offending key.
func TestSubstituteBackstop_RejectsControlChar(t *testing.T) {
	dotPath := writeTempDot(t, toolDot("echo __SID__"))
	_, err := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"SID": "x\ny"})
	if err == nil {
		t.Fatal("expected load error for control-char param value, got nil")
	}
	var loadErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}
	if !strings.Contains(loadErr.Reason, "SID") {
		t.Errorf("load error %q does not name the offending key SID", loadErr.Reason)
	}
}
