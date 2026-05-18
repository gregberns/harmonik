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
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// RunHandle holds the live metadata for a single in-flight bead run. It is
// stored by pointer in RunRegistry so that callers can update fields (e.g.
// attach a cancel func) without removing and re-inserting.
type RunHandle struct {
	// BeadID is the bead being executed during this run.
	BeadID core.BeadID

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

// Len returns the number of currently registered in-flight runs.
func (r *RunRegistry) Len() int {
	r.mu.RLock()
	n := len(r.handles)
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
