package workers

import "sync"

// Registry wraps a loaded Config and provides per-bead worker selection with
// slot tracking and live-disable support (remote-substrate B5).
type Registry struct {
	mu        sync.Mutex
	worker    Worker
	hasWorker bool
	inFlight  int
}

// NewRegistry constructs a Registry from a loaded Config.
// If cfg has no workers, SelectWorker always returns nil (local fallback).
func NewRegistry(cfg Config) *Registry {
	r := &Registry{}
	if len(cfg.Workers) > 0 {
		r.worker = cfg.Workers[0]
		r.hasWorker = true
	}
	return r
}

// SelectWorker returns the configured worker when it is enabled and has a free
// slot, atomically reserving that slot. Returns nil when:
//   - no worker is configured (local-only path, NFR7)
//   - the worker is Enabled == false (live-disable, FR12)
//   - all slots are occupied (inFlight >= MaxSlots, MaxSlots > 0)
//
// Enabled is re-read on every call so a SetEnabled(false) takes effect
// immediately on the next SelectWorker call (FR12).
//
// The caller MUST call ReleaseSlot after the remote run completes.
func (r *Registry) SelectWorker() *Worker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasWorker {
		return nil
	}
	if !r.worker.Enabled {
		return nil
	}
	if r.worker.MaxSlots > 0 && r.inFlight >= r.worker.MaxSlots {
		return nil
	}
	r.inFlight++
	w := r.worker
	return &w
}

// ReleaseSlot decrements the in-flight count when a remote run finishes.
// It is a no-op if no worker is configured or the count is already zero.
func (r *Registry) ReleaseSlot() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hasWorker && r.inFlight > 0 {
		r.inFlight--
	}
}

// SetEnabled updates the Enabled flag on the configured worker (live-disable,
// FR12). The next SelectWorker call will observe the new value immediately.
func (r *Registry) SetEnabled(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hasWorker {
		r.worker.Enabled = v
	}
}

// InFlight returns the current count of reserved remote slots.
func (r *Registry) InFlight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inFlight
}

// WorkerSnapshot returns a copy of the configured worker with its CURRENT
// Enabled state (which the boot health check may have flipped via SetEnabled),
// or nil when no worker is configured. It does NOT reserve a slot — unlike
// SelectWorker — so the recurring worker-report poll (WR3) can read live-disable
// state without perturbing dispatch capacity.
func (r *Registry) WorkerSnapshot() *Worker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasWorker {
		return nil
	}
	w := r.worker
	return &w
}
