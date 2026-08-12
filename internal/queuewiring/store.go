// Package queuewiring holds the daemon-side queue OWNERSHIP layer extracted out
// of internal/daemon (P2 unit E3). The queue ENGINE is already a clean leaf
// (internal/queue); this package holds the in-memory QueueStore that satisfies
// queue.QueueSetter / queue.LockedQueueView / queue.MutationLocker, the brcli →
// queue.BeadLedger bridge, and the operator pause/resume consumer. The daemon is
// the composition root and injects these; it MUST NOT be imported back.
//
// store.go — daemon-owned queue registry with single-writer discipline.
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
package queuewiring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

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
	queueMu      sync.RWMutex
	completionMu sync.Mutex
	queues       map[string]*queue.Queue
	generations  map[string]uint64
	quarantined  map[string]error
	// wakeC receives a signal after every SetQueue / SetQueueByName call so the
	// workloop can break out of its idle sleep immediately on queue-submit (hk-24xn1).
	// Buffer of 1 coalesces rapid bursts; a full buffer is silently dropped
	// (non-blocking send).
	wakeC chan struct{}
}

var errCompletionObservationInProgress = errors.New("completion observation is in progress")

// newQueueStore returns a ready-to-use QueueStore with no active queues.
//
// Bead ref: hk-j808w, hk-tigaf.2.
func newQueueStore() *QueueStore {
	return &QueueStore{
		queues:      make(map[string]*queue.Queue),
		generations: make(map[string]uint64),
		quarantined: make(map[string]error),
		wakeC:       make(chan struct{}, submitWakeCBufSize),
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
// slot and signals the wake channel. It refuses a quarantined name because a
// raw memory write cannot prove that durable recovery completed.
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
	if s.quarantined[name] != nil {
		s.queueMu.Unlock()
		return
	}
	s.queues[name] = q
	s.generations[name]++
	s.queueMu.Unlock()
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// Queue returns a deep copy of the *queue.Queue for the QueueNameMain ("main")
// slot, or nil when no such queue is loaded. Backward-compatible accessor for
// the workloop and all pre-NQ-A1 callers.
//
// Returns a copy (not the live stored pointer) because the write path
// (runWorkLoop, via LockForMutation/LockedQueueByName) mutates fields on the
// stored *queue.Queue in place under the write lock rather than always
// installing a fresh object; callers that read fields off a pointer obtained
// here do so without holding any lock, so an aliased pointer would race with
// that in-place mutation. This was a real, reproducible race (hk-ri2in.4)
// caught by go test -race across TestWorkLoop_QueuePath_* and friends.
//
// Spec ref: specs/queue-model.md §9.1 QM-060, §9.6 QM-064.
// Bead ref: hk-j808w.
func (s *QueueStore) Queue() *queue.Queue {
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	return queue.CloneQueue(s.queues[queue.QueueNameMain])
}

// ClearQueue removes the QueueNameMain ("main") slot under the write lock.
// It refuses a quarantined name because that name still owns unresolved
// durable state.
//
// Called by the composition root after queue completion (QM-003: queue.json
// unlinked when all groups reach complete-success).
//
// Spec ref: specs/queue-model.md §2.1 QM-003.
// Bead ref: hk-j808w.
func (s *QueueStore) ClearQueue() {
	s.queueMu.Lock()
	if s.quarantined[queue.QueueNameMain] != nil {
		s.queueMu.Unlock()
		return
	}
	delete(s.queues, queue.QueueNameMain)
	s.generations[queue.QueueNameMain]++
	s.queueMu.Unlock()
}

// ---------------------------------------------------------------------------
// Name-keyed API — per-name set/get/clear for multi-queue dispatch (NQ-A1).
// ---------------------------------------------------------------------------

// QueueByName returns a deep copy of the *queue.Queue for the given name, or
// nil when no queue with that name is loaded. name MUST be normalised
// (non-empty) before calling; use queue.NormaliseQueueName.
//
// Returns a copy rather than the live stored pointer — see [QueueStore.Queue]
// for why (hk-ri2in.4).
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) QueueByName(name string) *queue.Queue {
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	return queue.CloneQueue(s.queues[name])
}

// SetQueueByName installs q under the write lock at the given name slot,
// replacing any prior value. name MUST be normalised before calling. Signals
// the wake channel. It refuses an I/O quarantine.
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) SetQueueByName(name string, q *queue.Queue) {
	s.queueMu.Lock()
	if s.quarantined[name] != nil {
		s.queueMu.Unlock()
		return
	}
	s.queues[name] = q
	s.generations[name]++
	s.queueMu.Unlock()
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// ClearQueueByName removes the queue at the given name slot under the write
// lock. name MUST be normalised before calling. No-ops when the name is
// absent. It refuses an I/O quarantine.
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) ClearQueueByName(name string) {
	s.queueMu.Lock()
	if s.quarantined[name] != nil {
		s.queueMu.Unlock()
		return
	}
	delete(s.queues, name)
	s.generations[name]++
	s.queueMu.Unlock()
}

// AllQueues returns a snapshot of the full name→queue map under the read
// lock. Both the map and each *Queue value are deep copies — see
// [QueueStore.Queue] for why the *Queue values cannot be the live stored
// pointers (hk-ri2in.4).
//
// Bead ref: hk-tigaf.2.
func (s *QueueStore) AllQueues() map[string]*queue.Queue {
	s.queueMu.RLock()
	out := make(map[string]*queue.Queue, len(s.queues))
	for k, v := range s.queues {
		out[k] = queue.CloneQueue(v)
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

// LockForMutationView adapts LockForMutation to the queue.LockedQueueView
// interface so queue.HandlerAdapter (which cannot import internal/daemon —
// cycle prevention) can route the queue-append read-modify-write through the
// mutation lock. Together with Wake, this makes *QueueStore satisfy
// queue.MutationLocker (B1: two-writer lost-update fix).
//
// Spec ref: specs/queue-model.md §9.1 QM-060, §9.6 QM-064.
func (s *QueueStore) LockForMutationView() queue.LockedQueueView {
	return s.LockForMutation()
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
// It returns nil when the name is quarantined. This refuses a mutation flow
// before it can persist a replacement. Safe to call while the write lock is
// held (i.e. during a LockForMutation block).
//
// Bead ref: hk-j808w.
func (lq *LockedQueueStore) Queue() *queue.Queue {
	if lq.s.quarantined[queue.QueueNameMain] != nil {
		return nil
	}
	return lq.s.queues[queue.QueueNameMain]
}

// SetQueue updates the queue pointer at the slot derived from q.Name
// (normalised to QueueNameMain if empty). Safe to call while the write lock
// is held. Does NOT signal the wake channel (use QueueStore.SetQueue for that).
// It refuses an I/O quarantine.
//
// Bead ref: hk-j808w, hk-tigaf.2.
func (lq *LockedQueueStore) SetQueue(q *queue.Queue) {
	name := queue.NormaliseQueueName(q.Name)
	if lq.s.quarantined[name] != nil {
		return
	}
	lq.s.queues[name] = q
	lq.s.generations[name]++
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
// loaded. A quarantined queue is hidden from the mutation view. This refuses
// read-modify-persist flows before they can perform an external write.
//
// Safe to call while holding the LockForMutation write lock.
//
// Bead ref: hk-tigaf.6.
func (lq *LockedQueueStore) LockedQueueByName(name string) *queue.Queue {
	if lq.s.quarantined[name] != nil {
		return nil
	}
	return lq.s.queues[name]
}

// LockedSetQueueByName updates the queue pointer at the given name slot
// while the write lock is held. name MUST be normalised before calling.
// Does NOT signal the wake channel (use QueueStore.SetQueueByName for that).
// It refuses an I/O quarantine.
//
// Bead ref: hk-tigaf.6.
func (lq *LockedQueueStore) LockedSetQueueByName(name string, q *queue.Queue) {
	if lq.s.quarantined[name] != nil {
		return
	}
	lq.s.queues[name] = q
	lq.s.generations[name]++
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

// ---------------------------------------------------------------------------

// Snapshot is kept as an alias for existing QueueStore callers. The queue
// package owns the transaction port so queue operations do not import this
// registry package.
type Snapshot = queue.QueueSnapshot

// ErrQueueQuarantined marks a transaction refused because an earlier write to
// that queue failed. QM-001 requires the daemon to refuse further mutations
// after any I/O error in the atomic-write sequence, so the refusal is sticky
// and does not clear by retrying.
//
// Callers MUST distinguish it from an ordinary rejection: an ordinary rejection
// means "your snapshot is stale, look again", while this means "this queue is
// shut until an operator repairs it". Reporting the second as the first is how
// a hard failure becomes a silent spin.
var ErrQueueQuarantined = errors.New("queuewiring: queue is quarantined after a failed write")

// ErrStaleSnapshot marks a transaction or a completion refused because the
// caller's snapshot no longer describes the live queue — the generation moved,
// or the bytes differ at the same generation. It is the ONE refusal a caller
// may retry by re-reading: the queue is healthy and the world simply moved.
//
// It exists so a caller classifies the retryable case POSITIVELY. Classifying
// it as "rejected and not quarantined" reads the same on this file today and
// breaks quietly on the next refusal somebody adds: the completion path, for
// one, returns the quarantine reason unwrapped, so a negative test there would
// retry a queue that is shut until an operator repairs it.
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single writer).
var ErrStaleSnapshot = errors.New("queuewiring: queue snapshot is stale")

// The queue read commands reach the quarantine through this port. It is
// satisfied by a runtime type assertion in queue.HandlerAdapter, so nothing
// else would catch a rename of the method below.
var _ queue.QuarantineReader = (*QueueStore)(nil)

// QuarantineReason returns the error that shut the named queue, or nil when the
// queue still accepts writes. name is normalised here, so callers may pass the
// operator's spelling.
//
// The reason is returned rather than a bare boolean because the operator
// response depends on it: a full disk and a queue file another process replaced
// need different repairs. A surface that only says "quarantined" tells an
// operator to look somewhere, not what to fix.
//
// This is a read accessor over state Transact already holds. It adds no state
// and changes no write behaviour.
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
// Bead ref: hk-ujanf.
func (s *QueueStore) QuarantineReason(name string) error {
	name = queue.NormaliseQueueName(name)
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	return s.quarantined[name]
}

// TransactionRequest is kept as an alias for existing QueueStore callers.
type TransactionRequest = queue.TransactionRequest

// TransactionResult is kept as an alias for existing QueueStore callers.
type TransactionResult = queue.TransactionResult

// FailedRecoveryResult reports either a new durable recovery or an exact
// already-durable recovery receipt.
type FailedRecoveryResult struct {
	TransactionResult
	Receipt queue.FailedRecoveryReceipt

	// AlreadyRecovered is true when the queue was already active behind this
	// exact receipt and the call minted nothing and mutated nothing. The caller
	// needs to tell that apart from a fresh recovery, because the two are the
	// same success to the transaction layer and a different answer to an
	// operator.
	AlreadyRecovered bool
}

var _ queue.TransactionStore = (*QueueStore)(nil)

// commitFailedRecovery resumes one paused-by-failure queue through the QM-001
// transaction owner. It returns the same receipt without a second mutation
// when the queue was already recovered.
//
// It is the durable half of recovery and it decides nothing about policy. The
// caller is RecoverFailed in recovery.go, which owns the QM-052b preflight and
// the typed refusals. Keeping the two apart is what stops a second recovery
// entry point from growing: this method is unexported, so the ledger preflight
// cannot be skipped by reaching past it.
func (s *QueueStore) commitFailedRecovery(ctx context.Context, projectDir, name string) FailedRecoveryResult {
	name = queue.NormaliseQueueName(name)
	snapshot := s.Snapshot(name)
	if snapshot.Queue == nil {
		return FailedRecoveryResult{TransactionResult: rejectedTransaction(errors.New("failed recovery queue is absent"))}
	}
	if snapshot.Queue.Status == queue.QueueStatusActive && snapshot.Queue.FailedRecoveryReceiptID != nil {
		receipt, _, err := queue.ReadFailedRecoveryReceipt(projectDir, *snapshot.Queue)
		if err != nil {
			return FailedRecoveryResult{TransactionResult: rejectedTransaction(fmt.Errorf("read failed recovery receipt: %w", err))}
		}
		return FailedRecoveryResult{
			TransactionResult: TransactionResult{
				NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable},
				Snapshot:        snapshot,
			},
			Receipt:          receipt,
			AlreadyRecovered: true,
		}
	}
	prepared, err := queue.PrepareFailedRecovery(*snapshot.Queue, time.Now())
	if err != nil {
		return FailedRecoveryResult{TransactionResult: rejectedTransaction(err)}
	}
	result := s.Transact(ctx, TransactionRequest{
		Snapshot:                     snapshot,
		ProjectDir:                   projectDir,
		TransactionID:                prepared.TransactionID,
		OperationKind:                queue.OperationFailedRecovery,
		WakeRequired:                 true,
		FailedRecoveryReceiptBinding: prepared.Binding,
		Mutate: func(candidate *queue.Queue) error {
			*candidate = prepared.Candidate
			return nil
		},
	})
	if !result.Committed() || result.CleanupErr != nil {
		return FailedRecoveryResult{TransactionResult: result}
	}
	return FailedRecoveryResult{TransactionResult: result, Receipt: prepared.Receipt}
}

// Snapshot returns a deep-cloned, immutable view. Callers must provide this
// exact generation to Transact; any intervening mutation rejects before I/O.
func (s *QueueStore) Snapshot(name string) Snapshot {
	name = queue.NormaliseQueueName(name)
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	return Snapshot{
		Name:       name,
		Queue:      queue.CloneQueue(s.queues[name]),
		Generation: s.generations[name],
	}
}

// Transact enforces clone -> mutate -> persist -> install. It intentionally
// performs no event emission and has no completion-receipt behavior.
func (s *QueueStore) Transact(ctx context.Context, req TransactionRequest) TransactionResult {
	name := queue.NormaliseQueueName(req.Snapshot.Name)
	s.queueMu.Lock()
	if quarantineErr := s.quarantined[name]; quarantineErr != nil && req.OperationKind != queue.OperationFailedRecovery {
		s.queueMu.Unlock()
		return rejectedTransaction(fmt.Errorf("%w: queue name %q: %w", ErrQueueQuarantined, name, quarantineErr))
	}
	if req.Snapshot.Generation != s.generations[name] {
		s.queueMu.Unlock()
		return rejectedTransaction(fmt.Errorf("%w: generation moved", ErrStaleSnapshot))
	}
	current := queue.CloneQueue(s.queues[name])
	if !sameQueue(current, req.Snapshot.Queue) {
		s.queueMu.Unlock()
		return rejectedTransaction(fmt.Errorf("%w: bytes differ at the same generation", ErrStaleSnapshot))
	}
	if req.Mutate == nil {
		s.queueMu.Unlock()
		return rejectedTransaction(errors.New("transaction mutation is required"))
	}
	if req.Precondition != nil {
		if err := req.Precondition(s.otherQueuesLocked(name)); err != nil {
			s.queueMu.Unlock()
			return rejectedTransaction(err)
		}
	}
	candidate := queue.CloneQueue(current)
	if candidate == nil {
		candidate = &queue.Queue{Name: name}
	}
	if err := req.Mutate(candidate); err != nil {
		s.queueMu.Unlock()
		return rejectedTransaction(err)
	}
	candidate.Name = name
	priorBytes, err := marshalOptional(current)
	if err != nil {
		s.queueMu.Unlock()
		return rejectedTransaction(fmt.Errorf("marshal prior: %w", err))
	}
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		s.queueMu.Unlock()
		return TransactionResult{NamespaceResult: queue.NamespaceResult{
			Outcome: queue.OutcomeNotCommitted,
			Err:     fmt.Errorf("marshal candidate: %w", err),
		}}
	}
	if bytes.Equal(priorBytes, candidateBytes) {
		if req.OperationKind == queue.OperationCancellation ||
			req.ArchiveHandoff != nil ||
			req.OperationKind == queue.OperationFailedRecovery ||
			req.OperationKind == queue.OperationCompletion {
			s.queueMu.Unlock()
			return rejectedTransaction(errors.New("receipt-bearing transaction cannot collapse as no-op"))
		}
		resultSnapshot := Snapshot{
			Name:       name,
			Queue:      queue.CloneQueue(current),
			Generation: s.generations[name],
		}
		s.queueMu.Unlock()
		return TransactionResult{
			NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable},
			Snapshot:        resultSnapshot,
		}
	}
	commit := queue.WriteReplacement(ctx, queue.ReplacementPlan{
		ProjectDir:                   req.ProjectDir,
		TransactionID:                req.TransactionID,
		OperationKind:                req.OperationKind,
		NormalizedName:               name,
		QueueID:                      candidate.QueueID,
		PriorBytes:                   priorBytes,
		CandidateBytes:               candidateBytes,
		WakeRequired:                 req.WakeRequired,
		ArchiveHandoff:               req.ArchiveHandoff,
		FailedRecoveryReceiptBinding: req.FailedRecoveryReceiptBinding,
		CompletionReceiptBinding:     req.CompletionReceiptBinding,
	})
	if !commit.Committed() {
		// QM-001: on ANY I/O error in the atomic-write sequence the daemon MUST
		// refuse further mutations to this queue. That covers both a write that
		// definitely did not land and one whose result is unknown — a full disk
		// and a corrupt queue file do not clear by trying again, and a caller
		// that keeps trying turns one failure into an unbounded retry loop.
		//
		// OutcomeRejected is deliberately NOT quarantined: the replacement was
		// refused before any I/O was attempted (a malformed request, or a
		// cancelled context), so nothing about the queue on disk is in doubt and
		// the next caller deserves a fresh try.
		//
		// Spec ref: specs/queue-model.md §3.1 QM-001.
		if commit.Outcome != queue.OutcomeRejected {
			s.quarantined[name] = commit.Err
		}
		s.queueMu.Unlock()
		return TransactionResult{NamespaceResult: commit.NamespaceResult}
	}

	var cleanupErr error
	if req.OperationKind == queue.OperationFailedRecovery {
		cleanupErr = queue.CleanupReplaceIntent(req.ProjectDir, name)
		if cleanupErr != nil {
			s.quarantined[name] = cleanupErr
			s.queueMu.Unlock()
			return TransactionResult{
				NamespaceResult: commit.NamespaceResult,
				CleanupErr:      cleanupErr,
			}
		}
	}
	s.queues[name] = queue.CloneQueue(candidate)
	s.generations[name]++
	if req.OperationKind == queue.OperationFailedRecovery {
		delete(s.quarantined, name)
	}
	resultSnapshot := Snapshot{
		Name:       name,
		Queue:      queue.CloneQueue(candidate),
		Generation: s.generations[name],
	}

	if req.WakeRequired {
		s.Wake()
	}
	if commit.Intent.ArchiveHandoffBinding == nil && req.OperationKind != queue.OperationFailedRecovery {
		cleanupErr = queue.CleanupReplaceIntent(req.ProjectDir, name)
		if cleanupErr != nil {
			s.quarantined[name] = cleanupErr
		}
	}
	s.queueMu.Unlock()
	return TransactionResult{
		NamespaceResult: commit.NamespaceResult,
		Snapshot:        resultSnapshot,
		CleanupErr:      cleanupErr,
	}
}

// Complete executes the QM-053 final-success transaction for one exact live
// snapshot. It retains name ownership on every pre-release failure.
func (s *QueueStore) Complete(ctx context.Context, req queue.CompletionRequest) queue.CompletionResult {
	return s.complete(
		ctx, req, queue.CleanupCompletedCanonical, queue.CleanupReplaceIntent,
		queue.InstallCompletionReleaseMarker,
	)
}

// completionRejected builds the one result shape every pre-commit guard on the
// completion path returns. A rejection is always Outcome=rejected in the
// namespace result AND Phase=rejected; writing the pair out at each guard let
// the two disagree, and only one of them is what callers switch on.
func completionRejected(err error) queue.CompletionResult {
	return queue.CompletionResult{
		NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeRejected, Err: err},
		Phase:           queue.CompletionPhaseRejected,
	}
}

// validateCompletionSnapshotLocked checks the caller's snapshot against live
// store state: the queue is not quarantined, and the snapshot still describes
// what the store holds. The caller MUST hold queueMu — every field this reads
// is guarded by it.
//
// The nil check on Snapshot.Queue is last in its condition on purpose:
// sameQueue answers for a nil argument, and a caller that reordered these
// would turn a stale snapshot into a nil dereference in the identity check
// that runs next.
func (s *QueueStore) validateCompletionSnapshotLocked(req queue.CompletionRequest, name string) error {
	if quarantineErr := s.quarantined[name]; quarantineErr != nil {
		return quarantineErr
	}
	if req.Snapshot.Generation != s.generations[name] ||
		!sameQueue(s.queues[name], req.Snapshot.Queue) ||
		req.Snapshot.Queue == nil {
		return fmt.Errorf("%w: completion snapshot no longer matches the live queue", ErrStaleSnapshot)
	}
	if req.Observe == nil || req.ReleaseTime == nil {
		return errors.New("completion observation and release time source are required")
	}
	return nil
}

// validateCompletionCandidate checks the caller's candidate queue against the
// snapshot's identity, then replays the value-only completion decision the
// candidate claims to be the result of. It reads no store state and holds no
// lock: the snapshot it dereferences has already been validated by
// validateCompletionSnapshotLocked, which is why that call comes first.
func validateCompletionCandidate(req queue.CompletionRequest, name string) error {
	if req.Candidate == nil || req.Candidate.QueueID != req.Snapshot.Queue.QueueID ||
		req.Candidate.Name != name {
		return errors.New("completion candidate does not match snapshot identity")
	}
	if req.ReceiptID != req.DecisionInput.CompletionReceiptID {
		return errors.New("completion receipt does not match decision")
	}
	expected, err := queue.DecideGroupCompletion(*req.Snapshot.Queue, req.DecisionInput)
	if err != nil || expected.Disposition != queue.GroupCompletionDispositionQueueCompleted ||
		!sameQueue(expected.NextQueue, req.Candidate) {
		return errors.Join(errors.New("completion candidate does not match decision"), err)
	}
	return nil
}

func (s *QueueStore) complete(
	ctx context.Context,
	req queue.CompletionRequest,
	cleanupCanonical func(string, string, string, string) error,
	cleanupIntent func(string, string) error,
	installMarker func(string, queue.CompletionReleaseMarkerInputs, time.Time) (queue.CompletionReleaseMarker, error),
) queue.CompletionResult {
	name := queue.NormaliseQueueName(req.Snapshot.Name)
	s.queueMu.Lock()

	if err := s.validateCompletionSnapshotLocked(req, name); err != nil {
		s.queueMu.Unlock()
		return completionRejected(err)
	}
	if err := validateCompletionCandidate(req, name); err != nil { //nolint:contextcheck // Candidate validation must replay the same value-only decision.
		s.queueMu.Unlock()
		return completionRejected(err)
	}
	prepared, err := queue.PrepareCompletion(
		*req.Candidate,
		req.TransactionID,
		req.ReceiptID,
		req.CompletedAt,
	)
	if err != nil {
		s.queueMu.Unlock()
		return completionRejected(err)
	}
	priorBytes, err := json.Marshal(req.Snapshot.Queue)
	if err != nil {
		s.queueMu.Unlock()
		return completionRejected(err)
	}
	commit := queue.WriteReplacement(ctx, queue.ReplacementPlan{
		ProjectDir:               req.ProjectDir,
		TransactionID:            prepared.TransactionID,
		OperationKind:            queue.OperationCompletion,
		NormalizedName:           name,
		QueueID:                  prepared.Candidate.QueueID,
		PriorBytes:               priorBytes,
		CandidateBytes:           prepared.CandidateBytes,
		CompletionReceiptBinding: prepared.Binding,
	})
	result := queue.CompletionResult{
		NamespaceResult: commit.NamespaceResult,
		Phase:           commit.Phase,
		Receipt:         prepared.Receipt,
	}
	if !commit.Committed() {
		if commit.Outcome != queue.OutcomeRejected {
			s.quarantined[name] = commit.Err
		}
		s.queueMu.Unlock()
		return result
	}
	return s.finishCompletion(
		req, prepared, result, name, cleanupCanonical, cleanupIntent,
		installMarker,
	)
}

func (s *QueueStore) finishCompletion(
	req queue.CompletionRequest,
	prepared queue.CompletionPlan,
	result queue.CompletionResult,
	name string,
	cleanupCanonical func(string, string, string, string) error,
	cleanupIntent func(string, string) error,
	installMarker func(string, queue.CompletionReleaseMarkerInputs, time.Time) (queue.CompletionReleaseMarker, error),
) queue.CompletionResult {
	// The completed canonical and receipt are durable. Retain the completed
	// queue in memory until canonical and intent absence are also durable.
	s.queues[name] = queue.CloneQueue(&prepared.Candidate)
	s.generations[name]++
	installedGeneration := s.generations[name]
	s.quarantined[name] = errCompletionObservationInProgress
	s.queueMu.Unlock()

	result.Phase = queue.CompletionPhaseObservationAttempted
	result.ObservationErr = req.Observe(prepared.Receipt)

	s.queueMu.Lock()
	if s.generations[name] != installedGeneration || !sameQueue(s.queues[name], &prepared.Candidate) {
		err := errors.New("completion ownership changed during observation")
		result.CleanupErr = err
		s.quarantined[name] = err
		s.queueMu.Unlock()
		return result
	}
	if err := cleanupCanonical(
		req.ProjectDir,
		name,
		prepared.Candidate.QueueID,
		prepared.Receipt.CompletedQueueSHA256,
	); err != nil {
		result.CleanupErr = err
		s.quarantined[name] = err
		s.queueMu.Unlock()
		return result
	}
	if err := cleanupIntent(req.ProjectDir, name); err != nil {
		result.CleanupErr = err
		s.quarantined[name] = err
		s.queueMu.Unlock()
		return result
	}
	result.Phase = queue.CompletionPhaseCleaned
	delete(s.queues, name)
	delete(s.quarantined, name)
	s.generations[name]++
	result.Phase = queue.CompletionPhaseOwnershipReleased
	s.queueMu.Unlock()
	s.completionMu.Lock()
	defer s.completionMu.Unlock()
	releasedAt := req.ReleaseTime()
	if _, err := installMarker(req.ProjectDir, prepared.MarkerInputs, releasedAt); err != nil {
		result.Phase = queue.CompletionPhaseMarkerFailed
		result.MarkerErr = err
		return result
	}
	result.Phase = queue.CompletionPhaseMarkerDurable
	return result
}

// GarbageCollectCompletionReceipts serializes receipt deletion with marker
// installation. An untrusted observation returns before namespace I/O.
func (s *QueueStore) GarbageCollectCompletionReceipts(
	projectDir string,
	observation queue.CompletionGCObservation,
) ([]queue.CompletionGCResult, error) {
	return s.garbageCollectCompletionReceipts(projectDir, observation, queue.GarbageCollectCompletionReceipts)
}

func (s *QueueStore) garbageCollectCompletionReceipts(
	projectDir string,
	observation queue.CompletionGCObservation,
	collect func(string, queue.CompletionGCObservation) ([]queue.CompletionGCResult, error),
) ([]queue.CompletionGCResult, error) {
	s.completionMu.Lock()
	defer s.completionMu.Unlock()
	return collect(projectDir, observation)
}

// otherQueuesLocked returns a deep copy of every queue except exclude. The
// caller must hold the write lock. Copies are handed out rather than the stored
// pointers because the write path mutates those in place.
func (s *QueueStore) otherQueuesLocked(exclude string) map[string]*queue.Queue {
	others := make(map[string]*queue.Queue, len(s.queues))
	for otherName, otherQueue := range s.queues {
		if otherName == exclude {
			continue
		}
		others[otherName] = queue.CloneQueue(otherQueue)
	}
	return others
}

func rejectedTransaction(err error) TransactionResult {
	return TransactionResult{NamespaceResult: queue.NamespaceResult{
		Outcome: queue.OutcomeRejected,
		Err:     err,
	}}
}

func marshalOptional(q *queue.Queue) ([]byte, error) {
	if q == nil {
		return nil, nil
	}
	return json.Marshal(q)
}

func sameQueue(a, b *queue.Queue) bool {
	aBytes, aErr := marshalOptional(a)
	bBytes, bErr := marshalOptional(b)
	return aErr == nil && bErr == nil && bytes.Equal(aBytes, bBytes)
}
