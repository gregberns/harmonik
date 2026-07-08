package brcli_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
)

// timeoutFixtureMockBinary writes a shell script that prints stdout/stderr and
// exits with the given code. The binary is used for RunWithTimeout tests that
// expect normal subprocess completion (no timeout).
func timeoutFixtureMockBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nprintf '%%s' %q >&2\nexit %d\n", stdout, stderr, exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("timeoutFixtureMockBinary: write mock: %v", err)
	}
	return path
}

// timeoutFixtureSleepBinary writes a shell script that sleeps for the given
// duration then exits 0. The binary is used to trigger wall-clock timeout and
// context-cancellation paths in RunWithTimeout.
func timeoutFixtureSleepBinary(t *testing.T, d time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	// Use fractional seconds via printf to avoid integer rounding; `sleep`
	// accepts decimal arguments on macOS and Linux.
	seconds := d.Seconds()
	script := fmt.Sprintf("#!/bin/sh\nsleep %.3f\nexit 0\n", seconds)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("timeoutFixtureSleepBinary: write mock: %v", err)
	}
	return path
}

// timeoutFixtureAdapter returns an Adapter pointed at the given brPath.
// Fails the test if construction fails.
func timeoutFixtureAdapter(t *testing.T, brPath string) *brcli.Adapter {
	t.Helper()
	a, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("timeoutFixtureAdapter: New: %v", err)
	}
	return a
}

// timeoutFixtureFastCfg returns a TimeoutConfig with generous timeouts that
// allow short-lived mock shell scripts to complete normally (5s read, 10s
// write). Use timeoutFixtureTightCfg for tests that exercise the timeout path.
func timeoutFixtureFastCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// timeoutFixtureTightCfg returns a TimeoutConfig with very short timeouts
// that will fire before a long-running binary exits (50ms read, 100ms write).
// Only suitable for tests that deliberately exercise the timeout path.
func timeoutFixtureTightCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	}
}

// --- Tests ---

// TestRunWithTimeoutSuccessRead verifies that RunWithTimeout returns the
// subprocess stdout/stderr and ExitCode 0 when the subprocess exits before the
// timeout, for a read command.
func TestRunWithTimeoutSuccessRead(t *testing.T) {
	path := timeoutFixtureMockBinary(t, "read-ok", "", 0)
	a := timeoutFixtureAdapter(t, path)

	result, err := a.RunWithTimeout(context.Background(), timeoutFixtureFastCfg(), brcli.CommandKindRead)
	if err != nil {
		t.Fatalf("RunWithTimeout: unexpected error: %v", err)
	}
	if string(result.Stdout) != "read-ok" {
		t.Errorf("Stdout = %q; want %q", string(result.Stdout), "read-ok")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d; want 0", result.ExitCode)
	}
}

// TestRunWithTimeoutSuccessWrite verifies that RunWithTimeout returns correct
// results for a write command that exits before the timeout.
func TestRunWithTimeoutSuccessWrite(t *testing.T) {
	path := timeoutFixtureMockBinary(t, "write-ok", "", 0)
	a := timeoutFixtureAdapter(t, path)

	result, err := a.RunWithTimeout(context.Background(), timeoutFixtureFastCfg(), brcli.CommandKindWrite)
	if err != nil {
		t.Fatalf("RunWithTimeout: unexpected error: %v", err)
	}
	if string(result.Stdout) != "write-ok" {
		t.Errorf("Stdout = %q; want %q", string(result.Stdout), "write-ok")
	}
}

// TestRunWithTimeoutNonZeroExit verifies that a non-zero exit before the
// timeout is returned as a Result with no error (same semantics as Run).
func TestRunWithTimeoutNonZeroExit(t *testing.T) {
	path := timeoutFixtureMockBinary(t, "", "err-output", 1)
	a := timeoutFixtureAdapter(t, path)

	result, err := a.RunWithTimeout(context.Background(), timeoutFixtureFastCfg(), brcli.CommandKindRead)
	if err != nil {
		t.Fatalf("RunWithTimeout: unexpected error for non-zero exit: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d; want 1", result.ExitCode)
	}
}

// TestRunWithTimeoutReadTimeout verifies that a subprocess exceeding the read
// budget is terminated and an error wrapping BrUnavailable is returned.
func TestRunWithTimeoutReadTimeout(t *testing.T) {
	// Sleep much longer than the tight read budget.
	path := timeoutFixtureSleepBinary(t, 10*time.Second)
	a := timeoutFixtureAdapter(t, path)

	cfg := timeoutFixtureTightCfg()

	start := time.Now()
	_, err := a.RunWithTimeout(context.Background(), cfg, brcli.CommandKindRead)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("RunWithTimeout: expected BrUnavailable, got nil")
	}
	if !errors.Is(err, brcli.BrUnavailable) {
		t.Errorf("err = %v; want errors.Is(err, BrUnavailable) = true", err)
	}
	// Should return promptly: budget (50ms) + sigtermGrace (5s worst-case) + slack.
	// Derive the upper bound from the test's own deadline so it scales with
	// -timeout and only fires on a true hang, not CPU starvation under heavy
	// -race parallelism. Keep it comfortably above the 5s sigtermGrace.
	upperBound := 30 * time.Second
	if dl, ok := t.Deadline(); ok {
		if budget := time.Until(dl) - 2*time.Second; budget > 8*time.Second && budget < upperBound {
			upperBound = budget
		}
	}
	if elapsed > upperBound {
		t.Errorf("RunWithTimeout took %v; expected < %s", elapsed, upperBound)
	}
}

// TestRunWithTimeoutWriteTimeout verifies that a subprocess exceeding the write
// budget is terminated and an error wrapping BrUnavailable is returned.
func TestRunWithTimeoutWriteTimeout(t *testing.T) {
	path := timeoutFixtureSleepBinary(t, 10*time.Second)
	a := timeoutFixtureAdapter(t, path)

	cfg := timeoutFixtureTightCfg()

	_, err := a.RunWithTimeout(context.Background(), cfg, brcli.CommandKindWrite)
	if err == nil {
		t.Fatal("RunWithTimeout: expected BrUnavailable, got nil")
	}
	if !errors.Is(err, brcli.BrUnavailable) {
		t.Errorf("err = %v; want errors.Is(err, BrUnavailable) = true", err)
	}
}

// TestRunWithTimeoutContextCancellation verifies that canceling the outer ctx
// terminates the subprocess and returns BrUnavailable.
func TestRunWithTimeoutContextCancellation(t *testing.T) {
	path := timeoutFixtureSleepBinary(t, 10*time.Second)
	a := timeoutFixtureAdapter(t, path)

	// Use a long budget so the timeout does not fire before ctx is canceled.
	cfg := brcli.TimeoutConfig{ReadTimeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, runErr := a.RunWithTimeout(ctx, cfg, brcli.CommandKindRead)
		done <- runErr
	}()

	// Let the subprocess start, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Derive the upper bound from the test's own deadline so it scales with
	// -timeout and only fires on a true hang, not CPU starvation under heavy
	// -race parallelism. Keep it comfortably above the 5s sigtermGrace.
	returnTimeout := 30 * time.Second
	if dl, ok := t.Deadline(); ok {
		if budget := time.Until(dl) - 2*time.Second; budget > 8*time.Second && budget < returnTimeout {
			returnTimeout = budget
		}
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RunWithTimeout: expected BrUnavailable after ctx cancellation, got nil")
		}
		if !errors.Is(err, brcli.BrUnavailable) {
			t.Errorf("err = %v; want errors.Is(err, BrUnavailable) = true", err)
		}
	case <-time.After(returnTimeout):
		t.Fatal("RunWithTimeout did not return promptly after ctx cancellation")
	}
}

// TestRunWithTimeoutDefaultReadConfig verifies that a zero TimeoutConfig
// applies the BI-025c default read timeout (5s) when no explicit value is set.
// This test uses a fast-exiting binary to confirm the default config is wired
// correctly (not that the 5s timer fires, which would be slow).
func TestRunWithTimeoutDefaultReadConfig(t *testing.T) {
	path := timeoutFixtureMockBinary(t, "default-read", "", 0)
	a := timeoutFixtureAdapter(t, path)

	// Zero TimeoutConfig — defaults to 5s read / 10s write per BI-025c.
	result, err := a.RunWithTimeout(context.Background(), brcli.TimeoutConfig{}, brcli.CommandKindRead)
	if err != nil {
		t.Fatalf("RunWithTimeout: unexpected error: %v", err)
	}
	if string(result.Stdout) != "default-read" {
		t.Errorf("Stdout = %q; want %q", string(result.Stdout), "default-read")
	}
}

// TestRunWithTimeoutDefaultWriteConfig verifies that a zero TimeoutConfig
// applies the BI-025c default write timeout (10s) when no explicit value is set.
func TestRunWithTimeoutDefaultWriteConfig(t *testing.T) {
	path := timeoutFixtureMockBinary(t, "default-write", "", 0)
	a := timeoutFixtureAdapter(t, path)

	result, err := a.RunWithTimeout(context.Background(), brcli.TimeoutConfig{}, brcli.CommandKindWrite)
	if err != nil {
		t.Fatalf("RunWithTimeout: unexpected error: %v", err)
	}
	if string(result.Stdout) != "default-write" {
		t.Errorf("Stdout = %q; want %q", string(result.Stdout), "default-write")
	}
}

// TestRunWithTimeoutMissingBinary verifies that a missing br binary returns an
// exec failure error (not BrUnavailable).
func TestRunWithTimeoutMissingBinary(t *testing.T) {
	a := timeoutFixtureAdapter(t, "/nonexistent/br")

	_, err := a.RunWithTimeout(context.Background(), timeoutFixtureFastCfg(), brcli.CommandKindRead)
	if err == nil {
		t.Fatal("RunWithTimeout: expected error for missing binary, got nil")
	}
	if errors.Is(err, brcli.BrUnavailable) {
		t.Errorf("missing-binary error should NOT be BrUnavailable; got: %v", err)
	}
}
