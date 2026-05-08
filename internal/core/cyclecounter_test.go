package core

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// cycleCounterFixtureRunID allocates a fresh RunID for cycle-counter tests.
// Helper prefix: hk-b3f.57-impl per implementer-protocol.md.
func cycleCounterFixtureRunID() RunID {
	return RunID(uuid.Must(uuid.NewV7()))
}

// cycleCounterFixtureTransition builds a minimal valid Transition for use in
// reconciliation tests. Only RunID, FromState.NodeID, and ToState.NodeID are
// meaningful for the cycle counter; other fields carry plausible sentinel values.
func cycleCounterFixtureTransition(runID RunID, from, to NodeID) Transition {
	now := time.Now()
	fromState := State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     runID,
		NodeID:    from,
		EnteredAt: now,
		TransitionHistory: CommitRange{
			FirstCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LastCommitSHA:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	toState := State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     runID,
		NodeID:    to,
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
		OutcomeStatus:   OutcomeStatusSuccess,
		TransitionKind:  TransitionKindForward,
		SchemaVersion:   1,
	}
}

// --- Basic increment / get ---

// TestCycleCounterEM043_IncrementGet verifies that a fresh counter starts at
// zero and each Increment call returns the incremented value, per EM-043a.
func TestCycleCounterEM043_IncrementGet(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()
	from := NodeID("node-a")
	to := NodeID("node-b")

	if got := cc.Get(runID, from, to); got != 0 {
		t.Errorf("Get before any Increment = %d, want 0", got)
	}

	n, err := cc.Increment(runID, from, to, nil)
	if err != nil {
		t.Fatalf("Increment(nil cap) returned unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("first Increment = %d, want 1", n)
	}
	if got := cc.Get(runID, from, to); got != 1 {
		t.Errorf("Get after first Increment = %d, want 1", got)
	}

	n, err = cc.Increment(runID, from, to, nil)
	if err != nil {
		t.Fatalf("second Increment returned unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("second Increment = %d, want 2", n)
	}
}

// --- Per-run isolation ---

// TestCycleCounterEM043_PerRunIsolation verifies that counters for different
// run_ids are independent: incrementing an edge in run-A does not affect the
// counter for the same edge in run-B. Spec reference: EM-043a.
func TestCycleCounterEM043_PerRunIsolation(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runA := cycleCounterFixtureRunID()
	runB := cycleCounterFixtureRunID()
	from := NodeID("node-x")
	to := NodeID("node-y")

	for range 3 {
		if _, err := cc.Increment(runA, from, to, nil); err != nil {
			t.Fatalf("Increment runA: %v", err)
		}
	}

	if got := cc.Get(runB, from, to); got != 0 {
		t.Errorf("runB counter after 3 runA increments = %d, want 0 (per-run isolation violated)", got)
	}

	if got := cc.Get(runA, from, to); got != 3 {
		t.Errorf("runA counter = %d, want 3", got)
	}
}

// --- Cap-reached → ErrCompilationLoop ---

// TestCycleCounterEM043_CapReached verifies that Increment returns an error
// wrapping ErrCompilationLoop when the traversal count reaches the cap, and
// that the counter is NOT incremented beyond the cap. Spec reference: EM-043,
// §8.6.
func TestCycleCounterEM043_CapReached(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()
	from := NodeID("node-a")
	to := NodeID("node-b")
	traversalCap := 3

	for i := range traversalCap {
		n, err := cc.Increment(runID, from, to, &traversalCap)
		if err != nil {
			t.Fatalf("Increment %d/%d returned unexpected error: %v", i+1, traversalCap, err)
		}
		want := uint64(i + 1) //nolint:gosec // G115: i is a bounded loop index, no overflow risk
		if n != want {
			t.Errorf("Increment %d/%d returned count %d, want %d", i+1, traversalCap, n, want)
		}
	}

	// Next increment must fail with ErrCompilationLoop.
	n, err := cc.Increment(runID, from, to, &traversalCap)
	if err == nil {
		t.Fatal("Increment at cap returned nil error, want ErrCompilationLoop")
	}
	if !errors.Is(err, ErrCompilationLoop) {
		t.Errorf("Increment at cap returned %v, want error wrapping ErrCompilationLoop", err)
	}
	// Counter must not have advanced.
	wantCap := uint64(traversalCap)
	if n != wantCap {
		t.Errorf("counter returned alongside ErrCompilationLoop = %d, want %d (counter must not advance past cap)", n, wantCap)
	}
	if got := cc.Get(runID, from, to); got != wantCap {
		t.Errorf("Get after cap-reached = %d, want %d (counter must not advance past cap)", got, wantCap)
	}
}

// TestCycleCounterEM043_CapOneEdge verifies that a cap of 1 allows exactly one
// traversal and fails on the second.
func TestCycleCounterEM043_CapOneEdge(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()
	from := NodeID("start")
	to := NodeID("end")
	traversalCap := 1

	n, err := cc.Increment(runID, from, to, &traversalCap)
	if err != nil {
		t.Fatalf("first Increment with cap=1 returned error: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}

	_, err = cc.Increment(runID, from, to, &traversalCap)
	if !errors.Is(err, ErrCompilationLoop) {
		t.Errorf("second Increment with cap=1: got %v, want ErrCompilationLoop", err)
	}
}

// --- Reset ---

// TestCycleCounterEM043_Reset verifies that Reset clears all counters for the
// target run and does not affect counters for other runs.
func TestCycleCounterEM043_Reset(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runA := cycleCounterFixtureRunID()
	runB := cycleCounterFixtureRunID()
	from := NodeID("a")
	to := NodeID("b")

	for range 2 {
		if _, err := cc.Increment(runA, from, to, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := cc.Increment(runB, from, to, nil); err != nil {
			t.Fatal(err)
		}
	}

	cc.Reset(runA)

	if got := cc.Get(runA, from, to); got != 0 {
		t.Errorf("after Reset(runA), runA counter = %d, want 0", got)
	}
	if got := cc.Get(runB, from, to); got != 2 {
		t.Errorf("after Reset(runA), runB counter = %d, want 2 (must be unaffected)", got)
	}
}

// --- Recovery from transition slice ---

// TestCycleCounterEM043_ReconcileFromTransitions verifies that
// ReconcileFromTransitions replays a slice of Transition records and sets
// in-memory counters to the correct derived counts. Spec reference: EM-043a
// (git-derived count is authoritative; ReconcileFromTransitions is the
// restart-recovery path).
func TestCycleCounterEM043_ReconcileFromTransitions(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()

	transitions := []Transition{
		cycleCounterFixtureTransition(runID, "a", "b"),
		cycleCounterFixtureTransition(runID, "b", "c"),
		cycleCounterFixtureTransition(runID, "a", "b"), // second traversal of a→b
		cycleCounterFixtureTransition(runID, "a", "b"), // third traversal of a→b
		cycleCounterFixtureTransition(runID, "b", "c"), // second traversal of b→c
	}

	cc.ReconcileFromTransitions(runID, transitions)

	tests := []struct {
		from NodeID
		to   NodeID
		want uint64
	}{
		{"a", "b", 3},
		{"b", "c", 2},
		{"c", "d", 0}, // never traversed
	}
	for _, tc := range tests {
		got := cc.Get(runID, tc.from, tc.to)
		if got != tc.want {
			t.Errorf("after Reconcile: Get(%s→%s) = %d, want %d", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestCycleCounterEM043_ReconcileOtherRunsIgnored verifies that transitions
// for other runs in a mixed slice do not affect the target run's counters.
func TestCycleCounterEM043_ReconcileOtherRunsIgnored(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runA := cycleCounterFixtureRunID()
	runB := cycleCounterFixtureRunID()

	mixed := []Transition{
		cycleCounterFixtureTransition(runA, "x", "y"),
		cycleCounterFixtureTransition(runB, "x", "y"), // different run — must be ignored
		cycleCounterFixtureTransition(runA, "x", "y"),
	}

	cc.ReconcileFromTransitions(runA, mixed)

	if got := cc.Get(runA, "x", "y"); got != 2 {
		t.Errorf("runA counter = %d, want 2 (only runA transitions should be counted)", got)
	}
	if got := cc.Get(runB, "x", "y"); got != 0 {
		t.Errorf("runB counter = %d, want 0 (runB was not reconciled)", got)
	}
}

// TestCycleCounterEM043_ReconcileIdempotent verifies that calling
// ReconcileFromTransitions twice with the same slice produces the same result
// as calling it once.
func TestCycleCounterEM043_ReconcileIdempotent(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()
	transitions := []Transition{
		cycleCounterFixtureTransition(runID, "p", "q"),
		cycleCounterFixtureTransition(runID, "p", "q"),
	}

	cc.ReconcileFromTransitions(runID, transitions)
	cc.ReconcileFromTransitions(runID, transitions) // idempotent second call

	if got := cc.Get(runID, "p", "q"); got != 2 {
		t.Errorf("after two identical Reconcile calls: counter = %d, want 2 (must be idempotent)", got)
	}
}

// TestCycleCounterEM043_ReconcileReplacesExistingInMemory verifies that
// ReconcileFromTransitions replaces existing in-memory counts for the run
// (not appended). This ensures restart recovery is authoritative.
func TestCycleCounterEM043_ReconcileReplacesExistingInMemory(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()

	// Pre-populate in-memory counter with a stale value.
	for range 5 {
		if _, err := cc.Increment(runID, "m", "n", nil); err != nil {
			t.Fatal(err)
		}
	}

	// Reconcile with a smaller authoritative count.
	transitions := []Transition{
		cycleCounterFixtureTransition(runID, "m", "n"),
		cycleCounterFixtureTransition(runID, "m", "n"),
	}
	cc.ReconcileFromTransitions(runID, transitions)

	if got := cc.Get(runID, "m", "n"); got != 2 {
		t.Errorf("after Reconcile over stale in-memory: counter = %d, want 2 (reconcile must replace, not add)", got)
	}
}

// --- Single edge participates in multiple cycles (OQ-EM-004) ---

// TestCycleCounterEM043_SingleEdgeMultiCycleSharedCounter verifies that when a
// single edge is traversed from different cycle paths, it shares one counter
// per-run. The counter advances regardless of which cycle path brought the
// traversal. Spec reference: EM-043a + OQ-EM-004.
func TestCycleCounterEM043_SingleEdgeMultiCycleSharedCounter(t *testing.T) {
	t.Parallel()

	cc := NewCycleCounter()
	runID := cycleCounterFixtureRunID()
	from := NodeID("shared-from")
	to := NodeID("shared-to")
	traversalCap := 4

	// Traverse three times (simulating three different cycle paths hitting the
	// same a→b edge). All three traversals must increment the same counter.
	for i := range 3 {
		n, err := cc.Increment(runID, from, to, &traversalCap)
		if err != nil {
			t.Fatalf("traversal %d returned unexpected error: %v", i+1, err)
		}
		want := uint64(i + 1) //nolint:gosec // G115: i is a bounded loop index, no overflow risk
		if n != want {
			t.Errorf("traversal %d: count = %d, want %d", i+1, n, want)
		}
	}

	// Fourth traversal still within cap.
	if _, err := cc.Increment(runID, from, to, &traversalCap); err != nil {
		t.Fatalf("fourth traversal (at cap) returned error: %v", err)
	}

	// Fifth traversal must fail.
	_, err := cc.Increment(runID, from, to, &traversalCap)
	if !errors.Is(err, ErrCompilationLoop) {
		t.Errorf("fifth traversal beyond cap: got %v, want ErrCompilationLoop", err)
	}
}
