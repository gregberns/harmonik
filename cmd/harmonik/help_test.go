package main

// help_test.go — substring-assertion tests for `harmonik --help` and all
// subcommand --help flags (hk-u0oo2).
//
// Helper prefix: helpFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-u0oo2).
//
// All tests are parallel-safe except TestHarmonikTopLevelHelp and
// TestMaxConcurrentDefaultNotDuplicated, which redirect os.Stderr and/or
// mutate os.Args + flag.CommandLine (see package comment in main_test.go).
//
// Bead: hk-u0oo2.

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

// helpFixtureCaptureStderr redirects os.Stderr to an in-process pipe, calls
// fn, then returns everything written to the redirected stderr. The original
// os.Stderr is restored via t.Cleanup regardless of panics.
func helpFixtureCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("helpFixtureCaptureStderr: os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	fn()

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("helpFixtureCaptureStderr: close write end: %v", closeErr)
	}
	var buf bytes.Buffer
	if _, readErr := buf.ReadFrom(r); readErr != nil {
		t.Fatalf("helpFixtureCaptureStderr: read pipe: %v", readErr)
	}
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("helpFixtureCaptureStderr: close read end: %v", closeErr)
	}
	return buf.String()
}

// helpFixtureCaptureStdout redirects os.Stdout to an in-process pipe, calls
// fn, then returns everything written to the redirected stdout.
func helpFixtureCaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("helpFixtureCaptureStdout: os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	fn()

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("helpFixtureCaptureStdout: close write end: %v", closeErr)
	}
	var buf bytes.Buffer
	if _, readErr := buf.ReadFrom(r); readErr != nil {
		t.Fatalf("helpFixtureCaptureStdout: read pipe: %v", readErr)
	}
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("helpFixtureCaptureStdout: close read end: %v", closeErr)
	}
	return buf.String()
}

// TestHarmonikTopLevelHelp verifies that `harmonik --help` exits 0 and the
// output lists all subcommands and key daemon flags.
//
// Not parallel: mutates os.Args and flag.CommandLine (see main_test.go package
// comment).
//
// Acceptance: hk-u0oo2 — top-level help lists all subcommands.
func TestHarmonikTopLevelHelp(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "--help"})

	var exitCode int
	output := helpFixtureCaptureStderr(t, func() {
		exitCode = run()
	})

	if exitCode != 0 {
		t.Errorf("run() with --help: got exit code %d, want 0", exitCode)
	}

	for _, want := range []string{
		"run",
		"handler",
		"queue",
		"subscribe",
		"comms",
		"crew",
		"reconcile",
		"tmux-start",
		"hook-relay",
		"--project",
		"--max-concurrent",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("top-level help output missing %q:\n%s", want, output)
		}
	}
}

// TestQueueHelpFlag verifies that `harmonik queue --help` exits 0 and the
// VERBS list includes set-concurrency (the live concurrent-dispatch ceiling
// verb), which is a real verb in the queue switch but was historically omitted
// from the printed help block.
//
// Not parallel: drives run() which mutates os.Args and flag.CommandLine.
//
// Acceptance: hk-nh2 — queue --help surfaces set-concurrency.
func TestQueueHelpFlag(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "queue", "--help"})

	var exitCode int
	output := helpFixtureCaptureStdout(t, func() {
		exitCode = run()
	})

	if exitCode != 0 {
		t.Errorf("run() with queue --help: got exit code %d, want 0", exitCode)
	}

	for _, want := range []string{
		"submit",
		"append",
		"set-concurrency",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("queue --help output missing %q:\n%s", want, output)
		}
	}
}

// TestRunHelpFlag verifies that `harmonik run --help` exits 0 and the output
// contains the expected flags, exit-code section, and at least one example.
//
// Parallel: safe — runBeadSubcommand does not touch flag.CommandLine.
//
// Acceptance: hk-u0oo2 — run --help lists flags, exit codes, and examples.
func TestRunHelpFlag(t *testing.T) {
	t.Parallel()

	var exitCode int
	output := helpFixtureCaptureStdout(t, func() {
		exitCode = runBeadSubcommand([]string{"--help"})
	})

	if exitCode != 0 {
		t.Errorf("runBeadSubcommand([--help]): got exit code %d, want 0", exitCode)
	}

	for _, want := range []string{
		"--beads",
		"--max-concurrent",
		"--context",
		"--review-loop",
		"--project",
		"EXIT CODES",
		"harmonik run", // at least one example line
	} {
		if !strings.Contains(output, want) {
			t.Errorf("run --help output missing %q:\n%s", want, output)
		}
	}
}

// TestHandlerHelpFlag verifies that `harmonik handler --help` exits 0 and the
// output lists the "status" and "resume" verbs and their flag sets.
//
// Parallel: safe — runHandlerSubcommandIO accepts explicit writers.
//
// Acceptance: hk-u0oo2 — handler --help lists verbs and per-verb flags.
func TestHandlerHelpFlag(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	exitCode := runHandlerSubcommandIO([]string{"--help"}, &out, &errOut)

	if exitCode != 0 {
		t.Errorf("runHandlerSubcommandIO([--help]): got exit code %d, want 0; stderr: %s",
			exitCode, errOut.String())
	}

	output := out.String()
	for _, want := range []string{
		"status",
		"resume",
		"--type",
		"--project",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("handler --help output missing %q:\n%s", want, output)
		}
	}
}

// TestHandlerStatusHelpFlag verifies per-verb help for `harmonik handler status --help`.
//
// Parallel: safe.
func TestHandlerStatusHelpFlag(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	exitCode := runHandlerSubcommandIO([]string{"status", "--help"}, &out, &errOut)

	if exitCode != 0 {
		t.Errorf("handler status --help: got exit code %d, want 0; stderr: %s",
			exitCode, errOut.String())
	}

	output := out.String()
	for _, want := range []string{
		"--type",
		"--format",
		"--project",
		"EXIT CODES",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("handler status --help output missing %q:\n%s", want, output)
		}
	}
}

// TestHandlerResumeHelpFlag verifies per-verb help for `harmonik handler resume --help`.
//
// Parallel: safe.
func TestHandlerResumeHelpFlag(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	exitCode := runHandlerSubcommandIO([]string{"resume", "--help"}, &out, &errOut)

	if exitCode != 0 {
		t.Errorf("handler resume --help: got exit code %d, want 0; stderr: %s",
			exitCode, errOut.String())
	}

	output := out.String()
	for _, want := range []string{
		"--type",
		"--force",
		"--project",
		"EXIT CODES",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("handler resume --help output missing %q:\n%s", want, output)
		}
	}
}

// TestMaxConcurrentDefaultNotDuplicated verifies that the top-level help does
// not contain the duplicated default string "(default 1) (default 1)", which
// was the bug fixed by hk-oj65f (drop the duplicated flag.IntVar default).
//
// Not parallel: mutates os.Args and flag.CommandLine.
//
// Acceptance: hk-u0oo2 — "(default 1)" appears exactly once on the
// --max-concurrent line.
func TestMaxConcurrentDefaultNotDuplicated(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "--help"})

	output := helpFixtureCaptureStderr(t, func() {
		_ = run()
	})

	// The doubled-default string must not appear.
	if strings.Contains(output, "(default 1) (default 1)") {
		t.Errorf("top-level help contains doubled default %q:\n%s",
			"(default 1) (default 1)", output)
	}

	// The flag itself must still appear (sanity check that we got real output).
	if !strings.Contains(output, "--max-concurrent") {
		t.Errorf("top-level help missing --max-concurrent entirely:\n%s", output)
	}
}

// TestReconcileHelpFlag verifies that `harmonik reconcile --help` exits 0 and
// the output contains the expected flags and exit-code documentation.
//
// Parallel: safe — runReconcileSubcommand does not touch flag.CommandLine.
//
// Acceptance: hk-u0oo2 — reconcile --help lists flags and exit codes.
func TestReconcileHelpFlag(t *testing.T) {
	t.Parallel()

	var exitCode int
	output := helpFixtureCaptureStdout(t, func() {
		exitCode = runReconcileSubcommand([]string{"--help"})
	})

	if exitCode != 0 {
		t.Errorf("runReconcileSubcommand([--help]): got exit code %d, want 0", exitCode)
	}

	for _, want := range []string{
		"--project",
		"--target-branch",
		"EXIT CODES",
		"harmonik reconcile", // at least one example or usage line
	} {
		if !strings.Contains(output, want) {
			t.Errorf("reconcile --help output missing %q:\n%s", want, output)
		}
	}
}

// TestConfirmVerdictHelpFlag verifies that `harmonik confirm-verdict --help`
// exits 0 and the output contains the expected grammar, flags, and exit codes.
//
// Parallel: safe — runConfirmVerdictSubcommand does not touch flag.CommandLine.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
//
//	specs/operator-nfr.md §4.3 ON-014.
func TestConfirmVerdictHelpFlag(t *testing.T) {
	t.Parallel()

	var exitCode int
	output := helpFixtureCaptureStdout(t, func() {
		exitCode = runConfirmVerdictSubcommand([]string{"--help"})
	})

	if exitCode != 0 {
		t.Errorf("runConfirmVerdictSubcommand([--help]): got exit code %d, want 0", exitCode)
	}

	for _, want := range []string{
		"confirm-verdict",
		"<run_id>",
		"--project",
		"EXIT CODES",
		"16", // operator-control-invalid-state
		"17", // daemon not running
	} {
		if !strings.Contains(output, want) {
			t.Errorf("confirm-verdict --help output missing %q:\n%s", want, output)
		}
	}
}

// TestVetoVerdictHelpFlag verifies that `harmonik veto-verdict --help` exits 0
// and the output contains the expected grammar, flags (including --promote-to),
// and exit codes.
//
// Parallel: safe — runVetoVerdictSubcommand does not touch flag.CommandLine.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
//
//	specs/operator-nfr.md §4.3 ON-014 —
//	"harmonik veto-verdict <run_id> [--promote-to escalate-to-human]".
func TestVetoVerdictHelpFlag(t *testing.T) {
	t.Parallel()

	var exitCode int
	output := helpFixtureCaptureStdout(t, func() {
		exitCode = runVetoVerdictSubcommand([]string{"--help"})
	})

	if exitCode != 0 {
		t.Errorf("runVetoVerdictSubcommand([--help]): got exit code %d, want 0", exitCode)
	}

	for _, want := range []string{
		"veto-verdict",
		"<run_id>",
		"--promote-to",
		"escalate-to-human",
		"--project",
		"EXIT CODES",
		"16", // operator-control-invalid-state
		"17", // daemon not running
	} {
		if !strings.Contains(output, want) {
			t.Errorf("veto-verdict --help output missing %q:\n%s", want, output)
		}
	}
}

// TestTopLevelHelpListsConfirmVetoCmds verifies that the top-level
// `harmonik --help` output lists the confirm-verdict and veto-verdict
// subcommands.
//
// Not parallel: mutates os.Args and flag.CommandLine.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-014.
func TestTopLevelHelpListsConfirmVetoCmds(t *testing.T) {
	mainFixtureResetFlags(t)
	mainFixtureSaveRestoreArgs(t, []string{"harmonik", "--help"})

	output := helpFixtureCaptureStderr(t, func() {
		_ = run()
	})

	for _, want := range []string{
		"confirm-verdict",
		"veto-verdict",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("top-level help missing %q:\n%s", want, output)
		}
	}
}

// Compile-time assertion: flag package is imported and used (avoids unused-import lint).
var _ = flag.CommandLine
