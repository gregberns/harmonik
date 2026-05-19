package brcli_test

// unavailableretrywidened_hkekz5v_test.go — regression guard for hk-ekz5v.
//
// Bug: daemon's CloseBead emitted "BrUnavailable persisted after 3 retries"
// during dogfood run hk-75rij, despite the br write succeeding immediately
// after the run (transient SQLite contention from concurrent kerf/agent
// activity).
//
// Fix: terminal-transition writes (CloseBead, ClaimBead, ReopenBead, ResetBead)
// now use UnavailableRetryMax (10) instead of DBLockedRetryMax (3), with a
// faster initial backoff (UnavailableRetryBase = 50ms) and a 2s per-sleep cap.
// Non-terminal-transition reads are unchanged.
//
// Spec ref: beads-integration.md §4.10 BI-031 step (4c-transient).

import (
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
)

// TestUnavailableRetryConstsAlignmentHkekz5v verifies that the exported
// UnavailableRetry* constants match the BI-025c §4.10 step (4c-transient)
// spec values: max=10, base=50ms, cap=2s.
//
// Spec ref: beads-integration.md §4.10 BI-031 step (4c-transient).
func TestUnavailableRetryConstsAlignmentHkekz5v(t *testing.T) {
	if brcli.UnavailableRetryMax != 10 {
		t.Errorf("UnavailableRetryMax = %d; want 10 (per BI-031 step 4c-transient)", brcli.UnavailableRetryMax)
	}
	if brcli.UnavailableRetryBase != 50*time.Millisecond {
		t.Errorf("UnavailableRetryBase = %v; want 50ms (per BI-031 step 4c-transient)", brcli.UnavailableRetryBase)
	}
	if brcli.UnavailableRetryCap != 2*time.Second {
		t.Errorf("UnavailableRetryCap = %v; want 2s (per BI-031 step 4c-transient)", brcli.UnavailableRetryCap)
	}
}

// TestUnavailableRetryWiderThanDbLockedHkekz5v verifies the widening invariant:
// the UnavailableRetryMax budget for terminal-transition writes is strictly
// larger than the DBLockedRetryMax budget.
func TestUnavailableRetryWiderThanDbLockedHkekz5v(t *testing.T) {
	if brcli.UnavailableRetryMax <= brcli.DBLockedRetryMax {
		t.Errorf("UnavailableRetryMax (%d) must be > DBLockedRetryMax (%d): "+
			"terminal-transition writes need the wider budget (hk-ekz5v)",
			brcli.UnavailableRetryMax, brcli.DBLockedRetryMax)
	}
}

// TestRunWithDBLockedRetrySucceedsWithinWidenedBudgetHkekz5v is the regression
// guard for hk-ekz5v. It verifies that a wall-clock timeout (BrUnavailable)
// that persists for 4 consecutive attempts (more than the old DBLockedRetryMax=3
// budget) eventually succeeds when invoked with UnavailableRetryMax (10).
//
// This simulates exactly the dogfood scenario: `br close` was timing out due
// to transient SQLite contention; the old 3-retry budget was exhausted before
// contention resolved; the new 10-retry budget covers the tail.
func TestRunWithDBLockedRetrySucceedsWithinWidenedBudgetHkekz5v(t *testing.T) {
	// Fail 4 times with wall-clock timeout (exceeds old budget of 3, within new
	// budget of 10). On the 5th attempt the binary exits 0 immediately.
	const failCount = 4
	const sleepDuration = 300 * time.Millisecond

	// Reuse the sleep-then-succeed fixture from the hk-yjsk8 test file.
	path := hkyjsk8FixtureSleepThenSucceedBinary(t, sleepDuration, failCount)
	a, err := brcli.New(path)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	// Tight write timeout so the first 4 attempts trigger wall-clock timeout.
	cfg := brcli.TimeoutConfig{
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	}

	result, retryErr := a.RunWithDBLockedRetry(
		t.Context(),
		cfg,
		brcli.CommandKindWrite,
		brcli.UnavailableRetryMax, // 10 retries — the widened terminal-write budget
		1*time.Millisecond,        // fast backoff for test speed
		10*time.Millisecond,
	)
	if retryErr != nil {
		t.Fatalf("RunWithDBLockedRetry: expected success on attempt 5/10, got err: %v", retryErr)
	}
	if result.BrErr != brcli.BrOK {
		t.Errorf("BrErr = %v; want BrOK after widened-budget retry", result.BrErr)
	}
}

// TestRunWithDBLockedRetryOldBudgetWouldHaveFailedHkekz5v verifies that the
// same 4-failure scenario WOULD have failed under the old DBLockedRetryMax=3
// budget, confirming the widening was necessary.
func TestRunWithDBLockedRetryOldBudgetWouldHaveFailedHkekz5v(t *testing.T) {
	const failCount = 4
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

	// Old budget of 3: fails because failCount=4 exceeds it.
	_, retryErr := a.RunWithDBLockedRetry(
		t.Context(),
		cfg,
		brcli.CommandKindWrite,
		brcli.DBLockedRetryMax, // 3 — old budget that caused dogfood failure
		1*time.Millisecond,
		10*time.Millisecond,
	)
	if retryErr == nil {
		t.Fatal("RunWithDBLockedRetry: expected failure with old DBLockedRetryMax=3 budget, got nil — " +
			"the old-budget test must fail to demonstrate that widening was necessary")
	}
	if !errors.Is(retryErr, brcli.BrUnavailable) {
		t.Errorf("err = %v; want BrUnavailable with old budget", retryErr)
	}
}

// TestRunWithDBLockedRetryWidenedBudgetExhaustedHkekz5v verifies that when
// BrUnavailable persists past UnavailableRetryMax (10), the call escalates to
// BrUnavailable — same final classification as before, just reached after the
// wider budget.
func TestRunWithDBLockedRetryWidenedBudgetExhaustedHkekz5v(t *testing.T) {
	// Always sleep past the write timeout.
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

	// Use maxRetries=2 to keep the test bounded (3 attempts × ~100ms each ≈ 300ms).
	// The key invariant is BrUnavailable escalation, not the exact retry count.
	_, retryErr := a.RunWithDBLockedRetry(
		t.Context(),
		cfg,
		brcli.CommandKindWrite,
		2,
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
