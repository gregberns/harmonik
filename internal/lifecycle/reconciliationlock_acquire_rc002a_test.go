package lifecycle

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestAcquireReconciliationLock_SuccessAndRelease verifies that
// AcquireReconciliationLock succeeds for a fresh target_run_id and that
// Release frees the fd so a subsequent acquire for the same run succeeds.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a.
// Bead ref: hk-63oh.4.
func TestAcquireReconciliationLock_SuccessAndRelease(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const targetRunID = "run-acquire-release-test"

	lock, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: unexpected error: %v", err)
	}
	if lock == nil {
		t.Fatal("AcquireReconciliationLock: returned nil lock")
	}
	if lock.TargetRunID() != targetRunID {
		t.Errorf("TargetRunID() = %q, want %q", lock.TargetRunID(), targetRunID)
	}
	if !strings.Contains(lock.LockPath(), ".harmonik/reconciliation-locks/") {
		t.Errorf("LockPath() = %q; want path containing .harmonik/reconciliation-locks/", lock.LockPath())
	}

	// Release and verify idempotent second call.
	if err := lock.Release(); err != nil {
		t.Errorf("Release: unexpected error: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Errorf("Release (second call): unexpected error: %v (want idempotent nil)", err)
	}

	// After release, a new acquire for the same run ID must succeed.
	lock2, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock after Release: unexpected error: %v", err)
	}
	_ = lock2.Release()
}

// TestAcquireReconciliationLock_ErrLockHeld verifies that a second acquire
// for the same target_run_id returns ErrReconciliationLockHeld while the first
// lock is still held.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a — "A second reconciliation
// dispatch targeting the same target_run_id MUST attempt flock(LOCK_EX|LOCK_NB);
// on EWOULDBLOCK, the second dispatch MUST emit reconciliation_dispatch_deduplicated
// and skip without re-classification."
// Bead ref: hk-63oh.4.
func TestAcquireReconciliationLock_ErrLockHeld(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const targetRunID = "run-ewouldblock-test"

	lock1, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("first AcquireReconciliationLock: unexpected error: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Second acquire must return ErrReconciliationLockHeld.
	_, err = AcquireReconciliationLock(projectDir, targetRunID)
	if !errors.Is(err, ErrReconciliationLockHeld) {
		t.Errorf("second AcquireReconciliationLock: got %v, want ErrReconciliationLockHeld", err)
	}
}

// TestAcquireReconciliationLock_DifferentRunIDsAreIndependent verifies that
// locks for different target_run_ids are independent: holding lock A does not
// prevent acquiring lock B.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a.
// Bead ref: hk-63oh.4.
func TestAcquireReconciliationLock_DifferentRunIDsAreIndependent(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	lockA, err := AcquireReconciliationLock(projectDir, "run-indep-A")
	if err != nil {
		t.Fatalf("AcquireReconciliationLock A: %v", err)
	}
	defer func() { _ = lockA.Release() }()

	lockB, err := AcquireReconciliationLock(projectDir, "run-indep-B")
	if err != nil {
		t.Fatalf("AcquireReconciliationLock B while A is held: %v", err)
	}
	_ = lockB.Release()
}

// TestAcquireReconciliationLock_MetadataWritten verifies that after a
// successful acquire the lock file contains creator_pid and run_id lines
// matching the calling process and target_run_id.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a — lock file content
// mirrors the pidfile discipline: creator_pid=<int>, run_id=<str>.
// Bead ref: hk-63oh.4.
func TestAcquireReconciliationLock_MetadataWritten(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const targetRunID = "run-metadata-test"

	lock, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	//nolint:gosec // G304: lock path constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(lock.LockPath())
	if err != nil {
		t.Fatalf("ReadFile lock: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "run_id="+targetRunID) {
		t.Errorf("lock file does not contain run_id=%s; content: %q", targetRunID, content)
	}
	if !strings.Contains(content, "creator_pid=") {
		t.Errorf("lock file does not contain creator_pid= line; content: %q", content)
	}
}

// ---- RC-002b: WriteVerdictExecuted tests ----

// TestWriteVerdictExecuted_AppendsTrailerAndSyncs verifies that
// WriteVerdictExecuted appends "Harmonik-Verdict-Executed: true" to the lock
// file and that the content is readable after the call.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b — "verdict-executor writes
// Harmonik-Verdict-Executed: true to the lock file just before releasing the lock."
// Bead ref: hk-63oh.5.
func TestWriteVerdictExecuted_AppendsTrailerAndSyncs(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const targetRunID = "run-write-verdict-test"

	lock, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	if err := lock.WriteVerdictExecuted(); err != nil {
		t.Fatalf("WriteVerdictExecuted: unexpected error: %v", err)
	}

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(lock.LockPath())
	if err != nil {
		t.Fatalf("ReadFile lock: %v", err)
	}
	const wantTrailer = "Harmonik-Verdict-Executed: true"
	if !strings.Contains(string(data), wantTrailer) {
		t.Errorf("WriteVerdictExecuted: lock file does not contain %q after write; content: %q",
			wantTrailer, string(data))
	}
}

// TestWriteVerdictExecuted_AfterReleaseFails verifies that WriteVerdictExecuted
// returns an error when called after Release (the fd is closed).
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b — write must be before Release.
// Bead ref: hk-63oh.5.
func TestWriteVerdictExecuted_AfterReleaseFails(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const targetRunID = "run-write-verdict-after-release"

	lock, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if err := lock.WriteVerdictExecuted(); err == nil {
		t.Error("WriteVerdictExecuted after Release: expected error, got nil")
	}
}

// TestWriteVerdictExecuted_LockFileContentReadableAfterRelease verifies that the
// verdict-executed trailer written by WriteVerdictExecuted persists in the lock
// file after Release (fd closed). This is the artifact the PL-006 sweep reads on
// next-daemon-startup to discriminate between "lock outlived purpose" (has trailer)
// vs. "verdict not executed" (no trailer) per RC-002b.
//
// Note: the startup sweep's staleness criterion requires the creator PID to be
// dead (cross-daemon-invocation boundary). The sweep behavior with a dead creator
// PID is covered by TestRC002b_SweepWithVerdictExecutedNoCat3b using the
// startupSweepFixtureSeedReconciliationLock fixture.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b.
// Bead ref: hk-63oh.5.
func TestWriteVerdictExecuted_LockFileContentReadableAfterRelease(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const targetRunID = "run-e2e-content-check"

	// Simulate verdict-executor: acquire → write verdict-executed → release.
	lock, err := AcquireReconciliationLock(projectDir, targetRunID)
	if err != nil {
		t.Fatalf("AcquireReconciliationLock: %v", err)
	}
	if err := lock.WriteVerdictExecuted(); err != nil {
		t.Fatalf("WriteVerdictExecuted: %v", err)
	}
	lockPath := lock.LockPath()
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Lock file must still exist on disk after Release.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock file absent after Release; WriteVerdictExecuted must leave the file on disk")
	}

	// The verdict-executed trailer must be readable from the file.
	//nolint:gosec // G304: path constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile after Release: %v", err)
	}
	const wantTrailer = "Harmonik-Verdict-Executed: true"
	if !strings.Contains(string(data), wantTrailer) {
		t.Errorf("lock file after Release does not contain trailer %q; content: %q", wantTrailer, string(data))
	}
}
