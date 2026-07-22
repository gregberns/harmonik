package daemon_test

// reviewer_harness_hkiv748_test.go — reviewer harness resolution end-to-end
// (codex-harness C5/T14, hk-iv748).
//
// # What this file proves
//
//  1. DEFAULT (review-loop mode): when no reviewer_harness override is present, the
//     reviewer specBuilder is built with nodeDefault = the implementer's resolved
//     agent type — so the reviewer always uses the same harness. Concretely:
//     routedLaunchSpecBuilder with nodeDefault=codex resolves to codex for the
//     reviewer, matching what runReviewLoop builds after getting
//     implArtifacts.resolvedAgentType.
//
//  2. OVERRIDE (DOT mode): the reviewer_harness= attr on the implementer's DOT node
//     is threaded as reviewerHarnessOverride into dispatchDotAgenticNode. When the
//     override is set to "claude-code", resolveHarness produces claude-code for the
//     reviewer even when the implementer resolved to codex. This proves the chain:
//       dot.Parse → node.ReviewerHarness → reviewerHarnessOverride → resolveHarness
//
//  3. BYTE-IDENTICAL all-claude case: nodeDefault=claude-code resolves to claude-code,
//     matching the pre-T14 reviewer=claude behaviour.
//
// The tests use ExportedResolveHarness (hk-y01k6) to drive the resolution walk
// directly, avoiding the need for real binaries or a running daemon. The wiring
// in reviewloop.go and dot_cascade.go is verified at compile time.
//
// Helper prefix: hkiv748 (per implementer-protocol.md helper-prefix discipline).
//
// Tags: mechanism
//
// Bead: hk-iv748 [C5/T14]. Spec: codex-harness C5, AR-025.

import (
	"context"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// hkiv748Bus is a no-op event collector satisfying handlercontract.EventEmitter.
type hkiv748Bus struct {
	mu     sync.Mutex
	events []core.EventType
}

func (b *hkiv748Bus) Emit(_ context.Context, et core.EventType, _ []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, et)
	return nil
}

func (b *hkiv748Bus) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(context.Background(), et, payload)
}

var _ handlercontract.EventEmitter = (*hkiv748Bus)(nil)

// hkiv748GraphWithReviewerHarness returns a minimal DOT graph whose implementer node
// carries harness="codex" and reviewer_harness="claude-code".
func hkiv748GraphWithReviewerHarness() string {
	return `digraph reviewer_harness_test {
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

// hkiv748FindNode returns the named node or fails.
func hkiv748FindNode(t *testing.T, g *dot.Graph, id string) *dot.Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found in graph", id)
	return nil
}

// TestReviewerHarnessDefaultSameAsImplementer proves DEFAULT behaviour:
// when no reviewer_harness override is present, the reviewer specBuilder resolves to
// the same harness as the implementer (nodeDefault = implementer's resolvedAgentType).
//
// This binds to the reviewloop.go change: routedLaunchSpecBuilder is called with
// nodeDefault = implArtifacts.resolvedAgentType (instead of the shared deps.launchSpecBuilder
// whose nodeDefault is empty). For an all-claude run the result is byte-identical to
// pre-T14: reviewer = claude-code.
//
// SUPERSEDED IN PART by hk-pkxju: reviewloop.go no longer passes a SessionIDCaptured
// implementer harness (codex, pi) straight through as the reviewer's tier-3 nodeDefault
// — reviewerDefaultHarness swaps it for claude-code first, because no SessionIDCaptured
// harness can review today. Case A below still holds as a statement about the
// resolveHarness TIER-3 WALK (feed it codex, get codex), which hk-pkxju does not change;
// it is no longer a statement about what the review loop feeds in. The hk-pkxju
// behaviour is covered in reviewer_never_inherits_captured_hkpkxju_test.go.
func TestReviewerHarnessDefaultSameAsImplementer(t *testing.T) {
	t.Parallel()

	bus := &hkiv748Bus{}
	// Case A: implementer resolved to codex → reviewer should also resolve to codex.
	bead := core.BeadRecord{
		BeadID:   core.BeadID("hkiv748-default-codex"),
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   nil, // no tier-1 bead-label harness override
	}
	got := daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),       // queue default absent
		core.AgentTypeCodex,      // tier-3: implementer's resolvedAgentType = codex
		core.AgentTypeClaudeCode, // global default
		bus,
	)
	if got != core.AgentTypeCodex {
		t.Errorf("DEFAULT (codex implementer): reviewer resolved to %q; want %q", got, core.AgentTypeCodex)
	}

	// Case B: implementer resolved to claude-code → reviewer should also resolve to claude-code.
	got = daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),       // queue default absent
		core.AgentTypeClaudeCode, // tier-3: implementer's resolvedAgentType = claude-code
		core.AgentTypeClaudeCode, // global default
		bus,
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("DEFAULT (claude implementer): reviewer resolved to %q; want %q (byte-identical pre-T14)", got, core.AgentTypeClaudeCode)
	}
}

// TestReviewerHarnessOverrideFromDotNode proves the OVERRIDE behaviour:
// the reviewer_harness= attr parsed off the implementer's DOT node drives
// resolveHarness's nodeDefault for the reviewer, so the reviewer can use a
// different harness even when the implementer resolved to codex.
//
// This binds to the dot_cascade.go change: lastImplementerReviewerHarness is set
// from node.ReviewerHarness when dispatching the implementer, then passed as
// reviewerHarnessOverride to dispatchDotAgenticNode for the reviewer node.
func TestReviewerHarnessOverrideFromDotNode(t *testing.T) {
	t.Parallel()

	g, err := dot.Parse(hkiv748GraphWithReviewerHarness(), "reviewer_harness.dot")
	if err != nil {
		t.Fatalf("dot.Parse: unexpected error: %v", err)
	}
	work := hkiv748FindNode(t, g, "work")

	// The implementer node's own harness resolved to codex.
	if work.Harness != "codex" {
		t.Fatalf("work.Harness = %q, want %q", work.Harness, "codex")
	}
	// The reviewer_harness override is claude-code.
	if work.ReviewerHarness != "claude-code" {
		t.Fatalf("work.ReviewerHarness = %q, want %q", work.ReviewerHarness, "claude-code")
	}

	bus := &hkiv748Bus{}
	bead := core.BeadRecord{
		BeadID:   core.BeadID("hkiv748-override"),
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   nil,
	}

	// Simulate what dispatchDotAgenticNode does when isReviewer=true:
	// reviewerHarnessOverride = lastImplementerReviewerHarness (= node.ReviewerHarness)
	// is passed as the nodeDefault to resolveHarness.
	reviewerHarnessOverride := core.AgentType(work.ReviewerHarness) // = "claude-code"
	got := daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),       // queue default absent
		reviewerHarnessOverride,  // tier-3: reviewer_harness override from implementer node
		core.AgentTypeClaudeCode, // global default
		bus,
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("OVERRIDE (reviewer_harness=claude-code): reviewer resolved to %q; want %q", got, core.AgentTypeClaudeCode)
	}

	// Sanity: if there were no override, and the reviewer node had no harness= attr,
	// the reviewer would fall through to the global default (claude-code in this case).
	// This mirrors dispatchDotAgenticNode's DEFAULT path (effectiveNodeHarness empty →
	// specBuilder = deps.launchSpecBuilder → global default applies).
	gotNoOverride := daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),       // queue default absent
		core.AgentType(""),       // node default absent (no override, no reviewer node harness)
		core.AgentTypeClaudeCode, // global default = claude-code
		bus,
	)
	if gotNoOverride != core.AgentTypeClaudeCode {
		t.Errorf("no-override fallback: reviewer resolved to %q; want global default %q", gotNoOverride, core.AgentTypeClaudeCode)
	}
}

// TestReviewerHarnessAllClaudeByteIdentical proves that for an all-claude run
// (no reviewer_harness attr, no codex bead label) the reviewer resolution is
// byte-identical to the pre-T14 behaviour: reviewer = claude-code.
func TestReviewerHarnessAllClaudeByteIdentical(t *testing.T) {
	t.Parallel()

	bus := &hkiv748Bus{}
	bead := core.BeadRecord{
		BeadID:   core.BeadID("hkiv748-all-claude"),
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   nil,
	}

	// In review-loop mode: nodeDefault = implArtifacts.resolvedAgentType = claude-code.
	// In DOT mode: no reviewerHarnessOverride (= empty), reviewer node has no harness= attr.
	// Both collapse to: resolveHarness with all-empty upper tiers → falls to global default.
	got := daemon.ExportedResolveHarness(
		t.Context(),
		bead,
		core.AgentType(""),       // queue default absent
		core.AgentTypeClaudeCode, // implementer's resolvedAgentType = claude-code
		core.AgentTypeClaudeCode, // global default
		bus,
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("all-claude run: reviewer resolved to %q; want %q (byte-identical pre-T14)", got, core.AgentTypeClaudeCode)
	}
}
