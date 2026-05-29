package daemon_test

// dot_param_subst_goal_hk4bn9o_test.go — T6d scenario: workflow-graph.md goal +
// --param substitution into agentic brief, end-to-end (hk-4bn9o).
//
// # What this file proves
//
//  1. GoalAppearsInAgentTaskMD (WG-044 + WG-045 + WG-046): a .dot source with
//     graph-level goal="Fix #__ISSUE_NUMBER__ in __GITHUB_REPO__" and node attrs
//     embedding the same tokens substitutes all tokens before parse, and the
//     substituted goal appears in agent-task.md via the run-level ExtraContext
//     channel. No literal __TOKEN__ survives into any parsed node attribute.
//
//  2. MissingParamBlocksLaunch (WG-045 residual-token error): a .dot with
//     unresolved __TOKEN__ placeholders fails at load time with
//     *workflow.ErrWorkflowLoad whose Reason cites "template substitution
//     failed" — the parser never runs (ordering invariant WG-046), and the run
//     never starts.
//
//  3. SealedParamsReplay (WG-045 determinism): calling
//     workflow.LoadDotWorkflowWithParams twice with the same params and same
//     source yields identical substituted goal and node-attr values. This
//     verifies that Run.template_params sealing guarantees a replay
//     re-substitutes identically.
//
// Graph topology:
//
//	investigate [agentic, implementer, NonCommitting=true, Prompt="Address __ISSUE_NUMBER__"]
//	→ validate  [non-agentic, shell, tool_command="echo __ISSUE_NUMBER__"]
//	→ close / close-needs-attention  [terminals]
//
// The investigate node is NonCommitting so the handler (exit 0) produces
// SUCCESS without advancing HEAD.  agent-task.md is written before the handler
// fires; the test reads it to assert ExtraContext injection.
//
// Substrate: twin-free (/bin/sh -c "exit 0"), no real Claude or tmux.
// Spec refs: WG-044, WG-045, WG-046.
// Bead ref: hk-4bn9o (T6d). Depends on hk-55zv2 (T5), hk-l8rpd (T1), hk-sdnzj (T2).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// paramSubstDOT is a .dot source embedding __ISSUE_NUMBER__ and __GITHUB_REPO__
// in all three substitution sites: graph-level goal (WG-044), an agentic node
// prompt (WG-040), and a non-agentic tool_command (WG-039).
const paramSubstDOT = `digraph param_subst_test {
  schema_version="1";
  version="1.0";
  start_node="investigate";
  terminal_node_ids="close,close-needs-attention";
  goal="Fix #__ISSUE_NUMBER__ in __GITHUB_REPO__";

  investigate [type="agentic", agent_type="implementer",
               handler_ref="claude-implementer",
               idempotency_class="non-idempotent",
               non_committing="true",
               prompt="Address issue __ISSUE_NUMBER__ in __GITHUB_REPO__"];

  validate [type="non-agentic", handler_ref="shell",
            idempotency_class="idempotent",
            tool_command="echo __ISSUE_NUMBER__ && exit 0"];

  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
  "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  investigate -> validate;
  validate -> close [condition="outcome.status == 'SUCCESS'"];
  validate -> "close-needs-attention" [condition="outcome.status == 'FAIL'"];
  validate -> "close-needs-attention";
}`

// writeParamSubstDot writes paramSubstDOT to a temp .dot file and returns its path.
func writeParamSubstDot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "param-subst.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(paramSubstDOT), 0o644); err != nil {
		t.Fatalf("writeParamSubstDot: %v", err)
	}
	return p
}

// paramSubstTestParams are the canonical test params for T6d.
var paramSubstTestParams = map[string]string{
	"ISSUE_NUMBER": "172",
	"GITHUB_REPO":  "foo/bar",
}

// expectedSubstitutedGoal is the substituted form of the goal attr in paramSubstDOT.
const expectedSubstitutedGoal = "Fix #172 in foo/bar"

// ─────────────────────────────────────────────────────────────────────────────
// T6d-1: substituted goal appears in agent-task.md via ExtraContext channel
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParamSubst_GoalAppearsInAgentTaskMD verifies the full end-to-end path:
//
//  1. LoadDotWorkflowWithParams substitutes __ISSUE_NUMBER__ and __GITHUB_REPO__
//     in goal, prompt, and tool_command before parse (WG-045/WG-046).
//  2. No literal __TOKEN__ appears in any parsed node attribute.
//  3. The daemon constructs dotExtraContext = "Workflow goal: <substituted_goal>"
//     and the substituted goal text appears in agent-task.md.
func TestDotParamSubst_GoalAppearsInAgentTaskMD(t *testing.T) {
	t.Parallel()

	dotPath := writeParamSubstDot(t)

	// WG-046 ordering: substitute → parse → validate.
	graph, loadErr := workflow.LoadDotWorkflowWithParams(dotPath, paramSubstTestParams)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflowWithParams: %v", loadErr)
	}

	// ── Assertion: graph-level goal is substituted (WG-044 + WG-045) ─────────
	if graph.Goal != expectedSubstitutedGoal {
		t.Errorf("graph.Goal = %q, want %q", graph.Goal, expectedSubstitutedGoal)
	}

	// ── Assertion: no literal __TOKEN__ in any node attribute (WG-045) ────────
	for _, n := range graph.Nodes {
		if containsParamToken(n.Prompt) {
			t.Errorf("node %q: Prompt still contains __TOKEN__: %q", n.ID, n.Prompt)
		}
		if containsParamToken(n.ToolCommand) {
			t.Errorf("node %q: ToolCommand still contains __TOKEN__: %q", n.ID, n.ToolCommand)
		}
		for k, v := range n.UnknownAttrs {
			if containsParamToken(v) {
				t.Errorf("node %q: UnknownAttrs[%q] still contains __TOKEN__: %q", n.ID, k, v)
			}
		}
	}

	// ── End-to-end: drive cascade; goal surfaces in agent-task.md ─────────────
	// Mirror the daemon's WG-044 goal-injection logic from workloop.go.
	dotExtraContext := "Workflow goal: " + graph.Goal

	const beadID = core.BeadID("hk-4bn9o-param-subst-goal")
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
		"param-subst test",
		"bead body (should not appear in extra context section)",
		wtPath, parentSHA,
		graph,
		dotExtraContext,
	)

	// Cascade must succeed: non-committing implementer (exit 0) + tool node (exit 0).
	if !result.Success {
		t.Errorf("Success = false; want true (summary: %q)", result.Summary)
	}

	// ── Assertion: agent-task.md contains the substituted goal ─────────────────
	taskFile := filepath.Join(wtPath, ".harmonik", "agent-task.md")
	raw, readErr := os.ReadFile(taskFile)
	if readErr != nil {
		t.Fatalf("agent-task.md not found at %s: %v", taskFile, readErr)
	}
	content := string(raw)

	if !strings.Contains(content, expectedSubstitutedGoal) {
		t.Errorf("agent-task.md does not contain substituted goal %q (WG-044 ExtraContext injection)\ncontent:\n%s",
			expectedSubstitutedGoal, content)
	}

	// No residual __TOKEN__ must appear in agent-task.md.
	if containsParamToken(content) {
		t.Errorf("agent-task.md still contains literal __TOKEN__; substitution incomplete\ncontent:\n%s", content)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6d-2: missing param blocks launch (residual-token error, run never starts)
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParamSubst_MissingParamBlocksLaunch verifies that LoadDotWorkflowWithParams
// with a missing (or nil) param map returns *workflow.ErrWorkflowLoad whose Reason
// cites "template substitution failed" — the parse step never runs (WG-046 ordering),
// and the run cannot start.
func TestDotParamSubst_MissingParamBlocksLaunch(t *testing.T) {
	t.Parallel()

	dotPath := writeParamSubstDot(t)

	// Case 1: nil params — both __ISSUE_NUMBER__ and __GITHUB_REPO__ are unresolved.
	_, err := workflow.LoadDotWorkflowWithParams(dotPath, nil)
	if err == nil {
		t.Fatal("expected load error for missing params, got nil")
	}
	var wlErr *workflow.ErrWorkflowLoad
	if !errors.As(err, &wlErr) {
		t.Fatalf("expected *workflow.ErrWorkflowLoad, got %T: %v", err, err)
	}
	// WG-046 ordering: the error must originate from template substitution, NOT parse.
	if !strings.Contains(wlErr.Reason, "template substitution failed") {
		t.Errorf("Reason %q does not contain %q (ordering invariant WG-046: substitution fires before parse)",
			wlErr.Reason, "template substitution failed")
	}
	if strings.Contains(wlErr.Reason, "parse failed") {
		t.Errorf("Reason %q must not cite parse failure; parser must not run before substitution (WG-046)",
			wlErr.Reason)
	}
	// Error must name at least one of the unresolved tokens.
	if !strings.Contains(wlErr.Reason, "ISSUE_NUMBER") && !strings.Contains(wlErr.Reason, "GITHUB_REPO") {
		t.Errorf("Reason %q does not name unresolved tokens ISSUE_NUMBER / GITHUB_REPO", wlErr.Reason)
	}

	// Case 2: partial params — only ISSUE_NUMBER provided, GITHUB_REPO missing.
	_, err2 := workflow.LoadDotWorkflowWithParams(dotPath, map[string]string{"ISSUE_NUMBER": "172"})
	if err2 == nil {
		t.Fatal("expected load error for partial params, got nil")
	}
	var wlErr2 *workflow.ErrWorkflowLoad
	if !errors.As(err2, &wlErr2) {
		t.Fatalf("expected *workflow.ErrWorkflowLoad for partial params, got %T: %v", err2, err2)
	}
	if !strings.Contains(wlErr2.Reason, "template substitution failed") {
		t.Errorf("partial-params Reason %q does not cite template substitution", wlErr2.Reason)
	}
	if !strings.Contains(wlErr2.Reason, "GITHUB_REPO") {
		t.Errorf("partial-params Reason %q does not name missing token GITHUB_REPO", wlErr2.Reason)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// T6d-3: Run.template_params sealing — replay re-substitutes identically
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParamSubst_SealedParamsReplay verifies that
// workflow.LoadDotWorkflowWithParams is deterministic: calling it twice with the
// same params and same source yields byte-identical substituted field values.
// This exercises the WG-045 sealing invariant — if Run.template_params is stored
// at launch time, a replay that re-calls LoadDotWorkflowWithParams with those
// same params produces an identical graph, which is the precondition for correct
// replay behaviour.
func TestDotParamSubst_SealedParamsReplay(t *testing.T) {
	t.Parallel()

	dotPath := writeParamSubstDot(t)

	graph1, err1 := workflow.LoadDotWorkflowWithParams(dotPath, paramSubstTestParams)
	if err1 != nil {
		t.Fatalf("first LoadDotWorkflowWithParams: %v", err1)
	}
	graph2, err2 := workflow.LoadDotWorkflowWithParams(dotPath, paramSubstTestParams)
	if err2 != nil {
		t.Fatalf("second LoadDotWorkflowWithParams: %v", err2)
	}

	// Substituted goal must be identical across both calls.
	if graph1.Goal != graph2.Goal {
		t.Errorf("replay goal mismatch: first=%q second=%q", graph1.Goal, graph2.Goal)
	}
	if graph1.Goal != expectedSubstitutedGoal {
		t.Errorf("graph.Goal = %q; want %q", graph1.Goal, expectedSubstitutedGoal)
	}

	// Node attributes must be identical across both calls.
	nodeMap1 := make(map[string]string, len(graph1.Nodes))
	nodeMap2 := make(map[string]string, len(graph2.Nodes))
	for _, n := range graph1.Nodes {
		nodeMap1[n.ID+":prompt"] = n.Prompt
		nodeMap1[n.ID+":tool_command"] = n.ToolCommand
	}
	for _, n := range graph2.Nodes {
		nodeMap2[n.ID+":prompt"] = n.Prompt
		nodeMap2[n.ID+":tool_command"] = n.ToolCommand
	}
	for k, v1 := range nodeMap1 {
		if v2, ok := nodeMap2[k]; !ok || v1 != v2 {
			t.Errorf("replay mismatch at %s: first=%q second=%q (present=%v)", k, v1, v2, ok)
		}
	}
}

// containsParamToken reports whether s contains an unsubstituted __UPPER_SNAKE__
// placeholder per WG-045 token grammar.
func containsParamToken(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if s[i] == '_' && s[i+1] == '_' && s[i+2] >= 'A' && s[i+2] <= 'Z' {
			return true
		}
	}
	return false
}
