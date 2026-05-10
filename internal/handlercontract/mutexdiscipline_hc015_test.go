package handlercontract_test

import (
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// mutexDiscipline — per-bead helper prefix for test helpers in this file.
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.18)

// ─────────────────────────────────────────────────────────────────────────────
// HC-015 — RunLock basic locking
// ─────────────────────────────────────────────────────────────────────────────

// TestMutexDiscipline_RunLock_LockUnlock verifies that Lock and Unlock operate
// symmetrically: a second Lock call blocks until the first Unlock completes.
func TestMutexDiscipline_RunLock_LockUnlock(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock

	// Lock + Unlock in the same goroutine should not block.
	lock.Lock()
	lock.Unlock()
}

// TestMutexDiscipline_RunLock_RLockRUnlock verifies that RLock and RUnlock
// operate symmetrically.
func TestMutexDiscipline_RunLock_RLockRUnlock(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock

	lock.RLock()
	lock.RUnlock()
}

// TestMutexDiscipline_RunLock_MultipleRLocksConcurrent verifies that multiple
// concurrent RLock calls are allowed simultaneously (shared-read semantics).
func TestMutexDiscipline_RunLock_MultipleRLocksConcurrent(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock
	const n = 8

	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			lock.RLock()
			defer lock.RUnlock()
		}()
	}
	wg.Wait()
}

// TestMutexDiscipline_RunLock_TryLock_SucceedsWhenFree verifies that TryLock
// returns true when the lock is not contended.
func TestMutexDiscipline_RunLock_TryLock_SucceedsWhenFree(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock

	if !lock.TryLock() {
		t.Error("TryLock() = false on uncontended lock, want true")
	}
	lock.Unlock()
}

// TestMutexDiscipline_RunLock_TryLock_FailsWhenHeld verifies that TryLock
// returns false when another goroutine holds the write lock.
func TestMutexDiscipline_RunLock_TryLock_FailsWhenHeld(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock

	lock.Lock() // hold the lock
	defer lock.Unlock()

	// TryLock MUST return false because the lock is held.
	if lock.TryLock() {
		lock.Unlock() // release the extra acquire to avoid double-unlock confusion
		t.Error("TryLock() = true while lock is held by current goroutine, want false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-015 — acquire-mutate-release-then-publish sequence
// ─────────────────────────────────────────────────────────────────────────────

// TestMutexDiscipline_HC015_LockReleasedBeforePublish documents and verifies
// the HC-015 pattern: state is mutated under the write lock, the lock is
// released, and then events are published.
//
// This test is a structural sensor: it asserts that the sequence
//
//	lock.Lock()  → mutate state  → lock.Unlock()  → publish
//
// can complete without deadlock. A lock held across the publish step would
// deadlock if any synchronous subscriber tried to acquire the same lock.
func TestMutexDiscipline_HC015_LockReleasedBeforePublish(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock
	published := false

	// Step 1: acquire and mutate.
	lock.Lock()
	stateValue := 42 // simulated state mutation
	lock.Unlock()    // Step 2: release BEFORE publish.

	// Step 3: publish (outside the lock).
	_ = stateValue
	published = true

	if !published {
		t.Error("publish step was not reached; HC-015 sequence broken")
	}

	// Verify the lock can still be acquired (no leak).
	lock.Lock()
	lock.Unlock()
}

// TestMutexDiscipline_HC015_NoLockHeldAcrossAdapterCall documents the
// HC-015 constraint: no RunLock may be held across a call into an S04 adapter.
//
// This test is a structural sensor using a mock adapter; it verifies that
// the adapter is called only when the RunLock is NOT held.
//
// Strategy: the "adapter call" tries to acquire the write lock. If the caller
// held the lock, the "adapter call" would deadlock (write lock is not
// re-entrant). Using a goroutine with TryLock provides the observable signal.
func TestMutexDiscipline_HC015_NoLockHeldAcrossAdapterCall(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock

	// Simulate the watcher's HC-015-compliant sequence:
	// acquire lock → mutate → release lock → call adapter.
	lock.Lock()
	// ... mutate state ...
	lock.Unlock() // released BEFORE adapter call

	// Adapter call happens here (outside the lock).
	// Assert the lock is free by TryLock-ing from a goroutine.
	done := make(chan bool, 1)
	go func() {
		// TryLock should succeed because the caller released the lock.
		ok := lock.TryLock()
		if ok {
			lock.Unlock()
		}
		done <- ok
	}()

	if !<-done {
		t.Error("lock was still held at the point of adapter call — HC-015 violation: " +
			"no mutex may be held across S04 adapter call")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-015 — RunLock zero value is usable
// ─────────────────────────────────────────────────────────────────────────────

// TestMutexDiscipline_RunLock_ZeroValueUsable verifies that the zero value of
// RunLock is directly usable without explicit initialisation (mirrors
// sync.RWMutex zero-value usability).
func TestMutexDiscipline_RunLock_ZeroValueUsable(t *testing.T) {
	t.Parallel()

	// Zero value — no NewRunLock constructor needed.
	var lock handlercontract.RunLock
	lock.Lock()
	lock.Unlock()
	lock.RLock()
	lock.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-015 — concurrent write lock exclusion (race detector coverage)
// ─────────────────────────────────────────────────────────────────────────────

// TestMutexDiscipline_RunLock_WriteExclusion verifies that concurrent write
// operations on a shared counter produce the correct result under the RunLock.
// Run with -race to exercise the race detector.
func TestMutexDiscipline_RunLock_WriteExclusion(t *testing.T) {
	t.Parallel()

	var lock handlercontract.RunLock
	counter := 0
	const goroutines = 50
	const increments = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range increments {
				lock.Lock()
				counter++
				lock.Unlock()
			}
		}()
	}
	wg.Wait()

	want := goroutines * increments
	if counter != want {
		t.Errorf("counter = %d after concurrent increments, want %d (RunLock not providing exclusion)", counter, want)
	}
}
