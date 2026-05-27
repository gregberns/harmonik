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

// validDOT is a minimal well-formed DOT workflow that passes the
// workflowvalidator's parseDOT + Validate pipeline. Uses the graph [ ... ]
// block syntax expected by the workflowvalidator parser.
const validDOT = `digraph workflow {
    graph [
        workflow_id       = "018f1e2b-0040-7000-8000-000000000099"
        name              = "graph-validate-explore-fixture"
        version           = "0.1.0"
        start_node_id     = "start"
        terminal_node_ids = "end"
    ]

    start [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    end [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    start -> end [ordering_key = "a"]
}
`

// invalidDOT_badNodeType has an unrecognised node type value ("banana"),
// which should cause a validation diagnostic.
const invalidDOT_badNodeType = `digraph workflow {
    graph [
        workflow_id       = "018f1e2b-0040-7000-8000-000000000100"
        name              = "bad-type-fixture"
        version           = "0.1.0"
        start_node_id     = "start"
        terminal_node_ids = "end"
    ]

    start [
        type               = "banana"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    end [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    start -> end [ordering_key = "a"]
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
	// Known gap: review-loop.dot uses the internal/workflow/dot parser's syntax
	// (bare key="value"; attrs, leading // comments) which differs from the
	// workflowvalidator's expected syntax (graph [ ... ] block, no leading
	// comments before "digraph"). The workflowvalidator parser rejects
	// review-loop.dot as unparseable. This test documents that gap —
	// the CLI surface uses workflowvalidator, not the dot parser.
	//
	// When the two parsers are unified (or the CLI switches to internal/workflow/dot),
	// this test should be updated to expect exit 0.
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

	// Currently fails due to parser dialect mismatch (see comment above).
	// When unified, change this to assert exitCode == 0.
	if exitCode == 0 {
		t.Log("review-loop.dot now validates cleanly — parser dialect gap may be resolved")
	} else {
		// Expect em038_not_parseable due to leading comments / bare attrs.
		if !strings.Contains(stdout, "em038_not_parseable") {
			t.Errorf("expected em038_not_parseable diagnostic for review-loop.dot, got stdout=%q", stdout)
		}
	}
}
