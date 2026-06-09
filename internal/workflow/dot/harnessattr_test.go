package dot

// harnessattr_test.go — tests for per-node harness selection attributes
// (codex-harness C4/T5, hk-u67of): harness, agent_runtime, reviewer_harness.
//
// These three node attributes supply the tier-3 (node) default for the
// harness-selection precedence walk in internal/daemon/harnessresolve.go
// (bead-label > per-queue > NODE > global). T5's job is to parse them off the
// DOT node into the typed AST so the dispatch path (C5) can read them.
//
// Test prefix: dotFixtureHarness (per implementer-protocol.md helper-prefix discipline).
//
// Spec coverage:
//   AR-025 — harness values MUST satisfy core.AgentType.Valid().
//   WG-002 — per-type attribute catalog (parsed into typed fields, not UnknownAttrs).
//
// Tags: mechanism
//
// Bead: hk-u67of [C4/T5]

import (
	"strings"
	"testing"
)

// dotFixtureHarnessNode finds the named node in a parsed graph or fails.
func dotFixtureHarnessNode(t *testing.T, g *Graph, id string) *Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found in parsed graph", id)
	return nil
}

// dotFixtureHarnessGraph returns a minimal graph whose implementer node carries
// harness=codex, agent_runtime=codex, and reviewer_harness=claude-code.
func dotFixtureHarnessGraph() string {
	return `digraph harness_test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [type="agentic", agent_type="implementer", handler_ref="codex",
        idempotency_class="non-idempotent",
        harness="codex", agent_runtime="codex", reviewer_harness="claude-code"];
  close [type="non-agentic", handler_ref="merge-handler",
         idempotency_class="idempotent"];

  work -> close [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
}`
}

// TestDotFixtureHarnessAttrsParsed verifies the three per-node harness attrs are
// parsed into typed fields (NOT UnknownAttrs) so the dispatch path can read them.
func TestDotFixtureHarnessAttrsParsed(t *testing.T) {
	g, err := Parse(dotFixtureHarnessGraph(), "harness.dot")
	if err != nil {
		t.Fatalf("Parse(harness): unexpected error: %v", err)
	}
	n := dotFixtureHarnessNode(t, g, "work")

	if n.Harness != "codex" {
		t.Errorf("node work: Harness = %q, want %q", n.Harness, "codex")
	}
	if n.AgentRuntime != "codex" {
		t.Errorf("node work: AgentRuntime = %q, want %q", n.AgentRuntime, "codex")
	}
	if n.ReviewerHarness != "claude-code" {
		t.Errorf("node work: ReviewerHarness = %q, want %q", n.ReviewerHarness, "claude-code")
	}

	// The attrs MUST be typed fields, not retained in UnknownAttrs (WG-002),
	// otherwise the dispatcher (forbidden from reading UnknownAttrs) cannot see them.
	for _, k := range []string{"harness", "agent_runtime", "reviewer_harness"} {
		if v, ok := n.UnknownAttrs[k]; ok {
			t.Errorf("node work: attr %q leaked into UnknownAttrs (=%q); must be a typed field", k, v)
		}
	}
}

// TestDotFixtureHarnessAbsentDefaultsEmpty verifies that a node without the
// attrs leaves the typed fields empty so resolveHarness falls through the
// node tier to the global default (claude-code).
func TestDotFixtureHarnessAbsentDefaultsEmpty(t *testing.T) {
	g, err := Parse(dotFixtureMinimal(), "minimal.dot")
	if err != nil {
		t.Fatalf("Parse(minimal): unexpected error: %v", err)
	}
	for _, n := range g.Nodes {
		if n.Harness != "" {
			t.Errorf("node %q: Harness = %q, want empty (attr absent)", n.ID, n.Harness)
		}
		if n.AgentRuntime != "" {
			t.Errorf("node %q: AgentRuntime = %q, want empty (attr absent)", n.ID, n.AgentRuntime)
		}
		if n.ReviewerHarness != "" {
			t.Errorf("node %q: ReviewerHarness = %q, want empty (attr absent)", n.ID, n.ReviewerHarness)
		}
	}
}

// TestDotFixtureHarnessInvalidValueStrictError verifies AR-025: a harness value
// that is not a valid agent_type is a strict parse error (so node-tier
// resolution never yields a malformed AgentType).
func TestDotFixtureHarnessInvalidValueStrictError(t *testing.T) {
	cases := []struct {
		name string
		attr string
	}{
		{"harness uppercase invalid", `harness="CODEX_BAD"`},
		{"agent_runtime uppercase invalid", `agent_runtime="Claude"`},
		{"reviewer_harness empty invalid", `reviewer_harness=""`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="idempotent", ` + tc.attr + `];
}`
			_, err := Parse(src, "bad.dot")
			if err == nil {
				t.Fatalf("expected strict parse error for %s, got nil", tc.attr)
			}
		})
	}
}

// TestDotFixtureHarnessAgentRuntimeConflict verifies that harness= and
// agent_runtime= (alias spellings of the same override) with DIFFERENT values
// is a strict error, while matching values are accepted.
func TestDotFixtureHarnessAgentRuntimeConflict(t *testing.T) {
	conflicting := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="idempotent", harness="codex", agent_runtime="claude-code"];
}`
	_, err := Parse(conflicting, "bad.dot")
	if err == nil {
		t.Fatal("expected strict error for harness/agent_runtime conflict, got nil")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("error %q does not mention the conflict", err.Error())
	}

	matching := `digraph ok {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="impl", handler_ref="h",
     idempotency_class="idempotent", harness="codex", agent_runtime="codex"];
}`
	g, err := Parse(matching, "ok.dot")
	if err != nil {
		t.Fatalf("Parse(matching harness/agent_runtime): unexpected error: %v", err)
	}
	n := dotFixtureHarnessNode(t, g, "n")
	if n.Harness != "codex" || n.AgentRuntime != "codex" {
		t.Errorf("matching values: Harness=%q AgentRuntime=%q, want both %q", n.Harness, n.AgentRuntime, "codex")
	}
}
