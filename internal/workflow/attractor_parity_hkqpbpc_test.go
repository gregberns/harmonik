package workflow_test

// attractor_parity_hkqpbpc_test.go — conformance gate: param substitution
// covers prompt/tool_command/goal uniformly before parse (T6f).
//
// Acceptance criteria (bead hk-qpbpc, T6f — WG-039, WG-040, WG-044, WG-045, WG-046):
//
//  1. AllThreeSitesSubstituted: a __TARGET__ token in goal (WG-044), an agentic
//     node prompt (WG-040), and a non-agentic node tool_command (WG-039) are ALL
//     replaced by a single LoadDotWorkflowWithParams call — one pre-parse pass
//     covers every attribute site.
//
//  2. OrderingInvariant: residual __TOKEN__ tokens at any of the three sites
//     cause a load-time error (*ErrWorkflowLoad wrapping *ErrResidualToken)
//     before the parser ever runs — load → substitute → parse → validate →
//     dispatch; the parser must never see an unsubstituted placeholder.
//
//  3. UniformParity: the same token substitutes to the same value at every
//     site; substitution is a single source-text pass (WG-045), not per-field.
//
// Substrate: LoadDotWorkflowWithParams (no real claude / twin required).
// Bead ref: hk-qpbpc (T6f). Depends on hk-l8rpd (T1), hk-sdnzj (T2), hk-55zv2 (T5).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow"
)

// attractorParityDot is a minimal .dot workflow embedding __TARGET__ in all
// three substitution sites: graph-level goal (WG-044), an agentic node prompt
// (WG-040), and a non-agentic tool_command (WG-039).
const attractorParityDot = `digraph attractor_parity {
	schema_version="1";
	version="1.0";
	start_node="impl";
	terminal_node_ids="verify";
	goal="Target: __TARGET__";

	impl [type="agentic"; agent_type="claude-code"; handler_ref="builtin:claude-code"; idempotency_class="non-idempotent"; prompt="Implement __TARGET__"];
	verify [type="non-agentic"; handler_ref="shell"; idempotency_class="idempotent"; tool_command="echo __TARGET__ && exit 0"];

	impl -> verify;
}`

// writeTempDot writes src to a temp .dot file and returns its path.
func writeTempDot(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "workflow.dot")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("writeTempDot: %v", err)
	}
	return p
}

// TestAttractionParityAllThreeSitesSubstituted verifies that a single
// LoadDotWorkflowWithParams call replaces __TARGET__ uniformly in goal,
// prompt, and tool_command before parse (WG-045 scope + WG-046 ordering).
func TestAttractionParityAllThreeSitesSubstituted(t *testing.T) {
	const wantValue = "issue-172"

	dotPath := writeTempDot(t, attractorParityDot)
	params := map[string]string{"TARGET": wantValue}

	graph, err := workflow.LoadDotWorkflowWithParams(dotPath, params)
	if err != nil {
		t.Fatalf("LoadDotWorkflowWithParams: %v", err)
	}

	// Site 1 — graph-level goal (WG-044).
	if graph.Goal != "Target: "+wantValue {
		t.Errorf("goal: got %q, want %q", graph.Goal, "Target: "+wantValue)
	}

	// Site 2 — agentic node prompt (WG-040).
	// Site 3 — non-agentic tool_command (WG-039).
	for _, n := range graph.Nodes {
		switch n.ID {
		case "impl":
			if n.Prompt != "Implement "+wantValue {
				t.Errorf("impl.prompt: got %q, want %q", n.Prompt, "Implement "+wantValue)
			}
		case "verify":
			wantCmd := "echo " + wantValue + " && exit 0"
			if n.ToolCommand != wantCmd {
				t.Errorf("verify.tool_command: got %q, want %q", n.ToolCommand, wantCmd)
			}
		}
	}

	// Confirm no residual __TARGET__ tokens remain anywhere in the parsed graph.
	if graph.Goal != "" && containsTemplateToken(graph.Goal) {
		t.Errorf("goal still contains unsubstituted token: %q", graph.Goal)
	}
	for _, n := range graph.Nodes {
		if containsTemplateToken(n.Prompt) {
			t.Errorf("node %q prompt still contains unsubstituted token: %q", n.ID, n.Prompt)
		}
		if containsTemplateToken(n.ToolCommand) {
			t.Errorf("node %q tool_command still contains unsubstituted token: %q", n.ID, n.ToolCommand)
		}
	}
}

// TestAttractionParityOrderingInvariant verifies that residual __TARGET__
// tokens at all three sites produce a load-time error from the substitution
// step — not a parse error — confirming the ordering invariant (WG-046):
// substitute → parse, never the reverse.
func TestAttractionParityOrderingInvariant(t *testing.T) {
	dotPath := writeTempDot(t, attractorParityDot)

	// Supply no params → all three __TARGET__ occurrences remain.
	_, err := workflow.LoadDotWorkflowWithParams(dotPath, nil)
	if err == nil {
		t.Fatal("expected load error for residual tokens, got nil")
	}

	var wlErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &wlErr) {
		t.Fatalf("expected *ErrWorkflowLoad, got %T: %v", err, err)
	}

	// Ordering check: the Reason must be a substitution failure, NOT a parse
	// failure — substitution must fire (and fail) before the parser runs (WG-046).
	// ErrWorkflowLoad.Reason carries the stringified cause.
	reason := wlErr.Reason
	if !strings.Contains(reason, "template substitution failed") {
		t.Errorf("expected Reason to contain %q (substitution fires before parse), got %q",
			"template substitution failed", reason)
	}
	if strings.Contains(reason, "parse failed") {
		t.Errorf("Reason must not say %q — ordering invariant violated: parser ran before substitution",
			"parse failed")
	}

	// The reason must name the unresolved token.
	if !strings.Contains(reason, "TARGET") {
		t.Errorf("Reason %q does not mention unresolved token TARGET", reason)
	}
}

// TestAttractionParityUniformSubstitution verifies that the same __TARGET__
// token substitutes to the same value at all three sites — the single-pass
// source-text substitution (WG-045) is per-token, not per-field.
func TestAttractionParityUniformSubstitution(t *testing.T) {
	const wantValue = "uniform-check"

	dotPath := writeTempDot(t, attractorParityDot)
	params := map[string]string{"TARGET": wantValue}

	graph, err := workflow.LoadDotWorkflowWithParams(dotPath, params)
	if err != nil {
		t.Fatalf("LoadDotWorkflowWithParams: %v", err)
	}

	var goalVal, promptVal, cmdVal string
	goalVal = graph.Goal
	for _, n := range graph.Nodes {
		switch n.ID {
		case "impl":
			promptVal = n.Prompt
		case "verify":
			cmdVal = n.ToolCommand
		}
	}

	// All three must contain wantValue and must not contain the raw token.
	for site, val := range map[string]string{
		"goal":         goalVal,
		"impl.prompt":  promptVal,
		"verify.cmd":   cmdVal,
	} {
		if !strings.Contains(val, wantValue) {
			t.Errorf("%s: substituted value %q does not contain %q", site, val, wantValue)
		}
		if containsTemplateToken(val) {
			t.Errorf("%s: residual __TOKEN__ in substituted value %q", site, val)
		}
	}
}

// containsTemplateToken reports whether s contains a __UPPERCASE__ placeholder.
// Looks for the pattern __ followed by an uppercase letter (WG-045 token grammar).
func containsTemplateToken(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if s[i] == '_' && s[i+1] == '_' && s[i+2] >= 'A' && s[i+2] <= 'Z' {
			return true
		}
	}
	return false
}
