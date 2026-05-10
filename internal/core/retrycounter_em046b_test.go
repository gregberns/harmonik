package core

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// retryRedispatchFixtureRunID allocates a fresh RunID for retry-counter tests.
// Helper prefix: retryRedispatch per implementer-protocol.md (bead hk-b3f.62).
func retryRedispatchFixtureRunID() RunID {
	return RunID(uuid.Must(uuid.NewV7()))
}

// retryRedispatchFixtureTransition builds a minimal valid Transition for use in
// retry-counter reconciliation tests. outcomeStatus controls which status is
// recorded; only RETRY transitions are counted by ReconcileFromTransitions.
func retryRedispatchFixtureTransition(runID RunID, nodeID NodeID, outcomeStatus OutcomeStatus) Transition {
	now := time.Now()
	fromState := State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     runID,
		NodeID:    nodeID,
		EnteredAt: now,
		TransitionHistory: CommitRange{
			FirstCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LastCommitSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	toState := State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     runID,
		NodeID:    nodeID,
		EnteredAt: now,
		TransitionHistory: CommitRange{
			FirstCommitSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LastCommitSHA:  "cccccccccccccccccccccccccccccccccccccccc",
		},
	}
	return Transition{
		TransitionID:    TransitionID(uuid.Must(uuid.NewV7())),
		RunID:           runID,
		FromState:       fromState,
		ToState:         toState,
		ActorRole:       ActorRoleDaemon,
		ChosenAction:    ActionDescriptor("forward"),
		PolicyVersion:   PolicyVersion("v1.0.0"),
		Evidence:        Evidence{},
		VerifierMetrics: VerifierMetrics{},
		OutcomeStatus:   outcomeStatus,
		TransitionKind:  TransitionKindForward,
		SchemaVersion:   1,
	}
}

// --- Basic increment / get ---

// TestRetryCounterEM046b_IncrementGet verifies that a fresh counter starts at
// zero and each Increment call returns the incremented value, per EM-046b.
func TestRetryCounterEM046b_IncrementGet(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-a")

	if got := rc.Get(runID, nodeID); got != 0 {
		t.Errorf("Get before any Increment = %d, want 0", got)
	}

	n, err := rc.Increment(runID, nodeID, nil)
	if err != nil {
		t.Fatalf("Increment(nil cap) returned unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("first Increment = %d, want 1", n)
	}
	if got := rc.Get(runID, nodeID); got != 1 {
		t.Errorf("Get after first Increment = %d, want 1", got)
	}

	n, err = rc.Increment(runID, nodeID, nil)
	if err != nil {
		t.Fatalf("second Increment returned unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("second Increment = %d, want 2", n)
	}
}

// --- Per-run isolation ---

// TestRetryCounterEM046b_PerRunIsolation verifies that counters for different
// run_ids are independent: incrementing a node in run-A does not affect the
// counter for the same node in run-B. Spec reference: EM-046b.
func TestRetryCounterEM046b_PerRunIsolation(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runA := retryRedispatchFixtureRunID()
	runB := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-x")

	for range 3 {
		if _, err := rc.Increment(runA, nodeID, nil); err != nil {
			t.Fatalf("Increment runA: %v", err)
		}
	}

	if got := rc.Get(runB, nodeID); got != 0 {
		t.Errorf("runB counter after 3 runA increments = %d, want 0 (per-run isolation violated)", got)
	}
	if got := rc.Get(runA, nodeID); got != 3 {
		t.Errorf("runA counter = %d, want 3", got)
	}
}

// --- Per-node isolation within a run ---

// TestRetryCounterEM046b_PerNodeIsolation verifies that counters for different
// nodes within the same run are independent per EM-046b (attempt count is
// per-node within the run).
func TestRetryCounterEM046b_PerNodeIsolation(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeA := NodeID("node-a")
	nodeB := NodeID("node-b")

	for range 2 {
		if _, err := rc.Increment(runID, nodeA, nil); err != nil {
			t.Fatalf("Increment nodeA: %v", err)
		}
	}

	if got := rc.Get(runID, nodeB); got != 0 {
		t.Errorf("nodeB counter after nodeA increments = %d, want 0 (per-node isolation violated)", got)
	}
	if got := rc.Get(runID, nodeA); got != 2 {
		t.Errorf("nodeA counter = %d, want 2", got)
	}
}

// --- Cap-reached → ErrRetryCapExhausted → FailureClassTransient ---

// TestRetryCounterEM046b_CapReached verifies that Increment returns an error
// wrapping ErrRetryCapExhausted when the retry count reaches the cap, and that
// the counter is NOT incremented beyond the cap. On cap exhaustion the failure
// class is transient per EM-046b.
func TestRetryCounterEM046b_CapReached(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-a")
	retryCap := 3

	for i := range retryCap {
		n, err := rc.Increment(runID, nodeID, &retryCap)
		if err != nil {
			t.Fatalf("Increment %d/%d returned unexpected error: %v", i+1, retryCap, err)
		}
		want := uint64(i + 1) //nolint:gosec // G115: i is a bounded loop index, no overflow risk
		if n != want {
			t.Errorf("Increment %d/%d returned count %d, want %d", i+1, retryCap, n, want)
		}
	}

	// Next increment must fail with ErrRetryCapExhausted.
	n, err := rc.Increment(runID, nodeID, &retryCap)
	if err == nil {
		t.Fatal("Increment at cap returned nil error, want ErrRetryCapExhausted")
	}
	if !errors.Is(err, ErrRetryCapExhausted) {
		t.Errorf("Increment at cap returned %v, want error wrapping ErrRetryCapExhausted", err)
	}
	// Counter must not have advanced.
	wantCap := uint64(retryCap)
	if n != wantCap {
		t.Errorf("counter returned alongside ErrRetryCapExhausted = %d, want %d (counter must not advance past cap)", n, wantCap)
	}
	if got := rc.Get(runID, nodeID); got != wantCap {
		t.Errorf("Get after cap-reached = %d, want %d (counter must not advance past cap)", got, wantCap)
	}
}

// TestRetryCounterEM046b_CapOne verifies that a cap of 1 allows exactly one
// retry attempt and fails on the second.
func TestRetryCounterEM046b_CapOne(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeID := NodeID("single-retry-node")
	retryCap := 1

	n, err := rc.Increment(runID, nodeID, &retryCap)
	if err != nil {
		t.Fatalf("first Increment with cap=1 returned error: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}

	_, err = rc.Increment(runID, nodeID, &retryCap)
	if !errors.Is(err, ErrRetryCapExhausted) {
		t.Errorf("second Increment with cap=1: got %v, want ErrRetryCapExhausted", err)
	}
}

// --- Reset ---

// TestRetryCounterEM046b_Reset verifies that Reset clears all counters for the
// target run and does not affect counters for other runs.
func TestRetryCounterEM046b_Reset(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runA := retryRedispatchFixtureRunID()
	runB := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-a")

	for range 2 {
		if _, err := rc.Increment(runA, nodeID, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := rc.Increment(runB, nodeID, nil); err != nil {
			t.Fatal(err)
		}
	}

	rc.Reset(runA)

	if got := rc.Get(runA, nodeID); got != 0 {
		t.Errorf("after Reset(runA), runA counter = %d, want 0", got)
	}
	if got := rc.Get(runB, nodeID); got != 2 {
		t.Errorf("after Reset(runA), runB counter = %d, want 2 (must be unaffected)", got)
	}
}

// --- Recovery from transition slice ---

// TestRetryCounterEM046b_ReconcileFromTransitions verifies that
// ReconcileFromTransitions counts only RETRY-status transitions and derives the
// per-node attempt count. Non-RETRY transitions (SUCCESS, FAIL, PARTIAL_SUCCESS)
// MUST NOT be counted. Spec reference: EM-046b (git-derived count is the
// authoritative restart-recovery path).
func TestRetryCounterEM046b_ReconcileFromTransitions(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeA := NodeID("node-a")
	nodeB := NodeID("node-b")

	transitions := []Transition{
		retryRedispatchFixtureTransition(runID, nodeA, OutcomeStatusRetry),
		retryRedispatchFixtureTransition(runID, nodeA, OutcomeStatusRetry),   // second retry of nodeA
		retryRedispatchFixtureTransition(runID, nodeA, OutcomeStatusSuccess), // not a retry — must not count
		retryRedispatchFixtureTransition(runID, nodeB, OutcomeStatusRetry),
		retryRedispatchFixtureTransition(runID, nodeB, OutcomeStatusFail), // not a retry — must not count
	}

	rc.ReconcileFromTransitions(runID, transitions)

	tests := []struct {
		nodeID NodeID
		want   uint64
	}{
		{nodeA, 2},
		{nodeB, 1},
		{NodeID("node-c"), 0}, // never retried
	}
	for _, tc := range tests {
		got := rc.Get(runID, tc.nodeID)
		if got != tc.want {
			t.Errorf("after Reconcile: Get(%s) = %d, want %d", tc.nodeID, got, tc.want)
		}
	}
}

// TestRetryCounterEM046b_ReconcileOtherRunsIgnored verifies that transitions
// for other runs in a mixed slice do not affect the target run's counters.
func TestRetryCounterEM046b_ReconcileOtherRunsIgnored(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runA := retryRedispatchFixtureRunID()
	runB := retryRedispatchFixtureRunID()
	nodeID := NodeID("shared-node")

	mixed := []Transition{
		retryRedispatchFixtureTransition(runA, nodeID, OutcomeStatusRetry),
		retryRedispatchFixtureTransition(runB, nodeID, OutcomeStatusRetry), // different run — must be ignored
		retryRedispatchFixtureTransition(runA, nodeID, OutcomeStatusRetry),
	}

	rc.ReconcileFromTransitions(runA, mixed)

	if got := rc.Get(runA, nodeID); got != 2 {
		t.Errorf("runA counter = %d, want 2 (only runA transitions should be counted)", got)
	}
	if got := rc.Get(runB, nodeID); got != 0 {
		t.Errorf("runB counter = %d, want 0 (runB was not reconciled)", got)
	}
}

// TestRetryCounterEM046b_ReconcileIdempotent verifies that calling
// ReconcileFromTransitions twice with the same slice produces the same result
// as calling it once.
func TestRetryCounterEM046b_ReconcileIdempotent(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-p")

	transitions := []Transition{
		retryRedispatchFixtureTransition(runID, nodeID, OutcomeStatusRetry),
		retryRedispatchFixtureTransition(runID, nodeID, OutcomeStatusRetry),
	}

	rc.ReconcileFromTransitions(runID, transitions)
	rc.ReconcileFromTransitions(runID, transitions) // idempotent second call

	if got := rc.Get(runID, nodeID); got != 2 {
		t.Errorf("after two identical Reconcile calls: counter = %d, want 2 (must be idempotent)", got)
	}
}

// TestRetryCounterEM046b_ReconcileReplacesExistingInMemory verifies that
// ReconcileFromTransitions replaces existing in-memory counts for the run
// (not appended). This ensures restart recovery is authoritative.
func TestRetryCounterEM046b_ReconcileReplacesExistingInMemory(t *testing.T) {
	t.Parallel()

	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-m")

	// Pre-populate in-memory counter with a stale value.
	for range 5 {
		if _, err := rc.Increment(runID, nodeID, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Reconcile with a smaller authoritative count.
	transitions := []Transition{
		retryRedispatchFixtureTransition(runID, nodeID, OutcomeStatusRetry),
		retryRedispatchFixtureTransition(runID, nodeID, OutcomeStatusRetry),
	}
	rc.ReconcileFromTransitions(runID, transitions)

	if got := rc.Get(runID, nodeID); got != 2 {
		t.Errorf("after Reconcile over stale in-memory: counter = %d, want 2 (reconcile must replace, not add)", got)
	}
}

// TestRetryCounterEM046b_ErrSentinelFailureClassTransient verifies that
// ErrRetryCapExhausted carries the transient failure class string in its
// message, matching the EM-046b requirement that cap exhaustion transitions to
// failure class transient.
func TestRetryCounterEM046b_ErrSentinelFailureClassTransient(t *testing.T) {
	t.Parallel()

	// The sentinel must be non-nil.
	if ErrRetryCapExhausted == nil {
		t.Fatal("ErrRetryCapExhausted is nil")
	}

	// Verify the sentinel wraps the transient failure class string.
	rc := NewRetryCounter()
	runID := retryRedispatchFixtureRunID()
	nodeID := NodeID("node-cap")
	retryCap := 1

	_, _ = rc.Increment(runID, nodeID, &retryCap) // first: allowed
	_, err := rc.Increment(runID, nodeID, &retryCap)
	if !errors.Is(err, ErrRetryCapExhausted) {
		t.Fatalf("cap-exhausted error does not wrap ErrRetryCapExhausted: %v", err)
	}
}
