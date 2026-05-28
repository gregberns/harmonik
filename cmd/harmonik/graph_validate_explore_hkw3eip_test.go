package main

// graph_validate_explore_hkw3eip_test.go — exploratory tests for the
// operator-facing `harmonik graph validate <path>` CLI surface.
//
// Helper prefix: graphValidateFixture (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-w3eip).
//
// These tests exercise runGraphValidate directly (no os.Args mutation needed)
// against temporary DOT files on disk, verifying exit codes and output
// messages for: valid input, invalid input (bad node type), and missing file.
//
// Bead: hk-w3eip.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// graphValidateFixtureCaptureOutput redirects os.Stdout and os.Stderr to
// in-process pipes, calls fn, and returns the captured stdout and stderr.
func graphValidateFixtureCaptureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	// Capture stdout.
	origOut := os.Stdout
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("graphValidateFixtureCaptureOutput: os.Pipe (stdout): %v", err)
	}

	// Capture stderr.
	origErr := os.Stderr
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("graphValidateFixtureCaptureOutput: os.Pipe (stderr): %v", err)
	}

	os.Stdout = wOut
	os.Stderr = wErr
	t.Cleanup(func() {
		os.Stdout = origOut
		os.Stderr = origErr
	})

	fn()

	wOut.Close()
	wErr.Close()

	var bufOut, bufErr bytes.Buffer
	bufOut.ReadFrom(rOut)
	bufErr.ReadFrom(rErr)
	rOut.Close()
	rErr.Close()

	return bufOut.String(), bufErr.String()
}

// graphValidateFixtureWriteFile writes content to a temp file with the given
// name inside t.TempDir and returns the full path.
func graphValidateFixtureWriteFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("graphValidateFixtureWriteFile: %v", err)
	}
	return p
}

// validDOT is a minimal well-formed DOT workflow in the unified
// internal/workflow/dot dialect (the same dialect the daemon execution path
// accepts via workflow.LoadDotWorkflow). Bare graph-level attributes,
// start_node (not start_node_id), handler_ref required on every node per
// EM-007/WG-024, and idempotency_class on the non-agentic nodes per WG-008.
const validDOT = `digraph workflow {
    schema_version="1";
    version="0.1.0";
    workflow_id="018f1e2b-0040-7000-8000-000000000099";
    start_node="start";
    terminal_node_ids="end";

    start [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="entry"
    ];

    end [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="terminal"
    ];

    start -> end;
}
`

// invalidDOT_badNodeType has an unrecognised node type value ("banana"),
// which the unified validator rejects (WG-001 node-type enum).
const invalidDOT_badNodeType = `digraph workflow {
    schema_version="1";
    version="0.1.0";
    workflow_id="018f1e2b-0040-7000-8000-000000000100";
    start_node="start";
    terminal_node_ids="end";

    start [
        type="banana",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="entry"
    ];

    end [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="terminal"
    ];

    start -> end;
}
`

func TestGraphValidate_ValidFile_ExitZero(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "valid.dot", validDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for valid DOT, got %d; stdout=%q", exitCode, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "valid") {
		t.Errorf("expected stdout to contain 'valid', got %q", stdout)
	}
}

func TestGraphValidate_ValidFile_JSONMode(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "valid.dot", validDOT)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{"--json", path})
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for valid DOT (--json), got %d; stdout=%q", exitCode, stdout)
	}
	trimmed := strings.TrimSpace(stdout)
	if trimmed != "[]" {
		t.Errorf("expected empty JSON array '[]' for valid DOT, got %q", trimmed)
	}
}

func TestGraphValidate_InvalidFile_BadNodeType_ExitNonZero(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "bad-type.dot", invalidDOT_badNodeType)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{path})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit code for invalid DOT (bad node type), got 0")
	}
	// The output should contain diagnostic information about the bad type.
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "diagnostic") && !strings.Contains(lower, "banana") &&
		!strings.Contains(lower, "error") && !strings.Contains(lower, "invalid") {
		t.Errorf("expected diagnostic output mentioning the bad type; got stdout=%q", stdout)
	}
}

func TestGraphValidate_InvalidFile_BadNodeType_JSONMode(t *testing.T) {
	path := graphValidateFixtureWriteFile(t, "bad-type.dot", invalidDOT_badNodeType)

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{"--json", path})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit code for invalid DOT (--json), got 0")
	}
	// JSON mode should produce a JSON array with at least one diagnostic.
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "[") {
		t.Errorf("expected JSON array output, got %q", trimmed)
	}
	if trimmed == "[]" {
		t.Errorf("expected non-empty JSON diagnostic array for invalid DOT, got empty []")
	}
}

func TestGraphValidate_MissingFile_ExitNonZero(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "does-not-exist.dot")

	var exitCode int
	_, stderr := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{missingPath})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit code for missing file, got 0")
	}
	// Should mention the file and indicate it cannot be read.
	if !strings.Contains(stderr, "cannot read") && !strings.Contains(stderr, "no such file") {
		t.Errorf("expected helpful error about missing file; got stderr=%q", stderr)
	}
}

func TestGraphValidate_MissingPath_ExitTwo(t *testing.T) {
	var exitCode int
	_, stderr := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{})
	})

	if exitCode != 2 {
		t.Errorf("expected exit code 2 for missing <path> argument, got %d", exitCode)
	}
	if !strings.Contains(stderr, "missing") {
		t.Errorf("expected usage error about missing path; got stderr=%q", stderr)
	}
}

func TestGraphValidate_CanonicalFixture_ReviewLoopDot(t *testing.T) {
	// Validate the canonical review-loop.dot fixture that ships with the repo.
	//
	// hk-kxygy unified the CLI on internal/workflow/dot — the same parser the
	// daemon execution path uses — so the canonical fixture (bare key="value";
	// attrs, leading // comments, start_node, handler_ref on non-agentic nodes)
	// now validates cleanly. This test guards that unification: review-loop.dot
	// MUST report 0 diagnostics and exit 0.
	fixturePath := filepath.Join("testdata", "review-loop.dot")
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		// Fall back to the specs/examples path relative to repo root.
		// We're in cmd/harmonik/, so go up two levels.
		fixturePath = filepath.Join("..", "..", "specs", "examples", "review-loop.dot")
	}
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Skip("review-loop.dot fixture not found; skipping canonical fixture test")
	}

	var exitCode int
	stdout, _ := graphValidateFixtureCaptureOutput(t, func() {
		exitCode = runGraphValidate([]string{fixturePath})
	})

	if exitCode != 0 {
		t.Errorf("expected exit 0 (0 diagnostics) for canonical review-loop.dot after parser unification (hk-kxygy), got exit %d; stdout=%q", exitCode, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "valid") {
		t.Errorf("expected stdout to report the fixture as valid, got %q", stdout)
	}
}
