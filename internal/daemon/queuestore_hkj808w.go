package daemon

// queuestore_hkj808w.go — daemon-owned queue handle with single-writer discipline.
//
// QueueStore is the composition-root holder for the single in-memory
// *queue.Queue instance. It enforces the QM-060 single-writer contract:
// all mutations to the Queue object MUST be serialized through the
// queueMu mutex; concurrent readers (queue-status, dispatcher capacity
// evaluation) MUST hold queueMu.RLock before accessing the queue pointer.
//
// Usage in the composition root (daemon.Start):
//
//	qs := newQueueStore()
//	// Later: thread qs into socket handlers and the workloop (T50).
//
// The existing capacity-gate (RunRegistry.Len() >= effectiveMax) and
// claim-write serialization semaphore (claimSem) already implement
// EM-049 and EM-050 respectively in workloop.go (hk-e61c3.2,
// hk-e61c3.3). This file wires only the queue-object ownership layer
// (EM-049 via QM-060) on top of those existing primitives.
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer),
//
//	§9.3 QM-062 (capacity composition).
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate),
//
//	§4.11 EM-050 (claim-write serialization),
//	§4.11 EM-051 (max_concurrent configuration).
//
// Bead ref: hk-j808w.

import (
	"sync"

	"github.com/gregberns/harmonik/internal/queue"
)

// submitWakeCBufSize is the buffer depth for the wake channel. Buffer of 1
// ensures a non-blocking send never blocks and coalesces rapid bursts into a
// single wakeup (hk-24xn1).
const submitWakeCBufSize = 1

// QueueStore is the daemon-singleton holder for the active *queue.Queue.
//
// One QueueStore instance is created at daemon.Start (composition root) and
// shared between the socket-handler path (queue-submit / queue-append /
// queue-status) and the workloop dispatcher (T50). The zero value is valid
// (no queue loaded).
//
// All mutations to the queue pointer MUST go through SetQueue / ClearQueue
// while holding the write lock. All reads MUST go through Queue while
// holding the read lock. Both methods acquire the appropriate lock
// internally, so callers do NOT hold queueMu directly.
//
// Spec ref: specs/queue-model.md §9.1 QM-060.
// Bead ref: hk-j808w.
type QueueStore struct {
	queueMu sync.RWMutex
	q       *queue.Queue
	// wakeC receives a signal after every SetQueue call so the workloop can
	// break out of its idle sleep immediately on queue-submit (hk-24xn1).
	// Buffer of 1 coalesces rapid bursts; a full buffer is silently dropped
	// (non-blocking send).
	wakeC chan struct{}
}

// newQueueStore returns a ready-to-use QueueStore with no active queue.
//
// Bead ref: hk-j808w.
func newQueueStore() *QueueStore {
	return &QueueStore{wakeC: make(chan struct{}, submitWakeCBufSize)}
}

// NewQueueStore is the exported constructor for callers outside the daemon
// package (e.g. cmd/harmonik/run.go) that need to retain a QueueStore
// reference to inspect status after daemon.Start returns (hk-8jh26 Fix 2).
//
// Bead ref: hk-8jh26.
func NewQueueStore() *QueueStore {
	return newQueueStore()
}

// SetQueue installs q as the active queue under the write lock, replacing
// any prior value. q MUST be non-nil; use ClearQueue to remove the queue.
//
// This is the sole mutation entry point per QM-060: all queue-submit /
// queue-append paths MUST call SetQueue (or ClearQueue) rather than
// mutating the *Queue pointer directly.
//
// Spec ref: specs/queue-model.md §9.1 QM-060.
// Bead ref: hk-j808w.
func (s *QueueStore) SetQueue(q *queue.Queue) {
	s.queueMu.Lock()
	s.q = q
	s.queueMu.Unlock()
	// Signal the workloop that a new queue is available so it wakes immediately
	// instead of waiting for the next poll tick (hk-24xn1). Non-blocking: a
	// full buffer means a wake is already pending, so no additional signal is
	// needed.
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// Queue returns the active *queue.Queue under the read lock, or nil when
// no queue is loaded. The returned pointer MUST NOT be mutated by the
// caller; use SetQueue for mutations.
//
// Callers that perform a read-then-write sequence (e.g. validate-then-
// mutate for queue-append) MUST acquire the write lock via LockForMutation
// for the entire sequence to preserve the QM-064 no-mutation-during-
// validation invariant.
//
// Spec ref: specs/queue-model.md §9.1 QM-060, §9.6 QM-064.
// Bead ref: hk-j808w.
func (s *QueueStore) Queue() *queue.Queue {
	s.queueMu.RLock()
	q := s.q
	s.queueMu.RUnlock()
	return q
}

// ClearQueue removes the active queue under the write lock. After
// ClearQueue returns, Queue returns nil.
//
// Called by the composition root after queue completion (QM-003: queue.json
// unlinked when all groups reach complete-success).
//
// Spec ref: specs/queue-model.md §2.1 QM-003.
// Bead ref: hk-j808w.
func (s *QueueStore) ClearQueue() {
	s.queueMu.Lock()
	s.q = nil
	s.queueMu.Unlock()
}

// WakeCh returns the channel that receives a signal after every SetQueue call.
// The workloop selects on this channel alongside its poll timer to wake
// immediately when a new queue is submitted (hk-24xn1).
//
// Receiving from a nil channel blocks forever — callers that pass a nil
// *QueueStore safely ignore the channel via workloopSleep's nil-channel case.
//
// Bead ref: hk-24xn1.
func (s *QueueStore) WakeCh() <-chan struct{} {
	return s.wakeC
}

// LockForMutation acquires the write lock and returns a *LockedQueueStore
// whose Done method releases it. Use for read-then-write sequences
// (validate-then-mutate per QM-064).
//
// Example:
//
//	lq := qs.LockForMutation()
//	defer lq.Done()
//	q := lq.Queue() // snapshot under write lock — no concurrent mutation possible
//	// ... validate and mutate q ...
//	lq.SetQueue(q)  // write through; Done releases the lock
//
// Spec ref: specs/queue-model.md §9.1 QM-060, §9.6 QM-064.
// Bead ref: hk-j808w.
func (s *QueueStore) LockForMutation() *LockedQueueStore {
	s.queueMu.Lock()
	return &LockedQueueStore{s: s}
}

// LockedQueueStore is a write-locked view of QueueStore. The caller holds
// the write lock for the lifetime of the LockedQueueStore. Call Done to
// release.
//
// Bead ref: hk-j808w.
type LockedQueueStore struct {
	s *QueueStore
}

// Queue returns the current queue pointer. Safe to call while the write
// lock is held (i.e. during a LockForMutation block).
//
// Bead ref: hk-j808w.
func (lq *LockedQueueStore) Queue() *queue.Queue {
	return lq.s.q
}

// SetQueue updates the queue pointer. Safe to call while the write lock
// is held.
//
// Bead ref: hk-j808w.
func (lq *LockedQueueStore) SetQueue(q *queue.Queue) {
	lq.s.q = q
}

// Done releases the write lock. MUST be called exactly once per
// LockForMutation call (idiomatic: defer lq.Done()).
//
// Bead ref: hk-j808w.
func (lq *LockedQueueStore) Done() {
	lq.s.queueMu.Unlock()
}
