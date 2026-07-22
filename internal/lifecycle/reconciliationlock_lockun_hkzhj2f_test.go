package lifecycle

import (
	"errors"
	"syscall"
	"testing"
)

// TestReconciliationLock_Release_UnlocksOpenFileDescription pins the hk-zhj2f
// fix: Release MUST issue flock(LOCK_UN), not merely close the fd.
//
// WHY A dup() AND NOT A REAL fork(): the production hazard is that fork()
// duplicates the lock fd into a child, and O_CLOEXEC drops that duplicate at
// EXEC rather than at FORK — so during a sibling's fork->exec window a second
// fd references the same OPEN FILE DESCRIPTION, and close() on the original
// releases nothing. Reproducing that with a real fork is inherently racy (the
// window is microseconds; measured at 18/1500 releases under heavy fork
// pressure), and a test that reproduces a defect only ~1% of the time is not a
// guard, it is a lottery. dup(2) produces the SAME condition — a second fd on
// the same open file description — deterministically and with no timing
// dependency at all. The property under test is identical: can the lock be
// re-acquired after Release while another fd still references its description.
//
// MUTATION ORACLE (the standard this test is written to satisfy): delete the
// `syscall.Flock(..., LOCK_UN)` line from ReconciliationLock.Release and this
// test MUST fail with ErrReconciliationLockHeld. If it still passes, the guard
// is vacuous and the test is worthless — re-run it that way before trusting it.
func TestReconciliationLock_Release_UnlocksOpenFileDescription(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	const runID = "hkzhj2f-lockun-target-run"

	lock, err := AcquireReconciliationLock(projectDir, runID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: %v", err)
	}

	// Duplicate the lock fd: a second descriptor on the SAME open file
	// description, exactly what a forked child holds before it execs. Held open
	// deliberately across Release so a close()-only release cannot succeed.
	dupFd, err := syscall.Dup(int(lock.fd.Fd()))
	if err != nil {
		t.Fatalf("dup lock fd: %v", err)
	}
	// errcheck runs with check-blank, so `_ =` is not an accepted discard here.
	// The close is genuinely best-effort — it only drops the extra descriptor the
	// test itself created — so report it rather than fail the test on it.
	defer func() {
		if closeErr := syscall.Close(dupFd); closeErr != nil {
			t.Logf("close dup lock fd: %v", closeErr)
		}
	}()

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// With LOCK_UN the description is unlocked regardless of the surviving
	// duplicate, so this acquire must succeed on the FIRST attempt — no retry
	// window, because there is no transient state left to wait out.
	lock2, err := AcquireReconciliationLock(projectDir, runID)
	if err != nil {
		if errors.Is(err, ErrReconciliationLockHeld) {
			t.Fatalf("RC-002a lock still held after Release while a duplicate fd " +
				"survives: Release closed the fd without issuing flock(LOCK_UN), so " +
				"the lock remains on the open file description (hk-zhj2f)")
		}
		t.Fatalf("re-acquire after Release: %v", err)
	}
	if err := lock2.Release(); err != nil {
		t.Errorf("Release lock2: %v", err)
	}
}

// TestReconciliationLock_Release_IdempotentWithUnlock guards the other half of
// the change: adding a syscall before the close must not break Release's
// documented idempotence, and must not turn a second call into a double-close
// or a flock on a closed descriptor.
func TestReconciliationLock_Release_IdempotentWithUnlock(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	const runID = "hkzhj2f-lockun-idempotent-run"

	lock, err := AcquireReconciliationLock(projectDir, runID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release must be a no-op, got: %v", err)
	}
}
