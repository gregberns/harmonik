// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import (
	"errors"
	"fmt"
	"sync"
)

// retryKey is the composite key used to identify a per-node retry count within
// one run. The key is (RunID, NodeID) per execution-model.md §4.10.EM-046b:
// attempt count is tracked per-node within a run.
type retryKey struct {
	runID  RunID
	nodeID NodeID
}

// RetryCounter maintains per-(run_id, node_id) attempt counts in daemon memory
// per execution-model.md §4.10.EM-046b.
//
// # Authority model
//
// Daemon-memory counters are NON-AUTHORITATIVE across daemon restart. The
// authoritative retry count across restart is the git-derived count obtained by
// scanning the run's task-branch commit trail and counting prior Transition
// records whose OutcomeStatus == RETRY for the given node (identified by
// FromState.NodeID on each Transition). Callers that resume a run after restart
// MUST call ReconcileFromTransitions before issuing any Increment calls for that
// run.
//
// # Concurrency
//
// RetryCounter is safe for concurrent use. A single mutex guards the counter
// map; contention is expected to be low since each daemon handles one run at a
// time per MVH.
type RetryCounter struct {
	mu       sync.Mutex
	counters map[retryKey]uint64
}

// NewRetryCounter allocates a zero-state RetryCounter ready for use.
func NewRetryCounter() *RetryCounter {
	return &RetryCounter{
		counters: make(map[retryKey]uint64),
	}
}

// Increment increments the retry attempt counter for the given node within the
// given run and returns the new count.
//
// If retryCap is non-nil and the counter has already reached *retryCap,
// Increment returns an error wrapping ErrRetryCapExhausted with
// FailureClassTransient per execution-model.md §4.10.EM-046b. The counter is
// NOT incremented when the cap is reached — the retry is rejected before it
// occurs.
//
// retryCap must be positive when non-nil; a non-positive value is an authoring
// error and is treated the same as nil (no bounding).
func (r *RetryCounter) Increment(runID RunID, nodeID NodeID, retryCap *int) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := retryKey{runID: runID, nodeID: nodeID}
	current := r.counters[k]

	if retryCap != nil && *retryCap > 0 && current >= uint64(*retryCap) {
		return current, fmt.Errorf(
			"retrycounter: retry cap %d reached for node %s in run %s: %w",
			*retryCap, nodeID, runID, ErrRetryCapExhausted,
		)
	}

	r.counters[k] = current + 1
	return r.counters[k], nil
}

// Get returns the current retry attempt count for the given node within the
// given run. Returns 0 if the node has never been retried in this run.
func (r *RetryCounter) Get(runID RunID, nodeID NodeID) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.counters[retryKey{runID: runID, nodeID: nodeID}]
}

// Reset removes all retry counters for the given run. It is called when a run
// terminates (successfully or with failure) to release memory.
func (r *RetryCounter) Reset(runID RunID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k := range r.counters {
		if k.runID == runID {
			delete(r.counters, k)
		}
	}
}

// ReconcileFromTransitions reconstructs retry attempt counts for a run from a
// slice of prior Transition records.
//
// This is the restart-recovery path per EM-046b. The daemon calls this after
// scanning the run's task-branch git commit trail and deserialising each
// Transition record. Only transitions whose RunID matches runID AND whose
// OutcomeStatus == OutcomeStatusRetry are counted; other transitions are
// silently skipped. The node is identified by Transition.FromState.NodeID
// (the node that was re-dispatched).
//
// ReconcileFromTransitions does NOT depend on git plumbing. The caller
// (typically a git-history adapter outside internal/core) supplies the
// pre-scanned slice.
//
// Any existing in-memory counters for runID are replaced by the reconciled
// counts (not added to). This guarantees idempotency: calling Reconcile twice
// with the same slice produces the same state as calling it once.
func (r *RetryCounter) ReconcileFromTransitions(runID RunID, transitions []Transition) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Build fresh counts from the supplied slice.
	fresh := make(map[retryKey]uint64)
	for i := range transitions {
		tr := &transitions[i]
		if tr.RunID != runID {
			continue
		}
		if tr.OutcomeStatus != OutcomeStatusRetry {
			continue
		}
		k := retryKey{
			runID:  runID,
			nodeID: tr.FromState.NodeID,
		}
		fresh[k]++
	}

	// Remove stale in-memory entries for this run.
	for k := range r.counters {
		if k.runID == runID {
			delete(r.counters, k)
		}
	}

	// Install reconciled counts.
	for k, v := range fresh {
		r.counters[k] = v
	}
}

// ErrRetryCapExhausted is the sentinel error returned by RetryCounter.Increment
// when a per-node retry cap is reached (execution-model.md §4.10.EM-046b).
// Callers SHOULD use errors.Is to test for this sentinel.
//
// The FailureClass emitted on the run_failed event payload when the cap is
// exhausted is FailureClassTransient per EM-046b; this sentinel is the Go-layer
// signal that triggers that class assignment.
var ErrRetryCapExhausted = errors.New("failure class " + string(FailureClassTransient) + ": retry cap exhausted")
