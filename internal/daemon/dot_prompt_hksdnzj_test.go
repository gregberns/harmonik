package daemon_test

// dot_prompt_hksdnzj_test.go — unit tests: inline per-node prompt= attr on
// agentic nodes (hk-sdnzj).
//
// # What this file proves
//
// 1. An agentic implementer node with Prompt = "X" causes dispatchDotAgenticNode
//    to pass nodePrompt = "X" to the launchSpecBuilder (threading test).
//
// 2. An agentic reviewer node with Prompt = "Y" also passes nodePrompt = "Y" to
//    the launchSpecBuilder — the value is retained on the claudeRunCtx even for
//    reviewers; the spec builder (buildClaudeLaunchSpec) is responsible for
//    treating it as inert for that phase.
//
// 3. When prompt= is absent, nodePrompt is empty (no spurious injection).
//
// 4. buildClaudeLaunchSpec: when nodePrompt is non-empty and phase is
//    implementer-initial, the agent-task.md Body is the prompt (not the bead
//    description). Tested via ExportedBuildClaudeLaunchSpec + workspace fixture.
//
// 5. buildClaudeLaunchSpec: when nodePrompt is non-empty and phase is reviewer,
//    the agent-task.md Body remains the bead description (prompt is inert).
//
// Bead ref: hk-sdnzj.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ─────────────────────────────────────────────────────────────────────────────
// Threading tests (capture nodePrompt via spec-builder stub)
// ─────────────────────────────────────────────────────────────────────────────

// TestDotNodePromptThreadedIntoRunCtxForImplementer verifies that an
// implementer agentic node with Prompt = "X" causes dispatchDotAgenticNode to
// pass nodePrompt = "X" to the launchSpecBuilder (hk-sdnzj).
func TestDotNodePromptThreadedIntoRunCtxForImplementer(t *testing.T) {
	t.Parallel()

	const (
		wantPrompt = "implement the feature described here"
		beadID     = core.BeadID("hk-sdnzj-prompt-impl-test-001")
	)

	projectDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("commit", "--allow-empty", "-m", "Initial commit")

	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	graph := &dot.Graph{
		StartNodeID:     "implement",
		TerminalNodeIDs: []string{"close"},
		Nodes: []*dot.Node{
			{
				ID:               "implement",
				Type:             core.NodeTypeAgentic,
				AgentType:        "implementer",
				HandlerRef:       "claude-implementer",
				IdempotencyClass: "non-idempotent",
				Prompt:           wantPrompt,
			},
		},
		UnknownAttrs: map[string]string{},
	}

	captured := make(chan string, 1)
	lsb := daemon.ExportedCaptureNodePromptBuilder(captured)

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
		LaunchSpecBuilder:   lsb,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"implement task",
		"bead body that should be overridden",
		wtPath, parentSHA,
		graph,
		"",
	)

	if !result.NeedsAttention {
		t.Errorf("expected NeedsAttention=true (spec builder short-circuited), got false; summary=%q", result.Summary)
	}

	var capturedPrompt string
	select {
	case capturedPrompt = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called — nodePrompt not captured")
	}

	if capturedPrompt != wantPrompt {
		t.Errorf("nodePrompt = %q; want %q", capturedPrompt, wantPrompt)
	}
}

// TestDotNodePromptRetainedOnRunCtxForReviewer verifies that a reviewer agentic
// node with Prompt = "Y" passes nodePrompt = "Y" to the launchSpecBuilder —
// the value is retained on the claudeRunCtx even for reviewer nodes. The spec
// builder (buildClaudeLaunchSpec) treats it as inert for that phase (hk-sdnzj).
func TestDotNodePromptRetainedOnRunCtxForReviewer(t *testing.T) {
	t.Parallel()

	const (
		wantPrompt = "reviewer-scoped prompt (accepted-but-inert at v1)"
		beadID     = core.BeadID("hk-sdnzj-prompt-reviewer-test-001")
	)

	projectDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("commit", "--allow-empty", "-m", "Initial commit")

	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	graph := &dot.Graph{
		StartNodeID:     "review",
		TerminalNodeIDs: []string{"close"},
		Nodes: []*dot.Node{
			{
				ID:               "review",
				Type:             core.NodeTypeAgentic,
				AgentType:        "reviewer",
				HandlerRef:       "claude-reviewer",
				IdempotencyClass: "idempotent",
				Prompt:           wantPrompt,
			},
		},
		UnknownAttrs: map[string]string{},
	}

	captured := make(chan string, 1)
	lsb := daemon.ExportedCaptureNodePromptBuilder(captured)

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
		LaunchSpecBuilder:   lsb,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"review task",
		"bead body for review",
		wtPath, parentSHA,
		graph,
		"",
	)

	var capturedPrompt string
	select {
	case capturedPrompt = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called — nodePrompt not captured")
	}

	// nodePrompt is retained on the claudeRunCtx even for reviewer nodes.
	if capturedPrompt != wantPrompt {
		t.Errorf("nodePrompt = %q; want %q (retained even for reviewer)", capturedPrompt, wantPrompt)
	}
}

// TestDotNodeNoPromptNodePromptEmpty verifies that when prompt= is absent,
// nodePrompt is empty (no spurious injection).
func TestDotNodeNoPromptNodePromptEmpty(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-sdnzj-noprompt-test-001")

	projectDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("commit", "--allow-empty", "-m", "Initial commit")

	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	graph := &dot.Graph{
		StartNodeID:     "implement",
		TerminalNodeIDs: []string{"close"},
		Nodes: []*dot.Node{
			{
				ID:               "implement",
				Type:             core.NodeTypeAgentic,
				AgentType:        "implementer",
				HandlerRef:       "claude-implementer",
				IdempotencyClass: "non-idempotent",
				// Prompt deliberately absent.
			},
		},
		UnknownAttrs: map[string]string{},
	}

	captured := make(chan string, 1)
	lsb := daemon.ExportedCaptureNodePromptBuilder(captured)

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
		LaunchSpecBuilder:   lsb,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"implement task", "bead body",
		wtPath, parentSHA,
		graph,
		"",
	)

	var capturedPrompt string
	select {
	case capturedPrompt = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called — nodePrompt not captured")
	}

	if capturedPrompt != "" {
		t.Errorf("nodePrompt = %q; want empty when prompt= is absent", capturedPrompt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildClaudeLaunchSpec override tests (workspace fixture)
// ─────────────────────────────────────────────────────────────────────────────

// promptFixtureWorkspace creates a minimal workspace for buildClaudeLaunchSpec.
func promptFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("promptFixtureWorkspace: MkdirAll .claude/: %v", err)
	}
	return dir
}

// promptFixtureRunCtx builds an ExportedClaudeRunCtx for the given phase and
// optional nodePrompt.
func promptFixtureRunCtx(
	t *testing.T,
	workspacePath string,
	phase handlercontract.ReviewLoopPhase,
	beadDescription string,
	nodePrompt string,
) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("promptFixtureRunCtx: NewV7 runID: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:           core.RunID(runUID),
		BeadID:          "hk-sdnzj-build-test",
		WorkspacePath:   workspacePath,
		DaemonSocket:    "/tmp/harmonik-test-sdnzj.sock",
		WorkflowMode:    core.WorkflowModeDot,
		Phase:           phase,
		IterationCount:  1,
		HandlerBinary:   "claude",
		BaseEnv:         []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
		BeadDescription: beadDescription,
		NodePrompt:      nodePrompt,
	}
}

// TestBuildClaudeLaunchSpecPromptOverridesBodyForImplementer verifies that when
// nodePrompt is non-empty and phase is implementer-initial, the agent-task.md
// Body is the prompt value (not the bead description) per WG-040 §I.3 /
// HC-006a §III.3 (hk-sdnzj).
func TestBuildClaudeLaunchSpecPromptOverridesBodyForImplementer(t *testing.T) {
	t.Parallel()

	const (
		beadBody   = "original bead body — should be overridden"
		nodePrompt = "inline prompt from DOT node"
	)

	wsPath := promptFixtureWorkspace(t)
	rc := promptFixtureRunCtx(t, wsPath, handlercontract.ReviewLoopPhaseImplementerInitial, beadBody, nodePrompt)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("ExportedBuildClaudeLaunchSpec: %v", err)
	}

	// Read the agent-task.md written by WriteAgentTask.
	taskFile := filepath.Join(wsPath, ".harmonik", "agent-task.md")
	raw, readErr := os.ReadFile(taskFile)
	if readErr != nil {
		t.Fatalf("read agent-task.md: %v", readErr)
	}
	content := string(raw)

	// Body channel must contain the prompt, not the bead body.
	if !strings.Contains(content, nodePrompt) {
		t.Errorf("agent-task.md does not contain nodePrompt %q\ncontent:\n%s", nodePrompt, content)
	}
	if strings.Contains(content, beadBody) {
		t.Errorf("agent-task.md contains bead body %q; want it replaced by nodePrompt\ncontent:\n%s", beadBody, content)
	}
	// Bead ID must remain for traceability (HC-006a).
	if !strings.Contains(content, rc.BeadID) {
		t.Errorf("agent-task.md does not contain bead ID %q for traceability\ncontent:\n%s", rc.BeadID, content)
	}
}

// TestBuildClaudeLaunchSpecPromptInertForReviewer verifies that when
// nodePrompt is non-empty and phase is reviewer, the agent-task.md Body is
// the bead description (prompt is accepted-but-inert per EM-015d-RIA)
// (hk-sdnzj).
func TestBuildClaudeLaunchSpecPromptInertForReviewer(t *testing.T) {
	t.Parallel()

	const (
		beadBody   = "bead body for reviewer — must not be replaced"
		nodePrompt = "reviewer prompt (inert at v1)"
	)

	wsPath := promptFixtureWorkspace(t)
	rc := promptFixtureRunCtx(t, wsPath, handlercontract.ReviewLoopPhaseReviewer, beadBody, nodePrompt)

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("ExportedBuildClaudeLaunchSpec: %v", err)
	}

	taskFile := filepath.Join(wsPath, ".harmonik", "agent-task.md")
	raw, readErr := os.ReadFile(taskFile)
	if readErr != nil {
		t.Fatalf("read agent-task.md: %v", readErr)
	}
	content := string(raw)

	// Body channel must contain the bead body, not the reviewer prompt.
	if !strings.Contains(content, beadBody) {
		t.Errorf("agent-task.md does not contain bead body %q; reviewer prompt must not override\ncontent:\n%s", beadBody, content)
	}
}
