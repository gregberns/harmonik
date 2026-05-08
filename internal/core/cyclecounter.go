// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import (
	"fmt"
	"sync"
)

// edgeKey is the composite key used to identify a unique directed edge within
// one run. The key is (RunID, from_node NodeID, to_node NodeID) per
// execution-model.md §4.10.EM-043a: a single edge in multiple cycles shares
// one counter per-run.
type edgeKey struct {
	runID    RunID
	fromNode NodeID
	toNode   NodeID
}

// CycleCounter maintains per-(run_id, edge) traversal counts in daemon memory
// per execution-model.md §4.10.EM-043a.
//
// # Authority model
//
// Daemon-memory counters are NON-AUTHORITATIVE across daemon restart. The
// authoritative traversal count across restart is the git-derived count
// obtained by scanning the run's task-branch commit trail and counting prior
// traversals for the edge (edge identified by from_node / to_node on each
// durable Transition record). Callers that resume a run after restart MUST call
// ReconcileFromTransitions before issuing any Increment calls for that run.
//
// # Concurrency
//
// CycleCounter is safe for concurrent use. A single mutex guards the counter
// map; contention is expected to be low since each daemon handles one run at a
// time per MVH.
type CycleCounter struct {
	mu       sync.Mutex
	counters map[edgeKey]uint64
}

// NewCycleCounter allocates a zero-state CycleCounter ready for use.
func NewCycleCounter() *CycleCounter {
	return &CycleCounter{
		counters: make(map[edgeKey]uint64),
	}
}

// Increment increments the traversal counter for the directed edge (from, to)
// within the given run and returns the new count.
//
// If traversalCap is non-nil and the counter has already reached *traversalCap,
// Increment returns an error wrapping ErrCompilationLoop with
// FailureClassCompilationLoop per execution-model.md §4.10.EM-043. The counter
// is NOT incremented when the cap is reached — the traversal is rejected before
// it occurs.
//
// traversalCap must be positive when non-nil; a non-positive value is an
// authoring error and is treated the same as nil (no bounding).
func (c *CycleCounter) Increment(runID RunID, from, to NodeID, traversalCap *int) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := edgeKey{runID: runID, fromNode: from, toNode: to}
	current := c.counters[k]

	if traversalCap != nil && *traversalCap > 0 && current >= uint64(*traversalCap) {
		return current, fmt.Errorf(
			"cyclecounter: traversal cap %d reached for edge (%s → %s) in run %s: %w",
			*traversalCap, from, to, runID, ErrCompilationLoop,
		)
	}

	c.counters[k] = current + 1
	return c.counters[k], nil
}

// Get returns the current traversal count for the directed edge (from, to)
// within the given run. Returns 0 if the edge has never been traversed in this
// run.
func (c *CycleCounter) Get(runID RunID, from, to NodeID) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counters[edgeKey{runID: runID, fromNode: from, toNode: to}]
}

// Reset removes all traversal counters for the given run. It is called when a
// run terminates (successfully or with failure) to release memory.
func (c *CycleCounter) Reset(runID RunID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k := range c.counters {
		if k.runID == runID {
			delete(c.counters, k)
		}
	}
}

// ReconcileFromTransitions reconstructs traversal counts for a run from a
// slice of prior Transition records.
//
// This is the restart-recovery path per EM-043a. The daemon calls this after
// scanning the run's task-branch git commit trail and deserialising each
// durable Transition record. The edge is identified by
// (Transition.FromState.NodeID, Transition.ToState.NodeID) per EM-043a.
//
// ReconcileFromTransitions does NOT depend on git plumbing. The caller
// (typically a git-history adapter outside internal/core) supplies the
// pre-scanned slice. Only transitions whose RunID matches runID are counted;
// transitions for other runs are silently skipped, allowing a mixed-run slice
// to be passed without pre-filtering.
//
// Any existing in-memory counters for runID are replaced by the reconciled
// counts (not added to). This guarantees idempotency: calling Reconcile twice
// with the same slice produces the same state as calling it once.
func (c *CycleCounter) ReconcileFromTransitions(runID RunID, transitions []Transition) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build fresh counts from the supplied slice.
	fresh := make(map[edgeKey]uint64)
	for i := range transitions {
		tr := &transitions[i]
		if tr.RunID != runID {
			continue
		}
		k := edgeKey{
			runID:    runID,
			fromNode: tr.FromState.NodeID,
			toNode:   tr.ToState.NodeID,
		}
		fresh[k]++
	}

	// Remove stale in-memory entries for this run.
	for k := range c.counters {
		if k.runID == runID {
			delete(c.counters, k)
		}
	}

	// Install reconciled counts.
	for k, v := range fresh {
		c.counters[k] = v
	}
}

// ErrCompilationLoop is the sentinel error returned by CycleCounter.Increment
// when a per-edge traversal cap is reached (execution-model.md §4.10.EM-043,
// §8.6). Callers SHOULD use errors.Is to test for this sentinel.
//
// The FailureClass value emitted on the run_failed event payload is
// FailureClassCompilationLoop; this sentinel is the Go-layer signal that
// triggers that class assignment. The handler is never consulted at cap-hit
// per §8.6 and OQ-EM-006 (daemon-observed only; ErrCompilationLoop is NOT
// one of the five handler-contract sentinels in handler-contract.md §4.5).
var ErrCompilationLoop = fmt.Errorf("failure class %s", FailureClassCompilationLoop)
