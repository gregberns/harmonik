package daemon

// queuestore_hkj808w.go — daemon-owned queue registry with single-writer discipline.
//
// QueueStore is the composition-root holder for the name-keyed in-memory
// queue registry. It enforces the QM-060 single-writer contract:
// all mutations to the queues map MUST be serialised through the queueMu
// mutex; concurrent readers (queue-status, dispatcher capacity evaluation)
// MUST hold queueMu.RLock before accessing the map.
//
// Prior to hk-tigaf.2 QueueStore held a single *queue.Queue pointer.
// hk-tigaf.2 (NQ-A1) reshapes it to map[string]*queue.Queue so each
// named queue gets its own slot. The per-name single-active guard (QM-027)
// is now enforced at validation time against the per-name slot rather than
// a global singleton.
//
// Backward-compatibility shim: Queue() / SetQueue() / ClearQueue() /
// LockForMutation() still exist and operate on the QueueNameMain ("main")
// slot so that workloop.go callers are unaffected by this component.
//
// Usage in the composition root (daemon.Start):
//
//	qs := newQueueStore()
//	// Later: thread qs into socket handlers and the workloop (T50).
//
// The existing capacity-gate (RunRegistry.Len() >= effectiveMax) and
// claim-write serialisation semaphore (claimSem) already implement
// EM-049 and EM-050 respectively in workloop.go (hk-e61c3.2, hk-e61c3.3).
// This file wires only the queue-object ownership layer (EM-049 via QM-060)
// on top of those existing primitives.
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer),
//
//	§9.3 QM-062 (capacity composition).
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate),
//
//	§4.11 EM-050 (claim-write serialisation),
//	§4.11 EM-051 (max_concurrent configuration).
//
// Bead ref: hk-j808w (original), hk-tigaf.2 (name-keyed reshape).

import (
	"sync"

	"github.com/gregberns/harmonik/internal/queue"
)

// submitWakeCBufSize is the buffer depth for the wake channel. Buffer of 1
// ensures a non-blocking send never blocks and coalesces rapid bursts into a
// single wakeup (hk-24xn1).
const submitWakeCBufSize = 1

// QueueStore is the daemon-singleton holder for the name-keyed queue registry.
//
// One QueueStore instance is created at daemon.Start (composition root) and
// shared between the socket-handler path (queue-submit / queue-append /
// queue-status) and the workloop dispatcher (T50). The zero value is NOT valid
// — use newQueueStore() / NewQueueStore().
//
// All mutations to the queues map MUST go through SetQueue / SetQueueByName /
// ClearQueue / ClearQueueByName while holding the write lock. All reads MUST
// go through Queue / QueueByName while holding the read lock. Both families
// acquire the appropriate lock internally; callers do NOT hold queueMu directly.
//
// Spec ref: specs/queue-model.md §9.1 QM-060.
// Bead ref: hk-j808w, hk-tigaf.2.
type QueueStore struct {
	queueMu sync.RWMutex
	queues  map[string]*queue.Queue
	// wakeC receives a signal after every SetQueue / SetQueueByName call so the
	// workloop can break out of its idle sleep immediately on queue-submit (hk-24xn1).
	// Buffer of 1 coalesces rapid bursts; a full buffer is silently dropped
	// (non-blocking send).
	wakeC chan struct{}
}

// newQueueStore returns a ready-to-use QueueStore with no active queues.
//
// Bead ref: hk-j808w, hk-tigaf.2.
func newQueueStore() *QueueStore {
	return &QueueStore{
		queues: make(map[string]*queue.Queue),
		wakeC:  make(chan struct{}, submitWakeCBufSize),
	}
}

// NewQueueStore is the exported constructor for callers outside the daemon
// package (e.g. cmd/harmonik/run.go) that need to retain a QueueStore
// reference to inspect status after daemon.Start returns (hk-8jh26 Fix 2).
//
// Bead ref: hk-8jh26.
func NewQueueStore() *QueueStore {
	return newQueueStore()
}

// ---------------------------------------------------------------------------
// Single-name shims — backward-compat API that targets QueueNameMain ("main").
// The workloop, RunRegistry, and all pre-NQ-A1 callers use these methods.
// ---------------------------------------------------------------------------

// SetQueue installs q under the write lock at the slot derived from q.Name
// (normalised to QueueNameMain if empty). It replaces any prior value at that
// slot and signals the wake channel.
//
// This is the primary mutation entry point per QM-060. All queue-submit /
// queue-append paths MUST call SetQueue (or SetQueueByName / ClearQueue /
// ClearQueueByName) rather than mutating the map directly.
//
// Spec ref: specs/queue-model.md §9.1 QM-060.
// Bead ref: hk-j808w, hk-tigaf.2.
func (s *QueueStore) SetQueue(q *queue.Queue) {
	name := queue.NormaliseQueueName(q.Name)
	s.queueMu.Lock()
	s.queues[name] = q
	s.queueMu.Unlock()
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// Queue returns the *queue.Queue for the QueueNameMain ("main") slot under the
// read lock, or nil when no such queue is loaded. Backward-compatible accessor
// for the workloop and all pre-NQ-A1 callers.
//
// The returned pointer MUST NOT be mutated by the caller; use SetQueue for
// mutations.
//
// Spec ref: specs/queue-model.md §9.1 QM-060, §9.6 QM-064.
// Bead ref: hk-j808w.
func (s *QueueStore) Queue() *queue.Queue {
	s.queueMu.RLock()
	q := s.queues[queue.QueueNameMain]
	s.queueMu.RUnlock()
	return q
}

// ClearQueue removes the QueueNameMain ("main") slot under the write lock.
// After ClearQueue returns, Queue returns nil.
//
// Called by the composition root after queue completion (QM-003: queue.json
// unlinked when all groups reach complete-success).
//
// Spec ref: specs/queue-model.md §2.1 QM-003.
// Bead ref: hk-j808w.
func (s *QueueStore) ClearQueue() {
	s.queueMu.Lock()
	delete(s.queues, queue.QueueNameMain)
	s.queueMu.Unlock()
}

// ---------------------------------------------------------------------------
// Name-keyed API — per-name set/get/clear for multi-queue dispatch (NQ-A1).
// ---------------------------------------------------------------------------

// QueueByName returns the *queue.Queue for the given name under the read lock,
// or nil when no queue with that name is loaded. name MUST be normalised
// (non-empty) before calling; use queue.NormaliseQueueName.
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) QueueByName(name string) *queue.Queue {
	s.queueMu.RLock()
	q := s.queues[name]
	s.queueMu.RUnlock()
	return q
}

// SetQueueByName installs q under the write lock at the given name slot,
// replacing any prior value. name MUST be normalised before calling. Signals
// the wake channel.
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) SetQueueByName(name string, q *queue.Queue) {
	s.queueMu.Lock()
	s.queues[name] = q
	s.queueMu.Unlock()
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// ClearQueueByName removes the queue at the given name slot under the write
// lock. name MUST be normalised before calling. No-ops when the name is
// absent.
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) ClearQueueByName(name string) {
	s.queueMu.Lock()
	delete(s.queues, name)
	s.queueMu.Unlock()
}

// AllQueues returns a snapshot of the full name→queue map under the read lock.
// The returned map is a shallow copy; the *Queue pointers MUST NOT be mutated.
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) AllQueues() map[string]*queue.Queue {
	s.queueMu.RLock()
	out := make(map[string]*queue.Queue, len(s.queues))
	for k, v := range s.queues {
		out[k] = v
	}
	s.queueMu.RUnlock()
	return out
}

// ---------------------------------------------------------------------------
// Wake / WakeCh — workloop submit-wake (unchanged from hk-j808w).
// ---------------------------------------------------------------------------

// WakeCh returns the channel that receives a signal after every SetQueue /
// SetQueueByName call. The workloop selects on this channel alongside its poll
// timer to wake immediately when a new queue is submitted (hk-24xn1).
//
// Receiving from a nil channel blocks forever — callers that pass a nil
// *QueueStore safely ignore the channel via workloopSleep's nil-channel case.
//
// Bead ref: hk-24xn1.
func (s *QueueStore) WakeCh() <-chan struct{} {
	return s.wakeC
}

// Wake fires the wake channel without mutating the queue pointer. Unlike
// SetQueue it touches only wakeC, so callers that have already persisted the
// queue (or that mutate it via the LockedQueueStore.SetQueue no-wake variant)
// can signal the idle dispatch loop without a second SetQueue/persist round —
// avoiding a double-persist race.
//
// The per-run completion path (evaluateGroupAdvanceWithOutcome) calls Wake on
// every run_completed so the idle loop re-runs its §2.8 deferred-item
// re-evaluation: a freshly-terminal blocker un-defers its dependent, but the
// loop must tick to observe it (hk-nbjht). The send is non-blocking and
// coalesces (buffer of 1), matching SetQueue's wake semantics.
//
// Bead ref: hk-nbjht.
func (s *QueueStore) Wake() {
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------------------
// LockForMutation — read-then-write serialisation (QM-064).
// ---------------------------------------------------------------------------

// LockForMutation acquires the write lock and returns a *LockedQueueStore
// whose Done method releases it. Use for read-then-write sequences
// (validate-then-mutate per QM-064).
//
// The LockedQueueStore accessor operates on the QueueNameMain ("main") slot
// for backward compatibility with workloop.go callers.
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

// Queue returns the current queue pointer for the QueueNameMain ("main") slot.
// Safe to call while the write lock is held (i.e. during a LockForMutation block).
//
// Bead ref: hk-j808w.
func (lq *LockedQueueStore) Queue() *queue.Queue {
	return lq.s.queues[queue.QueueNameMain]
}

// SetQueue updates the queue pointer at the slot derived from q.Name
// (normalised to QueueNameMain if empty). Safe to call while the write lock
// is held. Does NOT signal the wake channel (use QueueStore.SetQueue for that).
//
// Bead ref: hk-j808w, hk-tigaf.2.
func (lq *LockedQueueStore) SetQueue(q *queue.Queue) {
	name := queue.NormaliseQueueName(q.Name)
	lq.s.queues[name] = q
}

// Done releases the write lock. MUST be called exactly once per
// LockForMutation call (idiomatic: defer lq.Done()).
//
// Bead ref: hk-j808w.
func (lq *LockedQueueStore) Done() {
	lq.s.queueMu.Unlock()
}

// LockedQueueByName returns the *queue.Queue for the given name while the
// write lock is held. name MUST be normalised before calling (use
// queue.NormaliseQueueName). Returns nil when no queue with that name is
// loaded.
//
// Safe to call while holding the LockForMutation write lock.
//
// Bead ref: hk-tigaf.6.
func (lq *LockedQueueStore) LockedQueueByName(name string) *queue.Queue {
	return lq.s.queues[name]
}

// LockedSetQueueByName updates the queue pointer at the given name slot
// while the write lock is held. name MUST be normalised before calling.
// Does NOT signal the wake channel (use QueueStore.SetQueueByName for that).
//
// Bead ref: hk-tigaf.6.
func (lq *LockedQueueStore) LockedSetQueueByName(name string, q *queue.Queue) {
	lq.s.queues[name] = q
}

// LockedAllQueueNames returns the names of all queues currently in the store
// while the write lock is held. The returned slice is a snapshot; callers
// must not modify the underlying map entries through this slice.
//
// Bead ref: hk-tigaf.6.
func (lq *LockedQueueStore) LockedAllQueueNames() []string {
	names := make([]string, 0, len(lq.s.queues))
	for name := range lq.s.queues {
		names = append(names, name)
	}
	return names
}
