package brcli

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
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("stderrCapFixtureBinary: write: %v", err)
	}
	return path
}

func stderrCapFixtureLargeStderrBinary(t *testing.T, n, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf(
		"#!/bin/sh\ndd if=/dev/zero bs=%d count=1 2>/dev/null | tr '\\0' 'A' >&2\nexit %d\n",
		n, exitCode,
	)
	//nolint:gosec // G306: test fixture binary; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("stderrCapFixtureLargeStderrBinary: write: %v", err)
	}
	return path
}

func stderrCapFixtureSIGKILLBinary(t *testing.T, partialStderr string, sleepSeconds float64) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	stderrFile := filepath.Join(dir, "stderr.txt")

	if err := os.WriteFile(stderrFile, []byte(partialStderr), 0o600); err != nil {
		t.Fatalf("stderrCapFixtureSIGKILLBinary: write stderr data: %v", err)
	}

	script := fmt.Sprintf(
		"#!/bin/sh\ncat %s >&2\necho ready\nsleep %.3f\nexit 0\n",
		stderrFile, sleepSeconds,
	)
	//nolint:gosec // G306: test fixture binary; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("stderrCapFixtureSIGKILLBinary: write: %v", err)
	}
	return path
}

func runWithStderrCap(t *testing.T, name string, args ...string) (result StderrResult, exitCode int) {
	t.Helper()
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

// TestStderrCapScenarioRustPanicExit101 verifies BI-025d scenario (c):
// a process exiting 101 with a large "Rust panic" backtrace written to stderr
// (approaching or exceeding the 1 MiB cap) is correctly truncated.  The adapter
// must classify as BrOther per BI-025a with the (possibly truncated) panic
// stderr attached.
func TestStderrCapScenarioRustPanicExit101(t *testing.T) {
	t.Run("rust_panic_exit_101_large_backtrace", func(t *testing.T) {
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
				string(sr.Bytes[maxInt(0, len(sr.Bytes)-len(StderrTruncationSuffix)-10):]))
		}
		suffixWithNL := "\n" + StderrTruncationSuffix
		capturedBody := sr.Bytes[:len(sr.Bytes)-len(suffixWithNL)]
		if len(capturedBody) != stderrCapMaxBytes {
			t.Errorf("captured body len = %d; want %d (1 MiB cap)", len(capturedBody), stderrCapMaxBytes)
		}
	})
}

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
		partialMsg := "thread 'main' panicked at 'index out of bounds'"
		path := stderrCapFixtureSIGKILLBinary(t, partialMsg, 30.0) // sleeps 30s

		//nolint:gosec // G204: test helper; path is a synthetic fixture
		cmd := exec.CommandContext(t.Context(), path)
		capW := newStderrCapWriter()
		cmd.Stderr = capW
		stdoutR, stdoutW, pipeErr := os.Pipe()
		if pipeErr != nil {
			t.Fatalf("os.Pipe: %v", pipeErr)
		}
		cmd.Stdout = stdoutW
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := cmd.Start(); err != nil {
			_ = stdoutW.Close()
			_ = stdoutR.Close()
			t.Fatalf("cmd.Start: %v", err)
		}
		_ = stdoutW.Close()

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

		readyTimeout := 60 * time.Second
		if dl, ok := t.Deadline(); ok {
			if budget := time.Until(dl) - 2*time.Second; budget > 0 && budget < readyTimeout {
				readyTimeout = budget
			}
		}
		select {
		case <-readyCh:
		case <-time.After(readyTimeout):
			t.Fatalf("subprocess did not emit ready marker within %s", readyTimeout)
		}

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

		_ = stdoutR.Close()

		if err := cmd.Wait(); err == nil {
			t.Error("Wait: expected SIGKILL exit error, got nil")
		}

		sr := capW.Result()

		if sr.Truncated {
			t.Errorf("Truncated = true; want false — kill-truncation is not a 1 MiB cap overflow")
		}
		if !strings.Contains(string(sr.Bytes), partialMsg) {
			t.Errorf("captured stderr %q does not contain expected partial msg %q",
				string(sr.Bytes), partialMsg)
		}
	})
}

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
	half := stderrCapMaxBytes / 2
	if _, err := w.Write(bytes.Repeat([]byte{'A'}, half)); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := w.Write(bytes.Repeat([]byte{'B'}, half)); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
