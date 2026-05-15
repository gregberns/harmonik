package brcli_test

// unavailableretry_hkyjsk8_test.go — regression guard for hk-yjsk8.
//
// Bug: daemon's `br close` hits the 10s wall-clock timeout under SQLite
// contention; RunWithDBLockedRetry historically retried only BrDbLocked
// (exit 3) Result values and propagated BrUnavailable-wrapped errors from
// wall-clock timeouts immediately. The terminal-transition write therefore
// failed after a single 10s attempt even though one retry would have
// succeeded — the bead was left IN_PROGRESS while the commit had already
// landed on main.
//
// Fix: RunWithDBLockedRetry now also retries when RunWithTimeout returns an
// error wrapping BrUnavailable (wall-clock timeout). The retry uses the same
// backoff schedule as the existing BrDbLocked retry path. Idempotency is
// preserved by the BI-029/BI-030 intent-log protocol.

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

// hkyjsk8FixtureSleepThenSucceedBinary writes a shell script that sleeps long
// enough to trip a tight write timeout on the first failCount invocations,
// then exits 0 immediately on subsequent calls. A per-binary call counter is
// maintained on disk (one sentinel file per call).
//
// sleepDuration must exceed the test's TimeoutConfig.WriteTimeout so that the
// first failCount attempts produce a BrUnavailable-wrapped wall-clock-timeout
// error from RunWithTimeout.
func hkyjsk8FixtureSleepThenSucceedBinary(t *testing.T, sleepDuration time.Duration, failCount int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	countDir := filepath.Join(dir, "calls")
	if err := os.MkdirAll(countDir, 0o755); err != nil { //nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		t.Fatalf("hkyjsk8FixtureSleepThenSucceedBinary: mkdir: %v", err)
	}
	// Sleep on first failCount calls so the harness sees a wall-clock timeout
	// (BrUnavailable). Subsequent calls exit 0 immediately.
	script := fmt.Sprintf(`#!/bin/sh
count=$(ls %q 2>/dev/null | wc -l | tr -d ' ')
touch %q/"call_${count}"
if [ "$count" -lt "%d" ]; then
  sleep %.3f
fi
exit 0
`, countDir, countDir, failCount, sleepDuration.Seconds())
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("hkyjsk8FixtureSleepThenSucceedBinary: write: %v", err)
	}
	return path
}

// TestRunWithDBLockedRetryBrUnavailableRetriedHkyjsk8 is the regression
// guard for hk-yjsk8. It verifies that a wall-clock timeout
// (BrUnavailable-wrapped err) is retried with backoff and that the call
// succeeds on the second attempt — rather than escalating immediately, which
// was the pre-fix behaviour that left beads stuck IN_PROGRESS.
func TestRunWithDBLockedRetryBrUnavailableRetriedHkyjsk8(t *testing.T) {
	// First invocation: sleep 300ms (exceeds 100ms write timeout in tight cfg)
	// to trigger a BrUnavailable-wrapped wall-clock-timeout error.
	// Second invocation: exit 0 immediately.
	const failCount = 1
	const sleepDuration = 300 * time.Millisecond

	path := hkyjsk8FixtureSleepThenSucceedBinary(t, sleepDuration, failCount)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	// Tight write timeout (100ms) so the first attempt times out quickly.
	cfg := brcli.TimeoutConfig{
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	}

	result, retryErr := a.RunWithDBLockedRetry(
		context.Background(),
		cfg,
		brcli.CommandKindWrite,
		brcli.DBLockedRetryMax, // allow up to 3 retries
		1*time.Millisecond,     // fast backoff for test
		10*time.Millisecond,
	)
	if retryErr != nil {
		t.Fatalf("RunWithDBLockedRetry: expected success after BrUnavailable retry, got err: %v", retryErr)
	}
	if result.BrErr != brcli.BrOK {
		t.Errorf("BrErr = %v; want BrOK after BrUnavailable retry", result.BrErr)
	}
}

// TestRunWithDBLockedRetryBrUnavailableExhaustedHkyjsk8 verifies that when
// every attempt hits the wall-clock timeout (BrUnavailable), the call
// escalates to a BrUnavailable-wrapped error — same final classification as
// the pre-fix behaviour, but reached after the configured retry budget
// rather than after a single attempt.
func TestRunWithDBLockedRetryBrUnavailableExhaustedHkyjsk8(t *testing.T) {
	// Every invocation sleeps past the write timeout: all retries exhaust.
	const failCount = 999
	const sleepDuration = 300 * time.Millisecond

	path := hkyjsk8FixtureSleepThenSucceedBinary(t, sleepDuration, failCount)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	cfg := brcli.TimeoutConfig{
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	}

	_, retryErr := a.RunWithDBLockedRetry(
		context.Background(),
		cfg,
		brcli.CommandKindWrite,
		1, // maxRetries=1 keeps the test bounded (2 attempts × ~100ms each).
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
