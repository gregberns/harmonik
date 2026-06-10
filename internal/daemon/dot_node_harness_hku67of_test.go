package daemon_test

// dot_node_harness_hku67of_test.go — node-tier harness selection: a DOT node's
// parsed harness/agent_runtime/reviewer_harness attributes feed the tier-3
// (node) default of the harness-selection precedence walk (resolveHarness).
//
// # What this file proves (codex-harness C4/T5, hk-u67of)
//
//  1. A DOT node carrying harness="codex" parses into dot.Node.Harness, and when
//     that field is passed as resolveHarness's nodeDefault (with no bead label
//     and no queue default) it resolves to codex at the node tier — overriding
//     a claude-code global default.
//
//  2. A sibling node carrying NO harness attribute parses to an empty
//     dot.Node.Harness, so resolveHarness falls through the node tier to the
//     global default (claude-code).
//
//  3. reviewer_harness parses independently into dot.Node.ReviewerHarness so the
//     reviewer node (C5/T14, hk-iv748) can resolve a different harness than the
//     implementer.
//
// This binds the T5 parser change (internal/workflow/dot) to the T4 resolver
// (internal/daemon/harnessresolve.go, hk-y01k6) end-to-end at the node tier.
//
// Helper prefix: hku67of (per implementer-protocol.md helper-prefix discipline).
//
// Bead: hk-u67of [C4/T5]. Spec: AR-025; codex-harness C4-selection-spec.

import (
	"context"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// hku67ofBus is a no-op event collector satisfying handlercontract.EventEmitter.
type hku67ofBus struct {
	mu     sync.Mutex
	events []core.EventType
}

func (b *hku67ofBus) Emit(_ context.Context, et core.EventType, _ []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, et)
	return nil
}

func (b *hku67ofBus) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(context.Background(), et, payload)
}

var _ handlercontract.EventEmitter = (*hku67ofBus)(nil)

// hku67ofGraph returns a work→close graph whose implementer node selects the
// codex harness for itself and a claude-code harness for its reviewer.
func hku67ofGraph() string {
	return `digraph node_harness {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [type="agentic", agent_type="implementer", handler_ref="codex",
        idempotency_class="non-idempotent",
        harness="codex", reviewer_harness="claude-code"];
  close [type="non-agentic", handler_ref="merge-handler",
         idempotency_class="idempotent"];

  work -> close [condition="outcome.status == 'SUCCESS'", weight="10", ordering_key="a"];
}`
}

func hku67ofNode(t *testing.T, g *dot.Graph, id string) *dot.Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found", id)
	return nil
}

// TestNodeTierHarnessFromParsedDOT proves the parsed node attr drives the node
// tier of resolveHarness.
func TestNodeTierHarnessFromParsedDOT(t *testing.T) {
	t.Parallel()

	g, err := dot.Parse(hku67ofGraph(), "node_harness.dot")
	if err != nil {
		t.Fatalf("dot.Parse: unexpected error: %v", err)
	}
	work := hku67ofNode(t, g, "work")

	// The implementer node parsed harness="codex" into the typed field.
	if work.Harness != "codex" {
		t.Fatalf("work.Harness = %q, want %q", work.Harness, "codex")
	}
	// And reviewer_harness="claude-code" independently.
	if work.ReviewerHarness != "claude-code" {
		t.Errorf("work.ReviewerHarness = %q, want %q", work.ReviewerHarness, "claude-code")
	}

	bus := &hku67ofBus{}
	bead := core.BeadRecord{
		BeadID:   core.BeadID("hku67of-node-tier"),
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   nil, // no bead-label harness override → tier 1 absent
	}

	// Feed the parsed node attr as the node-tier default with no bead label and
	// no queue default; global default = claude-code. Node tier must win.
	got := daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),           // queueDefault absent
		core.AgentType(work.Harness), // nodeDefault from parsed DOT
		core.AgentTypeClaudeCode,     // globalDefault
		bus,
	)
	if got != core.AgentTypeCodex {
		t.Errorf("resolveHarness with node harness=codex: got %q; want %q", got, core.AgentTypeCodex)
	}
}

// TestNodeTierAbsentFallsThroughToGlobal proves a node with no harness attr
// parses to an empty field, so resolveHarness falls through to the global default.
func TestNodeTierAbsentFallsThroughToGlobal(t *testing.T) {
	t.Parallel()

	g, err := dot.Parse(hku67ofGraph(), "node_harness.dot")
	if err != nil {
		t.Fatalf("dot.Parse: unexpected error: %v", err)
	}
	// "close" is non-agentic and carries no harness attr.
	closeNode := hku67ofNode(t, g, "close")
	if closeNode.Harness != "" {
		t.Fatalf("close.Harness = %q, want empty", closeNode.Harness)
	}

	bus := &hku67ofBus{}
	bead := core.BeadRecord{
		BeadID:   core.BeadID("hku67of-fallthrough"),
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}

	got := daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),                // queueDefault absent
		core.AgentType(closeNode.Harness), // nodeDefault empty → tier 3 absent
		core.AgentTypeClaudeCode,          // globalDefault
		bus,
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("resolveHarness with absent node harness: got %q; want %q (global)", got, core.AgentTypeClaudeCode)
	}
}
