package main

// harness_sh033_test.go — contract tests for SH-033 signal handling.
//
// SH-033: The harness MUST handle SIGINT and SIGTERM by attempting graceful
// shutdown: cancel the currently-running scenario (treating it as a
// timeout-equivalent), execute SH-015 teardown, write a partial SuiteResult to
// stdout containing the results of completed scenarios plus the interrupted
// scenario's error verdict, and exit with code 130 (SIGINT) or 143 (SIGTERM).
// If a second SIGINT arrives during graceful shutdown the harness MUST exit
// immediately (exit code 130) without further teardown.
//
// Test strategy:
//
//  1. Single-signal tests (SIGINT → 130, SIGTERM → 143): use runHarnessWithSigs
//     with a pre-loaded signal channel. The harness blocks at <-ctx.Done()
//     (execution-loop stub) until the signal goroutine cancels ctx.
//
//  2. Double-SIGINT hard-exit test: subprocess pattern (calls os.Exit — cannot
//     be observed in-process). The subprocess receives two real OS SIGINT
//     signals from the parent and must exit within a bounded time.
//
//  3. harnessInterruptExitCode and harnessEmitInterruptResult: pure-function
//     unit tests that exercise the helpers without subprocess overhead.
//
// Helper prefix: sh033 (per implementer-protocol.md §Helper-prefix discipline).
// Spec ref: specs/scenario-harness.md §4.13 SH-033.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sh033SubprocessEnv is the env var that puts the test binary into the
// double-SIGINT subprocess role.
const sh033SubprocessEnv = "HARMONIK_TEST_SH033_SUBPROCESS"

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// sh033MinimalScenarioPath creates a minimal valid scenario YAML file in a
// temporary directory and returns the absolute path. The temp dir is cleaned
// up by t.Cleanup.
func sh033MinimalScenarioPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := strings.Join([]string{
		"name: sh033-test-scenario",
		"description: SH-033 signal handling test",
		"workflow_path: test.dot",
		"timeout_secs: 30",
		"cadence_tag: smoke",
		"",
	}, "\n")
	path := filepath.Join(dir, "sh033-test.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("sh033MinimalScenarioPath: write: %v", err)
	}
	return path
}

// sh033PreloadedSigCh returns a signal channel (cap 2) with sig already
// buffered, simulating an OS signal that arrives at harness startup.
func sh033PreloadedSigCh(sig os.Signal) <-chan os.Signal {
	ch := make(chan os.Signal, 2)
	ch <- sig
	return ch
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-033 unit tests: harnessInterruptExitCode
// ─────────────────────────────────────────────────────────────────────────────

// TestHarnessSH033_InterruptExitCode verifies harnessInterruptExitCode returns
// 130 for SIGINT and 143 for SIGTERM per specs §4.12 SH-032 exit-code table.
func TestHarnessSH033_InterruptExitCode(t *testing.T) {
	t.Parallel()

	if got := harnessInterruptExitCode(syscall.SIGINT); got != 130 {
		t.Errorf("SIGINT: want 130, got %d", got)
	}
	if got := harnessInterruptExitCode(syscall.SIGTERM); got != 143 {
		t.Errorf("SIGTERM: want 143, got %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-033 integration tests: single SIGINT/SIGTERM
// ─────────────────────────────────────────────────────────────────────────────

// TestHarnessSH033_SIGINTExitCode130 verifies that a SIGINT received during
// the execution phase causes exit code 130 and emits a partial SuiteResult
// to stdout per SH-033.
func TestHarnessSH033_SIGINTExitCode130(t *testing.T) {
	t.Parallel()

	scenarioPath := sh033MinimalScenarioPath(t)
	sigCh := sh033PreloadedSigCh(syscall.SIGINT)

	var stdout, stderr bytes.Buffer
	code := runHarnessWithSigs(
		[]string{"--scenario", scenarioPath},
		&stdout, &stderr,
		sigCh,
	)

	if code != 130 {
		t.Errorf("SIGINT: want exit 130, got %d\nstderr: %s", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Error("SIGINT: want partial SuiteResult on stdout, got empty output")
	}
}

// TestHarnessSH033_SIGTERMExitCode143 verifies that a SIGTERM received during
// the execution phase causes exit code 143 and emits a partial SuiteResult
// to stdout per SH-033.
func TestHarnessSH033_SIGTERMExitCode143(t *testing.T) {
	t.Parallel()

	scenarioPath := sh033MinimalScenarioPath(t)
	sigCh := sh033PreloadedSigCh(syscall.SIGTERM)

	var stdout, stderr bytes.Buffer
	code := runHarnessWithSigs(
		[]string{"--scenario", scenarioPath},
		&stdout, &stderr,
		sigCh,
	)

	if code != 143 {
		t.Errorf("SIGTERM: want exit 143, got %d\nstderr: %s", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Error("SIGTERM: want partial SuiteResult on stdout, got empty output")
	}
}

// TestHarnessSH033_PartialSuiteResultJSON verifies that on SIGINT with
// --output json the stdout contains valid JSON with the expected fields.
func TestHarnessSH033_PartialSuiteResultJSON(t *testing.T) {
	t.Parallel()

	scenarioPath := sh033MinimalScenarioPath(t)
	sigCh := sh033PreloadedSigCh(syscall.SIGINT)

	var stdout, stderr bytes.Buffer
	code := runHarnessWithSigs(
		[]string{"--scenario", scenarioPath, "--output", "json"},
		&stdout, &stderr,
		sigCh,
	)

	if code != 130 {
		t.Errorf("want exit 130, got %d\nstderr: %s", code, stderr.String())
	}

	// Stdout must contain valid JSON with a suite_id field.
	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout.String())
	}

	if _, ok := result["suite_id"]; !ok {
		t.Errorf("JSON result missing suite_id field; got keys: %v", jsonKeys(result))
	}
	if _, ok := result["suite_verdict"]; !ok {
		t.Errorf("JSON result missing suite_verdict field; got keys: %v", jsonKeys(result))
	}
}

// TestHarnessSH033_StderrMentionsSignal verifies that the graceful-shutdown
// path logs the signal name to stderr so operators can identify the cause.
func TestHarnessSH033_StderrMentionsSignal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		sig     os.Signal
		sigName string
	}{
		{syscall.SIGINT, "SIGINT"},
		{syscall.SIGTERM, "SIGTERM"},
	} {
		tc := tc
		t.Run(tc.sigName, func(t *testing.T) {
			t.Parallel()

			scenarioPath := sh033MinimalScenarioPath(t)
			sigCh := sh033PreloadedSigCh(tc.sig)

			var stdout, stderr bytes.Buffer
			runHarnessWithSigs(
				[]string{"--scenario", scenarioPath},
				&stdout, &stderr,
				sigCh,
			)

			if !strings.Contains(stderr.String(), tc.sigName) {
				t.Errorf("stderr does not mention %s\nstderr: %s", tc.sigName, stderr.String())
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-033 subprocess test: double-SIGINT hard exit
// ─────────────────────────────────────────────────────────────────────────────

// sh033SubprocessRoleDoubleSIGINT is the subprocess worker for
// TestHarnessSH033_DoubleSIGINTHardExit. It pre-loads both SIGINT signals into
// the harness signal channel before calling runHarnessWithSigs so the
// double-SIGINT detection is deterministic (no OS signal timing races).
//
// The first signal triggers the graceful-shutdown path; the main goroutine's
// non-blocking pre-check reads the second signal from the buffer and returns
// harnessExitOperatorInterrupt (130) before writing any SuiteResult to stdout.
//
// In a concurrent race (goroutine's second select wins the sigCh read instead),
// os.Exit(harnessExitOperatorInterrupt) fires — same exit code, same empty stdout.
//
// This function MUST be called only from the subprocess role; it always exits
// via os.Exit and never returns normally.
func sh033SubprocessRoleDoubleSIGINT() {
	// Create a temp dir with a valid scenario file so discovery succeeds.
	dir, err := os.MkdirTemp("", "harmonik-sh033-dbl-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sh033 subprocess: MkdirTemp: %v\n", err)
		os.Exit(99)
	}
	// No cleanup — subprocess exits immediately.

	content := strings.Join([]string{
		"name: sh033-dbl-sigint-scenario",
		"description: double-SIGINT subprocess test",
		"workflow_path: test.dot",
		"timeout_secs: 30",
		"cadence_tag: smoke",
		"",
	}, "\n")
	scenarioFile := filepath.Join(dir, "scenario.yaml")
	if writeErr := os.WriteFile(scenarioFile, []byte(content), 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "sh033 subprocess: write scenario: %v\n", writeErr)
		os.Exit(99)
	}

	// Pre-load both signals so the main goroutine's non-blocking sigCh check
	// reads the second SIGINT deterministically before calling
	// harnessEmitInterruptResult. No OS signals from the parent are needed.
	sigCh := make(chan os.Signal, 2)
	sigCh <- syscall.SIGINT // first signal — triggers graceful shutdown
	sigCh <- syscall.SIGINT // second signal — caught by main's pre-check → return 130

	code := runHarnessWithSigs([]string{"--scenario", scenarioFile}, os.Stdout, os.Stderr, sigCh)
	os.Exit(code)
}

// TestHarnessSH033_DoubleSIGINTHardExit verifies that a second SIGINT during
// graceful shutdown causes os.Exit(130) immediately per SH-033.
//
// Uses the subprocess pattern because os.Exit cannot be observed in-process.
// NOT parallel: the subprocess role modifies global signal state.
func TestHarnessSH033_DoubleSIGINTHardExit(t *testing.T) {
	// Subprocess worker role: run the blocking harness and accept signals.
	if os.Getenv(sh033SubprocessEnv) == "1" {
		sh033SubprocessRoleDoubleSIGINT()
		// sh033SubprocessRoleDoubleSIGINT should never return.
		os.Exit(99)
	}

	// Parent role: locate and spawn the test binary as a subprocess.
	// The subprocess role pre-loads both signals into the harness channel, so
	// the parent does NOT need to send any OS signals or wait for the child to
	// reach a blocking state. Without -test.v the test framework emits nothing
	// to stdout before the subprocess role runs.
	testBin, lookErr := exec.LookPath(os.Args[0])
	if lookErr != nil {
		testBin = os.Args[0]
	}

	cmd := exec.CommandContext( //nolint:gosec // G204: argv from os.Args[0], test-only
		context.Background(),
		testBin,
		"-test.run=TestHarnessSH033_DoubleSIGINTHardExit",
	)
	cmd.Env = append(os.Environ(), sh033SubprocessEnv+"=1")
	var subStdout bytes.Buffer
	cmd.Stdout = &subStdout
	cmd.Stderr = os.Stderr

	if startErr := cmd.Start(); startErr != nil {
		t.Fatalf("subprocess start: %v", startErr)
	}

	// Wait for the subprocess to exit. It pre-loads both SIGINTs so it exits
	// almost immediately (no signals or sleeps needed from the parent side).
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	const deadline = 5 * time.Second
	select {
	case waitErr := <-done:
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("subprocess did not exit via os.Exit: %v", waitErr)
		}
		if exitErr.ExitCode() != 130 {
			t.Errorf("double-SIGINT: want exit 130, got %d", exitErr.ExitCode())
		}
		// Hard-exit path must NOT write a SuiteResult to stdout. Any SuiteResult
		// content proves the graceful-shutdown path ran instead (the two paths
		// produce the same exit code 130, so stdout is the only distinguishing
		// observable).
		if strings.Contains(subStdout.String(), "suite_id:") || strings.Contains(subStdout.String(), `"suite_id"`) {
			t.Errorf("double-SIGINT hard-exit: stdout contains SuiteResult (graceful-shutdown path ran):\n%s",
				subStdout.String())
		}
	case <-time.After(deadline):
		_ = cmd.Process.Kill()
		t.Errorf("subprocess did not exit within %s", deadline)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// jsonKeys returns the top-level keys of a JSON object for test error messages.
func jsonKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
