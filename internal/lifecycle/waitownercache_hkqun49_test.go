package lifecycle

// waitownercache_hkqun49_test.go — regression tests for WaitOwner's promise that
// every caller of Wait observes the same subprocess exit error.
//
// Helper prefix: waitOwnerCacheFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// WaitOwner used to hand the exit error to callers over a buffered(1) channel
// that was written once and then closed. That delivers the error to its FIRST
// reader only; every later reader observes the closed-channel zero value, i.e.
// nil — a failed subprocess reported as a clean exit.
//
// The pre-existing suite missed it because no single test combined the two
// conditions needed to see it. Exposing the bug requires BOTH a non-zero-exiting
// child AND a third read, and the old tests each had only one:
//
//   - The broken code could serve the real error exactly TWICE: once as
//     WaitAndReap's own return value (a local variable assigned inside once.Do,
//     which never touched the channel) and once as the single buffered receive
//     in the first Wait. TestPL014_WaitOwner_NonZeroExitPreserved in
//     spawnwait_pl014_test.go did use a child exiting 1 — but it read the value
//     exactly twice, in exactly that order, so both reads were served and it
//     passed against the broken code.
//   - The tests that DID read further — MultipleWaiters (five concurrent Waits)
//     and WaitAndReapIdempotent (a second WaitAndReap) — used clean-exiting
//     children, for which nil is the right answer for every reader.
//
// So the bead's "existing tests only ever use clean-exiting children" is only
// half the reason: the non-zero test existed, it just never asked for a third
// delivery. These tests use a NON-ZERO exiting child AND read the result more
// times than the broken code could serve. Verified: all three fail against the
// pre-change file replayed with go test -overlay.
//
// Bead: hk-qun49.

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
)

// waitOwnerCacheFixtureExitCode is the exit status of the fixture child. It is
// deliberately neither 0 nor 1 so a lost error cannot be confused with a clean
// exit or with a generic failure from the shell itself.
//
// It must stay in sync with the literal in waitOwnerCacheFixtureShellCmd below.
// Both are kept as untyped constants rather than derived with fmt.Sprintf
// because gosec's G204 flags a non-constant exec argument, and a nolint for a
// cosmetic de-duplication is a worse trade than the coupling. Drift fails
// loudly: every assertion goes through waitOwnerCacheFixtureAssertExit, which
// compares the observed code against this constant.
const (
	waitOwnerCacheFixtureExitCode = 3
	waitOwnerCacheFixtureShellCmd = "exit 3"
)

// waitOwnerCacheFixtureStart starts a child that exits waitOwnerCacheFixtureExitCode
// and returns a WaitOwner wrapping it. The caller owns the reap.
func waitOwnerCacheFixtureStart(t *testing.T) *WaitOwner {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "sh", "-c", waitOwnerCacheFixtureShellCmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("waitOwnerCacheFixtureStart: cmd.Start: %v", err)
	}
	return NewWaitOwner(cmd)
}

// waitOwnerCacheFixtureAssertExit fails the test unless err is the fixture
// child's non-zero exit error.
func waitOwnerCacheFixtureAssertExit(t *testing.T, label string, err error) {
	t.Helper()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("%s: want *exec.ExitError, got %v", label, err)
		return
	}
	if got := exitErr.ExitCode(); got != waitOwnerCacheFixtureExitCode {
		t.Errorf("%s: exit code = %d, want %d", label, got, waitOwnerCacheFixtureExitCode)
	}
}

// TestPL014_WaitOwner_ExitErrorRepeatsForEverySequentialWait verifies that Wait
// re-delivers the cached exit error on every call, not just the first.
//
// Spec ref: process-lifecycle.md §4.5 PL-014; §4.6 PL-016 — single owner,
// multiple observers.
func TestPL014_WaitOwner_ExitErrorRepeatsForEverySequentialWait(t *testing.T) {
	t.Parallel()

	owner := waitOwnerCacheFixtureStart(t)

	waitOwnerCacheFixtureAssertExit(t, "WaitAndReap", owner.WaitAndReap())
	for i := 1; i <= 3; i++ {
		waitOwnerCacheFixtureAssertExit(t, fmt.Sprintf("Wait call %d", i), owner.Wait())
	}
}

// TestPL014_WaitOwner_ExitErrorReachesEveryConcurrentWaiter verifies that N
// goroutines blocked in Wait before the reap all receive the exit error.
//
// Spec ref: process-lifecycle.md §4.6 PL-016 — observers receive the owner's
// exit status.
func TestPL014_WaitOwner_ExitErrorReachesEveryConcurrentWaiter(t *testing.T) {
	t.Parallel()

	owner := waitOwnerCacheFixtureStart(t)

	const waiters = 5
	var wg sync.WaitGroup
	errs := make([]error, waiters)
	for i := range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = owner.Wait()
		}()
	}

	waitOwnerCacheFixtureAssertExit(t, "WaitAndReap", owner.WaitAndReap())
	wg.Wait()

	for i, err := range errs {
		waitOwnerCacheFixtureAssertExit(t, fmt.Sprintf("concurrent waiter %d", i), err)
	}
}

// TestPL014_WaitOwner_SecondWaitAndReapReturnsCachedExitError verifies that the
// idempotent second WaitAndReap returns the memoized exit error rather than the
// zero value, matching its documented contract.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — cmd.Wait() exactly once.
func TestPL014_WaitOwner_SecondWaitAndReapReturnsCachedExitError(t *testing.T) {
	t.Parallel()

	owner := waitOwnerCacheFixtureStart(t)

	waitOwnerCacheFixtureAssertExit(t, "first WaitAndReap", owner.WaitAndReap())
	waitOwnerCacheFixtureAssertExit(t, "second WaitAndReap", owner.WaitAndReap())
}
