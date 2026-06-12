package daemon

// runregistry.go — in-flight run registry for the harmonik daemon (hk-7s9z9).
//
// RunRegistry tracks all currently executing bead runs inside the daemon
// process. It is a field on the Daemon struct (NOT a package-level variable —
// package-level globals break concurrent tests per
// POST_MVH_PARALLELISM_ROADMAP.md §6 anti-pattern).
//
// The registry is the foundation for concurrent throughput (roadmap row #4 /
// blocker E). Once the work loop (row #5) launches goroutine-per-bead it will
// Register on claim and Unregister after the bead closes. MaxConcurrent
// enforcement (row #6) reads Len() before accepting a new claim.
//
// Spec ref: POST_MVH_PARALLELISM_ROADMAP.md §1 blocker E, §3 row #4.
// Bead: hk-7s9z9.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// RunHandle holds the live metadata for a single in-flight bead run. It is
// stored by pointer in RunRegistry so that callers can update fields (e.g.
// attach a cancel func) without removing and re-inserting.
type RunHandle struct {
	// BeadID is the bead being executed during this run.
	BeadID core.BeadID

	// QueueName is the durable routing key (queue.Queue.Name) of the queue that
	// dispatched this run. Used by the two-level capacity gate to compute the
	// per-queue in-flight tally (LenForQueue) so each named queue is bounded by
	// its own Queue.Workers count while the bare Len() stays the global ceiling
	// (specs/queue-model.md §9.3 QM-062). Empty for br-ready-fallback runs (no
	// queue); those count only against the global ceiling.
	//
	// Bead ref: hk-tigaf.4 (NQ-B1).
	QueueName string

	// Labels holds the raw bead label strings copied from BeadRecord.Labels at
	// registration time. Used by StaleWatcher to apply per-bead overrides (e.g.
	// "stale_after=<seconds>").
	Labels []string

	// WorktreePath is the absolute path of the git worktree created for this
	// run (workspace-model.md §4.1 WM-003).
	WorktreePath string

	// Watcher is the per-run NDJSON reader attached to the handler subprocess
	// stdout. May be nil before the handler is launched.
	Watcher *handlercontract.Watcher

	// StartedAt is the wall-clock time when Register was called.
	StartedAt time.Time

	// Cancel is the context cancel function for this run's goroutine. Calling
	// it signals the handler to stop. May be nil if the run was registered
	// before a cancel function was available.
	Cancel context.CancelFunc //nolint:containedctx // CancelFunc is not a Context; stored for operator signal routing

	// OwningEpicID is the BeadID of the parent epic for this run's bead (hk-7evda).
	// Empty when the bead has no parent epic. Set at run start by beadRunOne via
	// resolveOwningEpicFromRecord; read by StaleWatcher and terminal event emitters
	// to denormalize attribution and eliminate br round-trips (logmine F13).
	OwningEpicID string

	// OwningEpicAssignee is the crew name assigned to OwningEpicID (hk-7evda).
	// Empty when OwningEpicID is empty or the epic has no assignee. Mirrors the
	// captain's `br update <epic> --assignee <crew>` attribution durable marker so
	// operators can attribute run events without additional br show calls.
	OwningEpicAssignee string

	// machine is the per-session lifecycle FSM (HC-064..HC-067). Set atomically
	// by beadRunOne after a successful handler.Launch; nil before the handler
	// is launched. Use SetMachine / GetMachine for race-free access.
	machine atomic.Pointer[hclifecycle.Machine]
}

// SetMachine stores m as the lifecycle Machine for this run. Thread-safe.
// Called by beadRunOne immediately after a successful handler.Launch.
func (h *RunHandle) SetMachine(m *hclifecycle.Machine) {
	h.machine.Store(m)
}

// GetMachine returns the lifecycle Machine for this run, or nil if the
// handler has not yet launched. Thread-safe.
func (h *RunHandle) GetMachine() *hclifecycle.Machine {
	return h.machine.Load()
}

// RunRegistry is a concurrency-safe map of run_id → *RunHandle.
//
// It is safe to call Register, Unregister, Get, Len, and Snapshot from
// multiple goroutines simultaneously.
type RunRegistry struct {
	mu      sync.RWMutex
	handles map[core.RunID]*RunHandle
}

// NewRunRegistry returns an empty, ready-to-use RunRegistry.
func NewRunRegistry() *RunRegistry {
	return &RunRegistry{
		handles: make(map[core.RunID]*RunHandle),
	}
}

// Register adds handle under runID. If runID is already present, Register
// overwrites the existing entry (last-writer wins; the caller is responsible
// for ensuring uniqueness across concurrent runs).
func (r *RunRegistry) Register(runID core.RunID, handle *RunHandle) {
	r.mu.Lock()
	r.handles[runID] = handle
	r.mu.Unlock()
}

// Unregister removes the entry for runID. It is a no-op if runID is not
// present.
func (r *RunRegistry) Unregister(runID core.RunID) {
	r.mu.Lock()
	delete(r.handles, runID)
	r.mu.Unlock()
}

// Get returns the RunHandle for runID, or (nil, false) if not found.
func (r *RunRegistry) Get(runID core.RunID) (*RunHandle, bool) {
	r.mu.RLock()
	h, ok := r.handles[runID]
	r.mu.RUnlock()
	return h, ok
}

// Len returns the number of currently registered in-flight runs. This is the
// GLOBAL ceiling tally consumed by the work-loop's --max-concurrent gate
// (specs/execution-model.md §4.3 EM-049): it counts every in-flight run across
// all queues plus any br-ready-fallback runs.
func (r *RunRegistry) Len() int {
	r.mu.RLock()
	n := len(r.handles)
	r.mu.RUnlock()
	return n
}

// LenForQueue returns the number of currently registered in-flight runs whose
// QueueName equals name. This is the PER-QUEUE tally consumed by the two-level
// capacity gate (specs/queue-model.md §9.3 QM-062): a named queue may dispatch
// only while LenForQueue(name) < Queue.Workers, independent of the global
// ceiling enforced by Len(). br-ready-fallback runs (empty QueueName) are
// excluded from every named-queue tally; pass "" to count them explicitly.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
func (r *RunRegistry) LenForQueue(name string) int {
	r.mu.RLock()
	n := 0
	for _, h := range r.handles {
		if h.QueueName == name {
			n++
		}
	}
	r.mu.RUnlock()
	return n
}

// Snapshot returns a stable slice of all currently registered RunHandles.
// The slice is a shallow copy: mutations to RunHandle fields via the returned
// pointers are visible to other callers, but additions/deletions to the
// registry after Snapshot returns are not reflected in the returned slice.
func (r *RunRegistry) Snapshot() []*RunHandle {
	r.mu.RLock()
	out := make([]*RunHandle, 0, len(r.handles))
	for _, h := range r.handles {
		out = append(out, h)
	}
	r.mu.RUnlock()
	return out
}

// snapshotWithKeys returns a stable map copy of all currently registered
// (runID → *RunHandle) entries.  This is the key-preserving variant of
// Snapshot, used internally by HandlerPausePolicyGoroutine to build the
// in-flight freeze-list (hk-37zy8).
//
// The map is a shallow copy: keys are value-copied (RunID is a UUIDv7 value
// type), and RunHandle pointers are copied (not the structs they point to).
func (r *RunRegistry) snapshotWithKeys() map[core.RunID]*RunHandle {
	r.mu.RLock()
	out := make(map[core.RunID]*RunHandle, len(r.handles))
	for id, h := range r.handles {
		out[id] = h
	}
	r.mu.RUnlock()
	return out
}
