package main

// workflowmode_dot_hk4fvid_test.go — exploratory tests for the
// --workflow-mode=dot CLI surface (flag plumbing + error paths).
//
// Helper prefix: dotExploreFixture (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-4fvid).
//
// Tests cover:
//   - --workflow-mode=dot accepts --workflow-ref path
//   - --workflow-mode=dot without --workflow-ref yields helpful error (not panic)
//   - --workflow-ref without --workflow-mode=dot is rejected
//   - unknown --workflow-mode value yields usage error
//   - --workflow-mode=dot + valid review-loop.dot passes flag parsing
//     (reaches br-lookup phase, proving parse+load succeeded)
//
// All tests call runBeadSubcommand directly — parallel-safe (no
// flag.CommandLine or os.Args mutation).
//
// Bead ref: hk-4fvid.

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// dotExploreFixtureCaptureStderr redirects os.Stderr to a pipe, calls fn,
// returns everything written. Restores os.Stderr via t.Cleanup.
func dotExploreFixtureCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("dotExploreFixtureCaptureStderr: os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	fn()

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("dotExploreFixtureCaptureStderr: close write end: %v", closeErr)
	}
	var buf bytes.Buffer
	if _, readErr := buf.ReadFrom(r); readErr != nil {
		t.Fatalf("dotExploreFixtureCaptureStderr: read pipe: %v", readErr)
	}
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("dotExploreFixtureCaptureStderr: close read end: %v", closeErr)
	}
	return buf.String()
}

// ─── Test: --workflow-mode=dot requires --workflow-ref ────────────────────────

// TestWorkflowModeDot_MissingWorkflowRef verifies that --workflow-mode=dot
// without --workflow-ref returns exit code 1 with a helpful error message,
// not a panic.
//
// Acceptance: hk-4fvid — absence of --workflow-ref when mode=dot yields
// helpful error.
func TestWorkflowModeDot_MissingWorkflowRef(t *testing.T) {

	var exitCode int
	stderr := dotExploreFixtureCaptureStderr(t, func() {
		exitCode = runBeadSubcommand([]string{
			"--workflow-mode", "dot",
			"--beads", "hk-fake-bead",
		})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit, got 0")
	}
	if !strings.Contains(stderr, "--workflow-mode dot requires --workflow-ref") {
		t.Errorf("expected helpful error about missing --workflow-ref, got:\n%s", stderr)
	}
}

// ─── Test: unknown --workflow-mode rejected ───────────────────────────────────

// TestWorkflowMode_UnknownValueRejected verifies that an invalid
// --workflow-mode value (e.g. "banana") returns exit code 1 with a usage
// error listing the valid values.
//
// Acceptance: hk-4fvid — unknown --workflow-mode value rejected with usage
// error.
func TestWorkflowMode_UnknownValueRejected(t *testing.T) {

	var exitCode int
	stderr := dotExploreFixtureCaptureStderr(t, func() {
		exitCode = runBeadSubcommand([]string{
			"--workflow-mode", "banana",
			"--beads", "hk-fake-bead",
		})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit, got 0")
	}
	if !strings.Contains(stderr, "unknown --workflow-mode") {
		t.Errorf("expected 'unknown --workflow-mode' in stderr, got:\n%s", stderr)
	}
	// The error message should list valid values so the operator knows what to use.
	if !strings.Contains(stderr, "dot") {
		t.Errorf("expected valid values including 'dot' in stderr, got:\n%s", stderr)
	}
}

// ─── Test: --workflow-ref without --workflow-mode=dot rejected ────────────────

// TestWorkflowRef_WithoutDotModeRejected verifies that --workflow-ref
// is rejected when --workflow-mode is not "dot" (e.g. default/builtin mode).
//
// Acceptance: hk-4fvid — --workflow-ref requires --workflow-mode dot.
func TestWorkflowRef_WithoutDotModeRejected(t *testing.T) {

	var exitCode int
	stderr := dotExploreFixtureCaptureStderr(t, func() {
		exitCode = runBeadSubcommand([]string{
			"--workflow-ref", "/tmp/some-workflow.dot",
			"--beads", "hk-fake-bead",
		})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit, got 0")
	}
	if !strings.Contains(stderr, "--workflow-ref requires --workflow-mode dot") {
		t.Errorf("expected error about --workflow-ref requiring --workflow-mode dot, got:\n%s", stderr)
	}
}

// ─── Test: --workflow-ref with explicit non-dot mode rejected ─────────────────

// TestWorkflowRef_WithSingleModeRejected verifies that --workflow-ref
// combined with --workflow-mode=single is rejected.
func TestWorkflowRef_WithSingleModeRejected(t *testing.T) {

	var exitCode int
	stderr := dotExploreFixtureCaptureStderr(t, func() {
		exitCode = runBeadSubcommand([]string{
			"--workflow-mode", "single",
			"--workflow-ref", "/tmp/some-workflow.dot",
			"--beads", "hk-fake-bead",
		})
	})

	if exitCode == 0 {
		t.Errorf("expected non-zero exit, got 0")
	}
	if !strings.Contains(stderr, "--workflow-ref requires --workflow-mode dot") {
		t.Errorf("expected error about --workflow-ref requiring --workflow-mode dot, got:\n%s", stderr)
	}
}

// ─── Test: --workflow-mode=dot + valid --workflow-ref passes flag parse ───────

// TestWorkflowModeDot_ValidRefPassesFlagParse verifies that --workflow-mode=dot
// combined with a valid --workflow-ref path (pointing to the canonical
// review-loop.dot fixture) passes the flag-parsing and workflow-mode resolution
// phase. The test expects a non-zero exit from a later validation step (br
// lookup) — the key assertion is that we get past the workflow-mode switch
// without error, and the stderr does NOT contain any workflow-mode related
// error messages.
//
// Acceptance: hk-4fvid — --workflow-ref + valid review-loop.dot dispatches
// successfully through the parsing/loading phase.
func TestWorkflowModeDot_ValidRefPassesFlagParse(t *testing.T) {

	var exitCode int
	stderr := dotExploreFixtureCaptureStderr(t, func() {
		exitCode = runBeadSubcommand([]string{
			"--workflow-mode", "dot",
			"--workflow-ref", "../../specs/examples/review-loop.dot",
			"--beads", "hk-fake-bead",
		})
	})

	// We expect a non-zero exit because br validation or project-dir checks
	// will fail in a test environment. That is fine — we are testing that the
	// flag-parsing phase succeeded.
	if exitCode == 0 {
		t.Logf("unexpected exit 0 in test env (br may have resolved); stderr:\n%s", stderr)
	}

	// The key assertion: no workflow-mode-related error messages.
	for _, bad := range []string{
		"unknown --workflow-mode",
		"--workflow-mode dot requires --workflow-ref",
		"--workflow-ref requires --workflow-mode dot",
	} {
		if strings.Contains(stderr, bad) {
			t.Errorf("flag-parse phase emitted unexpected error %q; stderr:\n%s", bad, stderr)
		}
	}
}

// ─── Test: = form of flags also accepted ─────────────────────────────────────

// TestWorkflowModeDot_EqualsSyntax verifies that --workflow-mode=dot and
// --workflow-ref=<path> (equals-separated form) also work.
func TestWorkflowModeDot_EqualsSyntax(t *testing.T) {

	var exitCode int
	stderr := dotExploreFixtureCaptureStderr(t, func() {
		exitCode = runBeadSubcommand([]string{
			"--workflow-mode=dot",
			"--workflow-ref=../../specs/examples/review-loop.dot",
			"--beads", "hk-fake-bead",
		})
	})

	if exitCode == 0 {
		t.Logf("unexpected exit 0 in test env; stderr:\n%s", stderr)
	}

	// No workflow-mode-related errors — flag parsing passed.
	for _, bad := range []string{
		"unknown --workflow-mode",
		"--workflow-mode dot requires --workflow-ref",
		"--workflow-ref requires --workflow-mode dot",
	} {
		if strings.Contains(stderr, bad) {
			t.Errorf("equals-syntax flag-parse emitted unexpected error %q; stderr:\n%s", bad, stderr)
		}
	}
}

// ─── Test: help output documents --workflow-mode and --workflow-ref ───────────

// TestRunHelp_DocumentsWorkflowModeFlags verifies that `harmonik run --help`
// documents both --workflow-mode and --workflow-ref so operators can discover
// the DOT surface.
func TestRunHelp_DocumentsWorkflowModeFlags(t *testing.T) {

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	exitCode := runBeadSubcommand([]string{"--help"})

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()

	output := buf.String()

	if exitCode != 0 {
		t.Errorf("run --help exit code = %d, want 0", exitCode)
	}

	for _, want := range []string{
		"--workflow-mode",
		"--workflow-ref",
		"dot",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("run --help output missing %q:\n%s", want, output)
		}
	}
}
