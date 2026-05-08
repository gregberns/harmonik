package brcli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
)

// brcliFixtureMockBinary writes a shell script that prints stdout/stderr and
// exits with the given code. The returned path is valid for the duration of
// the test (t.TempDir is used for cleanup).
//
// The binary is written with mode 0755 for executability; the gosec G306
// finding is suppressed because this is a test fixture, not production data.
func brcliFixtureMockBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nprintf '%%s' %q >&2\nexit %d\n", stdout, stderr, exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("brcliFixtureMockBinary: write mock: %v", err)
	}
	return path
}

// brcliFixtureSleepBinary writes a shell script that sleeps for the given
// number of seconds then exits 0. Used for context-cancellation tests.
func brcliFixtureSleepBinary(t *testing.T, seconds int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nsleep %d\nexit 0\n", seconds)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("brcliFixtureSleepBinary: write mock: %v", err)
	}
	return path
}

func TestNewRejectsEmptyPath(t *testing.T) {
	adapter, err := brcli.New("")
	if err == nil {
		t.Fatal("expected error for empty brPath, got nil")
	}
	if adapter != nil {
		t.Fatal("expected nil Adapter on error, got non-nil")
	}
}

func TestNewAcceptsValidPath(t *testing.T) {
	adapter, err := brcli.New("/path/to/br")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil Adapter, got nil")
	}
}

func TestRunCapturesStdout(t *testing.T) {
	path := brcliFixtureMockBinary(t, "hello stdout", "", 0)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if string(result.Stdout) != "hello stdout" {
		t.Errorf("Stdout = %q; want %q", string(result.Stdout), "hello stdout")
	}
}

func TestRunCapturesStderr(t *testing.T) {
	path := brcliFixtureMockBinary(t, "", "hello stderr", 0)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if string(result.Stderr) != "hello stderr" {
		t.Errorf("Stderr = %q; want %q", string(result.Stderr), "hello stderr")
	}
}

func TestRunReportsExitCode(t *testing.T) {
	path := brcliFixtureMockBinary(t, "", "", 1)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: unexpected error for non-zero exit: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d; want 1", result.ExitCode)
	}
}

func TestRunReportsExitCodeZero(t *testing.T) {
	path := brcliFixtureMockBinary(t, "ok", "", 0)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d; want 0", result.ExitCode)
	}
}

func TestRunReturnsErrorOnMissingBinary(t *testing.T) {
	adapter, err := brcli.New("/nonexistent/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestRunPropagatesContextCancellation(t *testing.T) {
	path := brcliFixtureSleepBinary(t, 5)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, runErr := adapter.Run(ctx)
		done <- runErr
	}()

	// Cancel after a short delay to let the subprocess start.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after context cancellation")
	}
}
