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

// dblockretryFixtureCountedBinary writes a shell script that returns
// exitCode for the first n calls, then exitSuccessCode thereafter.
// A shared atomic counter file at counterPath is used to track invocations
// so that the mock binary can distinguish attempts without any in-process
// state.
//
// The binary uses a lock-free counter via the filesystem: it atomically
// increments a numeric counter in counterPath to determine which attempt
// this is. Because shell scripts cannot use OS atomics, we use a simpler
// approach: the binary increments an int written to counterPath.
//
// NOTE: to keep the fixture simple and the test deterministic, the counter
// file approach is replaced with a simpler "N-exit-then-success" pattern
// driven by a temp directory: one file is created per call; when the file
// count exceeds failCount the script exits 0.
func dblockretryFixtureCountedBinary(t *testing.T, failExitCode int, failCount int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	countDir := filepath.Join(dir, "calls")
	if err := os.MkdirAll(countDir, 0o755); err != nil {
		t.Fatalf("dblockretryFixtureCountedBinary: mkdir: %v", err)
	}
	// The script creates a new file per call; counts existing files to decide exit.
	script := fmt.Sprintf(`#!/bin/sh
count=$(ls %q 2>/dev/null | wc -l | tr -d ' ')
touch %q/"call_${count}"
if [ "$count" -lt "%d" ]; then
  exit %d
fi
exit 0
`, countDir, countDir, failCount, failExitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("dblockretryFixtureCountedBinary: write: %v", err)
	}
	return path
}

// dblockretryFixtureMockBinary is a simple mock that always exits with the
// given exit code. Reuses the brcliFixtureMockBinary helper from adapter_test.go
// via a local thin wrapper (separate prefix; no collision).
func dblockretryFixtureMockBinary(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("dblockretryFixtureMockBinary: write: %v", err)
	}
	return path
}

// dblockretryFixtureAtomicCountedBinary creates a mock `br` binary that uses
// an in-process atomic counter (via a shared file) to fail exactly failCount
// times with failExitCode before succeeding. It uses a temp counter file and
// flock-style replacement to stay deterministic even under concurrency.
//
// Because shell-based counters are inherently racy, we instead generate N
// separate mock binaries that fail on specific invocations using a pre-seeded
// temp directory approach: each call touches a sentinel file; the count of
// existing sentinels determines whether to fail or succeed.
//
// For simplicity we use an atomic int64 in the test process and a fresh
// binary per attempt — but since each RunWithDBLockedRetry call uses the
// SAME binary path, we use the filesystem counter approach.
func dblockretryFixtureCountedAdapter(t *testing.T, failExitCode int, failCount int) *brcli.Adapter {
	t.Helper()
	path := dblockretryFixtureCountedBinary(t, failExitCode, failCount)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("dblockretryFixtureCountedAdapter: New: %v", err)
	}
	return a
}

// dblockretryFixtureFastCfg returns a fast TimeoutConfig for retry tests.
func dblockretryFixtureFastCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// --- Tests ---

// TestRunWithDBLockedRetrySuccessOnFirstAttempt verifies that a binary that
// exits 0 immediately is returned without retrying.
func TestRunWithDBLockedRetrySuccessOnFirstAttempt(t *testing.T) {
	path := dblockretryFixtureMockBinary(t, 0)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := a.RunWithDBLockedRetry(
		context.Background(),
		dblockretryFixtureFastCfg(),
		brcli.CommandKindRead,
		brcli.DBLockedRetryMax,
		brcli.DBLockedRetryBase,
		brcli.DBLockedRetryCap,
	)
	if err != nil {
		t.Fatalf("RunWithDBLockedRetry: unexpected error: %v", err)
	}
	if result.BrErr != brcli.BrOK {
		t.Errorf("BrErr = %v; want BrOK", result.BrErr)
	}
}

// TestRunWithDBLockedRetryNonDbLockedErrorPassthrough verifies that a
// non-BrDbLocked non-zero exit (e.g. BrNotFound) is returned immediately
// without retrying — retry is only for BrDbLocked (exit 3).
func TestRunWithDBLockedRetryNonDbLockedErrorPassthrough(t *testing.T) {
	// Exit 1 = BrNotFound — should be returned immediately without retry.
	path := dblockretryFixtureMockBinary(t, 1)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := a.RunWithDBLockedRetry(
		context.Background(),
		dblockretryFixtureFastCfg(),
		brcli.CommandKindRead,
		brcli.DBLockedRetryMax,
		brcli.DBLockedRetryBase,
		brcli.DBLockedRetryCap,
	)
	if err != nil {
		t.Fatalf("RunWithDBLockedRetry: unexpected error: %v", err)
	}
	if result.BrErr != brcli.BrNotFound {
		t.Errorf("BrErr = %v; want BrNotFound (passthrough without retry)", result.BrErr)
	}
}

// TestRunWithDBLockedRetrySuccessAfterRetries verifies that a binary that
// returns BrDbLocked (exit 3) for the first N calls then exits 0 is
// eventually returned successfully.
func TestRunWithDBLockedRetrySuccessAfterRetries(t *testing.T) {
	// Fail twice with exit 3 (BrDbLocked), succeed on the third attempt.
	const failCount = 2
	a := dblockretryFixtureCountedAdapter(t, 3, failCount)

	result, err := a.RunWithDBLockedRetry(
		context.Background(),
		dblockretryFixtureFastCfg(),
		brcli.CommandKindWrite,
		brcli.DBLockedRetryMax, // 3 retries; failCount=2 means 3rd attempt (attempt index 2) succeeds
		1*time.Millisecond,     // very short base to keep the test fast
		10*time.Millisecond,    // very short cap
	)
	if err != nil {
		t.Fatalf("RunWithDBLockedRetry: unexpected error after successful retry: %v", err)
	}
	if result.BrErr != brcli.BrOK {
		t.Errorf("BrErr = %v; want BrOK after retry", result.BrErr)
	}
}

// TestRunWithDBLockedRetryExhaustedReturnsUnavailable verifies that when all
// retry attempts produce BrDbLocked, the call escalates to BrUnavailable per
// BI-025c step 4c.
func TestRunWithDBLockedRetryExhaustedReturnsUnavailable(t *testing.T) {
	// Always exit 3 (BrDbLocked): more failures than maxRetries.
	path := dblockretryFixtureMockBinary(t, 3)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, retryErr := a.RunWithDBLockedRetry(
		context.Background(),
		dblockretryFixtureFastCfg(),
		brcli.CommandKindWrite,
		brcli.DBLockedRetryMax, // 3 retries; all fail
		1*time.Millisecond,
		10*time.Millisecond,
	)
	if retryErr == nil {
		t.Fatal("RunWithDBLockedRetry: expected BrUnavailable after exhausted retries, got nil")
	}
	if !errors.Is(retryErr, brcli.BrUnavailable) {
		t.Errorf("err = %v; want errors.Is(err, BrUnavailable) = true", retryErr)
	}
}

// TestRunWithDBLockedRetryZeroMaxRetries verifies that maxRetries=0 means one
// attempt only: a BrDbLocked on the first (only) attempt immediately escalates
// to BrUnavailable.
func TestRunWithDBLockedRetryZeroMaxRetries(t *testing.T) {
	path := dblockretryFixtureMockBinary(t, 3)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, retryErr := a.RunWithDBLockedRetry(
		context.Background(),
		dblockretryFixtureFastCfg(),
		brcli.CommandKindRead,
		0, // zero retries: first attempt is the only attempt
		1*time.Millisecond,
		10*time.Millisecond,
	)
	if retryErr == nil {
		t.Fatal("RunWithDBLockedRetry: expected BrUnavailable for maxRetries=0, got nil")
	}
	if !errors.Is(retryErr, brcli.BrUnavailable) {
		t.Errorf("err = %v; want errors.Is(err, BrUnavailable) = true", retryErr)
	}
}

// TestRunWithDBLockedRetryContextCanceledDuringBackoff verifies that if the
// context is canceled during the backoff sleep, RunWithDBLockedRetry returns
// promptly with an error (either context.Canceled or BrUnavailable, depending
// on whether cancellation fires during the subprocess or the sleep).
//
// The key invariant is that the function does NOT hang until the full
// DBLockedRetryMax * longBackoff duration has elapsed.
func TestRunWithDBLockedRetryContextCanceledDuringBackoff(t *testing.T) {
	// Always exit 3 so retry always fires and hits the backoff sleep.
	path := dblockretryFixtureMockBinary(t, 3)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Use a long backoff so the test can cancel before all retries complete.
	const longBackoff = 10 * time.Second

	done := make(chan error, 1)
	go func() {
		_, runErr := a.RunWithDBLockedRetry(
			ctx,
			dblockretryFixtureFastCfg(),
			brcli.CommandKindWrite,
			brcli.DBLockedRetryMax,
			longBackoff,
			longBackoff,
		)
		done <- runErr
	}()

	// Let the first attempt run (it will fail with BrDbLocked and enter backoff).
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case retryErr := <-done:
		if retryErr == nil {
			t.Fatal("RunWithDBLockedRetry: expected error after ctx cancellation, got nil")
		}
		// Accept either context.Canceled (cancel during backoff sleep) or
		// BrUnavailable (cancel during RunWithTimeout subprocess kill path).
		// Both are correct; the invariant is prompt return, not exact error type.
		if !errors.Is(retryErr, context.Canceled) && !errors.Is(retryErr, brcli.BrUnavailable) {
			t.Errorf("err = %v; want context.Canceled or BrUnavailable after ctx cancel", retryErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithDBLockedRetry did not return promptly after ctx cancellation")
	}
}

// TestRunWithDBLockedRetryDefaultConstsAlignment verifies that the exported
// default constants match the BI-025c spec values: max=3, base=100ms, cap=1s.
//
// Spec ref: specs/beads-integration.md §4.8a BI-025c (step 4c).
func TestRunWithDBLockedRetryDefaultConstsAlignment(t *testing.T) {
	if brcli.DBLockedRetryMax != 3 {
		t.Errorf("DBLockedRetryMax = %d; want 3 (per BI-025c step 4c)", brcli.DBLockedRetryMax)
	}
	if brcli.DBLockedRetryBase != 100*time.Millisecond {
		t.Errorf("DBLockedRetryBase = %v; want 100ms (per BI-025c step 4c)", brcli.DBLockedRetryBase)
	}
	if brcli.DBLockedRetryCap != 1*time.Second {
		t.Errorf("DBLockedRetryCap = %v; want 1s (per BI-025c step 4c)", brcli.DBLockedRetryCap)
	}
}
