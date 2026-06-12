package daemon_test

// dot_non_committing_hk69asi_test.go — unit tests: non-committing agentic node
// mode (SUCCESS without HEAD advance) per WG-041 §I.4 / EM-015d carve-out /
// EM-058 non-committing sub-note / HC §4.2a SUCCESS-without-commit note.
//
// # What this file proves
//
//  1. An implementer-class agentic node with NonCommitting=true that exits
//     cleanly without advancing HEAD returns SUCCESS and the cascade continues
//     (the terminal node is reached → success=true).
//
//  2. An implementer-class agentic node with NonCommitting=false (the default)
//     that exits cleanly without advancing HEAD is a node failure
//     (needsAttention=true).  This is the pre-existing behavior; the test is a
//     regression guard to ensure the default path is unchanged.
//
//  3. Parser: non_committing="true" on an agentic node parses to
//     node.NonCommitting=true.  non_committing="false" and the absent case both
//     yield NonCommitting=false.
//
//  4. Parser: auto_status is reserved-and-rejected with a strict error whose
//     message directs the author to non_committing (WG-041 §I.4).
//
//  5. Parser: non_committing= on a non-agentic node is retained with a v1
//     WARNING and does not raise a strict error (WG-031 permissive-retain).
//
// # Review-loop regression note
//
// dispatchDotAgenticNode is only reachable from driveDotWorkflow (DOT mode).
// runReviewLoop (review-loop mode) has its own independent implementer-commit
// check in reviewloop.go and does NOT call dispatchDotAgenticNode; the
// non_committing relaxation is therefore structurally isolated to DOT mode.
// The existing review-loop test suite (e.g. TestScenario_ReviewLoop_*) guards
// the review-loop commit requirement; no new review-loop test is added here.
//
// Bead ref: hk-69asi.

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

// nonCommittingFixtureProjectDir creates a minimal git repo for the tests.
func nonCommittingFixtureProjectDir(t *testing.T) string {
	t.Helper()
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
	return projectDir
}

// nonCommittingFixtureGraph builds a minimal DOT graph where the agentic node
// is BOTH the start node AND the sole terminal node.  This avoids needing an
// edge infrastructure: after the implementer outcome, DecideNextNode sees the
// current node is in TerminalNodeIDs and returns IsTerminal=true immediately.
//
// nonCommitting controls whether the node carries the non_committing flag.
func nonCommittingFixtureGraph(nonCommitting bool) *dot.Graph {
	return &dot.Graph{
		StartNodeID:     "implement",
		TerminalNodeIDs: []string{"implement"},
		Nodes: []*dot.Node{
			{
				ID:               "implement",
				Type:             core.NodeTypeAgentic,
				AgentType:        "implementer",
				HandlerRef:       "claude-implementer",
				IdempotencyClass: "non-idempotent",
				NonCommitting:    nonCommitting,
			},
		},
		UnknownAttrs: map[string]string{},
	}
}

// TestDotNonCommitting_CleanExitNoCommit_Success verifies that an
// implementer-class agentic node with NonCommitting=true that exits cleanly
// (exit 0) without advancing HEAD returns SUCCESS and the cascade reaches the
// terminal node (hk-69asi WG-041 §I.4).
func TestDotNonCommitting_CleanExitNoCommit_Success(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-69asi-non-committing-success-001")

	projectDir := nonCommittingFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	// Handler: exits 0 immediately without making a commit.
	graph := nonCommittingFixtureGraph(true /* NonCommitting */)

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

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"non-committing task",
		"bead body",
		wtPath, parentSHA,
		graph,
		"",
	)

	if !result.Success {
		t.Errorf("Success = false; want true for non_committing implementer clean exit (summary: %q)", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("NeedsAttention = true; want false for non_committing implementer clean exit (summary: %q)", result.Summary)
	}
	if result.TerminalNodeID != "implement" {
		t.Errorf("TerminalNodeID = %q; want %q", result.TerminalNodeID, "implement")
	}
}

// TestDotNonCommitting_DefaultCleanExitNoCommit_Failure verifies that an
// implementer-class agentic node with NonCommitting=false (the default) that
// exits cleanly without advancing HEAD is a node failure (needsAttention=true).
// This is the regression guard for the pre-existing behavior (hk-69asi).
func TestDotNonCommitting_DefaultCleanExitNoCommit_Failure(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-69asi-default-no-commit-failure-001")

	projectDir := nonCommittingFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

	// Handler: exits 0 immediately without making a commit.
	graph := nonCommittingFixtureGraph(false /* NonCommitting = default */)

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

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflowFull(
		ctx, deps, runID, beadID,
		"committing task",
		"bead body",
		wtPath, parentSHA,
		graph,
		"",
	)

	if result.Success {
		t.Errorf("Success = true; want false for default implementer clean exit without commit")
	}
	if !result.NeedsAttention {
		t.Errorf("NeedsAttention = false; want true for default implementer clean exit without commit")
	}
	// Verify the summary message mentions HEAD / commit to confirm the right failure path.
	if !strings.Contains(result.Summary, "HEAD") && !strings.Contains(result.Summary, "commit") {
		t.Errorf("Summary %q does not mention HEAD or commit; expected implementer-commit-required failure path", result.Summary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parser tests
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParser_NonCommittingTrue_Parsed verifies that non_committing="true" on
// an agentic node parses to node.NonCommitting=true (hk-69asi WG-041 §I.4).
func TestDotParser_NonCommittingTrue_Parsed(t *testing.T) {
	t.Parallel()

	src := `digraph {
  start_node = "n"
  terminal_node_ids = "n"
  n [type="agentic", agent_type="implementer", handler_ref="h",
     idempotency_class="non-idempotent", non_committing="true"]
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	if !g.Nodes[0].NonCommitting {
		t.Errorf("NonCommitting = false; want true when non_committing=\"true\"")
	}
	if len(g.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", g.Warnings)
	}
}

// TestDotParser_NonCommittingAbsent_DefaultFalse verifies that when
// non_committing is absent, node.NonCommitting defaults to false.
func TestDotParser_NonCommittingAbsent_DefaultFalse(t *testing.T) {
	t.Parallel()

	src := `digraph {
  start_node = "n"
  terminal_node_ids = "n"
  n [type="agentic", agent_type="implementer", handler_ref="h",
     idempotency_class="non-idempotent"]
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	if g.Nodes[0].NonCommitting {
		t.Errorf("NonCommitting = true; want false when non_committing is absent")
	}
}

// TestDotParser_AutoStatus_NonBooleanRejected verifies that a non-boolean
// auto_status value (e.g. a policy name) produces a strict ParseError.
// Boolean values ("true"/"false") are accepted per WG-041 §I.4; non-boolean
// policy values are reserved for a future step (hk-oo4).
func TestDotParser_AutoStatus_NonBooleanRejected(t *testing.T) {
	t.Parallel()

	src := `digraph {
  start_node = "n"
  terminal_node_ids = "n"
  n [type="agentic", agent_type="implementer", handler_ref="h",
     idempotency_class="non-idempotent", auto_status="some-policy"]
}`

	_, err := dot.Parse(src, "")
	if err == nil {
		t.Fatal("Parse: want error for non-boolean auto_status value, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "auto_status") {
		t.Errorf("error %q does not mention \"auto_status\"", msg)
	}
}

// TestDotParser_NonCommitting_WarnOnNonAgentic verifies that non_committing=
// on a non-agentic node is retained with a v1 WARNING and does NOT raise a
// strict error (WG-031 permissive-retain / WG-041 §I.4).
func TestDotParser_NonCommitting_WarnOnNonAgentic(t *testing.T) {
	t.Parallel()

	src := `digraph {
  start_node = "n"
  terminal_node_ids = "n"
  n [type="non-agentic", handler_ref="noop", idempotency_class="idempotent",
     non_committing="true"]
}`

	g, err := dot.Parse(src, "")
	if err != nil {
		t.Fatalf("Parse: unexpected strict error: %v", err)
	}
	if len(g.Warnings) == 0 {
		t.Fatal("Warnings is empty; want at least one warning for non_committing on non-agentic node")
	}
	found := false
	for _, w := range g.Warnings {
		if strings.Contains(w.Message, "non_committing") && strings.Contains(w.Message, "agentic-only") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no warning mentions non_committing + agentic-only; warnings: %v", g.Warnings)
	}
	// Value is retained in the AST.
	if len(g.Nodes) != 1 || !g.Nodes[0].NonCommitting {
		t.Errorf("NonCommitting not retained on non-agentic node; node=%v", g.Nodes)
	}
}
