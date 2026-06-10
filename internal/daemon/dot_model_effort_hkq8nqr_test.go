package daemon_test

// dot_model_effort_hkq8nqr_test.go — unit tests: per-node model= / effort=
// override on agentic nodes (hk-q8nqr WG-042 §I.5 / EM-012b-NODE).
//
// # What this file proves
//
// 1. An agentic node with Model="claude-opus-4-8" and Effort="high" causes
//    dispatchDotAgenticNode to pass that pair to the launchSpecBuilder,
//    overriding the run-level defaults (threading test).
//
// 2. A sibling agentic node without Model/Effort inherits the run-level
//    resolved model/effort (no spurious override).
//
// 3. (Parser) out-of-enum effort → ingest-time STRICT error.
//
// 4. (Parser) model on a non-agentic node → reserved-out-of-position STRICT error.
//
// 5. (Parser) class= and model_stylesheet= are NOT reserved; they parse
//    permissively (warned, retained in UnknownAttrs, never dispatched).
//
// Bead ref: hk-q8nqr.

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

// ─────────────────────────────────────────────────────────────────────────────
// Threading tests — per-node model/effort override
// ─────────────────────────────────────────────────────────────────────────────

// TestDotNodeModelEffortOverridesRunLevel verifies that a node with
// Model="claude-opus-4-8" and Effort="high" causes dispatchDotAgenticNode
// to pass that pair to the launchSpecBuilder, overriding run-level defaults.
func TestDotNodeModelEffortOverridesRunLevel(t *testing.T) {
	t.Parallel()

	const (
		wantModel  = "claude-opus-4-8"
		wantEffort = "high"
		runModel   = "claude-haiku-4-5"
		runEffort  = "low"
		beadID     = core.BeadID("hk-q8nqr-override-test-001")
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
				Model:            wantModel,
				Effort:           wantEffort,
			},
		},
		UnknownAttrs: map[string]string{},
	}

	captured := make(chan daemon.ModelEffortPair, 1)
	lsb := daemon.ExportedCaptureModelEffortBuilder(captured)

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

	result := daemon.ExportedDriveDotWorkflowWithModelEffort(
		ctx, deps, runID, beadID,
		"implement task", "bead body",
		wtPath, parentSHA,
		graph,
		runModel, runEffort,
	)

	// The spec builder short-circuits, so NeedsAttention is expected.
	if !result.NeedsAttention {
		t.Errorf("expected NeedsAttention=true (spec builder short-circuited), got false; summary=%q", result.Summary)
	}

	var pair daemon.ModelEffortPair
	select {
	case pair = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called — model/effort not captured")
	}

	if pair.Model != wantModel {
		t.Errorf("model = %q; want %q (per-node override)", pair.Model, wantModel)
	}
	if pair.Effort != wantEffort {
		t.Errorf("effort = %q; want %q (per-node override)", pair.Effort, wantEffort)
	}
}

// TestDotNodeModelEffortRunLevelDefaultWhenAbsent verifies that a node without
// Model/Effort fields inherits the run-level resolved model and effort.
func TestDotNodeModelEffortRunLevelDefaultWhenAbsent(t *testing.T) {
	t.Parallel()

	const (
		runModel  = "claude-sonnet-4-6"
		runEffort = "medium"
		beadID    = core.BeadID("hk-q8nqr-default-test-001")
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
				// Model and Effort deliberately absent — inherits run-level.
			},
		},
		UnknownAttrs: map[string]string{},
	}

	captured := make(chan daemon.ModelEffortPair, 1)
	lsb := daemon.ExportedCaptureModelEffortBuilder(captured)

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

	daemon.ExportedDriveDotWorkflowWithModelEffort(
		ctx, deps, runID, beadID,
		"implement task", "bead body",
		wtPath, parentSHA,
		graph,
		runModel, runEffort,
	)

	var pair daemon.ModelEffortPair
	select {
	case pair = <-captured:
	default:
		t.Fatal("launchSpecBuilder was never called")
	}

	if pair.Model != runModel {
		t.Errorf("model = %q; want run-level default %q", pair.Model, runModel)
	}
	if pair.Effort != runEffort {
		t.Errorf("effort = %q; want run-level default %q", pair.Effort, runEffort)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parser tests — ingest-time validation
// ─────────────────────────────────────────────────────────────────────────────

// TestDotParserOutOfEnumEffortIsStrictError verifies that an out-of-enum
// effort value on an agentic node is a STRICT parse error at ingest time.
func TestDotParserOutOfEnumEffortIsStrictError(t *testing.T) {
	t.Parallel()

	src := `digraph test {
  schema_version="1";
  start_node="work";
  terminal_node_ids="close";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent",
        effort="ultra"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  work -> close;
}`

	_, err := dot.Parse(src, "test.dot")
	if err == nil {
		t.Fatal("expected parse error for out-of-enum effort, got nil")
	}
	if !strings.Contains(err.Error(), "effort") {
		t.Errorf("error does not mention \"effort\": %v", err)
	}
	if !strings.Contains(err.Error(), "ultra") {
		t.Errorf("error does not mention the bad value \"ultra\": %v", err)
	}
}

// TestDotParserModelOnNonAgenticIsStrictError verifies that model= on a
// non-agentic node is a reserved-out-of-position STRICT error.
func TestDotParserModelOnNonAgenticIsStrictError(t *testing.T) {
	t.Parallel()

	src := `digraph test {
  schema_version="1";
  start_node="start";
  terminal_node_ids="close";

  start [type="agentic", agent_type="implementer",
         handler_ref="claude-code", idempotency_class="non-idempotent"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent",
         model="claude-opus-4-8"];

  start -> close;
}`

	_, err := dot.Parse(src, "test.dot")
	if err == nil {
		t.Fatal("expected parse error for model= on non-agentic node, got nil")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("error does not mention \"model\": %v", err)
	}
	if !strings.Contains(err.Error(), "reserved-out-of-position") {
		t.Errorf("error does not mention \"reserved-out-of-position\": %v", err)
	}
}

// TestDotParserEffortOnGateIsStrictError verifies that effort= on a gate node
// is a reserved-out-of-position STRICT error.
func TestDotParserEffortOnGateIsStrictError(t *testing.T) {
	t.Parallel()

	src := `digraph test {
  schema_version="1";
  start_node="start";
  terminal_node_ids="close";

  start [type="agentic", agent_type="implementer",
         handler_ref="claude-code", idempotency_class="non-idempotent"];
  gate1 [type="gate", gate_ref="my-gate", handler_ref="gate-handler",
         idempotency_class="idempotent", effort="high"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  start -> gate1;
  gate1 -> close;
}`

	_, err := dot.Parse(src, "test.dot")
	if err == nil {
		t.Fatal("expected parse error for effort= on gate node, got nil")
	}
	if !strings.Contains(err.Error(), "effort") {
		t.Errorf("error does not mention \"effort\": %v", err)
	}
	if !strings.Contains(err.Error(), "reserved-out-of-position") {
		t.Errorf("error does not mention \"reserved-out-of-position\": %v", err)
	}
}

// TestDotParserClassAndModelStylesheetPermissive verifies that class= and
// model_stylesheet= are NOT reserved — they parse permissively (warned,
// retained in UnknownAttrs, never dispatched on).
func TestDotParserClassAndModelStylesheetPermissive(t *testing.T) {
	t.Parallel()

	src := `digraph test {
  schema_version="1";
  start_node="work";
  terminal_node_ids="close";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent",
        class="hard", model_stylesheet="heavy"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  work -> close;
}`

	g, err := dot.Parse(src, "test.dot")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	// Warnings should mention both unknown permissive attributes.
	var warnMsgs []string
	for _, w := range g.Warnings {
		warnMsgs = append(warnMsgs, w.Message)
	}
	joined := strings.Join(warnMsgs, "; ")

	if !strings.Contains(joined, "class") {
		t.Errorf("no warning for \"class\" in warnings: %v", warnMsgs)
	}
	if !strings.Contains(joined, "model_stylesheet") {
		t.Errorf("no warning for \"model_stylesheet\" in warnings: %v", warnMsgs)
	}

	// Both must be retained in UnknownAttrs.
	workNode := g.Nodes[0]
	if workNode.UnknownAttrs["class"] != "hard" {
		t.Errorf("UnknownAttrs[\"class\"] = %q; want \"hard\"", workNode.UnknownAttrs["class"])
	}
	if workNode.UnknownAttrs["model_stylesheet"] != "heavy" {
		t.Errorf("UnknownAttrs[\"model_stylesheet\"] = %q; want \"heavy\"", workNode.UnknownAttrs["model_stylesheet"])
	}

	// The DOT node's Model and Effort fields must be empty (not dispatched on).
	if workNode.Model != "" {
		t.Errorf("Node.Model = %q; want empty (class/model_stylesheet must not populate Model)", workNode.Model)
	}
}

// TestDotParserModelAndEffortOnAgenticParsed verifies that valid model= and
// effort= on an agentic node are parsed into Node.Model / Node.Effort.
func TestDotParserModelAndEffortOnAgenticParsed(t *testing.T) {
	t.Parallel()

	src := `digraph test {
  schema_version="1";
  start_node="work";
  terminal_node_ids="close";

  work [type="agentic", agent_type="implementer",
        handler_ref="claude-code", idempotency_class="non-idempotent",
        model="claude-opus-4-8", effort="high"];
  close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

  work -> close;
}`

	g, err := dot.Parse(src, "test.dot")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	workNode := g.Nodes[0]
	if workNode.Model != "claude-opus-4-8" {
		t.Errorf("Node.Model = %q; want \"claude-opus-4-8\"", workNode.Model)
	}
	if workNode.Effort != "high" {
		t.Errorf("Node.Effort = %q; want \"high\"", workNode.Effort)
	}
}
