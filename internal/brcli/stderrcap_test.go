package brcli

// stderrcap_test.go — BI-025d: bounded stderr capture, 5 scenarios.
//
// Tests are in package brcli (white-box) to access stderrCapWriter directly.
// Spec ref: specs/beads-integration.md §4.8a BI-025d.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// stderrCapFixtureBinary writes an executable shell script to a temp dir and
// returns its path. The script writes stderrText to stderr and exits with
// exitCode. stdoutText is written to stdout.
//
// Text is delivered via companion data files (not shell quoting) so that
// arbitrary bytes including newlines and single-quotes survive the round-trip.
func stderrCapFixtureBinary(t *testing.T, stdoutText, stderrText string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	stdoutFile := filepath.Join(dir, "stdout.txt")
	stderrFile := filepath.Join(dir, "stderr.txt")

	if err := os.WriteFile(stdoutFile, []byte(stdoutText), 0o600); err != nil {
		t.Fatalf("stderrCapFixtureBinary: write stdout data: %v", err)
	}
	if err := os.WriteFile(stderrFile, []byte(stderrText), 0o600); err != nil {
		t.Fatalf("stderrCapFixtureBinary: write stderr data: %v", err)
	}

	script := fmt.Sprintf(
		"#!/bin/sh\ncat %s\ncat %s >&2\nexit %d\n",
		stdoutFile, stderrFile, exitCode,
	)
	//nolint:gosec // G306: test fixture binary; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("stderrCapFixtureBinary: write: %v", err)
	}
	return path
}

// stderrCapFixtureLargeStderrBinary writes a binary that emits exactly n bytes
// to stderr and exits 0.  Used for the 1 MiB cap scenario.
func stderrCapFixtureLargeStderrBinary(t *testing.T, n int, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	// dd writes n bytes of zeros (as text 'x' repeated) to stderr.
	// We use printf + a Python one-liner to avoid shell portability issues with
	// large repeat counts; fall back to a simpler approach: write a helper Go
	// program is overkill — use dd reading /dev/zero through stderr.
	//
	// Simple approach: shell script using dd to emit n bytes of 'A' on stderr.
	script := fmt.Sprintf(
		"#!/bin/sh\ndd if=/dev/zero bs=%d count=1 2>/dev/null | tr '\\0' 'A' >&2\nexit %d\n",
		n, exitCode,
	)
	//nolint:gosec // G306: test fixture binary; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("stderrCapFixtureLargeStderrBinary: write: %v", err)
	}
	return path
}

// stderrCapFixtureSIGKILLBinary writes a binary that writes partialStderr to
// stderr (via a data file to avoid shell-quoting issues), then sleeps.
// The test kills the process while it is sleeping, capturing whatever stderr
// was flushed before the kill.
func stderrCapFixtureSIGKILLBinary(t *testing.T, partialStderr string, sleepSeconds float64) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	stderrFile := filepath.Join(dir, "stderr.txt")

	if err := os.WriteFile(stderrFile, []byte(partialStderr), 0o600); err != nil {
		t.Fatalf("stderrCapFixtureSIGKILLBinary: write stderr data: %v", err)
	}

	// cat flushes on exit; the script echoes a synchronisation marker to stdout
	// after the stderr write so the test can wait for the write to complete
	// before sending SIGKILL.  "exec 1>&2" is not used; instead we rely on
	// the pipe + buffering being complete well within the 500ms sleep we allow.
	script := fmt.Sprintf(
		"#!/bin/sh\ncat %s >&2\necho ready\nsleep %.3f\nexit 0\n",
		stderrFile, sleepSeconds,
	)
	//nolint:gosec // G306: test fixture binary; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("stderrCapFixtureSIGKILLBinary: write: %v", err)
	}
	return path
}

// runWithStderrCap is a thin helper that wires a stderrCapWriter to a command's
// Stderr, starts and waits for the command, and returns the StderrResult plus
// exit code. It models how the adapter will use stderrCapWriter in production.
func runWithStderrCap(t *testing.T, name string, args ...string) (StderrResult, int) {
	t.Helper()
	//nolint:gosec // G204: test helper; name/args are synthetic fixture paths, not user input
	cmd := exec.CommandContext(t.Context(), name, args...)
	capW := newStderrCapWriter()
	cmd.Stderr = capW
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	if err := cmd.Run(); err != nil {
		switch e := err.(type) { //nolint:errorlint // direct type switch on exec.ExitError is idiomatic here
		case *exec.ExitError:
			return capW.Result(), e.ExitCode()
		default:
			t.Fatalf("runWithStderrCap: unexpected exec error: %v", err)
		}
	}
	return capW.Result(), 0
}

// --- Scenario 1: exit 0 + non-empty stderr (warnings on success) ---

// TestStderrCapScenarioExitZeroWithWarnings verifies BI-025d scenario (a):
// when br exits 0 but emits warning text on stderr, the full stderr is captured
// and the result is NOT truncated.  The adapter must classify as BrOK and
// surface the warnings via structured-log without blocking the success path.
func TestStderrCapScenarioExitZeroWithWarnings(t *testing.T) {
	t.Run("exit0_with_stderr_warnings", func(t *testing.T) {
		warningText := "warning: database file is read-only\nwarning: schema version drift detected\n"
		path := stderrCapFixtureBinary(t, "ok-output", warningText, 0)

		sr, exitCode := runWithStderrCap(t, path)

		if exitCode != 0 {
			t.Errorf("exitCode = %d; want 0", exitCode)
		}
		if sr.Truncated {
			t.Errorf("Truncated = true; want false for warning-size stderr")
		}
		if string(sr.Bytes) != warningText {
			t.Errorf("Bytes = %q; want %q", string(sr.Bytes), warningText)
		}
	})
}

// --- Scenario 2: exit ≠ 0 + empty stderr ---

// TestStderrCapScenarioNonZeroEmptyStderr verifies BI-025d scenario (b):
// when br exits non-zero with empty stderr, the captured bytes are empty and
// not truncated.  The adapter must attach "(empty stderr)" as a placeholder
// string in the typed error (that adapter-layer behaviour is not in scope here;
// the capture mechanism returns an empty, non-truncated result).
func TestStderrCapScenarioNonZeroEmptyStderr(t *testing.T) {
	t.Run("nonzero_exit_empty_stderr", func(t *testing.T) {
		path := stderrCapFixtureBinary(t, "", "", 1)

		sr, exitCode := runWithStderrCap(t, path)

		if exitCode != 1 {
			t.Errorf("exitCode = %d; want 1", exitCode)
		}
		if sr.Truncated {
			t.Errorf("Truncated = true; want false for empty stderr")
		}
		if len(sr.Bytes) != 0 {
			t.Errorf("Bytes = %q; want empty", string(sr.Bytes))
		}
	})
}

// --- Scenario 3: Rust panic exit 101 with large stderr (backtrace) ---

// TestStderrCapScenarioRustPanicExit101 verifies BI-025d scenario (c):
// a process exiting 101 with a large "Rust panic" backtrace written to stderr
// (approaching or exceeding the 1 MiB cap) is correctly truncated.  The adapter
// must classify as BrOther per BI-025a with the (possibly truncated) panic
// stderr attached.
func TestStderrCapScenarioRustPanicExit101(t *testing.T) {
	t.Run("rust_panic_exit_101_large_backtrace", func(t *testing.T) {
		// Write slightly more than the 1 MiB cap so truncation fires.
		overCapBytes := stderrCapMaxBytes + 512

		path := stderrCapFixtureLargeStderrBinary(t, overCapBytes, 101)

		sr, exitCode := runWithStderrCap(t, path)

		if exitCode != 101 {
			t.Errorf("exitCode = %d; want 101", exitCode)
		}
		if !sr.Truncated {
			t.Errorf("Truncated = false; want true for %d-byte stderr payload", overCapBytes)
		}
		if !strings.HasSuffix(string(sr.Bytes), StderrTruncationSuffix) {
			t.Errorf("Bytes does not end with StderrTruncationSuffix; got suffix: %q",
				string(sr.Bytes[max(0, len(sr.Bytes)-len(StderrTruncationSuffix)-10):]))
		}
		// The captured payload before the suffix must be exactly the cap.
		// sr.Bytes = cap-bytes + '\n' + suffix
		suffixWithNL := "\n" + StderrTruncationSuffix
		capturedBody := sr.Bytes[:len(sr.Bytes)-len(suffixWithNL)]
		if len(capturedBody) != stderrCapMaxBytes {
			t.Errorf("captured body len = %d; want %d (1 MiB cap)", len(capturedBody), stderrCapMaxBytes)
		}
	})
}

// --- Scenario 4: argparse error exit 2 with usage text on stderr ---

// TestStderrCapScenarioArgparseExit2 verifies BI-025d scenario (d):
// br exits 2 (Rust clap convention for argparse errors) with usage text on
// stderr.  The capture must include the full usage text so the adapter can
// surface it for operator triage.  Classified as BrOther per BI-025a.
func TestStderrCapScenarioArgparseExit2(t *testing.T) {
	t.Run("argparse_exit_2_usage_text", func(t *testing.T) {
		usageText := "error: unexpected argument '--unknown-flag'\n\nUsage: br <COMMAND> [OPTIONS]\n"
		path := stderrCapFixtureBinary(t, "", usageText, 2)

		sr, exitCode := runWithStderrCap(t, path)

		if exitCode != 2 {
			t.Errorf("exitCode = %d; want 2", exitCode)
		}
		if sr.Truncated {
			t.Errorf("Truncated = true; want false for short argparse error message")
		}
		if string(sr.Bytes) != usageText {
			t.Errorf("Bytes = %q; want %q", string(sr.Bytes), usageText)
		}
	})
}

// --- Scenario 5: partial stderr at SIGKILL ---

// TestStderrCapScenarioPartialStderrAtSIGKILL verifies BI-025d scenario (e):
// when the BI-025c timeout path kills the subprocess with SIGKILL while it is
// mid-write, the capture returns whatever bytes were flushed before the kill,
// without marking as truncated (the kill is signaled by ErrBrTimeout at the
// adapter layer, not by the 1 MiB cap).
//
// This test directly manages the subprocess lifecycle (Start/SIGKILL/Wait)
// rather than using runWithStderrCap, because it must interpose the kill
// between the stderr write and the process exit.
func TestStderrCapScenarioPartialStderrAtSIGKILL(t *testing.T) {
	t.Run("partial_stderr_at_sigkill", func(t *testing.T) {
		// The binary writes a known string to stderr, then sleeps.
		// We SIGKILL it after a short delay; the partial stderr must be present
		// in the capture.
		partialMsg := "thread 'main' panicked at 'index out of bounds'"
		path := stderrCapFixtureSIGKILLBinary(t, partialMsg, 30.0) // sleeps 30s

		//nolint:gosec // G204: test helper; path is a synthetic fixture
		cmd := exec.CommandContext(t.Context(), path)
		capW := newStderrCapWriter()
		cmd.Stderr = capW
		// Pipe stdout through a channel so we can detect the "ready" marker
		// without relying on bytes.Buffer visibility across goroutines (the exec
		// package drains the pipe in a background goroutine).
		stdoutR, stdoutW, pipeErr := os.Pipe()
		if pipeErr != nil {
			t.Fatalf("os.Pipe: %v", pipeErr)
		}
		cmd.Stdout = stdoutW
		// Run in its own process group so we can kill the entire group (shell +
		// sleep child) with a single SIGKILL, preventing the orphaned sleep from
		// holding the stdout pipe open and blocking cmd.Wait().
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := cmd.Start(); err != nil {
			_ = stdoutW.Close()
			_ = stdoutR.Close()
			t.Fatalf("cmd.Start: %v", err)
		}
		// Close the write end in the parent so the read end sees EOF when the
		// process group exits.
		_ = stdoutW.Close()

		// Read stdout asynchronously, looking for the "ready" marker.
		readyCh := make(chan struct{})
		go func() {
			buf := make([]byte, 64)
			var acc strings.Builder
			for {
				n, err := stdoutR.Read(buf)
				if n > 0 {
					acc.Write(buf[:n])
					if strings.Contains(acc.String(), "ready") {
						close(readyCh)
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()

		// Derive the readiness bound from the test's own deadline so it scales
		// with -timeout and only fires on a true hang, not CPU starvation under
		// heavy -race parallelism.
		readyTimeout := 60 * time.Second
		if dl, ok := t.Deadline(); ok {
			if budget := time.Until(dl) - 2*time.Second; budget > 0 && budget < readyTimeout {
				readyTimeout = budget
			}
		}
		select {
		case <-readyCh:
			// Subprocess has flushed stderr; proceed to kill.
		case <-time.After(readyTimeout):
			t.Fatalf("subprocess did not emit ready marker within %s", readyTimeout)
		}

		// SIGKILL the entire process group — kills the shell AND its sleep child,
		// ensuring cmd.Wait() returns promptly (no orphan holds the pipe open).
		if cmd.Process != nil {
			pgid, pgidErr := syscall.Getpgid(cmd.Process.Pid)
			if pgidErr == nil {
				//nolint:errcheck // SIGKILL errors on already-exited groups are expected
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
					t.Logf("SIGKILL: %v (process may have already exited)", err)
				}
			}
		}

		// Drain remaining stdout so the goroutine unblocks.
		_ = stdoutR.Close()

		// Wait to reap per PL-014; ignore the error (SIGKILL always returns one).
		_ = cmd.Wait()

		sr := capW.Result()

		// The partial message must be present; truncation flag must not be set
		// (we didn't hit the 1 MiB cap — the process was killed, not overflowed).
		if sr.Truncated {
			t.Errorf("Truncated = true; want false — kill-truncation is not a 1 MiB cap overflow")
		}
		if !strings.Contains(string(sr.Bytes), partialMsg) {
			t.Errorf("captured stderr %q does not contain expected partial msg %q",
				string(sr.Bytes), partialMsg)
		}
	})
}

// --- Unit tests for stderrCapWriter internals ---

// TestStderrCapWriterBelowCap verifies that writes below the 1 MiB cap are
// stored verbatim and Truncated remains false.
func TestStderrCapWriterBelowCap(t *testing.T) {
	w := newStderrCapWriter()
	payload := []byte("hello stderr\n")
	n, err := w.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Errorf("Write returned %d; want %d", n, len(payload))
	}
	sr := w.Result()
	if sr.Truncated {
		t.Errorf("Truncated = true; want false")
	}
	if !bytes.Equal(sr.Bytes, payload) {
		t.Errorf("Bytes = %q; want %q", sr.Bytes, payload)
	}
}

// TestStderrCapWriterExactlyAtCap verifies that a write of exactly 1 MiB is
// not truncated.
func TestStderrCapWriterExactlyAtCap(t *testing.T) {
	w := newStderrCapWriter()
	payload := bytes.Repeat([]byte{'X'}, stderrCapMaxBytes)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sr := w.Result()
	if sr.Truncated {
		t.Errorf("Truncated = true; want false for exactly-at-cap payload")
	}
	if len(sr.Bytes) != stderrCapMaxBytes {
		t.Errorf("len(Bytes) = %d; want %d", len(sr.Bytes), stderrCapMaxBytes)
	}
}

// TestStderrCapWriterOneByteOverCap verifies that writing 1 MiB + 1 byte
// triggers truncation and appends the truncation suffix.
func TestStderrCapWriterOneByteOverCap(t *testing.T) {
	w := newStderrCapWriter()
	payload := bytes.Repeat([]byte{'X'}, stderrCapMaxBytes+1)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sr := w.Result()
	if !sr.Truncated {
		t.Errorf("Truncated = false; want true for cap+1 payload")
	}
	if !strings.HasSuffix(string(sr.Bytes), StderrTruncationSuffix) {
		t.Errorf("Bytes does not end with StderrTruncationSuffix; got: %q", string(sr.Bytes))
	}
}

// TestStderrCapWriterMultipleWritesCrossing verifies that multiple smaller
// writes that collectively exceed the cap trigger truncation at the boundary.
func TestStderrCapWriterMultipleWritesCrossing(t *testing.T) {
	w := newStderrCapWriter()
	// Write 512 KiB in two chunks, then a third chunk that crosses the cap.
	half := stderrCapMaxBytes / 2
	if _, err := w.Write(bytes.Repeat([]byte{'A'}, half)); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := w.Write(bytes.Repeat([]byte{'B'}, half)); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	// This write crosses the cap.
	if _, err := w.Write([]byte("overflow")); err != nil {
		t.Fatalf("Write 3: %v", err)
	}
	sr := w.Result()
	if !sr.Truncated {
		t.Errorf("Truncated = false; want true after crossing cap across multiple writes")
	}
}

// TestStderrCapWriterEmptyResult verifies that an empty write produces an empty
// non-truncated result.
func TestStderrCapWriterEmptyResult(t *testing.T) {
	w := newStderrCapWriter()
	sr := w.Result()
	if sr.Truncated {
		t.Errorf("Truncated = true; want false for empty writer")
	}
	if len(sr.Bytes) != 0 {
		t.Errorf("Bytes = %q; want empty", sr.Bytes)
	}
}

// max returns the larger of a and b.  Avoids importing "math" for a single use.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
