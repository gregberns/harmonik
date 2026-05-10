package handlercontract

import "sync"

// mutexDiscipline — per-bead helper prefix for test helpers in
// mutexdiscipline_hc015_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.18).

// RunLock is a per-run read/write mutex that enforces the HC-015 mutex
// discipline for state transitions.
//
// # HC-015 rules
//
//  1. State transitions MUST acquire the write lock before mutating run state.
//  2. Event publication MUST NOT hold the write lock (publication happens after
//     the lock is released, avoiding deadlock with synchronous subscribers).
//  3. No RunLock MUST be held across a call into S04's adapter methods; the
//     adapter may itself acquire other locks and holding the run lock at the call
//     site would create a lock-ordering hazard.
//
// # Usage
//
// Daemon code that manages per-run state allocates one RunLock per run_id at
// run creation time and stores it alongside the run's in-memory state. Workers
// MUST follow the acquire-mutate-release-then-publish sequence:
//
//	lock.Lock()
//	// mutate state
//	lock.Unlock()
//	// publish event (outside the lock)
//
// # Adapter calls
//
// Before calling any Adapter method (DetectReady, DetectRateLimit,
// CleanExitSequence, RotateAccount), the watcher MUST verify that it does NOT
// hold the RunLock. Holding the RunLock across an adapter call violates HC-015
// and is a daemon defect.
//
// Spec: specs/handler-contract.md §4.3.HC-015.
type RunLock struct {
	mu sync.RWMutex
}

// Lock acquires the exclusive (write) lock for a state transition.
//
// The caller MUST release the lock via Unlock before publishing any events to
// the in-process bus (HC-015: event publication MUST NOT block the state lock).
// The caller MUST also release the lock before invoking any Adapter method
// (HC-015: no mutex held across adapter call).
func (r *RunLock) Lock() { r.mu.Lock() }

// Unlock releases the exclusive (write) lock acquired by Lock.
func (r *RunLock) Unlock() { r.mu.Unlock() }

// RLock acquires the shared (read) lock for state observation.
//
// Multiple goroutines may hold RLock concurrently. The caller MUST release via
// RUnlock; the same no-adapter-call constraint applies (do not invoke an
// Adapter method while holding RLock).
func (r *RunLock) RLock() { r.mu.RLock() }

// RUnlock releases the shared (read) lock acquired by RLock.
func (r *RunLock) RUnlock() { r.mu.RUnlock() }

// TryLock attempts to acquire the exclusive (write) lock without blocking.
//
// Returns true and holds the lock on success. Returns false immediately if the
// lock is contended. Callers that use TryLock MUST still follow the full HC-015
// discipline if TryLock succeeds (release before publish, release before
// adapter call).
func (r *RunLock) TryLock() bool { return r.mu.TryLock() }
