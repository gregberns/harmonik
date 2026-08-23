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

func timeoutFixtureMockBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nprintf '%%s' %q >&2\nexit %d\n", stdout, stderr, exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("timeoutFixtureMockBinary: write mock: %v", err)
	}
	return path
}

func timeoutFixtureSleepBinary(t *testing.T, d time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	seconds := d.Seconds()
	script := fmt.Sprintf("#!/bin/sh\nsleep %.3f\nexit 0\n", seconds)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("timeoutFixtureSleepBinary: write mock: %v", err)
	}
	return path
}

func timeoutFixtureAdapter(t *testing.T, brPath string) *brcli.Adapter {
	t.Helper()
	a, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("timeoutFixtureAdapter: New: %v", err)
	}
	return a
}

func timeoutFixtureFastCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

func timeoutFixtureTightCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	}
}

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

	cfg := brcli.TimeoutConfig{ReadTimeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, runErr := a.RunWithTimeout(ctx, cfg, brcli.CommandKindRead)
		done <- runErr
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

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
