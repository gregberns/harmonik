package dot

// harnessalias_hkozbio_test.go — agent_runtime= is an ALIAS of harness=, and the
// parser resolves it into Node.Harness so no consumer has to know it exists.
//
// # The defect this pins (hk-ozbio)
//
// The parser blessed agent_runtime= as a first-class alias (same AR-025 validity
// contract; a hard error when the two spellings disagree), but every consumer —
// internal/daemon/dot_cascade.go — reads only Node.Harness. A node written with
// agent_runtime= ALONE therefore parsed clean and dispatched with NO node-tier
// pin: dot_cascade's `effectiveNodeHarness.Valid()` guard was false, the pinned
// builder was skipped, and a tier-1 harness:<x> BEAD LABEL got to decide the
// node's harness. On a reviewer node with a harness:codex bead that routes the
// reviewer to codex, which emits no agent_ready and fails the run after a
// successful implement+commit (hk-pkxju shape) — the exact hole
// pinnedHarnessLaunchSpecBuilder was written to close, reopened by a spelling
// the parser endorses.
//
// The fix normalises at the parse boundary, so the equivalence the parser
// asserts is the equivalence the dispatcher gets.
//
// Test prefix: dotAliasHkozbio (per implementer-protocol.md helper-prefix discipline).
//
// Spec coverage:
//
//	AR-025 — harness values MUST satisfy core.AgentType.Valid().
//	WG-002 — per-type attribute catalog (typed fields, not UnknownAttrs).
//
// Tags: mechanism
//
// Bead: hk-ozbio

import "testing"

// dotAliasHkozbioGraph returns a two-node graph where the reviewer-class node
// carries ONLY the alias spelling and the implementer carries ONLY the
// canonical one. Both must end up with the same typed node-tier harness.
func dotAliasHkozbioGraph() string {
	return `digraph harness_alias {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [type="agentic", agent_type="implementer", handler_ref="codex",
        idempotency_class="non-idempotent", harness="codex"];
  review [type="agentic", agent_type="reviewer", handler_ref="claude",
          idempotency_class="non-idempotent", agent_runtime="claude-code"];
  close [type="non-agentic", handler_ref="merge-handler",
         idempotency_class="idempotent"];

  work -> review [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
  review -> close [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="b"];
}`
}

// TestDotAliasHkozbioAgentRuntimeResolvesIntoHarness proves the alias spelling
// alone populates Node.Harness — the only field the dispatch path reads.
func TestDotAliasHkozbioAgentRuntimeResolvesIntoHarness(t *testing.T) {
	g, err := Parse(dotAliasHkozbioGraph(), "harness_alias.dot")
	if err != nil {
		t.Fatalf("Parse(harness_alias): unexpected error: %v", err)
	}
	review := dotFixtureHarnessNode(t, g, "review")

	if review.Harness != "claude-code" {
		t.Errorf("node review: Harness = %q, want %q — agent_runtime= must resolve into the field consumers read (hk-ozbio)",
			review.Harness, "claude-code")
	}
	// The source spelling is still reported, so the AST does not lie about what
	// the author wrote.
	if review.AgentRuntime != "claude-code" {
		t.Errorf("node review: AgentRuntime = %q, want %q (source spelling preserved)",
			review.AgentRuntime, "claude-code")
	}
}

// TestDotAliasHkozbioSpellingsAreEquivalent is the equivalence the parser
// asserts at :893 and the dispatcher must inherit: a node using ONLY
// agent_runtime= resolves the same node-tier harness as one using ONLY
// harness= with the same value.
func TestDotAliasHkozbioSpellingsAreEquivalent(t *testing.T) {
	node := func(t *testing.T, attr string) *Node {
		t.Helper()
		src := `digraph one {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="reviewer", handler_ref="h",
     idempotency_class="idempotent", ` + attr + `];
}`
		g, err := Parse(src, "one.dot")
		if err != nil {
			t.Fatalf("Parse(%s): unexpected error: %v", attr, err)
		}
		return dotFixtureHarnessNode(t, g, "n")
	}

	canonical := node(t, `harness="claude-code"`)
	alias := node(t, `agent_runtime="claude-code"`)

	if canonical.Harness != alias.Harness {
		t.Errorf("spellings diverge: harness= gives Harness=%q, agent_runtime= gives Harness=%q; the parser asserts they are the same override",
			canonical.Harness, alias.Harness)
	}
}

// TestDotAliasHkozbioNoAliasLeavesHarnessEmpty guards the other direction: the
// normalisation must not manufacture a pin on a node that declared neither
// spelling, or every unpinned node would silently become pinned to "".
func TestDotAliasHkozbioNoAliasLeavesHarnessEmpty(t *testing.T) {
	g, err := Parse(dotFixtureMinimal(), "minimal.dot")
	if err != nil {
		t.Fatalf("Parse(minimal): unexpected error: %v", err)
	}
	for _, n := range g.Nodes {
		if n.Harness != "" {
			t.Errorf("node %q: Harness = %q, want empty (neither spelling present)", n.ID, n.Harness)
		}
	}
}

// TestDotAliasHkozbioInvalidAliasDoesNotPropagate proves an alias value that
// fails AR-025 is a strict parse error and never reaches Node.Harness — the
// normalisation must not become a back door around the validity contract.
func TestDotAliasHkozbioInvalidAliasDoesNotPropagate(t *testing.T) {
	src := `digraph bad {
  schema_version="1";
  version="1.0";
  start_node="n";
  terminal_node_ids="n";
  n [type="agentic", agent_type="reviewer", handler_ref="h",
     idempotency_class="idempotent", agent_runtime="Claude_BAD"];
}`
	g, err := Parse(src, "bad.dot")
	if err == nil {
		t.Fatal("expected strict parse error for an invalid agent_runtime value, got nil")
	}
	if g != nil {
		for _, n := range g.Nodes {
			if n.Harness != "" {
				t.Errorf("node %q: Harness = %q, want empty — an invalid alias must not propagate", n.ID, n.Harness)
			}
		}
	}
}
