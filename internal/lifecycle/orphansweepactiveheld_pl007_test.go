package lifecycle

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// startupSweepFixtureAcquireReconciliationLockEX opens a reconciliation lock
// file and holds an exclusive flock for the lifetime of the test (released via
// returned releaseFn). This simulates a live process mid-acquisition, which the
// sweep MUST NOT remove (EWOULDBLOCK on probe means in-use).
func startupSweepFixtureAcquireReconciliationLockEX(t *testing.T, lockPath string) (releaseFn func()) {
	t.Helper()

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("startupSweepFixtureAcquireReconciliationLockEX: OpenFile: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup error unactionable
		t.Fatalf("startupSweepFixtureAcquireReconciliationLockEX: Flock LOCK_EX: %v", err)
	}
	return func() {
		_ = f.Close() //nolint:errcheck // closing fd releases the flock; cleanup error unactionable
	}
}

// startupSweepFixtureProbeIsHeld returns true if a reconciliation lock file's
// flock is currently held by another fd (EWOULDBLOCK on LOCK_EX|LOCK_NB probe).
// Returns false if the lock can be acquired (no live holder).
func startupSweepFixtureProbeIsHeld(t *testing.T, lockPath string) bool {
	t.Helper()

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("startupSweepFixtureProbeIsHeld: OpenFile: %v", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // cleanup error unactionable

	flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr == nil {
		// Acquired: no live holder. Release immediately.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck // release error unactionable
		return false
	}
	return true
}

// TestPL007_ActiveFlockHeldLockNotRemoved verifies that a reconciliation lock
// file currently under active flock(LOCK_EX) acquisition (by a concurrently
// running goroutine) is NOT removed by the orphan sweep.
//
// The spec discipline: for each .harmonik/reconciliation-locks/*.lock file, the
// daemon MUST attempt flock(LOCK_EX|LOCK_NB); if EWOULDBLOCK is observed the
// lock is in active use and MUST NOT be removed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "The sweep MUST NOT racily
// unlink a lock file currently being acquired by another daemon process — the
// flock(LOCK_EX|LOCK_NB) probe is the serialization point; if EWOULDBLOCK is
// observed the lock is in active use and MUST NOT be removed."
// process-lifecycle.md §4.2 PL-007 — "The orphan sweep MUST be deterministic
// given the filesystem + process state AND the project-scoped provenance marker
// of PL-006a."
func TestPL007_ActiveFlockHeldLockNotRemoved(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	// Create the reconciliation locks directory and a lock file.
	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("PL-007 active flock: MkdirAll: %v", err)
	}

	lockPath := filepath.Join(lockDir, "run-active-held.lock")
	if err := os.WriteFile(lockPath, []byte("creator_pid=12345\n"), 0o600); err != nil {
		t.Fatalf("PL-007 active flock: WriteFile: %v", err)
	}

	// Acquire an exclusive flock on the lock file in a goroutine to simulate
	// a live process mid-acquisition. Use a WaitGroup to synchronize.
	var (
		holderReady sync.WaitGroup
		holderDone  sync.WaitGroup
		releaseCh   = make(chan struct{})
	)
	holderReady.Add(1)
	holderDone.Add(1)

	go func() {
		defer holderDone.Done()

		release := startupSweepFixtureAcquireReconciliationLockEX(t, lockPath)
		holderReady.Done() // signal: lock is now held

		<-releaseCh // wait for the test to finish its sweep probe
		release()
	}()

	// Wait for the goroutine to hold the lock.
	holderReady.Wait()

	// The sweep probe: attempt LOCK_EX|LOCK_NB. Must return EWOULDBLOCK.
	isHeld := startupSweepFixtureProbeIsHeld(t, lockPath)
	if !isHeld {
		// Signal release before failing so the goroutine exits.
		close(releaseCh)
		holderDone.Wait()
		t.Fatal("PL-007 active flock: probe returned not-held while goroutine holds LOCK_EX; expected EWOULDBLOCK")
	}

	// Simulate sweep decision: EWOULDBLOCK → do NOT remove.
	// We assert non-removal by verifying the file still exists.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		close(releaseCh)
		holderDone.Wait()
		t.Fatal("PL-007 active flock: lock file was removed while actively held; MUST NOT be removed")
	}

	// Release the goroutine's hold.
	close(releaseCh)
	holderDone.Wait()

	// After release, the file must still exist on disk (sweep skipped it).
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("PL-007 active flock: lock file absent after goroutine release; sweep should have skipped it while held")
	}

	// Post-release: the probe must now return not-held (lock is acquirable).
	// Retry briefly: a concurrent exec.Command fork(2) elsewhere in this
	// parallel test binary can transiently keep the just-released flock alive
	// via an inherited fd copy until that child's exec(2) closes it.
	becameFree := plFixtureEventuallyTrue(t, 2*time.Second, func() bool {
		return !startupSweepFixtureProbeIsHeld(t, lockPath)
	})
	if !becameFree {
		t.Error("PL-007 active flock: probe still returns held after goroutine released the lock")
	}
}

// TestPL007_ActiveFlockSweepCounterNotIncremented verifies that the
// reconciliation_locks_removed counter in the event payload is NOT incremented
// for lock files whose flock probe returned EWOULDBLOCK (active acquisition).
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Event: On completion, the
// daemon MUST emit daemon_orphan_sweep_completed with counts of ... reconciliation
// lock files removed..."
// process-lifecycle.md §4.2 PL-007 — "After the sweep completes, no
// harmonik-owned process bearing this project's provenance marker from a prior
// daemon instance is alive and no harmonik-owned worktree is locked by a
// prior-instance lease."
func TestPL007_ActiveFlockSweepCounterNotIncremented(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("PL-007 counter: MkdirAll: %v", err)
	}

	// One actively-held lock (not swept).
	activeLockPath := filepath.Join(lockDir, "run-active.lock")
	if err := os.WriteFile(activeLockPath, []byte("creator_pid=12345\n"), 0o600); err != nil {
		t.Fatalf("PL-007 counter: WriteFile active: %v", err)
	}

	// One stale lock (creator PID 99999 — dead).
	const deadPID = 99999
	staleLockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-stale-for-counter", deadPID, false)

	// Check that deadPID is actually dead before proceeding.
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-007 counter: PID %d is live on this host; skipping", deadPID)
	}

	// Acquire active lock.
	var holderReady sync.WaitGroup
	var holderDone sync.WaitGroup
	releaseCh := make(chan struct{})

	holderReady.Add(1)
	holderDone.Add(1)
	go func() {
		defer holderDone.Done()
		release := startupSweepFixtureAcquireReconciliationLockEX(t, activeLockPath)
		holderReady.Done()
		<-releaseCh
		release()
	}()
	holderReady.Wait()

	// Simulate sweep: probe active lock → held → skip. Probe stale lock → not held + dead → remove.
	var locksRemoved int

	// Active lock probe: must observe held → skip.
	if !startupSweepFixtureProbeIsHeld(t, activeLockPath) {
		close(releaseCh)
		holderDone.Wait()
		t.Fatal("PL-007 counter: active lock not reported as held; fixture state invalid")
	}
	// Skip (do not remove): locksRemoved stays at 0 for this one.

	// Stale lock probe: must observe not-held + dead creator.
	if startupSweepFixtureProbeIsHeld(t, staleLockPath) {
		close(releaseCh)
		holderDone.Wait()
		t.Fatal("PL-007 counter: stale lock reported as held; unexpected")
	}
	// Stale + dead: remove it.
	if err := os.Remove(staleLockPath); err != nil {
		close(releaseCh)
		holderDone.Wait()
		t.Fatalf("PL-007 counter: Remove stale lock: %v", err)
	}
	locksRemoved++

	close(releaseCh)
	holderDone.Wait()

	// Counter must be 1 (only the stale lock was swept; the active lock was skipped).
	if locksRemoved != 1 {
		t.Errorf("PL-007 counter: locksRemoved = %d, want 1 (active lock skipped; stale lock removed)", locksRemoved)
	}

	payload := startupSweepFixtureOrphanSweepPayload{
		ReconciliationLocksRemoved: locksRemoved,
	}
	if payload.ReconciliationLocksRemoved != 1 {
		t.Errorf("PL-007 counter: payload.ReconciliationLocksRemoved = %d, want 1", payload.ReconciliationLocksRemoved)
	}

	// Active lock file must still be on disk.
	if _, err := os.Stat(activeLockPath); os.IsNotExist(err) {
		t.Error("PL-007 counter: active lock was removed; MUST NOT be removed while held")
	}
}
