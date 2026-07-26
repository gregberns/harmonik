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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sh033SubprocessEnv is the env var that puts the test binary into the
// double-SIGINT subprocess role.
const sh033SubprocessEnv = "HARMONIK_TEST_SH033_SUBPROCESS"

const (
	sh033ReadyMarker         = "SH033_READY"
	sh033GracefulWriteMarker = "SH033_GRACEFUL_WRITE_STARTED"
)

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
// TestHarnessSH033_DoubleSIGINTHardExit. It announces when signal handling is
// armed, then blocks the graceful SuiteResult write after announcing that the
// shutdown path has begun. The parent sends SIGINT only at those two explicit
// barriers, proving the second signal lands during graceful shutdown rather
// than racing harness startup or completion.
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

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT)
	if _, wErr := fmt.Fprintln(os.Stdout, sh033ReadyMarker); wErr != nil {
		fmt.Fprintf(os.Stderr, "sh033 subprocess: write ready marker: %v\n", wErr)
		os.Exit(99)
	}

	code := runHarnessWithSigs(
		[]string{"--scenario", scenarioFile},
		sh033BlockingResultWriter{}, os.Stderr, sigCh,
	)
	os.Exit(code)
}

// sh033BlockingResultWriter holds the subprocess inside graceful result
// emission. The second SIGINT must therefore take the goroutine's os.Exit(130)
// hard-exit path; a normal return is impossible after this write begins.
type sh033BlockingResultWriter struct{}

func (sh033BlockingResultWriter) Write(_ []byte) (int, error) {
	// If the barrier marker cannot be emitted the parent will never send the
	// second SIGINT, so fail the write instead of blocking forever — the parent
	// then fails fast at its waitMarker barrier rather than at the 5 s timeout.
	if _, wErr := fmt.Fprintln(os.Stdout, sh033GracefulWriteMarker); wErr != nil {
		return 0, wErr
	}
	select {}
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
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		t.Fatalf("subprocess stdout pipe: %v", pipeErr)
	}
	cmd.Stderr = os.Stderr

	if startErr := cmd.Start(); startErr != nil {
		t.Fatalf("subprocess start: %v", startErr)
	}

	// Exactly one goroutine owns Wait. Every post-Start failure runs the defer,
	// which kills a still-live child and synchronously waits for that owner to
	// reap it. The success path marks the already-reaped child so the defer is a
	// no-op; this avoids both leaked subprocesses and double-Wait races.
	var waitErr error
	done := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	reaped := false
	defer func() {
		if reaped {
			return
		}
		// A kill error is not actionable here — the child may have exited between
		// the failure and this defer — but it must not be silently discarded.
		// os.ErrProcessDone is that benign race and is filtered out; anything
		// else means a subprocess may have leaked, which is worth surfacing.
		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Logf("sh033: kill subprocess: %v", killErr)
		}
		<-done
	}()

	lines := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	waitMarker := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("subprocess marker = %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for subprocess marker %q", want)
		}
	}

	waitMarker(sh033ReadyMarker)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send first SIGINT: %v", err)
	}
	waitMarker(sh033GracefulWriteMarker)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send second SIGINT: %v", err)
	}

	const deadline = 5 * time.Second
	select {
	case <-done:
		reaped = true
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("subprocess did not exit via os.Exit: %v", waitErr)
		}
		if exitErr.ExitCode() != 130 {
			t.Errorf("double-SIGINT: want exit 130, got %d", exitErr.ExitCode())
		}
	case <-time.After(deadline):
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
