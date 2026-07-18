package workers

import (
	"fmt"
	"sync"
)

// Registry wraps a loaded Config and provides per-bead worker selection with
// slot tracking and live-disable support (remote-substrate B5).
type Registry struct {
	mu        sync.Mutex
	worker    Worker
	hasWorker bool
	inFlight  int
}

// PrimaryWorkerIndex returns the index of the worker the Registry consumes as
// THE active remote worker, or -1 when cfg configures no workers. It is the
// single source of truth for "which worker is live" so that callers applying
// CLI overrides (applyWorkerOverrides) target the same entry NewRegistry selects
// and cannot drift to a different index than the one dispatch actually uses.
func PrimaryWorkerIndex(cfg Config) int {
	if len(cfg.Workers) == 0 {
		return -1
	}
	return 0
}

// NewRegistry constructs a Registry from a loaded Config.
// If cfg has no workers, SelectWorker always returns nil (local fallback).
func NewRegistry(cfg Config) *Registry {
	r := &Registry{}
	if i := PrimaryWorkerIndex(cfg); i >= 0 {
		r.worker = cfg.Workers[i]
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

// SelectWorkerByName returns the configured worker when its Name matches
// target, it is enabled, and has a free slot — atomically reserving that slot.
// Returns nil when target is empty, no worker is configured, the worker's Name
// does not match target, the worker is disabled, or all slots are occupied.
//
// The caller MUST call ReleaseSlot after the remote run completes.
// Bead ref: hk-f10xl [L5 Move 2 — per-queue WorkerTarget pin].
func (r *Registry) SelectWorkerByName(target string) *Worker {
	if target == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasWorker {
		return nil
	}
	if r.worker.Name != target {
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

// HasFreeSlot returns true when the configured worker is enabled and has at
// least one free slot. It does NOT reserve the slot — use SelectWorker to
// atomically check and reserve. Used by the split capacity gate (hk-hs7ex)
// as a non-consuming peek.
func (r *Registry) HasFreeSlot() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasWorker || !r.worker.Enabled {
		return false
	}
	return r.worker.MaxSlots <= 0 || r.inFlight < r.worker.MaxSlots
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

// SetEnabledByName flips the Enabled flag on the configured worker iff its Name
// matches name, returning the worker's name on success. It is the operator-facing
// live toggle behind `harmonik worker enable/disable` (hk-xjbvi): unlike
// SetEnabled(bool) — used by the boot health check, which knows it is acting on
// the single configured worker — this validates the caller-supplied name so an
// unknown name is rejected rather than silently flipping the wrong (only) worker.
//
// Returns an error when no worker is configured, or when name does not match the
// (single, v1) configured worker's Name. On success the next SelectWorker call
// observes the new Enabled state immediately (no restart), exactly like SetEnabled.
func (r *Registry) SetEnabledByName(name string, v bool) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasWorker {
		return "", fmt.Errorf("no remote worker configured")
	}
	if r.worker.Name != name {
		return "", fmt.Errorf("no such worker %q (configured worker is %q)", name, r.worker.Name)
	}
	r.worker.Enabled = v
	return r.worker.Name, nil
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
