package daemon_test

// dot_attractor_parity_hk156il_test.go — combined scenario test: inline-prompt
// + non-committing + validating tool node (hk-156il, T6g).
//
// # What this file proves
//
//  1. Brief override (WG-040 §I.3): an implementer-class agentic node with
//     Prompt="X" causes agent-task.md to contain "X" as the task body — not
//     the bead description. Asserted by reading agent-task.md after the
//     multi-node cascade completes.
//
//  2. Non-committing cascade continues (WG-041 §I.4): an agentic node with
//     NonCommitting=true that exits cleanly without advancing HEAD returns
//     SUCCESS and the cascade advances to the downstream node. Asserted by
//     checking result.Success=true and that the terminal node is reached.
//
//  3. Validating tool node gates routing (WG-039 / HC-063): a non-agentic
//     shell node with tool_command= determines which terminal node is reached:
//       - exit 0 → SUCCESS → "close"          (success=true)
//       - exit 1 → FAIL   → "close-needs-attention" (needsAttention=true)
//
// Graph topology (assertions 2+3 run in one end-to-end pass):
//
//   investigate [agentic, implementer, non_committing=true, prompt=X]
//   → validate  [non-agentic, shell, tool_command=<cmd>]
//   → close / close-needs-attention  [terminals]
//
// Substrate: twin-free (/bin/sh -c "exit 0") — no tmux, no agent_ready, no
// real Claude. The sealed adapter registry makes waitAgentReady a no-op.
//
// Dependencies: T1 (hk-l8rpd), T2 (hk-sdnzj), T3 (hk-69asi).
// Spec refs: WG-039 (tool_command), WG-040 (prompt=), WG-041 (non_committing).
// Bead ref: hk-156il.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

const (
	// attractionNodePrompt is the inline prompt set on the investigate node.
	// assertion (1) verifies this appears in agent-task.md.
	attractionNodePrompt = "inline investigation task — verify the change compiles and passes linting"

	// attractionBeadBody is the bead description that the node prompt REPLACES.
	// assertion (1) verifies this does NOT appear in agent-task.md.
	attractionBeadBody = "bead body — must NOT appear in agent-task.md when prompt= overrides it"
)

// attractionParityGraph builds a 4-node DOT graph:
//
//	investigate [agentic, implementer, NonCommitting=true, Prompt=nodePrompt]
//	→ validate  [non-agentic, shell, ToolCommand=toolCmd]
//	→ close              (terminal, SUCCESS path)
//	→ close-needs-attention (terminal, FAIL path)
//
// toolCmd controls the exit code of the validate node:
//   - "exit 0" → SUCCESS → routes to "close"
//   - "exit 1" → FAIL   → routes to "close-needs-attention"
func attractionParityGraph(nodePrompt, toolCmd string) *dot.Graph {
	// Edge conditions use the WG-013 equality dialect (LHS from WG-014 whitelist).
	successCond := &dot.Condition{
		Clauses: []dot.Equality{{LHS: "outcome.status", Op: "==", RHS: "SUCCESS"}},
	}
	failCond := &dot.Condition{
		Clauses: []dot.Equality{{LHS: "outcome.status", Op: "==", RHS: "FAIL"}},
	}

	return &dot.Graph{
		StartNodeID:     "investigate",
		TerminalNodeIDs: []string{"close", "close-needs-attention"},
		Nodes: []*dot.Node{
			// investigate: agentic implementer; carries both WG-040 (prompt=) and
			// WG-041 (non_committing=true) to exercise assertion 1 + assertion 2.
			{
				ID:               "investigate",
				Type:             core.NodeTypeAgentic,
				AgentType:        "implementer",
				HandlerRef:       "claude-implementer",
				IdempotencyClass: "non-idempotent",
				Prompt:           nodePrompt,
				NonCommitting:    true,
			},
			// validate: non-agentic shell node; carries WG-039 (tool_command=)
			// to exercise assertion 3.
			{
				ID:               "validate",
				Type:             core.NodeTypeNonAgentic,
				HandlerRef:       "shell",
				IdempotencyClass: "idempotent",
				ToolCommand:      toolCmd,
			},
			// Terminal nodes: non-agentic noop (no tool_command → SUCCESS synth).
			// Terminal classification is by node identity per WG-021/WG-022.
			{
				ID:               "close",
				Type:             core.NodeTypeNonAgentic,
				HandlerRef:       "noop",
				IdempotencyClass: "idempotent",
			},
			{
				ID:               "close-needs-attention",
				Type:             core.NodeTypeNonAgentic,
				HandlerRef:       "noop",
				IdempotencyClass: "idempotent",
			},
		},
		Edges: []*dot.Edge{
			// (1) Entry: investigate → validate (unconditional).
			{FromNodeID: "investigate", ToNodeID: "validate", UnknownAttrs: map[string]string{}},
			// (2) SUCCESS path: validate → close.
			{
				FromNodeID:   "validate",
				ToNodeID:     "close",
				Condition:    successCond,
				ConditionRaw: "outcome.status == 'SUCCESS'",
				UnknownAttrs: map[string]string{},
			},
			// (3) FAIL path: validate → close-needs-attention.
			{
				FromNodeID:   "validate",
				ToNodeID:     "close-needs-attention",
				Condition:    failCond,
				ConditionRaw: "outcome.status == 'FAIL'",
				UnknownAttrs: map[string]string{},
			},
			// (4) Unconditional fallback (WG-011 invariant): catches outcomes with
			// no matching conditional edge (e.g. canceled).
			{FromNodeID: "validate", ToNodeID: "close-needs-attention", UnknownAttrs: map[string]string{}},
		},
		UnknownAttrs: map[string]string{},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// End-to-end cascade tests (assertions 1 + 2 + 3)
// ─────────────────────────────────────────────────────────────────────────────

// TestDotAttractionParity_CascadeSuccess drives the full 4-node graph end-to-end
// with tool_command="exit 0" and asserts all three acceptance criteria:
//
//  1. agent-task.md body = nodePrompt (not bead body) — WG-040 brief override.
//  2. Non-committing implementer (exit 0, no commit) → SUCCESS, cascade continues.
//  3. Tool node (exit 0) → SUCCESS → terminal node = "close" (success path).
func TestDotAttractionParity_CascadeSuccess(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-156il-attraction-parity-success-001")

	projectDir := nonCommittingFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	runID := implReadyFixtureRunID(t)
	adapterReg := NewSealedAdapterRegistryForTest(t)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 0"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		HookStore:           daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"attractor-parity combined test",
		attractionBeadBody,
		wtPath, parentSHA,
		attractionParityGraph(attractionNodePrompt, "exit 0"),
		"",
	)

	// ── Assertion 2: non-committing node succeeded + cascade reached terminal ──
	if !result.Success {
		t.Errorf("Success = false; want true — non_committing implementer clean exit + tool exit 0 (summary: %q)", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("NeedsAttention = true; want false (summary: %q)", result.Summary)
	}

	// ── Assertion 3: tool node gated routing to "close" ───────────────────────
	if result.TerminalNodeID != "close" {
		t.Errorf("TerminalNodeID = %q; want %q — tool exit 0 must route to close (summary: %q)",
			result.TerminalNodeID, "close", result.Summary)
	}

	// ── Assertion 1: brief override — agent-task.md body is the node prompt ───
	// buildClaudeLaunchSpec writes agent-task.md to wtPath/.harmonik/agent-task.md
	// before launching the handler. The investigate node runs first, so the file
	// reflects its nodePrompt.
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	raw, readErr := os.ReadFile(taskFile)
	if readErr != nil {
		t.Fatalf("agent-task.md not written to %s: %v", taskFile, readErr)
	}
	content := string(raw)

	if !strings.Contains(content, attractionNodePrompt) {
		t.Errorf("agent-task.md does not contain nodePrompt %q (WG-040 brief override not applied)\ncontent:\n%s",
			attractionNodePrompt, content)
	}
	if strings.Contains(content, attractionBeadBody) {
		t.Errorf("agent-task.md contains bead body %q; nodePrompt must replace it (WG-040)\ncontent:\n%s",
			attractionBeadBody, content)
	}
}

// TestDotAttractionParity_ToolNodeGatesRouting_FailPath drives the same graph
// with tool_command="exit 1" and asserts that the FAIL outcome routes the
// cascade to "close-needs-attention" (needs-attention terminal).
//
// This is assertion 3's FAIL path: the validating tool node gates the routing
// decision. A failing tool must surface as needsAttention=true so the operator
// is notified and the bead is NOT silently closed.
func TestDotAttractionParity_ToolNodeGatesRouting_FailPath(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-156il-attraction-parity-fail-001")

	projectDir := nonCommittingFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	runID := implReadyFixtureRunID(t)
	adapterReg := NewSealedAdapterRegistryForTest(t)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 0"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		HookStore:           daemon.ExportedNewHookSessionStore(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"attractor-parity fail-path test",
		attractionBeadBody,
		wtPath, parentSHA,
		attractionParityGraph(attractionNodePrompt, "exit 1"),
		"",
	)

	// Tool exit 1 → FAIL → must route to close-needs-attention.
	if result.Success {
		t.Errorf("Success = true; want false — tool exit 1 must route to close-needs-attention (summary: %q)", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("NeedsAttention = false; want true — close-needs-attention terminal must surface as needs-attention (summary: %q)", result.Summary)
	}
	if result.TerminalNodeID != "close-needs-attention" {
		t.Errorf("TerminalNodeID = %q; want %q — tool FAIL must route to close-needs-attention (summary: %q)",
			result.TerminalNodeID, "close-needs-attention", result.Summary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parser round-trip test (WG-040 + WG-041 attributes compose in DOT source)
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParser_AttractionParity_AllAttrsRoundTrip verifies that a DOT source
// carrying prompt=, non_committing="true", and tool_command= on the appropriate
// node types parses without errors and that all three attribute values are
// retained in the AST.
func TestDotParser_AttractionParity_AllAttrsRoundTrip(t *testing.T) {
	t.Parallel()

	src := `digraph {
  start_node = "investigate"
  terminal_node_ids = "close,close-needs-attention"

  investigate [type="agentic", agent_type="implementer", handler_ref="claude-implementer",
               idempotency_class="non-idempotent",
               prompt="inline investigation task",
               non_committing="true"]

  validate [type="non-agentic", handler_ref="shell",
            idempotency_class="idempotent",
            tool_command="exit 0"]

  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"]
  "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"]

  investigate -> validate
  validate -> close [condition="outcome.status == 'SUCCESS'"]
  validate -> "close-needs-attention" [condition="outcome.status == 'FAIL'"]
  validate -> "close-needs-attention"
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Build a node index for targeted assertions.
	nodesByID := make(map[string]*dot.Node, len(g.Nodes))
	for _, n := range g.Nodes {
		nodesByID[n.ID] = n
	}

	// ── investigate: prompt= (WG-040) ────────────────────────────────────────
	inv := nodesByID["investigate"]
	if inv == nil {
		t.Fatal("investigate node not found in parsed graph")
	}
	if inv.Prompt != "inline investigation task" {
		t.Errorf("investigate.Prompt = %q; want %q", inv.Prompt, "inline investigation task")
	}

	// ── investigate: non_committing="true" (WG-041) ──────────────────────────
	if !inv.NonCommitting {
		t.Errorf("investigate.NonCommitting = false; want true")
	}

	// ── validate: tool_command= (WG-039) ─────────────────────────────────────
	val := nodesByID["validate"]
	if val == nil {
		t.Fatal("validate node not found in parsed graph")
	}
	if val.ToolCommand != "exit 0" {
		t.Errorf("validate.ToolCommand = %q; want %q", val.ToolCommand, "exit 0")
	}

	// ── graph topology ────────────────────────────────────────────────────────
	if g.StartNodeID != "investigate" {
		t.Errorf("StartNodeID = %q; want %q", g.StartNodeID, "investigate")
	}
	if len(g.TerminalNodeIDs) != 2 {
		t.Errorf("TerminalNodeIDs len = %d; want 2", len(g.TerminalNodeIDs))
	}
	if len(g.Edges) != 4 {
		t.Errorf("Edges len = %d; want 4", len(g.Edges))
	}

	// No unexpected warnings from the three known attributes.
	for _, w := range g.Warnings {
		if strings.Contains(w.Message, "prompt") ||
			strings.Contains(w.Message, "non_committing") ||
			strings.Contains(w.Message, "tool_command") {
			t.Errorf("unexpected warning for a recognised attribute: %s", w.Message)
		}
	}
}
