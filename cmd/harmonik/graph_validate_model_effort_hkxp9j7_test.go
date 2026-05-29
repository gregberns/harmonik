package main

// graph_validate_model_effort_hkxp9j7_test.go — CLI tests for per-node model=/effort=
// validation via `harmonik graph validate` (bead hk-xp9j7, WG-042 / WG-031).
//
// What this file proves at the CLI layer:
//
//  (a) A valid .dot with model=/effort= on an agentic node → exit 0, "valid".
//  (b) An out-of-enum effort value on an agentic node → exit 1, diagnostic
//      mentioning "effort" and the bad value.
//  (c) model= on a non-agentic node → exit 1, "reserved-out-of-position".
//  (d) effort= on a gate node → exit 1, "reserved-out-of-position".
//  (e) class= and model_stylesheet= are permissive → exit 0, "valid" (warnings
//      are not exposed as CLI exit-1 failures per loadDotWorkflow contract).
//  (f) The canonical specs/examples/per-node-model-effort.dot round-trips through
//      the CLI loader without errors.
//
// Bead ref: hk-xp9j7.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// DOT fixture constants
// ─────────────────────────────────────────────────────────────────────────────

// modelEffortValidDOT is a minimal well-formed DOT with valid model=/effort=
// on the agentic implementer node and model= only on the reviewer node.
const modelEffortValidDOT = `digraph test {
  schema_version="1";
  version="1.0";
  start_node="implement";
  terminal_node_ids="close,close-needs-attention";

  implement [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-implementer",
    idempotency_class="non-idempotent",
    model="claude-opus-4-8",
    effort="high"
  ];

  review [
    type="agentic",
    agent_type="reviewer",
    handler_ref="claude-reviewer",
    idempotency_class="idempotent",
    model="claude-haiku-4-5"
  ];

  close [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent"
  ];

  "close-needs-attention" [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent"
  ];

  implement -> review;
  review -> close [condition="outcome.preferred_label == 'APPROVE'"];
  review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
  review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
  review -> "close-needs-attention";
}`

// modelEffortOutOfEnumDOT has effort="ultra" — not in {low,medium,high,xhigh,max}.
const modelEffortOutOfEnumDOT = `digraph test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-code",
    idempotency_class="non-idempotent",
    effort="ultra"
  ];

  close [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent"
  ];

  work -> close;
}`

// modelOnNonAgenticDOT has model= on a non-agentic node — reserved-out-of-position.
const modelOnNonAgenticDOT = `digraph test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-code",
    idempotency_class="non-idempotent"
  ];

  close [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent",
    model="claude-opus-4-8"
  ];

  work -> close;
}`

// effortOnGateDOT has effort= on a gate node — reserved-out-of-position.
const effortOnGateDOT = `digraph test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-code",
    idempotency_class="non-idempotent"
  ];

  gate1 [
    type="gate",
    gate_ref="my-gate",
    handler_ref="gate-handler",
    idempotency_class="idempotent",
    effort="high"
  ];

  close [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent"
  ];

  work -> gate1;
  gate1 -> close;
}`

// classAndModelStylesheetDOT has class= and model_stylesheet= — permissive
// (warned, retained in UnknownAttrs, not dispatched on) per WG-043.
const classAndModelStylesheetDOT = `digraph test {
  schema_version="1";
  version="1.0";
  start_node="work";
  terminal_node_ids="close";

  work [
    type="agentic",
    agent_type="implementer",
    handler_ref="claude-code",
    idempotency_class="non-idempotent",
    class="hard",
    model_stylesheet="heavy"
  ];

  close [
    type="non-agentic",
    handler_ref="noop",
    idempotency_class="idempotent"
  ];

  work -> close;
}`

// ─────────────────────────────────────────────────────────────────────────────
// (a) Valid model/effort on agentic node — exit 0
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphValidateModelEffort_ValidAgenticNode_ExitZero(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "valid-model-effort.dot", modelEffortValidDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode != 0 {
		t.Errorf("expected exit 0 for valid model/effort on agentic node, got %d; stdout=%q", exitCode, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "valid") {
		t.Errorf("expected stdout to contain 'valid', got %q", stdout)
	}
}

func TestGraphValidateModelEffort_ValidAgenticNode_JSONMode(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "valid-model-effort.dot", modelEffortValidDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{"--json", path})
	})

	if exitCode != 0 {
		t.Errorf("expected exit 0 (--json) for valid model/effort, got %d; stdout=%q", exitCode, stdout)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("expected empty JSON array '[]' for valid DOT, got %q", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) Out-of-enum effort → strict error, exit 1
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphValidateModelEffort_OutOfEnumEffort_ExitOne(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "bad-effort.dot", modelEffortOutOfEnumDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit for out-of-enum effort, got 0")
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "effort") {
		t.Errorf("expected diagnostic to mention 'effort'; stdout=%q", stdout)
	}
	if !strings.Contains(stdout, "ultra") {
		t.Errorf("expected diagnostic to mention the bad value 'ultra'; stdout=%q", stdout)
	}
}

func TestGraphValidateModelEffort_OutOfEnumEffort_JSONMode(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "bad-effort.dot", modelEffortOutOfEnumDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{"--json", path})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit (--json) for out-of-enum effort, got 0")
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "[") || trimmed == "[]" {
		t.Errorf("expected non-empty JSON diagnostic array, got %q", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (c) model= on non-agentic node → reserved-out-of-position, exit 1
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphValidateModelEffort_ModelOnNonAgentic_ExitOne(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "model-on-non-agentic.dot", modelOnNonAgenticDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit for model= on non-agentic node, got 0")
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "model") {
		t.Errorf("expected diagnostic to mention 'model'; stdout=%q", stdout)
	}
	if !strings.Contains(lower, "reserved-out-of-position") {
		t.Errorf("expected diagnostic to mention 'reserved-out-of-position'; stdout=%q", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (d) effort= on gate node → reserved-out-of-position, exit 1
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphValidateModelEffort_EffortOnGate_ExitOne(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "effort-on-gate.dot", effortOnGateDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit for effort= on gate node, got 0")
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "effort") {
		t.Errorf("expected diagnostic to mention 'effort'; stdout=%q", stdout)
	}
	if !strings.Contains(lower, "reserved-out-of-position") {
		t.Errorf("expected diagnostic to mention 'reserved-out-of-position'; stdout=%q", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (e) class= / model_stylesheet= are permissive — exit 0 (warnings only)
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphValidateModelEffort_ClassAndModelStylesheet_Permissive_ExitZero(t *testing.T) {
	// class= and model_stylesheet= are NOT reserved per WG-043; they are
	// accepted permissively (warning emitted, retained, not dispatched on).
	// SeverityWarning diagnostics do NOT flip the exit code — exit 0 is correct.
	path := graphValidateFixtureWriteFile(t, "class-and-stylesheet.dot", classAndModelStylesheetDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode != 0 {
		t.Errorf("expected exit 0 for class=/model_stylesheet= (permissive attrs), got %d; stdout=%q", exitCode, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "valid") {
		t.Errorf("expected stdout to contain 'valid' for permissive-only attrs, got %q", stdout)
	}
}

func TestGraphValidateModelEffort_ClassAndModelStylesheet_JSONMode_EmptyArray(t *testing.T) {
	// --json mode: SeverityWarning diagnostics are dropped; no SeverityError
	// findings → empty JSON array [] and exit 0.
	path := graphValidateFixtureWriteFile(t, "class-and-stylesheet.dot", classAndModelStylesheetDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{"--json", path})
	})

	if exitCode != 0 {
		t.Errorf("expected exit 0 (--json) for permissive attrs, got %d", exitCode)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("expected '[]' for permissive-only attrs in JSON mode, got %q", stdout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (f) Canonical example file round-trips cleanly
// ─────────────────────────────────────────────────────────────────────────────

func TestGraphValidateModelEffort_CanonicalExampleFile_ExitZero(t *testing.T) {
	// Validate the canonical specs/examples/per-node-model-effort.dot fixture.
	// This guards that the example file introduced by hk-xp9j7 remains valid
	// through parser changes (mirrors the review-loop.dot test in hk-w3eip).
	fixturePath := filepath.Join("..", "..", "specs", "examples", "per-node-model-effort.dot")
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Skip("per-node-model-effort.dot fixture not found; skipping canonical fixture test")
	}

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{fixturePath})
	})

	if exitCode != 0 {
		t.Errorf("expected exit 0 for canonical per-node-model-effort.dot, got %d; stdout=%q", exitCode, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "valid") {
		t.Errorf("expected stdout to report the fixture as valid, got %q", stdout)
	}
}
