package daemon_test

// dot_role_surfacing_hkm5lmo_test.go — unit test: node role= is surfaced into
// the agent brief as a "Role: <value>" prefix in the extraContext channel.
//
// # Why this file exists
//
// The marquee live smoke (hk-3wbff, dual-review-consolidate) exposed that
// per-axis reviewer nodes ran IDENTICAL generic review behaviour despite each
// having a distinct role= attribute ("correctness + tests" vs "design &
// idioms"). The root cause: role= was parsed into Node.UnknownAttrs (now
// Node.Role) but was never read when building the agent brief in
// dispatchDotAgenticNode (dot_cascade.go).
//
// The fix (hk-m5lmo) reads Node.Role and prepends "Role: <value>" to the
// extraContext that flows into AgentTaskPayload.ExtraContext, which renders as
// the "## Extra Context" section of agent-task.md.
//
// # What this test proves
//
// A dot.Graph node with Role = "design&idioms" causes dispatchDotAgenticNode
// to pass an extraContext string prefixed with "Role: design&idioms" to the
// launchSpecBuilder. The stub builder captures the first call and short-circuits
// dispatch; the test asserts on the captured value.
//
// Bead ref: hk-m5lmo.

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// TestDotNodeRoleSurfacedIntoExtraContext verifies that a node's Role field
// is prepended to extraContext as "Role: <value>" before the agent brief is
// built (hk-m5lmo).
func TestDotNodeRoleSurfacedIntoExtraContext(t *testing.T) {
	t.Parallel()

	const (
		wantRole       = "design&idioms"
		wantRolePrefix = "Role: design&idioms"
		beadID         = core.BeadID("hk-m5lmo-role-test-001")
		additionalCtx  = "predecessor: hk-jyqxe"
	)

	// Stand up a minimal git repo (required for the production worktree factory
	// used in the dispatcher). Mirrors implReadyFixtureProjectDir.
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

	// A detached worktree for the cascade to dispatch into.
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	// A graph with a single agentic node that has Role = "design&idioms".
	// The cascade starts at this node and tries to dispatch it; the stub builder
	// intercepts before a subprocess is spawned.
	graph := &dot.Graph{
		StartNodeID:     "review_design",
		TerminalNodeIDs: []string{"close"},
		Nodes: []*dot.Node{
			{
				ID:               "review_design",
				Type:             core.NodeTypeAgentic,
				AgentType:        "reviewer",
				HandlerRef:       "claude-reviewer",
				IdempotencyClass: "idempotent",
				Role:             wantRole,
			},
		},
		UnknownAttrs: map[string]string{},
	}

	// Channel receives the first extraContext that reaches the spec builder.
	captured := make(chan string, 1)
	lsb := daemon.ExportedCaptureExtraContextBuilder(captured)

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

	// ExtraContext from the operator (simulates a goal line or queue item context).
	// The role must be PREPENDED so it appears first.
	result := daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"review task",
		"check the diff for design issues",
		wtPath, parentSHA,
		graph,
		additionalCtx,
	)

	// The spec builder returned an error → the cascade returns needsAttention.
	// That is expected and correct: we care about the captured extraContext.
	if !result.NeedsAttention {
		t.Errorf("expected NeedsAttention=true (spec builder short-circuited), got false; summary=%q", result.Summary)
	}

	// The captured extraContext must start with the role prefix.
	var capturedCtx string
	select {
	case capturedCtx = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called — extraContext not captured")
	}

	if !strings.HasPrefix(capturedCtx, wantRolePrefix) {
		t.Errorf("extraContext = %q; want prefix %q", capturedCtx, wantRolePrefix)
	}
	if !strings.Contains(capturedCtx, additionalCtx) {
		t.Errorf("extraContext = %q; want to contain base context %q", capturedCtx, additionalCtx)
	}
}

// TestDotNodeNoRoleExtraContextUnchanged verifies that when Role is empty the
// extraContext is passed through unchanged (no spurious "Role: " prefix).
func TestDotNodeNoRoleExtraContextUnchanged(t *testing.T) {
	t.Parallel()

	const (
		beadID  = core.BeadID("hk-m5lmo-norole-test-001")
		baseCtx = "predecessor: hk-jyqxe"
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
				// Role deliberately empty.
			},
		},
		UnknownAttrs: map[string]string{},
	}

	captured := make(chan string, 1)
	lsb := daemon.ExportedCaptureExtraContextBuilder(captured)

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
		"implement task", "write the code",
		wtPath, parentSHA,
		graph,
		baseCtx,
	)

	var capturedCtx string
	select {
	case capturedCtx = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called — extraContext not captured")
	}

	if strings.HasPrefix(capturedCtx, "Role:") {
		t.Errorf("extraContext = %q; unexpected \"Role:\" prefix when node has no role", capturedCtx)
	}
	if capturedCtx != baseCtx {
		t.Errorf("extraContext = %q; want %q (base context unchanged when no role)", capturedCtx, baseCtx)
	}
}
