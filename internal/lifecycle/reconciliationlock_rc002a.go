package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
)

// reconciliationlock_rc002a.go — per-run reconciliation lock primitive.
//
// RC-002a: for any given target_run_id, at most ONE reconciliation workflow may
// be in-flight at a time. The daemon acquires this lock before emitting the
// first reconciliation event (RC-013 category-assigned) and holds it until one
// of the terminal states listed in RC-002a fires.
//
// The lock is an advisory flock(LOCK_EX|LOCK_NB) on the file:
//
//	.harmonik/reconciliation-locks/<target_run_id>.lock
//
// On EWOULDBLOCK the caller MUST emit reconciliation_dispatch_deduplicated and
// skip dispatch. The kernel releases the lock automatically on process
// termination; the orphan sweep (PL-006) removes stale files on startup.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a.

// ErrReconciliationLockHeld is returned by AcquireReconciliationLock when
// flock(LOCK_EX|LOCK_NB) returns EWOULDBLOCK or EAGAIN — meaning another
// reconciliation workflow is already in-flight for the same target_run_id.
//
// The caller MUST emit reconciliation_dispatch_deduplicated (EV §8.6.11) and
// skip dispatch without re-classification.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a.
var ErrReconciliationLockHeld = errors.New("lifecycle: reconciliation lock is already held for this target run")

// ReconciliationLock is an acquired per-run reconciliation lock. The underlying
// fd is kept open for the lock's lifetime (fd-lifetime advisory lock per
// PL-002a discipline). Callers MUST call Release when one of the RC-002a
// terminal states fires.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a;
// specs/process-lifecycle.md §4.1 PL-002a.
type ReconciliationLock struct {
	targetRunID string
	lockPath    string
	fd          *os.File
	mu          sync.Mutex // guards fd for idempotent Release
}

// TargetRunID returns the run ID for which this lock was acquired.
func (l *ReconciliationLock) TargetRunID() string {
	return l.targetRunID
}

// LockPath returns the absolute path of the lock file on disk.
func (l *ReconciliationLock) LockPath() string {
	return l.lockPath
}

// WriteVerdictExecuted appends "Harmonik-Verdict-Executed: true\n" to the lock
// file and calls Sync. This MUST be called by the verdict-executor just before
// releasing the lock (i.e., before the verdict-executed commit is pushed to git
// and before Release is called).
//
// Writing this line is the physical write that pairs with the verdict-executed
// git commit. Because the two writes are NOT atomic (RC-002b), the startup
// sweep (PL-006) reads this line to discriminate: lock-with-trailer → lock
// outlived its purpose → delete; lock-without-trailer → route to Cat 3b.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b — "Lock acquisition
// (RC-002a) and the verdict-executed-commit emission (RC-025 + schemas.md §6.4)
// are two physically distinct write operations and CANNOT be made atomic."
func (l *ReconciliationLock) WriteVerdictExecuted() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fd == nil {
		return fmt.Errorf("lifecycle: WriteVerdictExecuted: lock already released")
	}
	const line = "Harmonik-Verdict-Executed: true\n"
	if _, err := fmt.Fprint(l.fd, line); err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictExecuted: write: %w", err)
	}
	if err := l.fd.Sync(); err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictExecuted: sync: %w", err)
	}
	return nil
}

// Release closes the underlying fd, which also releases the advisory flock
// per PL-002a (fd-lifetime lock). Release is idempotent: a second call returns
// nil without a double-close.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a — lock MUST be released
// on verdict-executed commit, budget exhaustion, malformed verdict,
// investigator-process crash, or operator pause (ON-027 entry).
func (l *ReconciliationLock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fd == nil {
		return nil
	}
	fd := l.fd
	l.fd = nil
	return fd.Close()
}

// AcquireReconciliationLock opens (or creates) the per-run reconciliation lock
// file at .harmonik/reconciliation-locks/<targetRunID>.lock, acquires an
// exclusive non-blocking advisory flock (LOCK_EX|LOCK_NB), writes
// creator_pid and run_id metadata to the file, and returns a *ReconciliationLock
// whose fd is kept open for the lock's lifetime.
//
// On EWOULDBLOCK (lock already held by another reconciliation workflow for this
// target run): closes the fd and returns ErrReconciliationLockHeld. The caller
// MUST emit reconciliation_dispatch_deduplicated (EV §8.6.11) and skip dispatch.
//
// On any other flock error: closes the fd and returns a wrapped error.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002a.
func AcquireReconciliationLock(projectDir, targetRunID string) (*ReconciliationLock, error) {
	lockDir := ReconciliationLocksDir(projectDir)
	//nolint:gosec // G301: 0755 matches .harmonik/ subdir conventions throughout lifecycle package
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("lifecycle: AcquireReconciliationLock: MkdirAll %q: %w", lockDir, err)
	}

	lockPath := ReconciliationLockPath(projectDir, targetRunID)

	//nolint:gosec // G304: lockPath is constructed from projectDir + .harmonik/reconciliation-locks/ + targetRunID + ".lock", not user input
	fd, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: AcquireReconciliationLock: open %q: %w", lockPath, err)
	}

	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup error unactionable; primary error takes precedence
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrReconciliationLockHeld
		}
		return nil, fmt.Errorf("lifecycle: AcquireReconciliationLock: flock %q: %w", lockPath, err)
	}

	// Write metadata after acquiring the lock (truncate-rewrite pattern per PL-002b discipline).
	if err := fd.Truncate(0); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup error unactionable; primary error takes precedence
		return nil, fmt.Errorf("lifecycle: AcquireReconciliationLock: truncate %q: %w", lockPath, err)
	}
	if _, err := fd.Seek(0, 0); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup error unactionable; primary error takes precedence
		return nil, fmt.Errorf("lifecycle: AcquireReconciliationLock: seek %q: %w", lockPath, err)
	}

	content := fmt.Sprintf("creator_pid=%d\nrun_id=%s\n", os.Getpid(), targetRunID)
	if err := writeAll(fd, []byte(content)); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup error unactionable; primary error takes precedence
		return nil, fmt.Errorf("lifecycle: AcquireReconciliationLock: write %q: %w", lockPath, err)
	}

	return &ReconciliationLock{
		targetRunID: targetRunID,
		lockPath:    lockPath,
		fd:          fd,
	}, nil
}
